import { $, S, doc_, withKeys } from './state.js';
import { layoutPref } from './diff.js';

// The status module only owns review feedback and the diff view state. The
// previous IDE-style footer metadata and responsive fit observer were removed.
export function updateStatus() {
  const d = doc_();
  const hasDiff = !!(d && d.diffAvailable);
  const isDiffOn = !!(d && d.diffMode);
  const currentLayout = (d && d.diffMode) || layoutPref();
  const dsw = $('#diff-switch');
  if (!dsw) return;

  dsw.hidden = !hasDiff;
  document.body.classList.toggle('diff-tab', hasDiff);
  const btn = $('#diff-btn');
  if (btn) {
    btn.classList.toggle('on', hasDiff && isDiffOn);
    btn.title = withKeys(`Show changes against HEAD, ${currentLayout === 'unified' ? 'unified' : 'split'} ({Mod+D})`);
  }
  $('#diff-source')?.classList.toggle('on', hasDiff && !isDiffOn);
  for (const item of dsw.querySelectorAll('.diff-menu-item')) {
    item.classList.toggle('active', item.dataset.diffOpt === currentLayout);
  }
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

export function setLspState(j) {
  if (!j || !j.state) return;
  S.lsp.state = j.state;
  S.lsp.server = j.server || S.lsp.server;
  if ('missing' in j || j.state !== 'off') S.lsp.missing = j.missing || '';
}
