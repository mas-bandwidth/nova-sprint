const fs = require('fs');
const {chromium} = require('/var/tmp/friendG-dash-visual-20261106/node_modules/playwright');
(async () => {
  const dest = '/var/tmp/friendG-dash-visual-20261106/output-round5';
  fs.mkdirSync(dest, {recursive:true});
  const browser = await chromium.launch({headless:true,ignoreDefaultArgs:["--hide-scrollbars"]});
  const context = await browser.newContext({timezoneId:'America/New_York',deviceScaleFactor:1});
  const page = await context.newPage();
 await page.addInitScript(()=>document.addEventListener("DOMContentLoaded",()=>{const s=document.createElement("style");s.textContent="html { overflow-y: scroll !important; } ::-webkit-scrollbar { width:15px; height:15px; }";document.head.appendChild(s)}));
  const errors = [];
  page.on('pageerror', e => errors.push(String(e)));
  const response = page.waitForResponse(r => new URL(r.url()).pathname === '/api/sprint' && r.status() === 200);
  await page.goto('http://37.27.135.206', {waitUntil:'domcontentloaded'});
  const envelope = await (await response).json();
  fs.writeFileSync(dest+'/live-v1-envelope.json', JSON.stringify(envelope,null,2));
  await page.evaluate(() => document.fonts.ready);
  await page.waitForFunction(() => !document.querySelector('#live')?.textContent.includes('Connecting'));
  const checks=[];
  for (const width of [2000]) {
    await page.setViewportSize({width,height:1000});
    await page.screenshot({path:dest+'/v1-live-'+width+'.png',fullPage:true,animations:'disabled'});
    checks.push(await page.evaluate(() => ({width:innerWidth,scroll:document.documentElement.scrollWidth,client:document.documentElement.clientWidth,scrollbar:innerWidth-document.documentElement.clientWidth,title:document.title,fonts:document.fonts.status})));
  }
  fs.writeFileSync(dest+'/v1-capture.json', JSON.stringify({url:page.url(),at:new Date().toISOString(),zone:'America/New_York',checks,errors},null,2));
  console.log(JSON.stringify({dest,checks,errors}));
  await browser.close();
})().catch(e => {console.error(e);process.exit(1)});
