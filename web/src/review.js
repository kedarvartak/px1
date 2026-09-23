import { $, esc, S, api, apiPost } from './state.js';
import { openFile } from './tabs.js';
import { reloadOpenTabs } from './tabs.js';
import { drawTree, treeEl } from './tree.js';
import { openReviewDiff, setPinHandler, syncDiffView, revealPin, revealChallenge, revealRuleHit } from './diff.js';
import { showToast } from './ui.js';
import { openSettings } from './settings.js';
import { hideSelectionBar, setReviewCommentHandler, setReviewPatchHandler } from './selbar.js';
import { switchWorktree } from './worktree.js';

const queueEl = $('#review-queue');
const tree = $('#tree');
let shown = false;
let commentTarget = null;
const commentBox = $('#review-commentbox');
const commentInput = $('#review-comment-input');
const patchBox = $('#review-patchbox');
const patchInput = $('#review-patch-input');
let patchTarget = null;
let explaining = false;
let sendingRules = false;
let rulesOpen = false;
let pinSig = '';
let ruleTarget = null;
const ruleBox = $('#review-rulebox');

const pending = item => item.state === 'unreviewed' || item.state === 'stale' || item.state === 'blocked';

export async function refreshReviewQueue() {
  try {
    S.review = await api('/api/review/session');
  } catch {
    S.review = null;
  }
  try {
    const j = S.review?.active ? await api('/api/review/comments') : { comments: [] };
    S.reviewComments = j.comments || [];
  } catch { S.reviewComments = []; }
  try {
    const j = S.review?.active ? await api('/api/review/pins') : { pins: [], explain: {} };
    S.reviewPins = j.pins || [];
    S.reviewExplain = j.explain || {};
  } catch { S.reviewPins = []; S.reviewExplain = {}; }
  try {
    const j = S.review?.active ? await api('/api/review/rule-hits') : { hits: [] };
    S.reviewRuleHits = j.hits || [];
  } catch { S.reviewRuleHits = []; }
  try { S.reviewRules = (await api('/api/rules')) || { rules: [] }; } catch { S.reviewRules = { rules: [] }; }
  try {
    const j = S.review?.active ? await api('/api/review/challenges') : { challenges: [] };
    S.reviewChallenges = j.challenges || [];
  } catch { S.reviewChallenges = []; }
  try { S.reviewChecks = S.review?.active ? await api('/api/review/checks') : { commands: {}, jobs: [] }; } catch { S.reviewChecks = { commands: {}, jobs: [] }; }
  try { S.reviewInbox = (await api('/api/review/inbox')).items || []; } catch { S.reviewInbox = []; }
  drawReviewQueue();
  const sig = JSON.stringify([S.reviewPins, S.reviewChallenges, S.reviewRuleHits]);
  if (sig !== pinSig) {
    pinSig = sig;
    syncDiffView();
  }
}

function ruleHitsMarkup() {
  const hits = S.reviewRuleHits || [];
  if (!hits.length) return '';
  const rows = hits.map(h => `<button class="review-pin review-rulehit" data-review-rulehit="${esc(h.key)}" data-line="${h.line}" data-path="${esc(h.path)}" title="${esc(h.text)}"><span class="review-pin-mark">⚑</span><span class="review-pin-text">${esc(h.message)}</span><span class="review-pin-ref">${esc(h.path.split('/').pop())}:${h.line}</span></button>`).join('');
  return `<div class="review-pins review-rulehits"><div class="review-pins-head"><strong>Rule hits</strong><span>${hits.length}</span><button class="review-explain" data-review-rulehits-send ${sendingRules ? 'disabled' : ''}>${sendingRules ? 'Sending…' : 'Send to agent'}</button></div><div class="review-pin-list">${rows}</div></div>`;
}

