'use strict';
// Notepad tile: file tree, tabs, CodeMirror editors (editor.js), saving with
// encoding, line ending and conflict check. Shared helpers come from common.js
// (api, safeGet/safeSet, esc, setStatus, initTheme), strings from i18n.js.

const LANGS = window.NOTEPAD_I18N;
const SHELL_LANG_MAP = { en: 'en', de: 'de', fr: 'fr', es: 'es', zh: 'zh' };
const API = '/notepad/api';
const ENC_LABEL = {
  'utf-8': 'UTF-8', 'utf-8-bom': 'UTF-8 BOM', 'utf-16le': 'UTF-16 LE', 'utf-16be': 'UTF-16 BE', 'windows-1252': 'Windows-1252 (ANSI)',
};
const EOL_LABEL = { lf: 'LF', crlf: 'CRLF', cr: 'CR' };

let lang = 'en';
let info = { root: '/DATA', encodings: Object.keys(ENC_LABEL), maxText: 32 << 20 };
// {path, name, enc, eol, sha, savedDoc, diskEnc, diskEol, dirty, readOnly, language, ed, el, btn, saving}
const tabs = [];
let active = null;
let currentDir = '/DATA';
const settings = { wrap: safeGet('notepad.wrap') === '1', font: Number(safeGet('notepad.font')) || 14 };

// ---------- i18n ----------
function resolveLanguage() {
  const own = safeGet('notepad.lang');
  if (own && LANGS[own]) return own;
  const shell = (safeGet('lang') || navigator.language || 'en').slice(0, 2).toLowerCase();
  return LANGS[SHELL_LANG_MAP[shell]] ? SHELL_LANG_MAP[shell] : 'en';
}

function t(key, params) {
  let s = (LANGS[lang] && LANGS[lang][key]) || (LANGS.en && LANGS.en[key]) || key;
  if (params) for (const [k, v] of Object.entries(params)) s = s.split(`{${k}}`).join(String(v));
  return s;
}

function applyI18n() {
  document.documentElement.lang = lang;
  document.title = t('app.title');
  document.querySelectorAll('[data-i18n]').forEach((el) => { el.textContent = t(el.dataset.i18n); });
  document.querySelectorAll('[data-i18n-title]').forEach((el) => { el.title = t(el.dataset.i18nTitle); el.setAttribute('aria-label', t(el.dataset.i18nTitle)); });
  $('#langSelect').value = lang;
  for (const x of tabs) x.btn.querySelector('.np-x').setAttribute('aria-label', t('btn.close'));
  // Position and line count are texts too: without this they kept the previous language until the next cursor move.
  if (active) { updateBar(); showCursor(active); }
}

// ---------- errors ----------
function humanSize(n) {
  return n >= 1 << 20 ? `${(n / (1 << 20)).toFixed(1)} MiB` : `${Math.round(n / 1024)} KiB`;
}

function errText(err, tab) {
  if (err && err.status === 401) return t('error.session');
  if (err && err.status === 403 && err.code === 'auth_forbidden') return t('error.forbidden');
  const code = err && err.code;
  if (code === 'unencodable') return t('error.unencodable', { line: err.line, char: err.char, encoding: ENC_LABEL[tab && tab.enc] || '' });
  if (code === 'too_large') return t('error.too_large', { limit: humanSize(info.maxText) });
  const known = ['outside', 'not_found', 'not_dir', 'is_dir', 'binary', 'read_only', 'busy', 'conflict', 'exists', 'bad_name', 'permission'];
  if (known.includes(code)) return t(`error.${code}`);
  return t('error.generic', { msg: (err && err.message) || String(err) });
}

function fail(err, tab) {
  setStatus(errText(err, tab), 'err');
}

// The write call sends the text as the raw body: JSON escaping would make a
// text near the API's body limit too large.
async function writeRaw(query, text, retried = false) {
  const headers = { Accept: 'application/json', 'Content-Type': 'text/plain;charset=UTF-8' };
  const token = safeGet('access_token');
  if (token) headers.Authorization = `Bearer ${token}`;
  const res = await fetch(`${API}/write?${new URLSearchParams(query)}`, { method: 'POST', headers, body: text });
  if (res.status === 401 && !retried && await refreshSession()) return writeRaw(query, text, true);
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    const e = new ApiError(res.status, data.code || 'http', data.error || `HTTP ${res.status}`);
    e.line = data.line; e.char = data.char;
    throw e;
  }
  return data;
}

