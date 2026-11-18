"""Phase 2 (Schema Discovery): dump payload mentah dari Hyperliquid TESTNET
dan sorot field-field yang dibutuhkan RegimeX (OpenOrderInfo, isStopOrder,
dan investigasi Position Identity).

TIDAK BISA dijalankan dari sandbox Claude (tidak ada akses jaringan ke
api.hyperliquid-testnet.xyz). Jalankan ini di lingkungan yang punya akses
testnet, lalu kirim kembali hasil `raw/*.json` dan ringkasan untuk dipetakan
ke schema_contract.md.

Alur yang dibutuhkan SEBELUM menjalankan skrip ini (manual, sekali saja):
  1. Buat wallet testnet, isi saldo testnet (faucet Hyperliquid testnet).
  2. Buka posisi kecil di UI testnet (atau lewat Phase 1.5 sender) pada
     SATU symbol, misal BTC.
  3. Pasang SL (trigger), lalu TP (trigger), lalu satu reduce-only limit
     biasa (BUKAN trigger) pada posisi yang sama.
  4. SEBELUM modify SL: jalankan skrip ini dengan --tag pre_modify (dump
     openOrders akan berisi oid dan timestamp SL yang LAMA).
  5. Modify SL sekali (ubah triggerPx), catat waktu (UTC) saat modify
     dilakukan.
  6. SEGERA setelah modify: jalankan skrip ini lagi dengan --tag post_modify
     (dump openOrders akan berisi oid dan timestamp SL yang BARU, kalau
     Hyperliquid memakai skema cancel-lalu-create; atau oid yang SAMA
     kalau modify in-place; keduanya adalah temuan yang sama pentingnya).
  7. Pasang SATU trigger market (isMarket=true) dan SATU trigger limit
     (isMarket=false) berdampingan pada order terpisah (boleh keduanya
     reduce-only kecil, tidak harus dieksekusi), lalu jalankan skrip
     dengan --tag trigger_market_vs_limit -> untuk membandingkan field
     limit_px pada kedua bentuk trigger.
  8. Tutup sebagian posisi (partial fill), lalu tutup penuh.
  9. Buka lagi posisi yang SAMA PERSIS (side, size, entry price kalau bisa)
     -> untuk investigasi Position Identity (reopen).

Setiap pemanggilan skrip dengan --tag berbeda TIDAK menimpa hasil sebelumnya;
semua tersimpan sebagai file terpisah bertanda tag dan timestamp, supaya
transisi antar langkah (bukan hanya snapshot akhir) bisa dibandingkan.

Pakai:
    pip install hyperliquid-python-sdk==0.24.0
    python3 dump_payloads.py --address 0xYOUR_TESTNET_ADDRESS
"""
import argparse
import json
import os
import sys
from datetime import datetime, timezone

from hyperliquid.info import Info

TESTNET_URL = "https://api.hyperliquid-testnet.xyz"

# Field yang saat ini DIASUMSIKAN dipakai OpenOrderInfo / isStopOrder di kode Go
# (lihat parity/go/recovery.go). Skrip ini TIDAK memaksa payload cocok dengan
# ini -- ia hanya menyoroti kandidat field yang mirip, supaya pemetaan manual
# ke schema_contract.md lebih cepat.
CANDIDATE_FIELD_NAMES = {
    "oid": ["oid"],
    "coin": ["coin"],
    "cloid": ["cloid"],
    "is_trigger": ["orderType", "isTrigger", "triggerCondition"],
    "trigger_tpsl": ["tpsl", "isPositionTpsl"],
    "reduce_only": ["reduceOnly"],
    "timestamp": ["timestamp"],
    "trigger_px": ["triggerPx"],
}

# Hipotesis Position Identity yang perlu dikonfirmasi/dibantah (lihat exit
# criteria v4): apakah field pada fill pembuka (startPosition == 0) bisa
# dipakai sebagai penanda awal siklus posisi baru.
POSITION_IDENTITY_CANDIDATE_FIELDS = ["startPosition", "oid", "tid", "time", "hash"]


def save_raw(raw_dir: str, name: str, obj) -> str:
    os.makedirs(raw_dir, exist_ok=True)
    path = os.path.join(raw_dir, f"{name}.json")
    with open(path, "w") as f:
        json.dump(obj, f, indent=2, default=str)
    return path


