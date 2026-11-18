package hlsign

import (
	"errors"
	"testing"
)

var cfg = StopConfig{SlippageBufferPct: 1.0, MinDistancePct: 0.1, RatchetOnly: true}

// --- Marketability: SL tidak pernah boleh langsung terisi (audit poin 2) ---

func TestBuildStopLoss_NeverImmediatelyExecutable(t *testing.T) {
	cases := []struct {
		name    string
		side    PositionSide
		mark    float64
		trigger float64
	}{
		{"long_sl_below_mark", SideLong, 110, 95},
		{"long_sl_far_below_mark", SideLong, 110, 50},
		{"short_sl_above_mark", SideShort, 90, 95},
		{"short_sl_far_above_mark", SideShort, 90, 150},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			spec, err := BuildStopLossSpec(0, c.side, 1.0, c.trigger, nil, 4, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if spec.OrderType.Trigger == nil {
				t.Fatal("stop loss must be a trigger order, not a plain limit order")
			}
			if spec.OrderType.Trigger.Tpsl != "sl" {
				t.Fatalf("tpsl = %q, want sl", spec.OrderType.Trigger.Tpsl)
			}
			if !spec.ReduceOnly {
				t.Fatal("stop must be reduce-only")
			}
			if WouldExecuteImmediately(spec, c.mark) {
				t.Fatalf("stop would execute immediately at mark=%v: %+v", c.mark, spec)
			}
		})
	}
}

func TestValidateStopPlacement_RejectsAlreadyBreached(t *testing.T) {
	// Long, mark 100, trigger 105 (di ATAS mark) -> harusnya sudah kena, tolak.
	if err := ValidateStopPlacement(SideLong, 105, 100, cfg); !errors.Is(err, ErrStopWouldTriggerImmediately) {
		t.Fatalf("want ErrStopWouldTriggerImmediately, got %v", err)
	}
	// Short, mark 100, trigger 95 (di BAWAH mark) -> sudah kena, tolak.
	if err := ValidateStopPlacement(SideShort, 95, 100, cfg); !errors.Is(err, ErrStopWouldTriggerImmediately) {
		t.Fatalf("want ErrStopWouldTriggerImmediately, got %v", err)
	}
}

func TestValidateStopPlacement_RejectsTooClose(t *testing.T) {
	tight := StopConfig{MinDistancePct: 5.0}
	if err := ValidateStopPlacement(SideLong, 99.9, 100, tight); !errors.Is(err, ErrStopTooClose) {
		t.Fatalf("want ErrStopTooClose, got %v", err)
	}
}

// --- Regresi terhadap bug asli: format fixed lama tidak boleh pernah kembali ---