// ---------- dialog ----------
// One <dialog> for every question. buttons: [{label, value, kind}] -> resolves the value ('' = cancelled).
function ask({ title, text, input, buttons }) {
  const dlg = $('#dlg');
  $('#dlgTitle').textContent = title;
  $('#dlgText').textContent = text || '';
  $('#dlgText').hidden = !text;
  const wrap = $('#dlgInputWrap');
  const inp = $('#dlgInput');
  wrap.hidden = !input;
  inp.value = '';
  const actions = $('#dlgActions');
  actions.textContent = '';
  for (const b of buttons) {
    const el = document.createElement('button');
    el.type = 'submit';
    el.value = b.value;
    el.textContent = b.label;
    el.className = b.kind === 'primary' ? 'btn-primary' : b.kind === 'danger' ? 'btn-danger' : 'btn-secondary';
    actions.appendChild(el);
  }
  return new Promise((resolve) => {
    dlg.returnValue = '';
    const done = () => {
      dlg.removeEventListener('close', done);
      const v = dlg.returnValue;
      resolve(input ? (v === 'ok' ? inp.value.trim() : '') : v);
    };
    dlg.addEventListener('close', done);
    // Enter in the name field confirms; the form's implicit submit would take the first button (Cancel).
    inp.onkeydown = (ev) => { if (ev.key === 'Enter') { ev.preventDefault(); dlg.close('ok'); } };
    dlg.showModal();
    if (input) inp.focus();
  });
}

// ---------- tree ----------
function parentOf(p) {
  const i = p.lastIndexOf('/');
  return i > 0 ? p.slice(0, i) : p;
}

function baseName(p) {
  return p.slice(p.lastIndexOf('/') + 1);
}

async function loadFolder(path, container) {
  const res = await api(`${API}/list?path=${encodeURIComponent(path)}`);
  container.textContent = '';
  if (!res.entries.length) {
    const e = document.createElement('div');
    e.className = 'np-empty';
    e.textContent = t('tree.empty');
    container.appendChild(e);
    return;
  }
  for (const en of res.entries) container.appendChild(node(en));
}

function node(en) {
  const wrap = document.createElement('div');
  const btn = document.createElement('button');
  btn.type = 'button';
  btn.className = 'np-node' + (en.outside ? ' outside' : '');
  btn.dataset.path = en.path;
  btn.dataset.dir = en.dir ? '1' : '';
  btn.title = en.outside ? t('tree.outside') : en.path;
  btn.innerHTML = `<span class="np-ico">${en.dir ? '▸' : '·'}</span><span class="np-name">${esc(en.name)}${en.link ? ' ↪' : ''}</span>`;
  wrap.appendChild(btn);
  if (en.outside) return wrap;
  if (en.dir) {
    const kids = document.createElement('div');
    kids.className = 'np-children';
    kids.hidden = true;
    wrap.appendChild(kids);
    btn.addEventListener('click', async () => {
      currentDir = en.path;
      markActiveNode(btn);
      if (!kids.hidden) { kids.hidden = true; btn.firstChild.textContent = '▸'; return; }
      try {
        await loadFolder(en.path, kids);
        kids.hidden = false;
        btn.firstChild.textContent = '▾';
      } catch (e) { fail(e); }
    });
  } else {
    btn.addEventListener('click', () => {
      currentDir = parentOf(en.path);
      markActiveNode(btn);
      openFile(en.path);
      if (window.matchMedia('(max-width: 760px)').matches) $('#tree').classList.remove('open');
    });
  }
  return wrap;
}

function markActiveNode(btn) {
  document.querySelectorAll('.np-node.active').forEach((b) => b.classList.remove('active'));
  btn.classList.add('active');
}

// Reload the listing that shows dir (the root or an opened folder).
async function refreshDir(dir) {
  if (dir === info.root) return loadFolder(info.root, $('#treeBody'));
  const btn = [...document.querySelectorAll('.np-node')].find((b) => b.dataset.path === dir && b.dataset.dir);
  if (btn && !btn.nextElementSibling.hidden) return loadFolder(dir, btn.nextElementSibling);
  return undefined;
}

// ---------- tabs ----------
function isDark() {
  const th = document.documentElement.getAttribute('data-theme');
  if (th) return th === 'dark';
  return window.matchMedia('(prefers-color-scheme: dark)').matches;
}

