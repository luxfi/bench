#!/usr/bin/env python3
"""Live C-Chain throughput — OPEN-LOOP (saturating) variant of tp_loadgen.py.

The closed-loop sender advances one nonce per account only after the prior tx is
accepted, so the offered rate is coupled to the mined rate and cannot saturate the
block builder. This variant keeps a deep PIPELINE of consecutive-nonce txs in flight
per account (fire-and-forget, refreshed against the mined nonce), flooding the mempool
so the ONLY throttle left is the C-Chain block builder + fee/gas-target. That pins the
true saturated mined ceiling and the max block fullness. Same monitor/MEASURE contract."""
import os, json, time, random, threading, http.client
from eth_account import Account

_port = os.environ.get("PORT", "9640"); _ns = os.environ.get("NS", "lux-testnet")
_n = int(os.environ.get("NNODES", "5"))
_default = ",".join(f"luxd-{i}.luxd-headless.{_ns}.svc.cluster.local:{_port}" for i in range(_n))
NODES = [hp.rsplit(":", 1) for hp in os.environ.get("NODES", _default).split(",")]
HOST = [h for h, _ in NODES]; PORT = [int(p) for _, p in NODES]; NN = len(NODES)
RPC_PATH = "/v1/bc/C/rpc"
CHAINID  = int(os.environ.get("CHAINID", "96368"))
N_KEYS   = int(os.environ.get("N_KEYS", "60"))
FUND_LUX = int(os.environ.get("FUND_LUX", "5"))
PIPELINE = int(os.environ.get("PIPELINE", "64"))    # in-flight nonces per account
WINDOW   = int(os.environ.get("WINDOW", "90"))
SAMPLE   = float(os.environ.get("SAMPLE", "3.0"))
SEND_TIMEOUT = float(os.environ.get("SEND_TIMEOUT", "3.0"))

def log(*a): print(f"[{time.strftime('%H:%M:%S')}]", *a, flush=True)
FUNDER_PK = os.environ["FUNDER_PK"].strip()
if not FUNDER_PK.startswith("0x"): FUNDER_PK = "0x" + FUNDER_PK
FUNDER = Account.from_key(FUNDER_PK); FUNDER_KEY = FUNDER.key; FUNDER_ADDR = FUNDER.address
del FUNDER_PK
log("funder", FUNDER_ADDR, "| OPEN-LOOP pipeline", PIPELINE, "| chainId", CHAINID)

random.seed(0xBEEF)
KEYS = [Account.create().key for _ in range(N_KEYS)]
ADDR = [Account.from_key(k).address for k in KEYS]
STOP = threading.Event(); SENT = [0]; SENT_LOCK = threading.Lock()
MINED = [0] * N_KEYS

def raw_of(s): return getattr(s, "raw_transaction", None) or getattr(s, "rawTransaction")
def sign_raw(key, nonce, gp, to, value=0, gas=21000):
    return "0x" + raw_of(Account.sign_transaction({"nonce": nonce, "gasPrice": gp, "gas": gas, "to": to, "value": value, "data": b"", "chainId": CHAINID}, key)).hex()
def rpc_on(conn, method, params, rid=1):
    conn.request("POST", RPC_PATH, json.dumps({"jsonrpc": "2.0", "id": rid, "method": method, "params": params}), {"Content-Type": "application/json"})
    return json.loads(conn.getresponse().read())
def rpc(i, method, params, timeout=8):
    c = http.client.HTTPConnection(HOST[i], PORT[i], timeout=timeout)
    try: return rpc_on(c, method, params)
    finally:
        try: c.close()
        except Exception: pass
def h2i(x):
    try: return int(x, 16)
    except Exception: return None
def get_gas_price():
    try: bf = h2i(rpc(0, "eth_getBlockByNumber", ["latest", False])["result"].get("baseFeePerGas", "0x0")) or 0
    except Exception: bf = 0
    return max(bf * 8, 100_000_000_000)

