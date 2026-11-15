# Spezifikation: zima-notepad – Notepad++-ähnlicher Editor für ZimaOS

- Thema: zima-notepad
- Stand: 2026-11-15
- Reifegrad: Produktivsystem – Produkt für fremde Nutzer über den Store (#3)
- Entscheidungs-Log: `docs/kickoff/zima-notepad/decisions.md`
- Freigabe: erteilt 2026-11-15

Jede Aussage nennt die Log-Nummer, aus der sie stammt. Der Maintainer hat am 14.11.2026 nach Frage 2 die
übrigen Fragen der Empfehlung überlassen („Leg einfach los und bau, nachdem du das geplant hast“);
diese Entscheidungen stehen im Log als „Annahme“ und hier unter 18.

## 1. Ziel

ZimaOS-Nutzer wollen Textdateien auf ihrer Box (`.conf`, `.yaml`, Compose-Dateien, Skripte,
Notizen) direkt im Browser anlegen und bearbeiten, ohne PC-Editor, SMB-Freigabe oder SSH. Der
Editor der Files-App hat keinen Speicher-Knopf, kann keine Datei anlegen und nicht von der Platte
neu laden; im Forum wird ein Texteditor wiederholt verlangt (#4, #5).

**Erfolg in einem Satz:** Ein Nutzer installiert zima-notepad aus dem Store, öffnet eine Datei
unter `/DATA` im Browser (auch am Handy), bearbeitet sie mit Notepad++-Komfort und speichert sie –
Kodierung, Zeilenenden und Dateirechte bleiben unverändert (#5).

## 2. Nutzer

Besitzer und Admins einer ZimaOS-Box, technisch mittel, meist ein Konto; Browser am PC und am
Handy (#6).

## 3. Scope

**Gehört dazu:** Dateibaum ab `/DATA`, Tabs, Syntaxfarben, Suchen/Ersetzen mit Regex in der
Datei, Gehe zu Zeile, neue Datei und neuer Ordner, Speichern per Strg+S und Knopf, von Platte neu
laden, Kodierung und Zeilenende anzeigen, behalten und umstellen, Konflikterkennung, Warnung bei
Ungespeichertem; Aussehen wie ZFW und alle Lintux-Module (lintux-modkit `styles.css`), hell/dunkel/auto (#22); mehrsprachig: Englisch, Deutsch, Französisch, Spanisch, Chinesisch, folgt der ZimaOS-Systemsprache (#21) (#8, #15).

**Gehört ausdrücklich nicht dazu:** keine IDE (kein LSP, Terminal, Git, Plugins), kein gemeinsames
Live-Bearbeiten, keine Binär- oder Hex-Bearbeitung, kein Dateimanager-Ersatz (nur neue Datei und
neuer Ordner), keine Makros und kein Dateivergleich in 0.1 (#7).

**Erster nutzbarer Stand:** Version 0.1 mit dem Umfang oben (#8).

**Ort:** `~/dev/zima-notepad` (#1). Ausgangspunkt: notepadqq ist eine Qt6-Desktop-App (GPL-3.0) mit JS-Editorkern in Qt WebEngine; ZimaOS hat keinen Desktop, daher keine Portierung, sondern eigene Web-App (#2, #10).

**Feste Vorgaben:** Auslieferung als systemd-sysext-Modul mit lintux-modkit (#10); Store-Eintrag
und öffentliches Repo erst nach Test des Maintainers (#9, #20).

## 4. Anforderungen

### Abläufe

| ID | Ablauf (Auslöser → Ergebnis) | Abnahme: woran geprüft | Log |
|---|---|---|---|
| F1 | Kachel öffnen → Dateibaum von `/DATA`, Ordner auf- und zuklappen | Playwright: Baum zeigt Testordner | #8 |
| F2 | Datei anklicken → öffnet in neuem Tab mit Syntaxfarben passend zur Endung; zweiter Klick wechselt nur den Tab | Playwright: Tab-Anzahl, Sprache in der Statusleiste | #8, #11 |
| F3 | Bearbeiten und Strg+S oder Knopf → Datei auf Platte geändert, Inode/Modus/Eigentümer/Gruppe gleich, Kodierung und Zeilenende gleich; ein Container mit einzeln eingehängter Datei sieht die neue Fassung | Go-Test je Kodierung; Playwright: Inhalt auf Platte; `tests/bindmount.sh` | #15, #28 |
| F4 | Datei wurde inzwischen außen geändert → Speichern meldet Konflikt mit Wahl „Überschreiben“ oder „Neu laden“ | Go-Test 409; Playwright: Dialog | #28 |
| F5 | Neue Datei / neuer Ordner im gewählten Ordner → angelegt, Datei öffnet als Tab | Playwright | #8 |
| F6 | Suchen/Ersetzen (auch Regex), Gehe zu Zeile | Playwright: Ersetzung im Inhalt | #8 |
| F7 | Tab mit Ungespeichertem schließen oder Seite verlassen → Warnung | Playwright | #8 |
| F8 | Datei > 32 MiB oder mit NUL-Bytes → klare Meldung, nichts geöffnet | Go-Test | #28 |
| F9 | Pfad außerhalb `/DATA` (`..`, Symlink nach außen) → abgelehnt | Go-Test | #27 |
| F10 | ohne gültiges ZimaOS-Sitzungstoken → jede API-Route 401 | Go-Test über webmod-verify | #10 |

### Rahmenwerte

| Eigenschaft | Wert | Log |
|---|---|---|
| Datenmenge, Last | Dateien bis 32 MiB, ein bis wenige gleichzeitige Nutzer | #6, #28 |
| Antwortzeit, erlaubter Stillstand | Datei bis 1 MB öffnet in unter 1 s im LAN; Ausfall stört keine andere App | #10 |
| Sprachen, Zeitzonen | Englisch, Deutsch, Französisch, Spanisch, Chinesisch; folgt der ZimaOS-Systemsprache, im Modul umschaltbar | #21 |
| Barrierefreiheit | Tastaturbedienung aller Funktionen, Kontrast hell/dunkel | #8 |
| Rechtliche Pflichten | Lizenzen der gebündelten Bibliotheken (MIT) im Modul unter `usr/share/doc/notepad` | #9, #11 |
| Bestandsdaten, Migration | keine | #17 |

## 5. Betriebsziel

ZimaOS 1.8 auf amd64 und arm64, als sysext-Image `notepad.raw` (plus `-amd64`, `-arm64`), installiert
mit `zpkg` bzw. dem Module Manager; Oberfläche über das ZimaOS-Gateway unter `/modules/notepad/`,
API unter `/notepad/…` (#10, #20).

## 6. Architektur

**Grundform:** eigene Web-App als sysext-Modul mit `lintux-modkit/webmod`: Kachel im Dashboard,
Loopback-Listener, Gateway-Route `/notepad`, ZimaOS-Sitzungstoken auf jeder API-Route, CSRF-Schutz,
Boot-Watchdog (#10).

**Bausteine und erlaubte Abhängigkeiten:** `cmd/notepad-web` (main, verdrahtet webmod) →
`internal/files` (Pfadschutz, Lesen, Schreiben in place mit Sicherungskopie, Kodierung); Frontend `web/` (CodeMirror 6,
gebündelt) spricht nur die eigene API (#11, #12).

**Zustand und Quelle der Wahrheit:** Dateien auf der Platte; der Server hält keinen Zustand.
Einstellungen und offene Tabs im localStorage des Browsers (#17).

**Hintergrundarbeit und Verhalten bei Teilausfall:** keine Hintergrundarbeit; fällt der Dienst aus,
startet systemd ihn neu (`Restart=always`), ungespeicherte Änderungen bleiben im Browser-Tab (#10).

## 7. Daten

Keine eigene Datenhaltung. Speichern überschreibt die Datei an Ort und Stelle (der Inode bleibt, damit ein
Container, der genau diese Datei einhängt, die neue Fassung sieht); vorher kommt die alte Fassung als
Kopie neben die Datei, nach Erfolg wird sie gelöscht, bei einem Fehler zurückgeschrieben (wie Vims
`backupcopy=yes`). Modus, Eigentümer und Gruppe bleiben dadurch von selbst. Beim Laden liefert der Server sha256 und mtime;
weicht die Platte beim Speichern ab, antwortet er 409; zwei Speichervorgänge laufen nacheinander, nie
gleichzeitig. Eine schreibgeschützte Datei (kein Schreibbit) öffnet nur lesend und wird nicht gespeichert. Öffnen und Speichern bis 32 MiB; dafür setzt das Modul die neue webmod-Option `MaxBody` auf 33 MiB (lintux-modkit, Branch `feat/webmod-maxbody`) (#28). Kodierungen: UTF-8 mit und ohne BOM, UTF-16
LE/BE mit BOM, sonst Windows-1252; Zeilenenden LF, CRLF, CR; beides wird beim Speichern erhalten,
ein nicht darstellbares Zeichen ergibt einen Fehler statt stillen Verlust (#15, #28).

## 8. Tech-Stack

| Baustein | Wahl | Version | Beleg (Quelle, abgerufen am) | Log |
|---|---|---|---|---|
| Sprache/Laufzeit | Go | 1.27.4 | `go version`, go.dev/dl, 14.11.2026 | #12 |
| Framework | lintux-modkit webmod | Checkout `1f5a43f` | `git -C ~/dev/lintux-modkit log -1`, 14.11.2026 | #10 |
| Editor | CodeMirror 6 | codemirror 6.0.2, @codemirror/view 6.43.15 | `npm view`, 14.11.2026 | #11 |
| Paketverwaltung/Build | npm + esbuild | esbuild 0.28.2 | `npm view esbuild version`, 14.11.2026 | #11 |

**Abhängigkeiten:** Versionen fest in `go.mod` und `package-lock.json`; lintux-modkit per `replace
../lintux-modkit` wie zima-cron; Frontend wird beim Bau gebündelt, nichts von einem CDN (#11, #12).

**Entwicklungsumgebung:** `sh dev.sh` startet `cmd/notepad-dev` lokal mit einer Testwurzel, ohne
Anmeldung und nur auf 127.0.0.1; dieses Programm kommt nicht ins Image (#29).

## 9. Oberfläche

Eine Seite: links Dateibaum, rechts Tabs mit Editor, unten Statusleiste (Zeile:Spalte, Sprache,
Kodierung, Zeilenende, gespeichert/geändert), Kopf wie bei ZFW mit Thema- und Sprachwahl (#21, #22). Handy: Baum als ausklappbare Leiste. Fehler erscheinen
sichtbar als Meldung, nie still; Laden zeigt einen Zustand (#8, #11).

## 10. Schnittstellen

Eigene JSON-API unter `/notepad/`: `list`, `read`, `write`, `create`, `health`; nur für die eigene
Oberfläche, kein Kompatibilitätsversprechen in 0.x. Fremdsystem: ZimaOS-Gateway und Sitzungstoken
über lintux-modkit (#10).

## 11. Security

Anmeldung über das ZimaOS-Sitzungstoken (webmod verify), CSRF über httpx (#10). Nur eine Sitzung mit
`role=admin` kommt durch; ein ZimaOS-Unterkonto (oder eine fehlende Rolle) bekommt 403 `auth_forbidden`
und die Kachel einen Hinweis, zentral in lintux-modkit `auth` ab v0.1.4 (#30). Der Dienst läuft
als root wie die anderen Module; deshalb nur Zugriff unter `/DATA`, über ein `os.Root`-Handle auf die
aufgelöste Wurzel: jede Datei wird einmal geöffnet und über diesen Deskriptor geprüft, gelesen und
geschrieben, ein nachträglich untergeschobener Symlink führt nicht nach außen (#27). Höchstens zwei
Text-Anfragen laufen gleichzeitig (#28). Keine Geheimnisse im Modul. Lieferkette:
festgelegte Versionen, gebündeltes Frontend (#11).

## 12. Codequalität

`gofmt`, `go vet`, `node --check` für das gebündelte JS; Fehler der API mit festen Codes über httpx;
Code, Kommentare, Commits auf Englisch, Oberfläche in fünf Sprachen (#12, #19, #21).

## 13. Testing und Definition of Done

**Fertig heißt:** `sh check.sh` ist grün (#19) und das Image läuft auf einem ZimaOS-Testhost:
Kachel öffnet, eine Datei wird bearbeitet und gespeichert, Modus/Eigentümer/sha256 vorher-nachher
belegt (#29).

Go-Tests für Pfadschutz, Kodierung (Rundreise je Form), Speichern in place (Inode, Rückschreiben, Sicherungskopie), Konflikt, Auth; `tests/bindmount.sh` (Docker, einzeln eingehängte Datei);
Playwright im echten Browser gegen den Dev-Server; jeder Test einmal rot (#29).

## 14. CI/CD

`build.sh [amd64|arm64]` mit `build-sysext.sh` und `add-web.sh` aus lintux-modkit; `check.sh` fährt
go vet, go test, Frontend-Bau, node --check und Playwright. GitHub Actions mit lintux-modkit auf einem
festen Tag (v0.1.4) baut beide Images und hängt sie an ein Release (#19).

## 15. Betrieb

Keine Konfiguration. Update über `zpkg`/Module Manager; webmod meldet ein ersetztes Binary in
`/notepad/health`. Rückweg: vorherige Release-Datei installieren (#20).

## 16. Doku

README (Englisch) mit Funktionen, Installation, Sicherheit; CHANGELOG; diese Spezifikation und das
Log (#9, #19).

## 17. Übersprungene Zweige

| Zweig | Grund | Log |
|---|---|---|
| Schnittstellen zu Fremdsystemen über ZimaOS hinaus | keine vorhanden | #10 |

## 18. Annahmen

| Annahme | Betrifft | Wie sie sich klären lässt | Log |
|---|---|---|---|
| Nutzer: Box-Admins, PC und Handy | 2 | Rückmeldung aus dem Forum nach Vorstellung | #6 |
| Nicht-Ziele wie in 3 | 3 | Test des Maintainers | #7 |
| Umfang 0.1 wie in 3 | 3, 21 | Test des Maintainers | #8 |
| öffentliches Repo, MIT, erst nach Test des Maintainers | 16 | Maintainer | #9 |
| sysext-Modul mit webmod statt Docker-App | 5, 6 | Maintainer | #10 |
| CodeMirror 6 statt Monaco | 8, 9 | Handytest | #11 |
| Go 1.27.4, lintux-modkit per replace | 8 | – | #12 |
| nur `/DATA`, Pfadschutz | 11 | Maintainer | #27 |
| Speichern in place mit Sicherungskopie, 409 bei Konflikt, NUL ⇒ binär | 7 | Tests | #28 |
| Kodierungen und Zeilenenden wie in 7 | 7 | Tests mit echten Dateien von der Box | #15 |
| Einstellungen im localStorage | 6 | – | #17 |
| Teststrategie wie in 13 | 13 | – | #29 |
| check.sh/build.sh, CI später | 14 | – | #19 |
| Auslieferung über Release und Katalog nach Test | 15 | Maintainer | #20 |

## 19. Offene Punkte und Risiken

| Punkt | Folge, wenn er offen bleibt | Wer klärt | bis wann |
|---|---|---|---|
| Rollenwert eines ZimaOS-Unterkontos nicht gemessen (auf dem Testhost 0 Unterkonten) | keiner für die Sicherheit: jede Rolle außer `admin` wird abgewiesen; offen ist nur, ob ein Unterkonto die Kachel überhaupt sieht | Maintainer (Unterkonto anlegen und Kachel öffnen) | nach dem Release |

## 20. Architekturentscheidungen

| ADR | Titel | Log |
|---|---|---|
| 0001 | sysext-Modul mit webmod statt Docker-App oder notepadqq im Browser-Desktop | #10 |
| 0002 | CodeMirror 6 statt Monaco | #11 |

## 21. Umsetzungsplan

| Schritt | Ergebnis | Fertig, wenn | deckt Abläufe |
|---|---|---|---|
| 1 | Gerüst: go.mod, `cmd/notepad-web` mit webmod, `/notepad/health`, leere Seite | `go build ./...` grün, Dev-Server antwortet auf `/notepad/health` | F10 |
| 2 | `internal/files`: Pfadschutz, Liste, Lesen mit Kodierung/Zeilenende/sha256, Schreiben in place, Anlegen | `go test ./...` grün, jeder Test einmal rot | F3, F4, F8, F9 |
| 3 | API-Handler list/read/write/create + Auth-Test | `go test ./...` grün | F1–F5, F10 |
| 4 | Frontend: Baum, Tabs, CodeMirror, Statusleiste, Speichern, Konfliktdialog, Suchen/Ersetzen, Neu, Warnung, ZFW-Aussehen hell/dunkel, 5 Sprachen | Playwright gegen Dev-Server grün, Screenshots angesehen | F1–F7 |
| 5 | Paket: Unit, Modul-JSON, Icon, build.sh, check.sh, README, Lizenzen | `sh check.sh` und `sh build.sh amd64` grün, Image enthält die erwarteten Dateien (`unsquashfs -l`) | alle |
| 6 | Einbau-Probe auf einem ZimaOS-Testhost | Kachel öffnet, Datei bearbeitet, Modus/Eigentümer/sha256 belegt | F1–F3 |
