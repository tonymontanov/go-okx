/*
FILE: orderbook/engine.go

DESCRIPTION:
engine.go implements a local order book engine for OKX. The source of truth is
the WebSocket `books` channel (snapshot + delta), validated by seqId/prevSeqId
and the CRC32 checksum of the top-25 levels. This is OKX's native protection
against missed updates; in addition there is the seqId sequence itself, as in
Binance.

MAIN ENTITIES:
  - Engine             — one instance per instrument.
  - Update             — structure of one delta update (action=update).
  - Snapshot           — structure of a snapshot (action=snapshot or REST snapshot).
  - GapKind            — category of the detected gap (for resync policies).

ALGORITHM:
  1. ApplySnapshot initializes local bid/ask slices + price→level index.
  2. ApplyUpdate checks prevSeqId == lastSeqId; if they differ —
     returns GapDetected and puts the engine into "needs resync" state.
  3. After applying the update, the CRC32 of the top-25 levels is compared
     with the received checksum; mismatch ⇒ GapDetected (checksum mismatch).
  4. Fallback check: a snapshot always resets state and compares checksum
     (if received).

CRC32 FORMAT (per OKX documentation):
  - Top-25 levels on each side (fewer if less are available).
  - String of the form "bid0_price:bid0_size:ask0_price:ask0_size:bid1_price:...".
  - If one side has fewer levels — the pairs from the other side are appended
    as-is without empty-pair separators.
  - Hash: stdlib crc32.ChecksumIEEE → result interpreted as int32
    (signed!), this is important: OKX sends checksum as signed int32 in JSON.

PERFORMANCE:
  - Levels are stored in sorted slices + map[price]→index.
  - On update — binary search (sort.Search) O(log n), insertion/deletion
    via copy. For depth <= 400 levels (cfg.MaxDepth) this fits in tens of
    nanoseconds per update.
  - For the checksum, a sync.Pool of []byte buffers is used to avoid
    allocating on every message (CRC32 is sent ~5x/sec per instrument).

DEPENDENCIES:
- hash/crc32: native CRC32 IEEE.
- sort: binary search.
- sync: Pool for buffers.
- github.com/shopspring/decimal: prices and volumes.
- swap/types: reuses the OrderBookLevel type.
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

// SideKind — order book side (for internal tables).
type SideKind uint8

const (
	// SideBid — bids (sorted in descending price order).
	SideBid SideKind = iota
	// SideAsk — asks (sorted in ascending price order).
	SideAsk
)

// GapKind — category of the detected gap.
type GapKind uint8

const (
	// GapNone — no gap; the update applied cleanly.
	GapNone GapKind = iota
	// GapSequence — prevSeqId does not match the local lastSeqId.
	GapSequence
	// GapChecksum — CRC32 mismatch after applying the update.
	GapChecksum
)

// String — human-readable name for logs/metrics.
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

// Snapshot — order book snapshot for engine initialization.
type Snapshot struct {
	InstID    string
	Bids      []types.OrderBookLevel
	Asks      []types.OrderBookLevel
	SeqID     int64
	PrevSeqID int64
	Checksum  int32 // 0 if not received
	TsMs      int64
}

// Update — delta order book update.
type Update struct {
	InstID    string
	Bids      []types.OrderBookLevel // changed levels only
	Asks      []types.OrderBookLevel
	SeqID     int64
	PrevSeqID int64
	Checksum  int32
	TsMs      int64
}

// ApplyResult — result of applying an update.
type ApplyResult struct {
	Gap     GapKind
	SeqID   int64
	TsMs    int64
	BidsLen int
	AsksLen int
}

// Engine — order book engine for one instrument.
type Engine struct {
	instID         string
	maxDepth       int
	checksumLevels int

	mu        sync.RWMutex
	bids      []types.OrderBookLevel
	asks      []types.OrderBookLevel
	lastSeqID int64
	lastTsMs  int64
	dirty     bool // if true — resync is needed (gap, checksum mismatch)
}

// NewEngine creates an empty engine. maxDepth limits the local order book depth
// (default 400). checksumLevels is the CRC32 depth (always 25 per OKX spec;
// parameterized in case the protocol changes).
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

// InstID returns the instrument identifier.
func (e *Engine) InstID() string { return e.instID }

// IsDirty returns true if the engine requires resync (snapshot + ApplyUpdate
// after clearing).
func (e *Engine) IsDirty() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.dirty
}

// LastSeqID returns the last applied seqId (0 if nothing has been applied).
func (e *Engine) LastSeqID() int64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.lastSeqID
}

/*
ApplySnapshot replaces the local order book with snapshot data. If the snapshot
has Checksum != 0 — it is compared against the CRC32 of the top-25 local levels;
a mismatch is returned as GapChecksum (but the local state is still updated with
the snapshot — otherwise we would hang on a persistent divergence).
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
ApplyUpdate applies a delta update to the local order book.

Behavior:
  - If u.PrevSeqID != lastSeqID — returns GapSequence, sets dirty=true,
    local state is NOT modified. The caller must resync via REST snapshot
    or a new WS subscription.
  - If consistent — applies the update; levels with size==0 are removed.
  - After applying, if u.Checksum != 0, computes CRC32 of the top-25 and
    compares with the received value; mismatch ⇒ GapChecksum + dirty=true,
    but the changes are already applied (same as for snapshot).
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

// TopLevels returns a copy of the top-n bid/ask levels. n <= 0 ⇒ takes min(maxDepth, current).
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

// BestBidAsk returns the best (first) bid and ask level. Empty decimal.Decimal
// if either side is empty.
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

// MarkResynced clears the dirty flag and accepts new seqId values. Called by the
// consumer after a successful resync (e.g. after receiving a fresh snapshot).
func (e *Engine) MarkResynced(seqID int64, tsMs int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.lastSeqID = seqID
	e.lastTsMs = tsMs
	e.dirty = false
}

// applyLevelLocked applies one level. Size 0 ⇒ remove the level.
// e.mu must be held for writing.
func (e *Engine) applyLevelLocked(side SideKind, lvl types.OrderBookLevel) {
	var slice *[]types.OrderBookLevel
	var less func(a, b decimal.Decimal) bool
	if side == SideBid {
		slice = &e.bids
		// bids are sorted in descending price order: priceA > priceB ⇒ A before B.
		less = func(a, b decimal.Decimal) bool { return a.GreaterThan(b) }
	} else {
		slice = &e.asks
		less = func(a, b decimal.Decimal) bool { return a.LessThan(b) }
	}

	// binary search for the position
	var arr []types.OrderBookLevel = *slice
	var idx int = sort.Search(len(arr), func(i int) bool {
		// Return true for indices "not before" the given price.
		// less(arr[i], lvl) → true means: arr[i] is STRICTLY BEFORE lvl
		// ⇒ find the first i for which arr[i] is not strictly before lvl.
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

// trimLocked trims the local order book to maxDepth. e.mu must be held.
func (e *Engine) trimLocked() {
	if len(e.bids) > e.maxDepth {
		e.bids = e.bids[:e.maxDepth]
	}
	if len(e.asks) > e.maxDepth {
		e.asks = e.asks[:e.maxDepth]
	}
}

// checksumLocked computes the CRC32 IEEE of the top-checksumLevels levels in
// OKX format: "b0px:b0sz:a0px:a0sz:b1px:b1sz:a1px:a1sz:..." (pairs from one
// side are appended if the other is shorter). Returns signed int32, as OKX does.
func (e *Engine) checksumLocked() int32 {
	var bp *strings.Builder = buildersPool.Get().(*strings.Builder)
	bp.Reset()
	defer buildersPool.Put(bp)

	var n int = e.checksumLevels
	var nb int = len(e.bids)
	var na int = len(e.asks)
	if n > nb && n > na {
		// take minimum but iterate to max(nb, na)
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

// buildersPool — pool of strings.Builder for CRC32 calculation (avoids
// allocations per update).
var buildersPool = sync.Pool{
	New: func() any { return &strings.Builder{} },
}

// copyLevelsSortedDesc copies levels and sorts them in descending price order (bids).
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

// copyLevelsSortedAsc copies levels and sorts them in ascending price order (asks).
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
