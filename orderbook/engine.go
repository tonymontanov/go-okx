/*
ФАЙЛ: orderbook/engine.go

ОПИСАНИЕ:
Файл engine.go реализует движок локального стакана для OKX. Источник истины —
WebSocket-канал `books` (snapshot + delta), валидируется по `seqId/prevSeqId`
и CRC32-checksum'у топ-25 уровней. Это «нативная» защита OKX от пропуска
обновлений; помимо неё есть собственно последовательность seqId, как в Binance.

ОСНОВНЫЕ СУЩНОСТИ:
  - Engine             — инстанс на один инструмент.
  - Update             — структура одного дельта-обновления (action=update).
  - Snapshot           — структура снапшота (action=snapshot, либо REST-snapshot).
  - GapKind            — категория обнаруженного разрыва (для resync-политик).

АЛГОРИТМ:
  1. ApplySnapshot инициализирует локальные срезы bid/ask + индекс цена→уровень.
  2. ApplyUpdate проверяет prevSeqId == lastSeqId; если не совпадает —
     возвращается GapDetected, движок переводится в state "needs resync".
  3. После применения апдейта сравнивается CRC32 топ-25 уровней с
     присланным checksum'ом; mismatch ⇒ GapDetected (checksum mismatch).
  4. Резервная проверка: snapshot всегда сбрасывает state и сравнивает checksum
     (если приехал).

ФОРМАТ CRC32 (по документации OKX):
  - Топ-25 уровней с каждой стороны (если меньше, берём столько, сколько есть).
  - Строка вида "bid0_price:bid0_size:ask0_price:ask0_size:bid1_price:...".
  - При меньшем количестве уровней с одной стороны — пары другой стороны
    дописываются «как есть» без разделителей пустых пар.
  - Hash: stdlib crc32.ChecksumIEEE → результат интерпретируется как int32
    (signed!), это важно: OKX в JSON передаёт checksum как signed int32.

ПРОИЗВОДИТЕЛЬНОСТЬ:
  - Уровни хранятся в отсортированных слайсах + map[price]→index.
  - При апдейте — бинарный поиск (sort.Search) O(log n), вставка/удаление
    через copy. Для глубины <= 400 уровней (cfg.MaxDepth) это укладывается
    в десятки наносекунд на апдейт.
  - Для контрольной суммы используется sync.Pool из []byte-буферов, чтобы
    не аллоцировать на каждом сообщении (CRC32 шлётся ~5x/sec на инструмент).

ЗАВИСИМОСТИ:
- hash/crc32: nativный CRC32 IEEE.
- sort: бинарный поиск.
- sync: Pool для буферов.
- github.com/shopspring/decimal: цены и объёмы.
- swap/types: переиспользуем тип OrderBookLevel.
*/

package orderbook

import (
	"hash/crc32"
	"sort"
	"strings"
	"sync"

	"github.com/shopspring/decimal"
	"github.com/tonymontanov/go-okx/v2/swap/types"
)

// SideKind — сторона стакана (для внутренних таблиц).
type SideKind uint8

const (
	// SideBid — биды (упорядочены по убыванию цены).
	SideBid SideKind = iota
	// SideAsk — аски (упорядочены по возрастанию цены).
	SideAsk
)

// GapKind — категория обнаруженного разрыва.
type GapKind uint8

const (
	// GapNone — без разрыва, апдейт применился чисто.
	GapNone GapKind = iota
	// GapSequence — несовпадение prevSeqId с локальным lastSeqId.
	GapSequence
	// GapChecksum — несовпадение CRC32 после применения апдейта.
	GapChecksum
)

// String — человекочитаемое имя для логов/метрик.
func (g GapKind) String() string {
	switch g {
	case GapSequence:
		return "sequence"
	case GapChecksum:
		return "checksum"
	default:
		return "none"
	}
}

// Snapshot — снимок стакана для инициализации движка.
type Snapshot struct {
	InstID    string
	Bids      []types.OrderBookLevel
	Asks      []types.OrderBookLevel
	SeqID     int64
	PrevSeqID int64
	Checksum  int32 // 0 если не пришёл
	TsMs      int64
}

// Update — дельта-обновление стакана.
type Update struct {
	InstID    string
	Bids      []types.OrderBookLevel // только меняющиеся уровни
	Asks      []types.OrderBookLevel
	SeqID     int64
	PrevSeqID int64
	Checksum  int32
	TsMs      int64
}

// ApplyResult — результат применения апдейта.
type ApplyResult struct {
	Gap     GapKind
	SeqID   int64
	TsMs    int64
	BidsLen int
	AsksLen int
}

// Engine — движок стакана для одного инструмента.
type Engine struct {
	instID         string
	maxDepth       int
	checksumLevels int

	mu        sync.RWMutex
	bids      []types.OrderBookLevel
	asks      []types.OrderBookLevel
	lastSeqID int64
	lastTsMs  int64
	dirty     bool // если true — нужен resync (gap, checksum mismatch)
}