async function openFile(path) {
  const open = tabs.find((x) => x.path === path);
  if (open) { activate(open); return; }
  setStatus(t('status.loading'));
  let d;
  try {
    d = await api(`${API}/read?path=${encodeURIComponent(path)}`);
  } catch (e) { fail(e); return; }
  setStatus('');
  // A double click starts two reads; the second finds the tab of the first.
  const again = tabs.find((x) => x.path === d.path);
  if (again) { activate(again); return; }
  const tab = {
    path: d.path, name: baseName(d.path), enc: d.encoding, eol: d.eol, diskEnc: d.encoding, diskEol: d.eol, sha: d.sha,
    dirty: false, readOnly: !!d.readOnly, language: NotepadEditor.languageFor(d.path),
  };
  tab.el = document.createElement('div');
  tab.el.className = 'np-editor';
  $('#editors').appendChild(tab.el);
  tab.ed = NotepadEditor.create({
    parent: tab.el, doc: d.text, language: tab.language, dark: isDark(), wrap: settings.wrap, readOnly: tab.readOnly,
    onChange: () => refreshDirty(tab),
    onCursor: () => { if (tab === active) showCursor(tab); },
    onSave: () => save(tab),
  });
  tab.savedDoc = tab.ed.view.state.doc;
  tab.btn = document.createElement('div');
  tab.btn.className = 'np-tab';
  tab.btn.setAttribute('role', 'tab');
  tab.btn.tabIndex = -1;
  tab.btn.title = tab.path;
  tab.btn.innerHTML = `<span class="np-dirty" hidden>●</span><span>${esc(tab.name)}</span><button type="button" class="np-x" aria-label="${esc(t('btn.close'))}">×</button>`;
  tab.btn.addEventListener('click', (ev) => { if (!ev.target.classList.contains('np-x')) activate(tab); });
  tab.btn.addEventListener('keydown', (ev) => tabKey(ev, tab));
  tab.btn.querySelector('.np-x').addEventListener('click', () => closeTab(tab));
  $('#tabs').appendChild(tab.btn);
  tabs.push(tab);
  activate(tab);
  rememberTabs();
  if (d.mixedEol) setStatus(t('status.mixedEol', { name: tab.name, eol: EOL_LABEL[tab.eol] }), 'err');
}

function activate(tab) {
  active = tab;
  for (const x of tabs) {
    x.el.hidden = x !== tab;
    x.btn.classList.toggle('active', x === tab);
    x.btn.setAttribute('aria-selected', x === tab ? 'true' : 'false');
    x.btn.tabIndex = x === tab ? 0 : -1;
  }
  $('#welcome').hidden = !!tab;
  $('#statusbar').hidden = !tab;
  for (const id of ['saveBtn', 'reloadBtn', 'searchBtn', 'gotoBtn']) $(`#${id}`).disabled = !tab;
  if (tab) {
    currentDir = parentOf(tab.path);
    updateBar();
    tab.ed.focus();
    showCursor(tab);
  }
}

// Tab list keys (WAI-ARIA tabs): arrows move between tabs, Delete closes one.
function tabKey(ev, tab) {
  const i = tabs.indexOf(tab);
  const to = { ArrowLeft: i - 1, ArrowRight: i + 1, Home: 0, End: tabs.length - 1 }[ev.key];
  if (to !== undefined) {
    ev.preventDefault();
    const next = tabs[(to + tabs.length) % tabs.length];
    activate(next);
    next.btn.focus();
  } else if (ev.key === 'Delete') {
    ev.preventDefault();
    closeTab(tab);
  }
}

function showCursor(tab) {
  const st = tab.ed.view.state;
  const head = st.selection.main.head;
  const line = st.doc.lineAt(head);
  $('#barPos').textContent = t('bar.line', { line: line.number, col: head - line.from + 1 });
  $('#barLines').textContent = t('bar.lines', { n: st.doc.lines });
}

// Modified = the text differs from what is on disk, or the encoding or line ending will change the bytes.
// Text.eq compares the length first, so typing in a large file costs no full copy.
function isDirty(tab) {
  return !tab.ed.view.state.doc.eq(tab.savedDoc) || tab.enc !== tab.diskEnc || tab.eol !== tab.diskEol;
}

function refreshDirty(tab) {
  tab.dirty = isDirty(tab);
  tab.btn.querySelector('.np-dirty').hidden = !tab.dirty;
  if (tab === active) updateBar();
}

