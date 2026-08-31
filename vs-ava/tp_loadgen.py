#!/usr/bin/env python3
"""Live C-Chain throughput measurement — in-cluster, NON-DESTRUCTIVE.

Funds N keys, fires a fixed set of steady senders, and runs a monitor thread that
samples the chain over a wall-clock WINDOW to measure the REAL mined ceiling:
  offered tx/s  (accepted eth_sendRawTransaction, shared counter)
  mined  tx/s   (sum of block txCount over the window / wall-clock elapsed)
  blocks/s, gas/block, gas/s, block fullness = gasUsed / gasLimit
  cross-node coherence at a confirmed height (all live nodes hold the same hash)

Emits PROGRESS lines during the run and one final `MEASURE {json}` line the
orchestrator parses. Derived from the proven nova_loadgen.py funding+sender machinery
(no node drops here — this only measures steady-state throughput and the fee/gas-target
ceiling; the consensus-liveness proof is nova_proof.sh)."""
import os, json, time, random, threading, http.client
from eth_account import Account

_port = os.environ.get("PORT", "9640")
_ns   = os.environ.get("NS", "lux-testnet")
_n    = int(os.environ.get("NNODES", "5"))
_default = ",".join(f"luxd-{i}.luxd-headless.{_ns}.svc.cluster.local:{_port}" for i in range(_n))
NODES = [hp.rsplit(":", 1) for hp in os.environ.get("NODES", _default).split(",")]
HOST  = [h for h, _ in NODES]; PORT = [int(p) for _, p in NODES]; NN = len(NODES)
RPC_PATH = "/v1/chain/C/rpc"
CHAINID  = int(os.environ.get("CHAINID", "96368"))
N_KEYS   = int(os.environ.get("N_KEYS", "60"))
FUND_LUX = int(os.environ.get("FUND_LUX", "5"))
SENDERS  = int(os.environ.get("SENDERS", "24"))
WINDOW   = int(os.environ.get("WINDOW", "90"))      # measurement wall-clock window
SAMPLE   = float(os.environ.get("SAMPLE", "3.0"))
SEND_TIMEOUT = float(os.environ.get("SEND_TIMEOUT", "3.0"))

def log(*a): print(f"[{time.strftime('%H:%M:%S')}]", *a, flush=True)
FUNDER_PK = os.environ["FUNDER_PK"].strip()
if not FUNDER_PK.startswith("0x"): FUNDER_PK = "0x" + FUNDER_PK
FUNDER = Account.from_key(FUNDER_PK); FUNDER_KEY = FUNDER.key; FUNDER_ADDR = FUNDER.address
del FUNDER_PK
log("funder", FUNDER_ADDR, "| nodes:", [h.split('.')[0] for h in HOST], "| chainId", CHAINID)

random.seed(0xC0FFEE)
KEYS  = [Account.create().key for _ in range(N_KEYS)]
ADDR  = [Account.from_key(k).address for k in KEYS]
NONCE = [0] * N_KEYS
STOP  = threading.Event()
SENT  = [0]                                          # shared offered counter
SENT_LOCK = threading.Lock()

def raw_of(s): return getattr(s, "raw_transaction", None) or getattr(s, "rawTransaction")
def sign_raw(key, nonce, gp, to, value=0, gas=21000):
    tx = {"nonce": nonce, "gasPrice": gp, "gas": gas, "to": to, "value": value, "data": b"", "chainId": CHAINID}
    return "0x" + raw_of(Account.sign_transaction(tx, key)).hex()
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
    try: gp = h2i(rpc(0, "eth_gasPrice", [])["result"]) or 0
    except Exception: gp = 0
    return max(bf * 8, gp * 2, 100_000_000_000)

def fund_all():
    gp = get_gas_price()
    base = h2i(rpc(0, "eth_getTransactionCount", [FUNDER_ADDR, "pending"], timeout=6).get("result"))
    log(f"funding {N_KEYS} keys x {FUND_LUX} LUX, nonce={base}, gp={gp/1e9:.0f}gwei")
    val = FUND_LUX * 10**18; WINDOW_F = 60
    conn = http.client.HTTPConnection(HOST[0], PORT[0], timeout=8)
    next_i = 0; t0 = time.time(); last_prog = t0; last_mined = base
    while True:
        try: mined = h2i(rpc(0, "eth_getTransactionCount", [FUNDER_ADDR, "latest"])["result"])
        except Exception: mined = last_mined
        if mined is None: mined = last_mined
        if mined > last_mined: last_prog = time.time(); last_mined = mined
        if next_i >= N_KEYS and mined >= base + N_KEYS: break
        while next_i < N_KEYS and (base + next_i - mined) < WINDOW_F:
            raw = sign_raw(FUNDER_KEY, base + next_i, gp, ADDR[next_i], val)
            try: r = rpc_on(conn, "eth_sendRawTransaction", [raw])
            except Exception:
                try: conn.close()
                except Exception: pass
                conn = http.client.HTTPConnection(HOST[0], PORT[0], timeout=8); break
            s = str(r).lower()
            if "result" in r or "already known" in s or "nonce too low" in s: next_i += 1
            elif "underpriced" in s: gp = int(gp * 1.6); break
            elif "full" in s or "limit" in s: break
            else: next_i += 1
        if time.time() - last_prog > 25: gp = int(gp * 1.6); next_i = max(0, mined - base); last_prog = time.time()
        if time.time() - t0 > 240: log("funding timeout"); break
        time.sleep(0.4)
    funded = [i for i in range(N_KEYS) if (h2i(rpc(0, "eth_getBalance", [ADDR[i], "latest"]).get("result")) or 0) > 0]
    log(f"funded {len(funded)}/{N_KEYS} in {time.time()-t0:.0f}s")
    return funded

