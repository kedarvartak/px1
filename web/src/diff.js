// web/src/diff.js
// Git diff view for the active tab: renders the file's unified diff against
// HEAD in a dedicated overlay (like the Markdown preview), in either a
// side-by-side split layout (default) or a single-column unified layout.
// Unlike the code viewport this is not virtualized -- a file's own diff is
// bounded in size, so a plain DOM render is simple and fast enough.
import { $, S, doc_, esc, api, apiPost } from './state.js';
import { syncPreview } from './markdown.js';
import { setStatusNote, updateStatus } from './status.js';

export const diffview = $('#diffview');
const diffContent = $('#diffcontent');

let shown = null; // doc the diff view is currently showing, null while hidden
let pinHandler = null;

export function setPinHandler(fn) { pinHandler = fn; }

// d.diffMode is 'split' | 'unified' | null (off), per tab. The layout last
// picked (split vs unified) is remembered globally as the default for the
// next file entering diff view.
export function setLayoutPref(mode) {
  try { localStorage.setItem('px1.diffLayout', mode); } catch {}
}

export function layoutPref() {
  try { return localStorage.getItem('px1.diffLayout') || 'split'; } catch { return 'split'; }
}

function diffMode(d = doc_()) {
  return (d && d.diffMode) || null;
}

/* Show or hide the diff overlay to match the active tab, and re-render when
   the layout (split/unified) changes while already showing the same doc --
   switching layout doesn't change which doc is "shown", so that alone can't
   be the signal to redraw. Call whenever either might have changed. */
export function syncDiffView() {
  const d = doc_();
  const want = (d && d.diffMode) ? d : null;
  if (want !== shown) {
    shown = want;
    diffview.hidden = !want;
    if (want) drawDiff(want);
    else diffContent.replaceChildren();
  } else if (want && want.diffHunks !== undefined) {
    renderDiff(want);
  }
}

// Read by a reload, which swaps the doc and so redraws the diff from the top.
export function diffScrollTop() {
  return diffview.hidden ? 0 : diffview.scrollTop;
}

export async function toggleDiff() {
  if (!S.meta?.git) return;
  const d = doc_();
  if (!d) return;
  if (!d.diffMode && !d.diffAvailable) { setStatusNote('No diff — clean file or not a git repo', 4000); return; }
  setDiffMode(d.diffMode ? 'source' : (layoutPref() || 'split'));
}

export async function setDiffMode(mode) {
  const d = doc_();
  if (!d) return;
  if (mode !== 'source' && !d.diffAvailable) { setStatusNote('No diff — clean file or not a git repo', 4000); return; }
  if (mode === 'source') {
    d.diffMode = null;
    d.diffDismissed = true;
  } else {
    d.diffMode = mode;
    d.diffDismissed = false;
    setLayoutPref(mode);
  }
  syncPreview(); // markdown preview and diff view are mutually exclusive
  syncDiffView();
  updateStatus();
}

// Opens the review queue's authoritative view: task-start snapshot versus the
// current file, not Git HEAD. This keeps pre-existing local work out of the
// agent review surface.
export async function openReviewDiff(path) {
  const d = S.tabs.find(t => t.path === path);
  if (!d) return;
  d.diffSource = 'review';
  d.diffText = undefined;
  d.diffHunks = undefined;
  d.diffMode = layoutPref() || 'split';
  d.diffDismissed = false;
  syncPreview();
  syncDiffView();
  await drawDiff(d);
  updateStatus();
}

async function drawDiff(d) {
  if (d.diffText === undefined) {
    diffContent.replaceChildren();
    try {
      const endpoint = d.diffSource === 'review' ? '/api/review/diff' : '/api/diff';
      d.diffReq = d.diffReq || api(endpoint, { path: d.path });
      const j = await d.diffReq;
      d.diffText = j.diff || '';
      d.diffHunks = parseDiff(d.diffText);
    } catch (e) {
      d.diffText = '';
      d.diffHunks = [];
      setStatusNote('No diff: ' + e.message, 4000);
    } finally {
      d.diffReq = null;
    }
    if (shown !== d) return;
  }
  renderDiff(d);
  if (d.diffScroll) {
    diffview.scrollTop = d.diffScroll;
    d.diffScroll = 0;
  }
}

