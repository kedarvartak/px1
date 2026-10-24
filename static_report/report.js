(()=>{
const report=JSON.parse(document.getElementById('report-data').textContent);
const app=document.getElementById('app');
const files=Array.isArray(report.files)?report.files:[];
const blocks=Array.isArray(report.blocks)?report.blocks:[];
const inspectionQuestions=Array.isArray(report.inspectionQuestions)?report.inspectionQuestions:[];
const inspectionAnalysis=report.inspectionAnalysis||null;
const ruleHits=Array.isArray(report.ruleHits)?report.ruleHits:[];
const attention=Array.isArray(report.attention)?report.attention:[];
const explanations=Array.isArray(report.explanations)?report.explanations:[];
const findings=Array.isArray(report.findings)?report.findings:[];
const verification=report.verification||{checks:[]};
const checks=Array.isArray(verification.checks)?verification.checks:[];
const evidence=Array.isArray(report.evidence)?report.evidence:[];

const h=(tag,cls,txt)=>{const n=document.createElement(tag);if(cls)n.className=cls;if(txt!=null)n.textContent=String(txt);return n};
const short=(sha)=>String(sha||'').slice(0,7);
const STATUS={A:'added',M:'modified',D:'deleted',R:'renamed',C:'copied',T:'type changed'};
const KIND={rule:'Rule',attn:'Attention',finding:'AI finding',note:'Note'};
const CHIP={rule:'chip-danger',attn:'chip-attention',finding:'chip-finding',note:'chip-note'};
const chipFor=(k)=>h('span','chip '+CHIP[k],KIND[k]);

/* Acknowledgements live in this browser only. The key predates the redesign;
   keeping it means earlier acknowledgements still apply. */
const ackKey='px1:ack:'+report.repository+':'+report.head;
let ack=new Set();
try{const saved=JSON.parse(localStorage.getItem(ackKey)||'[]');if(Array.isArray(saved))ack=new Set(saved.filter((k)=>typeof k==='string'))}catch{}
const persist=()=>{try{localStorage.setItem(ackKey,JSON.stringify([...ack].sort()))}catch{}};

/* Explicit human declarations are independent of automated findings. Include
   both revisions: a different comparison must start a fresh review. */
const reviewKey='px1:file-review:'+JSON.stringify([report.repository,report.base,report.head]);
const reviewStates=new Map();
const validPaths=new Set(files.map((f)=>f.path));
let reviewStorageAvailable=true;
try{
  const saved=JSON.parse(localStorage.getItem(reviewKey)||'[]');
  if(Array.isArray(saved))for(const entry of saved){
    if(Array.isArray(entry)&&entry.length===2&&validPaths.has(entry[0])&&['reviewed','follow-up'].includes(entry[1]))reviewStates.set(entry[0],entry[1]);
  }
}catch{reviewStorageAvailable=false}
const reviewState=(path)=>reviewStates.get(path)||'unreviewed';
const persistReview=()=>{
  try{localStorage.setItem(reviewKey,JSON.stringify([...reviewStates]));reviewStorageAvailable=true}
  catch{reviewStorageAvailable=false}
};

/* One list of everything worth a reader's attention, anchored to a file. */
const items=[];
const fileIndex=new Map(files.map((f,i)=>[f.path,i]));
const add=(it)=>{if(fileIndex.has(it.path)){it.fi=fileIndex.get(it.path);it.id='ann-'+items.length;items.push(it)}};
for(const hit of ruleHits)add({k:'rule',ackId:hit.key,path:hit.path,line:hit.line,title:hit.title||hit.message,why:(hit.why?hit.why+' · ':'')+(hit.severity?hit.severity+' · ':'')+'Team rule '+hit.ruleId+(hit.origin?' ('+hit.origin+')':'')+'.',ev:hit.text,src:hit.source});
for(const flag of attention)add({k:'attn',ackId:flag.id,path:flag.path,line:flag.line||0,title:flag.title,why:flag.reason,ev:flag.evidence,src:'rule '+flag.rule});
for(const finding of findings)add({k:'finding',ackId:['ai',finding.path,finding.start_line,finding.end_line,finding.title].join(':'),path:finding.path,line:finding.start_line,lineEnd:finding.end_line,title:finding.severity+' · '+finding.title,why:finding.body+' · confidence '+Number(finding.confidence||0).toFixed(2),ev:finding.evidence,src:'verified AI finding'});
for(const ex of explanations)add({k:'note',path:ex.path,line:ex.lineStart,lineEnd:Math.max(ex.lineEnd||0,ex.lineStart),title:ex.title,why:ex.summary,src:'AI explanation'});
const needsDecision=(it)=>it.k!=='note'&&!ack.has(it.ackId);
const open=()=>items.filter(needsDecision);

/* ---- top bar ---- */
const top=h('header','top');
const topIn=h('div','top-in');
const brand=h('div','brand');
brand.append(h('span',null,'Review report'),h('span',null,report.repository||''));
const range=h('span','range');
range.append(h('b',null,short(report.head)),document.createTextNode(' ← '),h('b',null,short(report.base)),document.createTextNode(report.generatedAt?' · generated '+new Date(report.generatedAt).toISOString().replace('T',' ').slice(0,16)+' UTC':''));
brand.append(range);
const root=document.documentElement;
const themeBtn=h('button','theme-btn');themeBtn.type='button';
const paintTheme=()=>{const light=root.dataset.theme==='light';themeBtn.textContent=light?'☾':'☀';themeBtn.setAttribute('aria-label','Switch to '+(light?'dark':'light')+' theme');themeBtn.title='Switch to '+(light?'dark':'light')+' theme'};
themeBtn.onclick=()=>{const next=root.dataset.theme==='light'?'dark':'light';root.dataset.theme=next;try{localStorage.setItem('px1:theme',next)}catch{}paintTheme()};
paintTheme();
brand.append(themeBtn);
const headline=h('div','headline');
const h1=h('h1');const verdict=h('div','verdict');
headline.append(h1,verdict);
topIn.append(brand,headline);
const checksPanel=h('div','checks');checksPanel.hidden=true;
top.append(topIn,checksPanel);

if(verification.available&&checks.length){
  const inner=h('div','checks-in');
  for(const c of checks){
    const row=h(c.url?'a':'div','check '+c.status);
    if(c.url){row.href=c.url;row.rel='noopener noreferrer'}
    row.append(h('span','dot'),h('span','name',c.name),h('span','st',c.status));
    inner.append(row);
  }
  checksPanel.append(inner);
}else if(verification.available){
  checksPanel.append(h('div','checks-note','CI verification is attached for revision '+short(verification.revision)+' with 0 recorded checks. This is not a live status feed.'));
}else{
  checksPanel.append(h('div','checks-note',verification.error||'No CI verification is attached to this report.'));
}

const plural=(n,w)=>n+' '+w+(n===1?'':'s');
function paintTop(){
  const o=open(),rules=o.filter((i)=>i.k==='rule').length,attn=o.filter((i)=>i.k==='attn').length;
  const failing=checks.filter((c)=>c.status==='failed').length;
  const pending=checks.filter((c)=>c.status==='running'||c.status==='queued').length;
  const parts=[];
  h1.textContent='';
  const reviewed=files.filter((f)=>reviewState(f.path)==='reviewed').length;
  const followUp=files.filter((f)=>reviewState(f.path)==='follow-up').length;
  h1.textContent=files.length?reviewed+' of '+plural(files.length,'file')+' marked reviewed':'No changed files';
  if(followUp)h1.textContent+=' · '+followUp+' need follow-up';
  if(failing)h1.textContent+=' · '+plural(failing,'check')+' failing';
  verdict.textContent='';
  verdict.append(h('span','chip '+(rules?'chip-danger':''),plural(rules,'rule finding')));
  verdict.append(h('span','chip '+(attn?'chip-attention':''),plural(attn,'attention flag')));
  {const n=open().filter((i)=>i.k==='finding').length;verdict.append(h('span','chip'+(n?' chip-finding':''),plural(n,'AI finding')))}
  const ci=h('button','chip '+(failing?'chip-danger':pending?'chip-attention':''));
  ci.type='button';
  ci.setAttribute('aria-expanded',String(!checksPanel.hidden));
  ci.textContent=verification.available?(checks.length?'CI '+checks.filter((c)=>c.status==='passed').length+'/'+checks.length+' passed':'CI attached · 0 checks'):verification.error?'CI unavailable':'CI not attached';
  ci.onclick=()=>{checksPanel.hidden=!checksPanel.hidden;ci.setAttribute('aria-expanded',String(!checksPanel.hidden));syncTop()};
  verdict.append(ci);
  {const n=items.filter((i)=>i.k==='note').length;verdict.append(h('span','chip'+(n?' chip-note':''),plural(n,'AI note')))}
}

/* ---- rail ---- */
const rail=h('aside','rail');rail.setAttribute('aria-label','Review navigation');
const railScaleKey='px1:rail-text-scale';
const railScales=[.85,.95,1,1.1,1.2,1.3];
let railScaleIndex=2;
try{const saved=Number(localStorage.getItem(railScaleKey));const index=railScales.indexOf(saved);if(index>=0)railScaleIndex=index}catch{}
const railTools=h('div','rail-tools');
const railToolsLabel=h('span','rail-tools-label','Sidebar text');
const zoomOut=h('button','zoom-btn','−');zoomOut.type='button';zoomOut.title='Decrease sidebar text size';zoomOut.setAttribute('aria-label','Decrease sidebar text size');
const zoomStatus=h('span','zoom-status');zoomStatus.setAttribute('aria-live','polite');
const zoomIn=h('button','zoom-btn','+');zoomIn.type='button';zoomIn.title='Increase sidebar text size';zoomIn.setAttribute('aria-label','Increase sidebar text size');
const applyRailScale=(remember)=>{
  const scale=railScales[railScaleIndex];
  rail.style.fontSize=14*scale+'px';
  rail.style.setProperty('--fs-small',12.5*scale+'px');
  rail.style.setProperty('--fs-fine',11.5*scale+'px');
  rail.style.setProperty('--fs-eyebrow',11*scale+'px');
  zoomStatus.textContent=Math.round(scale*100)+'%';
  zoomOut.disabled=railScaleIndex===0;zoomIn.disabled=railScaleIndex===railScales.length-1;
  if(remember){try{localStorage.setItem(railScaleKey,String(scale))}catch{}}
};
zoomOut.onclick=()=>{if(railScaleIndex>0){railScaleIndex--;applyRailScale(true);syncTop()}};
zoomIn.onclick=()=>{if(railScaleIndex<railScales.length-1){railScaleIndex++;applyRailScale(true);syncTop()}};
railTools.append(railToolsLabel,zoomOut,zoomStatus,zoomIn);rail.append(railTools);
const evidenceSec=h('section','evidence');
const evidenceHead=h('h2','eyebrow','Evidence & limits');
const evidenceIntro=h('p','evidence-intro','Each source is separate. None of these signals approves the pull request.');
const evidenceList=h('ul','evidence-list');
for(const source of evidence){
  const li=h('li','evidence-source');
  const sourceHead=h('div','evidence-head');
  sourceHead.append(h('span','evidence-label',source.label),h('span','evidence-state state-'+source.state,source.state.replaceAll('-',' ')));
  li.append(sourceHead,h('p','evidence-detail',source.detail));
  if(source.revision){
    const identity=h('p','evidence-revision');
    identity.append(document.createTextNode(source.base?'Comparison ': 'Revision '));
    if(source.base)identity.append(h('code',null,short(source.base)),document.createTextNode('…'));
    identity.append(h('code',null,short(source.revision)));
    li.append(identity);
  }
  for(const limit of Array.isArray(source.limits)?source.limits:[])li.append(h('p','evidence-limit',limit));
  evidenceList.append(li);
}
if(!evidence.length)evidenceList.append(h('li','evidence-source evidence-missing','Evidence metadata is unavailable in this older snapshot. Treat every source as limited.'));
evidenceSec.append(evidenceHead,evidenceIntro,evidenceList);rail.append(evidenceSec);
const progressSec=h('section','review-progress');
progressSec.append(h('h2','eyebrow','Your code review'));
const progressSummary=h('p','review-summary');progressSummary.setAttribute('role','status');
const nextUnreviewed=h('button','btn','Next unreviewed file');nextUnreviewed.type='button';
const progressHint=h('p','review-hint');
progressSec.append(progressSummary,nextUnreviewed,progressHint);rail.append(progressSec);
const inspectionSec=h('section','inspection-nav');const inspectionHead=h('h2','eyebrow','Inspection questions ');inspectionHead.append(h('span','count',inspectionQuestions.length));
const inspectionList=h('ul','inspection-list');
for(const prompt of inspectionQuestions){
  const li=h('li'),a=h('a','inspection-link');a.href='#'+prompt.id;
  a.append(h('span','t',prompt.question),h('span','w',prompt.path+(prompt.line?':'+prompt.line:'')));
  li.append(a);inspectionList.append(li);
}
if(inspectionQuestions.length||inspectionAnalysis?.truncated){
  inspectionSec.append(inspectionHead,inspectionList);
  if(inspectionAnalysis?.truncated)for(const limit of inspectionAnalysis.limits||[])inspectionSec.append(h('p','inspection-limit',limit+'.'));
  rail.append(inspectionSec);
}
const queueSec=h('section');const queueHead=h('h2','eyebrow','Automated findings ');const queueCount=h('span','count');queueHead.append(queueCount);
const queue=h('ul','queue');queueSec.append(queueHead,queue);
const filesSec=h('section');const filesHead=h('h2','eyebrow','Files ');
const seg=h('span','seg');seg.setAttribute('role','group');seg.setAttribute('aria-label','File filter');
const fAll=h('button',null,'All '+files.length),fFlag=h('button',null,'Flagged');
fAll.type=fFlag.type='button';seg.append(fAll,fFlag);filesHead.append(seg);
const search=h('input','file-search');search.type='search';search.placeholder='Search files and changed code';search.setAttribute('aria-label','Search files and changed code');
const fileList=h('ul','files');filesSec.append(filesHead,fileList);
filesSec.insertBefore(search,fileList);
rail.append(queueSec,filesSec);
let flaggedOnly=false;
let searchQuery='';

const order={rule:0,attn:1,finding:2,note:3};
function paintQueue(){
  queue.textContent='';
  queueCount.textContent=open().length+' open';
  if(!items.length){queue.append(h('p','empty',files.length?'No automated findings matched. This is not evidence that the change is safe; changed code still needs your review.':'No automated findings matched.'));return}
  [...items].sort((a,b)=>order[a.k]-order[b.k]).forEach((it)=>{
    const a=h('a','qitem'+(it.k!=='note'&&ack.has(it.ackId)?' done':''));a.href='#'+it.id;
    const row=h('div','row');row.append(chipFor(it.k),h('span','t',it.title));
    a.append(row,h('span','w',it.path+(it.line?':'+it.line:'')));
    const li=h('li');li.append(a);queue.append(li);
  });
}
const stats=files.map((f)=>{let a=0,d=0;for(const hk of f.hunks||[])for(const l of hk.lines||[]){if(l.kind==='added')a++;else if(l.kind==='removed')d++}return{a,d}});
const searchable=files.map((f,i)=>[f.path,...(f.hunks||[]).flatMap((hk)=>(hk.lines||[]).map((l)=>l.text)),...items.filter((x)=>x.fi===i).flatMap((x)=>[x.title,x.why,x.ev])].join('\n').toLowerCase());
const fileVisible=(f,i)=>{if(flaggedOnly&&!items.some((x)=>x.fi===i))return false;return !searchQuery||searchable[i].includes(searchQuery)};
function paintFiles(){
  fileList.textContent='';
  fAll.setAttribute('aria-pressed',String(!flaggedOnly));fFlag.setAttribute('aria-pressed',String(flaggedOnly));
  const flaggedCount=files.filter((f,i)=>items.some((x)=>x.fi===i)).length;
  fFlag.textContent='Flagged '+flaggedCount;
  files.forEach((f,i)=>{
    const mine=items.filter((x)=>x.fi===i);
    if(!fileVisible(f,i))return;
    const li=h('li'),link=h('a','file');link.href='#f'+i;
    const parts=f.path.split('/'),base=parts.pop(),p=h('span','p');
    if(parts.length)p.append(h('span',null,parts.join('/')+'/'));
    p.append(h('b',null,base));
    const m=h('span','m');
    for(const [kind,cls] of [['rule','r'],['attn','a'],['finding','f'],['note','n']]){const n=mine.filter((x)=>x.k===kind).length;if(n)m.append(h('i','mark '+cls,n))}
    const stateLabel={unreviewed:'Unreviewed',reviewed:'Reviewed','follow-up':'Needs follow-up'}[reviewState(f.path)];
    link.append(p,m,h('span','delta','+'+stats[i].a+' −'+stats[i].d+' · '+(STATUS[f.status]||f.status)+' · '+stateLabel));
    li.append(link);fileList.append(li);
  });
}
const paintMainFilter=()=>document.querySelectorAll('.fsec').forEach((sec)=>{const i=Number(sec.dataset.fileIndex);sec.hidden=!fileVisible(files[i],i)});
fAll.onclick=()=>{flaggedOnly=false;paintFiles();paintMainFilter()};
fFlag.onclick=()=>{flaggedOnly=true;paintFiles();paintMainFilter()};
search.addEventListener('input',()=>{searchQuery=search.value.trim().toLowerCase();paintFiles();paintMainFilter()});
let lastReviewIndex=-1;
function paintProgress(){
  const unread=files.filter((f)=>reviewState(f.path)==='unreviewed').length;
  const followUp=files.filter((f)=>reviewState(f.path)==='follow-up').length;
  progressSummary.textContent=unread+' unreviewed · '+followUp+' need follow-up';
  nextUnreviewed.disabled=unread===0;
  progressHint.textContent=(reviewStorageAvailable?'Saved in this browser for this comparison.':'Browser storage unavailable; progress lasts only on this page.')+' Your marks record review progress, not approval or proof of correctness.';
}
nextUnreviewed.onclick=()=>{
  for(let offset=1;offset<=files.length;offset++){
    const i=(lastReviewIndex+offset)%files.length;
    if(reviewState(files[i].path)!=='unreviewed')continue;
    lastReviewIndex=i;
    flaggedOnly=false;searchQuery='';search.value='';paintFiles();paintMainFilter();
    const section=document.getElementById('f'+i);section.focus({preventScroll:true});section.scrollIntoView();
    break;
  }
};

/* ---- diff ---- */
function annotation(it,fileLevel){
  const a=h('div','ann '+it.k+(fileLevel?' filelevel':''));a.id=it.id;
  const head=h('div','ann-top');
  head.append(chipFor(it.k),h('span','ann-title',it.title));
  if(fileLevel&&it.line)head.append(h('span','src','line '+it.line));
  if(it.src)head.append(h('span','src',it.src));
  if(it.k!=='note'){
    const b=h('button','btn act');b.type='button';
    const paint=()=>{const on=ack.has(it.ackId);b.textContent=on?'Acknowledged':'Acknowledge';b.setAttribute('aria-pressed',String(on));a.classList.toggle('done',on)};
    b.onclick=()=>{ack.has(it.ackId)?ack.delete(it.ackId):ack.add(it.ackId);persist();paint();paintTop();paintQueue()};
    paint();head.append(b);
  }
  a.append(head);
  if(it.why)a.append(h('p',null,it.why));
  if(it.ev)a.append(h('code','ev',it.ev));
  return a;
}
/* An AI note is a slim strip under the last line it explains, collapsed until opened. */
const rangeLabel=(it)=>it.lineEnd>it.line?'Lines '+it.line+'–'+it.lineEnd:'Line '+it.line;
function noteStrip(it){
  const a=h('div','ann note');a.id=it.id;
  const head=h('button','note-head');head.type='button';head.setAttribute('aria-expanded','false');
  const glyph=h('span','note-glyph','✦');glyph.setAttribute('aria-hidden','true');
  head.append(glyph,h('span','note-title',it.title),h('span','note-range',rangeLabel(it)),h('span','note-caret'));
  const body=h('div','note-body');body.hidden=true;
  body.append(h('p',null,it.why),h('span','src','AI explanation · generated for this commit'));
  const set=(on)=>{head.setAttribute('aria-expanded',String(on));body.hidden=!on;a.classList.toggle('open',on)};
  head.onclick=()=>set(body.hidden);
  a.open=()=>set(true);
  a.append(head,body);
  return a;
}
const openNoteFromHash=()=>{const el=location.hash.length>1?document.getElementById(location.hash.slice(1)):null;if(el&&el.open)el.open()};
addEventListener('hashchange',openNoteFromHash);

const main=h('main','main');
const renderedGroups=new Set();
files.forEach((f,i)=>{
  const sec=h('section','fsec');sec.id='f'+i;sec.dataset.fileIndex=String(i);sec.tabIndex=-1;sec.setAttribute('aria-label',f.path);
  const head=h('div','fhead');
  const group=blocks.find((block)=>block.paths.includes(f.path));
  if(group&&!renderedGroups.has(group.id)){const anchor=h('span');anchor.id=group.id;main.append(anchor);renderedGroups.add(group.id)}
  head.append(h('span','path',f.path),h('span','eyebrow',STATUS[f.status]||f.status),h('span','spacer'),h('span','mono muted','+'+stats[i].a+' −'+stats[i].d));
  const state=h('select','review-state');state.setAttribute('aria-label','Review status for '+f.path);
  for(const [value,label] of [['unreviewed','Unreviewed'],['reviewed','Reviewed'],['follow-up','Needs follow-up']]){
    const option=h('option',null,label);option.value=value;state.append(option);
  }
  state.value=reviewState(f.path);
  state.onchange=()=>{
    if(state.value==='unreviewed')reviewStates.delete(f.path);else reviewStates.set(f.path,state.value);
    lastReviewIndex=i;persistReview();paintTop();paintFiles();paintProgress();syncTop();
  };
  head.append(state);
  sec.append(head);
  const body=h('div','fbody');
  for(const prompt of inspectionQuestions.filter((question)=>question.path===f.path)){
    const card=h('article','inspection-card');card.id=prompt.id;
    const title=h('div','inspection-title');title.append(h('span','chip chip-question','Question'),h('span','src',prompt.kind));card.append(title);
    card.append(h('b','inspection-label','Observed evidence'),h('p','inspection-observation',prompt.observation));
    card.append(h('b','inspection-label','Reviewer question'),h('p','inspection-question',prompt.question));
    const links=h('div','inspection-references');
    if(prompt.line){const lineLink=h('a',null,'View diff line '+prompt.line);lineLink.href='#line-f'+i+'-n'+prompt.line;links.append(lineLink)}
    for(const ref of Array.isArray(prompt.related)?prompt.related:[]){
      if(ref.inDiff&&fileIndex.has(ref.path)){const link=h('a',null,ref.path);link.href='#f'+fileIndex.get(ref.path);links.append(link)}
      else{const related=h('span',null);related.append(h('code',null,ref.path),document.createTextNode(' · '+ref.relationship));links.append(related)}
    }
    if(links.childNodes.length)card.append(links);
    body.append(card);
  }
  const mine=items.filter((x)=>x.fi===i);
  const placed=new Set();
  const hunks=Array.isArray(f.hunks)?f.hunks:[];
  /* An annotation sits under its line when that line is in the diff; otherwise it leads the file. */
  const lineOf=(it)=>{if(!it.line)return null;for(const hk of hunks)for(const l of hk.lines||[])if(l.newLine===it.line&&l.kind!=='removed')return l;return null};
  const spanOf=(it)=>{const out=[];for(const hk of hunks)for(const l of hk.lines||[])if(l.newLine>=it.line&&l.newLine<=it.lineEnd&&l.kind!=='removed')out.push(l);return out};
  const byLine=new Map(),noteAfter=new Map(),noteSpans=new Map();
  for(const it of mine){
    if(it.k==='note'){const span=spanOf(it);if(span.length){noteSpans.set(it,span);const last=span[span.length-1];if(!noteAfter.has(last))noteAfter.set(last,[]);noteAfter.get(last).push(it);placed.add(it)}continue}
    const l=lineOf(it);if(l){if(!byLine.has(l))byLine.set(l,[]);byLine.get(l).push(it);placed.add(it)}
  }
  for(const it of mine)if(!placed.has(it)){const card=annotation(it,true);if(it.k==='note'){card.querySelector('.ann-top').append(h('span','src',rangeLabel(it)))}body.append(card)}
  const rowOf=new Map(),strips=[];
  if(hunks.length){
    for(const hk of hunks){
      const hunk=h('div','hunk');hunk.id=hk.id||'';hunk.append(h('div','hh',hk.header));
      for(const l of hk.lines||[]){
        const marks=byLine.get(l)||[];
        const row=h('div','ln '+l.kind);
        if(l.newLine)row.id='line-f'+i+'-n'+l.newLine;
        row.append(h('span','g',l.oldLine||''),h('span','g',l.newLine||''),h('span','mk',l.kind==='added'?'+':l.kind==='removed'?'−':' '),h('span','c',l.text));
        rowOf.set(l,row);hunk.append(row);
        for(const it of marks)hunk.append(annotation(it,false));
        for(const it of noteAfter.get(l)||[])strips.push([it,hunk]);
      }
      body.append(hunk);
    }
    /* Strips are placed after their last explained line once every row exists. */
    for(const [it,hunk] of strips){
      const span=noteSpans.get(it);
      rowOf.get(span[span.length-1]).after(noteStrip(it));
    }
  }else if(f.diff){body.append(h('pre',null,f.diff))}
  else body.append(h('p','none','No textual diff available.'));
  sec.append(body);main.append(sec);
});
if(!files.length)main.append(h('p','muted','No changed files.'));

/* ---- spine: every annotation at its position on the page ---- */
const spine=h('div','spine');spine.setAttribute('aria-hidden','true');
const thumb=h('div','thumb');spine.append(thumb);
function paintSpine(){
  spine.querySelectorAll('a').forEach((a)=>a.remove());
  const total=document.documentElement.scrollHeight;
  for(const it of items){
    const el=document.getElementById(it.id);if(!el)continue;
    const a=h('a',it.k);a.href='#'+it.id;a.tabIndex=-1;
    a.style.top=((el.getBoundingClientRect().top+scrollY)/total*100)+'%';
    spine.append(a);
  }
  paintThumb();
}
function paintThumb(){const total=document.documentElement.scrollHeight;thumb.style.top=(scrollY/total*100)+'%';thumb.style.height=(innerHeight/total*100)+'%'}
function syncTop(){document.documentElement.style.setProperty('--top-h',top.offsetHeight+'px');paintSpine()}

const wrap=h('div','wrap');wrap.append(rail,main,spine);
/* The divider between the side panel and the diff is a drag handle. Width is remembered;
   double-click or Home resets it. Narrow screens stack the panel and hide the handle. */
const RAIL_DEFAULT=288,RAIL_MIN=200,RAIL_MAX=520,railKey='px1:rail-width';
const handle=h('div','resize-handle');
handle.setAttribute('role','separator');handle.setAttribute('aria-orientation','vertical');
handle.setAttribute('aria-label','Resize side panel');handle.tabIndex=0;
handle.append(h('span','grip'));
wrap.append(handle);
const clampRail=(w)=>Math.round(Math.min(RAIL_MAX,Math.max(RAIL_MIN,Math.min(w,wrap.clientWidth*0.45))));
function setRailWidth(w,remember){
  w=clampRail(w);
  wrap.style.setProperty('--rail-w',w+'px');
  handle.setAttribute('aria-valuenow',String(w));handle.setAttribute('aria-valuemin',String(RAIL_MIN));handle.setAttribute('aria-valuemax',String(RAIL_MAX));
  if(remember){try{localStorage.setItem(railKey,String(w))}catch{}}
  return w;
}
let savedWidth=RAIL_DEFAULT;
try{const n=parseInt(localStorage.getItem(railKey),10);if(n>0)savedWidth=n}catch{}
let frame=0;const settle=()=>{cancelAnimationFrame(frame);frame=requestAnimationFrame(syncTop)};
handle.addEventListener('pointerdown',(e)=>{
  if(e.button!==0)return;
  e.preventDefault();handle.setPointerCapture(e.pointerId);
  document.body.classList.add('resizing');handle.classList.add('active');
  const left=wrap.getBoundingClientRect().left+parseFloat(getComputedStyle(wrap).paddingLeft);
  const move=(ev)=>{setRailWidth(ev.clientX-left,false);settle()};
  const done=()=>{
    handle.removeEventListener('pointermove',move);handle.removeEventListener('pointerup',done);handle.removeEventListener('pointercancel',done);
    document.body.classList.remove('resizing');handle.classList.remove('active');
    setRailWidth(parseFloat(wrap.style.getPropertyValue('--rail-w')),true);settle();
  };
  handle.addEventListener('pointermove',move);handle.addEventListener('pointerup',done);handle.addEventListener('pointercancel',done);
});
handle.addEventListener('dblclick',()=>{setRailWidth(RAIL_DEFAULT,true);settle()});
handle.addEventListener('keydown',(e)=>{
  const now=parseFloat(wrap.style.getPropertyValue('--rail-w'))||RAIL_DEFAULT;
  let next=null;
  if(e.key==='ArrowLeft')next=now-16;else if(e.key==='ArrowRight')next=now+16;
  else if(e.key==='Home')next=RAIL_DEFAULT;
  if(next===null)return;
  e.preventDefault();setRailWidth(next,true);settle();
});
addEventListener('resize',()=>setRailWidth(parseFloat(wrap.style.getPropertyValue('--rail-w'))||savedWidth,false));
const foot=h('div','foot');
foot.append(document.createTextNode('File review progress and finding acknowledgements are independent and browser-local. This static snapshot represents '+short(report.base)+'…'+short(report.head)+' and cannot know the current live PR head or check status. Confirm both in GitHub before approving. Bundle checksums detect file changes; they do not prove authorship. '));
const checksumLink=h('a',null,'View checksums');checksumLink.href='checksums.sha256';foot.append(checksumLink);
app.textContent='';
app.append(top,wrap,foot);
setRailWidth(savedWidth,false);
applyRailScale(false);
paintTop();paintQueue();paintFiles();paintProgress();syncTop();
/* A deep link such as #ann-3 opens its note and scrolls to it once the page is built. */
openNoteFromHash();
if(location.hash.length>1)document.getElementById(location.hash.slice(1))?.scrollIntoView();
addEventListener('scroll',paintThumb,{passive:true});
addEventListener('resize',syncTop);
document.fonts?.ready.then(syncTop);
})();
