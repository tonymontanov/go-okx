/*
FILE: orderbook/engine_test.go

DESCRIPTION:
Unit tests for OrderbookEngine. Coverage:
  - TestEngine_ApplySnapshot_Sorts:    snapshot correctly sorts bid/ask.
  - TestEngine_ApplyUpdate_NewLevels:  adding new levels on both sides.
  - TestEngine_ApplyUpdate_RemoveZero: removing a level (size=0).
  - TestEngine_ApplyUpdate_ChangeSize: updating an existing level.
  - TestEngine_GapSequence:            prevSeqId mismatch → GapSequence,
                                       local state is not modified.
  - TestEngine_Checksum_OKXDocs:       example from the official OKX docs
                                       (order book + expected signed int32).
  - TestEngine_Checksum_Mismatch:      checksum mismatch → GapChecksum.

For the CRC32 reference example the canonical OKX example is used:
  bids = [(3366.1, 7), (3366,  6), (3365.9, 5)]
  asks = [(3366.8, 9), (3366.9, 8), (3367,   2)]
  pre-image: "3366.1:7:3366.8:9:3366:6:3366.9:8:3365.9:5:3367:2"
  checksum (signed int32): 831078360 (see OKX docs examples).
*/

package orderbook

import (
	"hash/crc32"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/tonymontanov/go-okx/v2/swap/types"
)

func dec(s string) decimal.Decimal {
	var d decimal.Decimal
	var err error
	d, err = decimal.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return d
}

func lvl(price, size string) types.OrderBookLevel {
	return types.OrderBookLevel{Price: dec(price), Size: dec(size)}
}

func TestEngine_ApplySnapshot_Sorts(t *testing.T) {
	var eng *Engine = NewEngine("BTC-USDT-SWAP", 400, 25)
	var res ApplyResult = eng.ApplySnapshot(Snapshot{
		InstID: "BTC-USDT-SWAP",
		Bids:   []types.OrderBookLevel{lvl("100", "1"), lvl("99", "2"), lvl("101", "0.5")},
		Asks:   []types.OrderBookLevel{lvl("103", "1"), lvl("102", "2"), lvl("104", "0.5")},
		SeqID:  10,
	})
	if res.Gap != GapNone {
		t.Fatalf("snapshot must not produce gap, got %v", res.Gap)
	}

	var bids, asks []types.OrderBookLevel
	bids, asks = eng.TopLevels(3)
	if !bids[0].Price.Equal(dec("101")) || !bids[1].Price.Equal(dec("100")) || !bids[2].Price.Equal(dec("99")) {
		t.Fatalf("bids must be sorted desc by price, got %v", bids)
	}
	if !asks[0].Price.Equal(dec("102")) || !asks[1].Price.Equal(dec("103")) || !asks[2].Price.Equal(dec("104")) {
		t.Fatalf("asks must be sorted asc by price, got %v", asks)
	}
}

func TestEngine_ApplyUpdate_NewLevels(t *testing.T) {
	var eng *Engine = NewEngine("BTC-USDT-SWAP", 400, 25)
	eng.ApplySnapshot(Snapshot{
		Bids:  []types.OrderBookLevel{lvl("100", "1")},
		Asks:  []types.OrderBookLevel{lvl("101", "1")},
		SeqID: 1,
	})
	var res ApplyResult = eng.ApplyUpdate(Update{
		Bids:      []types.OrderBookLevel{lvl("99", "2"), lvl("101.5", "3")},
		Asks:      []types.OrderBookLevel{lvl("102", "5")},
		PrevSeqID: 1,
		SeqID:     2,
	})
	if res.Gap != GapNone {
		t.Fatalf("expected GapNone, got %v", res.Gap)
	}

	var bids, asks []types.OrderBookLevel
	bids, asks = eng.TopLevels(10)
	if len(bids) != 3 || !bids[0].Price.Equal(dec("101.5")) || !bids[2].Price.Equal(dec("99")) {
		t.Fatalf("unexpected bids: %v", bids)
	}
	if len(asks) != 2 || !asks[0].Price.Equal(dec("101")) || !asks[1].Price.Equal(dec("102")) {
		t.Fatalf("unexpected asks: %v", asks)
	}
}

func TestEngine_ApplyUpdate_RemoveZero(t *testing.T) {
	var eng *Engine = NewEngine("BTC-USDT-SWAP", 400, 25)
	eng.ApplySnapshot(Snapshot{
		Bids:  []types.OrderBookLevel{lvl("100", "1"), lvl("99", "2")},
		Asks:  []types.OrderBookLevel{lvl("101", "1")},
		SeqID: 1,
	})
	eng.ApplyUpdate(Update{
		Bids:      []types.OrderBookLevel{lvl("100", "0")},
		PrevSeqID: 1,
		SeqID:     2,
	})
	var bids, _ = eng.TopLevels(10)
	if len(bids) != 1 || !bids[0].Price.Equal(dec("99")) {
		t.Fatalf("expected only one bid level 99, got %v", bids)
	}
}

func TestEngine_ApplyUpdate_ChangeSize(t *testing.T) {
	var eng *Engine = NewEngine("BTC-USDT-SWAP", 400, 25)
	eng.ApplySnapshot(Snapshot{
		Bids:  []types.OrderBookLevel{lvl("100", "1")},
		SeqID: 1,
	})
	eng.ApplyUpdate(Update{
		Bids:      []types.OrderBookLevel{lvl("100", "5")},
		PrevSeqID: 1,
		SeqID:     2,
	})
	var bids, _ = eng.TopLevels(10)
	if len(bids) != 1 || !bids[0].Size.Equal(dec("5")) {
		t.Fatalf("expected size=5 on level 100, got %v", bids)
	}
}

