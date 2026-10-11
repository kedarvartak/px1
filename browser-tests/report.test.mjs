import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {dirname, join} from 'node:path';
import {fileURLToPath} from 'node:url';
import test, {after, before} from 'node:test';
import AxeBuilder from '@axe-core/playwright';
import {chromium} from 'playwright';

const projectRoot=dirname(dirname(fileURLToPath(import.meta.url)));
const fixtureRoot=mkdtempSync(join(tmpdir(),'px1-browser-'));
const repo=join(fixtureRoot,'repo');
const site=join(fixtureRoot,'site');
let browser;
let reportHTML;

function git(...args){return execFileSync('git',['-C',repo,...args],{encoding:'utf8'}).trim()}

before(async()=>{
  mkdirSync(join(repo,'src'),{recursive:true});
  mkdirSync(join(repo,'.px1'),{recursive:true});
  git('init','-q');
  git('config','user.email','fixture@px1.test');
  git('config','user.name','px1 fixture');
  writeFileSync(join(repo,'src','orders.ts'),'export const ready = true;\n');
  writeFileSync(join(repo,'src','orders.test.ts'),"import {ready} from './orders';\nvoid ready;\n");
  writeFileSync(join(repo,'README.md'),'Fixture repository.\n');
  writeFileSync(join(repo,'.px1','rules.json'),JSON.stringify({rules:[{pattern:'console\\.log',glob:'*.ts',message:'Use the shared logger'}]}));
  git('add','.');git('commit','-qm','base');
  const base=git('rev-parse','HEAD');
  const lines=['export function reviewOrders() {'];
  for(let i=0;i<2500;i++)lines.push(`  console.log("order-${i}");`);
  lines.push('}');
  writeFileSync(join(repo,'src','orders.ts'),lines.join('\n')+'\n');
  writeFileSync(join(repo,'README.md'),'Fixture repository with deployment notes.\n');
  git('add','.');git('commit','-qm','head');
  const head=git('rev-parse','HEAD');
  execFileSync('go',['run','.', 'export-review','--base',base,'--head',head,'--root',repo,'--out',site],{cwd:projectRoot,stdio:'pipe'});
  reportHTML=readFileSync(join(site,'index.html'),'utf8');
  const executablePath=process.env.PX1_CHROMIUM_PATH||undefined;
  browser=await chromium.launch({headless:true,executablePath,args:['--no-sandbox']});
});

after(async()=>{if(browser)await browser.close();rmSync(fixtureRoot,{recursive:true,force:true})});

test('report renders a large diff inside the performance budget',async()=>{
  const page=await browser.newPage();
  const started=performance.now();
  await page.setContent(reportHTML,{waitUntil:'load'});
  await page.locator('.fsec').first().waitFor();
  const elapsed=performance.now()-started;
  assert.ok(await page.locator('.ln').count()>=2500,'all large-diff lines should render');
  assert.ok(elapsed<5000,`large report rendered in ${Math.round(elapsed)}ms; budget is 5000ms`);
  await page.close();
});

test('report has no serious automated accessibility violations',async()=>{
  const context=await browser.newContext();
  const page=await context.newPage();
  await page.setContent(reportHTML,{waitUntil:'load'});
  await page.locator('.fsec').first().waitFor();
  const results=await new AxeBuilder({page}).disableRules(['color-contrast']).analyze();
  const serious=results.violations.filter((v)=>v.impact==='serious'||v.impact==='critical');
  assert.deepEqual(serious.map((v)=>({id:v.id,impact:v.impact,nodes:v.nodes.length})),[]);
  await context.close();
});

test('keyboard controls expose report navigation',async()=>{
  const page=await browser.newPage();
  await page.setContent(reportHTML,{waitUntil:'load'});
  const theme=page.getByRole('button',{name:/switch to .* theme/i});
  await theme.focus();
  await page.keyboard.press('Enter');
  assert.equal(await page.locator('html').getAttribute('data-theme'),'light');
  const separator=page.getByRole('separator',{name:'Resize side panel'});
  await separator.focus();
  const before=Number(await separator.getAttribute('aria-valuenow'));
  await page.keyboard.press('ArrowRight');
  assert.ok(Number(await separator.getAttribute('aria-valuenow'))>before);
  await page.close();
});

