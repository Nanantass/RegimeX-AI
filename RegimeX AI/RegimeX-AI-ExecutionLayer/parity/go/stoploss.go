package hlsign

import (
	"errors"
	"fmt"
	"math"
	"strconv"
)

// Stop loss HARUS berupa order trigger (tpsl "sl"), bukan order limit reduce-only.
// Limit reduce-only di harga SL (di bawah pasar untuk posisi long) bersifat marketable
// dan dapat langsung terisi, menutup posisi seketika. Berkas ini memuat builder stop yang
// benar, guard marketability, dan model eksekusi sederhana untuk pengujian.

type PositionSide int

const (
	SideLong PositionSide = iota
	SideShort
)

var (
	ErrStopWouldTriggerImmediately = errors.New("stop trigger already breached by mark price (would trigger immediately)")
	ErrStopTooClose                = errors.New("stop trigger closer to mark price than allowed minimum distance")
	ErrWouldLoosenStop             = errors.New("new stop is less protective than current stop")
	ErrNoPosition                  = errors.New("no open position")
	ErrModifyUnsupported           = errors.New("modify not supported for this order")
	ErrOrderNotFound               = errors.New("order not found")
	ErrOldStopNotCancelled         = errors.New("new stop is active but old stop could not be cancelled")
	ErrAmbiguous                   = errors.New("outcome unknown, reconcile with exchange before retrying")
)

type StopConfig struct {
	// SlippageBufferPct: batas harga terburuk (limit_px) untuk trigger market, sebagai persen
	// dari trigger. Perilaku field limit_px pada trigger market di server HARUS diverifikasi
	// di testnet (Fase 2); nilai ini hanya batas aman, bukan pemicu.
	SlippageBufferPct float64
	// MinDistancePct: jarak minimum trigger dari mark, mencegah stop yang nyaris langsung kena.
	MinDistancePct float64
	// RatchetOnly: stop hanya boleh bergerak ke arah yang lebih protektif.
	RatchetOnly bool
}

// RoundPriceHL meniru round(float(f"{px:.5g}"), 6 - szDecimals) dari contoh SDK resmi:
// 5 angka signifikan, maksimum (6 - szDecimals) desimal untuk perp.
// Perilaku pada nilai tepat di tengah (tie) bisa berbeda dari round() Python; verifikasi di testnet.
func RoundPriceHL(px float64, szDecimals int) (float64, error) {
	if px <= 0 || math.IsNaN(px) || math.IsInf(px, 0) {
		return 0, fmt.Errorf("invalid price %v", px)
	}
	v, err := strconv.ParseFloat(strconv.FormatFloat(px, 'g', 5, 64), 64)
	if err != nil {
		return 0, err
	}
	d := 6 - szDecimals
	if d < 0 {
		d = 0
	}
	p := math.Pow(10, float64(d))
	return math.Round(v*p) / p, nil
}

func FloorSize(sz float64, szDecimals int) float64 {
	p := math.Pow(10, float64(szDecimals))
	return math.Floor(sz*p+1e-9) / p
}

// ValidateStopPlacement menolak stop yang akan langsung terpicu atau terlalu dekat.
func ValidateStopPlacement(side PositionSide, trigger, mark float64, cfg StopConfig) error {
	if trigger <= 0 || mark <= 0 {
		return fmt.Errorf("invalid trigger %v or mark %v", trigger, mark)
	}
	var breached bool
	var dist float64
	if side == SideLong {
		breached = trigger >= mark
		dist = (mark - trigger) / mark * 100
	} else {
		breached = trigger <= mark
		dist = (trigger - mark) / mark * 100
	}
	if breached {
		return fmt.Errorf("%w: trigger=%v mark=%v side=%v", ErrStopWouldTriggerImmediately, trigger, mark, side)
	}
	if dist < cfg.MinDistancePct {
		return fmt.Errorf("%w: distance=%.4f%% min=%.4f%%", ErrStopTooClose, dist, cfg.MinDistancePct)
	}
	return nil
}

// BuildStopLossSpec membangun spec order stop loss: trigger market, reduce-only, sisi berlawanan
// dengan posisi. Semua angka dibulatkan sesuai aturan tick/size sebelum masuk wire.
func BuildStopLossSpec(asset uint64, side PositionSide, size, trigger float64, cloid *string, szDecimals int, cfg StopConfig) (OrderSpec, error) {
	trig, err := RoundPriceHL(trigger, szDecimals)
	if err != nil {
		return OrderSpec{}, err
	}
	sz := FloorSize(size, szDecimals)
	if sz <= 0 {
		return OrderSpec{}, fmt.Errorf("size %v rounds to zero at %d decimals", size, szDecimals)
	}
	limit := trig
	if side == SideLong {
		limit = trig * (1 - cfg.SlippageBufferPct/100)
	} else {
		limit = trig * (1 + cfg.SlippageBufferPct/100)
	}
	limit, err = RoundPriceHL(limit, szDecimals)
	if err != nil {
		return OrderSpec{}, err
	}
	return OrderSpec{
		Asset:      asset,
		IsBuy:      side == SideShort, // stop untuk long = jual, untuk short = beli
		Sz:         sz,
		LimitPx:    limit,
		ReduceOnly: true,
		OrderType:  OrderTypeSpec{Trigger: &TriggerSpec{IsMarket: true, TriggerPx: trig, Tpsl: "sl"}},
		Cloid:      cloid,
	}, nil
}

