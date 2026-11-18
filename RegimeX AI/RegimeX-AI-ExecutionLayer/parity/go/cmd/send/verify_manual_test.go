package main

import (
	"encoding/json"
	"testing"
)

func TestVerifyOrderIntent_EmptyFieldMapReportsMissing(t *testing.T) {
	m := VerifyOrderIntent(map[string]any{}, ExpectedIntent{ReduceOnly: true, Sz: 0.001}, map[string]string{})
	if len(m) == 0 {
		t.Fatal("fieldMap kosong harus menghasilkan mismatch (belum dipetakan), bukan dianggap cocok")
	}
	for _, x := range m {
		if x.Got != "FIELD MAPPING BELUM DIISI, lihat schema_contract.md" {
			t.Fatalf("pesan tidak sesuai: %+v", x)
		}
	}
}

func TestVerifyOrderIntent_DetectsReduceOnlyMismatch(t *testing.T) {
	fm := map[string]string{"reduce_only": "r", "is_buy": "b", "sz": "s"}
	order := map[string]any{"r": false, "b": true, "s": "0.001"} // r salah, harusnya true
	m := VerifyOrderIntent(order, ExpectedIntent{ReduceOnly: true, IsBuy: true, Sz: 0.001}, fm)
	found := false
	for _, x := range m {
		if x.Field == "reduce_only" {
			found = true
		}
	}
	if !found {
		t.Fatalf("mismatch reduce_only tidak terdeteksi: %+v", m)
	}
}

func TestVerifyOrderIntent_MatchYieldsNoMismatch(t *testing.T) {
	fm := map[string]string{"reduce_only": "r", "is_buy": "b", "sz": "s"}
	order := map[string]any{"r": true, "b": true, "s": "0.001"}
	m := VerifyOrderIntent(order, ExpectedIntent{ReduceOnly: true, IsBuy: true, Sz: 0.001}, fm)
	if len(m) != 0 {
		t.Fatalf("seharusnya tidak ada mismatch, dapat %+v", m)
	}
}

// Regresi Temuan 2: int vs float64 (artefak json.Unmarshal) tidak boleh mismatch.
func TestValuesMatch_IntVsFloatIsNotMismatch(t *testing.T) {
	fm := map[string]string{"reduce_only": "r", "is_buy": "b", "sz": "s"}
	order := map[string]any{"r": false, "b": false, "s": 1.0} // json.Unmarshal selalu menghasilkan float64
	m := VerifyOrderIntent(order, ExpectedIntent{Sz: 1}, fm)
	if len(m) != 0 {
		t.Fatalf("want=1 (int-like) vs got=1.0 (float64) tidak boleh mismatch, dapat %+v", m)
	}
}

// Regresi Temuan 2: presisi float JSON (0.1 vs 0.1000000000001) tidak boleh mismatch.
func TestValuesMatch_FloatPrecisionWithinEpsilon(t *testing.T) {
	fm := map[string]string{"reduce_only": "r", "is_buy": "b", "sz": "s"}
	order := map[string]any{"r": false, "b": false, "s": 0.1000000000001}
	m := VerifyOrderIntent(order, ExpectedIntent{Sz: 0.1}, fm)
	if len(m) != 0 {
		t.Fatalf("selisih presisi float JSON tidak boleh mismatch, dapat %+v", m)
	}
}

// Regresi Temuan 2: string numerik dari exchange (umum untuk harga/size) tetap terbandingkan.
func TestValuesMatch_StringNumericFromExchange(t *testing.T) {
	fm := map[string]string{"reduce_only": "r", "is_buy": "b", "sz": "s"}
	order := map[string]any{"r": false, "b": false, "s": "0.001"} // exchange sering kirim angka sebagai string
	m := VerifyOrderIntent(order, ExpectedIntent{Sz: 0.001}, fm)
	if len(m) != 0 {
		t.Fatalf("string numerik yang setara tidak boleh mismatch, dapat %+v", m)
	}
}

// Pastikan nilai yang BENAR-BENAR beda tetap terdeteksi (guard regresi ke arah sebaliknya).
func TestValuesMatch_GenuinelyDifferentStillDetected(t *testing.T) {
	fm := map[string]string{"reduce_only": "r", "is_buy": "b", "sz": "s"}
	order := map[string]any{"r": false, "b": false, "s": 0.002}
	m := VerifyOrderIntent(order, ExpectedIntent{Sz: 0.001}, fm)
	if len(m) == 0 {
		t.Fatal("perbedaan nyata (0.001 vs 0.002) harus tetap terdeteksi sebagai mismatch")
	}
}

// Future-proofing (bukan regresi aktual pada jalur kode sekarang): kalau parser
// suatu saat memakai decoder.UseNumber() alih-alih json.Unmarshal biasa,
// json.Number tidak boleh jatuh ke default dan menghasilkan mismatch palsu.
func TestValuesMatch_JSONNumberSupported(t *testing.T) {
	fm := map[string]string{"reduce_only": "r", "is_buy": "b", "sz": "s"}
	order := map[string]any{"r": false, "b": false, "s": json.Number("0.001")}
	m := VerifyOrderIntent(order, ExpectedIntent{Sz: 0.001}, fm)
	if len(m) != 0 {
		t.Fatalf("json.Number yang setara tidak boleh mismatch, dapat %+v", m)
	}
}
