#!/bin/bash
# 5/5-UNDER-LOAD proof — NON-DESTRUCTIVE (no validator scaling).
# Composes two proven, orthogonal pieces observing ONE live chain:
#   (a) HAMMER  : cchain_live.sh open-loop (tp_loadgen_open.py) floods the mempool
#                 to pin the saturated mined ceiling + gas util + block fullness.
#   (b) OBSERVE : pentad_monitor.py reads all 5 validators every SAMPLE s and
#                 verifies fork-freedom at EVERY confirmed height for the whole run
#                 (the owner's deliverable: same block at every height, not just tip).
# Emits: throughput json (from cchain_live) + coherence VERDICT json (from monitor).
set -uo pipefail
export PATH=/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin:$PATH
CTX=${CTX:-do-sfo3-lux-k8s}; NS=${NS:-lux-testnet}
RPC_PORT=${RPC_PORT:-9640}; CHAINID=${CHAINID:-96368}
WINDOW=${WINDOW:-150}; PIPELINE=${PIPELINE:-128}
MON_DURATION=${MON_DURATION:-$((WINDOW+130))}     # cover fund + window + slack
SCRATCH=$(cd "$(dirname "$0")" && pwd)
say(){ echo "$@"; }

RUNIMG=$(kubectl --context $CTX -n $NS get sts luxd -o jsonpath='{.spec.template.spec.containers[0].image}' 2>/dev/null)
say "### hammer5 on $NS ($CTX) image=$RUNIMG chainId=$CHAINID — $(date -u +%FT%TZ) ###"

# --- baseline: all 5 tips (height + hash) for the record ---
say "### baseline tips ###"
for i in 0 1 2 3 4; do
  printf "luxd-%s " $i
  kubectl --context $CTX -n $NS exec luxd-$i -c luxd -- curl -s -m5 http://localhost:$RPC_PORT/v1/chain/C/rpc -X POST -H 'content-type: application/json' \
    --data '{"jsonrpc":"2.0","id":1,"method":"eth_getBlockByNumber","params":["latest",false]}' 2>/dev/null \
    | python3 -c 'import sys,json;b=json.load(sys.stdin)["result"];print("h="+str(int(b["number"],16)),"hash="+b["hash"][:18])' 2>/dev/null || echo ERR
done

# --- (b) deploy read-only coherence monitor pod ---
say "### deploy pentad-monitor (DURATION=${MON_DURATION}s) ###"
kubectl --context $CTX -n $NS delete pod pentad-monitor --wait=false >/dev/null 2>&1; sleep 2
kubectl --context $CTX -n $NS create configmap pentad-monitor-src --from-file=pentad_monitor.py=$SCRATCH/pentad_monitor.py --dry-run=client -o yaml | kubectl --context $CTX -n $NS apply -f - >/dev/null
cat <<EOF | kubectl --context $CTX -n $NS apply -f - >/dev/null
apiVersion: v1
kind: Pod
metadata: {name: pentad-monitor, labels: {app: pentad-monitor}}
spec:
  restartPolicy: Never
  containers:
  - name: mon
    image: python:3.11-slim
    command: ["python","/src/pentad_monitor.py"]
    env:
    - {name: NS,       value: "$NS"}
    - {name: PORT,     value: "$RPC_PORT"}
    - {name: NNODES,   value: "5"}
    - {name: DURATION, value: "$MON_DURATION"}
    - {name: SAMPLE,   value: "2.0"}
    volumeMounts: [{name: src, mountPath: /src}]
  volumes: [{name: src, configMap: {name: pentad-monitor-src}}]
EOF
for _ in $(seq 1 20); do kubectl --context $CTX -n $NS logs pentad-monitor 2>/dev/null | grep -q "pentad monitor:" && break; sleep 2; done
say "monitor: $(kubectl --context $CTX -n $NS logs pentad-monitor 2>/dev/null | head -2 | tail -1)"

# --- (a) run the hammer (foreground; blocks until MEASURE) ---
say "### run hammer: cchain_live.sh open-loop WINDOW=${WINDOW} PIPELINE=${PIPELINE} ###"
LOADGEN=tp_loadgen_open.py TAG=hammer PIPELINE=$PIPELINE WINDOW=$WINDOW \
  CTX=$CTX NS=$NS RPC_PORT=$RPC_PORT CHAINID=$CHAINID \
  "$SCRATCH/cchain_live.sh" 2>&1 | sed 's/^/[load] /'

# --- wait for the monitor to finish its window, then harvest ---
say "### hammer done — waiting for monitor VERDICT ###"
for _ in $(seq 1 80); do kubectl --context $CTX -n $NS logs pentad-monitor 2>/dev/null | grep -q "VERDICT " && break; sleep 4; done
MON=$(kubectl --context $CTX -n $NS logs pentad-monitor 2>/dev/null)
echo "$MON" > "$SCRATCH/results/pentad_monitor.log"
say ""; say "================ 5/5 COHERENCE-UNDER-LOAD VERDICT ================"
echo "$MON" | grep -E "VERDICT |FORKS " | sed 's/^\[[^]]*\] //'
echo "$MON" | tail -25 | grep -E "poll |FORK" | tail -12
kubectl --context $CTX -n $NS delete pod pentad-monitor --wait=false >/dev/null 2>&1
say "### done — monitor log $SCRATCH/results/pentad_monitor.log ; throughput $SCRATCH/results/cchain_live_hammer.json ###"
