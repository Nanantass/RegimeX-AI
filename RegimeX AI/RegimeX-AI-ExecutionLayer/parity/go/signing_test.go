package hlsign

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

type goldenTrace struct {
	MsgpackHex      string `json:"msgpack_hex"`
	MsgpackSHA256   string `json:"msgpack_sha256"`
	NonceHex        string `json:"nonce_hex"`
	SuffixHex       string `json:"vault_expires_suffix_hex"`
	ConnectionIDHex string `json:"connection_id_hex"`
	PhantomSource   string `json:"phantom_source"`
	EIP712DigestHex string `json:"eip712_digest_hex"`
	Signature       struct {
		R string `json:"r"`
		S string `json:"s"`
		V int    `json:"v"`
	} `json:"signature"`
}

type goldenCase struct {
	Name         string          `json:"name"`
	Note         string          `json:"note"`
	IsMainnet    bool            `json:"is_mainnet"`
	Nonce        uint64          `json:"nonce"`
	Vault        *string         `json:"vault"`
	ExpiresAfter *uint64         `json:"expires_after"`
	ActionJSON   json.RawMessage `json:"action_json"`
	Spec         ActionSpec      `json:"spec"`
	Trace        goldenTrace     `json:"trace"`
}

type goldenFile struct {
	SignerAddress string       `json:"signer_address"`
	TestKeyHex    string       `json:"test_private_key_hex"`
	Cases         []goldenCase `json:"cases"`
}

// decodeOrdered membaca JSON dengan urutan key TERJAGA (json.Unmarshal ke map membuang urutan).
func decodeOrdered(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch v := tok.(type) {
	case json.Delim:
		switch v {
		case '{':
			m := OMap{}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				val, err := decodeOrdered(dec)
				if err != nil {
					return nil, err
				}
				m = append(m, KV{kt.(string), val})
			}
			dec.Token()
			return m, nil
		case '[':
			arr := []any{}
			for dec.More() {
				val, err := decodeOrdered(dec)
				if err != nil {
					return nil, err
				}
				arr = append(arr, val)
			}
			dec.Token()
			return arr, nil
		}
	case string, bool:
		return v, nil
	case json.Number:
		return strconv.ParseUint(string(v), 10, 64)
	case nil:
		return nil, nil
	}
	return nil, fmt.Errorf("unexpected token %v", tok)
}

// traceFor menghitung seluruh layer dari msgpack bytes ke signature.
func traceFor(packed []byte, c goldenCase, priv *secp256k1.PrivateKey) goldenTrace {
	var vault []byte
	if c.Vault != nil {
		vault, _ = hex.DecodeString(strings.TrimPrefix(*c.Vault, "0x"))
	}
	sum := sha256.Sum256(packed)
	conn := ConnectionID(packed, c.Nonce, vault, c.ExpiresAfter)
	digest := L1Digest(conn, c.IsMainnet)
	sig := SignDigest(priv, digest)
	var t goldenTrace
	t.MsgpackHex = hex.EncodeToString(packed)
	t.MsgpackSHA256 = hex.EncodeToString(sum[:])
	t.NonceHex = hex.EncodeToString(NonceBytes(c.Nonce))
	t.SuffixHex = hex.EncodeToString(Suffix(vault, c.ExpiresAfter))
	t.ConnectionIDHex = hex.EncodeToString(conn)
	t.PhantomSource = PhantomSource(c.IsMainnet)
	t.EIP712DigestHex = hex.EncodeToString(digest)
	t.Signature.R, t.Signature.S, t.Signature.V = sig.R, sig.S, sig.V
	return t
}

