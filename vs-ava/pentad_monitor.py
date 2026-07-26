#!/usr/bin/env python3
"""Read-only N-validator per-height COHERENCE monitor (NON-DESTRUCTIVE).

Pairs with a saturating loadgen (cchain_live.sh open-loop / tp_loadgen_open.py)
that supplies the hammer; this file only OBSERVES. It answers the owner's exact
deliverable: are all 5 validators converged on the SAME block at EVERY height
throughout the load — not merely at the latest tip.

Method (mirrors the block-walk in tp_loadgen_open.py's monitor, but cross-node):
  - every SAMPLE s, read each node's eth_blockNumber (tip) — records lag/catch-up.
  - walk every NEW confirmed height h (anchor..max_tip-CONFIRM) exactly once;
    fetch hash(h) from every node that HAS h; require all present hashes identical.
    A height where >=2 distinct hashes appear is a FORK (the real failure).
    Height skew (a node simply not there yet) is NOT a fork — it's lag, tracked
    separately via all5_at_final_within2.
Emits a per-poll log line + one final `VERDICT {json}` line. Same in-cluster
node/RPC access as the loadgens (luxd-i.luxd-headless...:PORT /v1/bc/C/rpc)."""
import os, json, time, http.client

_port = os.environ.get("PORT", "9640")
_ns   = os.environ.get("NS", "lux-testnet")
_n    = int(os.environ.get("NNODES", "5"))
_default = ",".join(f"luxd-{i}.luxd-headless.{_ns}.svc.cluster.local:{_port}" for i in range(_n))
NODES = [hp.rsplit(":", 1) for hp in os.environ.get("NODES", _default).split(",")]
HOST  = [h for h, _ in NODES]; PORT = [int(p) for _, p in NODES]; NN = len(NODES)
RPC_PATH = "/v1/bc/C/rpc"
DURATION = int(os.environ.get("DURATION", "260"))
SAMPLE   = float(os.environ.get("SAMPLE", "2.0"))
CONFIRM  = int(os.environ.get("CONFIRM_DEPTH", "2"))   # verify at tip-CONFIRM

def log(*a): print(f"[{time.strftime('%H:%M:%S')}]", *a, flush=True)
def rpc(i, method, params, timeout=5):
    c = http.client.HTTPConnection(HOST[i], PORT[i], timeout=timeout)
    try:
        c.request("POST", RPC_PATH, json.dumps({"jsonrpc":"2.0","id":1,"method":method,"params":params}), {"Content-Type":"application/json"})
        return json.loads(c.getresponse().read())
    finally:
        try: c.close()
        except Exception: pass
def h2i(x):
    try: return int(x, 16)
    except Exception: return None
def tip(i):
    try: return h2i(rpc(i, "eth_blockNumber", [])["result"])
    except Exception: return None
def hashat(i, h):
    try:
        b = rpc(i, "eth_getBlockByNumber", [hex(h), False]).get("result")
        return b["hash"] if b else None
    except Exception: return None

def main():
    log(f"pentad monitor: {NN} nodes {[h.split('.')[0] for h in HOST]} conf_depth={CONFIRM} dur={DURATION}s sample={SAMPLE}s")
    t0 = time.time()
    tips0 = [tip(i) for i in range(NN)]
    log("baseline tips", tips0)
    present0 = [t for t in tips0 if t is not None]
    anchor = (max(present0) if present0 else 0) - 1     # verify heights produced from load onward
    checked = max(0, anchor)
    verified = {}          # h -> agreed hash
    forks = []             # (h, {node: hash})
    polls = 0; fork_polls = 0; unreachable_polls = 0
    end = t0 + DURATION
    while time.time() < end:
        time.sleep(SAMPLE); polls += 1
        tips = [tip(i) for i in range(NN)]
        present = [t for t in tips if t is not None]
        if not present:
            unreachable_polls += 1; log(f"poll {polls} ALL NODES UNREACHABLE"); continue
        mx = max(present); mn = min(present); conf = mx - CONFIRM
        newly = 0; fork_here = 0
        for h in range(checked + 1, conf + 1):
            hs = {}
            for i in range(NN):
                hh = hashat(i, h)
                if hh: hs[i] = hh
            distinct = set(hs.values())
            if len(distinct) > 1:
                forks.append((h, hs)); fork_here = 1
                log(f"  *** FORK @ height {h}: " + ", ".join(f"n{i}={v[:12]}" for i, v in hs.items()))
            elif distinct:
                verified[h] = next(iter(distinct)); newly += 1
            checked = h
        if fork_here: fork_polls += 1
        log(f"poll {polls} tips={tips} max={mx} min={mn} lag={mx-mn} verified_upto={checked} new={newly} total_verified={len(verified)} forks={len(forks)}")
    tipsF = [tip(i) for i in range(NN)]
    presentF = [t for t in tipsF if t is not None]
    fmax = max(presentF) if presentF else 0
    adv = [ (tipsF[i]-tips0[i]) if (tipsF[i] is not None and tips0[i] is not None) else None for i in range(NN)]
    v = {
        "nodes": NN, "duration_s": round(time.time()-t0, 1), "polls": polls,
        "unreachable_polls": unreachable_polls,
        "baseline_tips": tips0, "final_tips": tipsF, "final_max": fmax,
        "advance_per_node": adv,
        "heights_verified": len(verified),
        "verified_range": [min(verified) if verified else None, max(verified) if verified else None],
        "forks": len(forks), "fork_polls": fork_polls,
        "fork_free": len(forks) == 0,
        "all5_at_final_within2": all((t is not None and fmax - t <= 2) for t in tipsF),
        "tip_advanced": (fmax - (max(present0) if present0 else 0)) > 0,
    }
    log("VERDICT " + json.dumps(v))
    if forks:
        log("FORKS " + json.dumps([{"h": h, "hashes": {str(i): hh[:18] for i, hh in d.items()}} for h, d in forks[:25]]))

if __name__ == "__main__":
    main()
