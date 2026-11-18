package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestArchiveResponse_SuccessIsRecordedVerbatim(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(`{"status":"ok","response":{"data":{"statuses":[{"resting":{"oid":123}}]}}}`))
	}))
	defer srv.Close()

	tmp := t.TempDir()
	old, _ := os.Getwd()
	os.Chdir(tmp)
	defer os.Chdir(old)

	reqBody := []byte(`{"action":{"type":"order"},"nonce":1}`)
	resp, err := http.Post(srv.URL, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	sentAt := time.Now().UTC()
	path := archiveResponse("order", sentAt, reqBody, resp, nil)

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("arsip tidak terbaca di %s: %v", path, err)
	}
	var entry map[string]any
	if err := json.Unmarshal(raw, &entry); err != nil {
		t.Fatal(err)
	}
	if entry["action"] != "order" {
		t.Fatalf("action salah: %v", entry["action"])
	}
	if entry["http_status_code"].(float64) != 200 {
		t.Fatalf("status salah: %v", entry["http_status_code"])
	}
	body, ok := entry["response_body_raw"].(string)
	if !ok || body == "" {
		t.Fatalf("response_body_raw hilang atau kosong: %v", entry["response_body_raw"])
	}
	if entry["transport_error"] != nil {
		t.Fatalf("transport_error seharusnya nil untuk request sukses: %v", entry["transport_error"])
	}
}

func TestArchiveResponse_TransportErrorIsRecorded(t *testing.T) {
	tmp := t.TempDir()
	old, _ := os.Getwd()
	os.Chdir(tmp)
	defer os.Chdir(old)

	reqErr := &urlError{"connection refused"}
	path := archiveResponse("cancel", time.Now().UTC(), []byte(`{"action":{"type":"cancel"}}`), nil, reqErr)

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("arsip tidak terbaca: %v", err)
	}
	var entry map[string]any
	json.Unmarshal(raw, &entry)
	if entry["transport_error"] != "connection refused" {
		t.Fatalf("transport_error tidak terekam dengan benar: %v", entry["transport_error"])
	}
	// Nama field HARUS sama dengan jalur sukses (response_body_raw), bukan
	// response_body, supaya skema arsip konsisten satu nama per konsep.
	if _, exists := entry["response_body_raw"]; !exists {
		t.Fatal("field response_body_raw harus ada (bernilai null) bahkan saat transport error")
	}
	if entry["response_body_raw"] != nil {
		t.Fatalf("response_body_raw harus nil saat transport error, dapat %v", entry["response_body_raw"])
	}
	if _, exists := entry["response_body"]; exists {
		t.Fatal("field lama 'response_body' (tanpa _raw) tidak boleh muncul lagi, nama harus konsisten")
	}
}

// trackingBody membungkus io.Reader dan menghitung berapa kali Close()
// dipanggil, supaya kita bisa membuktikan DUA lifecycle terpisah (body asli
// vs replay body), bukan sekadar "pembacaan kedua masih jalan".
type trackingBody struct {
	io.Reader
	closed int
}

func (b *trackingBody) Close() error {
	b.closed++
	return nil
}

// Regresi BUG NYATA yang ditemukan audit: percobaan sebelumnya menghapus
// SEMUA Close() di archiveResponse, padahal begitu resp.Body ditimpa, referensi
// ke body HTTP ASLI hilang, sehingga body asli tidak pernah tertutup sungguhan
// (kebocoran koneksi), meski defer di caller "terlihat" berhasil menutup
// sesuatu (padahal itu NopCloser pengganti, Close()-nya no-op).
//
// Test ini memakai http.Transport asli (bukan trackingBody langsung di resp.Body,
// karena net/http mengelola body koneksinya sendiri) untuk membuktikan pola
// transfer-ownership via trackingBody yang disuntik LANGSUNG ke archiveResponse
// lewat resp palsu, memverifikasi:
//  1. body asli (trackingBody) ditutup TEPAT SATU KALI oleh archiveResponse
//  2. resp.Body setelahnya BUKAN body asli lagi (sudah diganti)
//  3. caller tetap bisa membaca payload yang sama dari body pengganti
//  4. caller menutup body pengganti tanpa mempengaruhi body asli
func TestArchiveResponse_OriginalBodyClosedExactlyOnce(t *testing.T) {
	tmp := t.TempDir()
	oldWd, _ := os.Getwd()
	os.Chdir(tmp)
	defer os.Chdir(oldWd)

	const payload = `{"status":"ok"}`
	original := &trackingBody{Reader: bytes.NewReader([]byte(payload))}
	resp := &http.Response{StatusCode: 200, Status: "200 OK", Body: original}

	archiveResponse("order", time.Now().UTC(), []byte("{}"), resp, nil)

	// 1. Body ASLI harus tertutup TEPAT SATU KALI oleh archiveResponse,
	// bukan nol kali (leak) dan bukan dua kali (double-close).
	if original.closed != 1 {
		t.Fatalf("body asli harus ditutup tepat 1 kali oleh archiveResponse, tertutup %d kali", original.closed)
	}

	// 2. resp.Body sekarang BUKAN lagi body asli (sudah diganti dengan replay).
	if resp.Body == io.ReadCloser(original) {
		t.Fatal("resp.Body seharusnya sudah diganti dengan replay body, bukan body asli yang sama")
	}

	// 3. Caller (simulasi main()) tetap bisa membaca payload yang sama persis
	// dari body pengganti.
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != payload {
		t.Fatalf("caller membaca %q, want %q", got, payload)
	}

	// 4. Caller menutup body PENGGANTI. Ini tidak boleh mempengaruhi hitungan
	// Close() pada body asli (keduanya objek berbeda, lifecycle terpisah).
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("Close() oleh caller terhadap replay body gagal: %v", err)
	}
	if original.closed != 1 {
		t.Fatalf("Close() oleh caller terhadap replay body TIDAK BOLEH mengubah hitungan Close() body asli, tapi sekarang %d", original.closed)
	}
}

