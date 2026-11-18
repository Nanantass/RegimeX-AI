# Testnet tools (Phase 1.5 dan Phase 2)

Tidak bisa dijalankan dari sandbox Claude: tidak ada akses jaringan ke domain
Hyperliquid dari sini. Kedua alat sudah diuji sebisa mungkin tanpa jaringan
(build, vet, dry-run, guard) dan siap dipakai di lingkungan dengan akses testnet.

## Phase 1.5 - Server Acceptance (`go/cmd/send`)

Mengirim action bertanda tangan (signer yang sudah lulus 2066 subtest parity)
ke Hyperliquid testnet.

    cd go
    export HL_TEST_PRIVATE_KEY=0x...   # WAJIB kunci testnet, jangan pernah kunci mainnet
    go run ./cmd/send -action order -dry-run=true            # lihat payload dulu
    go run ./cmd/send -action order -dry-run=false            # kirim sungguhan
    go run ./cmd/send -action stop_loss -trigger 45000 -mark 50000 -dry-run=false
    go run ./cmd/send -action cancel -oid 123 -asset 0 -dry-run=false
    go run ./cmd/send -action batch_modify -oid 123 -asset 0 -px 49000 -dry-run=false

Sudah diverifikasi tanpa jaringan:
- build dan `go vet` bersih
- dry-run mencetak msgpack, connectionId, EIP-712 digest, signature, dan body JSON
  persis dengan urutan key yang di-hash (bukan re-marshal map biasa)
- `stop_loss` memakai builder trigger yang sama dengan `parity/go/stoploss.go`,
  DITOLAK otomatis (exit 1, tidak mengirim apa pun) kalau trigger sudah terlampaui
  mark price
- tanpa `HL_TEST_PRIVATE_KEY`, program berhenti dengan pesan jelas, tidak diam-diam
  memakai kunci kosong

Checklist saat menjalankan sungguhan:
- [ ] `order`: diterima, muncul di openOrders
- [ ] `stop_loss`: diterima SEBAGAI TRIGGER (bukan limit) di openOrders, tidak langsung terisi
- [ ] `batch_modify`: oid berubah (sesuai temuan dokumentasi resmi), order lama tidak hilang kalau modify gagal
- [ ] `cancel`: order hilang dari openOrders
- [ ] catat response mentah tiap langkah untuk lampiran laporan Fase 1.5

## Phase 2 - Schema Discovery (`testnet_tools/dump_payloads.py`)

    pip install hyperliquid-python-sdk==0.24.0
    python3 dump_payloads.py --address 0xALAMAT_TESTNET_ANDA

Prasyarat manual di UI/API testnet sebelum menjalankan (lihat docstring di file):
posisi + SL + TP + reduce-only biasa pada symbol yang sama, satu kali modify SL
(di-dump SEBELUM dan SESUDAH lewat --tag, lihat bawah), trigger market DAN trigger
limit berdampingan, partial close, full close, lalu reopen dengan parameter identik
(untuk investigasi Position Identity).

### Investigasi transisi (bukan hanya snapshot akhir)

Setiap panggilan dengan --tag berbeda tersimpan sebagai file terpisah, tidak saling
menimpa:

    python3 dump_payloads.py --address 0x... --tag pre_modify
    #  ... lakukan modify SL di UI/API testnet ...
    python3 dump_payloads.py --address 0x... --tag post_modify
    python3 dump_payloads.py --address 0x... --tag trigger_market_vs_limit
    python3 dump_payloads.py --address 0x... --tag reopen

Setelah pre_modify dan post_modify tersedia, bandingkan otomatis:

    python3 dump_payloads.py --compare --out raw

Diverifikasi dengan data tiruan (dua skenario): mendeteksi modify sebagai
cancel-lama-lalu-create-baru (oid berbeda) dan modify in-place (oid sama),
keduanya melaporkan kesimpulan sementara yang benar.

Output: `raw/*.json` (payload mentah, disimpan apa adanya) plus ringkasan di
terminal yang menyorot kandidat pemetaan field ke `OpenOrderInfo` di
`parity/go/recovery.go`, dan kandidat field Position Identity pada fill
terbaru (`startPosition`, dll).

Kirim kembali `raw/` dan ringkasan terminal untuk dipetakan ke `schema_contract.md`.

## Trigger Investigation (limit_px semantics)

Setelah dump `trigger_market_vs_limit`, bandingkan manual di `raw/openOrders_trigger_market_vs_limit_*.json`:
field pada trigger `isMarket=true` vs `isMarket=false` untuk order stop yang sama arahnya.
Perbedaan struktur field di antara keduanya (atau ketiadaan perbedaan) adalah bukti langsung
untuk menjawab apakah `limit_px` berfungsi sebagai batas slippage, diabaikan, atau punya arti lain.
