package main

import (
	"strings"
	"testing"
)

// Regresi: jalur kunci privat sebelumnya memakai fmt.Sscanf tanpa pengecekan
// panjang. Test ini mengunci perilaku ketat dan memastikan pesan error tidak
// pernah memuat isi kunci.

const validKey = "1111111111111111111111111111111111111111111111111111111111111111"

func TestParsePrivateKey_AcceptsValid(t *testing.T) {
	for name, in := range map[string]string{
		"tanpa awalan":         validKey,
		"awalan 0x":            "0x" + validKey,
		"awalan 0X":            "0X" + validKey,
		"spasi di ujung (cmd)": "0x" + validKey + "  ",
		"huruf besar":          "0x" + strings.ToUpper("abcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcd"),
	} {
		if _, err := parsePrivateKey(in); err != nil {
			t.Fatalf("%s: kunci valid ditolak: %v", name, err)
		}
	}
}

func TestParsePrivateKey_RejectsAndNeverEchoesKey(t *testing.T) {
	cases := map[string]string{
		"teks contoh":          "0x...kunci_testnet",
		"terlalu pendek (31B)": "0x" + validKey[:62],
		"terlalu panjang(33B)": "0x" + validKey + "11",
		"tanda plus":           "0x" + strings.Repeat("+1", 32),
		"tanda minus":          "0x" + strings.Repeat("-1", 32),
		"huruf non-hex":        "0x" + strings.Repeat("zz", 32),
		"kosong setelah 0x":    "0x",
		"nol semua":            "0x" + strings.Repeat("00", 32),
		"di atas orde kurva":   "0x" + strings.Repeat("ff", 32),
		"pakai kutip":          "\"0x" + validKey + "\"",
		"kutip tunggal":        "'0x" + validKey + "'",
		"ganjil":               "0x" + validKey[:63],
	}
	for name, in := range cases {
		_, err := parsePrivateKey(in)
		if err == nil {
			t.Fatalf("%s: seharusnya ditolak", name)
		}
		body := strings.TrimPrefix(strings.Trim(in, "'\""), "0x")
		if len(body) >= 8 && strings.Contains(err.Error(), body) {
			t.Fatalf("%s: pesan error memuat isi kunci: %v", name, err)
		}
	}
}

func TestParsePrivateKey_SameKeyForSameInput(t *testing.T) {
	a, err := parsePrivateKey("0x" + validKey)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := parsePrivateKey(validKey)
	if string(a.Serialize()) != string(b.Serialize()) {
		t.Fatal("0x dan tanpa 0x seharusnya menghasilkan kunci yang sama")
	}
}
