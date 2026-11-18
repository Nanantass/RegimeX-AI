"""Membangkitkan golden corpus signing dari SDK Python RESMI Hyperliquid.

Semua vector deterministik: private key uji tetap (BUKAN kunci riil), nonce
di-hardcode, tanpa randomness. Setiap kasus menyimpan trace per layer supaya
divergensi Go vs Python bisa dilokalisasi ke layer pertama yang berbeda.
"""
import json, hashlib, sys
import msgpack
from eth_account import Account
from eth_account.messages import encode_typed_data
from eth_utils import keccak, to_hex
from hyperliquid.utils.signing import (
    action_hash, construct_phantom_agent, l1_payload, sign_l1_action,
    order_request_to_order_wire, order_wires_to_order_action, address_to_bytes,
)
from hyperliquid.utils.types import Cloid

TEST_KEY = "0x" + "11" * 32   # kunci uji publik, jangan pernah dipakai untuk dana riil
wallet = Account.from_key(TEST_KEY)
VAULT = "0x1234567890abcdef1234567890abcdef12345678"
CLOID_A = Cloid.from_str("0x" + "ab" * 16)
CLOID_B = Cloid.from_str("0x" + "cd" * 16)
NONCE = 1_758_960_001_234

def limit(tif="Gtc"): return {"limit": {"tif": tif}}
def trig(px, is_market, tpsl): return {"trigger": {"isMarket": is_market, "triggerPx": px, "tpsl": tpsl}}
def order(coin, is_buy, sz, px, ot, ro=False, cloid=None):
    return {"coin": coin, "is_buy": is_buy, "sz": sz, "limit_px": px, "order_type": ot,
            "reduce_only": ro, "cloid": cloid}
def wire(o, asset): return order_request_to_order_wire(o, asset)
def order_action(wires, grouping="na", builder=None):
    return order_wires_to_order_action(wires, builder, grouping)

def ospec(o, asset):
    ot = o["order_type"]
    if "limit" in ot:
        otj = {"limit": {"tif": ot["limit"]["tif"]}}
    else:
        t = ot["trigger"]
        otj = {"trigger": {"is_market": t["isMarket"], "trigger_px": t["triggerPx"], "tpsl": t["tpsl"]}}
    return {"asset": asset, "is_buy": o["is_buy"], "sz": o["sz"], "limit_px": o["limit_px"],
            "reduce_only": o["reduce_only"], "order_type": otj,
            "cloid": o["cloid"].to_raw() if o["cloid"] is not None else None}

def oa(pairs, grouping="na", builder=None):
    """pairs = [(order_dict, asset)] -> (action, spec)"""
    action = order_action([wire(o, a) for o, a in pairs], grouping, builder)
    spec = {"kind": "order", "orders": [ospec(o, a) for o, a in pairs], "grouping": grouping, "builder": builder}
    return action, spec

cases = []
def add(name, action_spec, nonce=NONCE, vault=None, expires=None, mainnet=True, note=""):
    action, spec = action_spec
    cases.append(dict(name=name, action=action, spec=spec, nonce=nonce, vault=vault,
                      expires_after=expires, is_mainnet=mainnet, note=note))

# --- order dasar dan boundary numerik ---
add("limit_buy_basic", oa([(order("BTC", True, 0.001, 100000.0, limit()), 0)]))
add("limit_sell_reduce_only_ioc", oa([(order("BTC", False, 0.5, 99999.5, limit("Ioc"), ro=True), 0)]))
add("limit_alo", oa([(order("ETH", True, 2.0, 3500.0, limit("Alo")), 1)]))
add("px_integer_sz_integer", oa([(order("ETH", True, 1.0, 100.0, limit()), 1)]), note="float_to_wire harus menghasilkan '100' dan '1', bukan '100.00000000'")
add("px_many_decimals", oa([(order("X", True, 1000000.0, 0.00012345, limit()), 5)]))
add("qty_minimum", oa([(order("BTC", True, 0.0001, 100000.0, limit()), 0)]))
add("qty_large", oa([(order("X", True, 1000000.5, 0.5, limit()), 5)]))
add("trailing_zero_px", oa([(order("ETH", True, 1.5, 2500.50, limit()), 1)]), note="'2500.5' bukan '2500.50000'")
# --- batas encoding integer msgpack pada field asset index ---
for a in (0, 127, 128, 255, 256, 65535, 65536, 100000):
    add(f"asset_index_{a}", oa([(order("X", True, 1.0, 10.0, limit()), a)]),
        note="boundary lebar integer msgpack (fixint/uint8/uint16/uint32)")
