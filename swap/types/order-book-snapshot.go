/*
ФАЙЛ: swap/types/order-book-snapshot.go

ОПИСАНИЕ:
Снимок стакана: набор bid/ask уровней + метаинформация о seqId и времени.
Возвращается REST endpoint'ом GetOrderBook и используется как input для
OrderbookEngine.ApplySnapshot.

ПОЛЯ:
  - InstID   — инструмент.
  - Bids     — уровни покупок, отсортированы по убыванию цены.
  - Asks     — уровни продаж, отсортированы по возрастанию цены.
  - SeqID    — текущий seqId (для синхронизации с WS-deltas).
  - PrevSeqID — prevSeqId (полезен при resync).
  - Checksum — CRC32 топ-25 уровней (если пришёл со снапшотом WS-канала books).
  - Ts       — таймштамп OKX (мс).
*/

package types

// OrderBookSnapshot — снапшот стакана для одного инструмента.
type OrderBookSnapshot struct {
	InstID    string
	Bids      []OrderBookLevel
	Asks      []OrderBookLevel
	SeqID     int64
	PrevSeqID int64
	Checksum  int32
	Ts        int64
}
