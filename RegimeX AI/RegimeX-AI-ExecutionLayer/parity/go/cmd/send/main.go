// Phase 1.5 (Server Acceptance): kirim action bertanda tangan Go ke Hyperliquid
// testnet dan cetak response mentah. TIDAK bisa dijalankan dari sandbox Claude
// (tidak ada akses jaringan ke domain Hyperliquid); jalankan di lingkungan
// dengan akses testnet.
//
// Kunci privat WAJIB berasal dari variabel lingkungan, tidak pernah hardcode
// atau muncul di argumen command line (terlihat di process list / shell history).
//
// Contoh:
//
//	export HL_TEST_PRIVATE_KEY=0x...   # WAJIB kunci testnet, bukan mainnet
//	go run ./cmd/send -action order -dry-run
//	go run ./cmd/send -action order              # kirim sungguhan ke testnet
//	go run ./cmd/send -action cancel -oid 123 -asset 0
package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"hlsign"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

const testnetURL = "https://api.hyperliquid-testnet.xyz/exchange"

// defaultHTTPTimeout: nilai AWAL, BELUM tervalidasi empiris terhadap
// perilaku testnet nyata. Konsisten dengan filosofi execution layer bahwa
// network call tidak boleh menggantung tanpa batas, tapi angka 10 detik
// adalah starting point operasional, bukan hasil pengukuran. Kalau server
// testnet ternyata butuh >10 detik untuk kondisi tertentu (misalnya saat
// load tinggi), request yang sebenarnya akan berhasil bisa timeout duluan
// dan jatuh ke jalur ambiguous (lihat cancel_idempotency.go: CancelUnknown).
// WAJIB dikalibrasi ulang berdasarkan observasi p99 latency nyata sebelum
// dipakai di luar Fase 1.5 (soak test dan recovery test kemungkinan perlu
// nilai berbeda). Dibuat sebagai flag, bukan konstanta tetap, supaya
// operator bisa menyesuaikan per fase tanpa mengubah kode.
const defaultHTTPTimeout = 10 * time.Second

func mustPrivKey() *secp256k1.PrivateKey {
	hexKey := os.Getenv("HL_TEST_PRIVATE_KEY")
	if strings.TrimSpace(hexKey) == "" {
		fmt.Fprintln(os.Stderr, "CRITICAL: HL_TEST_PRIVATE_KEY tidak diset. Jangan pernah "+
			"pakai kunci mainnet di sini; buat kunci testnet khusus.")
		os.Exit(1)
	}
	key, err := parsePrivateKey(hexKey)
	if err != nil {
		// Pesan error TIDAK PERNAH memuat isi kunci, hanya alasan penolakan.
		fmt.Fprintln(os.Stderr, "HL_TEST_PRIVATE_KEY tidak valid:", err)
		os.Exit(1)
	}
	return key
}

// parsePrivateKey memvalidasi kunci privat secp256k1 dalam hex secara ketat.
// Sebelumnya jalur ini memakai fmt.Sscanf tanpa pengecekan panjang:
// (a) panjang selain 32 byte diterima dan PrivKeyFromBytes diam-diam
// menghasilkan kunci LAIN (kunci terpotong saat di-paste jadi alamat berbeda
// tanpa error), dan (b) Sscanf "%02x" menerima tanda +/- sehingga input yang
// bentuknya salah bisa lolos. Sekarang: hanya [0-9a-fA-F], tepat 64 karakter
// (32 byte) setelah awalan 0x opsional, dan nilainya harus di rentang
// secp256k1 yang valid (bukan nol, bukan >= orde kurva).
// Error tidak memuat isi kunci; hanya posisi karakter atau panjangnya.
func parsePrivateKey(s string) (*secp256k1.PrivateKey, error) {
	s = strings.TrimSpace(s) // `set X=val ` di cmd Windows menyimpan spasi di ujung
	if s == "" {
		return nil, fmt.Errorf("kosong")
	}
	if strings.ContainsAny(s, "\"'") {
		return nil, fmt.Errorf("mengandung tanda kutip; di cmd Windows tulis tanpa kutip: set HL_TEST_PRIVATE_KEY=0x...")
	}
	body := s
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		body = s[2:]
	}
	for i := 0; i < len(body); i++ {
		c := body[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return nil, fmt.Errorf("karakter non-hex pada posisi %d (setelah awalan 0x); pastikan ini kunci asli, bukan teks contoh", i+1)
		}
	}
	if len(body) != 64 {
		return nil, fmt.Errorf("panjang harus 64 karakter hex (32 byte), dapat %d", len(body))
	}
	raw, err := decodePrivateKeyHex(body)
	if err != nil {
		return nil, fmt.Errorf("gagal decode hex")
	}
	var scalar secp256k1.ModNScalar
	if overflow := scalar.SetByteSlice(raw); overflow || scalar.IsZero() {
		return nil, fmt.Errorf("nilai di luar rentang kunci secp256k1 yang valid (nol atau >= orde kurva)")
	}
	return secp256k1.PrivKeyFromBytes(raw), nil
}