function rulesMarkup() {
  const rules = S.reviewRules?.rules || [];
  const err = S.reviewRules?.error ? `<div class="review-rule-error">${esc(S.reviewRules.error)}</div>` : '';
  if (!rules.length && !err) return '';
  const rows = rules.map(r => `<div class="review-rule${r.enabled ? '' : ' off'}"><span class="review-rule-text" title="${esc(r.pattern + (r.glob ? '  in ' + r.glob : ''))}">${esc(r.message)}</span><span class="review-rule-meta">${r.source === 'team' ? 'team' : r.hits + ' hit' + (r.hits === 1 ? '' : 's')}</span>${r.source === 'team' ? '' : `<button data-rule-toggle="${esc(r.id)}" data-enabled="${r.enabled ? '1' : ''}" title="${r.enabled ? 'Disable' : 'Enable'}">${r.enabled ? 'On' : 'Off'}</button><button data-rule-copy="${esc(r.id)}" title="Copy as a team rule for ${esc(S.reviewRules.teamFile || '.px1/rules.json')}">Copy</button><button data-rule-delete="${esc(r.id)}" title="Delete rule">×</button>`}</div>`).join('');
  return `<details class="review-rules"${rulesOpen ? ' open' : ''}><summary>Rules <span>${rules.filter(r => r.enabled).length} active</span></summary>${err}${rows}</details>`;
}

function challengesMarkup() {
  const list = S.reviewChallenges || [];
  if (!list.length) return '';
  const rows = list.map(c => `<button class="review-pin review-challenge" data-review-challenge="${esc(c.id)}" data-path="${esc(c.path)}" title="${esc(c.why || c.decision)}"><span class="review-pin-mark">⟲</span><span class="review-pin-text">${esc(c.decision)}</span><span class="review-pin-ref">${esc(c.path.split('/').pop())}</span></button>`).join('');
  return `<div class="review-pins review-challenges"><div class="review-pins-head"><strong>Reversed decisions</strong><span>${list.length} to resolve</span></div><div class="review-pin-list">${rows}</div></div>`;
}

function pinsMarkup() {
  const pins = S.reviewPins || [];
  const open = pins.filter(p => p.status === 'proposed' && !p.stale).length;
  const label = explaining ? 'Explaining…' : pins.length ? 'Re-explain' : 'Explain changes';
  const rows = pins.map(p => `<button class="review-pin impact-${p.impact}${p.stale ? ' stale' : ''} ${esc(p.status)}" data-review-pin="${esc(p.id)}" title="${esc(p.why || p.decision)}"><span class="review-pin-mark">◆</span><span class="review-pin-text">${esc(p.decision)}</span><span class="review-pin-ref">${esc(p.path.split('/').pop())}:${p.lineStart}</span></button>`).join('');
  return `<div class="review-pins"><div class="review-pins-head"><strong>Decisions</strong><span>${pins.length ? open + ' unread' : ''}</span><button class="review-explain" data-review-explain ${explaining || !(S.review?.queue?.total) ? 'disabled' : ''}>${label}</button></div>${rows ? `<div class="review-pin-list">${rows}</div>` : ''}</div>`;
}

function inboxMarkup() {
  const entries = S.reviewInbox || [];
  if (entries.length < 2) return '';
  const waiting = entries.reduce((n, e) => n + (e.queue?.remaining || 0), 0);
  const rows = entries.map(e => {
    const q = e.queue || {};
    const label = q.remaining ? `${q.remaining} to review` : q.total ? 'reviewed' : 'no changes';
    return `<button class="review-inbox-item${e.current ? ' current' : ''}" data-review-worktree="${esc(e.path)}" ${e.current ? 'disabled' : ''}>
      <span class="review-state ${q.remaining ? 'unreviewed' : ''}"></span><span class="review-inbox-name">${esc(e.name)}</span><span class="review-inbox-branch">${esc(e.branch || '')}</span><span class="review-inbox-count">${esc(label)}</span>
    </button>`;
  }).join('');
  return `<div class="review-inbox"><div class="review-inbox-head"><strong>Review inbox</strong><span>${waiting ? waiting + ' awaiting review' : 'all clear'}</span></div>${rows}</div>`;
}

function itemMarkup(item) {
  const state = item.state || 'unreviewed';
  const label = state === 'unreviewed' ? 'Needs review' : state;
  return `<div class="review-item" data-review-path="${esc(item.path)}">
    <button class="review-open" title="Open ${esc(item.path)}"><span class="review-state ${esc(state)}"></span><span class="review-path">${esc(item.path)}</span><span class="review-label">${esc(label)}</span></button>
    <button class="review-mark" data-review-mark="${esc(item.path)}" title="Mark reviewed" ${state === 'reviewed' ? 'disabled' : ''}>✓</button>
  </div>`;
}

