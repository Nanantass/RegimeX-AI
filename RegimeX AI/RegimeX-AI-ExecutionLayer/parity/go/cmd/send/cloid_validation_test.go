package main

import "testing"

// Regresi Temuan 1 audit: validasi cloid sebelumnya HANYA mengecek panjang
// (len(c)!=32), meloloskan karakter non-hex seperti "zzzz..." ke dalam
// action yang dikirim ke server. normalizeCloid sekarang memvalidasi
// seluruh karakter benar-benar hex.

func TestNormalizeCloid_RejectsNonHexCharacters(t *testing.T) {
	// 32 karakter, panjang benar, tapi mengandung 'z' (bukan hex)
	_, err := normalizeCloid("zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz")
	if err == nil {
		t.Fatal("32 karakter non-hex ('z') harus ditolak, bukan lolos karena panjangnya kebetulan benar")
	}
}

func TestNormalizeCloid_RejectsHyphen(t *testing.T) {
	_, err := normalizeCloid("abcdefab-cdefabcdefabcdefabcdefab") // 32 char termasuk '-'
	if err == nil {
		t.Fatal("karakter '-' di tengah hex harus ditolak")
	}
}

func TestNormalizeCloid_RejectsWrongLength(t *testing.T) {
	if _, err := normalizeCloid("abcdefabcdefabcdefabcdefabcdefa"); err == nil { // 31 char
		t.Fatal("31 karakter harus ditolak")
	}
	if _, err := normalizeCloid("abcdefabcdefabcdefabcdefabcdefabc"); err == nil { // 33 char
		t.Fatal("33 karakter harus ditolak")
	}
}

func TestNormalizeCloid_AcceptsValidLowercase(t *testing.T) {
	got, err := normalizeCloid("abcdefabcdefabcdefabcdefabcdefab")
	if err != nil {
		t.Fatalf("hex valid lowercase harus diterima: %v", err)
	}
	if got != "0xabcdefabcdefabcdefabcdefabcdefab" {
		t.Fatalf("hasil normalisasi salah: %s", got)
	}
}

func TestNormalizeCloid_AcceptsValidUppercaseAndNormalizesCase(t *testing.T) {
	got, err := normalizeCloid("ABCDEFABCDEFABCDEFABCDEFABCDEFAB")
	if err != nil {
		t.Fatalf("hex valid uppercase harus diterima: %v", err)
	}
	if got != "0xabcdefabcdefabcdefabcdefabcdefab" {
		t.Fatalf("uppercase harus dinormalisasi ke lowercase, dapat: %s", got)
	}
}

func TestNormalizeCloid_AcceptsWith0xPrefix(t *testing.T) {
	got, err := normalizeCloid("0xabcdefabcdefabcdefabcdefabcdefab")
	if err != nil {
		t.Fatalf("hex valid dengan prefix 0x harus diterima: %v", err)
	}
	if got != "0xabcdefabcdefabcdefabcdefabcdefab" {
		t.Fatalf("hasil salah: %s", got)
	}
}

func TestNormalizeCloid_RejectsEmpty(t *testing.T) {
	if _, err := normalizeCloid(""); err == nil {
		t.Fatal("cloid kosong harus ditolak")
	}
}
