package main

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// VerifyOrderIntent membandingkan intent yang dikirim dengan state order yang
// sebenarnya tersimpan di exchange (hasil query openOrders setelah POST).
// Menjawab audit poin 2: "POST berhasil" bukan bukti "order yang dibuat ==
// order yang dimaksud". Dipanggil manual setelah -dry-run=false lalu query
// openOrders terpisah (lihat README): go run ./cmd/send -action verify ...
//
// Nama field exchangeOrder di bawah adalah KERANGKA, menunggu konfirmasi nama
// asli dari dump testnet (Fase 2) sebelum dipetakan penuh.
type ExpectedIntent struct {
	ReduceOnly bool
	IsBuy      bool
	Sz         float64
	TriggerPx  float64 // 0 jika bukan trigger
	IsMarket   bool
	Tpsl       string // "sl", "tp", atau "" jika bukan trigger
}

type Mismatch struct {
	Field    string
	Expected any
	Got      any
}

// VerifyOrderIntent menerima exchangeOrder sebagai map hasil json.Unmarshal
// mentah (bukan struct tetap), karena nama field pasti berbeda dari asumsi
// sampai Fase 2 selesai. Caller mengisi fieldMap untuk memetakan nama
// konseptual ke nama field asli di response.
func VerifyOrderIntent(exchangeOrder map[string]any, expected ExpectedIntent, fieldMap map[string]string) []Mismatch {
	var mismatches []Mismatch
	check := func(concept string, want any) {
		key, ok := fieldMap[concept]
		if !ok {
			mismatches = append(mismatches, Mismatch{concept, want, "FIELD MAPPING BELUM DIISI, lihat schema_contract.md"})
			return
		}
		got, present := exchangeOrder[key]
		if !present {
			mismatches = append(mismatches, Mismatch{concept, want, "FIELD TIDAK ADA di response"})
			return
		}
		if !valuesMatch(want, got) {
			mismatches = append(mismatches, Mismatch{concept, want, got})
		}
	}
	check("reduce_only", expected.ReduceOnly)
	check("is_buy", expected.IsBuy)
	check("sz", expected.Sz)
	if expected.TriggerPx != 0 {
		check("trigger_px", expected.TriggerPx)
		check("is_market", expected.IsMarket)
		check("tpsl", expected.Tpsl)
	}
	return mismatches
}

// valuesMatch membandingkan sesuai TIPE want, bukan representasi string JSON,
// supaya want=1 (int) vs got=1.0 (float dari json.Unmarshal) tidak keliru
// dianggap beda, dan want=0.1 vs got=0.1000000000001 (presisi float JSON
// umum) tidak keliru dianggap beda. String dan bool tetap dibandingkan exact,
// tipe numerik (termasuk saat exchange mengirim angka sebagai string, seperti
// "0.001" untuk sz) dibandingkan dengan epsilon setelah konversi ke float64.
func valuesMatch(want, got any) bool {
	switch w := want.(type) {
	case bool:
		if g, ok := got.(bool); ok {
			return w == g
		}
		return false
	case string:
		if g, ok := got.(string); ok {
			return w == g
		}
		return false
	case float64, int, int64:
		wf, wok := toFloat(want)
		gf, gok := toFloat(got)
		if !wok || !gok {
			return false
		}
		const epsilon = 1e-9
		diff := wf - gf
		if diff < 0 {
			diff = -diff
		}
		return diff < epsilon
	default:
		return fmt.Sprintf("%v", want) == fmt.Sprintf("%v", got)
	}
}

// toFloat menerima float64/int/int64 langsung, ATAU string numerik (exchange
// kerap mengirim harga/size sebagai string, mis. "0.001"), dan mengonversinya
// ke float64 untuk perbandingan epsilon.
func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case string:
		f, err := strconv.ParseFloat(x, 64)
		return f, err == nil
	case json.Number:
		// Future-proofing: belum relevan selama caller memakai json.Unmarshal
		// biasa (yang selalu menghasilkan float64 untuk angka), TAPI kalau
		// parser diganti ke decoder.UseNumber() (dipakai di tempat lain di
		// repo ini, lihat signing_test.go decodeOrdered), json.Number masuk
		// di sini alih-alih default, mencegah mismatch palsu "0.001" != "0.001".
		f, err := x.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func printMismatches(label string, m []Mismatch) {
	if len(m) == 0 {
		fmt.Println(label + ": COCOK, semua field intent sesuai dengan state exchange")
		return
	}
	fmt.Printf("%s: %d KETIDAKCOCOKAN ditemukan (acceptance TIDAK boleh dianggap lulus):\n", label, len(m))
	for _, x := range m {
		fmt.Printf("  - %-14s expected=%v got=%v\n", x.Field, x.Expected, x.Got)
	}
}

// Dipakai manual: tempel response openOrders (satu order sebagai map) di sini
// lewat flag -exchange-order-json, bandingkan dengan intent yang baru dikirim.
func runVerify(rawJSON string, expected ExpectedIntent, fieldMap map[string]string) {
	var order map[string]any
	if err := json.Unmarshal([]byte(rawJSON), &order); err != nil {
		fmt.Println("gagal parse exchange order JSON:", err)
		return
	}
	printMismatches("verify_order_intent", VerifyOrderIntent(order, expected, fieldMap))
}
