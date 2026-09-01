#!/usr/bin/env python3
"""Nova node-drop proof — STEADY in-cluster loadgen (no ramp, no break-detection).
Runs INSIDE a pod, funds N keys, then fires a FIXED number of steady senders for
DURATION seconds so blocks keep flowing while the ORCHESTRATOR (outside) drops and
restores validators. The orchestrator measures heights/convergence/Quasar; this file
only keeps the mempool fed. Reuses the proven funding+sender machinery from harness_ic.py."""
import os, json, time, random, threading, http.client
from eth_account import Account

_port=os.environ.get('PORT','9640')  # testnet internal HTTP = 9640 (mainnet 9630, devnet 9650)
_default=",".join(f"luxd-{i}.luxd-headless.{os.environ.get('NS','lux-testnet')}.svc.cluster.local:{_port}" for i in range(5))
NODES=[hp.rsplit(":",1) for hp in os.environ.get("NODES",_default).split(",")]
HOST=[h for h,_ in NODES]; PORT=[int(p) for _,p in NODES]; NN=len(NODES)
RPC_PATH="/v1/chain/C/rpc"
CHAINID=int(os.environ.get("CHAINID","96368"))   # testnet C-Chain
N_KEYS=int(os.environ.get("N_KEYS","60"))
FUND_LUX=int(os.environ.get("FUND_LUX","5"))
SENDERS=int(os.environ.get("SENDERS","24"))       # steady, moderate — keep blocks flowing, don't saturate
DURATION=int(os.environ.get("DURATION","420"))    # cover the whole drop/restore sequence
SEND_TIMEOUT=float(os.environ.get("SEND_TIMEOUT","3.0"))

def log(*a): print(f"[{time.strftime('%H:%M:%S')}]", *a, flush=True)
FUNDER_PK=os.environ["FUNDER_PK"].strip()
if not FUNDER_PK.startswith("0x"): FUNDER_PK="0x"+FUNDER_PK
FUNDER=Account.from_key(FUNDER_PK); FUNDER_KEY=FUNDER.key; FUNDER_ADDR=FUNDER.address
del FUNDER_PK
log("funder", FUNDER_ADDR, "| nodes:", [h.split('.')[0] for h in HOST], "| chainId", CHAINID)

random.seed(0xC0FFEE)
KEYS=[Account.create().key for _ in range(N_KEYS)]
ADDR=[Account.from_key(k).address for k in KEYS]
NONCE=[0]*N_KEYS
STOP=threading.Event()

def raw_of(s): return getattr(s,"raw_transaction",None) or getattr(s,"rawTransaction")
def sign_raw(key,nonce,gp,to,value=0,gas=21000):
    tx={"nonce":nonce,"gasPrice":gp,"gas":gas,"to":to,"value":value,"data":b"","chainId":CHAINID}
    return "0x"+raw_of(Account.sign_transaction(tx,key)).hex()
def rpc_on(conn,method,params,rid=1):
    conn.request("POST",RPC_PATH,json.dumps({"jsonrpc":"2.0","id":rid,"method":method,"params":params}),{"Content-Type":"application/json"})
    return json.loads(conn.getresponse().read())
def rpc(i,method,params,timeout=8):
    c=http.client.HTTPConnection(HOST[i],PORT[i],timeout=timeout)
    try: return rpc_on(c,method,params)
    finally:
        try: c.close()
        except Exception: pass
def h2i(x):
    try: return int(x,16)
    except Exception: return None

def get_gas_price():
    try: bf=h2i(rpc(0,"eth_getBlockByNumber",["latest",False])["result"].get("baseFeePerGas","0x0")) or 0
    except Exception: bf=0
    try: gp=h2i(rpc(0,"eth_gasPrice",[])["result"]) or 0
    except Exception: gp=0
    return max(bf*8,gp*2,100_000_000_000)