def fund_all():
    gp = get_gas_price()
    base = h2i(rpc(0, "eth_getTransactionCount", [FUNDER_ADDR, "pending"], timeout=6).get("result"))
    log(f"funding {N_KEYS} keys x {FUND_LUX} LUX nonce={base} gp={gp/1e9:.0f}gwei")
    val = FUND_LUX * 10**18; W = 60
    conn = http.client.HTTPConnection(HOST[0], PORT[0], timeout=8)
    ni = 0; t0 = time.time(); last_prog = t0; last_mined = base
    while True:
        try: mined = h2i(rpc(0, "eth_getTransactionCount", [FUNDER_ADDR, "latest"])["result"])
        except Exception: mined = last_mined
        if mined is None: mined = last_mined
        if mined > last_mined: last_prog = time.time(); last_mined = mined
        if ni >= N_KEYS and mined >= base + N_KEYS: break
        while ni < N_KEYS and (base + ni - mined) < W:
            try: r = rpc_on(conn, "eth_sendRawTransaction", [sign_raw(FUNDER_KEY, base + ni, gp, ADDR[ni], val)])
            except Exception:
                try: conn.close()
                except Exception: pass
                conn = http.client.HTTPConnection(HOST[0], PORT[0], timeout=8); break
            s = str(r).lower()
            if "result" in r or "already known" in s or "nonce too low" in s: ni += 1
            elif "underpriced" in s: gp = int(gp * 1.6); break
            elif "full" in s or "limit" in s: break
            else: ni += 1
        if time.time() - last_prog > 25: gp = int(gp * 1.6); ni = max(0, mined - base); last_prog = time.time()
        if time.time() - t0 > 240: log("funding timeout"); break
        time.sleep(0.4)
    funded = [i for i in range(N_KEYS) if (h2i(rpc(0, "eth_getBalance", [ADDR[i], "latest"]).get("result")) or 0) > 0]
    log(f"funded {len(funded)}/{N_KEYS} in {time.time()-t0:.0f}s")
    return funded

def refresher(funded):
    """Every 2s, refresh each account's mined nonce so pipeliners know how far to lead."""
    while not STOP.is_set():
        for i in funded:
            try: MINED[i] = h2i(rpc(0, "eth_getTransactionCount", [ADDR[i], "latest"], timeout=6).get("result")) or MINED[i]
            except Exception: pass
            if STOP.is_set(): return
        time.sleep(2)

def pipeliner(i, gp):
    """Fire-and-forget consecutive nonces, staying up to PIPELINE ahead of mined."""
    conns = {}
    def conn(j):
        c = conns.get(j)
        if c is None: c = http.client.HTTPConnection(HOST[j], PORT[j], timeout=SEND_TIMEOUT); conns[j] = c
        return c
    to = ADDR[i]; key = KEYS[i]; nxt = MINED[i]
    while not STOP.is_set():
        if nxt - MINED[i] >= PIPELINE:
            time.sleep(0.02); nxt = max(nxt, MINED[i]); continue
        j = random.randint(0, NN - 1)
        try: raw = sign_raw(key, nxt, gp, to)
        except Exception: break
        try:
            resp = rpc_on(conn(j), "eth_sendRawTransaction", [raw], rid=i)
            if "result" in resp:
                with SENT_LOCK: SENT[0] += 1
                nxt += 1
            else:
                m = str(resp.get("error", {}).get("message", "")).lower()
                if "nonce too low" in m: nxt = max(nxt + 1, MINED[i])
                elif "already known" in m or "replacement" in m or "known transaction" in m: nxt += 1
                elif "underpriced" in m or "full" in m or "limit" in m: time.sleep(0.02)
                else: nxt += 1
        except (http.client.HTTPException, OSError):
            try: conns.pop(j).close()
            except Exception: pass
            time.sleep(0.02)

def block_hdr(i, num):
    try:
        b = rpc(i, "eth_getBlockByNumber", [hex(num), False]).get("result")
        if not b: return None
        return (len(b["transactions"]), h2i(b["gasUsed"]), h2i(b["gasLimit"]), h2i(b.get("baseFeePerGas", "0x0")) or 0, b["hash"])
    except Exception: return None

