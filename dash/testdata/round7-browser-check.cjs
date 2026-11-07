// Browser smoke test against the checked-in synthetic envelope; no live store.
const fs = require('fs'), http = require('http'), path = require('path');
const {chromium} = require('/var/tmp/friendG-dash-visual-20261106/node_modules/playwright');
const data = JSON.parse(fs.readFileSync('dash/testdata/seed.json', 'utf8'));
const amount = '$90,071,992,547,409.93';
data.data.tables.work.alpha.cost = amount;
data.data.tables.work.alpha.cost_by_tier = {pro: amount};
data.data.stream_costs.alpha = {total_cost: amount, work_cost: amount};
data.data.tables.work.beta = {...data.data.tables.work.alpha, waiting:'0', fail:'1', cost:'-', cost_by_tier:{}};
data.data.streams.push({Stream:'beta', State:'waiting'});
data.data.stream_costs.beta = {};
data.data.tables.friends = {};
data.data.all = 2;
const server = http.createServer((req, res) => {
  if (req.url.startsWith('/api/sprint')) { res.setHeader('Content-Type','application/json'); return res.end(JSON.stringify(data)); }
  const name = req.url.split('?')[0] === '/' ? 'index.html' : req.url.split('?')[0].slice(1);
  const types = {'.html':'text/html', '.js':'text/javascript', '.woff2':'font/woff2', '.png':'image/png'};
  res.setHeader('Content-Type', types[path.extname(name)] || 'application/octet-stream');
  fs.readFile(path.join('dash/page',name), (e, b) => { if (e) { res.statusCode = 404; return res.end(); } res.end(b); });
});
(async () => {
 await new Promise(resolve => server.listen(0,'127.0.0.1',resolve));
 const browser = await chromium.launch({headless:true});
 try {
  for (const width of [375, 1440]) {
   const page = await browser.newPage({viewport:{width,height:900}}), errors=[];
   page.on('pageerror', e => errors.push(String(e)));
   await page.goto(`http://127.0.0.1:${server.address().port}/`);
   await page.waitForFunction(expected => document.querySelector('#cost').textContent === expected, amount, {timeout:10000});
   const got = await page.evaluate(() => ({cost:document.querySelector('#cost').textContent, rows:document.querySelector('#streams').textContent, pie:document.querySelector('#pie').getAttribute('aria-label'), tierLegend:document.querySelector('#tier-sub').textContent, top:document.querySelector('#top-streams').textContent, history:!!document.querySelector('#history-panel'), summary:document.querySelector('#streams-sub').textContent, friendsHeader:document.querySelectorAll('#friends .row.head > div').length, friendsTotal:document.querySelectorAll('#friends .row.total').length, statuses:Array.from(document.querySelectorAll('#streams .row:not(.head):not(.total)')).map(r=>r.children[1].textContent), legend:Array.from(document.querySelectorAll('#legend > span')).filter(e=>!e.hidden).map(e=>e.textContent), bar:Array.from(document.querySelectorAll('#overall > .cell')).map(e=>e.className)}));
   if (!got.rows.includes('$90,071,992,547,409.93') || got.pie !== 'spend by tier' || !got.tierLegend.includes('$90,071,992,547,410') || !got.top.includes(amount) || got.history || got.summary !== '2 streams · 0 landed · 0 held' || got.friendsHeader !== 8 || got.friendsTotal !== 1 || got.statuses.join(',') !== 'waiting,fail' || !got.legend.at(-1).startsWith('fail ') || !got.bar.at(-1).includes('fail') || errors.length) throw Error(JSON.stringify({width,got,errors}));
   console.log(`width ${width}: exact cost and table, whole-dollar legend, fail order, empty Friends table, no history panel or page errors`);
   await page.close();
  }
 } finally { await browser.close(); server.close(); }
})().catch(e => { console.error(e); server.close(); process.exitCode=1; });
