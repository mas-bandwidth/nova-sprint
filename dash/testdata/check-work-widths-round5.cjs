const fs=require('fs'),assert=require('assert/strict');
const {chromium}=require('/var/tmp/friendG-dash-visual-20261106/node_modules/playwright');
(async()=>{
 const results=[];
 for(const scrollbar of [0,15]){
  const browser=await chromium.launch({headless:true,...(scrollbar?{ignoreDefaultArgs:['--hide-scrollbars']}:{})});
  const page=await browser.newPage({viewport:{width:2000,height:1000}});
  await page.goto('http://127.0.0.1:18082');if(scrollbar)await page.addStyleTag({content:'html { overflow-y: scroll !important; } ::-webkit-scrollbar { width:15px; height:15px; }'});await page.evaluate(()=>document.fonts.ready);
  await page.waitForFunction(()=>document.querySelector('#streams .row:not(.head)'));
  for(const width of [...Array.from({length:18},(_,i)=>1930+i*10),2800,375]){
   await page.setViewportSize({width,height:1000});
   await page.evaluate(()=>new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r))));
   const got=await page.evaluate(()=>{
    const panel=document.querySelector('.panel.streams'),ps=getComputedStyle(panel),innerRight=panel.getBoundingClientRect().right-parseFloat(ps.borderRightWidth),issues=[],boxes=[];
    for(const [i,row] of Array.from(panel.querySelectorAll('.row')).entries()){
     const r=row.getBoundingClientRect(),s=getComputedStyle(row),left=r.left+parseFloat(s.borderLeftWidth)+parseFloat(s.paddingLeft),right=r.right-parseFloat(s.borderRightWidth)-parseFloat(s.paddingRight),tracks=s.gridTemplateColumns.split(' ').map(Number.parseFloat),grid=tracks.reduce((a,b)=>a+b,0)+(tracks.length-1)*parseFloat(s.columnGap);
     if(grid>right-left+0.5)issues.push('grid outside content row '+i);
     if(row.scrollWidth>row.clientWidth+1)issues.push('row overflow '+i);
     const cells=Array.from(row.children).filter(c=>getComputedStyle(c).display!=='none');
     const bounds=c=>{if(!row.classList.contains('head'))return c.getBoundingClientRect();const range=document.createRange();range.selectNodeContents(c);return range.getBoundingClientRect()};
     for(const [j,c] of cells.entries()){
      const b=c.getBoundingClientRect();if(b.left<left-0.5||b.right>right+0.5)issues.push('cell outside content '+i+':'+j);
      if(!c.classList.contains('name')&&c.scrollWidth>c.clientWidth+7)issues.push('cell overflow '+i+':'+j);
      if(j&&bounds(cells[j-1]).right>bounds(c).left+1)issues.push('cell overlap '+i+':'+j);
     }
     boxes.push({row:i,contentWidth:right-left,gridWidth:grid,costInset:innerRight-row.lastElementChild.getBoundingClientRect().right});
    }
    return {width:innerWidth,clientWidth:document.documentElement.clientWidth,scrollbar:innerWidth-document.documentElement.clientWidth,compact:panel.classList.contains('compact'),gutter:getComputedStyle(panel.querySelector('.row')).columnGap,boxes,issues};
   });
   assert.equal(got.scrollbar,scrollbar);assert.equal(got.compact,width===375);assert.deepEqual(got.issues,[]);
   if(width>=1930)for(const row of got.boxes)assert.ok(Math.abs(row.costInset-32)<0.6,'cost inset '+JSON.stringify({width,scrollbar,row}));
   results.push(got);console.log(JSON.stringify({...got,boxes:got.boxes.slice(0,1)}));
  }
  await browser.close();
 }
 fs.writeFileSync('/var/tmp/friendG-dash-visual-20261106/output-round5/work-width-check.json',JSON.stringify(results,null,2));
})().catch(e=>{console.error(e);process.exit(1)});