function drawReviewQueue() {
  if (!queueEl) return;
  const active = S.review?.active;
  const q = S.review?.queue;
  if (!active) {
    queueEl.innerHTML = `<div class="review-empty"><strong>Review agent changes</strong><p>Snapshot the workspace before an agent task. px1 will queue only files changed after that point.</p><button class="review-primary" data-review-start>Start review session</button></div>`;
    return;
  }
  const items = q?.items || [];
  const count = q?.total || items.length;
  const reviewed = q?.reviewed || 0;
  const next = items.find(pending);
  const comments = (S.reviewComments || []).filter(c => c.status === 'open' && !c.stale);
  const checks = S.reviewChecks || { commands: {}, jobs: [] };
  const names = Object.keys(checks.commands || {}).sort();
  const latest = new Map();
  for (const job of checks.jobs || []) if (!latest.has(job.name)) latest.set(job.name, job);
  const checkMarkup = names.length ? `<div class="review-checks">${names.map(name => {
    const j = latest.get(name); const state = j?.running ? 'running' : j?.stale ? 'stale' : j?.exitCode ? 'failed' : j ? 'passed' : 'idle';
    const label = j?.running ? 'Running…' : j?.stale ? 'Stale' : j?.exitCode ? 'Failed' : j ? 'Passed' : 'Run';
    const detail = j?.output ? checks.commands[name] + '\n\n' + j.output.slice(-1200) : checks.commands[name];
    return `<button class="review-check ${state}" data-review-check="${esc(name)}" title="${esc(detail)}" ${j?.running ? 'disabled' : ''}><span>${esc(name)}</span><span>${label}</span></button>`;
  }).join('')}</div>` : '<button class="review-check-empty" data-review-setup-checks title="Add named commands under verification.commands in settings.json">Set up checks</button>';
  queueEl.innerHTML = `${inboxMarkup()}<div class="review-summary"><div class="review-summary-text"><strong>Agent changes</strong><span>${reviewed} of ${count} reviewed${active.baseRef ? ' · since worktree creation' : ''}</span></div><button class="review-close" data-review-close title="Close review session">Close</button><div class="review-progress"><span style="width:${count ? Math.round(reviewed * 100 / count) : 0}%"></span></div></div>
    ${challengesMarkup()}
    ${ruleHitsMarkup()}
    ${pinsMarkup()}
    <div class="review-list">${items.length ? items.map(itemMarkup).join('') : '<div class="hint">No files have changed since this review began.</div>'}</div>
    ${rulesMarkup()}
    <div class="review-foot">${checkMarkup}${comments.length ? `<button class="review-feedback" data-review-feedback>Ask agent to address ${comments.length} comment${comments.length === 1 ? '' : 's'}</button>` : ''}${S.lastReviewPatch ? '<button class="review-undo" data-review-undo>Undo last patch</button>' : ''}<button class="review-next" data-review-next ${next ? '' : 'disabled'}>${next ? 'Next change →' : 'All changes reviewed'}</button></div>`;
}

async function start() {
  try {
    await apiPost('/api/review/session/start');
    await refreshReviewQueue();
    showToast('✓', 'Review baseline captured');
  } catch (e) { showToast('!', e.message); }
}

async function mark(path) {
  try {
    const q = await apiPost('/api/review/mark', { path, state: 'reviewed' });
    if (S.review) S.review.queue = q.queue;
    drawReviewQueue();
    showToast('✓', 'Marked reviewed');
  } catch (e) { showToast('!', e.message); }
}

async function openNext() {
  const item = S.review?.queue?.items?.find(pending);
  if (!item) return;
  await openFile(item.path);
  await openReviewDiff(item.path);
}

async function close() {
  try {
    await apiPost('/api/review/session/close');
    await refreshReviewQueue();
    showToast('✓', 'Review session closed');
  } catch (e) { showToast('!', e.message); }
}

