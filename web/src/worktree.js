// web/src/worktree.js
// Worktree switcher. An agent asked to work on a branch often runs in a
// `git worktree add` directory, so the files it changed are not in the folder
// px1 was started in. The header names the checkout px1 is showing and opens a
// menu of the repository's others; picking one re-points the server (index,
// language servers, checks, and the per-checkout review session) and reloads.
import { $, S, esc, api, apiPost } from './state.js';
import { showToast } from './ui.js';

const nameEl = () => $('#root-name');
const menuEl = () => $('#worktree-menu');
let list = [];
let open = false;

export async function loadWorktrees() {
  try {
    const j = await api('/api/worktrees');
    list = j.worktrees || [];
  } catch { list = []; }
  draw();
}

function current() {
  return list.find(w => w.current);
}

// How long ago a worktree was added, for the row's tooltip. Newest is first in
// the list, which is usually the one an agent was just told to work in.
function age(iso) {
  const mins = Math.round((Date.now() - new Date(iso).getTime()) / 60000);
  if (mins < 1) return 'just now';
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.round(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.round(hours / 24)}d ago`;
}

function draw() {
  const btn = nameEl();
  const menu = menuEl();
  if (!btn || !menu) return;
  const here = current();
  btn.textContent = S.meta?.name || here?.name || '-';
  // One checkout is the ordinary case: leave the header as a plain label.
  btn.classList.toggle('has-worktrees', list.length > 1);
  btn.title = list.length > 1
    ? (here?.branch ? `${here.branch} · ${S.meta?.root}\nSwitch worktree` : 'Switch worktree')
    : (S.meta?.root || '');
  menu.innerHTML = list.map(w => `
    <button class="worktree-item${w.current ? ' active' : ''}${w.missing ? ' missing' : ''}" data-worktree="${w.path.replace(/"/g, '&quot;')}" title="${esc(w.path)}${w.addedAt ? ' · added ' + age(w.addedAt) : ''}" ${w.missing ? 'disabled' : ''}>
      <span class="worktree-name">${w.name}</span>
      <span class="worktree-branch">${w.missing ? 'missing' : (w.branch || w.head?.slice(0, 7) || '')}</span>
    </button>`).join('');
}

function setOpen(next) {
  open = next && list.length > 1;
  menuEl()?.toggleAttribute('hidden', !open);
  nameEl()?.classList.toggle('open', open);
}

async function pick(path) {
  setOpen(false);
  try {
    const res = await apiPost('/api/worktree/switch', undefined, {
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path }),
    });
    if (!res.switched) return;
    showToast('✓', 'Switched worktree');
    // Everything on screen belongs to the old checkout: open tabs, the tree,
    // the review queue, language server state. A reload is the honest reset.
    setTimeout(() => location.reload(), 250);
  } catch (e) { showToast('!', e.message); }
}

export function initWorktrees() {
  nameEl()?.addEventListener('click', e => {
    if (list.length < 2) return;
    e.stopPropagation();
    setOpen(!open);
  });
  menuEl()?.addEventListener('click', e => {
    const btn = e.target.closest('[data-worktree]');
    if (btn) pick(btn.dataset.worktree);
  });
  document.addEventListener('click', e => {
    if (open && !e.target.closest('#worktree-menu, #root-name')) setOpen(false);
  });
  document.addEventListener('keydown', e => {
    if (open && e.key === 'Escape') setOpen(false);
  });
  loadWorktrees();
}