def monitor(nsenders):
    def tip(i=0):
        try: return h2i(rpc(i, "eth_blockNumber", [])["result"])
        except Exception: return None
    while tip() is None and not STOP.is_set(): time.sleep(1)
    t0 = time.time(); b0 = tip(); last = b0; sent0 = SENT[0]
    log(f"=== OPEN-LOOP MEASURE START t0={t0:.1f} b0={b0} window={WINDOW}s pipeline={PIPELINE} ===")
    tx_sum = 0; gas_sum = 0; gl = None; bfs = []; nb = 0; ints = []; last_bt = t0
    peak_txblk = 0; peak_full = 0.0
    end = t0 + WINDOW
    while time.time() < end and not STOP.is_set():
        time.sleep(SAMPLE); cur = tip()
        if cur is None: continue
        while last < cur:
            last += 1; hdr = block_hdr(0, last)
            if hdr is None: continue
            txc, gu, glim, bf, _ = hdr
            tx_sum += txc; gas_sum += gu; gl = glim; bfs.append(bf); nb += 1
            peak_txblk = max(peak_txblk, txc); peak_full = max(peak_full, 100 * gu / (glim or 1))
            now = time.time(); ints.append(now - last_bt); last_bt = now
        off = SENT[0] - sent0; el = time.time() - t0
        log(f"PROGRESS t={el:.0f}s tip={cur} blocks={nb} mined_tx={tx_sum} offered={off} "
            f"off_tps={off/el:.1f} mined_tps={tx_sum/el:.1f} peak_tx/blk={peak_txblk} "
            f"fullness={100*gas_sum/((gl or 1)*max(nb,1)):.2f}% peak_full={peak_full:.2f}%")
    t1 = time.time(); el = t1 - t0; offered = SENT[0] - sent0
    conf = max(b0 + 1, last - 2); hashes = {i: (block_hdr(i, conf)[4][:12] if block_hdr(i, conf) else "x") for i in range(NN)}
    coherent = len(set(v for v in hashes.values() if v != "x")) <= 1
    m = {"mode": "open-loop", "window_s": round(el, 2), "b0": b0, "b1": last, "blocks": nb,
         "blocks_per_s": round(nb / el, 4) if el else 0, "pipeline": PIPELINE,
         "offered_tx": offered, "offered_tps": round(offered / el, 2) if el else 0,
         "mined_tx": tx_sum, "mined_tps": round(tx_sum / el, 2) if el else 0,
         "avg_tx_per_block": round(tx_sum / nb, 2) if nb else 0, "peak_tx_per_block": peak_txblk,
         "gas_used_sum": gas_sum, "gas_per_s": round(gas_sum / el, 1) if el else 0, "gas_limit": gl,
         "avg_gas_per_block": round(gas_sum / nb, 1) if nb else 0,
         "avg_fullness_pct": round(100 * gas_sum / ((gl or 1) * nb), 3) if nb else 0,
         "peak_fullness_pct": round(peak_full, 3),
         "base_fee_gwei_last": round((bfs[-1] / 1e9), 3) if bfs else 0,
         "block_interval_ms_mean": round(1000 * sum(ints) / len(ints), 1) if ints else 0,
         "coherent": coherent, "conf_height": conf, "node_hashes": hashes, "senders": nsenders, "nodes": NN}
    log("MEASURE " + json.dumps(m)); STOP.set()

def main():
    funded = [k for k in fund_all() if k < N_KEYS]
    if len(funded) < 8: log(f"FATAL only {len(funded)} funded"); return
    gp = get_gas_price()
    log(f"=== OPEN-LOOP SATURATE: {len(funded)} accts x pipeline {PIPELINE}, measuring {WINDOW}s (gp={gp/1e9:.0f}gwei) ===")
    for i in funded: MINED[i] = 0
    threading.Thread(target=refresher, args=(funded,), daemon=True).start()
    for i in funded: threading.Thread(target=pipeliner, args=(i, gp), daemon=True).start()
    mon = threading.Thread(target=monitor, args=(len(funded),), daemon=True); mon.start()
    mon.join(timeout=WINDOW + 60); STOP.set(); log("=== loadgen done ===")

if __name__ == "__main__":
    try: main()
    finally: STOP.set()
