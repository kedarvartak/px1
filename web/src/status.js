import { $, S, doc_, withKeys } from './state.js';
import { layoutPref } from './diff.js';

export function updateStatus() {
  const d = doc_();
  const sizeEl = $('#st-size');
  if (sizeEl) sizeEl.textContent = d ? fmtBytes(d.size) : '';

  const hasDiff = !!(d && d.diffAvailable);
  const isDiffOn = !!(d && d.diffMode);
  const currentLayout = (d && d.diffMode) || layoutPref();
  const dsw = $('#diff-switch');
  if (dsw) {
    dsw.hidden = !hasDiff;
    document.body.classList.toggle('diff-tab', hasDiff);
    const btn = $('#diff-btn');
    if (btn) {
      btn.classList.toggle('on', hasDiff && isDiffOn);
      btn.title = withKeys(`Show changes against HEAD, ${currentLayout === 'unified' ? 'unified' : 'split'} ({Mod+D})`);
    }
    $('#diff-source')?.classList.toggle('on', hasDiff && !isDiffOn);
    const menuItems = dsw.querySelectorAll('.diff-menu-item');
    for (const item of menuItems) {
      item.classList.toggle('active', item.dataset.diffOpt === currentLayout);
    }
  }

  const verEl = $('#st-ver');
  if (verEl && S.meta?.version) {
    verEl.textContent = 'v' + S.meta.version;
    verEl.title = `px1 v${S.meta.version} (Click for shortcuts & help)`;
  }
  drawLspStatus();
}

let noteTimer = null;

export function setStatusNote(msg, timeoutMs = 0) {
  if (noteTimer) {
    clearTimeout(noteTimer);
    noteTimer = null;
  }
  const el = $('#st-pos');
  if (el) el.textContent = msg || '';
  if (msg && timeoutMs > 0) {
    noteTimer = setTimeout(() => {
      if (el && el.textContent === msg) el.textContent = '';
      noteTimer = null;
    }, timeoutMs);
  }
}

export function fmtBytes(n) {
  if (n < 1024) return n + ' B';
  if (n < 1048576) return (n / 1024).toFixed(1) + ' KB';
  return (n / 1048576).toFixed(1) + ' MB';
}

export function setLspState(j) {
  if (!j || !j.state) return;
  S.lsp.state = j.state;
  S.lsp.server = j.server || S.lsp.server;
  // Only file, warm and start replies say what is missing; any running server means nothing is.
  if ('missing' in j || j.state !== 'off') S.lsp.missing = j.missing || '';
  drawLspStatus();
}

export function drawLspStatus() {
  const el = $('#st-lsp');
  const { state, server, missing } = S.lsp;
  el.title = '';
  if (state === 'off' && missing) {
    el.dataset.state = 'missing';
    el.textContent = 'LSP: set up';
    el.title = 'No language server for ' + missing + '. Click to install or start one.';
    return;
  }
  if (!server || state === 'off') { el.textContent = ''; el.removeAttribute('data-state'); return; }
  el.dataset.state = state;
  el.textContent = state === 'ready' ? server : server + ' ' + state;
  if (state === 'failed') el.title = 'The language server did not start. Click for details.';
}

/* The status bar stays on one line. When its contents outgrow the width, it
   sheds detail in steps (see the fit-N rules in style.css), least useful first,
   stopping at the first step that fits. */
const FIT_STEPS = 6;
const statusEl = $('#status');

export function fitStatus() {
  for (let i = 1; i <= FIT_STEPS; i++) statusEl.classList.remove('fit-' + i);
  for (let i = 1; i <= FIT_STEPS && statusEl.scrollWidth > statusEl.clientWidth; i++) {
    statusEl.classList.add('fit-' + i);
  }
}

export function initStatusFit() {
  // Width changes come from the window and the sidebar resizers; content changes
  // from LSP state and the selection bar. Class changes are not observed,
  // so fitStatus() toggling them cannot re-trigger itself.
  new ResizeObserver(fitStatus).observe(statusEl);
  new MutationObserver(fitStatus).observe(statusEl, { childList: true, subtree: true, characterData: true });
  document.fonts?.ready.then(fitStatus);
}