// Guard tambahan: kalau ReadAll body asli gagal, archiveResponse tetap harus
// menutup body asli (bukan membiarkannya bocor karena early-return yang lupa).
type errorBody struct{ closed int }

func (b *errorBody) Read(p []byte) (int, error) { return 0, fmt.Errorf("simulated read error") }
func (b *errorBody) Close() error               { b.closed++; return nil }

func TestArchiveResponse_ClosesOriginalEvenOnReadError(t *testing.T) {
	tmp := t.TempDir()
	oldWd, _ := os.Getwd()
	os.Chdir(tmp)
	defer os.Chdir(oldWd)

	eb := &errorBody{}
	resp := &http.Response{StatusCode: 200, Status: "200 OK", Body: eb}
	path := archiveResponse("order", time.Now().UTC(), []byte("{}"), resp, nil)

	if eb.closed != 1 {
		t.Fatalf("body asli harus tetap ditutup meski ReadAll gagal, tertutup %d kali", eb.closed)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var entry map[string]any
	json.Unmarshal(raw, &entry)
	if entry["read_error"] == nil {
		t.Fatal("read_error harus terekam eksplisit di arsip saat ReadAll gagal, bukan diam-diam diabaikan")
	}
}

// Regresi penting: setelah archiveResponse membaca resp.Body untuk arsip,
// PEMBACA DI main() SETELAHNYA (io.ReadAll(resp.Body)) harus tetap bisa
// membaca body yang SAMA, bukan mendapat body kosong karena sudah habis dibaca.
func TestArchiveResponse_DoesNotConsumeBodyForCaller(t *testing.T) {
	const payload = `{"status":"ok"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(payload))
	}))
	defer srv.Close()

	tmp := t.TempDir()
	old, _ := os.Getwd()
	os.Chdir(tmp)
	defer os.Chdir(old)

	resp, err := http.Post(srv.URL, "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	archiveResponse("order", time.Now().UTC(), []byte(`{}`), resp, nil)

	// Simulasikan pembacaan yang terjadi di main() SETELAH archiveResponse
	buf := make([]byte, len(payload))
	n, _ := resp.Body.Read(buf)
	if string(buf[:n]) != payload {
		t.Fatalf("body sudah habis terpakai oleh archiveResponse, caller membaca %q, want %q", buf[:n], payload)
	}
}

func TestArchiveResponse_FilenameContainsActionAndIsUnique(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("{}")) }))
	defer srv.Close()
	tmp := t.TempDir()
	old, _ := os.Getwd()
	os.Chdir(tmp)
	defer os.Chdir(old)

	resp1, _ := http.Post(srv.URL, "application/json", bytes.NewReader([]byte("{}")))
	p1 := archiveResponse("batch_modify", time.Now().UTC(), []byte("{}"), resp1, nil)
	time.Sleep(2 * time.Millisecond)
	resp2, _ := http.Post(srv.URL, "application/json", bytes.NewReader([]byte("{}")))
	p2 := archiveResponse("batch_modify", time.Now().UTC(), []byte("{}"), resp2, nil)

	if p1 == p2 {
		t.Fatalf("dua panggilan menghasilkan nama file sama, akan saling menimpa: %s", p1)
	}
	if filepath.Base(p1)[:12] != "batch_modify" {
		t.Fatalf("nama file tidak memuat action: %s", p1)
	}
}

type urlError struct{ msg string }

func (e *urlError) Error() string { return e.msg }
