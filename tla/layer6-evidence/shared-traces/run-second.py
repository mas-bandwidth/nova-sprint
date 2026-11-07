import os,sys,subprocess,json,hashlib,socket
from pathlib import Path
b=Path(__file__).resolve().parent
assert socket.gethostname().startswith("vision")
out=b/"evidence-second"
out.mkdir(exist_ok=False)
results=[]
for d in sorted((b/"inputs").iterdir()):
 if not (d.name.startswith("final-") or d.name=="negative-install"): continue
 model="Install" if d.name=="negative-install" else "Run" if d.name.startswith("final-run") else "RunClear"
 env=dict(os.environ,TRACE_LOG=str(out/(d.name+".tlc.log")))
 try:
  p=subprocess.run(["bash",str(b/"spec/check-trace.sh"),model,str(d)],env=env,capture_output=True,text=True,timeout=120)
  code=p.returncode;text=p.stdout+p.stderr
 except subprocess.TimeoutExpired:
  code=124;text="TIMEOUT"
 (out/(d.name+".harness.log")).write_text(text)
 row=dict(name=d.name,model=model,exit=code,output=text.strip())
 results.append(row);print(json.dumps(row),flush=True)
 if code!=0: break
(out/"results.json").write_text(json.dumps(results,indent=2)+"\n")
