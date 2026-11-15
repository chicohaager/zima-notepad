# ADR 0001: zima-notepad ist eine eigene Web-App als sysext-Modul mit lintux-modkit webmod

- Status: angenommen
- Datum: 2026-11-14
- Entschieden von: Maintainer (Empfehlung angenommen: „Leg einfach los und bau“)
- Kickoff-Log: `docs/kickoff/zima-notepad/decisions.md` #10

## Kontext

Gesucht war ein Notepad++-ähnlicher Editor für ZimaOS. notepadqq (GPL-3.0, v2.1.1) ist eine
Qt6-Desktop-App mit JavaScript-Editorkern in Qt WebEngine; ZimaOS hat keinen Desktop, Apps laufen
im Browser (#2). Der Editor schreibt Dateien unter `/DATA` und braucht deshalb eine Anmeldung (#5).

## Entscheidung

Eigene Web-App (Go-Backend, CodeMirror-Frontend) als systemd-sysext-Modul mit `lintux-modkit/webmod`:
Kachel im Dashboard, Gateway-Route `/notepad`, ZimaOS-Sitzungstoken auf jeder API-Route.

## Begründung

- Sieben Lintux-Module nutzen webmod bereits (gemessen: `grep -rl lintux-modkit/webmod ~/dev/*/cmd`);
  Anmeldung, CSRF, Boot-Watchdog, Kachel und Aussehen (ZFW) sind damit fertig (#22).
- Eine Docker-App bräuchte eine eigene Anmeldung und Bind-Mounts von `/DATA`.

## Betrachtete Alternativen

| Alternative | spricht dafür | spricht dagegen | warum nicht gewählt |
|---|---|---|---|
| notepadqq unverändert im Browser-Desktop (Selkies) | echtes notepadqq, kaum Code | großes Image, Pixelstrom, Handy schlecht, eigene Anmeldung | für ein Store-Produkt (#3) zu schwer |
| Editorkern von notepadqq herauslösen | vorhandener JS-Code | hängt an der Qt-Brücke, GPL-3 | kein Gewinn gegenüber CodeMirror direkt |
| code-server | sofort lauffähig | IDE statt Notepad, schwer | Nicht-Ziel „keine IDE“ (#7) |

## Folgen

- Leichter wird: Auslieferung über Module Manager, Anmeldung ohne eigene Benutzerverwaltung.
- Schwerer wird: Abhängigkeit von lintux-modkit; die 32-MiB-Grenze brauchte dort die neue Option
  `Options.MaxBody` (lintux-modkit v0.1.4, #24).