function updateBar() {
  const tab = active;
  const state = $('#barState');
  state.textContent = tab.dirty ? t('bar.modified') : t('bar.clean');
  state.className = 'pill ' + (tab.dirty ? 'pill-warn' : 'pill-ok');
  $('#barRo').hidden = !tab.readOnly;
  $('#saveBtn').disabled = tab.readOnly;
  $('#langPick').value = tab.language;
  $('#encPick').value = tab.enc;
  $('#eolPick').value = tab.eol;
}

async function closeTab(tab) {
  if (tab.dirty) {
    const v = await ask({
      title: t('dialog.closeTitle'), text: t('dialog.closeText', { name: tab.name }),
      buttons: [{ label: t('btn.cancel'), value: '' }, { label: t('btn.discard'), value: 'discard', kind: 'danger' }, { label: t('btn.saveAndClose'), value: 'save', kind: 'primary' }],
    });
    if (!v) return;
    if (v === 'save' && !(await save(tab))) return;
  }
  const i = tabs.indexOf(tab);
  tabs.splice(i, 1);
  tab.ed.destroy();
  tab.el.remove();
  tab.btn.remove();
  rememberTabs();
  activate(tabs[Math.min(i, tabs.length - 1)] || null);
}

function rememberTabs() {
  safeSet('notepad.tabs', JSON.stringify(tabs.map((x) => x.path)));
}

// ---------- save / reload ----------
// One save per tab at a time: a second one would send the old sha and report a false conflict.
async function save(tab, force = false) {
  if (!tab) return false;
  if (tab.saving) return tab.saving;
  tab.saving = doSave(tab, force);
  try { return await tab.saving; } finally { tab.saving = null; }
}

async function doSave(tab, force) {
  if (!force && !isDirty(tab)) { setStatus(t('status.unchanged')); return true; }
  // What is sent is what counts as saved: typing during the request keeps the tab modified.
  const doc = tab.ed.view.state.doc;
  const { enc, eol } = tab;
  try {
    const r = await writeRaw({ path: tab.path, sha: tab.sha, encoding: enc, eol, ...(force ? { force: '1' } : {}) }, doc.toString());
    tab.sha = r.sha;
    tab.savedDoc = doc;
    tab.diskEnc = enc;
    tab.diskEol = eol;
    refreshDirty(tab);
    setStatus(t('status.saved', { name: tab.name }), 'ok');
    return true;
  } catch (e) {
    if (e.code === 'not_found') {
      const v = await ask({
        title: t('dialog.goneTitle'), text: t('dialog.goneText', { name: tab.name }),
        buttons: [{ label: t('btn.cancel'), value: '' }, { label: t('btn.recreate'), value: 'recreate', kind: 'primary' }],
      });
      if (v !== 'recreate') return false;
      try {
        await api(`${API}/create`, { method: 'POST', body: { path: tab.path, dir: false } });
      } catch (ce) { fail(ce, tab); return false; }
      await refreshDir(parentOf(tab.path)).catch(() => {});
      return doSave(tab, true);
    }
    if (e.code === 'conflict') {
      const v = await ask({
        title: t('dialog.conflictTitle'), text: t('dialog.conflictText', { name: tab.name }),
        buttons: [{ label: t('btn.cancel'), value: '' }, { label: t('btn.reload'), value: 'reload' }, { label: t('btn.overwrite'), value: 'overwrite', kind: 'danger' }],
      });
      if (v === 'overwrite') return doSave(tab, true);
      if (v === 'reload') { await reload(tab, true); }
      return false;
    }
    fail(e, tab);
    return false;
  }
}

async function reload(tab, confirmed = false) {
  if (!tab) return;
  if (tab.dirty && !confirmed) {
    const v = await ask({
      title: t('dialog.reloadTitle'), text: t('dialog.reloadText', { name: tab.name }),
      buttons: [{ label: t('btn.cancel'), value: '' }, { label: t('btn.reload'), value: 'reload', kind: 'danger' }],
    });
    if (!v) return;
  }
  try {
    const d = await api(`${API}/read?path=${encodeURIComponent(tab.path)}`);
    tab.sha = d.sha;
    tab.enc = tab.diskEnc = d.encoding;
    tab.eol = tab.diskEol = d.eol;
    tab.readOnly = !!d.readOnly;
    tab.ed.setReadOnly(tab.readOnly);
    tab.ed.setText(d.text);
    tab.savedDoc = tab.ed.view.state.doc;
    refreshDirty(tab);
    setStatus(t('status.reloaded', { name: tab.name }), 'ok');
  } catch (e) { fail(e, tab); }
}

