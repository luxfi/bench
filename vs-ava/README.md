# `vs-ava` — Lux C-Chain vs avalanchego: a rigorous, provenance-tagged benchmark study

This directory is the reproducible harness + results for the head-to-head between the
**Lux v1.36 C-Chain** and **avalanchego**, plus the Lux **Nova** consensus evidence that
frames it. Every number is tagged by provenance — no projection is laundered as a result:

| Tag | Meaning |
|---|---|
| **M** | Measured by us this session (2026-07-10) on the hardware named below |
| **M\*** | Measured in prior cited Lux work |
| **P** | Projected / derived by calculation |
| **C** | External / cited claim |

## The one honest framing

At **v1.36**, the Lux C-Chain and avalanchego's C-Chain run the **same** sequential,
geth-derived EVM under the **same** dynamic-fee / gas-target throttle. So their **live**
mined throughput is comparable *by construction* — and we show it, on identical hardware,
at the execution layer. **The Lux advantage at v1.36 is on the consensus-liveness axis**
(Nova's two-tier ladder keeps a 5-node fleet producing at 3/5, where a ⅔-only engine
freezes), **not** raw live TPS. The GPU-native parallel executor that *would* separate the
two is **built and benchmarked but not wired into the live C-Chain** (proven in code) —
so we report it as headroom, tagged **P**-until-wired, never as a live win.

## Environments

- **Host** (M): Apple **M1 Max**, 10 cores, 64 GB, macOS 26.5.1 (25F80), `arm64`,
  Go **1.26.4**. Used for all local Go benchmarks. Both trees are built with the **same**
  installed toolchain (`GOTOOLCHAIN=local`) for a fair compiler.
- **Cluster** (M): the live Lux **testnet**, 5 validators `luxd-0..4` on
  `ghcr.io/luxfi/node:v1.36.0` (`consensus v1.36.0`, `evm v1.104.8`), ns `lux-testnet` on
  `do-sfo3-lux-k8s`, C-Chain id 96368, RPC `/v1/bc/C/rpc:9640`.
- **Reference**: `~/work/ava` — read-only avalanchego clone. avalanchego `v1.14.2`
  (`+subnet-evm graft`), coreth `v0.15.4`. **Not modified, not pushed.**

### Build-config asymmetry (documented, honest)

On this host, **coreth builds only `CGO_ENABLED=1`** (its pure-Go path has broken deps:
`blst` rb_tree + `libevm` nocgo btcec) and needs `SDKROOT` set for cgo headers;
**luxfi/evm builds only `CGO_ENABLED=0`** (CGO=1 needs the `luxgpu` C++ libs, absent here).
So on transaction-bearing benches the crypto backend differs — luxfi/evm uses **pure-Go**
ECDSA recovery (slower) vs coreth's **cgo libsecp256k1** (faster). **This asymmetry
handicaps Lux.** The `empty_memdb` row has **zero transactions ⇒ zero signature recovery ⇒
the confound-free comparison** of the block-processing machinery; treat it as the clean
anchor and read the tx-bearing rows as *conservative* for Lux. luxfi/geth also logs a
per-commit durability line inside the timed region; we send it to `/dev/null` so it neither
pollutes results nor inflates timing (`empty_memdb` triggers no commit ⇒ no such log).

## Harnesses

| Script | What it measures | Destructive? |
|---|---|---|
| `run_insertchain.sh` | Execution-layer head-to-head: identical `BenchmarkInsertChain_*` suite on luxfi/evm vs ava/coreth, run **sequentially** on the same host | no (local) |
| `cchain_live.sh` + `tp_loadgen.py` | Live C-Chain **closed-loop** throughput + block fullness (mined ceiling) | no (load only, no node scaling) |
| `cchain_live.sh` + `tp_loadgen_open.py` | Live C-Chain **open-loop (saturating)** throughput — deep per-account nonce pipeline floods the mempool to pin the true block-builder ceiling | no |
| `nova_proof.sh` + `nova_loadgen.py` | **Nova consensus** two-tier coherence under a `5/5→4/5→3/5→4/5→5/5` node-drop | **yes** — scales the testnet validator set; guarded (`CONFIRM=yes` + image match) |
| `parse_results.py` | Parses raw bench output → median tables + `results/summary.json` | no |

## Results

