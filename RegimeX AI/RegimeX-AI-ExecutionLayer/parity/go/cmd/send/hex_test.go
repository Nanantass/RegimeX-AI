package main

import "testing"

func TestHex_OddLengthRejected(t *testing.T) {
	if _, err := decodePrivateKeyHex("0x123"); err == nil {
		t.Fatal("panjang hex ganjil (setelah buang 0x) harus ditolak, bukan diam-diam dipotong")
	}
}

func TestHex_EmptyRejected(t *testing.T) {
	if _, err := decodePrivateKeyHex("0x"); err == nil {
		t.Fatal("hex kosong harus ditolak")
	}
	if _, err := decodePrivateKeyHex(""); err == nil {
		t.Fatal("string kosong harus ditolak")
	}
}

func TestHex_ValidKeyAccepted(t *testing.T) {
	key := "0x1111111111111111111111111111111111111111111111111111111111111111"[:66]
	b, err := decodePrivateKeyHex(key)
	if err != nil {
		t.Fatalf("kunci 32-byte valid seharusnya diterima: %v", err)
	}
	if len(b) != 32 {
		t.Fatalf("panjang hasil decode salah: %d, want 32", len(b))
	}
}

func TestHex_EvenButInvalidCharsRejected(t *testing.T) {
	if _, err := decodePrivateKeyHex("0xzz"); err == nil {
		t.Fatal("karakter non-hex harus ditolak")
	}
}