function renderDiff(d) {
  diffContent.replaceChildren();
  if (!d.diffHunks || !d.diffHunks.length) {
    const p = document.createElement('div');
    p.className = 'diff-empty';
    p.textContent = 'No changes against HEAD.';
    diffContent.append(p);
    return;
  }
  const frag = document.createDocumentFragment();
  const tables = [];
  for (const hunk of d.diffHunks) {
    frag.append(hunkHeader(d, hunk));
    const table = d.diffMode === 'unified' ? unifiedTable(hunk) : splitTable(hunk);
    tables.push(table);
    frag.append(table);
  }
  diffContent.append(frag);
  if (d.diffSource === 'review') placePins(d, tables);
  syncDiffAgentTargets();
}

function rowLines(row) {
  const els = row.matches('[data-l]') ? [row] : [...row.querySelectorAll('[data-l]')];
  return els.map(el => +el.dataset.l);
}

function rowOld(row) {
  const els = row.matches('[data-o]') ? [row] : [...row.querySelectorAll('[data-o]')];
  return els.map(el => +el.dataset.o);
}

function rowCurrent(row) {
  const els = row.matches('[data-l],[data-at]') ? [row] : [...row.querySelectorAll('[data-l],[data-at]')];
  return els.map(el => +(el.dataset.l || el.dataset.at));
}

function placePins(d, tables) {
  const pins = (S.reviewPins || []).filter(p => p.path === d.path);
  const loose = [];
  for (const hit of (S.reviewRuleHits || []).filter(h => h.path === d.path)) {
    let target = null;
    for (const table of tables) {
      for (const row of table.children) {
        if (rowLines(row).includes(hit.line)) target = row;
      }
      if (target) break;
    }
    if (target) target.after(ruleHitCard(hit));
    else loose.push(ruleHitCard(hit));
  }
  for (const ch of (S.reviewChallenges || []).filter(c => c.path === d.path)) {
    let target = null;
    const current = [];
    for (const table of tables) {
      for (const row of table.children) {
        if (rowOld(row).some(l => l >= ch.baselineFrom && l <= ch.baselineTo)) {
          target = row;
          current.push(...rowCurrent(row));
        }
      }
      if (target) break;
    }
    const lines = current.filter(n => n > 0);
    const at = lines.length ? { l1: Math.min(...lines), l2: Math.max(...lines) } : { l1: 1, l2: 1 };
    const card = challengeCard(ch, at);
    if (target) target.after(card);
    else loose.push(card);
  }
  for (const pin of pins) {
    let target = null;
    for (const table of tables) {
      for (const row of table.children) {
        if (rowLines(row).some(l => l >= pin.lineStart && l <= pin.lineEnd)) target = row;
      }
      if (target) break;
    }
    if (target) target.after(pinCard(pin));
    else loose.push(pinCard(pin));
  }
  if (loose.length) {
    const box = document.createElement('div');
    box.className = 'pin-loose';
    box.append(...loose);
    diffContent.prepend(box);
  }
}

function ruleHitCard(hit) {
  const card = document.createElement('div');
  card.className = 'pin pin-rule open';
  card.dataset.ruleHit = hit.key + '@' + hit.line;
  const head = document.createElement('div');
  head.className = 'pin-head';
  const mark = document.createElement('span');
  mark.className = 'pin-mark';
  mark.textContent = '⚑';
  const text = document.createElement('span');
  text.className = 'pin-decision';
  text.textContent = hit.message;
  const ref = document.createElement('span');
  ref.className = 'pin-ref';
  ref.textContent = (hit.source === 'team' ? 'team rule' : 'your rule') + ' · L' + hit.line;
  head.append(mark, text, ref);
  const acts = document.createElement('div');
  acts.className = 'pin-acts pin-body';
  acts.append(
    pinButton('Add as comment', 'rule-comment', hit),
    pinButton('Ignore here', 'rule-dismiss', hit),
  );
  if (hit.source !== 'team') acts.append(pinButton('Disable rule', 'rule-disable', hit));
  card.append(head, acts);
  return card;
}

export function revealRuleHit(key, line) {
  const card = diffContent.querySelector(`[data-rule-hit="${CSS.escape(key + '@' + line)}"]`);
  if (!card) return false;
  card.scrollIntoView({ block: 'center' });
  return true;
}