// NewEngine создаёт пустой движок. maxDepth ограничивает глубину локального
// стакана (по умолчанию 400). checksumLevels — глубина CRC32 (всегда 25 по
// спецификации OKX; параметризовано на случай изменений протокола).
func NewEngine(instID string, maxDepth, checksumLevels int) *Engine {
	if maxDepth <= 0 {
		maxDepth = 400
	}
	if checksumLevels <= 0 {
		checksumLevels = 25
	}
	return &Engine{
		instID:         instID,
		maxDepth:       maxDepth,
		checksumLevels: checksumLevels,
		bids:           make([]types.OrderBookLevel, 0, maxDepth),
		asks:           make([]types.OrderBookLevel, 0, maxDepth),
	}
}

// InstID возвращает идентификатор инструмента.
func (e *Engine) InstID() string { return e.instID }

// IsDirty возвращает true, если движок требует resync (snapshot + ApplyUpdate
// после очистки).
func (e *Engine) IsDirty() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.dirty
}

// LastSeqID возвращает последний применённый seqId (0 если ничего не применено).
func (e *Engine) LastSeqID() int64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.lastSeqID
}

/*
ApplySnapshot заменяет локальный стакан данными снапшота. Если у снапшота
есть Checksum != 0 — он сравнивается с CRC32 топ-25 локального стакана;
mismatch возвращается как GapChecksum (но локальное состояние всё равно
обновляется на snapshot — иначе при стабильном расхождении мы зависнем).
*/
func (e *Engine) ApplySnapshot(s Snapshot) ApplyResult {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.bids = copyLevelsSortedDesc(s.Bids, e.maxDepth)
	e.asks = copyLevelsSortedAsc(s.Asks, e.maxDepth)
	e.lastSeqID = s.SeqID
	e.lastTsMs = s.TsMs
	e.dirty = false

	var res ApplyResult = ApplyResult{
		Gap:     GapNone,
		SeqID:   s.SeqID,
		TsMs:    s.TsMs,
		BidsLen: len(e.bids),
		AsksLen: len(e.asks),
	}
	if s.Checksum != 0 {
		var local int32 = e.checksumLocked()
		if local != s.Checksum {
			res.Gap = GapChecksum
			e.dirty = true
		}
	}
	return res
}

/*
ApplyUpdate применяет дельта-обновление к локальному стакану.

Поведение:
  - Если u.PrevSeqID != lastSeqID — возвращает GapSequence, ставит dirty=true,
    локальное состояние НЕ модифицируется. Вызывающий код должен сделать
    resync через REST snapshot или новую WS-подписку.
  - Если консистентно — применяет апдейт; уровни size==0 удаляются.
  - После применения, если u.Checksum != 0, считает CRC32 топ-25 и сравнивает
    с присланным; mismatch ⇒ GapChecksum + dirty=true, но изменения уже
    применены (как и в случае snapshot).
*/
func (e *Engine) ApplyUpdate(u Update) ApplyResult {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.lastSeqID != 0 && u.PrevSeqID != 0 && u.PrevSeqID != e.lastSeqID {
		e.dirty = true
		return ApplyResult{Gap: GapSequence, SeqID: e.lastSeqID, TsMs: e.lastTsMs, BidsLen: len(e.bids), AsksLen: len(e.asks)}
	}

	var i int
	for i = 0; i < len(u.Bids); i++ {
		e.applyLevelLocked(SideBid, u.Bids[i])
	}
	for i = 0; i < len(u.Asks); i++ {
		e.applyLevelLocked(SideAsk, u.Asks[i])
	}

	e.trimLocked()
	e.lastSeqID = u.SeqID
	e.lastTsMs = u.TsMs

	var res ApplyResult = ApplyResult{
		Gap:     GapNone,
		SeqID:   u.SeqID,
		TsMs:    u.TsMs,
		BidsLen: len(e.bids),
		AsksLen: len(e.asks),
	}
	if u.Checksum != 0 {
		var local int32 = e.checksumLocked()
		if local != u.Checksum {
			res.Gap = GapChecksum
			e.dirty = true
		}
	}
	return res
}

// TopLevels возвращает копию топ-n уровней bid/ask. n <= 0 ⇒ берёт min(maxDepth, current).
func (e *Engine) TopLevels(n int) (bids, asks []types.OrderBookLevel) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var nb int = len(e.bids)
	var na int = len(e.asks)
	if n > 0 {
		if n < nb {
			nb = n
		}
		if n < na {
			na = n
		}
	}
	bids = make([]types.OrderBookLevel, nb)
	asks = make([]types.OrderBookLevel, na)
	copy(bids, e.bids[:nb])
	copy(asks, e.asks[:na])
	return bids, asks
}

// BestBidAsk возвращает первый уровень bid и ask. Пустые decimal.Decimal если
// стороны пусты.
func (e *Engine) BestBidAsk() (bidPx, bidSz, askPx, askSz decimal.Decimal) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if len(e.bids) > 0 {
		bidPx = e.bids[0].Price
		bidSz = e.bids[0].Size
	}
	if len(e.asks) > 0 {
		askPx = e.asks[0].Price
		askSz = e.asks[0].Size
	}
	return bidPx, bidSz, askPx, askSz
}

