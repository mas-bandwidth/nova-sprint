import json, threading, urllib.request, gzip
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from datetime import datetime, timezone
root=Path('/var/tmp/stella-abshot-20261106/round5')
root.mkdir(parents=True, exist_ok=True)
lock=threading.Lock()
inject=b'''<style>html { overflow-y: scroll !important; } ::-webkit-scrollbar { width:15px; height:15px; }</style><script>addEventListener('DOMContentLoaded',()=>{const sample=()=>requestAnimationFrame(()=>{fetch('/__scrollbar-evidence',{method:'POST',body:JSON.stringify({width:innerWidth,height:innerHeight,client:document.documentElement.clientWidth,scrollbar:innerWidth-document.documentElement.clientWidth,compact:document.querySelector('.panel.streams')?.classList.contains('compact')})})});addEventListener('resize',sample);setTimeout(sample,500)});</script>'''
def handler(upstream,role):
 class Handler(BaseHTTPRequestHandler):
  def log_message(self,*args): pass
  def do_POST(self):
   if self.path!='/__scrollbar-evidence': self.send_error(404);return
   data=json.loads(self.rfile.read(int(self.headers['Content-Length'])))
   with lock:
    with (root/'scrollbar-metrics.jsonl').open('a') as f:f.write(json.dumps({'at':datetime.now(timezone.utc).isoformat(),'role':role,'upstream':upstream,**data})+'\n')
   self.send_response(204);self.end_headers()
  def do_GET(self):
   req=urllib.request.Request(upstream+self.path,headers={'Accept-Encoding':'identity'})
   try:
    with urllib.request.urlopen(req,timeout=10) as res:
     body=res.read();ctype=res.headers.get('Content-Type','application/octet-stream')
     if res.headers.get('Content-Encoding')=='gzip':body=gzip.decompress(body)
     if 'text/html' in ctype:body=body.replace(b'</head>',inject+b'</head>',1)
     self.send_response(res.status);self.send_header('Content-Type',ctype);self.send_header('Content-Length',str(len(body)));self.send_header('Cache-Control','no-store');self.end_headers();self.wfile.write(body)
   except Exception as e:self.send_error(502,str(e))
 return Handler
servers=[ThreadingHTTPServer(('127.0.0.1',port),handler(url,role)) for port,url,role in [(18083,'http://37.27.135.206','A'),(18084,'http://127.0.0.1:18082','B')]]
for s in servers:threading.Thread(target=s.serve_forever,daemon=True).start()
print('Scrollbar-only test proxies ready on 18083/18084',flush=True)
threading.Event().wait()
