const fs=require('fs');
const {chromium}=require('/var/tmp/stella-dash-visual-20261106/node_modules/playwright');
(async()=>{
 const dest='/var/tmp/stella-dash-visual-20261106/output-round5';
 const browser=await chromium.launch({headless:true,ignoreDefaultArgs:["--hide-scrollbars"]});
 const context=await browser.newContext({timezoneId:'America/New_York',deviceScaleFactor:1});
 const page=await context.newPage(),errors=[],requests=[];
 await page.addInitScript(()=>document.addEventListener("DOMContentLoaded",()=>{const s=document.createElement("style");s.textContent="html { overflow-y: scroll !important; } ::-webkit-scrollbar { width:15px; height:15px; }";document.head.appendChild(s)}));
 page.on('pageerror',e=>errors.push(String(e)));
 page.on('console',m=>{if(m.type()==='error')errors.push(m.text())});
 page.on('response',r=>{if(r.status()>=400)errors.push(r.status()+' '+r.url())});
 page.on('request',r=>{if(new URL(r.url()).pathname.startsWith('/api/')||new URL(r.url()).pathname==='/events')requests.push({url:r.url(),at:Date.now()})});
 await page.goto('http://127.0.0.1:18082',{waitUntil:'domcontentloaded'});
 await page.evaluate(()=>document.fonts.ready);
 await page.waitForFunction(()=>!document.querySelector('#live')?.textContent.includes('Connecting'));
 const checks=[];
 for(const width of [2000]){
  await page.setViewportSize({width,height:1000});
  await page.evaluate(()=>new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r))));
  await page.screenshot({path:dest+'/v2-'+width+'.png',fullPage:true,animations:'disabled'});
  await page.screenshot({path:dest+'/v2-top-'+width+'.png',animations:'disabled'});
  checks.push(await page.evaluate(()=>{
   const style=(selector,key)=>{const e=document.querySelector(selector);return e?getComputedStyle(e)[key]:null};
   return {width:innerWidth,scroll:document.documentElement.scrollWidth,client:document.documentElement.clientWidth,scrollbar:innerWidth-document.documentElement.clientWidth,fonts:document.fonts.status,statsDisplay:style('#history-panel','display'),coordDisplay:style('#coord-chip','display'),failColor:style('#legend .fail','backgroundColor'),blockedColor:style('#legend .blocked','backgroundColor'),commentLeak:document.body.innerText.includes('is replaced;'),zeroCents:cents(0),zeroPercent:pct(0),flow:FLOW,legend:STATES,landed:document.querySelector('#landed').textContent,inflight:document.querySelector('#inflight').textContent,cost:document.querySelector('#cost').textContent,version:document.querySelector('#ver').textContent,coordinator:document.querySelector('#coord-chip').textContent,legendVisible:Array.from(document.querySelector('#legend').children).filter(e=>getComputedStyle(e).display!=='none').map(e=>e.textContent),fleetFirstOK:document.querySelector('#fleet .row:not(.head)').children[6].textContent,throughputText:document.querySelector('#tput').textContent,eta:document.querySelector('#eta').textContent,pulseStopped:document.querySelector('#overall').classList.contains('stopped'),emptyStreamState:Array.from(document.querySelectorAll('#streams .row')).filter(r=>r.children[0]?.textContent==='serialize-audit-cs').map(r=>r.children[1].textContent),workCompact:document.querySelector('.panel.streams').classList.contains('compact'),workHeaders:Array.from(document.querySelector('#streams .head').children).filter(e=>getComputedStyle(e).display!=='none').map(e=>e.textContent),workFirstVisible:Array.from(document.querySelector('#streams .row:not(.head)').children).filter(e=>getComputedStyle(e).display!=='none').map(e=>({text:e.textContent,x:e.getBoundingClientRect().x,y:e.getBoundingClientRect().y}))};
  }));
 }
 for(const check of checks){
  if(check.throughputText!=='0')throw Error('startup throughput missing');
  if(check.version!=='dev')throw Error('version fallback mismatch');
  if(check.emptyStreamState[0]!=='waiting')throw Error('empty stream not waiting');
  if(check.scrollbar!==15)throw Error("expected real15px scrollbar");
  if(check.scroll>check.width)throw Error('overflow at '+check.width);
  if(check.width>=2000&&(check.workCompact||check.workHeaders.length!==11))throw Error('Work must be full at '+check.width);
  if(check.width===375&&(!check.workCompact||check.workHeaders.join(',')!=='stream,status,landed'))throw Error('phone compact layout changed');
 }
 await page.waitForTimeout(3200);
 const response=await page.request.get('http://127.0.0.1:18082/api/sprint');
 const finalEnvelope=await response.json();
 fs.writeFileSync(dest+'/v2-envelope.json',JSON.stringify(finalEnvelope,null,2));
 const live=JSON.parse(fs.readFileSync(dest+'/live-v1-envelope.json','utf8')).data.tables,percentChecks=[];
 for(const side of ['fleet','friends']) for(const [name,row] of Object.entries(live[side])) {const actual=finalEnvelope.data.tables[side][name].okpct,expected=row.okpct;if(actual!==expected)throw Error(side+'.'+name+' ok% '+actual+' expected '+expected);percentChecks.push({table:side,name,live:row.okpct,actual,expected})}
 fs.writeFileSync(dest+'/okpct-comparison.json',JSON.stringify(percentChecks,null,2));
 const result={at:new Date().toISOString(),zone:'America/New_York',checks,errors,requests};
 fs.writeFileSync(dest+'/v2-capture.json',JSON.stringify(result,null,2));
 console.log(JSON.stringify(result));await browser.close();
})().catch(e=>{console.error(e);process.exit(1)});
