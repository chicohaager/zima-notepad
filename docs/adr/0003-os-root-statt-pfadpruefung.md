# ADR 0003: Dateizugriff über ein os.Root-Handle, nicht über geprüfte Pfade

- Status: angenommen
- Datum: 2026-11-14
- Entschieden von: Maintainer (Auftrag „Security-Review, Bugs beheben“), umgesetzt nach Befund
- Kickoff-Log: `docs/kickoff/zima-notepad/decisions.md` #27 (revidiert #13), #28

## Kontext

Der Dienst läuft als root. Die erste Fassung prüfte jeden Pfad (`filepath.EvalSymlinks`, dann „liegt
unter `/DATA`“) und öffnete die Datei danach erneut über den Pfad. Wer in einem Ordner unter `/DATA`
schreiben darf (Container mit eingehängtem AppData-Ordner, SMB-Freigabe), konnte die Datei zwischen
Prüfung und Öffnen gegen einen Symlink tauschen. Gemessen im Review vom 14.11.2026: Save schrieb in eine
Datei außerhalb der Wurzel, Read gab sie zurück, Create setzte ihren Modus auf 0644.

## Entscheidung

- `internal/files` hält ein `os.Root` (Go ≥ 1.24) auf die aufgelöste Wurzel. Jede Operation öffnet über
  dieses Handle; der Kernel löst dabei jede Pfadkomponente unterhalb der Wurzel auf (`openat` mit
  `O_NOFOLLOW` je Schritt).
- Eine Datei wird je Anfrage einmal geöffnet. Prüfen (fstat), Lesen, Schreiben, Kürzen, sync laufen
  über diesen Deskriptor; die Sicherungskopie bekommt Eigentümer und Modus per fchown/fchmod.
- Anlegen: Elternordner über `os.Root` öffnen, neuen Eintrag mit `openat`/`mkdirat` relativ dazu und
  `O_NOFOLLOW`, Modus und Eigentümer auf dem neuen Deskriptor.
- `os.Root` folgt keinem absoluten Symlink. Auf einem NAS sind Links wie `/DATA/Media` aber üblich;
  `resolve` übersetzt absolute Ziele innerhalb der Wurzel in relative Pfade (höchstens 40 Links). Ein
  danach untergeschobener Link lässt das Öffnen scheitern, führt aber nicht nach außen.

## Folgen

- Die Pfadprüfung von Hand (`clean`/`within`/`Resolve`) entfällt.
- Tests in `internal/files/review_test.go` tauschen während Read/Save/Create in einer Schleife und
  verlangen, dass die Datei außerhalb unverändert bleibt; alle drei waren gegen die alte Fassung rot.
- `os.Root` hindert nicht am Überqueren von Mount-Grenzen unter `/DATA` und nicht an Gerätedateien;
  Gerätedateien und FIFOs lehnt `readText` ab (nur reguläre Dateien), das Öffnen blockiert dank
  `O_NONBLOCK` nicht.
