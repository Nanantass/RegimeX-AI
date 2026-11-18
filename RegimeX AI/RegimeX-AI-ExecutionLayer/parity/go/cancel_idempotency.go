package hlsign

import (
	"errors"
	"strings"
)

// CancelOutcome: klasifikasi hasil cancel yang HARUS dibedakan, supaya timeout
// pada cancel yang sebenarnya sukses tidak memicu safe-mode palsu atau recovery
// loop (audit poin 3). Status dan pesan error PERSIS dari server BELUM diketahui
// (menunggu testnet); klasifikasi di sini beroperasi pada KATEGORI respons, bukan
// string literal, supaya pemetaan string->kategori adalah SATU titik yang perlu
// diupdate setelah dump nyata tersedia, bukan tersebar di banyak tempat.
type CancelOutcome int

const (
	CancelSucceeded   CancelOutcome = iota // order memang tercancel di exchange
	CancelAlreadyGone                      // order sudah tidak ada (cancel sebelumnya, atau fill, atau cancel ganda) -> idempoten, perlakukan sama seperti sukses
	CancelUnknown                          // network/timeout/5xx -> TIDAK diketahui, wajib rekonsiliasi, TIDAK boleh dianggap sukses maupun gagal
	CancelRejected                         // validasi/signature/permission -> gagal keras, jangan retry buta
)

var (
	ErrCancelUnknown  = errors.New("cancel outcome unknown, reconcile with exchange before any retry or state change")
	ErrCancelRejected = errors.New("cancel rejected by exchange")
)

// ClassifyCancelResponse memetakan response mentah ke CancelOutcome. errMsg kosong
// berarti HTTP 200 dengan body sukses. httpErr adalah error transport (termasuk
// timeout) kalau request gagal sebelum sempat dapat response.
//
// TODO TESTNET WAJIB: isi knownAlreadyGoneMarkers dan knownRejectedMarkers dari
// pesan error ASLI yang diamati di dump_payloads.py / go/cmd/send, BUKAN ditebak.
// Sampai diisi, fungsi ini SENGAJA konservatif: apa pun yang tidak cocok kategori
// sukses eksplisit jatuh ke CancelUnknown, bukan diam-diam dianggap salah satu
// dari dua kategori lain.
func ClassifyCancelResponse(httpErr error, statusCode int, errMsg string) CancelOutcome {
	if httpErr != nil {
		return CancelUnknown // timeout, connection reset, dst: TIDAK tahu apakah exchange memprosesnya
	}
	if statusCode == 200 && errMsg == "" {
		return CancelSucceeded
	}
	for _, m := range knownAlreadyGoneMarkers {
		if contains(errMsg, m) {
			return CancelAlreadyGone
		}
	}
	for _, m := range knownRejectedMarkers {
		if contains(errMsg, m) {
			return CancelRejected
		}
	}
	// Kelas 5xx/rate-limit yang BELUM punya marker pasti juga masuk sini secara aman
	if statusCode >= 500 || statusCode == 429 {
		return CancelUnknown
	}
	// Default aman: tidak dikenali -> unknown, bukan sukses dan bukan gagal
	return CancelUnknown
}

// TODO TESTNET: isi dari dump nyata. Kosong dengan sengaja sampai saat itu,
// supaya tidak ada yang keliru mengira daftar ini sudah final.
var knownAlreadyGoneMarkers = []string{
	// contoh placeholder, GANTI dengan string asli dari response testnet:
	// "Order was never placed, already filled, or canceled",
}
var knownRejectedMarkers = []string{
	// "Invalid signature", "Insufficient margin", dst dari observasi nyata
}

// contains melakukan pencarian substring CASE-INSENSITIVE, karena pesan error
// exchange bisa berbeda kapitalisasi dari marker yang dicatat ("Order Not Found"
// vs "order not found") tanpa mengubah makna. Marker TETAP harus diisi dari
// observasi nyata; normalisasi ini hanya mencegah mismatch murni karena huruf
// besar/kecil, bukan pengganti verifikasi isi marker itu sendiri.
func contains(s, sub string) bool {
	if sub == "" {
		return false
	}
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}

// ReconcileCancel: dipanggil saat CancelUnknown, memverifikasi lewat openOrders
// apakah order benar-benar masih ada, SEBELUM memutuskan retry atau sukses.
// Tidak pernah menebak dari pesan error semata.
//
// TODO TESTNET WAJIB sebelum dipakai di jalur produksi: fungsi ini saat ini
// hanya menerima sinyal biner (oid lama ada/tidak di openOrders). Ini BELUM
// cukup untuk membedakan dua kondisi yang berbeda maknanya:
//
//	(a) order benar-benar hilang (tercancel bersih), vs
//	(b) oid lama hilang KARENA modify menghasilkan oid baru (bukan cancel),
//	    sehingga stop sebenarnya MASIH hidup dengan identitas berbeda.
//
// Kondisi (b) kalau diperlakukan sama dengan (a) akan membuat sistem
// menyimpulkan "cancel sukses" padahal order (dengan oid baru) masih aktif,
// berpotensi menimbulkan orphan yang tidak pernah dibersihkan.
// ReconcileCancelWithCorrelation di bawah adalah kerangka untuk kondisi ini,
// AKTIF dipakai hanya setelah Fase 2 (schema discovery, khususnya hasil
// pre_modify/post_modify) mengonfirmasi apakah korelasi oid-lama ke oid-baru
// via cloid memang diperlukan untuk cancel (bukan hanya untuk modify).
func ReconcileCancel(stillInOpenOrders bool) CancelOutcome {
	if stillInOpenOrders {
		return CancelUnknown // masih ada, belum aman diasumsikan apa pun; caller boleh retry
	}
	return CancelAlreadyGone // sudah tidak ada -> idempoten sukses, retry TIDAK PERLU dan TIDAK berbahaya
}

// ReconcileCancelWithCorrelation: versi yang membedakan oid lama hilang-bersih
// dari oid lama digantikan oid baru dengan cloid yang sama. cloid kosong berarti
// korelasi tidak tersedia (fallback ke ReconcileCancel biasa).
func ReconcileCancelWithCorrelation(oldOidPresent bool, cloid string, openOrdersByCloid map[string]int64) (outcome CancelOutcome, survivingOid int64) {
	if oldOidPresent {
		return CancelUnknown, 0
	}
	if cloid != "" {
		if newOid, found := openOrdersByCloid[cloid]; found {
			// oid lama hilang TAPI cloid yang sama muncul dengan oid baru:
			// ini BUKAN cancel sukses, order masih hidup dengan identitas baru.
			return CancelUnknown, newOid
		}
	}
	return CancelAlreadyGone, 0
}