async function sendFeedback() {
  try {
    const job = await apiPost('/api/review/comments/agent');
    showToast('✓', 'Agent is addressing review comments');
    await waitForAgent(job.id);
    await api('/api/reindex');
    await reloadOpenTabs();
    await drawTree('', treeEl, 0);
    await refreshReviewQueue();
    showToast('✓', 'Review changes refreshed');
  } catch (e) { showToast('!', e.message); }
}

async function reloadReviewWorkspace() {
  await api('/api/reindex');
  await reloadOpenTabs();
  await drawTree('', treeEl, 0);
  await refreshReviewQueue();
}

// px1 is started before a normal harness session, not through a wrapper. Poll
// the compact review fingerprint so ordinary external edits become reviewable
// without asking the user to re-index or refresh the browser. The server owns
// the comparison against the task baseline; this only decides when to reload.
let watchedRevision = '';
let refreshInFlight = false;

export function watchReviewWorkspace() {
  const tick = async () => {
    if (refreshInFlight) return;
    let refreshing = false;
    try {
      const j = await api('/api/review/revision');
      if (!j.active) {
        watchedRevision = '';
        return;
      }
      if (!watchedRevision) {
        watchedRevision = j.revision;
        return;
      }
      if (j.revision === watchedRevision) return;
      watchedRevision = j.revision;
      refreshInFlight = refreshing = true;
      await reloadReviewWorkspace();
      const current = await api('/api/review/revision');
      watchedRevision = current.active ? current.revision : '';
      showToast('✓', 'External changes ready for review');
    } catch {
      // A transient reload or a stopped server should not disrupt review UI.
    } finally {
      if (refreshing) refreshInFlight = false;
    }
  };
  tick();
  setInterval(tick, 2000);
}

function openPatch(info) {
  const item = S.review?.queue?.items?.find(x => x.path === info?.path);
  if (!S.review?.active || !item?.currentHash) {
    showToast('!', 'Patch Mode is available for files changed in this review');
    return;
  }
  patchTarget = { ...info, expectedHash: item.currentHash };
  $('#review-patch-ref').textContent = commentRef(info);
  patchInput.value = info.text;
  patchBox.hidden = false;
  patchInput.focus();
}

function closePatch() {
  patchTarget = null;
  if (patchBox) patchBox.hidden = true;
}

async function applyPatch() {
  if (!patchTarget || !patchInput) return;
  try {
    const j = await apiPost('/api/review/patch', undefined, {
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path: patchTarget.path, lineStart: patchTarget.l1, lineEnd: patchTarget.l2, expectedHash: patchTarget.expectedHash, replacement: patchInput.value }),
    });
    S.lastReviewPatch = j.patch;
    closePatch();
    hideSelectionBar();
    await reloadReviewWorkspace();
    showToast('✓', 'Patch applied');
  } catch (e) { showToast('!', e.message); }
}

async function undoPatch() {
  if (!S.lastReviewPatch) return;
  try {
    await apiPost('/api/review/patch/undo', undefined, { headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ id: S.lastReviewPatch.id }) });
    S.lastReviewPatch = null;
    await reloadReviewWorkspace();
    showToast('✓', 'Patch undone');
  } catch (e) { showToast('!', e.message); }
}

async function runCheck(name) {
  try {
    const j = await apiPost('/api/review/check/run', { name });
    await refreshReviewQueue();
    while (true) {
      await new Promise(resolve => setTimeout(resolve, 700));
      await refreshReviewQueue();
      const job = (S.reviewChecks.jobs || []).find(x => x.id === j.job.id);
      if (!job?.running) {
        showToast(job?.exitCode ? '!' : '✓', job?.exitCode ? name + ' failed' : name + ' passed');
        return;
      }
    }
  } catch (e) { showToast('!', e.message); }
}

async function explain() {
  explaining = true;
  drawReviewQueue();
  try {
    const job = await apiPost('/api/review/pins/explain');
    showToast('✓', 'Agent is explaining its decisions');
    await waitForAgent(job.id);
    await refreshReviewQueue();
    if (S.reviewExplain?.error) showToast('!', S.reviewExplain.error);
    else showToast('✓', (S.reviewPins || []).length + ' decisions pinned');
  } catch (e) { showToast('!', e.message); }
  explaining = false;
  drawReviewQueue();
}

