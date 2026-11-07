const fs=require('fs'),assert=require('assert/strict');
const {chromium}=require('/var/tmp/friendG-dash-visual-20261106/node_modules/playwright');
(async()=>{const b=await chromium.launch({headless:true});const p=await b.newPage();await p.goto('http://127.0.0.1:18082');await p.evaluate(()=>document.fonts.ready);await p.waitForFunction(()=>document.querySelector('#streams .row:not(.head)'));
const result=[];
for(const width of [2000,2800,375]){
 await p.setViewportSize({width,height:1000});await p.evaluate(()=>new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r))));
 const got=await p.evaluate(()=>{
 const rows=Array.from(document.querySelectorAll('#streams .row')),issues=[],contentBoxes=[];const panel=document.querySelector('.panel.streams'),panelBox=panel.getBoundingClientRect(),panelStyle=getComputedStyle(panel),panelInnerRight=panelBox.right-parseFloat(panelStyle.borderRightWidth);
 for(const [i,r] of rows.entries()){
  if(r.scrollWidth>r.clientWidth+1)issues.push('row '+i+' overflow');
  const rb=r.getBoundingClientRect(),rs=getComputedStyle(r),left=rb.left+parseFloat(rs.borderLeftWidth)+parseFloat(rs.paddingLeft),right=rb.right-parseFloat(rs.borderRightWidth)-parseFloat(rs.paddingRight);
  const tracks=rs.gridTemplateColumns.split(' ').map(Number.parseFloat),gridWidth=tracks.reduce((a,b)=>a+b,0)+Math.max(0,tracks.length-1)*parseFloat(rs.columnGap),contentWidth=right-left;
  if(gridWidth>contentWidth+0.5)issues.push('row '+i+' grid '+gridWidth+' exceeds content '+contentWidth);
  contentBoxes.push({row:i,left,right,contentWidth,gridWidth,paddingRight:parseFloat(rs.paddingRight),costRight:r.lastElementChild.getBoundingClientRect().right,costInset:panelInnerRight-r.lastElementChild.getBoundingClientRect().right});
  const bounds=c=>{if(!r.classList.contains('head'))return c.getBoundingClientRect();const range=document.createRange();range.selectNodeContents(c);return range.getBoundingClientRect()};
  const cells=Array.from(r.children).filter(c=>getComputedStyle(c).display!=='none');
  for(const [j,c] of cells.entries()){
   if(c.getBoundingClientRect().right>right+0.5||c.getBoundingClientRect().left<left-0.5)issues.push('cell '+i+':'+j+' outside row content box');
   if(!c.classList.contains('name')&&c.scrollWidth>c.clientWidth+7)issues.push('cell '+i+':'+j+' overflow '+c.textContent);
   if(j&&bounds(cells[j-1]).right>bounds(c).left+1)issues.push('cell '+i+':'+j+' overlaps '+JSON.stringify({a:cells[j-1].outerHTML.slice(0,180),b:c.outerHTML.slice(0,180),aRight:cells[j-1].getBoundingClientRect().right,bLeft:c.getBoundingClientRect().left}));
  }
 }
 const statusEdges=['streams','fleet','friends'].map(id=>document.querySelector('#'+id+' .row:not(.head)').children[1].getBoundingClientRect().right);
 const pill=document.querySelector('#streams .row:not(.head) .pill');
 return {width:innerWidth,compact:document.querySelector('.panel.streams').classList.contains('compact'),gutter:getComputedStyle(rows[0]).columnGap,statusWordDisplay:getComputedStyle(pill.lastElementChild).display,statusEdges,contentBoxes,issues};
 });
 console.log(JSON.stringify(got)); assert.deepEqual(got.issues,[]);assert.equal(got.compact,width===375);
 if(width>=2000){for(const row of got.contentBoxes)assert.ok(Math.abs(row.costInset-32)<0.6,'cost inset row '+row.row+' = '+row.costInset);assert.notEqual(got.statusWordDisplay,'none');assert.ok(Math.max(...got.statusEdges)-Math.min(...got.statusEdges)<1);}
 result.push(got);
}
fs.writeFileSync('/var/tmp/friendG-dash-visual-20261106/output-round4/work-width-check.json',JSON.stringify(result,null,2));console.log(JSON.stringify(result));await b.close();})().catch(e=>{console.error(e);process.exit(1)});
