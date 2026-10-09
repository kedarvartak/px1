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