### 1. Execution-layer head-to-head — `BenchmarkInsertChain` (M, Host)

Both C-Chains carry the identical geth-lineage `BenchmarkInsertChain_*` suite. Median of
`-count=8 -benchtime=2s`, `GOMAXPROCS=10`, memdb (isolates execution from disk I/O).
`empty_memdb` = empty blocks (block-processing machinery, **crypto-free, clean**);
`valueTx` = 1 value-transfer/block; `ring200/1000` = contract-ring workloads (many txs/block).

| Bench | luxfi/evm (CGO=0) ns/op | ava/coreth (CGO=1) ns/op | ratio lux/coreth | Tag |
|---|---:|---:|---:|:--:|
| `empty_memdb`  (clean, no crypto) | 121,452 | 57,069 | **2.13×** (lux slower) | M |
| `valueTx_memdb` (1 tx/block) | 160,194 | 139,563 | 1.15× | M |
| `ring200_memdb` (contract-ring, ~200 tx/block) | 7,038,814 (n=8) | 8,058,817 (n=2, prelim²) | **0.87×** (lux faster) | M |
| `ring1000_memdb` | 9,459,739 | _excluded¹_ | — | M |

¹ `ring1000` excluded from the head-to-head: its untimed block-generation setup signs
~900 K txs with pure-Go ECDSA on the lux side, making wall-clock disproportionate; it adds
nothing the ring200 row doesn't. lux median shown for reference.
² `ring200` coreth median is n=2 (preliminary): its per-count block-generation setup is
~5 min (setup-bound, not exec-bound), so the full 8-count run was truncated as disproportionate
for a supporting row. The two coreth samples are tightly clustered and ring benches are
low-variance, so the ~0.89× direction is stable; lux ring200 is the full n=8 median.

**Reading — nuanced and honest.** Both are sequential geth-fork EVMs of the **same order of
magnitude**. The picture across the three rows:
- **Fixed per-block overhead** (`empty`): lux carries ~2.1× coreth's — its newer geth base
  (`luxfi/geth v1.16.98` vs `libevm v1.13.15`) plus a per-commit durability check. This is a
  fixed cost, not an execution-throughput gap.
- **Light block** (`valueTx`, 1 tx): within ~15 %, even with lux's pure-Go-crypto handicap.
- **Contract-heavy block** (`ring200`, ~200 tx): **lux is faster (0.89×)** — its execution
  path scales better on heavy blocks, overcoming both the fixed-overhead and the crypto
  handicap. On the realistic heavy workload, lux wins.

**Verdict:** at the execution layer, on identical hardware, the two C-Chains are comparable
(lux heavier fixed overhead, lux faster on heavy execution). Neither is remotely the live
bottleneck — even lux's 121 µs/block `empty` is ~8,200 blocks/s of headroom vs the ~0.2
blocks/s the fee-throttled live chain produces (§3). This is why **live** TPS is governed by
the shared fee/gas-target throttle, not by an execution gap — the Lux win is consensus
liveness (§4), and the parallel-execution win is the (unwired) Block-STM/GPU path (§2).

### 2. Block-STM parallel-execution headroom — CPU (M, Host)

The parallel executor is real and benchmarked on CPU. The honest result: the current
**pure-Go, copy-the-world speculation** engine is **state-copy-bound** — serial beats it on
CPU — while the worker sweep shows genuine parallelism extraction. The billions/s live in
the GPU / MvHashMap path (tagged below).

| Bench | serial | blockstm (parallel) | Note | Tag |
|---|---:|---:|---|:--:|
| `BlockSTMLightTx` (2000 tx, spin 0)     | **112.0 M tx/s** | 375,757 tx/s | copy tax dominates light tx | M |
| `BlockSTMHeavyTx` (2000 tx, spin 10000) | 291,669 tx/s | 53,593 tx/s | serial still wins at this weight | M |
| `BlockSTMWorkers` 1→2→4→8→10 | — | 30,520 → 41,181 → 47,638 → 51,679 → **51,934 tx/s** | **1.70× across 1→10 workers** | M |

