#!/usr/bin/env python3
"""Parse the vs-ava raw benchmark outputs into median tables with provenance.
Usage: python3 parse_results.py [results_dir]"""
import os, re, sys, statistics, json

RD = sys.argv[1] if len(sys.argv) > 1 else os.path.join(os.path.dirname(__file__), "results")

def bench_rows(path):
    """name -> list of (ns_per_op, extra_tx_per_s or None)."""
    rows = {}
    if not os.path.exists(path): return rows
    for ln in open(path, errors="ignore"):
        m = re.match(r"^(Benchmark\S+?)-\d+\s+\d+\s+([\d.]+)\s+ns/op(.*)$", ln)
        if not m: continue
        name = re.sub(r"-\d+$", "", m.group(1))
        ns = float(m.group(2))
        txm = re.search(r"([\d.]+)\s+tx/s", m.group(3))
        rows.setdefault(name, []).append((ns, float(txm.group(1)) if txm else None))
    return rows

def med(xs): return statistics.median(xs) if xs else float("nan")

def summarize(path, label):
    rows = bench_rows(path)
    print(f"\n== {label} ({os.path.basename(path)}) ==")
    out = {}
    for name in sorted(rows):
        nss = [r[0] for r in rows[name]]
        txs = [r[1] for r in rows[name] if r[1] is not None]
        n = len(nss); mns = med(nss)
        line = f"  {name:<46} n={n:2d}  median {mns:>12.0f} ns/op"
        if txs: line += f"  median {med(txs):>14.0f} tx/s"
        else:   line += f"  = {1e9/mns:>10.1f} op/s"
        print(line)
        out[name] = {"n": n, "median_ns_per_op": round(mns, 1),
                     "median_tx_per_s": round(med(txs), 1) if txs else None,
                     "op_per_s": round(1e9/mns, 2) if mns == mns else None}
    return out

result = {}
result["insertchain_lux"]    = summarize(os.path.join(RD, "insertchain_lux_raw.txt"), "InsertChain — luxfi/evm (CGO=0)")
result["insertchain_coreth"] = summarize(os.path.join(RD, "insertchain_coreth_raw.txt"), "InsertChain — ava/coreth (CGO=1)")
result["blockstm_lux"]       = summarize(os.path.join(RD, "blockstm_lux_raw.txt"), "Block-STM parallel — luxfi/evm (CGO=0)")

# head-to-head ratio table (coreth as baseline) for the memdb rows both ran
lux, cor = result["insertchain_lux"], result["insertchain_coreth"]
if lux and cor:
    print("\n== InsertChain head-to-head (ns/op median; ratio = lux/coreth) ==")
    for b in ["empty", "valueTx", "ring200", "ring1000"]:
        k = f"BenchmarkInsertChain_{b}_memdb"
        if k in lux and k in cor:
            lv, cv = lux[k]["median_ns_per_op"], cor[k]["median_ns_per_op"]
            print(f"  {b:<10} lux {lv:>12.0f}  coreth {cv:>12.0f}  ratio {lv/cv:5.2f}x")

open(os.path.join(RD, "summary.json"), "w").write(json.dumps(result, indent=2))
print(f"\nwrote {os.path.join(RD, 'summary.json')}")