function challengeCard(ch, at) {
  const card = document.createElement('div');
  card.className = 'pin pin-challenge';
  card.dataset.challengeId = ch.id;
  const head = document.createElement('button');
  head.className = 'pin-head';
  const mark = document.createElement('span');
  mark.className = 'pin-mark';
  mark.textContent = '⟲';
  const text = document.createElement('span');
  text.className = 'pin-decision';
  text.textContent = 'Reverses a past decision: ' + ch.decision;
  const ref = document.createElement('span');
  ref.className = 'pin-ref';
  ref.textContent = new Date(ch.recordedAt).toLocaleDateString();
  head.append(mark, text, ref);
  const body = document.createElement('div');
  body.className = 'pin-body';
  const lines = [];
  if (ch.why) lines.push(['why', ch.why]);
  if (ch.alternatives?.length) lines.push(['not', ch.alternatives.join(' · ')]);
  for (const [k, v] of lines) {
    const el = document.createElement('div');
    el.className = 'pin-line';
    el.innerHTML = '<span>' + k + '</span>' + esc(v);
    body.append(el);
  }
  const acts = document.createElement('div');
  acts.className = 'pin-acts';
  acts.append(
    pinButton('Ask agent to keep it', 'challenge-ask', { ...ch, at }),
    pinButton('Decision changed', 'challenge-supersede', ch),
    pinButton('Still holds', 'challenge-dismiss', ch),
  );
  body.append(acts);
  head.addEventListener('click', () => { body.hidden = !body.hidden; card.classList.toggle('open', !body.hidden); });
  card.classList.add('open');
  card.append(head, body);
  return card;
}

export function revealChallenge(id) {
  const card = diffContent.querySelector(`[data-challenge-id="${CSS.escape(id)}"]`);
  if (!card) return false;
  card.scrollIntoView({ block: 'center' });
  return true;
}

function lineLabel(pin) {
  return pin.lineStart === pin.lineEnd ? 'L' + pin.lineStart : 'L' + pin.lineStart + '–' + pin.lineEnd;
}

function pinButton(label, action, pin, choice) {
  const b = document.createElement('button');
  b.className = 'pin-act pin-act-' + action;
  b.textContent = label;
  b.addEventListener('click', e => {
    e.stopPropagation();
    pinHandler?.(action, pin, choice);
  });
  return b;
}

export function pinCard(pin) {
  const card = document.createElement('div');
  card.className = 'pin impact-' + pin.impact + ' pin-' + pin.status + (pin.stale ? ' pin-stale' : '');
  card.dataset.pinId = pin.id;
  const head = document.createElement('button');
  head.className = 'pin-head';
  const mark = document.createElement('span');
  mark.className = 'pin-mark';
  mark.textContent = '◆';
  const text = document.createElement('span');
  text.className = 'pin-decision';
  text.textContent = pin.status === 'switched' && pin.choice ? pin.decision + ' → ' + pin.choice : pin.decision;
  const ref = document.createElement('span');
  ref.className = 'pin-ref';
  ref.textContent = lineLabel(pin);
  head.append(mark, text, ref);
  const state = pin.stale ? 'lines changed' : pin.status === 'proposed' ? '' : pin.status;
  if (state) {
    const tag = document.createElement('span');
    tag.className = 'pin-tag';
    tag.textContent = state;
    head.append(tag);
  }
  const body = document.createElement('div');
  body.className = 'pin-body';
  body.hidden = true;
  if (pin.why) {
    const why = document.createElement('div');
    why.className = 'pin-line';
    why.innerHTML = '<span>why</span>' + esc(pin.why);
    body.append(why);
  }
  if (pin.alternatives?.length) {
    const not = document.createElement('div');
    not.className = 'pin-line';
    not.innerHTML = '<span>not</span>' + pin.alternatives.map(esc).join(' · ');
    body.append(not);
  }
  const acts = document.createElement('div');
  acts.className = 'pin-acts';
  if (pin.status === 'proposed') acts.append(pinButton('Accept', 'accept', pin));
  else acts.append(pinButton('Reopen', 'reopen', pin));
  acts.append(pinButton('Ask', 'ask', pin));
  if (!pin.stale && pin.status !== 'switched') {
    for (const alt of pin.alternatives || []) acts.append(pinButton('Switch → ' + alt, 'switch', pin, alt));
  }
  body.append(acts);
  head.addEventListener('click', () => { body.hidden = !body.hidden; card.classList.toggle('open', !body.hidden); });
  card.append(head, body);
  return card;
}