// firstDivergence mengembalikan layer PERTAMA yang berbeda, urut dari atas ke bawah.
func firstDivergence(got, want goldenTrace) string {
	type layer struct{ name, g, w string }
	layers := []layer{
		{"msgpack_hex", got.MsgpackHex, want.MsgpackHex},
		{"msgpack_sha256", got.MsgpackSHA256, want.MsgpackSHA256},
		{"nonce_hex", got.NonceHex, want.NonceHex},
		{"vault_expires_suffix_hex", got.SuffixHex, want.SuffixHex},
		{"connection_id_hex", got.ConnectionIDHex, want.ConnectionIDHex},
		{"phantom_source", got.PhantomSource, want.PhantomSource},
		{"eip712_digest_hex", got.EIP712DigestHex, want.EIP712DigestHex},
		{"signature.r", got.Signature.R, want.Signature.R},
		{"signature.s", got.Signature.S, want.Signature.S},
		{"signature.v", strconv.Itoa(got.Signature.V), strconv.Itoa(want.Signature.V)},
	}
	for _, l := range layers {
		if l.g != l.w {
			return fmt.Sprintf("layer pertama yang berbeda: %s\n  go     : %s\n  python : %s", l.name, l.g, l.w)
		}
	}
	return ""
}

func TestGoldenParity(t *testing.T)     { runCorpus(t, "../golden_corpus.json", 30) }
func TestRandomParity1000(t *testing.T) { runCorpus(t, "../golden_random_1000.json", 1000) }

func runCorpus(t *testing.T, path string, minCases int) {
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var g goldenFile
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	if len(g.Cases) < minCases {
		t.Fatalf("corpus terlalu kecil: %d", len(g.Cases))
	}
	keyBytes, _ := hex.DecodeString(strings.TrimPrefix(g.TestKeyHex, "0x"))
	priv := secp256k1.PrivKeyFromBytes(keyBytes)

	if got := "0x" + hex.EncodeToString(AddressOf(priv)); !strings.EqualFold(got, g.SignerAddress) {
		t.Fatalf("alamat signer beda: go=%s python=%s", got, g.SignerAddress)
	}

	for _, c := range g.Cases {
		c := c
		t.Run(c.Name+"/encoder_only", func(t *testing.T) {
			// Jalur A: action_json (urutan terjaga) -> msgpack Go -> seluruh layer
			dec := json.NewDecoder(bytes.NewReader(c.ActionJSON))
			dec.UseNumber()
			action, err := decodeOrdered(dec)
			if err != nil {
				t.Fatal(err)
			}
			packed, err := EncodeMsgpack(action)
			if err != nil {
				t.Fatal(err)
			}
			if d := firstDivergence(traceFor(packed, c, priv), c.Trace); d != "" {
				t.Fatalf("%s\n%s", c.Note, d)
			}
		})
		t.Run(c.Name+"/builder_from_spec", func(t *testing.T) {
			// Jalur B: parameter mentah (float, dst) -> builder Go -> seluruh layer.
			// Ini menguji FloatToWire, urutan key, omit cloid, dan lebar integer.
			action, err := BuildAction(c.Spec)
			if err != nil {
				t.Fatal(err)
			}
			packed, err := EncodeMsgpack(action)
			if err != nil {
				t.Fatal(err)
			}
			if d := firstDivergence(traceFor(packed, c, priv), c.Trace); d != "" {
				t.Fatalf("%s\n%s", c.Note, d)
			}
		})
	}
}

// Guard kecil: encoder harus menolak nil (SDK tidak pernah mengirim null).
func TestEncoderRejectsNil(t *testing.T) {
	if _, err := EncodeMsgpack(OMap{{"c", nil}}); err == nil {
		t.Fatal("nil seharusnya ditolak")
	}
}

func TestFloatToWire(t *testing.T) {
	cases := map[float64]string{100.0: "100", 2500.5: "2500.5", 0.00012345: "0.00012345", 1000000.5: "1000000.5", 0.001: "0.001", 1.0: "1"}
	for in, want := range cases {
		got, err := FloatToWire(in)
		if err != nil || got != want {
			t.Errorf("FloatToWire(%v)=%q,%v want %q", in, got, err, want)
		}
	}
	if _, err := FloatToWire(0.123456789123); err == nil {
		t.Error("harus menolak nilai yang berubah saat dibulatkan ke 8 desimal")
	}
}
