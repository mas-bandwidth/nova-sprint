import os,subprocess,json,hashlib,socket
from pathlib import Path
base=Path(__file__).resolve().parent
assert socket.gethostname().startswith(("vision","hetzner2"))
out=base/"omission-result";out.mkdir(exist_ok=False)
raw=out/"tlc.log"
p=subprocess.run(["bash",str(base/"checked-spec/check-trace.sh"),"RunClear",str(base/"omission-fixture")],env=dict(os.environ,TRACE_LOG=str(raw)),stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True,timeout=120)
(out/"harness.log").write_text(p.stdout)
text=raw.read_text()
good=p.returncode==0 and "TRACE REFUSED at 8 of 8: SaveCursor" in p.stdout and "Temporal property TraceMatched was violated" in text and "Warning:" not in text
r=dict(exit=p.returncode,expected_refusal=good,raw_sha256=hashlib.sha256(raw.read_bytes()).hexdigest())
(out/"result.json").write_text(json.dumps(r,indent=2)+"\n");print(json.dumps(r));print(p.stdout);raise SystemExit(0 if good else 1)