// normalizeCloid memvalidasi dan menormalisasi cloid: panjang 32 karakter
// (16 byte / 128-bit) berdasarkan dokumentasi resmi Hyperliquid, DAN seluruh
// karakter benar-benar hex valid, bukan cuma cek panjang string. Perbaikan
// dari versi sebelumnya yang hanya memeriksa len(c)!=32 dan meloloskan
// karakter non-hex ("zzzz...") ke dalam action yang dikirim ke server.
//
// KLAIM YANG AKURAT (dipersempit secara sengaja): fungsi ini memvalidasi
// "format cloid yang RegimeX yakini sesuai dokumentasi Hyperliquid", BUKAN
// "format yang telah terbukti diterima server". Lolos dari normalizeCloid
// berarti valid menurut aturan lokal RegimeX, tidak serta-merta berarti
// valid menurut Hyperliquid sesungguhnya, dokumentasi dan implementasi
// server bisa berbeda. Status ini tetap 🟡 sampai Fase 1.5 (acceptance
// testnet nyata) mengonfirmasi atau membantahnya; kode lain TIDAK BOLEH
// memperlakukan "normalizeCloid sukses" sebagai setara dengan "Hyperliquid
// pasti menerima".
func normalizeCloid(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(s, "0x")))
	if s == "" {
		return "", fmt.Errorf("action cancel_by_cloid butuh -cloid (lihat -help)")
	}
	if len(s) != 32 {
		return "", fmt.Errorf("cloid harus tepat 32 karakter hex (16 byte / 128-bit per dokumentasi Hyperliquid), dapat %d karakter: %q", len(s), s)
	}
	raw, err := hexDecode(s)
	if err != nil || len(raw) != 16 {
		return "", fmt.Errorf("cloid bukan hex 128-bit valid: %q (%v)", s, err)
	}
	return "0x" + s, nil
}

// hexDecode dulu diimplementasikan manual lewat fmt.Sscanf per-byte, diganti
// encoding/hex.DecodeString standar library: lebih sederhana, lebih cepat,
// dan tidak menambah surface area bug kustom untuk sesuatu yang sudah
// disediakan standar library. Perilaku funsgional identik (ditutup oleh
// test yang sama di cloid_validation_test.go dan hex_test.go).
func hexDecode(s string) ([]byte, error) {
	return hex.DecodeString(s)
}

func decodePrivateKeyHex(s string) ([]byte, error) {
	s = strings.TrimPrefix(s, "0x")
	if s == "" {
		return nil, fmt.Errorf("empty hex string")
	}
	if len(s)%2 != 0 {
		return nil, fmt.Errorf("odd-length hex string (len=%d): private key hex must have an even number of characters", len(s))
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		// Sengaja tidak meneruskan err: InvalidByteError menampilkan karakter input.
		return nil, fmt.Errorf("hex tidak valid")
	}
	return b, nil
}

