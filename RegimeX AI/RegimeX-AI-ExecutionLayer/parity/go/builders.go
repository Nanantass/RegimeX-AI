package hlsign

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// FloatToWire mereplikasi float_to_wire SDK: 8 desimal, tolak jika pembulatan mengubah
// nilai (>=1e-12), lalu buang nol di belakang (Decimal.normalize). Contoh: 100.0 -> "100",
// 2500.50 -> "2500.5". Format fixed "%.5f" seperti kode awal RegimeX menghasilkan
// "100.00000" dan TIDAK akan cocok dengan SDK.
func FloatToWire(x float64) (string, error) {
	rounded := strconv.FormatFloat(x, 'f', 8, 64)
	back, err := strconv.ParseFloat(rounded, 64)
	if err != nil {
		return "", err
	}
	if math.Abs(back-x) >= 1e-12 {
		return "", fmt.Errorf("float_to_wire causes rounding: %v", x)
	}
	if strings.Contains(rounded, ".") {
		rounded = strings.TrimRight(rounded, "0")
		rounded = strings.TrimSuffix(rounded, ".")
	}
	return rounded, nil
}

type LimitSpec struct {
	Tif string `json:"tif"`
}
type TriggerSpec struct {
	IsMarket  bool    `json:"is_market"`
	TriggerPx float64 `json:"trigger_px"`
	Tpsl      string  `json:"tpsl"`
}
type OrderTypeSpec struct {
	Limit   *LimitSpec   `json:"limit"`
	Trigger *TriggerSpec `json:"trigger"`
}
type OrderSpec struct {
	Asset      uint64        `json:"asset"`
	IsBuy      bool          `json:"is_buy"`
	Sz         float64       `json:"sz"`
	LimitPx    float64       `json:"limit_px"`
	ReduceOnly bool          `json:"reduce_only"`
	OrderType  OrderTypeSpec `json:"order_type"`
	Cloid      *string       `json:"cloid"`
}
type BuilderSpec struct {
	B string `json:"b"`
	F uint64 `json:"f"`
}
type ModifySpec struct {
	Oid   json.RawMessage `json:"oid"` // angka (oid) atau string (cloid)
	Order OrderSpec       `json:"order"`
}
type CancelSpec struct {
	Asset uint64  `json:"asset"`
	Oid   uint64  `json:"oid"`
	Cloid *string `json:"cloid"`
}
type ActionSpec struct {
	Kind     string       `json:"kind"`
	Orders   []OrderSpec  `json:"orders"`
	Grouping string       `json:"grouping"`
	Builder  *BuilderSpec `json:"builder"`
	Modifies []ModifySpec `json:"modifies"`
	Cancels  []CancelSpec `json:"cancels"`
}

// OrderWire: urutan key WAJIB a,b,p,s,r,t lalu c (hanya bila cloid ada, tidak pernah null).
func OrderWire(o OrderSpec) (OMap, error) {
	p, err := FloatToWire(o.LimitPx)
	if err != nil {
		return nil, err
	}
	s, err := FloatToWire(o.Sz)
	if err != nil {
		return nil, err
	}
	var t OMap
	switch {
	case o.OrderType.Limit != nil:
		t = OMap{{"limit", OMap{{"tif", o.OrderType.Limit.Tif}}}}
	case o.OrderType.Trigger != nil:
		tp, err := FloatToWire(o.OrderType.Trigger.TriggerPx)
		if err != nil {
			return nil, err
		}
		t = OMap{{"trigger", OMap{
			{"isMarket", o.OrderType.Trigger.IsMarket},
			{"triggerPx", tp},
			{"tpsl", o.OrderType.Trigger.Tpsl},
		}}}
	default:
		return nil, fmt.Errorf("order_type must be limit or trigger")
	}
	w := OMap{{"a", o.Asset}, {"b", o.IsBuy}, {"p", p}, {"s", s}, {"r", o.ReduceOnly}, {"t", t}}
	if o.Cloid != nil {
		w = append(w, KV{"c", *o.Cloid})
	}
	return w, nil
}

func BuildAction(spec ActionSpec) (OMap, error) {
	switch spec.Kind {
	case "order":
		wires := []any{}
		for _, o := range spec.Orders {
			w, err := OrderWire(o)
			if err != nil {
				return nil, err
			}
			wires = append(wires, w)
		}
		a := OMap{{"type", "order"}, {"orders", wires}, {"grouping", spec.Grouping}}
		if spec.Builder != nil {
			a = append(a, KV{"builder", OMap{{"b", strings.ToLower(spec.Builder.B)}, {"f", spec.Builder.F}}})
		}
		return a, nil
	case "batchModify":
		mods := []any{}
		for _, m := range spec.Modifies {
			w, err := OrderWire(m.Order)
			if err != nil {
				return nil, err
			}
			var oid any
			var s string
			if err := json.Unmarshal(m.Oid, &s); err == nil {
				oid = s
			} else {
				var n uint64
				if err := json.Unmarshal(m.Oid, &n); err != nil {
					return nil, fmt.Errorf("oid must be number or string: %s", m.Oid)
				}
				oid = n
			}
			mods = append(mods, OMap{{"oid", oid}, {"order", w}})
		}
		return OMap{{"type", "batchModify"}, {"modifies", mods}}, nil
	case "cancel":
		cs := []any{}
		for _, c := range spec.Cancels {
			cs = append(cs, OMap{{"a", c.Asset}, {"o", c.Oid}})
		}
		return OMap{{"type", "cancel"}, {"cancels", cs}}, nil
	case "cancelByCloid":
		cs := []any{}
		for _, c := range spec.Cancels {
			if c.Cloid == nil {
				return nil, fmt.Errorf("cancelByCloid requires cloid")
			}
			cs = append(cs, OMap{{"asset", c.Asset}, {"cloid", *c.Cloid}})
		}
		return OMap{{"type", "cancelByCloid"}, {"cancels", cs}}, nil
	}
	return nil, fmt.Errorf("unknown action kind %q", spec.Kind)
}
