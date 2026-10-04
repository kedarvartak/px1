(()=>{
const report=JSON.parse(document.getElementById('report-data').textContent);
const app=document.getElementById('app');
const files=Array.isArray(report.files)?report.files:[];
const ruleHits=Array.isArray(report.ruleHits)?report.ruleHits:[];
const attention=Array.isArray(report.attention)?report.attention:[];
const explanations=Array.isArray(report.explanations)?report.explanations:[];
const verification=report.verification||{checks:[]};
const checks=Array.isArray(verification.checks)?verification.checks:[];

const h=(tag,cls,txt)=>{const n=document.createElement(tag);if(cls)n.className=cls;if(txt!=null)n.textContent=String(txt);return n};
const short=(sha)=>String(sha||'').slice(0,7);
const STATUS={A:'added',M:'modified',D:'deleted',R:'renamed',C:'copied',T:'type changed'};
const KIND={rule:'Rule',attn:'Attention',note:'Note'};
const CHIP={rule:'chip-danger',attn:'chip-attention',note:'chip-note'};
const chipFor=(k)=>h('span','chip '+CHIP[k],KIND[k]);

/* Acknowledgements live in this browser only. The key predates the redesign;
   keeping it means earlier acknowledgements still apply. */
const ackKey='px1:ack:'+report.repository+':'+report.head;
let ack=new Set();
try{const saved=JSON.parse(localStorage.getItem(ackKey)||'[]');if(Array.isArray(saved))ack=new Set(saved.filter((k)=>typeof k==='string'))}catch{}
const persist=()=>{try{localStorage.setItem(ackKey,JSON.stringify([...ack].sort()))}catch{}};

/* One list of everything worth a reader's attention, anchored to a file. */
const items=[];
const fileIndex=new Map(files.map((f,i)=>[f.path,i]));
const add=(it)=>{if(fileIndex.has(it.path)){it.fi=fileIndex.get(it.path);it.id='ann-'+items.length;items.push(it)}};
for(const hit of ruleHits)add({k:'rule',ackId:hit.key,path:hit.path,line:hit.line,title:hit.message,why:'Team rule '+hit.ruleId+(hit.origin?' ('+hit.origin+')':'')+'.',ev:hit.text,src:hit.source});
for(const flag of attention)add({k:'attn',ackId:flag.id,path:flag.path,line:flag.line||0,title:flag.title,why:flag.reason,ev:flag.evidence,src:'rule '+flag.rule});
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
const paintTheme=()=>{const light=root.dataset.theme==='light';themeBtn.textContent=light?'Dark theme':'Light theme';themeBtn.setAttribute('aria-label','Switch to '+(light?'dark':'light')+' theme')};
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
  if(o.length)h1.textContent=plural(o.length,'place')+(o.length===1?' needs':' need')+' a decision';
  else h1.textContent='Nothing open';
  if(failing)h1.textContent+=(o.length?', and ':', but ')+plural(failing,'check')+(failing===1?' is':' are')+' failing';
  verdict.textContent='';
  verdict.append(h('span','chip '+(rules?'chip-danger':''),plural(rules,'rule finding')));
  verdict.append(h('span','chip '+(attn?'chip-attention':''),plural(attn,'attention flag')));
  const ci=h('button','chip '+(failing?'chip-danger':pending?'chip-attention':''));
  ci.type='button';
  ci.setAttribute('aria-expanded',String(!checksPanel.hidden));
  ci.textContent=verification.available&&checks.length?'CI '+checks.filter((c)=>c.status==='passed').length+'/'+checks.length+' passed':'CI not attached';
  ci.onclick=()=>{checksPanel.hidden=!checksPanel.hidden;ci.setAttribute('aria-expanded',String(!checksPanel.hidden));syncTop()};
  verdict.append(ci);
  {const n=items.filter((i)=>i.k==='note').length;verdict.append(h('span','chip'+(n?' chip-note':''),plural(n,'AI note')))}
}

/* ---- rail ---- */
const rail=h('aside','rail');rail.setAttribute('aria-label','Review navigation');
const queueSec=h('section');const queueHead=h('h2','eyebrow','Review queue ');const queueCount=h('span','count');queueHead.append(queueCount);
const queue=h('ul','queue');queueSec.append(queueHead,queue);
const filesSec=h('section');const filesHead=h('h2','eyebrow','Files ');
const seg=h('span','seg');seg.setAttribute('role','group');seg.setAttribute('aria-label','File filter');
const fAll=h('button',null,'All '+files.length),fFlag=h('button',null,'Flagged');
fAll.type=fFlag.type='button';seg.append(fAll,fFlag);filesHead.append(seg);
const fileList=h('ul','files');filesSec.append(filesHead,fileList);
rail.append(queueSec,filesSec);
let flaggedOnly=false;

const order={rule:0,attn:1,note:2};
function paintQueue(){
  queue.textContent='';
  queueCount.textContent=open().length+' open';
  if(!items.length){queue.append(h('p','empty','Nothing to review beyond the diff.'));return}
  [...items].sort((a,b)=>order[a.k]-order[b.k]).forEach((it)=>{
    const a=h('a','qitem'+(it.k!=='note'&&ack.has(it.ackId)?' done':''));a.href='#'+it.id;
    const row=h('div','row');row.append(chipFor(it.k),h('span','t',it.title));
    a.append(row,h('span','w',it.path+(it.line?':'+it.line:'')));
    const li=h('li');li.append(a);queue.append(li);
  });
}
const stats=files.map((f)=>{let a=0,d=0;for(const hk of f.hunks||[])for(const l of hk.lines||[]){if(l.kind==='added')a++;else if(l.kind==='removed')d++}return{a,d}});
function paintFiles(){
  fileList.textContent='';
  fAll.setAttribute('aria-pressed',String(!flaggedOnly));fFlag.setAttribute('aria-pressed',String(flaggedOnly));
  const flaggedCount=files.filter((f,i)=>items.some((x)=>x.fi===i)).length;
  fFlag.textContent='Flagged '+flaggedCount;
  files.forEach((f,i)=>{
    const mine=items.filter((x)=>x.fi===i);
    if(flaggedOnly&&!mine.length)return;
    const li=h('li'),link=h('a','file');link.href='#f'+i;
    const parts=f.path.split('/'),base=parts.pop(),p=h('span','p');
    if(parts.length)p.append(h('span',null,parts.join('/')+'/'));
    p.append(h('b',null,base));
    const m=h('span','m');
    for(const [kind,cls] of [['rule','r'],['attn','a'],['note','n']]){const n=mine.filter((x)=>x.k===kind).length;if(n)m.append(h('i','mark '+cls,n))}
    link.append(p,m,h('span','delta','+'+stats[i].a+' −'+stats[i].d+' · '+(STATUS[f.status]||f.status)));
    li.append(link);fileList.append(li);
  });
}
fAll.onclick=()=>{flaggedOnly=false;paintFiles()};
fFlag.onclick=()=>{flaggedOnly=true;paintFiles()};

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
/* An AI note is a slim strip under the last line it explains. Opening it tints the
   whole explained range in the diff, so the code and the reading stay together. */
const rangeLabel=(it)=>it.lineEnd>it.line?'Lines '+it.line+'–'+it.lineEnd:'Line '+it.line;
function noteStrip(it,rows){
  const a=h('div','ann note');a.id=it.id;
  const head=h('button','note-head');head.type='button';head.setAttribute('aria-expanded','false');
  const glyph=h('span','note-glyph','✦');glyph.setAttribute('aria-hidden','true');
  head.append(glyph,h('span','note-title',it.title),h('span','note-range',rangeLabel(it)),h('span','note-caret'));
  const body=h('div','note-body');body.hidden=true;
  body.append(h('p',null,it.why),h('span','src','AI explanation · generated for this commit'));
  const set=(on)=>{head.setAttribute('aria-expanded',String(on));body.hidden=!on;a.classList.toggle('open',on);rows.forEach((r)=>r.classList.toggle('n-on',on))};
  head.onclick=()=>set(body.hidden);
  head.addEventListener('mouseenter',()=>rows.forEach((r)=>r.classList.add('n-hover')));
  head.addEventListener('mouseleave',()=>rows.forEach((r)=>r.classList.remove('n-hover')));
  a.open=()=>set(true);
  a.append(head,body);
  return a;
}
const openNoteFromHash=()=>{const el=location.hash.length>1?document.getElementById(location.hash.slice(1)):null;if(el&&el.open)el.open()};
addEventListener('hashchange',openNoteFromHash);

const main=h('main','main');
files.forEach((f,i)=>{
  const sec=h('section','fsec');sec.id='f'+i;
  const head=h('div','fhead');
  head.append(h('span','path',f.path),h('span','eyebrow',STATUS[f.status]||f.status),h('span','spacer'),h('span','mono muted','+'+stats[i].a+' −'+stats[i].d));
  sec.append(head);
  const body=h('div','fbody');
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
        const strongest=marks.length?marks.slice().sort((a,b)=>order[a.k]-order[b.k])[0].k:'';
        const row=h('div','ln '+l.kind+(strongest?' k-'+strongest:''));
        row.append(h('span','bar'),h('span','g',l.oldLine||''),h('span','g',l.newLine||''),h('span','mk',l.kind==='added'?'+':l.kind==='removed'?'−':' '),h('span','c',l.text));
        rowOf.set(l,row);hunk.append(row);
        for(const it of marks)hunk.append(annotation(it,false));
        for(const it of noteAfter.get(l)||[])strips.push([it,hunk]);
      }
      body.append(hunk);
    }
    /* Strips are placed after their last explained line once every row exists. */
    for(const [it,hunk] of strips){
      const span=noteSpans.get(it),rows=span.map((l)=>rowOf.get(l));
      rows.forEach((r,i)=>{r.classList.add('n-range');if(i===0)r.classList.add('n-first');if(i===rows.length-1)r.classList.add('n-last')});
      rows[rows.length-1].after(noteStrip(it,rows));
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
const foot=h('div','foot','Acknowledgements are saved in this browser only and apply to commit '+short(report.head)+'.');
app.textContent='';
app.append(top,wrap,foot);
paintTop();paintQueue();paintFiles();syncTop();
/* A deep link such as #ann-3 opens its note and scrolls to it once the page is built. */
openNoteFromHash();
if(location.hash.length>1)document.getElementById(location.hash.slice(1))?.scrollIntoView();
addEventListener('scroll',paintThumb,{passive:true});
addEventListener('resize',syncTop);
document.fonts?.ready.then(syncTop);
})();