def sender(i, gp):
    conns = {}
    def conn(j):
        c = conns.get(j)
        if c is None: c = http.client.HTTPConnection(HOST[j], PORT[j], timeout=SEND_TIMEOUT); conns[j] = c
        return c
    to = ADDR[i]; key = KEYS[i]
    while not STOP.is_set():
        j = random.randint(0, NN - 1)
        try: raw = sign_raw(key, NONCE[i], gp, to)
        except Exception: break
        try: resp = rpc_on(conn(j), "eth_sendRawTransaction", [raw], rid=i)
        except (http.client.HTTPException, OSError):
            try: conns.pop(j).close()
            except Exception: pass
            time.sleep(0.05); continue
        if "result" in resp:
            NONCE[i] += 1
            with SENT_LOCK: SENT[0] += 1
        else:
            m = str(resp.get("error", {}).get("message", "")).lower()
            if "nonce too low" in m:
                pn = h2i(rpc(0, "eth_getTransactionCount", [to, "pending"]).get("result"))
                if pn is not None: NONCE[i] = pn
            elif "already known" in m or "replacement" in m: NONCE[i] += 1
            else: time.sleep(0.05)

def block_hdr(i, num):
    """(txcount, gasUsed, gasLimit, baseFee, hash) for block `num` on node i, or None."""
    try:
        b = rpc(i, "eth_getBlockByNumber", [hex(num), False]).get("result")
        if not b: return None
        return (len(b["transactions"]), h2i(b["gasUsed"]), h2i(b["gasLimit"]),
                h2i(b.get("baseFeePerGas", "0x0")) or 0, b["hash"])
    except Exception:
        return None

def monitor(funded_n):
    """Sample node-0 across the window; accumulate per-block mined tx + gas from the
    canonical block sequence. Coherence checked at the end at a confirmed height."""
    def tip(i=0):
        try: return h2i(rpc(i, "eth_blockNumber", [])["result"])
        except Exception: return None
    while tip() is None and not STOP.is_set(): time.sleep(1)
    t0 = time.time(); b0 = tip(); last = b0
    log(f"=== MEASURE START t0={t0:.1f} b0={b0} window={WINDOW}s senders={funded_n} ===")
    sent0 = SENT[0]
    tx_sum = 0; gas_sum = 0; gaslimit = None; basefees = []; nblocks = 0
    intervals = []; last_bt = t0
    end = t0 + WINDOW
    while time.time() < end and not STOP.is_set():
        time.sleep(SAMPLE)
        cur = tip()
        if cur is None: continue
        while last < cur:
            last += 1
            hdr = block_hdr(0, last)
            if hdr is None: continue
            txc, gu, gl, bf, _ = hdr
            tx_sum += txc; gas_sum += gu; gaslimit = gl; basefees.append(bf); nblocks += 1
            now = time.time(); intervals.append(now - last_bt); last_bt = now
        off = SENT[0] - sent0; el = time.time() - t0
        log(f"PROGRESS t={el:.0f}s tip={cur} blocks={nblocks} mined_tx={tx_sum} "
            f"offered={off} off_tps={off/el:.1f} mined_tps={tx_sum/el:.1f} "
            f"gasUsed_sum={gas_sum} fullness={100*gas_sum/((gaslimit or 1)*max(nblocks,1)):.2f}%")
    t1 = time.time(); b1 = last; el = t1 - t0
    offered = SENT[0] - sent0
    # coherence at a confirmed height (b1-2): all live nodes must hold the same hash
    conf = max(b0 + 1, b1 - 2); hashes = {}
    for i in range(NN):
        h = block_hdr(i, conf)
        hashes[i] = (h[4][:12] if h else "x")
    coherent = len(set(v for v in hashes.values() if v != "x")) <= 1
    m = {
        "window_s": round(el, 2), "b0": b0, "b1": b1, "blocks": nblocks,
        "blocks_per_s": round(nblocks / el, 4) if el else 0,
        "offered_tx": offered, "offered_tps": round(offered / el, 2) if el else 0,
        "mined_tx": tx_sum, "mined_tps": round(tx_sum / el, 2) if el else 0,
        "avg_tx_per_block": round(tx_sum / nblocks, 2) if nblocks else 0,
        "gas_used_sum": gas_sum, "gas_per_s": round(gas_sum / el, 1) if el else 0,
        "gas_limit": gaslimit, "avg_gas_per_block": round(gas_sum / nblocks, 1) if nblocks else 0,
        "avg_fullness_pct": round(100 * gas_sum / ((gaslimit or 1) * nblocks), 3) if nblocks else 0,
        "base_fee_gwei_last": round((basefees[-1] / 1e9), 3) if basefees else 0,
        "block_interval_ms_mean": round(1000 * sum(intervals) / len(intervals), 1) if intervals else 0,
        "coherent": coherent, "conf_height": conf, "node_hashes": hashes,
        "senders": funded_n, "nodes": NN,
    }
    log("MEASURE " + json.dumps(m))
    STOP.set()

def main():
    funded = [k for k in fund_all() if k < N_KEYS]
    if len(funded) < SENDERS:
        log(f"FATAL only {len(funded)} funded < {SENDERS} senders"); return
    gp = get_gas_price()
    log(f"=== STEADY LOAD: {SENDERS} senders, measuring {WINDOW}s (gp={gp/1e9:.0f}gwei) ===")
    ths = [threading.Thread(target=sender, args=(funded[n], gp), daemon=True) for n in range(SENDERS)]
    for th in ths: th.start()
    mon = threading.Thread(target=monitor, args=(SENDERS,), daemon=True); mon.start()
    mon.join(timeout=WINDOW + 60)
    STOP.set()
    log("=== loadgen done ===")

if __name__ == "__main__":
    try: main()
    finally: STOP.set()
