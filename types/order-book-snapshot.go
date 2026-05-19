/*
FILE: types/order-book-snapshot.go

DESCRIPTION:
Order book snapshot: a set of bid/ask levels plus seqId and timestamp metadata.
Returned by the GetOrderBook REST endpoint and used as input to
OrderbookEngine.ApplySnapshot.

The format is identical for spot and swap (one OKX endpoint /api/v5/market/books,
differentiated only by instID — without or with the -SWAP suffix).

FIELDS:
  - InstID    — instrument.
  - Bids      — buy levels, sorted in descending price order.
  - Asks      — sell levels, sorted in ascending price order.
  - SeqID     — current seqId (for synchronization with WS deltas).
  - PrevSeqID — prevSeqId (useful during resync).
  - Checksum  — CRC32 of top-25 levels (present when delivered by the WS books channel snapshot).
  - Ts        — OKX timestamp (ms).
*/

package types

// OrderBookSnapshot — order book snapshot for a single instrument.
type OrderBookSnapshot struct {
	InstID    string
	Bids      []OrderBookLevel
	Asks      []OrderBookLevel
	SeqID     int64
	PrevSeqID int64
	Checksum  int32
	Ts        int64
}