func TestBuildStopLoss_PriceIsProperlyRounded(t *testing.T) {
	spec, err := BuildStopLossSpec(0, SideLong, 1.0, 95.123456789, nil, 4, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if spec.OrderType.Trigger.TriggerPx == 95.123456789 {
		t.Fatal("trigger price was not rounded to Hyperliquid's price rules")
	}
}

// --- Ratchet: stop tidak boleh mundur ---

type fakeVenue struct {
	pos       Position
	mark      float64
	orders    map[int64]OrderSpec
	nextID    int64
	modifyErr error
	placeErr  error
}

func (f *fakeVenue) Position(coin string) (Position, error) {
	if f.pos.Size <= 0 {
		return Position{}, ErrNoPosition
	}
	return f.pos, nil
}
func (f *fakeVenue) Mark(coin string) (float64, error) { return f.mark, nil }
func (f *fakeVenue) PlaceStop(spec OrderSpec) (int64, error) {
	if f.placeErr != nil {
		return 0, f.placeErr
	}
	f.nextID++
	f.orders[f.nextID] = spec
	return f.nextID, nil
}
func (f *fakeVenue) ModifyStop(oid int64, spec OrderSpec) (int64, error) {
	if f.modifyErr != nil {
		return 0, f.modifyErr
	}
	if _, ok := f.orders[oid]; !ok {
		return 0, ErrOrderNotFound
	}
	f.nextID++
	delete(f.orders, oid)
	f.orders[f.nextID] = spec
	return f.nextID, nil
}
func (f *fakeVenue) CancelOrder(coin string, oid int64) error {
	if _, ok := f.orders[oid]; !ok {
		return ErrOrderNotFound
	}
	delete(f.orders, oid)
	return nil
}

func TestMoveStop_RatchetRejectsLoosening(t *testing.T) {
	v := &fakeVenue{pos: Position{Coin: "X", Side: SideLong, Size: 1, SzDecimals: 4}, mark: 110, orders: map[int64]OrderSpec{}}
	oid, _ := v.PlaceStop(OrderSpec{OrderType: OrderTypeSpec{Trigger: &TriggerSpec{TriggerPx: 95, Tpsl: "sl"}}})
	_, err := MoveStop(v, "X", oid, 95, 90, nil, cfg) // long: 90 < 95, kurang protektif
	if !errors.Is(err, ErrWouldLoosenStop) {
		t.Fatalf("want ErrWouldLoosenStop, got %v", err)
	}
}

func TestMoveStop_ModifyPathNeverLeavesGap(t *testing.T) {
	v := &fakeVenue{pos: Position{Coin: "X", Side: SideLong, Size: 1, SzDecimals: 4}, mark: 110, orders: map[int64]OrderSpec{}}
	oid, _ := v.PlaceStop(OrderSpec{OrderType: OrderTypeSpec{Trigger: &TriggerSpec{TriggerPx: 95, Tpsl: "sl"}}})
	res, err := MoveStop(v, "X", oid, 95, 100, nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != "modify" {
		t.Fatalf("path = %q, want modify", res.Path)
	}
	if len(v.orders) != 1 {
		t.Fatalf("expected exactly 1 live stop after modify, got %d", len(v.orders))
	}
}

func TestMoveStop_FallbackPlacesBeforeCancel(t *testing.T) {
	v := &fakeVenue{pos: Position{Coin: "X", Side: SideLong, Size: 1, SzDecimals: 4}, mark: 110,
		orders: map[int64]OrderSpec{}, modifyErr: ErrModifyUnsupported}
	oid, _ := v.PlaceStop(OrderSpec{OrderType: OrderTypeSpec{Trigger: &TriggerSpec{TriggerPx: 95, Tpsl: "sl"}}})
	res, err := MoveStop(v, "X", oid, 95, 100, nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != "place_then_cancel" {
		t.Fatalf("path = %q, want place_then_cancel", res.Path)
	}
	// Sepanjang proses tidak boleh pernah ada 0 stop hidup; di akhir harus tepat 1.
	if len(v.orders) != 1 {
		t.Fatalf("expected exactly 1 live stop at end, got %d: %v", len(v.orders), v.orders)
	}
}

func TestMoveStop_AmbiguousErrorDoesNotFallback(t *testing.T) {
	v := &fakeVenue{pos: Position{Coin: "X", Side: SideLong, Size: 1, SzDecimals: 4}, mark: 110,
		orders: map[int64]OrderSpec{}, modifyErr: errors.New("timeout")}
	oid, _ := v.PlaceStop(OrderSpec{OrderType: OrderTypeSpec{Trigger: &TriggerSpec{TriggerPx: 95, Tpsl: "sl"}}})
	before := len(v.orders)
	_, err := MoveStop(v, "X", oid, 95, 100, nil, cfg)
	if err == nil {
		t.Fatal("expected error on ambiguous timeout, got nil")
	}
	if errors.Is(err, ErrWouldLoosenStop) {
		t.Fatal("should not reach ratchet check after ambiguous modify error")
	}
	if len(v.orders) != before {
		t.Fatalf("ambiguous error must not touch live orders, before=%d after=%d", before, len(v.orders))
	}
}

// --- Recovery: TP != SL (audit poin 1 dan 3) ---

func TestRecovery_TPAndSLClassifiedSeparately(t *testing.T) {
	orders := []OpenOrderInfo{
		{Oid: 1, Coin: "BTC", IsTrigger: true, TriggerTpsl: "sl", ReduceOnly: true, Timestamp: 10},
		{Oid: 2, Coin: "BTC", IsTrigger: true, TriggerTpsl: "tp", ReduceOnly: true, Timestamp: 11},
		{Oid: 3, Coin: "ETH", ReduceOnly: true, Timestamp: 5}, // reduce-only biasa, bukan trigger sama sekali
	}
	rp := RecoverPositionOrders(orders, []string{"BTC", "ETH"})
	btc := rp["BTC"]
	if !btc.HasStop || btc.StopOid != 1 {
		t.Fatalf("BTC stop salah: %+v", btc)
	}
	if !btc.HasTakeProfit || btc.TPOid != 2 {
		t.Fatalf("BTC TP salah: %+v", btc)
	}
	if btc.StopOid == btc.TPOid {
		t.Fatal("TP dan SL tidak boleh tertukar")
	}
	eth := rp["ETH"]
	if eth.HasStop || eth.HasTakeProfit {
		t.Fatalf("ETH reduce-only biasa tidak boleh diklasifikasi sebagai SL atau TP: %+v", eth)
	}
}

func TestRecovery_MultipleStopsPicksMostRecent(t *testing.T) {
	orders := []OpenOrderInfo{
		{Oid: 1, Coin: "BTC", IsTrigger: true, TriggerTpsl: "sl", Timestamp: 10},
		{Oid: 2, Coin: "BTC", IsTrigger: true, TriggerTpsl: "sl", Timestamp: 20},
	}
	sl, ok := PickMostRecentStop(orders, "BTC")
	if !ok || sl.Oid != 2 {
		t.Fatalf("want oid 2 (most recent), got %+v ok=%v", sl, ok)
	}
}
