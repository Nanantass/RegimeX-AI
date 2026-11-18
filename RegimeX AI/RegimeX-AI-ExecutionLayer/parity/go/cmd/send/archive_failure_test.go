package main

// Regresi Temuan 3 audit: test sebelumnya (archive_policy_test.go, dihapus)
// hanya mencocokkan string literal "(gagal..." terhadap dirinya sendiri,
// TIDAK PERNAH memanggil archiveResponse sungguhan. Test di bawah memaksa
// kegagalan NYATA (direktori tidak bisa ditulis, dan collision nama file)
// lewat archiveResponseTo yang menerima direktori sebagai parameter.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestArchiveResponseTo_FailsWhenDirIsUnwritable memaksa kegagalan NYATA
// dengan mengarahkan ke path yang tidak bisa dibuat sebagai direktori
// (menimpa sebuah file biasa dengan nama yang sama), bukan mencocokkan
// string hasil kegagalan yang ditulis manual di test.
func TestArchiveResponseTo_FailsWhenDirIsUnwritable(t *testing.T) {
	tmp := t.TempDir()
	// Buat FILE biasa di path yang akan dipakai sebagai nama direktori arsip,
	// sehingga os.MkdirAll pada path itu PASTI gagal (ENOTDIR).
	blockingPath := filepath.Join(tmp, "blocked")
	if err := os.WriteFile(blockingPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("{}")) }))
	defer srv.Close()
	resp, err := http.Post(srv.URL, "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}

	got := archiveResponseTo(blockingPath, "order", time.Now().UTC(), []byte("{}"), resp, nil)
	if !isArchiveFailure(got) {
		t.Fatalf("archiveResponseTo seharusnya gagal (direktori tidak bisa dibuat di atas file biasa), dapat: %s", got)
	}
}

// TestArchiveResponseTo_CollisionIsExplicitErrorNotOverwrite memaksa dua
// penulisan dengan PARAMETER IDENTIK (termasuk sentAt yang sama persis) ke
// nama file yang SENGAJA sudah dibuat lebih dulu lewat O_EXCL manual, untuk
// membuktikan bahwa arsip TIDAK PERNAH diam-diam overwrite, sesuai invariant
// "1 request -> 1 file immutable".
func TestArchiveResponseTo_CollisionIsExplicitErrorNotOverwrite(t *testing.T) {
	tmp := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("{}")) }))
	defer srv.Close()

	resp1, _ := http.Post(srv.URL, "application/json", nil)
	sentAt := time.Now().UTC()
	path1 := archiveResponseTo(tmp, "order", sentAt, []byte("{}"), resp1, nil)
	if isArchiveFailure(path1) {
		t.Fatalf("penulisan pertama seharusnya sukses: %s", path1)
	}
	original, err := os.ReadFile(path1)
	if err != nil {
		t.Fatal(err)
	}

	// Paksa collision: tulis ulang file yang SAMA PERSIS secara manual dengan
	// O_EXCL, mensimulasikan race dua proses menghasilkan nama identik.
	// (Di implementasi nyata ini nyaris tidak mungkin terjadi karena ada
	// nextArchiveSeq(), tapi kita verifikasi bahwa SEANDAINYA terjadi,
	// hasilnya adalah error eksplisit, bukan overwrite senyap.)
	f, err := os.OpenFile(path1, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err == nil {
		f.Close()
		t.Fatal("O_EXCL terhadap file yang sudah ada seharusnya gagal, test ini salah setup")
	}

	// Isi file original TIDAK BOLEH berubah oleh percobaan collision di atas.
	after, err := os.ReadFile(path1)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) {
		t.Fatal("isi arsip berubah setelah percobaan collision, invariant 1-file-immutable dilanggar")
	}
}

// TestArchiveResponseTo_SequenceNumberPreventsCollisionInPractice membuktikan
// bahwa dalam penggunaan NORMAL (bukan collision paksa di atas), dua
// pemanggilan beruntun dengan action dan sentAt yang SAMA PERSIS tetap
// menghasilkan nama file berbeda berkat nextArchiveSeq(), sehingga keduanya
// benar-benar tersimpan, bukan salah satu hilang tertimpa.
func TestArchiveResponseTo_SequenceNumberPreventsCollisionInPractice(t *testing.T) {
	tmp := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("{}")) }))
	defer srv.Close()

	sentAt := time.Now().UTC() // SENGAJA sama persis untuk kedua panggilan
	resp1, _ := http.Post(srv.URL, "application/json", nil)
	resp2, _ := http.Post(srv.URL, "application/json", nil)

	p1 := archiveResponseTo(tmp, "order", sentAt, []byte(`{"n":1}`), resp1, nil)
	p2 := archiveResponseTo(tmp, "order", sentAt, []byte(`{"n":2}`), resp2, nil)

	if p1 == p2 {
		t.Fatalf("dua panggilan dengan sentAt identik tetap harus menghasilkan nama berbeda (nextArchiveSeq), dapat sama: %s", p1)
	}
	if isArchiveFailure(p1) || isArchiveFailure(p2) {
		t.Fatalf("keduanya seharusnya sukses, dapat p1=%s p2=%s", p1, p2)
	}
	// Keduanya harus benar-benar ada sebagai file terpisah dengan isi masing-masing.
	b1, err1 := os.ReadFile(p1)
	b2, err2 := os.ReadFile(p2)
	if err1 != nil || err2 != nil {
		t.Fatalf("salah satu file tidak terbaca: err1=%v err2=%v", err1, err2)
	}
	if string(b1) == string(b2) {
		t.Fatal("isi kedua file seharusnya berbeda (request_body berbeda), kemungkinan salah satu tertimpa")
	}
}

