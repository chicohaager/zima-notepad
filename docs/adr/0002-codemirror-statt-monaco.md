# ADR 0002: Der Editor ist CodeMirror 6, nicht Monaco

- Status: angenommen
- Datum: 2026-11-14
- Entschieden von: Maintainer (Empfehlung angenommen)
- Kickoff-Log: `docs/kickoff/zima-notepad/decisions.md` #11

## Kontext

Die App soll auch am Handy bedienbar sein (#6) und ohne CDN ausgeliefert werden.

## Entscheidung

CodeMirror 6 (codemirror 6.0.2, @codemirror/view 6.43.15), gebündelt mit esbuild 0.28.2 zu einer
Datei `editor.js`; Versionen fest in `web/package-lock.json`.

## Begründung

- Monaco-README auf die Frage nach mobilen Browsern: „No.“ (gh api repos/microsoft/monaco-editor/readme,
  Zeile 104–106, abgerufen 14.11.2026).
- CodeMirror 6 ist MIT-lizenziert; alle 42 gebündelten Pakete sind MIT (gemessen: tools/third-party.mjs).
- Im Browsertest am Handy-Viewport (390 px) öffnet und bearbeitet die Kachel Dateien ohne Querscrollen.

## Betrachtete Alternativen

| Alternative | spricht dafür | spricht dagegen | warum nicht gewählt |
|---|---|---|---|
| Monaco | VS-Code-Gefühl, Minimap | laut eigener Doku nicht für mobile Browser | Handy-Nutzung (#6) |

## Folgen

- Leichter wird: Bündel 1,0 MB, eine Datei, offline.
- Schwerer wird: Notepad++-Extras (Minimap, Makros) müssten selbst gebaut werden.
