"""1000 payload acak BERSEED (reproducible) dari SDK Python resmi, untuk parity Go vs Python.
Semua nilai float dibangkitkan sebagai desimal <=8 digit yang lolos float_to_wire SDK."""
import json, random, hashlib
from decimal import Decimal
import msgpack
from eth_account import Account
from eth_account.messages import encode_typed_data
from eth_utils import keccak
from hyperliquid.utils.signing import (action_hash, construct_phantom_agent, l1_payload,
    sign_l1_action, order_request_to_order_wire, order_wires_to_order_action, address_to_bytes)
from hyperliquid.utils.types import Cloid

SEED = 20260928
rng = random.Random(SEED)
KEY = "0x" + "11" * 32
wallet = Account.from_key(KEY)

def rfloat(max_int_digits, max_dec):
    ip = rng.randint(0, 10 ** rng.randint(0, max_int_digits) - 1)
    dp = rng.randint(0, max_dec)
    s = f"{ip}.{rng.randint(0, 10 ** dp - 1):0{dp}d}" if dp else f"{ip}"
    v = float(Decimal(s))
    return v if v > 0 else 1.0

def rcloid(): return Cloid.from_str("0x" + "".join(rng.choice("0123456789abcdef") for _ in range(32)))

def rorder():
    asset = rng.choice([0, 1, 2, 5, 127, 128, 255, 256, 1000, 65535, 65536, 100000, rng.randint(0, 200000)])
    if rng.random() < 0.25:
        px = rfloat(7, 8)
        ot = {"trigger": {"isMarket": rng.random() < .5, "triggerPx": px, "tpsl": rng.choice(["sl", "tp"])}}
    else:
        ot = {"limit": {"tif": rng.choice(["Gtc", "Ioc", "Alo"])}}
    o = {"coin": "X", "is_buy": rng.random() < .5, "sz": rfloat(8, 8), "limit_px": rfloat(7, 8),
         "order_type": ot, "reduce_only": rng.random() < .3,
         "cloid": rcloid() if rng.random() < .4 else None}
    return o, asset

def ospec(o, a):
    ot = o["order_type"]
    otj = ({"limit": {"tif": ot["limit"]["tif"]}} if "limit" in ot else
           {"trigger": {"is_market": ot["trigger"]["isMarket"], "trigger_px": ot["trigger"]["triggerPx"], "tpsl": ot["trigger"]["tpsl"]}})
    return {"asset": a, "is_buy": o["is_buy"], "sz": o["sz"], "limit_px": o["limit_px"], "reduce_only": o["reduce_only"],
            "order_type": otj, "cloid": o["cloid"].to_raw() if o["cloid"] else None}

def build(kind):
    """-> (action, spec). Meniru konstruksi action SDK untuk tiap jenis."""
    if kind == "order":
        pairs = [rorder() for _ in range(rng.choice([1, 1, 1, 2, 3]))]
        grouping = rng.choice(["na", "na", "normalTpsl", "positionTpsl"])
        builder = {"b": "0x" + "".join(rng.choice("0123456789abcdefABCDEF") for _ in range(40)), "f": rng.randint(0, 300)} if rng.random() < .1 else None
        wires = [order_request_to_order_wire(o, a) for o, a in pairs]
        # Meniru Exchange.bulk_orders: alamat builder di-lowercase SEBELUM action dibangun.
        b_wire = {"b": builder["b"].lower(), "f": builder["f"]} if builder else None
        action = order_wires_to_order_action(wires, b_wire, grouping)
        return action, {"kind": "order", "orders": [ospec(o, a) for o, a in pairs], "grouping": grouping, "builder": builder}
    if kind == "batchModify":
        mods_a, mods_s = [], []
        for _ in range(rng.choice([1, 1, 2])):
            o, a = rorder()
            oid = rng.randint(1, 2**40) if rng.random() < .6 else rcloid()
            raw = oid.to_raw() if isinstance(oid, Cloid) else oid
            mods_a.append({"oid": raw, "order": order_request_to_order_wire(o, a)})
            mods_s.append({"oid": raw, "order": ospec(o, a)})
        return {"type": "batchModify", "modifies": mods_a}, {"kind": "batchModify", "modifies": mods_s}
    if kind == "cancel":
        cs = [(rng.choice([0, 1, 127, 128, 256, 70000, rng.randint(0, 200000)]), rng.randint(1, 2**40)) for _ in range(rng.choice([1, 1, 2, 3]))]
        return ({"type": "cancel", "cancels": [{"a": a, "o": o} for a, o in cs]},
                {"kind": "cancel", "cancels": [{"asset": a, "oid": o} for a, o in cs]})
    cs = [(rng.choice([0, 1, 127, 128, 256, 70000, rng.randint(0, 200000)]), rcloid().to_raw()) for _ in range(rng.choice([1, 1, 2, 3]))]
    return ({"type": "cancelByCloid", "cancels": [{"asset": a, "cloid": c} for a, c in cs]},
            {"kind": "cancelByCloid", "cancels": [{"asset": a, "cloid": c} for a, c in cs]})

out = []
for i in range(1000):
    kind = rng.choices(["order", "batchModify", "cancel", "cancelByCloid"], weights=[55, 20, 12, 13])[0]
    action, spec = build(kind)
    nonce = rng.choice([1, rng.randint(1, 2**40), rng.randint(1_700_000_000_000, 1_800_000_000_000), 2**63 - 1, 2**64 - 1])
    vault = "0x" + "".join(rng.choice("0123456789abcdef") for _ in range(40)) if rng.random() < .2 else None
    exp = rng.randint(1_700_000_000_000, 1_800_000_000_000) if rng.random() < .2 else None
    mainnet = rng.random() < .5
    packed = msgpack.packb(action)
    conn = action_hash(action, vault, nonce, exp)
    agent = construct_phantom_agent(conn, mainnet)
    sg = encode_typed_data(full_message=l1_payload(agent))
    digest = keccak(b"\x19" + sg.version + sg.header + sg.body)
    sig = sign_l1_action(wallet, action, vault, nonce, exp, mainnet)
    vb = b"\x00" if vault is None else b"\x01" + address_to_bytes(vault)
    if exp is not None: vb += b"\x00" + exp.to_bytes(8, "big")
    out.append(dict(name=f"rand_{i:04d}", note=f"seed={SEED}", is_mainnet=mainnet, nonce=nonce, vault=vault,
        expires_after=exp, spec=spec, action_json=json.loads(json.dumps(action)),
        trace=dict(msgpack_hex=packed.hex(), msgpack_sha256=hashlib.sha256(packed).hexdigest(),
            nonce_hex=nonce.to_bytes(8, "big").hex(), vault_expires_suffix_hex=vb.hex(),
            connection_id_hex=conn.hex(), phantom_source=agent["source"], eip712_digest_hex=digest.hex(),
            signature=dict(r=sig["r"], s=sig["s"], v=sig["v"]))))
json.dump(dict(generated_by="hyperliquid-python-sdk 0.24.0", seed=SEED, signer_address=wallet.address,
               test_private_key_hex=KEY, cases=out), open("golden_random_1000.json", "w"))
print(len(out), "kasus acak, seed", SEED)