test('search narrows both navigation and visible diffs',async()=>{
  const page=await browser.newPage();
  await page.setContent(reportHTML,{waitUntil:'load'});
  const search=page.getByRole('searchbox',{name:'Search files and changed code'});
  await search.fill('deployment notes');
  assert.equal(await page.locator('.fsec:visible').count(),1);
  assert.match(await page.locator('.fsec:visible .path').textContent(),/README\.md/);
  await search.fill('order-2499');
  assert.equal(await page.locator('.fsec:visible').count(),1);
  assert.match(await page.locator('.fsec:visible .path').textContent(),/orders\.ts/);
  await page.close();
});

function reportVariant(update){
  return reportHTML.replace(/(<script type="application\/json" id="report-data">)([\s\S]*?)(<\/script>)/,(_,start,json,end)=>{
    const data=JSON.parse(json);update(data);
    return start+JSON.stringify(data).replace(/</g,'\\u003c')+end;
  });
}

test('human review includes unflagged files and stays independent of findings',async()=>{
  const page=await browser.newPage();
  await page.setContent(reportHTML);
  assert.match(await page.locator('h1').textContent(),/^0 of 2 files marked reviewed/);
  // Acknowledging every generated finding must never complete the code review.
  await page.locator('.ann .act').evaluateAll((buttons)=>buttons.forEach((button)=>button.click()));
  assert.match(await page.locator('h1').textContent(),/^0 of 2 files marked reviewed/);
  assert.equal(await page.locator('.queue .qitem:not(.done)').count(),0);
  await page.getByRole('button',{name:/^Flagged \d+$/}).click();
  await page.getByRole('searchbox').fill('order-2499');
  await page.getByRole('button',{name:'Next unreviewed file'}).click();
  assert.equal(await page.getByRole('searchbox').inputValue(),'');
  assert.equal(await page.locator('.fsec:visible').count(),2);
  assert.equal(await page.evaluate(()=>document.activeElement.getAttribute('aria-label')),'README.md');
  const readme=page.getByRole('combobox',{name:'Review status for README.md',exact:true});
  await readme.selectOption('reviewed');
  assert.match(await page.locator('h1').textContent(),/^1 of 2 files marked reviewed/);
  await page.getByRole('button',{name:'Next unreviewed file'}).click();
  assert.equal(await page.evaluate(()=>document.activeElement.getAttribute('aria-label')),'src/orders.ts');
  await page.getByRole('combobox',{name:'Review status for src/orders.ts',exact:true}).selectOption('follow-up');
  assert.match(await page.locator('h1').textContent(),/1 of 2 files marked reviewed · 1 need follow-up/);
  assert.equal(await page.getByRole('button',{name:'Next unreviewed file'}).isDisabled(),true);
  await readme.selectOption('unreviewed');
  assert.equal(await page.getByRole('button',{name:'Next unreviewed file'}).isEnabled(),true);
  await page.close();
});

test('a report with no findings still starts with every file unreviewed',async()=>{
  const page=await browser.newPage();
  await page.setContent(reportVariant((data)=>{data.ruleHits=[];data.attention=[];data.explanations=[]}));
  assert.match(await page.locator('h1').textContent(),/^0 of 2 files marked reviewed/);
  assert.equal(await page.getByText('No automated findings. Changed code still needs your review.',{exact:true}).count(),1);
  await page.locator('.fsec').last().scrollIntoViewIfNeeded();
  assert.match(await page.locator('h1').textContent(),/^0 of 2 files marked reviewed/);
  await page.close();
});

test('file review persists only for the same repository and revision pair',async()=>{
  const context=await browser.newContext();
  const page=await context.newPage();
  let currentHTML=reportHTML;
  await page.route('https://px1.test/**',(route)=>route.fulfill({contentType:'text/html',body:currentHTML}));
  await page.goto('https://px1.test/report');
  const state=()=>page.getByRole('combobox',{name:'Review status for README.md',exact:true});
  await state().selectOption('reviewed');
  await page.reload();
  assert.equal(await state().inputValue(),'reviewed');
  for(const field of ['repository','base','head']){
    currentHTML=reportVariant((data)=>{data[field]+='-different'});
    await page.reload();
    assert.equal(await state().inputValue(),'unreviewed',field+' must isolate progress');
  }
  currentHTML=reportHTML;await page.reload();
  assert.equal(await state().inputValue(),'reviewed');
  await context.close();
});

