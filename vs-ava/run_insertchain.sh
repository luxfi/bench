#!/bin/bash
# Execution-layer head-to-head: luxfi/evm vs ava/coreth, SAME BenchmarkInsertChain suite
# (both geth-fork EVMs), IDENTICAL hardware (this host), run SEQUENTIALLY so neither
# contends for cores. Uses precompiled test binaries so timing excludes compilation.
#
# Build-config asymmetry (documented, honest): coreth builds only CGO=1 (its pure-Go
# path has broken deps: blst rb_tree + libevm nocgo btcec); luxfi/evm builds only CGO=0
# on this host (CGO=1 needs the luxgpu C++ libs, absent). So the crypto backend differs
# on tx-bearing benches (Lux pure-Go ECDSA-recover is SLOWER than coreth's cgo libsecp256k1
# -> the asymmetry HANDICAPS Lux). empty_memdb has ZERO transactions => ZERO crypto in the
# hot path => the CLEAN, confound-free comparison of the block-processing machinery.
set -uo pipefail
LUX=${LUX:-/tmp/lux_core.test}; CORETH=${CORETH:-/tmp/coreth_core.test}
OUTDIR=$(cd "$(dirname "$0")" && pwd)/results; mkdir -p "$OUTDIR"
BENCHES='BenchmarkInsertChain_(empty|valueTx|ring200|ring1000)_memdb'
COUNT=${COUNT:-8}; BT=${BT:-2s}
export GOMAXPROCS=${GOMAXPROCS:-10}

run(){ local name="$1" bin="$2"
  echo "### $name  ($bin)  GOMAXPROCS=$GOMAXPROCS  count=$COUNT benchtime=$BT  $(date -u +%FT%TZ) ###" | tee "$OUTDIR/insertchain_${name}_raw.txt"
  # evm/geth structured logs go to STDERR -> /dev/null so they never enter the TIMED
  # region as file writes and never pollute the parseable bench results on STDOUT.
  # (luxfi/geth logs a per-commit durability line that coreth lacks; empty_memdb has no
  # state change => no commit log => the confound-free apples-to-apples row.)
  "$bin" -test.run='^$' -test.bench="^(${BENCHES})\$" -test.benchmem -test.count="$COUNT" -test.benchtime="$BT" 2>/dev/null | tee -a "$OUTDIR/insertchain_${name}_raw.txt"
  echo "### $name DONE $(date -u +%FT%TZ) ###" | tee -a "$OUTDIR/insertchain_${name}_raw.txt"
}

# lux first, then coreth — sequential, full machine each
run lux "$LUX"
run coreth "$CORETH"
echo "ALL_INSERTCHAIN_DONE $(date -u +%FT%TZ)"
