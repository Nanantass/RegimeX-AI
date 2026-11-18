// Package hlsign: signer L1 action Hyperliquid, dirancang untuk parity byte-for-byte
// dengan SDK Python resmi (hyperliquid-python-sdk).
//
// Keputusan desain:
//   - Encoding msgpack ditulis eksplisit (bukan reflection atas struct) supaya urutan key,
//     lebar integer, dan omit-vs-null tidak bergantung perilaku default library.
//   - nil, float, dan integer negatif DITOLAK saat encoding (fail loudly), tidak pernah
//     di-encode diam-diam menjadi sesuatu yang berbeda dari SDK.
package hlsign

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/big"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"golang.org/x/crypto/sha3"
)

// KV dan OMap: map dengan urutan key yang eksplisit (urutan menentukan hash).
type KV struct {
	Key string
	Val any
}
type OMap []KV

func keccak256(parts ...[]byte) []byte {
	h := sha3.NewLegacyKeccak256()
	for _, p := range parts {
		h.Write(p)
	}
	return h.Sum(nil)
}

// EncodeMsgpack meng-encode subset msgpack yang dipakai action Hyperliquid, identik
// dengan msgpack.packb Python (int positif selalu memakai keluarga uint kompak).
func EncodeMsgpack(v any) ([]byte, error) {
	var b bytes.Buffer
	if err := encode(&b, v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func encode(b *bytes.Buffer, v any) error {
	switch x := v.(type) {
	case OMap:
		writeHeader(b, len(x), 0x80, 0xde, 0xdf, 16)
		for _, kv := range x {
			if err := encode(b, kv.Key); err != nil {
				return err
			}
			if err := encode(b, kv.Val); err != nil {
				return fmt.Errorf("key %q: %w", kv.Key, err)
			}
		}
	case []any:
		writeHeader(b, len(x), 0x90, 0xdc, 0xdd, 16)
		for i, e := range x {
			if err := encode(b, e); err != nil {
				return fmt.Errorf("index %d: %w", i, err)
			}
		}
	case string:
		n := len(x)
		switch {
		case n < 32:
			b.WriteByte(0xa0 | byte(n))
		case n < 256:
			b.WriteByte(0xd9)
			b.WriteByte(byte(n))
		case n < 65536:
			b.WriteByte(0xda)
			binary.Write(b, binary.BigEndian, uint16(n))
		default:
			b.WriteByte(0xdb)
			binary.Write(b, binary.BigEndian, uint32(n))
		}
		b.WriteString(x)
	case bool:
		if x {
			b.WriteByte(0xc3)
		} else {
			b.WriteByte(0xc2)
		}
	case uint64:
		writeUint(b, x)
	case uint:
		writeUint(b, uint64(x))
	case int:
		if x < 0 {
			return fmt.Errorf("negative integer %d not supported", x)
		}
		writeUint(b, uint64(x))
	case nil:
		return fmt.Errorf("nil value refused: SDK omits absent fields, it never sends null")
	default:
		return fmt.Errorf("unsupported type %T", v)
	}
	return nil
}

func writeHeader(b *bytes.Buffer, n int, fix, m16, m32 byte, fixMax int) {
	switch {
	case n < fixMax:
		b.WriteByte(fix | byte(n))
	case n < 65536:
		b.WriteByte(m16)
		binary.Write(b, binary.BigEndian, uint16(n))
	default:
		b.WriteByte(m32)
		binary.Write(b, binary.BigEndian, uint32(n))
	}
}

func writeUint(b *bytes.Buffer, n uint64) {
	switch {
	case n <= 127:
		b.WriteByte(byte(n))
	case n <= 0xff:
		b.WriteByte(0xcc)
		b.WriteByte(byte(n))
	case n <= 0xffff:
		b.WriteByte(0xcd)
		binary.Write(b, binary.BigEndian, uint16(n))
	case n <= 0xffffffff:
		b.WriteByte(0xce)
		binary.Write(b, binary.BigEndian, uint32(n))
	default:
		b.WriteByte(0xcf)
		binary.Write(b, binary.BigEndian, n)
	}
}

// Suffix = bagian setelah nonce: penanda vault (+alamat) lalu optional expiresAfter.
// Mengikuti action_hash SDK: 0x00 jika tanpa vault, 0x01+addr jika ada; expiresAfter
// ditambahkan sebagai 0x00 + 8 byte big-endian bila tidak nil.
func Suffix(vault []byte, expiresAfter *uint64) []byte {
	var s []byte
	if vault == nil {
		s = []byte{0x00}
	} else {
		s = append([]byte{0x01}, vault...)
	}
	if expiresAfter != nil {
		s = append(s, 0x00)
		var e [8]byte
		binary.BigEndian.PutUint64(e[:], *expiresAfter)
		s = append(s, e[:]...)
	}
	return s
}

func NonceBytes(nonce uint64) []byte {
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], nonce)
	return n[:]
}

// ConnectionID = keccak256(msgpack || nonce || suffix)
func ConnectionID(packed []byte, nonce uint64, vault []byte, expiresAfter *uint64) []byte {
	return keccak256(packed, NonceBytes(nonce), Suffix(vault, expiresAfter))
}

// PhantomSource: "a" mainnet, "b" testnet (SDK: construct_phantom_agent).
func PhantomSource(isMainnet bool) string {
	if isMainnet {
		return "a"
	}
	return "b"
}

func uint256(n uint64) []byte {
	out := make([]byte, 32)
	binary.BigEndian.PutUint64(out[24:], n)
	return out
}

// L1Digest: hash EIP-712 untuk struct Agent dengan domain L1 Hyperliquid
// (name "Exchange", version "1", chainId 1337, verifyingContract nol).
func L1Digest(connectionID []byte, isMainnet bool) []byte {
	domainTypeHash := keccak256([]byte("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"))
	domainSep := keccak256(domainTypeHash, keccak256([]byte("Exchange")), keccak256([]byte("1")), uint256(1337), make([]byte, 32))
	agentTypeHash := keccak256([]byte("Agent(string source,bytes32 connectionId)"))
	structHash := keccak256(agentTypeHash, keccak256([]byte(PhantomSource(isMainnet))), connectionID)
	return keccak256([]byte{0x19, 0x01}, domainSep, structHash)
}

type Signature struct {
	R string
	S string
	V int
}

// SignDigest: ECDSA secp256k1 recoverable, nonce RFC6979 deterministik, low-S,
// v = 27 + recoveryID (format yang sama dengan eth_account).
// r dan s berformat hex minimal tanpa leading zero (sama dengan eth_utils.to_hex(int)).
func SignDigest(priv *secp256k1.PrivateKey, digest []byte) Signature {
	sig := ecdsa.SignCompact(priv, digest, false)
	r := new(big.Int).SetBytes(sig[1:33])
	s := new(big.Int).SetBytes(sig[33:65])
	return Signature{R: "0x" + r.Text(16), S: "0x" + s.Text(16), V: int(sig[0])}
}

func AddressOf(priv *secp256k1.PrivateKey) []byte {
	pub := priv.PubKey().SerializeUncompressed()
	return keccak256(pub[1:])[12:]
}