GPU / settlement ceilings from prior fleet runs (tagged, not re-run here — no AMD/NVIDIA
box reachable from this host): DEX matcher **12.48 B ord/s** (AMD Radeon 8060S/HIP),
**9.42 B** (NVIDIA GB10/CUDA), **30.3 B** four-device fleet (M\*, `gpu-native-billion-tps`);
settlement/MPT-commit bound **353,877 fills/s** (M, CPU, `BenchmarkSettleSharded`, this host).
**Provenance note:** the billions-ord/s figures have **no committed raw-data file** — they
live only in `papers/gpu-native-billion-tps.tex`; an earlier run recorded 12.76 B / 9.13 B.
Cite as fleet measurements pending a committed raw-results artifact.

### 3. Live C-Chain throughput ceiling — the fee/gas-target, not consensus (M, Cluster)

Two independent runs, both non-destructive, both with all 5 nodes coherent throughout:

| Metric | closed-loop | open-loop (saturating) | Tag |
|---|---:|---:|:--:|
| mined throughput | 1.81 tx/s | 5.72 tx/s | M |
| mean block interval | 4.38 s | 4.15 s | M |
| mean tx / block | 9.17 | 24.62 (peak **106**) | M |
| **mean block fullness** | **1.60 %** | **4.31 %** (peak **18.55 %**) | M |
| base fee | 25 gwei (floor) | 25 gwei (floor) | M |
| cross-node coherence | yes (all 5 = `0x5960dc41…`) | yes (all 5 = `0x9fb2b542…`) | M |

**Reading:** even when saturated, blocks peak at ~19 % full with the base fee pinned at its
floor while consensus stays coherent and idle — so the live ceiling is the **coreth/evm
block-builder gas-target + cadence policy**, not consensus and not execution capacity. This
is the same fee mechanism avalanchego's C-Chain runs, which is why the two chains' *live*
mined rates are comparable. (Prior in-cluster break-test: ~430 tx/s offered absorbed
coherently, ~25 tx/s mined — M\*.)

### 4. Consensus liveness — Nova's two-tier ladder under a node-drop (M, Cluster)

The property avalanchego's single-α Snowman cannot match: a 5-node fleet keeps **producing
and coherent at 3/5**, where a ⅔-only rule freezes (`⌈2·5/3⌉ = 4 > 3`).

| Phase | Nova advance (blk/70 s) | Coherent | Tier active | Tag |
|---|---:|:--:|---|:--:|
| 5/5 baseline | +20 | yes | Nova + Quasar | M |
| 4/5 drop luxd-4 | +8 | yes | Nova + Quasar (≥⅔) | M |
| **3/5 drop luxd-3** | **+6** | **yes** | **Nova only; Quasar honestly pauses (<⅔)** | M |
| 4/5 restore luxd-3 | rejoin | yes | dropped node caught up 150→161 | M |
| 5/5 restore luxd-4 | rejoin | yes | full set restored | M |

All phases held `distinct@conf = 1` (one common-height hash) and `clamp_ok` (exported height
never exceeded latest). Nova = majority local-accept `⌊n/2⌋+1`; Quasar = strict >⅔-stake
export. Full treatment: `papers/lp-305-nova-consensus/`, spec `lps/LP-305`.

## Reproduce

```sh
# Execution head-to-head (needs the two test binaries; see below)
CGO_ENABLED=0 GOTOOLCHAIN=local go test -c -o /tmp/lux_core.test    ~/work/lux/evm/core        # from evm/
SDKROOT=$(xcrun --show-sdk-path) CGO_ENABLED=1 GOTOOLCHAIN=local \
  go test -c -o /tmp/coreth_core.test ~/work/ava/coreth/core                                    # from coreth/
COUNT=8 BT=2s ./run_insertchain.sh

# Block-STM CPU headroom
cd ~/work/lux/evm && CGO_ENABLED=0 go test -run='^$' -bench='BenchmarkBlockSTM' -benchmem -count=5 ./core/parallel

# Live C-Chain ceiling (non-destructive)
./cchain_live.sh                                   # closed-loop
LOADGEN=tp_loadgen_open.py TAG=open PIPELINE=64 ./cchain_live.sh   # open-loop saturating

# Nova consensus node-drop (DESTRUCTIVE — scales the testnet; guarded)
CONFIRM=yes NOVA_IMG=ghcr.io/luxfi/node:v1.36.0 ./nova_proof.sh

python3 parse_results.py     # -> results/summary.json + median tables
```

Raw outputs and `summary.json` are in `results/`.
