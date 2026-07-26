#!/bin/bash
# Live Lux C-Chain throughput measurement — NON-DESTRUCTIVE (no validator scaling).
# Deploys an in-cluster steady loadgen (default tp_loadgen.py = closed-loop; set
# LOADGEN=tp_loadgen_open.py TAG=open PIPELINE=64 for the saturating open-loop run),
# which measures the REAL mined ceiling over a wall-clock window and prints one
# `MEASURE {json}` line. We parse it, clean up, and write results/cchain_live[_TAG].json.
# This proves the C-Chain live ceiling is the coreth/evm fee & gas-target throttle (low
# block fullness), NOT consensus. The consensus-liveness proof (5/5->4/5->3/5 node-drop
# coherence) is the separate, destructive nova_proof.sh and is NOT re-run here.
set -uo pipefail
export PATH=/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin:$PATH
CTX=${CTX:-do-sfo3-lux-k8s}; NS=${NS:-lux-testnet}
RPC_PORT=${RPC_PORT:-9640}; CHAINID=${CHAINID:-96368}
SENDERS=${SENDERS:-24}; WINDOW=${WINDOW:-90}; NNODES=${NNODES:-5}; PIPELINE=${PIPELINE:-64}
LOADGEN=${LOADGEN:-tp_loadgen.py}; TAG=${TAG:-}
SCRATCH=$(cd "$(dirname "$0")" && pwd)
LGBASE=$(basename "$LOADGEN"); SFX=${TAG:+_$TAG}; POD=tp-loadgen${TAG:+-$TAG}; CM=tp-loadgen-src${TAG:+-$TAG}
OUT=$SCRATCH/results/cchain_live${SFX}.json; RAW=$SCRATCH/results/cchain_live${SFX}_raw.log
mkdir -p "$SCRATCH/results"; : > "$RAW"
say(){ echo "$@" | tee -a "$RAW"; }

say "### Lux C-Chain live throughput ($LGBASE) on $NS ($CTX) — $(date -u +%FT%TZ) ###"
say "### image: $(kubectl --context $CTX -n $NS get sts luxd -o jsonpath='{.spec.template.spec.containers[0].image}' 2>/dev/null) ###"

PK=$(kubectl --context $CTX -n $NS get secret lux-deployer -o jsonpath='{.data.LUX_PRIVATE_KEY}' 2>/dev/null | base64 -d)
[ -n "$PK" ] || { say "REFUSING: no lux-deployer funder key in $NS"; exit 2; }
kubectl --context $CTX -n $NS delete pod "$POD" --wait=false >/dev/null 2>&1; sleep 2
kubectl --context $CTX -n $NS create configmap "$CM" --from-file="$LGBASE=$SCRATCH/$LOADGEN" --dry-run=client -o yaml | kubectl --context $CTX -n $NS apply -f - >/dev/null
cat <<EOF | kubectl --context $CTX -n $NS apply -f - >/dev/null
apiVersion: v1
kind: Pod
metadata: {name: $POD, labels: {app: tp-loadgen}}
spec:
  restartPolicy: Never
  containers:
  - name: gen
    image: python:3.11-slim
    command: ["sh","-c","pip -q install eth-account >/dev/null 2>&1 && python /src/$LGBASE"]
    env:
    - {name: NS,        value: "$NS"}
    - {name: PORT,      value: "$RPC_PORT"}
    - {name: CHAINID,   value: "$CHAINID"}
    - {name: SENDERS,   value: "$SENDERS"}
    - {name: WINDOW,    value: "$WINDOW"}
    - {name: NNODES,    value: "$NNODES"}
    - {name: PIPELINE,  value: "$PIPELINE"}
    - {name: FUNDER_PK, value: "$PK"}
    volumeMounts: [{name: src, mountPath: /src}]
  volumes: [{name: src, configMap: {name: $CM}}]
EOF
unset PK
say "### $POD deployed — waiting for MEASURE (fund + ${WINDOW}s window) ###"

DEADLINE=$((SECONDS + WINDOW + 320)); MEASURE=""
while [ $SECONDS -lt $DEADLINE ]; do
  LOGS=$(kubectl --context $CTX -n $NS logs "$POD" 2>/dev/null)
  echo "$LOGS" > "$SCRATCH/results/cchain_live${SFX}_pod.log"
  M=$(echo "$LOGS" | grep -E "^\[.*\] MEASURE " | tail -1 | sed 's/^\[[^]]*\] MEASURE //')
  [ -n "$M" ] && { MEASURE="$M"; break; }
  P=$(echo "$LOGS" | grep -E "PROGRESS" | tail -1); [ -n "$P" ] && say "  $P"
  phase=$(echo "$LOGS" | grep -E "funding|SATURATE|STEADY|FATAL" | tail -1); [ -n "$phase" ] && say "  $phase"
  echo "$LOGS" | grep -q "FATAL" && { say "FATAL in loadgen — aborting"; break; }
  sleep 8
done

kubectl --context $CTX -n $NS delete pod "$POD" --wait=false >/dev/null 2>&1
if [ -z "$MEASURE" ]; then say "### NO MEASURE captured — see $SCRATCH/results/cchain_live${SFX}_pod.log ###"; exit 1; fi

echo "$MEASURE" | python3 -m json.tool > "$OUT" 2>/dev/null || echo "$MEASURE" > "$OUT"
say ""; say "================ LUX C-CHAIN LIVE THROUGHPUT — ${LGBASE} (MEASURED) ================"
echo "$MEASURE" | python3 -c '
import sys,json; m=json.load(sys.stdin)
g=lambda k,d="-": m.get(k,d)
print(f"  mode              {g(\"mode\",\"closed-loop\")}   window {g(\"window_s\")}s  blocks {g(\"blocks\")} ({g(\"b0\")}->{g(\"b1\")})")
print(f"  offered  tx/s     {g(\"offered_tps\")}")
print(f"  MINED    tx/s     {g(\"mined_tps\")}     <-- live C-Chain ceiling")
print(f"  blocks/s          {g(\"blocks_per_s\")}   block interval ~{g(\"block_interval_ms_mean\")} ms")
print(f"  avg tx/block      {g(\"avg_tx_per_block\")}   peak tx/block {g(\"peak_tx_per_block\",\"-\")}")
print(f"  avg gas/block     {int(float(g(\"avg_gas_per_block\",0)))} of {g(\"gas_limit\")} limit")
print(f"  block FULLNESS    avg {g(\"avg_fullness_pct\")}%  peak {g(\"peak_fullness_pct\",\"-\")}%   <-- ceiling = fee/gas-target throttle, not consensus")
print(f"  gas/s             {int(float(g(\"gas_per_s\",0)))}   base fee {g(\"base_fee_gwei_last\")} gwei")
print(f"  coherent@{g(\"conf_height\")}       {g(\"coherent\")}   node hashes {g(\"node_hashes\")}")
' | tee -a "$RAW"
say "### done — json $OUT ###"
