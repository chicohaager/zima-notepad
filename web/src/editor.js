// CodeMirror 6 wrapper for the zima-notepad tile. Bundled by esbuild into one
// IIFE (global NotepadEditor) so the module ships without a CDN.
import { EditorState, Compartment } from '@codemirror/state';
import {
  EditorView, keymap, lineNumbers, highlightActiveLine, highlightActiveLineGutter,
  highlightSpecialChars, drawSelection, rectangularSelection, crosshairCursor, dropCursor,
} from '@codemirror/view';
import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands';
import {
  search, searchKeymap, openSearchPanel, highlightSelectionMatches, gotoLine,
} from '@codemirror/search';
import {
  syntaxHighlighting, defaultHighlightStyle, bracketMatching, foldGutter, foldKeymap,
  indentOnInput, StreamLanguage, HighlightStyle,
} from '@codemirror/language';
import { closeBrackets, closeBracketsKeymap } from '@codemirror/autocomplete';
import { tags as t } from '@lezer/highlight';

import { javascript } from '@codemirror/lang-javascript';
import { json } from '@codemirror/lang-json';
import { yaml } from '@codemirror/lang-yaml';
import { python } from '@codemirror/lang-python';
import { markdown } from '@codemirror/lang-markdown';
import { html } from '@codemirror/lang-html';
import { css } from '@codemirror/lang-css';
import { xml } from '@codemirror/lang-xml';
import { sql } from '@codemirror/lang-sql';
import { php } from '@codemirror/lang-php';
import { go } from '@codemirror/lang-go';
import { rust } from '@codemirror/lang-rust';
import { cpp } from '@codemirror/lang-cpp';
import { java } from '@codemirror/lang-java';
import { shell } from '@codemirror/legacy-modes/mode/shell';
import { nginx } from '@codemirror/legacy-modes/mode/nginx';
import { dockerFile } from '@codemirror/legacy-modes/mode/dockerfile';
import { toml } from '@codemirror/legacy-modes/mode/toml';
import { properties } from '@codemirror/legacy-modes/mode/properties';
import { diff } from '@codemirror/legacy-modes/mode/diff';
import { lua } from '@codemirror/legacy-modes/mode/lua';
import { perl } from '@codemirror/legacy-modes/mode/perl';
import { powerShell } from '@codemirror/legacy-modes/mode/powershell';

const legacy = (m) => () => StreamLanguage.define(m);

// id -> [display name, factory]
const LANGUAGES = {
  text: ['Plain text', null],
  javascript: ['JavaScript', () => javascript()],
  typescript: ['TypeScript', () => javascript({ typescript: true })],
  json: ['JSON', () => json()],
  yaml: ['YAML', () => yaml()],
  python: ['Python', () => python()],
  markdown: ['Markdown', () => markdown()],
  html: ['HTML', () => html()],
  css: ['CSS', () => css()],
  xml: ['XML', () => xml()],
  sql: ['SQL', () => sql()],
  php: ['PHP', () => php()],
  go: ['Go', () => go()],
  rust: ['Rust', () => rust()],
  cpp: ['C/C++', () => cpp()],
  java: ['Java', () => java()],
  shell: ['Shell', legacy(shell)],
  nginx: ['Nginx', legacy(nginx)],
  dockerfile: ['Dockerfile', legacy(dockerFile)],
  toml: ['TOML', legacy(toml)],
  ini: ['INI / Properties', legacy(properties)],
  diff: ['Diff', legacy(diff)],
  lua: ['Lua', legacy(lua)],
  perl: ['Perl', legacy(perl)],
  powershell: ['PowerShell', legacy(powerShell)],
};

const BY_EXT = {
  js: 'javascript', mjs: 'javascript', cjs: 'javascript', jsx: 'javascript', ts: 'typescript', tsx: 'typescript',
  json: 'json', jsonc: 'json', webmanifest: 'json',
  yml: 'yaml', yaml: 'yaml', py: 'python', md: 'markdown', markdown: 'markdown',
  html: 'html', htm: 'html', vue: 'html', css: 'css', scss: 'css', less: 'css',
  xml: 'xml', svg: 'xml', plist: 'xml', xsd: 'xml', sql: 'sql', php: 'php', go: 'go', rs: 'rust',
  c: 'cpp', h: 'cpp', cc: 'cpp', cpp: 'cpp', hpp: 'cpp', java: 'java', kt: 'java',
  sh: 'shell', bash: 'shell', zsh: 'shell', env: 'shell', profile: 'shell', bashrc: 'shell',
  toml: 'toml', ini: 'ini', conf: 'ini', cfg: 'ini', properties: 'ini', service: 'ini', timer: 'ini',
  mount: 'ini', network: 'ini', desktop: 'ini', diff: 'diff', patch: 'diff', lua: 'lua', pl: 'perl',
  ps1: 'powershell', psm1: 'powershell',
};
const BY_NAME = {
  dockerfile: 'dockerfile', containerfile: 'dockerfile', makefile: 'shell', '.bashrc': 'shell',
  '.profile': 'shell', '.env': 'shell', 'nginx.conf': 'nginx', 'docker-compose.yml': 'yaml', 'compose.yaml': 'yaml',
};

export function languageFor(name) {
  const base = String(name || '').split('/').pop().toLowerCase();
  if (BY_NAME[base]) return BY_NAME[base];
  if (base.startsWith('dockerfile')) return 'dockerfile';
  if (base.includes('nginx') && base.endsWith('.conf')) return 'nginx';
  const dot = base.lastIndexOf('.');
  const ext = dot >= 0 ? base.slice(dot + 1) : '';
  return BY_EXT[ext] || 'text';
}