// ---------- create ----------
async function create(dir) {
  const name = await ask({
    title: t(dir ? 'dialog.newFolder' : 'dialog.newFile', { dir: currentDir }), input: true,
    buttons: [{ label: t('btn.cancel'), value: '' }, { label: t('btn.create'), value: 'ok', kind: 'primary' }],
  });
  if (!name) return;
  if (name.includes('/') || name === '.' || name === '..') { setStatus(t('error.bad_name'), 'err'); return; }
  try {
    const r = await api(`${API}/create`, { method: 'POST', body: { path: `${currentDir}/${name}`, dir } });
    setStatus(t('status.created', { name }), 'ok');
    await refreshDir(currentDir);
    if (!dir) await openFile(r.path);
  } catch (e) { fail(e); }
}

// ---------- settings ----------
function applyFont() {
  document.documentElement.style.setProperty('--np-font-size', `${settings.font}px`);
  for (const x of tabs) x.ed.view.requestMeasure();
}

function fillPickers() {
  const lp = $('#langPick');
  lp.textContent = '';
  for (const l of NotepadEditor.languages()) lp.add(new Option(l.label, l.id));
  const ep = $('#encPick');
  ep.textContent = '';
  for (const e of info.encodings) ep.add(new Option(ENC_LABEL[e] || e, e));
}

async function init() {
  lang = resolveLanguage();
  initTheme('notepad.theme');
  applyI18n();
  fillPickers();
  applyFont();
  $('#wrapToggle').checked = settings.wrap;

  $('#langSelect').addEventListener('change', (ev) => { lang = ev.target.value; safeSet('notepad.lang', lang); applyI18n(); });
  $('#theme-select').addEventListener('change', () => { for (const x of tabs) x.ed.setDark(isDark()); });
  window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => { for (const x of tabs) x.ed.setDark(isDark()); });
  $('#treeToggle').addEventListener('click', () => $('#tree').classList.toggle('open'));
  $('#treeRefresh').addEventListener('click', () => loadFolder(info.root, $('#treeBody')).catch(fail));
  $('#newFileBtn').addEventListener('click', () => create(false));
  $('#newFolderBtn').addEventListener('click', () => create(true));
  $('#saveBtn').addEventListener('click', () => save(active));
  $('#reloadBtn').addEventListener('click', () => reload(active));
  $('#searchBtn').addEventListener('click', () => active && active.ed.openSearch());
  $('#gotoBtn').addEventListener('click', () => active && active.ed.gotoLine());
  $('#wrapToggle').addEventListener('change', (ev) => {
    settings.wrap = ev.target.checked;
    safeSet('notepad.wrap', settings.wrap ? '1' : '0');
    for (const x of tabs) x.ed.setWrap(settings.wrap);
  });
  $('#fontSmaller').addEventListener('click', () => { settings.font = Math.max(10, settings.font - 1); safeSet('notepad.font', String(settings.font)); applyFont(); });
  $('#fontLarger').addEventListener('click', () => { settings.font = Math.min(28, settings.font + 1); safeSet('notepad.font', String(settings.font)); applyFont(); });
  $('#langPick').addEventListener('change', (ev) => { if (active) { active.language = ev.target.value; active.ed.setLanguage(active.language); } });
  // Changing encoding or line ending changes the bytes on disk: isDirty counts it as a modification.
  $('#encPick').addEventListener('change', (ev) => { if (active) { active.enc = ev.target.value; refreshDirty(active); } });
  $('#eolPick').addEventListener('change', (ev) => { if (active) { active.eol = ev.target.value; refreshDirty(active); } });
  document.addEventListener('keydown', (ev) => {
    // Inside an editor CodeMirror's Mod-s already saved and marked the event handled.
    if (ev.defaultPrevented) return;
    if ((ev.ctrlKey || ev.metaKey) && ev.key.toLowerCase() === 's') { ev.preventDefault(); save(active); }
  });
  window.addEventListener('beforeunload', (ev) => {
    if (tabs.some((x) => x.dirty)) { ev.preventDefault(); ev.returnValue = t('unload.warning'); }
  });

  try {
    info = await api(`${API}/info`);
    currentDir = info.root;
    fillPickers();
    await loadFolder(info.root, $('#treeBody'));
  } catch (e) { fail(e); return; }
  let saved = [];
  try { saved = JSON.parse(safeGet('notepad.tabs') || '[]'); } catch { saved = []; }
  for (const p of saved) if (typeof p === 'string') await openFile(p);
}

init();