async function openPin(id) {
  const pin = (S.reviewPins || []).find(p => p.id === id);
  if (!pin) return;
  await openFile(pin.path);
  await openReviewDiff(pin.path);
  revealPin(id);
}

async function openChallenge(id, path) {
  await openFile(path);
  await openReviewDiff(path);
  revealChallenge(id);
}

function suggestPattern(code) {
  const text = code || '';
  const call = /([A-Za-z_$][\w$.]*)\s*\(/.exec(text);
  if (call) return '\\b' + call[1].replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '\\s*\\(';
  const word = (text.match(/[A-Za-z_$][\w$]{3,}/) || [])[0];
  return word ? '\\b' + word + '\\b' : '';
}

function openRule(info, message) {
  if (!ruleBox) return;
  const ext = (info.path.split('/').pop().match(/\.[^.]+$/) || [''])[0];
  ruleTarget = { ...info, message };
  $('#review-rule-ref').textContent = commentRef(info);
  $('#review-rule-message').value = message;
  $('#review-rule-pattern').value = suggestPattern(info.text);
  $('#review-rule-glob').value = ext ? '*' + ext : '';
  previewRule();
  ruleBox.hidden = false;
  $('#review-rule-pattern').focus();
}

function closeRule() {
  ruleTarget = null;
  if (ruleBox) ruleBox.hidden = true;
}

function previewRule() {
  const el = $('#review-rule-preview');
  if (!el || !ruleTarget) return;
  const pattern = $('#review-rule-pattern').value;
  try {
    const re = new RegExp(pattern);
    if (!pattern) { el.textContent = ''; return; }
    const lines = (ruleTarget.text || '').split('\n');
    const n = lines.filter(l => re.test(l)).length;
    el.textContent = n ? `Matches ${n} of ${lines.length} selected line${lines.length === 1 ? '' : 's'}` : 'Does not match the selected code';
    el.classList.toggle('warn', !n);
  } catch (e) {
    el.textContent = 'Invalid pattern';
    el.classList.add('warn');
  }
}

async function saveRule() {
  if (!ruleTarget) return;
  try {
    await apiPost('/api/rules', undefined, {
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        message: $('#review-rule-message').value,
        pattern: $('#review-rule-pattern').value,
        glob: $('#review-rule-glob').value,
        origin: commentRef(ruleTarget),
      }),
    });
    closeRule();
    await refreshReviewQueue();
    showToast('✓', 'Rule saved; future changes will be checked');
  } catch (e) { showToast('!', e.message); }
}

async function suggestRule() {
  if (!ruleTarget) return;
  const btn = $('#review-rule-suggest');
  btn.disabled = true;
  btn.textContent = 'Suggesting…';
  try {
    const job = await apiPost('/api/rules/suggest', undefined, {
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path: ruleTarget.path, lineStart: ruleTarget.l1, lineEnd: ruleTarget.l2, comment: ruleTarget.message }),
    });
    await waitForAgent(job.id);
    const res = await api('/api/rules/suggest', { job: job.id });
    if (res.error) throw new Error(res.error);
    if (ruleTarget) {
      $('#review-rule-pattern').value = res.pattern || '';
      $('#review-rule-glob').value = res.glob || '';
      if (res.message) $('#review-rule-message').value = res.message;
      previewRule();
    }
  } catch (e) { showToast('!', e.message); }
  btn.disabled = false;
  btn.textContent = 'Suggest';
}

async function sendRuleHits() {
  sendingRules = true;
  drawReviewQueue();
  try {
    const job = await apiPost('/api/review/rule-hits/send');
    showToast('✓', 'Agent is fixing rule hits');
    await waitForAgent(job.id);
    await reloadReviewWorkspace();
    showToast('✓', 'Rule hits refreshed');
  } catch (e) { showToast('!', e.message); }
  sendingRules = false;
  drawReviewQueue();
}