test('invalid stored states are ignored and storage failure keeps review usable',async()=>{
  const context=await browser.newContext();
  const page=await context.newPage();
  await page.route('https://px1.test/**',(route)=>route.fulfill({contentType:'text/html',body:reportHTML}));
  await page.goto('https://px1.test/report');
  await page.evaluate(()=>{
    const data=JSON.parse(document.getElementById('report-data').textContent);
    localStorage.setItem('px1:file-review:'+JSON.stringify([data.repository,data.base,data.head]),JSON.stringify([['README.md','approved'],['missing.ts','reviewed'],null]));
  });
  await page.reload();
  assert.match(await page.locator('h1').textContent(),/^0 of 2 files marked reviewed/);
  await page.evaluate(()=>{Storage.prototype.setItem=()=>{throw new Error('storage disabled')}});
  await page.getByRole('combobox',{name:'Review status for README.md',exact:true}).selectOption('reviewed');
  assert.match(await page.locator('h1').textContent(),/^1 of 2 files marked reviewed/);
  assert.match(await page.locator('.review-hint').textContent(),/storage unavailable; progress lasts only on this page/);
  await context.close();
});

test('affected-file hints explain unchanged files outside the changed-file list',async()=>{
  const page=await browser.newPage();
  await page.setContent(reportHTML,{waitUntil:'load'});
  const hints=page.locator('.affected-file');
  assert.equal(await hints.count(),1);
  assert.match(await hints.textContent(),/src\/orders\.test\.ts/);
  assert.match(await hints.textContent(),/test imports the changed file/);
  assert.equal(await page.locator('.files .file').count(),2,'unchanged hints must not enter the changed-file queue');
  await page.close();
});

test('inspection questions separate evidence from prompts and link back to code',async()=>{
  const page=await browser.newPage();
  await page.setContent(reportHTML,{waitUntil:'load'});
  const cards=page.locator('.inspection-card');
  assert.ok(await cards.count()>=2,'fixture should produce public API and related-test questions');
  assert.equal(await cards.getByText('Observed evidence',{exact:true}).count(),await cards.count());
  assert.equal(await cards.getByText('Reviewer question',{exact:true}).count(),await cards.count());
  assert.match(await cards.filter({hasText:'unchanged test'}).textContent(),/src\/orders\.test\.ts/);
  const lineLink=cards.getByRole('link',{name:/View diff line/}).first();
  await lineLink.click();
  assert.match(await page.evaluate(()=>location.hash),/^#line-f\d+-n\d+$/);
  assert.equal(await page.locator('.inspection-card .act').count(),0,'questions must not become acknowledgement findings');
  await page.close();
});

test('reviewer feedback exports browser-local notes and acknowledgements',async()=>{
  const page=await browser.newPage();
  await page.setContent(reportHTML,{waitUntil:'load'});
  await page.getByRole('button',{name:'Acknowledge'}).first().click();
  await page.getByRole('textbox',{name:'Reviewer notes'}).fill('Ready after the logging follow-up.');
  await page.evaluate(()=>{
    window.__feedbackExports=[];
    URL.createObjectURL=(blob)=>{blob.text().then((text)=>window.__feedbackExports.push(text));return 'blob:px1-test'};
    URL.revokeObjectURL=()=>{};
    HTMLAnchorElement.prototype.click=()=>{};
  });

  await page.getByRole('button',{name:'Export JSON'}).click();
  await page.waitForFunction(()=>window.__feedbackExports.length===1);
  const json=await page.evaluate(()=>window.__feedbackExports[0]);
  const data=JSON.parse(json);
  assert.equal(data.note,'Ready after the logging follow-up.');
  assert.equal(data.head,git('rev-parse','HEAD'));
  assert.equal(data.acknowledgements.length,1);
  assert.match(data.acknowledgements[0].path,/orders\.ts/);

  await page.getByRole('button',{name:'Export Markdown'}).click();
  await page.waitForFunction(()=>window.__feedbackExports.length===2);
  const markdown=await page.evaluate(()=>window.__feedbackExports[1]);
  assert.match(markdown,/## px1 reviewer feedback/);
  assert.match(markdown,/Ready after the logging follow-up/);
  assert.match(markdown,/- \[x\] .+ — `src\/orders\.ts/);
  await page.close();
});
