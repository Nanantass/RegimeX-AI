# RegimeX AI: Spesifikasi Paket

Dokumen ini merangkum seluruh isi paket: arsitektur yang dirancang, apa yang
sudah diuji dan bagaimana, serta apa yang masih murni menunggu data testnet
nyata. Ditulis sebagai titik masuk tunggal, bukan pengganti dokumen rinci
lain di dalam paket (lihat daftar di bagian "Isi Paket").

## 1. Ringkasan Proyek

RegimeX AI adalah sistem trading crypto kuantitatif berbasis Elliott Wave,
market structure, dan regime detection, dengan execution layer yang
terintegrasi ke Hyperliquid (Go). Filosofi inti: "Predict Structure, Not
Predict Price."

Arsitektur lengkap mencakup: Data Layer, Feature Engineering, Market
Structure Engine, Market Regime Engine, Elliott Wave Engine, Fibonacci
Cluster Engine, Signal Engine, Risk Engine, Execution Engine, Portfolio
Engine, Analytics Engine, dan ML Layer opsional. Paket ini berfokus pada
Execution Layer, yang telah melalui puluhan putaran audit mendalam
mencakup: concurrency model, recovery architecture, safe-mode lifecycle,
signing parity, dan tooling validasi testnet.

## 2. Status Saat Ini (per versi terakhir dokumen exit criteria)

| Area | Status |
|---|---|
| SafeMode (persistence, concurrency, history, schema validation) | Offline/mock-validated |
| Recovery architecture (snapshot stabilization, freshness, orphan handling) | Offline/mock-validated |
| Concurrency model (advisory lock, CAS lifecycle, single-flight) | Offline/mock-validated |
| Signing parity offline (2066+ subtest vs SDK Python resmi) | Offline/mock-validated |
| MoveStopLoss design (trigger order, guard marketability, ratchet) | Offline/mock-validated |
| Test tooling & forensic archive (go/cmd/send, dump_payloads.py) | Offline/mock-validated, 57/57 test lulus termasuk `-race` |
| OpenOrderInfo mapping | Testnet evidence required |
| Modify semantics (in-place vs cancel+create) | Testnet evidence required |
| Trigger modify support | Testnet evidence required |
| limit_px semantics | Testnet evidence required |
| Server acceptance (Fase 1.5) | Testnet evidence required |
| Position Identity | **Architectural risk**, berpotensi mengubah desain recovery |
| Real-money deployment | Not approved |

**"Offline/mock-validated"** berarti: teruji lewat unit test, mock server
HTTP lokal, dan data tiruan deterministik. **Belum pernah** menyentuh
jaringan Hyperliquid sungguhan (testnet maupun mainnet).

## 3. Isi Paket

```
SPESIFIKASI.md                     <- dokumen ini
regimex-testnet-exit-criteria.md   <- exit criteria terperinci per fase (v16)
parity/
  README.md                        <- cara menjalankan corpus + Go parity test
  gen_golden.py                    <- 33 kasus batas (boundary corpus) dari SDK resmi
  gen_random.py                    <- 1000 payload acak berseed, lintas order/batchModify/cancel/cancelByCloid
  golden_corpus.json                <- hasil gen_golden.py (deterministik)
  golden_random_1000.json           <- hasil gen_random.py (deterministik, seed 20260928)
  go/
    go.mod
    signing.go                     <- encoder msgpack, connectionId, EIP-712, ECDSA sign
    builders.go                    <- FloatToWire, OrderWire, BuildAction
    signing_test.go                <- parity test terhadap golden + random corpus
    stoploss.go                    <- BuildStopLossSpec, ValidateStopPlacement, MoveStop
    stoploss_test.go
    recovery.go                    <- IsStopLossOrder, PickMostRecentStop, RecoverPositionOrders
    cancel_idempotency.go          <- klasifikasi cancel: Succeeded/AlreadyGone/Unknown/Rejected
    cancel_idempotency_test.go
    cmd/send/
      main.go                      <- CLI pengirim signed action ke testnet (order/batch_modify/cancel/cancel_by_cloid/stop_loss)
      verify.go                    <- VerifyOrderIntent, perbandingan field-by-field sadar-tipe
      archive_test.go, archive_failure_test.go, cancel_by_cloid_test.go,
      cloid_validation_test.go, hex_test.go, verify_manual_test.go
  testnet_tools/
    README.md
    dump_payloads.py               <- dump payload testnet (openOrders, clearinghouseState, dst) + --compare
```