export function languages() {
  return Object.entries(LANGUAGES).map(([id, [label]]) => ({ id, label }));
}

// Colours follow the tile's CSS variables, so light and dark come from styles.css.
const darkHighlight = HighlightStyle.define([
  { tag: t.keyword, color: '#c792ea' },
  { tag: [t.name, t.deleted, t.character, t.macroName], color: '#e6e6e6' },
  { tag: [t.propertyName], color: '#82aaff' },
  { tag: [t.function(t.variableName), t.labelName], color: '#82aaff' },
  { tag: [t.color, t.constant(t.name), t.standard(t.name)], color: '#f78c6c' },
  { tag: [t.definition(t.name), t.separator], color: '#e6e6e6' },
  { tag: [t.typeName, t.className, t.number, t.changed, t.annotation, t.modifier, t.self, t.namespace], color: '#ffcb6b' },
  { tag: [t.operator, t.operatorKeyword, t.url, t.escape, t.regexp, t.link, t.special(t.string)], color: '#89ddff' },
  { tag: [t.meta, t.comment], color: '#8a93a6', fontStyle: 'italic' },
  { tag: t.strong, fontWeight: 'bold' },
  { tag: t.emphasis, fontStyle: 'italic' },
  { tag: t.link, textDecoration: 'underline' },
  { tag: t.heading, fontWeight: 'bold', color: '#82aaff' },
  { tag: [t.atom, t.bool, t.special(t.variableName)], color: '#f78c6c' },
  { tag: [t.processingInstruction, t.string, t.inserted], color: '#c3e88d' },
  { tag: t.invalid, color: '#ff5370' },
]);

const baseTheme = EditorView.theme({
  '&': { height: '100%', fontSize: 'var(--np-font-size, 14px)', backgroundColor: 'var(--np-editor-bg)', color: 'var(--text)' },
  '.cm-scroller': { fontFamily: 'var(--font-mono)', lineHeight: '1.5' },
  '.cm-gutters': { backgroundColor: 'var(--np-gutter-bg)', color: 'var(--text-muted)', borderRight: '1px solid var(--card-border)' },
  '.cm-activeLine': { backgroundColor: 'var(--np-active-line)' },
  '.cm-activeLineGutter': { backgroundColor: 'var(--np-active-line)' },
  '&.cm-focused .cm-cursor': { borderLeftColor: 'var(--accent)' },
  '&.cm-focused .cm-selectionBackground, .cm-selectionBackground, ::selection': { backgroundColor: 'var(--np-selection) !important' },
  '.cm-panels': { backgroundColor: 'var(--np-gutter-bg)', color: 'var(--text)' },
  '.cm-panels input, .cm-panels button': { fontSize: '13px' },
  '.cm-searchMatch': { backgroundColor: 'var(--warn-tint)', outline: '1px solid var(--warn)' },
  '.cm-searchMatch.cm-searchMatch-selected': { backgroundColor: 'var(--accent-tint)' },
});

export function create(opts) {
  const lang = new Compartment();
  const highlight = new Compartment();
  const wrap = new Compartment();
  const ro = new Compartment();
  const langExt = (id) => {
    const f = (LANGUAGES[id] || LANGUAGES.text)[1];
    return f ? f() : [];
  };
  const hl = (dark) => syntaxHighlighting(dark ? darkHighlight : defaultHighlightStyle, { fallback: true });
  const extraKeys = keymap.of([
    { key: 'Mod-s', preventDefault: true, run: () => { opts.onSave && opts.onSave(); return true; } },
    { key: 'Mod-g', preventDefault: true, run: gotoLine },
  ]);
  const view = new EditorView({
    parent: opts.parent,
    state: EditorState.create({
      doc: opts.doc || '',
      extensions: [
        extraKeys,
        lineNumbers(), highlightActiveLineGutter(), highlightSpecialChars(), history(), foldGutter(),
        drawSelection(), dropCursor(), EditorState.allowMultipleSelections.of(true), indentOnInput(),
        bracketMatching(), closeBrackets(), rectangularSelection(), crosshairCursor(), highlightActiveLine(),
        highlightSelectionMatches(), search({ top: true }),
        keymap.of([...closeBracketsKeymap, ...defaultKeymap, ...searchKeymap, ...historyKeymap, ...foldKeymap, indentWithTab]),
        baseTheme,
        lang.of(langExt(opts.language)),
        highlight.of(hl(opts.dark)),
        wrap.of(opts.wrap ? EditorView.lineWrapping : []),
        ro.of(EditorState.readOnly.of(!!opts.readOnly)),
        EditorView.updateListener.of((u) => {
          if (u.docChanged && opts.onChange) opts.onChange();
          if ((u.selectionSet || u.docChanged) && opts.onCursor) {
            const pos = u.state.selection.main.head;
            const line = u.state.doc.lineAt(pos);
            opts.onCursor(line.number, pos - line.from + 1, u.state.doc.lines);
          }
        }),
      ],
    }),
  });
  return {
    view,
    setText: (s) => view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: s } }),
    focus: () => view.focus(),
    openSearch: () => openSearchPanel(view),
    gotoLine: () => gotoLine(view),
    setLanguage: (id) => view.dispatch({ effects: lang.reconfigure(langExt(id)) }),
    setDark: (d) => view.dispatch({ effects: highlight.reconfigure(hl(d)) }),
    setWrap: (w) => view.dispatch({ effects: wrap.reconfigure(w ? EditorView.lineWrapping : []) }),
    setReadOnly: (r) => view.dispatch({ effects: ro.reconfigure(EditorState.readOnly.of(!!r)) }),
    destroy: () => view.destroy(),
  };
}
