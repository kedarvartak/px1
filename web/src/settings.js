// web/src/settings.js
import { $, S, api, apiPost } from './state.js';
import { applyEditorTypography, toggleWordWrap, toggleLineNumbers } from './renderer.js';
import { setLayoutPref } from './diff.js';

// Settings are intentionally a raw JSON escape hatch. The review surface does
// not need a second catalog of IDE preferences, but existing keys still apply
// when a user chooses to keep them in ~/.px1/settings.json.
export let settingsModalEl = null;
let settingsData = {
  settings: {},
  raw: '{\n}\n',
  path: '~/.px1/settings.json'
};

export async function loadSettings() {
  try {
    const data = await api('/api/settings');
    if (data) {
      settingsData = {
        ...settingsData,
        ...data,
        settings: data.settings || {}
      };
    }
    S.settings = settingsData.settings || {};
    return settingsData;
  } catch (err) {
    console.warn('Using local settings fallback:', err);
    return settingsData;
  }
}

export function applySettingLive(key, val) {
  if (!S.settings) S.settings = {};
  S.settings[key] = val;

  switch (key) {
    case 'editor.fontSize':
    case 'editor.fontFamily':
    case 'editor.lineHeight':
    case 'editor.tabSize': {
      const fs = parseFloat(S.settings['editor.fontSize']) || 13.5;
      const ff = S.settings['editor.fontFamily'] || '';
      const lh = parseFloat(S.settings['editor.lineHeight']) || 21.0;
      const ts = parseInt(S.settings['editor.tabSize'], 10) || 4;
      applyEditorTypography(fs, ff, lh, ts);
      break;
    }
    case 'editor.wordWrap':
      toggleWordWrap(val === 'on' || val === true);
      break;
    case 'editor.lineNumbers':
      toggleLineNumbers(val === 'on' || val === true);
      break;
    case 'editor.cursorStyle':
      document.body.classList.remove('cursor-block', 'cursor-underline');
      if (val === 'block') document.body.classList.add('cursor-block');
      else if (val === 'underline') document.body.classList.add('cursor-underline');
      break;
    case 'editor.cursorBlinking':
      document.body.classList.remove('cursor-blink-smooth', 'cursor-blink-solid', 'cursor-blink-blink');
      document.body.classList.add('cursor-blink-' + (val === 'solid' || val === 'blink' ? val : 'smooth'));
      break;
    case 'editor.renderLineHighlight':
      document.body.classList.toggle('no-line-highlight', val === 'none');
      break;
    case 'editor.scrollBeyondLastLine':
      document.body.classList.toggle('no-scroll-beyond', val === false || val === 'false');
      break;
    case 'git.gutterIndicators':
      document.body.classList.toggle('hide-git-gutter', val === false || val === 'false');
      break;
    case 'editor.minimap.enabled': {
      const minimap = $('#minimap-hits');
      if (minimap) minimap.style.display = val === false || val === 'false' ? 'none' : '';
      break;
    }
    case 'diffEditor.renderSideBySide':
      setLayoutPref(val === true || val === 'true' ? 'split' : 'unified');
      break;
  }
}

export function applyAllSettingsLive() {
  for (const [key, value] of Object.entries(S.settings || {})) {
    applySettingLive(key, value);
  }
}

export function openSettings() {
  if (!settingsModalEl) initSettingsDOM();
  settingsModalEl.hidden = false;
  showSettingsJSONView();
  loadSettings().then(showSettingsJSONView);
}

export function closeSettings() {
  if (settingsModalEl) settingsModalEl.hidden = true;
}

export function isSettingsOpen() {
  return settingsModalEl && !settingsModalEl.hidden;
}

function updateSettingsHeader() {
  const pathEl = $('#settings-path');
  if (pathEl && settingsData.path) {
    pathEl.textContent = settingsData.path;
    pathEl.title = 'Click to copy path: ' + settingsData.path;
  }
}

function showSettingsJSONView() {
  updateSettingsHeader();
  const rawEditor = $('#settings-raw-editor');
  if (rawEditor) {
    rawEditor.value = settingsData.raw || '{\n}\n';
    rawEditor.focus();
  }
  const errEl = $('#settings-raw-error');
  if (errEl) errEl.hidden = true;
}

async function handleSaveRawSettings() {
  const rawEditor = $('#settings-raw-editor');
  const errEl = $('#settings-raw-error');
  if (!rawEditor) return;

  const rawText = rawEditor.value;
  try {
    JSON.parse(rawText);
    if (errEl) errEl.hidden = true;
  } catch (err) {
    if (errEl) {
      errEl.textContent = 'JSON Syntax Error: ' + err.message;
      errEl.hidden = false;
    }
    return;
  }

  try {
    const res = await apiPost('/api/settings', { raw: rawText });
    if (res.settings) {
      settingsData.settings = res.settings;
      S.settings = res.settings;
      applyAllSettingsLive();
    }
    if (res.raw) settingsData.raw = res.raw;
    if (errEl) {
      errEl.textContent = 'Settings saved successfully.';
      errEl.hidden = false;
      errEl.classList.add('success');
      setTimeout(() => {
        errEl.hidden = true;
        errEl.classList.remove('success');
      }, 2500);
    }
  } catch (err) {
    if (errEl) {
      errEl.textContent = 'Failed to save: ' + err.message;
      errEl.hidden = false;
    }
  }
}

function initSettingsDOM() {
  settingsModalEl = $('#settings-modal');
  if (!settingsModalEl) return;

  $('#settings-close')?.addEventListener('click', closeSettings);
  settingsModalEl.addEventListener('click', e => {
    if (e.target === settingsModalEl) closeSettings();
  });
  $('#settings-path')?.addEventListener('click', () => {
    if (settingsData.path) navigator.clipboard.writeText(settingsData.path);
  });
  $('#btn-settings-save-raw')?.addEventListener('click', handleSaveRawSettings);
  $('#btn-settings-reset-raw')?.addEventListener('click', () => {
    const rawEditor = $('#settings-raw-editor');
    if (rawEditor) rawEditor.value = settingsData.raw || '{\n}\n';
    const errEl = $('#settings-raw-error');
    if (errEl) errEl.hidden = true;
  });
}

export function initSettings() {
  initSettingsDOM();
  loadSettings().then(applyAllSettingsLive);
}
