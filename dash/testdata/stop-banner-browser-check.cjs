// Synthetic stopped/start view; no live store or service is contacted.
const fs = require('fs'), http = require('http'), path = require('path');
const {chromium} = require('/var/tmp/stella-dash-visual-20261106/node_modules/playwright');
const data = JSON.parse(fs.readFileSync('dash/testdata/seed.json', 'utf8'));
data.data.stopped = true; data.data.stop_reason = 'Scheduled maintenance'; data.data.machine = 'STOPPED'; data.data.done = true;
const server = http.createServer((req, res) => {
 if (req.url.startsWith('/api/sprint')) { res.setHeader('Content-Type', 'application/json'); return res.end(JSON.stringify(data)); }
 const name = req.url.split('?')[0] === '/' ? 'index.html' : req.url.split('?')[0].slice(1);
 const types = {'.html':'text/html', '.js':'text/javascript', '.woff2':'font/woff2', '.png':'image/png'};
 res.setHeader('Content-Type', types[path.extname(name)] || 'application/octet-stream');
 fs.readFile(path.join('dash/page', name), (e,b) => { if (e) { res.statusCode=404; return res.end(); } res.end(b); });
});
(async()=>{
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
 const browser=await chromium.launch({headless:true});
 try {
  for (const width of [375,1440]) {
   data.data.stopped=true; data.data.stop_reason='Scheduled maintenance'; data.data.machine='STOPPED';
   const page=await browser.newPage({viewport:{width,height:900}}), errors=[];
   page.on('pageerror',e=>errors.push(String(e)));
   await page.goto(`http://127.0.0.1:${server.address().port}/`);
   await page.waitForFunction(()=>!document.querySelector('#stop-banner').hidden,{timeout:10000});
   const stopped=await page.evaluate(()=>({banner:document.querySelector('#stop-banner').textContent,reason:document.querySelector('#stop-reason').textContent,eta:document.querySelector('#eta').textContent,pulseStopped:document.querySelector('#overall').classList.contains('stopped'),scroll:document.documentElement.scrollWidth,client:document.documentElement.clientWidth}));
   if (!stopped.banner.includes('STOPPED') || stopped.reason!=='Scheduled maintenance' || stopped.eta!=='STOP' || !stopped.pulseStopped || stopped.scroll>stopped.client || errors.length) throw Error(JSON.stringify({width,stopped,errors}));
   data.data.stopped=false; data.data.stop_reason='Restarted'; delete data.data.machine;
   data.data.at=new Date(Date.parse(data.data.at)+1000).toISOString();
   await page.waitForFunction(()=>document.querySelector('#stop-banner').hidden && document.querySelector('#eta').textContent==='DONE',{timeout:10000});
   const started=await page.evaluate(()=>({hidden:document.querySelector('#stop-banner').hidden, reason:document.querySelector('#stop-reason').textContent}));
   if (!started.hidden || started.reason || errors.length) throw Error(JSON.stringify({width,started,errors}));
   console.log(`width ${width}: stopped banner, reason and pulse; start hides banner; no page errors`);
   await page.close();
  }
 } finally { await browser.close(); server.close(); }
})().catch(e=>{console.error(e);server.close();process.exitCode=1;});
