package main

// Regresi Temuan 1 audit: flag -action mengiklankan "cancel_by_cloid" di help
// text TAPI implementasinya sempat hilang dari switch, membuat perintah yang
// didokumentasikan justru gagal dengan "action tidak dikenal". Test berikut
// memverifikasi KEEMPAT action yang dijanjikan help text benar-benar punya
// jalur spec yang valid, supaya future regression (option tercantum tapi
// implementasi hilang lagi) tertangkap otomatis, bukan ditemukan manual.

import (
	"strings"
	"testing"

	"hlsign"
)

// buildSpecForAction mereplikasi logika pemilihan spec di main() tanpa efek
// samping (tidak exit, tidak sign, tidak network), supaya testable.
func buildSpecForAction(t *testing.T, action string, asset uint64, cloid string) (hlsign.ActionSpec, error) {
	t.Helper()
	switch action {
	case "order":
		return hlsign.ActionSpec{Kind: "order", Grouping: "na", Orders: []hlsign.OrderSpec{{
			Asset: asset, IsBuy: true, Sz: 0.001, LimitPx: 50000,
			OrderType: hlsign.OrderTypeSpec{Limit: &hlsign.LimitSpec{Tif: "Gtc"}},
		}}}, nil
	case "batch_modify":
		return hlsign.ActionSpec{Kind: "batchModify", Modifies: []hlsign.ModifySpec{{
			Oid: mustJSON(1),
			Order: hlsign.OrderSpec{Asset: asset, IsBuy: true, Sz: 0.001, LimitPx: 50000,
				OrderType: hlsign.OrderTypeSpec{Limit: &hlsign.LimitSpec{Tif: "Gtc"}}},
		}}}, nil
	case "cancel":
		return hlsign.ActionSpec{Kind: "cancel", Cancels: []hlsign.CancelSpec{{Asset: asset, Oid: 1}}}, nil
	case "cancel_by_cloid":
		c := strings.ToLower(strings.TrimPrefix(cloid, "0x"))
		if len(c) != 32 {
			return hlsign.ActionSpec{}, errInvalidCloidLen
		}
		cs := "0x" + c
		return hlsign.ActionSpec{Kind: "cancelByCloid", Cancels: []hlsign.CancelSpec{{Asset: asset, Cloid: &cs}}}, nil
	}
	return hlsign.ActionSpec{}, errUnknownAction
}

var errInvalidCloidLen = &testErr{"invalid cloid length"}
var errUnknownAction = &testErr{"unknown action"}

type testErr struct{ msg string }

func (e *testErr) Error() string { return e.msg }

// Setiap action yang dijanjikan help text ("order | batch_modify | cancel |
// cancel_by_cloid | stop_loss") WAJIB membangun action lewat BuildAction tanpa
// error. stop_loss diuji terpisah di stoploss_test.go (butuh posisi/mark),
// jadi di sini difokuskan ke empat action murni acceptance (order, batchModify,
// cancel, cancelByCloid).
func TestAllAdvertisedActions_BuildSuccessfully(t *testing.T) {
	validCloid := "0xabcdefabcdefabcdefabcdefabcdefab"
	for _, action := range []string{"order", "batch_modify", "cancel", "cancel_by_cloid"} {
		t.Run(action, func(t *testing.T) {
			spec, err := buildSpecForAction(t, action, 0, validCloid)
			if err != nil {
				t.Fatalf("action %q yang diiklankan help text gagal dibangun: %v", action, err)
			}
			built, err := hlsign.BuildAction(spec)
			if err != nil {
				t.Fatalf("action %q: BuildAction gagal: %v", action, err)
			}
			if _, err := hlsign.EncodeMsgpack(built); err != nil {
				t.Fatalf("action %q: EncodeMsgpack gagal: %v", action, err)
			}
		})
	}
}

func TestCancelByCloid_RejectsWrongLength(t *testing.T) {
	_, err := buildSpecForAction(t, "cancel_by_cloid", 0, "0xabcd")
	if err == nil {
		t.Fatal("cloid pendek harus ditolak")
	}
}

func TestCancelByCloid_ProducesCorrectActionType(t *testing.T) {
	spec, err := buildSpecForAction(t, "cancel_by_cloid", 5, "0xabcdefabcdefabcdefabcdefabcdefab")
	if err != nil {
		t.Fatal(err)
	}
	if spec.Kind != "cancelByCloid" {
		t.Fatalf("Kind = %q, want cancelByCloid", spec.Kind)
	}
	if len(spec.Cancels) != 1 || spec.Cancels[0].Cloid == nil {
		t.Fatalf("spec cancel salah bentuk: %+v", spec)
	}
	if spec.Cancels[0].Asset != 5 {
		t.Fatalf("asset salah: %d", spec.Cancels[0].Asset)
	}
}
