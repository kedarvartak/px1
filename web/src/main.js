// web/src/main.js
import { $, S, api, applyKeyLabels } from './state.js';
import { measure, layout, render, initRenderer } from './renderer.js';
import { initTabs, openFile } from './tabs.js';
import { initCursor } from './cursor.js';
import { initHover } from './hover.js';
import { initSelectionBar } from './selbar.js';
import { drawTree, treeEl, initTree, revealFile } from './tree.js';
import { initSearch } from './search.js';
import { initOutline } from './outline.js';
import { initPanels } from './panels.js';
import { initInspector } from './inspector.js';
import { initCalls } from './calls.js';
import { initFind } from './find.js';
import { initPalette } from './palette.js';
import { initShortcuts } from './shortcuts.js';
import { initDiff } from './diff.js';
import { updateStatus } from './status.js';
import { initSettings } from './settings.js';
import { initReviewQueue, refreshReviewQueue, watchReviewWorkspace } from './review.js';
import { initWorktrees } from './worktree.js';

function showAccessPolicy(p) {
  const el = $('#access-policy');
  const form = $('#access-token');
  if (!el || !p) return;
  const on = ['patch', 'agent', 'checks'].filter(k => p[k]);
  if (!p.remote) el.textContent = 'Local patch and agent access';
  else if (p.needsToken) el.textContent = 'Remote writes stay off until PX1_REMOTE_TOKEN is set';
  else if (!on.length) el.textContent = 'Remote read-only';
  else el.textContent = 'Remote ' + on.join(', ') + ' need a token';
  el.hidden = false;
  if (form) form.hidden = !p.tokenRequired;
}

function initAccessToken() {
  const form = $('#access-token');
  const input = $('#access-token-input');
  if (!form || !input) return;
  try { input.value = sessionStorage.getItem('px1.remoteToken') || ''; } catch {}
  form.addEventListener('submit', e => {
    e.preventDefault();
    try { sessionStorage.setItem('px1.remoteToken', input.value.trim()); } catch {}
  });
}

// Initialize all subsystems
initRenderer();
initTabs();
initCursor();
initHover();
initSelectionBar();
initTree();
initSearch();
initOutline();
initPanels();
initInspector();
initCalls();
initFind();
initPalette();
initShortcuts();
initDiff();
initSettings();
initReviewQueue();
initAccessToken();

// Bootstrap application lifecycle
(async function boot() {
  try {
    // Restore word wrap (default ON)
    const wrapPref = localStorage.getItem('px1.wrap');
    S.wrap = wrapPref !== null ? wrapPref === 'true' : true;
    document.body.classList.toggle('word-wrap', S.wrap);

    // Line numbers are always ON
    S.lineNumbers = true;
    document.body.classList.remove('hide-lines');

  } catch {}

  applyKeyLabels();

  measure();
  S.meta = await api('/api/meta');
  showAccessPolicy(S.meta.policy);
  if (S.meta.git) { const b = $('#btn-changed'); if (b) b.hidden = false; }
  document.title = S.meta.name + ' - px1';
  $('#root-name').textContent = S.meta.name;
  $('#root-name').title = S.meta.root;
  initWorktrees();
  if (S.meta.version) {
    const emptyVerEl = $('#empty-ver');
    if (emptyVerEl) emptyVerEl.textContent = 'v' + S.meta.version;
  }
  updateStatus();
  await drawTree('', treeEl, 0);
  await refreshReviewQueue();
  watchReviewWorkspace();

  const params = new URLSearchParams(window.location.search);
  const initialPath = params.get('path');
  const initialLine = parseInt(params.get('line'), 10) || undefined;
  if (initialPath) {
    await openFile(initialPath, { line: initialLine });
    await revealFile(initialPath);
    try {
      const u = new URL(window.location.href);
      u.searchParams.delete('path');
      u.searchParams.delete('line');
      const cleanSearch = u.searchParams.toString();
      const cleanUrl = u.pathname + (cleanSearch ? '?' + cleanSearch : '') + u.hash;
      window.history.replaceState({}, '', cleanUrl);
    } catch {}
  }

  if (document.fonts && document.fonts.ready) {
    document.fonts.ready.then(() => { measure(); layout(); render(); });
  }

})();