export function revealPin(id) {
  const card = diffContent.querySelector(`[data-pin-id="${CSS.escape(id)}"]`);
  if (!card) return false;
  card.querySelector('.pin-body').hidden = false;
  card.classList.add('open');
  card.scrollIntoView({ block: 'center' });
  return true;
}

export function syncDiffAgentTargets() {
  if (!diffview || diffview.hidden) return;
  const d = doc_();
  if (!d) return;
  const ranges = (S.agentTargets || []).filter(t => t.path === d.path);
  for (const el of diffview.querySelectorAll('[data-l]')) {
    const l = +el.dataset.l;
    const inAgent = ranges.some(r => l >= r.l1 && l <= r.l2);
    el.classList.toggle('agent-sel', inAgent);
  }
}

function hunkHeader(d, hunk) {
  const el = document.createElement('div');
  el.className = 'diff-hunk-head';
  const ref = document.createElement('span');
  ref.textContent = '@@ -' + hunk.oldStart + ' +' + hunk.newStart + ' @@';
  el.append(ref);
  if (d.diffSource === 'review') {
    const hasAdd = hunk.rows.some(row => row.type === 'add');
    const hasDel = hunk.rows.some(row => row.type === 'del');
    if (hasAdd && hasDel) {
      const button = document.createElement('button');
      button.className = 'diff-hunk-revert';
      button.textContent = 'Revert hunk';
      button.title = 'Restore this hunk to the task-start baseline';
      button.addEventListener('click', () => revertReviewHunk(d, hunk));
      el.append(button);
    } else {
      const note = document.createElement('span');
      note.className = 'diff-hunk-patch-note';
      note.textContent = 'Use Patch Mode';
      note.title = hasAdd ? 'Pure insertion: select the added lines and use Patch Mode to remove them.' : hasDel ? 'Pure deletion: use Patch Mode to restore the deleted lines.' : 'This hunk cannot be restored automatically.';
      el.append(note);
    }
  }
  return el;
}

async function revertReviewHunk(d, hunk) {
  const item = S.review?.queue?.items?.find(x => x.path === d.path);
  if (!item?.currentHash) {
    setStatusNote('This file is no longer current in the review queue', 4000);
    return;
  }
  const oldCount = hunk.rows.filter(r => r.oldLine !== undefined).length;
  const newCount = hunk.rows.filter(r => r.newLine !== undefined).length;
  if (!oldCount || !newCount || !hunk.rows.some(r => r.type === 'add') || !hunk.rows.some(r => r.type === 'del')) {
    setStatusNote('This edge-case hunk needs Patch Mode', 4000);
    return;
  }
  try {
    await apiPost('/api/review/revert-hunk', undefined, {
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        path: d.path,
        currentLineStart: hunk.newStart,
        currentLineEnd: hunk.newStart + newCount - 1,
        baselineLineStart: hunk.oldStart,
        baselineLineEnd: hunk.oldStart + oldCount - 1,
        expectedHash: item.currentHash,
      }),
    });
    await api('/api/reindex');
    d.diffText = undefined;
    d.diffHunks = undefined;
    await drawDiff(d);
    S.review = await api('/api/review/session');
    setStatusNote('Hunk restored to task baseline', 4000);
  } catch (e) { setStatusNote('Hunk restore failed: ' + e.message, 5000); }
}

/* ---------- unified diff parsing ---------- */

const HUNK_RE = /^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@[ \t]?(.*)$/;

// Parses a unified diff (as returned by `git diff`) into hunks, each a flat
// list of rows tagged ctx/add/del carrying old- and/or new-file line numbers.
// File headers (diff --git, index, ---, +++) are skipped: nothing before the
// first @@ is kept.
function parseDiff(text) {
  if (!text) return [];
  const hunks = [];
  let cur = null, oldLine = 0, newLine = 0;
  for (const line of text.split('\n')) {
    const m = HUNK_RE.exec(line);
    if (m) {
      oldLine = +m[1];
      newLine = +m[3];
      cur = { oldStart: oldLine, newStart: newLine, section: m[5] || '', rows: [] };
      hunks.push(cur);
      continue;
    }
    if (!cur || line === '' || line.startsWith('\\')) continue; // trailing split artifact, pre-hunk header, or "\ No newline..."
    const c = line[0], body = line.slice(1);
    if (c === '+') cur.rows.push({ type: 'add', newLine: newLine++, text: body });
    // A deletion has no line on disk; at is the working-tree line it sat before.
    else if (c === '-') cur.rows.push({ type: 'del', oldLine: oldLine++, at: newLine, text: body });
    else cur.rows.push({ type: 'ctx', oldLine: oldLine++, newLine: newLine++, text: body });
  }
  return hunks;
}