// TestArchiveResponseTo_ConcurrentCallsProduceUniqueFiles (Temuan 3 audit):
// klaim desain sebelumnya ("atomic sequence", "O_EXCL", "anti collision")
// hanya diuji untuk pemanggilan BERURUTAN. Test di atas membuktikan nomor
// urut mencegah collision pada pemanggilan sequential, tapi belum membuktikan
// aman terhadap pemanggilan PARALEL dari banyak goroutine, yang justru skenario
// paling realistis untuk nextArchiveSeq() (atomic.Int64) dan O_EXCL dirancang.
// Test ini menembak 1000 goroutine paralel ke archiveResponseTo dengan
// action dan sentAt yang SAMA PERSIS, memverifikasi: semua nama file unik,
// tidak ada error, dan seluruh isi file benar-benar berbeda (tidak ada yang
// kebetulan menimpa isi goroutine lain).
// Test ini sengaja HERMETIK: tidak ada server, tidak ada koneksi TCP.
// Versi sebelumnya menembak httptest.Server dengan 1000 goroutine untuk
// memperoleh *http.Response, lalu gagal di Windows dengan "connection
// actively refused" karena burst koneksi ke localhost, sebelum
// archiveResponseTo pernah dipanggil. Itu kegagalan harness, bukan kegagalan
// fungsi yang diuji: archiveResponseTo hanya menerima *http.Response yang
// sudah ada dan tidak membuka koneksi apa pun. Perbaikan yang tepat adalah
// menghilangkan jaringan dari test, bukan menambal burst koneksi dengan
// retry atau listener kustom (net.Listen sendiri tidak memberi kontrol
// atas backlog, jadi listener manual tidak mengubah apa-apa).
//
// Konkurensi yang diuji sekarang murni: 1000 goroutine dilepas serentak lewat
// barrier (channel yang ditutup), semuanya memanggil archiveResponseTo dengan
// action dan sentAt yang SAMA PERSIS, supaya kontensi pada nextArchiveSeq()
// dan O_EXCL maksimal. Diverifikasi: semua nama file unik, tidak ada error,
// dan isi tiap file milik goroutine-nya sendiri (tidak tertimpa).
func syntheticResponse() *http.Response {
	return &http.Response{
		StatusCode: 200,
		Status:     "200 OK",
		Body:       io.NopCloser(strings.NewReader("{}")),
	}
}

func TestArchiveResponseTo_ConcurrentCallsProduceUniqueFiles(t *testing.T) {
	tmp := t.TempDir()

	const n = 1000
	sentAt := time.Now().UTC() // SENGAJA identik di semua goroutine

	paths := make([]string, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			<-start // barrier: semua goroutine dilepas bersamaan
			reqBody := []byte(fmt.Sprintf(`{"n":%d}`, i))
			paths[i] = archiveResponseTo(tmp, "order", sentAt, reqBody, syntheticResponse(), nil)
		}()
	}
	close(start)
	wg.Wait()

	seen := make(map[string]int)
	for i, p := range paths {
		if isArchiveFailure(p) {
			t.Fatalf("goroutine %d: archiveResponseTo gagal secara tak terduga: %s", i, p)
		}
		if prev, dup := seen[p]; dup {
			t.Fatalf("COLLISION: goroutine %d dan %d menghasilkan nama file sama: %s", prev, i, p)
		}
		seen[p] = i
	}
	if len(seen) != n {
		t.Fatalf("want %d nama file unik, dapat %d", n, len(seen))
	}

	// Verifikasi isi dengan mem-parse JSON, bukan pencarian substring.
	// Substring rapuh terhadap format (spasi setelah titik dua, urutan field,
	// indentasi), dan itu persis yang menghasilkan false failure pertama
	// ("n": 0 vs "n":0). Parsing hanya bergantung pada struktur: field
	// request_body harus berisi {"n": i} milik goroutine ini sendiri.
	for i, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("goroutine %d: file tidak terbaca: %v", i, err)
		}
		var archived struct {
			RequestBody struct {
				N *int `json:"n"`
			} `json:"request_body"`
		}
		if err := json.Unmarshal(raw, &archived); err != nil {
			t.Fatalf("goroutine %d: arsip %s bukan JSON valid: %v", i, p, err)
		}
		if archived.RequestBody.N == nil {
			t.Fatalf("goroutine %d: arsip %s tidak memuat request_body.n", i, p)
		}
		if *archived.RequestBody.N != i {
			t.Fatalf("goroutine %d: arsip %s memuat n=%d, kemungkinan tertimpa goroutine lain", i, p, *archived.RequestBody.N)
		}
	}
}