# --- trigger (stop dan take profit) ---
add("trigger_sl_market", oa([(order("BTC", False, 0.001, 95000.0, trig(95000.0, True, "sl"), ro=True), 0)]))
add("trigger_tp_limit", oa([(order("BTC", False, 0.001, 110000.0, trig(110000.0, False, "tp"), ro=True), 0)]))
# --- cloid ada, kosong, dan campuran ---
add("cloid_present", oa([(order("BTC", True, 0.001, 100000.0, limit(), cloid=CLOID_A), 0)]))
add("cloid_absent_omitted", oa([(order("BTC", True, 0.001, 100000.0, limit(), cloid=None), 0)]), note="field 'c' harus OMITTED, bukan null")
add("multi_order_mixed_cloid", oa([
    (order("BTC", True, 0.001, 100000.0, limit(), cloid=CLOID_A), 0),
    (order("ETH", False, 1.0, 3600.0, limit(), cloid=None), 1),
    (order("BTC", False, 0.001, 95000.0, trig(95000.0, True, "sl"), ro=True, cloid=CLOID_B), 0)]))
add("grouping_normal_tpsl", oa([
    (order("BTC", True, 0.001, 100000.0, limit()), 0),
    (order("BTC", False, 0.001, 95000.0, trig(95000.0, True, "sl"), ro=True), 0)], grouping="normalTpsl"))
add("builder_present", oa([(order("BTC", True, 0.001, 100000.0, limit()), 0)],
    builder={"b": "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd", "f": 10}))
# --- modify dan cancel ---
sl_order = order("BTC", False, 0.001, 96000.0, trig(96000.0, True, "sl"), ro=True)
sl_wire = wire(sl_order, 0)
def mod(oid):
    return ({"type": "batchModify", "modifies": [{"oid": oid, "order": sl_wire}]},
            {"kind": "batchModify", "modifies": [{"oid": oid, "order": ospec(sl_order, 0)}]})
add("batch_modify_oid", mod(123456789))
add("batch_modify_cloid", mod(CLOID_A.to_raw()))
add("cancel_oid", ({"type": "cancel", "cancels": [{"a": 0, "o": 123456789}]},
                   {"kind": "cancel", "cancels": [{"asset": 0, "oid": 123456789}]}))
add("cancel_by_cloid", ({"type": "cancelByCloid", "cancels": [{"asset": 0, "cloid": CLOID_A.to_raw()}]},
                        {"kind": "cancelByCloid", "cancels": [{"asset": 0, "cloid": CLOID_A.to_raw()}]}))
# --- vault, expiry, jaringan, nonce ---
base = oa([(order("BTC", True, 0.001, 100000.0, limit()), 0)])
add("vault_present", base, vault=VAULT)
add("expires_after_present", base, expires=NONCE + 60_000)
add("vault_and_expires", base, vault=VAULT, expires=NONCE + 60_000)
add("testnet_source_b", base, mainnet=False, note="source 'b' untuk testnet, 'a' untuk mainnet")
add("nonce_small", base, nonce=1)
add("nonce_large", base, nonce=2**63 - 1)

out = []
for c in cases:
    action, nonce, vault, exp, mainnet = c["action"], c["nonce"], c["vault"], c["expires_after"], c["is_mainnet"]
    packed = msgpack.packb(action)
    conn = action_hash(action, vault, nonce, exp)
    agent = construct_phantom_agent(conn, mainnet)
    signable = encode_typed_data(full_message=l1_payload(agent))
    digest = keccak(b"\x19" + signable.version + signable.header + signable.body)
    sig = sign_l1_action(wallet, action, vault, nonce, exp, mainnet)
    vb = b"\x00" if vault is None else b"\x01" + address_to_bytes(vault)
    if exp is not None: vb += b"\x00" + exp.to_bytes(8, "big")
    out.append(dict(
        name=c["name"], note=c["note"], is_mainnet=mainnet, nonce=nonce, spec=c["spec"],
        vault=vault, expires_after=exp,
        action_json=json.loads(json.dumps(action)),
        trace=dict(
            msgpack_hex=packed.hex(),
            msgpack_sha256=hashlib.sha256(packed).hexdigest(),
            nonce_hex=nonce.to_bytes(8, "big").hex(),
            vault_expires_suffix_hex=vb.hex(),
            connection_id_hex=conn.hex(),
            phantom_source=agent["source"],
            eip712_digest_hex=digest.hex(),
            signature=dict(r=sig["r"], s=sig["s"], v=sig["v"]),
        )))

doc = dict(
    generated_by="hyperliquid-python-sdk 0.24.0 (SDK resmi)",
    signer_address=wallet.address,
    test_private_key_hex=TEST_KEY,
    warning="kunci uji publik, hanya untuk parity test",
    cases=out)
json.dump(doc, open("golden_corpus.json", "w"), indent=2)
print(f"{len(out)} kasus ditulis, signer={wallet.address}")
names = [o["name"] for o in out]
assert len(set(names)) == len(names)
# deterministik: jalankan ulang harus identik