/* ---------- unified layout: one row per diff line ---------- */

function unifiedTable(hunk) {
  const table = document.createElement('div');
  table.className = 'diff-table diff-unified';
  for (const row of hunk.rows) {
    const r = document.createElement('div');
    r.className = 'diff-row diff-' + row.type;
    anchor(r, row);
    r.append(
      lineCell(row.type === 'add' ? '' : row.oldLine),
      lineCell(row.type === 'del' ? '' : row.newLine),
      markerCell(row.type),
      codeCell(row.text),
    );
    table.append(r);
  }
  return table;
}

/* ---------- split layout: deletions and additions paired side by side ---------- */

function splitTable(hunk) {
  const table = document.createElement('div');
  table.className = 'diff-table diff-split';
  for (const pair of pairRows(hunk.rows)) {
    const r = document.createElement('div');
    r.className = 'diff-row-pair';
    r.append(splitSide(pair.left, 'left'), splitSide(pair.right, 'right'));
    table.append(r);
  }
  return table;
}

// Walks a hunk's flat row list, pairing each run of deletions with the run of
// additions that immediately follows it (a "changed" block) index-by-index,
// padding the shorter side with blanks. Context rows go straight across.
function pairRows(rows) {
  const pairs = [];
  let i = 0;
  while (i < rows.length) {
    const row = rows[i];
    if (row.type === 'ctx') { pairs.push({ left: row, right: row }); i++; continue; }
    let dels = [], adds = [];
    while (i < rows.length && rows[i].type === 'del') dels.push(rows[i++]);
    while (i < rows.length && rows[i].type === 'add') adds.push(rows[i++]);
    const n = Math.max(dels.length, adds.length);
    for (let k = 0; k < n; k++) pairs.push({ left: dels[k] || null, right: adds[k] || null });
  }
  return pairs;
}

function splitSide(row, side) {
  const el = document.createElement('div');
  el.className = 'diff-side diff-side-' + side + (row ? ' diff-' + row.type : ' diff-blank');
  if (!row) { el.append(lineCell(''), markerCell(''), codeCell('')); return el; }
  const ln = side === 'left' ? row.oldLine : row.newLine;
  anchor(el, row);
  el.append(lineCell(ln), markerCell(row.type), codeCell(row.text));
  return el;
}

/* Stamps where a row points in the working tree, so a selection on it can be
   edited. Context and added lines have a line on disk (data-l), which a context
   line shares across both sides of the split. A deleted line has none, only the
   place it used to be (data-at). */
function anchor(el, row) {
  if (row.oldLine !== undefined) el.dataset.o = row.oldLine;
  if (row.newLine !== undefined) el.dataset.l = row.newLine;
  else if (row.at !== undefined) el.dataset.at = row.at;
}

function lineCell(n) {
  const el = document.createElement('div');
  el.className = 'diff-ln';
  el.textContent = n === '' || n === undefined ? '' : String(n);
  return el;
}

const MARKS = { add: '+', del: '-', ctx: '' };

function markerCell(type) {
  const el = document.createElement('div');
  el.className = 'diff-mk';
  el.textContent = MARKS[type] || '';
  return el;
}

function codeCell(text) {
  const el = document.createElement('div');
  el.className = 'diff-code';
  el.innerHTML = esc(text || '') || '&nbsp;';
  return el;
}

export function initDiff() {
  const sw = $('#diff-switch');
  if (!sw) return;
  sw.addEventListener('mousedown', e => {
    if (!e.target.closest('button')) e.preventDefault();
  });
  // Each half of the switch names a view, so a click shows that view rather than toggling.
  $('#diff-source')?.addEventListener('click', e => {
    e.stopPropagation();
    setDiffMode('source');
  });
  $('#diff-btn')?.addEventListener('click', e => {
    e.stopPropagation();
    setDiffMode(doc_()?.diffMode || layoutPref());
  });
  const menu = $('#diff-menu');
  if (menu) {
    menu.addEventListener('click', e => {
      const item = e.target.closest('[data-diff-opt]');
      if (!item) return;
      e.stopPropagation();
      setDiffMode(item.dataset.diffOpt);
      item.blur();
    });
  }
}
