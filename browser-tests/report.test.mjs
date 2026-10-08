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
