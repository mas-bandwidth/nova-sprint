import json,subprocess,pathlib,sys,datetime
out=pathlib.Path('/var/tmp/friendG-abshot-20261106/round5');out.mkdir(exist_ok=True)
cmd=['/var/tmp/friendG-abshot-20261106/abshot','--a','http://127.0.0.1:18083/','--b','http://127.0.0.1:18084/','--widths','2000','--chrome','/var/tmp/friendG-dash-visual-20261106/chrome-scrollbars-round5.py','--out',str(out)]
with (out/'verified.log').open('w') as f: result=subprocess.run(cmd,stdout=f,stderr=subprocess.STDOUT)
receipt={'at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'command':cmd,'exit':result.returncode,'harness_commit':'f70cf5e1e6151a5127df48e17ef2d59ced5f5172','dashboard':'round5 candidate based on 62f2dcff','timezone':'America/New_York','settle_ms':1500,'channel_tolerance':8,'pixel_tolerance':64,'scrollbar_setup':'wrapper removes --hide-scrollbars; localhost proxies inject identical overflow-y:scroll and 15px scrollbar CSS into A/B; metrics recorded in scrollbar-metrics.jsonl','upstreams':{'A':'http://37.27.135.206/','B':'http://127.0.0.1:18082/'}}
(out/'receipt.json').write_text(json.dumps(receipt,indent=2)+'\n');print(json.dumps(receipt));sys.exit(0 if result.returncode in [0,1] else result.returncode)