// MarkResynced сбрасывает флаг dirty и принимает новые seqId. Вызывается
// потребителем после успешного resync (например, после получения свежего
// snapshot).
func (e *Engine) MarkResynced(seqID int64, tsMs int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.lastSeqID = seqID
	e.lastTsMs = tsMs
	e.dirty = false
}

// applyLevelLocked применяет один уровень. Размер 0 ⇒ удаление уровня.
// e.mu должен быть захвачен на запись.
func (e *Engine) applyLevelLocked(side SideKind, lvl types.OrderBookLevel) {
	var slice *[]types.OrderBookLevel
	var less func(a, b decimal.Decimal) bool
	if side == SideBid {
		slice = &e.bids
		// bids отсортированы по убыванию цены: pricesA > pricesB ⇒ A раньше B.
		less = func(a, b decimal.Decimal) bool { return a.GreaterThan(b) }
	} else {
		slice = &e.asks
		less = func(a, b decimal.Decimal) bool { return a.LessThan(b) }
	}

	// бинарный поиск позиции
	var arr []types.OrderBookLevel = *slice
	var idx int = sort.Search(len(arr), func(i int) bool {
		// Возвращаем true для индексов "не раньше" заданной цены.
		// less(arr[i], lvl) → true означает: arr[i] СТРОГО ПЕРЕД lvl
		// ⇒ ищем первый i, для которого arr[i] не строго перед lvl.
		return !less(arr[i].Price, lvl.Price)
	})

	if idx < len(arr) && arr[idx].Price.Equal(lvl.Price) {
		if lvl.Size.IsZero() {
			arr = append(arr[:idx], arr[idx+1:]...)
		} else {
			arr[idx].Size = lvl.Size
		}
	} else if !lvl.Size.IsZero() {
		arr = append(arr, types.OrderBookLevel{})
		copy(arr[idx+1:], arr[idx:])
		arr[idx] = lvl
	}
	*slice = arr
}

// trimLocked отрезает локальный стакан до maxDepth. e.mu должен быть захвачен.
func (e *Engine) trimLocked() {
	if len(e.bids) > e.maxDepth {
		e.bids = e.bids[:e.maxDepth]
	}
	if len(e.asks) > e.maxDepth {
		e.asks = e.asks[:e.maxDepth]
	}
}

// checksumLocked считает CRC32 IEEE топ-checksumLevels уровней в формате OKX:
// "b0px:b0sz:a0px:a0sz:b1px:b1sz:a1px:a1sz:..." (пары с одной стороны
// дописываются, если другая короче). Возвращает signed int32, как OKX.
func (e *Engine) checksumLocked() int32 {
	var bp *strings.Builder = buildersPool.Get().(*strings.Builder)
	bp.Reset()
	defer buildersPool.Put(bp)

	var n int = e.checksumLevels
	var nb int = len(e.bids)
	var na int = len(e.asks)
	if n > nb && n > na {
		// возьмём минимум, но идём до max(nb, na)
	}

	var i int
	var written int
	for i = 0; i < n; i++ {
		if i >= nb && i >= na {
			break
		}
		if i < nb {
			if written > 0 {
				bp.WriteByte(':')
			}
			bp.WriteString(e.bids[i].Price.String())
			bp.WriteByte(':')
			bp.WriteString(e.bids[i].Size.String())
			written++
		}
		if i < na {
			if written > 0 {
				bp.WriteByte(':')
			}
			bp.WriteString(e.asks[i].Price.String())
			bp.WriteByte(':')
			bp.WriteString(e.asks[i].Size.String())
			written++
		}
	}

	var sum uint32 = crc32.ChecksumIEEE([]byte(bp.String()))
	return int32(sum)
}

// buildersPool — пул strings.Builder для CRC32 расчёта (избегаем аллокаций
// под каждый апдейт).
var buildersPool = sync.Pool{
	New: func() any { return &strings.Builder{} },
}

// copyLevelsSortedDesc копирует уровни и сортирует по убыванию цены (биды).
func copyLevelsSortedDesc(src []types.OrderBookLevel, max int) []types.OrderBookLevel {
	var out []types.OrderBookLevel = make([]types.OrderBookLevel, len(src))
	copy(out, src)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Price.GreaterThan(out[j].Price)
	})
	if len(out) > max {
		out = out[:max]
	}
	return out
}

// copyLevelsSortedAsc копирует уровни и сортирует по возрастанию цены (аски).
func copyLevelsSortedAsc(src []types.OrderBookLevel, max int) []types.OrderBookLevel {
	var out []types.OrderBookLevel = make([]types.OrderBookLevel, len(src))
	copy(out, src)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Price.LessThan(out[j].Price)
	})
	if len(out) > max {
		out = out[:max]
	}
	return out
}