async function updateRule(body) {
  try {
    await apiPost('/api/rules/update', undefined, { headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
    await refreshReviewQueue();
  } catch (e) { showToast('!', e.message); }
}

async function onPin(action, pin, choice) {
  try {
    if (action === 'rule-comment') {
      await apiPost('/api/review/comment', undefined, {
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path: pin.path, lineStart: pin.line, lineEnd: pin.line, text: 'Rule: ' + pin.message }),
      });
      await refreshReviewQueue();
      showToast('✓', 'Review comment added');
      return;
    }
    if (action === 'rule-dismiss') {
      await apiPost('/api/review/rule-hits/dismiss', undefined, { headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ key: pin.key }) });
      await refreshReviewQueue();
      return;
    }
    if (action === 'rule-disable') {
      await updateRule({ id: pin.ruleId, enabled: false });
      showToast('✓', 'Rule disabled');
      return;
    }
    if (action === 'challenge-ask') {
      const why = pin.why ? ` (${pin.why})` : '';
      openComment({ path: pin.path, l1: pin.at.l1, l2: pin.at.l2 }, `This change reverses an earlier decision: ${pin.decision}${why}. Restore it unless the requirement changed.`);
      return;
    }
    if (action === 'challenge-supersede' || action === 'challenge-dismiss') {
      await apiPost('/api/decisions/resolve', undefined, {
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id: pin.id, action: action === 'challenge-supersede' ? 'supersede' : 'dismiss' }),
      });
      await refreshReviewQueue();
      showToast('✓', action === 'challenge-supersede' ? 'Decision retired' : 'Decision kept; change allowed');
      return;
    }
    if (action === 'ask') {
      openComment({ path: pin.path, l1: pin.lineStart, l2: pin.lineEnd }, `Why was this needed: ${pin.decision}?`);
      return;
    }
    if (action === 'accept' || action === 'reopen') {
      const j = await apiPost('/api/review/pin/status', undefined, {
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id: pin.id, status: action === 'accept' ? 'accepted' : 'proposed' }),
      });
      await refreshReviewQueue();
      revealPin(pin.id);
      if (action === 'accept') showToast('✓', j.remembered ? 'Marked as understood and remembered' : 'Marked as understood');
      return;
    }
    if (action === 'switch') {
      const j = await apiPost('/api/review/pin/status', undefined, {
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id: pin.id, status: 'switched', choice }),
      });
      showToast('✓', 'Agent is switching to ' + choice);
      await waitForAgent(j.job.id);
      await reloadReviewWorkspace();
      showToast('✓', 'Switched to ' + choice);
    }
  } catch (e) { showToast('!', e.message); }
}

async function waitForAgent(id) {
  for (;;) {
    const job = await api('/api/agent/job', { id });
    if (!job.running) {
      if (job.error) throw new Error(job.error);
      return job;
    }
    await new Promise(resolve => setTimeout(resolve, 600));
  }
}