func main() {
	action := flag.String("action", "order", "order | batch_modify | cancel | cancel_by_cloid | stop_loss")
	dryRun := flag.Bool("dry-run", true, "kalau true, HANYA cetak payload dan signature, TIDAK mengirim request")
	asset := flag.Uint64("asset", 0, "asset index")
	coin := flag.String("coin", "BTC", "nama coin (untuk log saja)")
	oid := flag.Int64("oid", 0, "oid untuk cancel/batch_modify")
	cloidFlag := flag.String("cloid", "", "cloid (hex 32 karakter tanpa 0x, atau dengan 0x) untuk cancel_by_cloid")
	sz := flag.Float64("sz", 0.001, "size")
	px := flag.Float64("px", 50000, "harga limit (order biasa)")
	triggerPx := flag.Float64("trigger", 45000, "harga trigger (stop_loss)")
	mark := flag.Float64("mark", 50000, "mark price saat ini (untuk guard stop_loss)")
	isBuy := flag.Bool("buy", true, "sisi order")
	szDecimals := flag.Int("sz-decimals", 4, "presisi size asset ini")
	timeoutFlag := flag.Duration("timeout", defaultHTTPTimeout, "timeout HTTP, BELUM dikalibrasi empiris, sesuaikan per fase (acceptance/soak/recovery)")
	requireArchive := flag.Bool("require-archive", false, "kalau true, kegagalan menulis arsip forensik menghentikan program (exit 1) alih-alih hanya warning. Pakai untuk sesi testnet formal yang butuh jaminan arsip lengkap.")
	flag.Parse()

	// Timeout <= 0 di http.Client berarti UNLIMITED (dokumentasi net/http),
	// yang diam-diam membatalkan invariant "network call tidak boleh
	// menggantung tanpa batas". Ditolak eksplisit, bukan dibiarkan jadi
	// escape hatch implisit yang mudah terjadi tanpa sadar (mis. salah ketik
	// -timeout=0 atau lupa menimpa default saat scripting).
	if *timeoutFlag <= 0 {
		fmt.Fprintf(os.Stderr, "timeout harus > 0 (dapat %s); Timeout<=0 pada http.Client berarti UNLIMITED, "+
			"yang melanggar invariant network call tidak boleh menggantung tanpa batas\n", *timeoutFlag)
		os.Exit(1)
	}

	priv := mustPrivKey()
	httpClient := &http.Client{Timeout: *timeoutFlag}
	nonce := uint64(time.Now().UnixMilli())

	var spec hlsign.ActionSpec
	switch *action {
	case "order":
		spec = hlsign.ActionSpec{Kind: "order", Grouping: "na", Orders: []hlsign.OrderSpec{{
			Asset: *asset, IsBuy: *isBuy, Sz: *sz, LimitPx: *px, ReduceOnly: false,
			OrderType: hlsign.OrderTypeSpec{Limit: &hlsign.LimitSpec{Tif: "Gtc"}},
		}}}
	case "stop_loss":
		side := hlsign.SideLong
		if !*isBuy {
			side = hlsign.SideShort
		}
		s, err := hlsign.BuildStopLossSpec(*asset, side, *sz, *triggerPx, nil, *szDecimals,
			hlsign.StopConfig{SlippageBufferPct: 1.0, MinDistancePct: 0.1, RatchetOnly: false})
		if err != nil {
			fmt.Fprintln(os.Stderr, "gagal membangun stop loss:", err)
			os.Exit(1)
		}
		if err := hlsign.ValidateStopPlacement(side, s.OrderType.Trigger.TriggerPx, *mark,
			hlsign.StopConfig{MinDistancePct: 0.1}); err != nil {
			fmt.Fprintln(os.Stderr, "REFUSING untuk mengirim (guard marketability):", err)
			os.Exit(1)
		}
		spec = hlsign.ActionSpec{Kind: "order", Grouping: "na", Orders: []hlsign.OrderSpec{s}}
	case "batch_modify":
		spec = hlsign.ActionSpec{Kind: "batchModify", Modifies: []hlsign.ModifySpec{{
			Oid: mustJSON(*oid),
			Order: hlsign.OrderSpec{Asset: *asset, IsBuy: *isBuy, Sz: *sz, LimitPx: *px,
				OrderType: hlsign.OrderTypeSpec{Limit: &hlsign.LimitSpec{Tif: "Gtc"}}},
		}}}
	case "cancel":
		spec = hlsign.ActionSpec{Kind: "cancel", Cancels: []hlsign.CancelSpec{{Asset: *asset, Oid: uint64(*oid)}}}
	case "cancel_by_cloid":
		cloidStr, err := normalizeCloid(*cloidFlag)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		spec = hlsign.ActionSpec{Kind: "cancelByCloid", Cancels: []hlsign.CancelSpec{{Asset: *asset, Cloid: &cloidStr}}}
	default:
		fmt.Fprintln(os.Stderr, "action tidak dikenal:", *action)
		os.Exit(1)
	}

	built, err := hlsign.BuildAction(spec)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gagal membangun action:", err)
		os.Exit(1)
	}
	packed, err := hlsign.EncodeMsgpack(built)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gagal encode msgpack:", err)
		os.Exit(1)
	}
	conn := hlsign.ConnectionID(packed, nonce, nil, nil)
	digest := hlsign.L1Digest(conn, false) // false = testnet (source "b")
	sig := hlsign.SignDigest(priv, digest)

	// PENTING: action_json yang dikirim ke server harus dibangun dari `built` (OMap)
	// supaya urutan key identik dengan yang di-hash, bukan re-marshal struct Go biasa
	// yang urutannya tidak terjamin.
	actionJSON := omapToOrderedJSON(built)

	req := map[string]any{
		"action":    json.RawMessage(actionJSON),
		"nonce":     nonce,
		"signature": map[string]any{"r": sig.R, "s": sig.S, "v": sig.V},
	}
	body, _ := json.MarshalIndent(req, "", "  ")

	fmt.Println("=== coin:", *coin, "asset:", *asset, "action:", *action, "===")
	fmt.Println("msgpack_hex   :", hexStr(packed))
	fmt.Println("connectionId  :", hexStr(conn))
	fmt.Println("eip712_digest :", hexStr(digest))
	fmt.Println("signature     :", sig.R, sig.S, sig.V)
	fmt.Println("request body  :")
	fmt.Println(string(body))

	if *dryRun {
		fmt.Println("\n[dry-run] TIDAK dikirim. Jalankan ulang dengan -dry-run=false untuk kirim ke testnet.")
		return
	}

	sentAt := time.Now().UTC()
	resp, err := httpClient.Post(testnetURL, "application/json", bytes.NewReader(body))

	// Arsip forensik DITULIS SELALU, baik sukses maupun gagal, supaya kalau
	// Hyperliquid mengubah perilaku di masa depan ada bukti apa adanya untuk
	// membedakan "bug RegimeX" dari "perubahan perilaku exchange", tanpa harus
	// menebak dari log yang sudah diproses. Arsip ditulis SEBELUM program bisa
	// exit karena error, supaya kegagalan request pun tetap terekam.
	//
	// KEBIJAKAN EKSPLISIT saat arsip gagal ditulis (disk penuh, permission,
	// path tidak ada): DEFAULT adalah fail-OPEN, request yang sudah terlanjur
	// dikirim ke exchange TIDAK dibatalkan hanya karena arsip lokal gagal,
	// karena request itu sendiri sudah punya efek nyata di exchange (order
	// terkirim, dst) yang tidak bisa "dibatalkan" dengan menghentikan program
	// setelahnya. Kegagalan tetap dilaporkan KERAS ke stderr (archiveResponse
	// sudah menulis WARNING eksplisit), bukan diam-diam diabaikan.
	// --require-archive membalik ini jadi fail-closed TERHADAP KELANJUTAN
	// TEST RUN LOKAL, BUKAN terhadap exchange action itu sendiri. Istilah
	// "fail-closed" di sini dipersempit secara sengaja: karena archiveResponse
	// dipanggil SETELAH httpClient.Post(...), kalau arsip gagal ditulis,
	// request ke exchange sudah terlanjur terkirim (dan mungkin sudah
	// dieksekusi) sebelum program sempat exit. Yang "ditutup" adalah proses
	// lokal dan exit criteria formal, bukan tindakan di exchange, yang tidak
	// bisa dibatalkan lewat menghentikan program setelahnya. Operator WAJIB
	// memeriksa state order secara manual (openOrders) kalau ini terjadi,
	// sesuai pesan CRITICAL di bawah.
	archivePath := archiveResponse(*action, sentAt, body, resp, err)
	archiveFailed := isArchiveFailure(archivePath)
	if archiveFailed {
		fmt.Fprintln(os.Stderr, "\nCRITICAL: arsip forensik GAGAL ditulis:", archivePath)
		if *requireArchive {
			fmt.Fprintln(os.Stderr, "--require-archive aktif: program dihentikan meski request sudah terkirim ke exchange. "+
				"PERIKSA state order secara manual (openOrders) karena hasil request ini TIDAK tercatat di arsip.")
			os.Exit(1)
		}
	} else {
		fmt.Println("\n[arsip] disimpan ke:", archivePath)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "request gagal:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	fmt.Println("\n=== response, status:", resp.Status, "===")
	fmt.Println(string(respBody))
}