def highlight_fields(label: str, items):
    if not items:
        print(f"  [{label}] kosong (0 item)")
        return
    sample = items[0]
    if not isinstance(sample, dict):
        print(f"  [{label}] {len(items)} item, bentuk tidak terduga: {type(sample)}")
        return
    print(f"  [{label}] {len(items)} item. Field pada contoh pertama:")
    for k in sorted(sample.keys()):
        print(f"      {k!r}: {sample[k]!r}")
    print(f"  [{label}] kandidat pemetaan ke OpenOrderInfo:")
    for target, candidates in CANDIDATE_FIELD_NAMES.items():
        found = [c for c in candidates if c in sample]
        print(f"      {target:15s} -> {found or 'TIDAK DITEMUKAN, cek manual'}")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--address", required=True, help="alamat wallet testnet (0x...)")
    ap.add_argument("--out", default="raw", help="direktori output payload mentah")
    ap.add_argument("--base-url", default=TESTNET_URL)
    ap.add_argument("--tag", default="snapshot",
        help="label langkah investigasi, mis. pre_modify / post_modify / "
             "trigger_market_vs_limit / reopen. Dipakai sebagai bagian nama file.")
    args = ap.parse_args()

    info = Info(base_url=args.base_url, skip_ws=True)
    ts = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    label = f"{args.tag}_{ts}"

    print(f"== Dump payload testnet [{args.tag}] untuk {args.address} @ {ts} ==\n")

    open_orders = info.open_orders(args.address)
    save_raw(args.out, f"openOrders_{label}", open_orders)
    highlight_fields("openOrders", open_orders)
    print()

    frontend_orders = info.frontend_open_orders(args.address)
    save_raw(args.out, f"frontendOpenOrders_{label}", frontend_orders)
    highlight_fields("frontendOpenOrders", frontend_orders)
    print()

    user_state = info.user_state(args.address)
    save_raw(args.out, f"clearinghouseState_{label}", user_state)
    positions = user_state.get("assetPositions", [])
    print(f"  [clearinghouseState] {len(positions)} assetPositions, {user_state.get('time')=}")
    for p in positions:
        print(f"      {json.dumps(p, indent=2)[:400]}")
    print()

    fills = info.user_fills(args.address)
    save_raw(args.out, f"userFills_{label}", fills)
    print(f"  [userFills] {len(fills)} fill. Kandidat field Position Identity pada fill terbaru:")
    if fills:
        f0 = fills[0]
        for k in POSITION_IDENTITY_CANDIDATE_FIELDS:
            print(f"      {k:15s} -> {f0.get(k, 'TIDAK ADA')}")
        print(f"      seluruh field fill terbaru: {sorted(f0.keys())}")
    print()

    print("== Selesai. Kirim seluruh isi direktori 'raw/' beserta output di atas. ==")
    print("Langkah berikutnya: cocokkan hasil di atas dengan struct OpenOrderInfo")
    print("di parity/go/recovery.go, lalu isi schema_contract.md.")


def compare_pre_post(out_dir: str):
    """Kalau ada tepat satu file openOrders_pre_modify_*.json dan satu
    openOrders_post_modify_*.json di --out, bandingkan otomatis dan tampilkan
    oid/timestamp mana yang berubah. Dipanggil manual setelah kedua tag
    sudah didump: python3 dump_payloads.py --compare --out raw
    """
    import glob

    pre = sorted(glob.glob(os.path.join(out_dir, "openOrders_pre_modify_*.json")))
    post = sorted(glob.glob(os.path.join(out_dir, "openOrders_post_modify_*.json")))
    if not pre or not post:
        print("Butuh minimal satu file pre_modify dan satu post_modify di", out_dir)
        return
    with open(pre[-1]) as f:
        before = json.load(f)
    with open(post[-1]) as f:
        after = json.load(f)

    def by_oid(items):
        return {i.get("oid"): i for i in items if isinstance(i, dict)}

    b, a = by_oid(before), by_oid(after)
    print(f"pre_modify  ({pre[-1]}): {len(b)} order, oid={sorted(b.keys())}")
    print(f"post_modify ({post[-1]}): {len(a)} order, oid={sorted(a.keys())}")
    same = set(b) & set(a)
    only_pre = set(b) - set(a)
    only_post = set(a) - set(b)
    print(f"oid tidak berubah   : {sorted(same) or 'tidak ada'}")
    print(f"oid HILANG (lama)   : {sorted(only_pre) or 'tidak ada'}  <- kalau ADA, modify = cancel+create")
    print(f"oid BARU (post)     : {sorted(only_post) or 'tidak ada'}  <- kalau ADA, modify = cancel+create")
    if same and not only_pre and not only_post:
        print(">> Kesimpulan sementara: modify tampak IN-PLACE (oid sama). Verifikasi field lain (timestamp) juga.")
    elif only_pre and only_post:
        print(">> Kesimpulan sementara: modify tampak sebagai CANCEL lama + CREATE baru (oid berbeda).")


if __name__ == "__main__":
    if "--compare" in sys.argv:
        out_dir = "raw"
        if "--out" in sys.argv:
            out_dir = sys.argv[sys.argv.index("--out") + 1]
        compare_pre_post(out_dir)
    else:
        main()
