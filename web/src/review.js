import { $, esc, S, api, apiPost } from './state.js';
import { openFile } from './tabs.js';
import { showToast } from './ui.js';
import { hideSelectionBar, setReviewCommentHandler } from './selbar.js';

const queueEl = $('#review-queue');
const tree = $('#tree');
let shown = false;
let commentTarget = null;
const commentBox = $('#review-commentbox');
const commentInput = $('#review-comment-input');

const pending = item => item.state === 'unreviewed' || item.state === 'stale' || item.state === 'blocked';

export async function refreshReviewQueue() {
  try {
    S.review = await api('/api/review/session');
  } catch {
    S.review = null;
  }
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
  queueEl.innerHTML = `<div class="review-summary"><div><strong>Agent changes</strong><span>${reviewed} / ${count} reviewed</span></div><button class="review-close" data-review-close title="Close review session">Close</button></div>
    <div class="review-progress"><span style="width:${count ? Math.round(reviewed * 100 / count) : 0}%"></span></div>
    <div class="review-list">${items.length ? items.map(itemMarkup).join('') : '<div class="hint">No files have changed since this review began.</div>'}</div>
    <div class="review-foot"><button class="review-next" data-review-next ${next ? '' : 'disabled'}>${next ? 'Next change →' : 'All changes reviewed'}</button></div>`;
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
}

async function close() {
  try {
    await apiPost('/api/review/session/close');
    await refreshReviewQueue();
    showToast('✓', 'Review session closed');
  } catch (e) { showToast('!', e.message); }
}

export function initReviewQueue() {
  setReviewCommentHandler(openComment);
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
    if (e.target.closest('[data-review-next]')) return openNext();
    const markBtn = e.target.closest('[data-review-mark]');
    if (markBtn) return mark(markBtn.dataset.reviewMark);
    const item = e.target.closest('[data-review-path]');
    if (item) await openFile(item.dataset.reviewPath);
  });
  $('#review-comment-cancel')?.addEventListener('click', closeComment);
  $('#review-comment-save')?.addEventListener('click', saveComment);
  commentInput?.addEventListener('keydown', e => {
    e.stopPropagation();
    if (e.key === 'Escape') { e.preventDefault(); closeComment(); }
    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) { e.preventDefault(); saveComment(); }
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