// archiveResponse menyimpan request body, response body (atau pesan error
// transport kalau request gagal sebelum dapat response), status HTTP, dan
// timestamp, apa adanya, tanpa diproses. Nama file memuat action dan timestamp
// supaya tidak saling menimpa antar pemanggilan.
func archiveResponse(action string, sentAt time.Time, reqBody []byte, resp *http.Response, reqErr error) string {
	return archiveResponseTo("raw_responses", action, sentAt, reqBody, resp, reqErr)
}

// archiveResponseTo menerima direktori sebagai parameter (bukan hardcode),
// supaya test bisa menyuntik direktori yang sengaja tidak bisa ditulis atau
// sudah berisi file untuk memaksa kegagalan NYATA, bukan hanya mencocokkan
// string prefix hasil kegagalan.
func archiveResponseTo(dir, action string, sentAt time.Time, reqBody []byte, resp *http.Response, reqErr error) string {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "WARNING: gagal membuat direktori arsip:", err)
		return "(gagal, lihat stderr)"
	}

	entry := map[string]any{
		"action":       action,
		"sent_at_utc":  sentAt.Format(time.RFC3339Nano),
		"request_body": json.RawMessage(reqBody),
	}

	// KONTRAK OWNERSHIP resp.Body, SEBAGAI TRANSFER, bukan "tidak pernah close":
	//
	//   HTTP original body -> archiveResponse ReadAll -> archiveResponse Close(ORIGINAL)
	//     -> pasang replay body -> ownership replay body kembali ke caller
	//     -> caller membaca -> caller Close(REPLAY BODY)
	//
	// Percobaan sebelumnya ("archiveResponse tidak pernah menutup apa pun")
	// SALAH: begitu resp.Body ditimpa dengan io.NopCloser, referensi ke body
	// HTTP ASLI hilang sepenuhnya dari resp.Body. defer di main() lalu menutup
	// REPLAY BODY (NopCloser, Close()-nya no-op), sementara koneksi HTTP asli
	// TIDAK PERNAH ditutup sungguhan. Itu kebocoran koneksi nyata (http.Client
	// tidak bisa mengembalikan koneksi ke pool), bukan cuma ambiguitas kontrak.
	//
	// Fix: archiveResponse WAJIB menutup body ASLI tepat setelah membacanya,
	// SEBELUM mengganti resp.Body. Transfer ownership terjadi di titik itu:
	// sebelum Close(original) -> archiveResponse yang bertanggung jawab;
	// setelah replay body terpasang -> caller yang bertanggung jawab.
	if reqErr != nil {
		entry["transport_error"] = reqErr.Error()
		entry["http_status"] = nil
		// Nama field SAMA dengan jalur sukses (response_body_raw), bukan
		// response_body, supaya skema arsip konsisten: satu nama field untuk
		// satu konsep, nilainya null kalau memang tidak ada response.
		entry["response_body_raw"] = nil
	} else {
		originalBody := resp.Body
		respBody, readErr := io.ReadAll(originalBody)
		// archiveResponse menutup body ASLI di sini, SEBELUM resp.Body diganti.
		// Ini satu-satunya titik di seluruh program yang menutup koneksi HTTP
		// asli; caller tidak pernah memegang referensinya lagi setelah ini.
		if closeErr := originalBody.Close(); closeErr != nil {
			fmt.Fprintln(os.Stderr, "WARNING: gagal menutup response body asli:", closeErr)
		}

		entry["http_status"] = resp.Status
		entry["http_status_code"] = resp.StatusCode
		if readErr != nil {
			// Arsip forensik tidak boleh diam-diam parsial: kegagalan baca
			// dicatat eksplisit, bukan dibuang lewat "_".
			entry["read_error"] = readErr.Error()
			entry["response_body_raw"] = nil
		} else {
			// response_body_raw disimpan sebagai string mentah (bukan
			// json.RawMessage), karena response yang gagal/malformed belum
			// tentu JSON valid, dan arsip tidak boleh menolak menyimpan data
			// mentah hanya karena parsing gagal.
			entry["response_body_raw"] = string(respBody)
		}

		// Pasang replay body. Dari titik ini ownership resp.Body SEPENUHNYA
		// milik caller lagi: caller yang membaca DAN caller yang menutup
		// (lewat defer resp.Body.Close() seperti sebelumnya di main()).
		resp.Body = io.NopCloser(bytes.NewReader(respBody))
	}

	out, marshalErr := json.MarshalIndent(entry, "", "  ")
	if marshalErr != nil {
		// Arsip forensik yang gagal di-marshal TIDAK boleh diam-diam
		// menghasilkan file kosong atau parsial; laporkan eksplisit.
		fmt.Fprintln(os.Stderr, "WARNING: gagal marshal entri arsip:", marshalErr)
		return "(gagal marshal, lihat stderr)"
	}

	// Invariant yang WAJIB dijamin: 1 request -> 1 file immutable, TIDAK PERNAH
	// diam-diam menimpa arsip sebelumnya.
	//
	// Cakupan jaminan (dipersempit secara sengaja, bukan "collision impossible"):
	//   - DALAM SATU PROSES: collision DICEGAH. nextArchiveSeq() (atomic.Int64)
	//     menjamin keunikan lintas goroutine di proses yang sama, terbukti lewat
	//     stress test 1000 goroutine paralel dengan action+sentAt identik
	//     (TestArchiveResponseTo_ConcurrentCallsProduceUniqueFiles, lulus bersih
	//     dengan -race).
	//   - LINTAS PROSES (dua instance tool ini berjalan bersamaan menulis ke
	//     direktori arsip yang sama): archiveSeqCounter TIDAK dibagi antar
	//     proses (ia in-memory per-proses), sehingga dua proses secara teoretis
	//     bisa menghasilkan seq yang sama persis pada sentAt yang juga sama
	//     sampai nanodetik. Ini TIDAK menghasilkan corruption atau overwrite
	//     senyap (O_EXCL mengubahnya jadi error eksplisit pada proses yang
	//     kalah), tapi SALAH SATU proses akan gagal mengarsipkan request-nya.
	//     Kontrak tool ini saat ini mengasumsikan single-process; menjalankan
	//     beberapa instance bersamaan terhadap direktori arsip yang sama belum
	//     didukung penuh.
	//
	// Presisi timestamp dinaikkan ke nanodetik + nomor urut atomik sebagai
	// pembeda, DAN O_EXCL sebagai lapis terakhir yang menjamin collision
	// (sisa kemungkinan apa pun, termasuk lintas proses) menjadi error
	// eksplisit, bukan overwrite senyap.
	seq := nextArchiveSeq()
	fname := fmt.Sprintf("%s/%s_%s_%06d.json", dir, action, sentAt.Format("20060102T150405.000000000Z"), seq)
	f, err := os.OpenFile(fname, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		fmt.Fprintln(os.Stderr, "WARNING: gagal membuat arsip (kemungkinan nama bentrok, invariant 1-file-per-request dilanggar):", err)
		return "(gagal, lihat stderr)"
	}
	defer f.Close()
	if _, err := f.Write(out); err != nil {
		fmt.Fprintln(os.Stderr, "WARNING: gagal menulis isi arsip:", err)
		return "(gagal, lihat stderr)"
	}
	return fname
}

