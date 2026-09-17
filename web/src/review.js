import { $, esc, S, api, apiPost } from './state.js';
import { openFile } from './tabs.js';
import { reloadOpenTabs } from './tabs.js';
import { drawTree, treeEl } from './tree.js';
import { openReviewDiff } from './diff.js';
import { showToast } from './ui.js';
import { hideSelectionBar, setReviewCommentHandler, setReviewPatchHandler } from './selbar.js';

const queueEl = $('#review-queue');
const tree = $('#tree');
let shown = false;
let commentTarget = null;
const commentBox = $('#review-commentbox');
const commentInput = $('#review-comment-input');
const patchBox = $('#review-patchbox');
const patchInput = $('#review-patch-input');
let patchTarget = null;

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
  drawReviewQueue();
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
  queueEl.innerHTML = `<div class="review-summary"><div><strong>Agent changes</strong><span>${reviewed} / ${count} reviewed</span></div><button class="review-close" data-review-close title="Close review session">Close</button></div>
    <div class="review-progress"><span style="width:${count ? Math.round(reviewed * 100 / count) : 0}%"></span></div>
    <div class="review-list">${items.length ? items.map(itemMarkup).join('') : '<div class="hint">No files have changed since this review began.</div>'}</div>
    <div class="review-foot"><button class="review-feedback" data-review-feedback ${comments.length ? '' : 'disabled'}>Ask agent to address ${comments.length} comment${comments.length === 1 ? '' : 's'}</button>${S.lastReviewPatch ? '<button class="review-undo" data-review-undo>Undo last patch</button>' : ''}<button class="review-next" data-review-next ${next ? '' : 'disabled'}>${next ? 'Next change →' : 'All changes reviewed'}</button></div>`;
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
  $('#btn-review')?.addEventListener('click', async () => {
    shown = !shown;
    tree.hidden = shown;
    queueEl.hidden = !shown;
    $('#btn-review').classList.toggle('active', shown);
    if (shown) await refreshReviewQueue();
  });
  queueEl?.addEventListener('click', async e => {
    const startBtn = e.target.closest('[data-review-start]');
    if (startBtn) return start();
    if (e.target.closest('[data-review-close]')) return close();
    if (e.target.closest('[data-review-feedback]')) return sendFeedback();
    if (e.target.closest('[data-review-undo]')) return undoPatch();
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

function openComment(info) {
  if (!info || !commentBox || !commentInput) return;
  if (!S.review?.active) {
    showToast('!', 'Start a review session before adding comments');
    return;
  }
  commentTarget = info;
  $('#review-comment-ref').textContent = commentRef(info);
  commentInput.value = '';
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
  } catch (e) { showToast('!', e.message); }
}