// WouldExecuteImmediately: model eksekusi yang disederhanakan (mark dipakai sebagai proxy bid/ask).
// Order limit langsung terisi jika harganya melewati pasar; order trigger langsung terpicu jika
// kondisi trigger sudah terpenuhi. Dipakai sebagai oracle uji, bukan pengganti perilaku server.
func WouldExecuteImmediately(o OrderSpec, mark float64) bool {
	if t := o.OrderType.Trigger; t != nil {
		switch t.Tpsl {
		case "sl":
			if o.IsBuy { // stop untuk short: terpicu saat harga naik ke trigger
				return mark >= t.TriggerPx
			}
			return mark <= t.TriggerPx // stop untuk long
		default: // "tp"
			if o.IsBuy {
				return mark <= t.TriggerPx
			}
			return mark >= t.TriggerPx
		}
	}
	if o.IsBuy {
		return o.LimitPx >= mark
	}
	return o.LimitPx <= mark
}

// ---- MoveStop: memindahkan stop tanpa pernah membuka celah tanpa proteksi ----

type Position struct {
	Coin       string
	Asset      uint64
	Side       PositionSide
	Size       float64
	EntryPx    float64
	SzDecimals int
}

type Venue interface {
	Position(coin string) (Position, error) // ErrNoPosition jika flat
	Mark(coin string) (float64, error)
	PlaceStop(spec OrderSpec) (oid int64, err error)
	ModifyStop(oid int64, spec OrderSpec) (newOid int64, err error)
	CancelOrder(coin string, oid int64) error
}

type MoveResult struct {
	Oid  int64
	Path string // "noop", "place_new", "modify", "place_then_cancel"
}

func moreProtective(side PositionSide, newTrig, oldTrig float64) bool {
	if side == SideLong {
		return newTrig > oldTrig
	}
	return newTrig < oldTrig
}

// MoveStop memindahkan stop. Urutan aman:
//  1. modify atomik (stop lama tetap hidup jika modify gagal);
//  2. jika modify tidak didukung untuk order trigger atau order lama sudah hilang:
//     PASANG stop baru DULU, baru cancel yang lama. Keduanya reduce-only, jadi dua stop
//     sementara tidak dapat membalik posisi. Tidak pernah cancel-dulu-baru-pasang.
//  3. error yang tidak jelas hasilnya (timeout) TIDAK memicu fallback buta: kembalikan
//     ErrAmbiguous, caller wajib rekonsiliasi via cloid dan openOrders.
func MoveStop(v Venue, coin string, currentOid int64, currentTrigger, newTrigger float64, cloid *string, cfg StopConfig) (MoveResult, error) {
	pos, err := v.Position(coin)
	if err != nil {
		return MoveResult{Oid: currentOid}, err
	}
	if pos.Size <= 0 {
		return MoveResult{Oid: currentOid}, ErrNoPosition
	}
	mark, err := v.Mark(coin)
	if err != nil {
		return MoveResult{Oid: currentOid}, err
	}
	spec, err := BuildStopLossSpec(pos.Asset, pos.Side, pos.Size, newTrigger, cloid, pos.SzDecimals, cfg)
	if err != nil {
		return MoveResult{Oid: currentOid}, err
	}
	rounded := spec.OrderType.Trigger.TriggerPx
	if currentOid != 0 {
		if rounded == currentTrigger {
			return MoveResult{Oid: currentOid, Path: "noop"}, nil
		}
		if cfg.RatchetOnly && !moreProtective(pos.Side, rounded, currentTrigger) {
			return MoveResult{Oid: currentOid}, fmt.Errorf("%w: new=%v current=%v", ErrWouldLoosenStop, rounded, currentTrigger)
		}
	}
	if err := ValidateStopPlacement(pos.Side, rounded, mark, cfg); err != nil {
		return MoveResult{Oid: currentOid}, err
	}
	// Pengaman terakhir independen dari guard di atas: tidak boleh ada order yang langsung terisi.
	if WouldExecuteImmediately(spec, mark) {
		return MoveResult{Oid: currentOid}, fmt.Errorf("refusing to submit order that would execute immediately: %+v", spec)
	}

	if currentOid == 0 {
		oid, err := v.PlaceStop(spec)
		if err != nil {
			return MoveResult{}, err
		}
		return MoveResult{Oid: oid, Path: "place_new"}, nil
	}

	newOid, err := v.ModifyStop(currentOid, spec)
	if err == nil {
		return MoveResult{Oid: newOid, Path: "modify"}, nil
	}
	if !errors.Is(err, ErrModifyUnsupported) && !errors.Is(err, ErrOrderNotFound) {
		// Termasuk ErrAmbiguous dan error jaringan: stop lama kemungkinan masih hidup.
		return MoveResult{Oid: currentOid}, fmt.Errorf("modify failed with unclear outcome, not falling back blindly: %w", err)
	}

	// Fallback: pastikan posisi masih ada (order lama bisa hilang karena SL terpicu).
	if p2, perr := v.Position(coin); perr != nil || p2.Size <= 0 {
		if perr == nil {
			perr = ErrNoPosition
		}
		return MoveResult{Oid: currentOid}, perr
	}
	oid, perr := v.PlaceStop(spec)
	if perr != nil {
		return MoveResult{Oid: currentOid}, fmt.Errorf("fallback place failed, old stop left in place: %w", perr)
	}
	if cerr := v.CancelOrder(coin, currentOid); cerr != nil && !errors.Is(cerr, ErrOrderNotFound) {
		return MoveResult{Oid: oid, Path: "place_then_cancel"}, fmt.Errorf("%w: %v", ErrOldStopNotCancelled, cerr)
	}
	return MoveResult{Oid: oid, Path: "place_then_cancel"}, nil
}
