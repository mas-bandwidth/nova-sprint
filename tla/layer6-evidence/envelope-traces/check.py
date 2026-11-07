#!/usr/bin/env python3
import hashlib,json,os,socket,subprocess
from pathlib import Path
base=Path(__file__).resolve().parent
assert socket.gethostname().startswith(("hetzner2","vision"))
for name,h in json.loads((base/"check-input-sha256.json").read_text()).items():
 assert hashlib.sha256((base/name).read_bytes()).hexdigest()==h,name
out=base/"results";out.mkdir(exist_ok=False)
results=[]
for case in json.loads((base/"cases.json").read_text()):
 name=case["name"]
 raw=out/(name+".tlc.log");env=dict(os.environ,TRACE_LOG=str(raw))
 try:
  p=subprocess.run(["bash",str(base/"checked-spec/check-trace.sh"),"RunClear",str(base/"inputs"/name)],env=env,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True,timeout=120)
  code,text=p.returncode,p.stdout
 except subprocess.TimeoutExpired as e:
  code,text=124,str(e)
 (out/(name+".harness.log")).write_text(text)
 log=raw.read_text() if raw.exists() else ""
 row=dict(name=name,exit=code,matched=code==0 and "TRACE OK steps=" in text and "Model checking completed. No error has been found." in log and "Warning:" not in log and "Error:" not in log,raw_sha256=hashlib.sha256(raw.read_bytes()).hexdigest() if raw.exists() else None)
 results.append(row);print(json.dumps(row),flush=True)
(out/"results.json").write_text(json.dumps(results,indent=2)+"\n")
raise SystemExit(0 if all(r["matched"] for r in results) else 1)
