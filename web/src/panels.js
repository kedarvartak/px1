// web/src/panels.js
import { $, api } from './state.js';
import { layout, render } from './renderer.js';
import { updateStatus } from './status.js';
import { treeEl, openDirs, drawTree } from './tree.js';
import { reloadOpenTabs } from './tabs.js';
import { showToast } from './ui.js';

export function showPanel() {
  document.body.classList.remove('side-hidden');
  layout();
  render();
}

export function initPanels() {
  $('#btn-reindex').addEventListener('click', async () => {
    await api('/api/reindex');
    treeEl.innerHTML = ''; openDirs.clear();
    await drawTree('', treeEl, 0);
    // Reindex is a refresh: re-fetch open tabs quietly in place without tab switching.
    await reloadOpenTabs();
    updateStatus();
    showToast('✓', 'Workspace reindexed');
  });

  /* sidebar resize */
  (() => {
    const rz = $('#resizer'); let dragging = false;
    rz.addEventListener('mousedown', e => { dragging = true; rz.classList.add('drag'); e.preventDefault(); });
    addEventListener('mousemove', e => {
      if (!dragging) return;
      $('#side').style.width = Math.max(170, Math.min(620, e.clientX)) + 'px';
    });
    addEventListener('mouseup', () => { if (dragging) { dragging = false; rz.classList.remove('drag'); layout(); render(); } });
  })();
}
