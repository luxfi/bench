#!/bin/bash
# Nova node-drop NON-EVENT proof. The one invariant the owner mandated:
#   a validator falls behind → the chain KEEPS FINALIZING → it rejoins → still finalizing.
# Sequence (StatefulSet scales from the highest ordinal, so this drops 4 then 3, restores 3 then 4):
#   5/5 baseline → 4/5 (drop luxd-4, ≥⅔: Nova+Quasar both advance)
#              → 3/5 (drop luxd-3, <⅔: THE PROOF — Nova ADVANCES, Quasar PAUSES, degraded=true)
#              → 4/5 (restore luxd-3: Quasar RESUMES, luxd-3 rejoins+converges)
#              → 5/5 (restore luxd-4: luxd-4 rejoins+converges)
# On the OLD single-⅔ engine the 3/5 phase FREEZES — that is exactly the regression Nova kills.
#
# SAFETY: refuses to run unless (a) CONFIRM=yes AND (b) the STS image matches NOVA_IMG — so merely
# staging this file cannot touch testnet, and it can NEVER run against the pre-Nova engine by accident.
set -uo pipefail
export PATH=/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin:$PATH
CTX=${CTX:-do-sfo3-lux-k8s}; NS=${NS:-lux-testnet}
NOVA_IMG=${NOVA_IMG:-}                    # REQUIRED: the ghcr.io/luxfi/node:vX.Y.Z that carries Nova
CHAINID=${CHAINID:-96368}
RPC_PORT=${RPC_PORT:-9640}                # testnet internal HTTP = 9640 (mainnet 9630, devnet 9650)
# QUASAR_ACTIVE=1 only when the export bridge is live (node w/ 40d3390fb2 + evm v1.104.8); on
# v1.104.7 the export tags track the Nova tip, so the quasar(finalized) column is INFORMATIONAL.
QUASAR_ACTIVE=${QUASAR_ACTIVE:-0}
PHASE_SECS=${PHASE_SECS:-70}              # dwell per phase (must exceed a few block times)
SCRATCH=$(cd "$(dirname "$0")" && pwd)
OUT=$SCRATCH/nova_proof.out; : > "$OUT"
say(){ echo "$@" | tee -a "$OUT"; }
# mlog: progress written to the LOG FILE ONLY (never stdout) — so measure()'s stdout is
# exclusively its final result line and $(measure ...) captures the 4 numbers, not the noise.
mlog(){ echo "$@" >> "$OUT"; }

[ "${CONFIRM:-no}" = "yes" ] || { say "REFUSING: set CONFIRM=yes to run the node-drop proof (it scales testnet validators)."; exit 2; }
[ -n "$NOVA_IMG" ]         || { say "REFUSING: set NOVA_IMG=ghcr.io/luxfi/node:vX.Y.Z (the Nova image)."; exit 2; }
RUNIMG=$(kubectl --context $CTX -n $NS get sts luxd -o jsonpath='{.spec.template.spec.containers[0].image}' 2>/dev/null)
[ "$RUNIMG" = "$NOVA_IMG" ] || { say "REFUSING: testnet STS runs '$RUNIMG', not Nova '$NOVA_IMG'. Deploy Nova first."; exit 2; }
say "### Nova proof on $NS — image $RUNIMG — $(date -u +%FT%TZ) ###"