## 4. Cara Menjalankan

### 4.1 Signing parity (lokal, tanpa testnet)
```bash
cd parity/go
go mod tidy
go test -count=1 -v ./...        # 57 test, semua lulus, termasuk -race
go test -count=1 -race ./...
```

### 4.2 Membangkitkan ulang corpus (opsional, deterministik)
```bash
pip install hyperliquid-python-sdk==0.24.0
cd parity
python3 gen_golden.py
python3 gen_random.py
```

### 4.3 Mengirim ke testnet nyata (Fase 1.5, BELUM pernah dijalankan dari sandbox ini)
```bash
cd parity/go
export HL_TEST_PRIVATE_KEY=0x...        # WAJIB kunci testnet, bukan mainnet
go run ./cmd/send -action order -dry-run=true          # lihat payload dulu
go run ./cmd/send -action order -dry-run=false          # kirim sungguhan
go run ./cmd/send -action stop_loss -trigger 45000 -mark 50000 -dry-run=false
go run ./cmd/send -action cancel -oid 123 -asset 0 -dry-run=false
go run ./cmd/send -action cancel_by_cloid -cloid 0x... -asset 0 -dry-run=false
go run ./cmd/send -action batch_modify -oid 123 -asset 0 -px 49000 -dry-run=false
```
Setiap pengiriman otomatis diarsipkan ke `raw_responses/`. Flag penting:
`-timeout` (default 10s, belum dikalibrasi empiris), `-require-archive`
(fail-closed terhadap kelanjutan test run lokal bila arsip gagal ditulis).

### 4.4 Dump payload Fase 2 (BELUM pernah dijalankan dari sandbox ini)
```bash
pip install hyperliquid-python-sdk==0.24.0
cd parity/testnet_tools
python3 dump_payloads.py --address 0xALAMAT_TESTNET --tag pre_modify
# ... lakukan modify SL di UI/API testnet ...
python3 dump_payloads.py --address 0xALAMAT_TESTNET --tag post_modify
python3 dump_payloads.py --compare --out raw
```

## 5. Keterbatasan yang Diketahui (bukan bug, tapi batas desain)

- **Position Identity**: belum ada pengenal posisi yang terbukti stabil
  lintas siklus close-reopen. Ini architectural risk tertinggi di paket ini.
- **Anti-collision arsip forensik**: unik per proses, bukan unik global;
  dua instance `go/cmd/send` berjalan bersamaan ke direktori arsip yang
  sama belum didukung penuh (lihat komentar di `main.go`).
- **Validasi cloid**: memvalidasi format sesuai dokumentasi Hyperliquid,
  bukan format yang terbukti diterima server.
- **Timeout HTTP**: nilai default (10 detik) adalah starting point
  operasional, belum dikalibrasi dari p99 latency testnet nyata.
- **Ownership fingerprint** (deployment identity untuk cloid): 8-byte,
  wajib dikonfigurasi unik per deployment (prod/paper/staging), bukan
  bukti kriptografis penuh terhadap kredensial yang diklon.

Rincian lengkap tiap keterbatasan, termasuk riwayat temuan dan
perbaikannya, ada di `regimex-testnet-exit-criteria.md`.

## 6. Prasyarat Sebelum Real-Money Deployment

Lima fase wajib dilalui berurutan (detail di `regimex-testnet-exit-criteria.md`):

1. **Signing parity + server acceptance** (Fase 1, 1.5, 1.6), termasuk
   verifikasi `MoveStopLoss` sungguhan menghasilkan trigger order, bukan
   limit order marketable.
2. **Schema discovery** (Fase 2), termasuk investigasi Position Identity,
   yang hasilnya bisa mengubah desain recovery.
3. **Recovery matrix** (Fase 3), diuji terhadap testnet nyata, termasuk
   skenario partial-failure jaringan.
4. **Crash testing** (Fase 4), enam titik injeksi `kill -9`.
5. **Soak test** (Fase 5), 7-30 hari, termasuk metrik durasi reconciliation.

Tidak ada jalan pintas: Fase 3-5 mensyaratkan Fase 1 lulus penuh, karena
tidak ada yang bisa diuji end-to-end tanpa order yang benar-benar bisa
terkirim dan ditandatangani sah.