export function initReviewQueue() {
  setReviewCommentHandler(openComment);
  setReviewPatchHandler(openPatch);
  setPinHandler(onPin);
  $('#btn-review')?.addEventListener('click', async () => {
    shown = !shown;
    tree.hidden = shown;
    queueEl.hidden = !shown;
    $('#btn-review').classList.toggle('active', shown);
    if (shown) await refreshReviewQueue();
  });
  queueEl?.addEventListener('toggle', e => {
    if (e.target.matches('.review-rules')) rulesOpen = e.target.open;
  }, true);
  queueEl?.addEventListener('click', async e => {
    const startBtn = e.target.closest('[data-review-start]');
    if (startBtn) return start();
    const worktree = e.target.closest('[data-review-worktree]');
    if (worktree) return switchWorktree(worktree.dataset.reviewWorktree);
    if (e.target.closest('[data-review-close]')) return close();
    if (e.target.closest('[data-review-feedback]')) return sendFeedback();
    if (e.target.closest('[data-review-undo]')) return undoPatch();
    if (e.target.closest('[data-review-setup-checks]')) return openSettings('json');
    if (e.target.closest('[data-review-explain]')) return explain();
    if (e.target.closest('[data-review-rulehits-send]')) return sendRuleHits();
    const hitBtn = e.target.closest('[data-review-rulehit]');
    if (hitBtn) {
      await openFile(hitBtn.dataset.path);
      await openReviewDiff(hitBtn.dataset.path);
      return revealRuleHit(hitBtn.dataset.reviewRulehit, +hitBtn.dataset.line);
    }
    const toggle = e.target.closest('[data-rule-toggle]');
    if (toggle) return updateRule({ id: toggle.dataset.ruleToggle, enabled: !toggle.dataset.enabled });
    const del = e.target.closest('[data-rule-delete]');
    if (del) return updateRule({ id: del.dataset.ruleDelete, delete: true });
    const copy = e.target.closest('[data-rule-copy]');
    if (copy) {
      const r = (S.reviewRules?.rules || []).find(x => x.id === copy.dataset.ruleCopy);
      if (r) {
        await navigator.clipboard?.writeText(JSON.stringify({ pattern: r.pattern, glob: r.glob || '', message: r.message }, null, 2));
        showToast('✓', 'Copied; add it to the "rules" array in ' + (S.reviewRules.teamFile || '.px1/rules.json'));
      }
      return;
    }
    const chBtn = e.target.closest('[data-review-challenge]');
    if (chBtn) return openChallenge(chBtn.dataset.reviewChallenge, chBtn.dataset.path);
    const pinBtn = e.target.closest('[data-review-pin]');
    if (pinBtn) return openPin(pinBtn.dataset.reviewPin);
    const check = e.target.closest('[data-review-check]');
    if (check) return runCheck(check.dataset.reviewCheck);
    if (e.target.closest('[data-review-next]')) return openNext();
    const markBtn = e.target.closest('[data-review-mark]');
    if (markBtn) return mark(markBtn.dataset.reviewMark);
    const item = e.target.closest('[data-review-path]');
    if (item) {
      await openFile(item.dataset.reviewPath);
      await openReviewDiff(item.dataset.reviewPath);
    }
  });
  $('#review-comment-cancel')?.addEventListener('click', closeComment);
  $('#review-comment-save')?.addEventListener('click', saveComment);
  $('#review-comment-rule')?.addEventListener('click', async () => {
    const info = commentTarget;
    const text = commentInput?.value.trim();
    if (!info || !text) return;
    if (await saveComment()) openRule(info, text);
  });
  $('#review-rule-cancel')?.addEventListener('click', closeRule);
  $('#review-rule-save')?.addEventListener('click', saveRule);
  $('#review-rule-suggest')?.addEventListener('click', suggestRule);
  $('#review-rule-pattern')?.addEventListener('input', previewRule);
  ruleBox?.addEventListener('keydown', e => {
    e.stopPropagation();
    if (e.key === 'Escape') { e.preventDefault(); closeRule(); }
    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) { e.preventDefault(); saveRule(); }
  });
  commentInput?.addEventListener('keydown', e => {
    e.stopPropagation();
    if (e.key === 'Escape') { e.preventDefault(); closeComment(); }
    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) { e.preventDefault(); saveComment(); }
  });
  $('#review-patch-cancel')?.addEventListener('click', closePatch);
  $('#review-patch-apply')?.addEventListener('click', applyPatch);
  patchInput?.addEventListener('keydown', e => {
    e.stopPropagation();
    if (e.key === 'Escape') { e.preventDefault(); closePatch(); }
    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) { e.preventDefault(); applyPatch(); }
  });
}

function commentRef(info) {
  return info.path + ':' + (info.l1 === info.l2 ? info.l1 : info.l1 + '-' + info.l2);
}

function openComment(info, text = '') {
  if (!info || !commentBox || !commentInput) return;
  if (!S.review?.active) {
    showToast('!', 'Start a review session before adding comments');
    return;
  }
  commentTarget = info;
  $('#review-comment-ref').textContent = commentRef(info);
  commentInput.value = text;
  commentBox.hidden = false;
  commentInput.focus();
}

function closeComment() {
  commentTarget = null;
  if (commentBox) commentBox.hidden = true;
}

async function saveComment() {
  const text = commentInput?.value.trim();
  if (!commentTarget || !text) return;
  try {
    await apiPost('/api/review/comment', undefined, {
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path: commentTarget.path, lineStart: commentTarget.l1, lineEnd: commentTarget.l2, text }),
    });
    closeComment();
    hideSelectionBar();
    showToast('✓', 'Review comment added');
    return true;
  } catch (e) { showToast('!', e.message); return false; }
}