def fund_all():
    gp=get_gas_price()
    base=h2i(rpc(0,"eth_getTransactionCount",[FUNDER_ADDR,"pending"],timeout=6).get("result"))
    log(f"funding {N_KEYS} keys x {FUND_LUX} LUX, nonce={base}, gp={gp/1e9:.0f}gwei")
    val=FUND_LUX*10**18; WINDOW=60
    conn=http.client.HTTPConnection(HOST[0],PORT[0],timeout=8)
    next_i=0; t0=time.time(); last_prog=t0; last_mined=base
    while True:
        try: mined=h2i(rpc(0,"eth_getTransactionCount",[FUNDER_ADDR,"latest"])["result"])
        except Exception: mined=last_mined
        if mined is None: mined=last_mined
        if mined>last_mined: last_prog=time.time(); last_mined=mined
        if next_i>=N_KEYS and mined>=base+N_KEYS: break
        while next_i<N_KEYS and (base+next_i-mined)<WINDOW:
            raw=sign_raw(FUNDER_KEY,base+next_i,gp,ADDR[next_i],val)
            try: r=rpc_on(conn,"eth_sendRawTransaction",[raw])
            except Exception:
                try: conn.close()
                except Exception: pass
                conn=http.client.HTTPConnection(HOST[0],PORT[0],timeout=8); break
            s=str(r).lower()
            if "result" in r or "already known" in s or "nonce too low" in s: next_i+=1
            elif "underpriced" in s: gp=int(gp*1.6); break
            elif "full" in s or "limit" in s: break
            else: next_i+=1
        if time.time()-last_prog>25: gp=int(gp*1.6); next_i=max(0,mined-base); last_prog=time.time()
        if time.time()-t0>240: log("funding timeout"); break
        time.sleep(0.4)
    funded=[i for i in range(N_KEYS) if (h2i(rpc(0,"eth_getBalance",[ADDR[i],"latest"]).get("result")) or 0)>0]
    log(f"funded {len(funded)}/{N_KEYS} in {time.time()-t0:.0f}s")
    return funded

def sender(i,gp):
    conns={}
    def conn(j):
        c=conns.get(j)
        if c is None: c=http.client.HTTPConnection(HOST[j],PORT[j],timeout=SEND_TIMEOUT); conns[j]=c
        return c
    to=ADDR[i]; key=KEYS[i]
    while not STOP.is_set():
        j=random.randint(0,NN-1)
        try: raw=sign_raw(key,NONCE[i],gp,to)
        except Exception: break
        try: resp=rpc_on(conn(j),"eth_sendRawTransaction",[raw],rid=i)
        except (http.client.HTTPException,OSError):
            try: conns.pop(j).close()
            except Exception: pass
            time.sleep(0.05); continue    # a dropped node just means pick another next loop
        if "result" in resp: NONCE[i]+=1
        else:
            m=str(resp.get("error",{}).get("message","")).lower()
            if "nonce too low" in m:
                pn=h2i(rpc(0,"eth_getTransactionCount",[to,"pending"]).get("result"))
                if pn is not None: NONCE[i]=pn
            elif "already known" in m or "replacement" in m: NONCE[i]+=1
            else: time.sleep(0.05)

def main():
    funded=[k for k in fund_all() if k<N_KEYS]
    if len(funded)<SENDERS: log(f"FATAL only {len(funded)} funded < {SENDERS} senders"); return
    gp=get_gas_price()
    log(f"=== STEADY LOAD: {SENDERS} senders for {DURATION}s (gp={gp/1e9:.0f}gwei) — orchestrator drives drops now ===")
    ths=[threading.Thread(target=sender,args=(funded[n],gp),daemon=True) for n in range(SENDERS)]
    for th in ths: th.start()
    t0=time.time()
    while time.time()-t0<DURATION and not STOP.is_set(): time.sleep(1)
    STOP.set()
    log(f"=== loadgen done ({time.time()-t0:.0f}s) ===")

if __name__=="__main__":
    try: main()
    finally: STOP.set()