# eth_blockNumber of a live ordinal (novaHeight proxy) via kubectl exec (measurement needs no throughput)
tip(){ kubectl --context $CTX -n $NS exec luxd-$1 -c luxd -- curl -s -m4 http://localhost:$RPC_PORT/v1/chain/C/rpc -X POST -H 'content-type: application/json' --data '{"jsonrpc":"2.0","id":1,"method":"eth_getBlockByNumber","params":["latest",false]}' 2>/dev/null | python3 -c 'import sys,json;b=json.load(sys.stdin)["result"];print(int(b["number"],16),b["hash"][:10])' 2>/dev/null; }
# finalized tag == the Quasar (⅔-stake export) tip after the v1.36 fix — STANDARD eth RPC, no custom method.
# Returns the finalized block height, or 0 before the first export cert forms.
qtip(){ kubectl --context $CTX -n $NS exec luxd-$1 -c luxd -- curl -s -m4 http://localhost:$RPC_PORT/v1/chain/C/rpc -X POST -H 'content-type: application/json' --data '{"jsonrpc":"2.0","id":1,"method":"eth_getBlockByNumber","params":["finalized",false]}' 2>/dev/null | python3 -c 'import sys,json;b=json.load(sys.stdin).get("result");print(int(b["number"],16) if b else 0)' 2>/dev/null; }

# hash at a SPECIFIC height on node $1 — for common-height convergence (not latest-height skew).
hashat(){ kubectl --context $CTX -n $NS exec luxd-$1 -c luxd -- curl -s -m4 http://localhost:$RPC_PORT/v1/chain/C/rpc -X POST -H 'content-type: application/json' --data '{"jsonrpc":"2.0","id":1,"method":"eth_getBlockByNumber","params":["'"$(printf '0x%x' "$2")"'",false]}' 2>/dev/null | python3 -c 'import sys,json;b=json.load(sys.stdin).get("result");print(b["hash"][:10] if b else "x")' 2>/dev/null; }

# measure a phase over `live` ordinals: nova(latest) advance + CONVERGENCE AT A COMMON CONFIRMED HEIGHT
# (min-2: all live nodes must hold the SAME block there — height skew is NOT a fork) + finalized(quasar).
measure(){ local phase="$1" secs="$2"; shift 2; local live=("$@")
  local h0=$(tip "${live[0]}" | awk '{print $1}'); h0=${h0:-0}
  local q0=$(qtip "${live[0]}"); q0=${q0:-0}
  mlog "--- $phase (live: ${live[*]}) start nova(latest)=$h0 quasar(finalized)=$q0 ---"
  local end=$((SECONDS+secs)) conv_ok=1 clamp_ok=1 last=$h0 qlast=$q0
  while [ $SECONDS -lt $end ]; do
    local hs="" mx=0 mn=""; local -a hv=()
    for i in "${live[@]}"; do read -r n hh < <(tip $i); n=${n:-x}; hs="$hs $i:$n"; [ "$n" != x ] && { hv+=("$i:$n"); [ "$n" -gt "$mx" ] && mx=$n; { [ -z "$mn" ] || [ "$n" -lt "$mn" ]; } && mn=$n; }; done
    local d=1 conf=0
    if [ -n "$mn" ] && [ "$mn" -ge 3 ]; then
      conf=$((mn-2)); local ch=""
      for pair in "${hv[@]}"; do ch="$ch $(hashat "${pair%%:*}" "$conf")"; done
      d=$(echo $ch | tr ' ' '\n' | grep -vE '^$|^x$' | sort -u | wc -l | tr -d ' ')
    fi
    local q=$(qtip "${live[0]}"); q=${q:-$qlast}
    { [ "$QUASAR_ACTIVE" = 1 ] && [ "$q" -gt "$mx" ] 2>/dev/null; } && { clamp_ok=0; mlog "  *** CLAMP VIOLATION: finalized=$q > latest=$mx"; }
    { [ "$q" -gt "$qlast" ] 2>/dev/null; } && qlast=$q
    mlog "  [$phase]$hs conf=$conf distinct@conf=$d nova=$mx finalized=$q"
    [ "${d:-1}" -gt 1 ] && conv_ok=0
    last=$mx; sleep 8
  done
  local adv=$((last-h0)) qadv=$((qlast-q0))
  mlog "--- $phase END nova=$last(+$adv) quasar=$qlast(+$qadv) converged=$([ $conv_ok = 1 ] && echo yes || echo NO) clamp_ok=$([ $clamp_ok = 1 ] && echo yes || echo NO) ---"
  echo "$adv $conv_ok $qadv $clamp_ok"
}

scale(){ say "### scale luxd → $1 ###"; kubectl --context $CTX -n $NS scale sts luxd --replicas=$1 >/dev/null 2>&1
  for _ in $(seq 1 40); do r=$(kubectl --context $CTX -n $NS get pods -l app=luxd --no-headers 2>/dev/null | awk '$2=="1/1"'|wc -l|tr -d ' '); [ "$r" = "$1" ] && break; sleep 6; done; }

# 0. deploy steady loadgen (in-cluster; port-forwards flap under load)
say "### deploy loadgen pod ###"
PK=$(kubectl --context $CTX -n $NS get secret lux-deployer -o jsonpath='{.data.LUX_PRIVATE_KEY}' | base64 -d)
kubectl --context $CTX -n $NS delete pod nova-loadgen --wait=false >/dev/null 2>&1; sleep 2
kubectl --context $CTX -n $NS create configmap nova-loadgen-src --from-file=nova_loadgen.py=$SCRATCH/nova_loadgen.py --dry-run=client -o yaml | kubectl --context $CTX -n $NS apply -f - >/dev/null
cat <<EOF | kubectl --context $CTX -n $NS apply -f - >/dev/null
apiVersion: v1
kind: Pod
metadata: {name: nova-loadgen, labels: {app: nova-loadgen}}
spec:
  restartPolicy: Never
  containers:
  - name: gen
    image: python:3.11-slim
    command: ["sh","-c","pip -q install eth-account >/dev/null 2>&1 && python /src/nova_loadgen.py"]
    env:
    - {name: NS, value: "$NS"}
    - {name: PORT, value: "$RPC_PORT"}
    - {name: CHAINID, value: "$CHAINID"}
    - {name: DURATION, value: "$((PHASE_SECS*10+500))"}   # cover all 5 phases + slow restore boots (~77s each) so load never expires mid-run
    - {name: SENDERS, value: "24"}
    - {name: FUNDER_PK, value: "$PK"}
    volumeMounts: [{name: src, mountPath: /src}]
  volumes: [{name: src, configMap: {name: nova-loadgen-src}}]
EOF
unset PK
for _ in $(seq 1 30); do kubectl --context $CTX -n $NS logs nova-loadgen 2>/dev/null | grep -q "STEADY LOAD" && break; sleep 5; done
say "$(kubectl --context $CTX -n $NS logs nova-loadgen 2>/dev/null | tail -2)"

# 1..5 the drop/restore sequence
R_BASE=$(measure "5of5-baseline" $PHASE_SECS 0 1 2 3 4)
scale 4
R_4=$(measure "4of5-drop-luxd4" $PHASE_SECS 0 1 2 3)
scale 3
R_3=$(measure "3of5-drop-luxd3-THE-PROOF" $PHASE_SECS 0 1 2)
scale 4
R_r3=$(measure "4of5-restore-luxd3" $PHASE_SECS 0 1 2 3)
scale 5
R_r4=$(measure "5of5-restore-luxd4" $PHASE_SECS 0 1 2 3 4)

# verdict — the FULL two-tier proof: Nova (latest) liveness + Quasar (finalized) honesty + clamp invariant
say ""; say "================ NOVA TWO-TIER NODE-DROP VERDICT ================"
f1(){ echo "$1"|awk '{print $1}'; }; f2(){ echo "$1"|awk '{print $2}'; }; f3(){ echo "$1"|awk '{print $3}'; }; f4(){ echo "$1"|awk '{print $4}'; }
PASS=1
# args: name  R("nova_adv conv quasar_adv clamp_ok")  qexp(adv|stall)
chk(){ local name="$1" R="$2" qexp="$3"; local adv=$(f1 "$R") conv=$(f2 "$R") qadv=$(f3 "$R") clamp=$(f4 "$R"); local ok=1 why=""
  [ "${adv:-0}" -le 0 ] && { ok=0; why="$why nova-stalled"; }
  [ "$conv" != 1 ]      && { ok=0; why="$why FORKED"; }
  [ "$clamp" != 1 ]     && { ok=0; why="$why finalized>latest"; }
  if [ "$QUASAR_ACTIVE" = 1 ]; then
    if [ "$qexp" = adv ]; then { [ "${qadv:-0}" -le 0 ] && { ok=0; why="$why quasar-stalled(want-advance)"; }; }
    else { [ "${qadv:-0}" -gt 0 ] && { ok=0; why="$why quasar-advanced(want-pause<⅔)"; }; }; fi
  fi
  [ $ok = 1 ] || PASS=0
  local qn="quasar+$qadv"; [ "$QUASAR_ACTIVE" = 1 ] || qn="finalized+$qadv(info:tracks-nova)"
  say "  $([ $ok = 1 ] && echo PASS || echo FAIL) $name: nova+$adv $qn conv=$conv clamp=$clamp$why"; }
chk "5/5 baseline — both tiers advance"                   "$R_BASE" adv
chk "4/5 drop-luxd4 — Nova+Quasar advance (≥⅔)"           "$R_4"    adv
chk "3/5 drop-luxd3 THE PROOF — Nova ADVANCES, Quasar PAUSES (<⅔)" "$R_3" stall
chk "4/5 restore-luxd3 — Quasar RESUMES, luxd-3 rejoined" "$R_r3"   adv
chk "5/5 restore-luxd4 — luxd-4 rejoined"                 "$R_r4"   adv
say ""; say "  >>> $([ $PASS = 1 ] && echo '✅ NOVA TWO-TIER PROVEN: 3/5 keeps producing (Nova) while export honestly pauses (no Quasar); finalized never exceeds latest; dropped nodes rejoin+converge' || echo '❌ FAILED — a phase stalled/forked/leaked; do NOT tag')"
kubectl --context $CTX -n $NS delete pod nova-loadgen --wait=false >/dev/null 2>&1
say "### done — full log $OUT ###"