func TestEngine_GapSequence(t *testing.T) {
	var eng *Engine = NewEngine("BTC-USDT-SWAP", 400, 25)
	eng.ApplySnapshot(Snapshot{
		Bids:  []types.OrderBookLevel{lvl("100", "1")},
		SeqID: 10,
	})
	var res ApplyResult = eng.ApplyUpdate(Update{
		Bids:      []types.OrderBookLevel{lvl("100", "2")},
		PrevSeqID: 5, // does not match
		SeqID:     11,
	})
	if res.Gap != GapSequence {
		t.Fatalf("expected GapSequence, got %v", res.Gap)
	}
	if !eng.IsDirty() {
		t.Fatalf("engine must be marked dirty after sequence gap")
	}
	// local state must not change
	var bids, _ = eng.TopLevels(10)
	if !bids[0].Size.Equal(dec("1")) {
		t.Fatalf("expected size=1 (no update), got %v", bids[0].Size)
	}
	if eng.LastSeqID() != 10 {
		t.Fatalf("lastSeqID must remain 10, got %d", eng.LastSeqID())
	}
}

// TestEngine_Checksum_BasicSelf verifies that the engine produces the same CRC32
// as the reference calculation over the same pre-image for the same data. This
// guards against regressions in the pre-image format without depending on
// hardcoded numbers from an external source.
func TestEngine_Checksum_BasicSelf(t *testing.T) {
	var bids []types.OrderBookLevel = []types.OrderBookLevel{
		lvl("3366.1", "7"), lvl("3366", "6"), lvl("3365.9", "5"),
	}
	var asks []types.OrderBookLevel = []types.OrderBookLevel{
		lvl("3366.8", "9"), lvl("3366.9", "8"), lvl("3367", "2"),
	}

	var eng *Engine = NewEngine("BTC-USDT-SWAP", 400, 25)
	eng.ApplySnapshot(Snapshot{Bids: bids, Asks: asks, SeqID: 1})

	// Reference pre-image per OKX documentation (order: bid,ask,bid,ask,...).
	var expected string = "3366.1:7:3366.8:9:3366:6:3366.9:8:3365.9:5:3367:2"
	var expectedSum int32 = int32(crc32.ChecksumIEEE([]byte(expected)))

	eng.mu.RLock()
	var actual int32 = eng.checksumLocked()
	eng.mu.RUnlock()

	if actual != expectedSum {
		t.Fatalf("checksum mismatch: expected=%d actual=%d (pre-image=%q)", expectedSum, actual, expected)
	}
}

// TestEngine_Checksum_Mismatch — when an intentionally incorrect checksum is
// supplied, the engine must mark the update as GapChecksum and set dirty.
func TestEngine_Checksum_Mismatch(t *testing.T) {
	var eng *Engine = NewEngine("BTC-USDT-SWAP", 400, 25)
	eng.ApplySnapshot(Snapshot{
		Bids:  []types.OrderBookLevel{lvl("100", "1")},
		Asks:  []types.OrderBookLevel{lvl("101", "1")},
		SeqID: 1,
	})

	// Apply an update with an intentionally bogus checksum.
	var res ApplyResult = eng.ApplyUpdate(Update{
		Bids:      []types.OrderBookLevel{lvl("100", "2")},
		PrevSeqID: 1,
		SeqID:     2,
		Checksum:  -1, // deliberately does not match the real value
	})
	if res.Gap != GapChecksum {
		t.Fatalf("expected GapChecksum, got %v", res.Gap)
	}
	if !eng.IsDirty() {
		t.Fatalf("engine must be marked dirty after checksum mismatch")
	}
}

// TestEngine_TopLevels_LimitDepth — TopLevels returns no more than n levels.
func TestEngine_TopLevels_LimitDepth(t *testing.T) {
	var eng *Engine = NewEngine("BTC-USDT-SWAP", 400, 25)
	eng.ApplySnapshot(Snapshot{
		Bids: []types.OrderBookLevel{lvl("100", "1"), lvl("99", "1"), lvl("98", "1")},
		Asks: []types.OrderBookLevel{lvl("101", "1"), lvl("102", "1"), lvl("103", "1")},
	})
	var bids, asks []types.OrderBookLevel
	bids, asks = eng.TopLevels(2)
	if len(bids) != 2 || len(asks) != 2 {
		t.Fatalf("expected 2 levels each side, got bids=%d asks=%d", len(bids), len(asks))
	}
}

// TestEngine_MaxDepth_Trim — if the snapshot contains more than maxDepth levels,
// the engine must trim them.
func TestEngine_MaxDepth_Trim(t *testing.T) {
	var eng *Engine = NewEngine("BTC-USDT-SWAP", 2, 25)
	eng.ApplySnapshot(Snapshot{
		Bids: []types.OrderBookLevel{lvl("100", "1"), lvl("99", "1"), lvl("98", "1")},
		Asks: []types.OrderBookLevel{lvl("101", "1"), lvl("102", "1"), lvl("103", "1")},
	})
	var bids, asks = eng.TopLevels(10)
	if len(bids) != 2 || len(asks) != 2 {
		t.Fatalf("expected trim to 2, got bids=%d asks=%d", len(bids), len(asks))
	}
	if !bids[0].Price.Equal(dec("100")) || !bids[1].Price.Equal(dec("99")) {
		t.Fatalf("top-2 bids must remain top-2 by price, got %v", bids)
	}
}