var archiveSeqCounter atomic.Int64

// nextArchiveSeq memberi nomor urut atomik per-proses, pembeda tambahan di
// luar timestamp supaya dua pemanggilan beruntun dalam proses yang sama
// (bahkan kalau jam sistem punya resolusi lebih kasar dari yang diasumsikan)
// tidak pernah menghasilkan nama file yang sama.
func nextArchiveSeq() int64 {
	return archiveSeqCounter.Add(1)
}

func hexStr(b []byte) string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, c := range b {
		out[i*2] = hexdigits[c>>4]
		out[i*2+1] = hexdigits[c&0xf]
	}
	return string(out)
}

func mustJSON(v int64) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// omapToOrderedJSON menulis JSON dengan urutan key PERSIS seperti OMap,
// supaya action yang dikirim ke server sama persis dengan yang di-hash
// (encoding/json Go tidak menjaga urutan map biasa, karena itu OMap dipakai).
func omapToOrderedJSON(v any) []byte {
	var b bytes.Buffer
	writeJSON(&b, v)
	return b.Bytes()
}

func writeJSON(b *bytes.Buffer, v any) {
	switch x := v.(type) {
	case hlsign.OMap:
		b.WriteByte('{')
		for i, kv := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			je, _ := json.Marshal(kv.Key)
			b.Write(je)
			b.WriteByte(':')
			writeJSON(b, kv.Val)
		}
		b.WriteByte('}')
	case []any:
		b.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			writeJSON(b, e)
		}
		b.WriteByte(']')
	default:
		je, _ := json.Marshal(x)
		b.Write(je)
	}
}

// isArchiveFailure mendeteksi apakah return value archiveResponse(To)
// menandakan kegagalan, dipakai di main() dan di test. Satu fungsi, satu
// definisi, supaya detektor di main() dan di test TIDAK BISA diam-diam
// berbeda (akar masalah Temuan 3: sebelumnya test menulis ulang string
// literal sendiri alih-alih memakai detektor yang sama dengan main()).
func isArchiveFailure(path string) bool {
	return strings.HasPrefix(path, "(gagal")
}
