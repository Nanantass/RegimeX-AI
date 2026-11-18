module hlsign

go 1.22

// Replace directives ini mengarahkan golang.org/x/crypto dan golang.org/x/sys
// ke mirror resminya di GitHub. Ini TIDAK WAJIB di lingkungan dengan akses
// internet normal (go mod tidy akan resolve langsung ke proxy.golang.org
// tanpa baris ini). Baris ini dipertahankan karena terbukti diperlukan di
// lingkungan dengan akses jaringan terbatas (hanya mengizinkan github.com,
// bukan golang.org), dan isi modul di GitHub mirror identik dengan sumber
// resminya, sehingga aman dipakai di kedua jenis lingkungan. go.sum di
// sebelah file ini sudah cocok dengan resolusi lewat jalur ini; kalau
// replace directive ini dihapus, jalankan `go mod tidy` ulang untuk
// menghasilkan go.sum yang sesuai jalur proxy.golang.org langsung.
replace golang.org/x/crypto => github.com/golang/crypto v0.31.0

replace golang.org/x/sys => github.com/golang/sys v0.28.0

require (
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.3.0
	golang.org/x/crypto v0.31.0
)

require golang.org/x/sys v0.28.0 // indirect
