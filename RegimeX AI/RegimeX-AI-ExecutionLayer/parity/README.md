# Signing parity harness (Go vs SDK Python resmi)

Sumber kebenaran: hyperliquid-python-sdk 0.24.0 (pin versi ini, catat jika di-upgrade).
Kunci di corpus adalah kunci uji publik. JANGAN dipakai untuk dana riil.

## Membangkitkan corpus (deterministik, bisa diulang)
    pip install hyperliquid-python-sdk==0.24.0
    python3 gen_golden.py     # 33 kasus batas (boundary corpus)
    python3 gen_random.py     # 1000 payload acak berseed 20260928

## Menjalankan parity di Go
    cd go && go mod tidy && go test -count=1 -v ./...

Setiap kasus diuji lewat DUA jalur:
- encoder_only: action_json (urutan key terjaga) -> msgpack Go -> seluruh layer.
- builder_from_spec: parameter mentah (float, dst) -> builder Go -> seluruh layer.
Kegagalan melaporkan LAYER PERTAMA yang berbeda:
msgpack_hex, msgpack_sha256, nonce_hex, vault_expires_suffix_hex, connection_id_hex,
phantom_source, eip712_digest_hex, signature.r, signature.s, signature.v.

## Batas klaim
Lulus di sini berarti signer Go setara dengan SDK Python untuk input di corpus.
Ini BELUM membuktikan server Hyperliquid menerima request. Itu sisa Fase 1 (uji testnet).
