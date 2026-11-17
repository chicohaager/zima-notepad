# Changelog

## Unreleased

Fixes from a full code and security review (17.11.2026):

- A view-only tab (invalid bytes in the chosen encoding, or a write-protected
  file) could be saved with Ctrl+S after changing its line ending or picking
  *Convert on save*, which replaced the invalid bytes with U+FFFD. Such a tab is
  never written now; line ending and *Convert on save* are off there.
- Saving a text whose bytes exceed 32 MiB (for example as UTF-16) is refused:
  the file could not have been opened or saved again.
- Opening a file while it is being saved waits for the save, so it never shows
  the new start over the old end.
- A file with a byte order mark and an invalid byte after it opens view-only in
  the BOM's encoding instead of "not a text file".
- Text typed while *Reload from disk* is loading is no longer replaced without
  asking.
- A new file or folder whose mode or owner cannot be set is removed again, so a
  retry does not report "already exists".
- Device nodes and other special files are refused before they are opened.
- Symlink chains with very long targets are bounded (a folder full of them made
  the tree view slow).
- `notepad-dev` (development only) answers only requests for its own address,
  against DNS rebinding.
- Where root cannot change the owner of the backup copy (CIFS without unix
  extensions, NFS with root_squash), saving no longer fails: the short-lived
  backup stays root's with mode 0600.
- `check.sh` runs `govulncheck` and `staticcheck` as well, and refuses a
  GitHub action that is not pinned to a commit SHA; CI pins all of them.

## 0.1.5 — 2026-11-17

- Files in other encodings: Chinese (GBK, GB18030, Big5), Japanese
  (Shift_JIS, EUC-JP), Korean (EUC-KR), Cyrillic (Windows-1251, ISO-8859-5,
  KOI8-R) and Central European (Windows-1250, ISO-8859-2) text used to open
  silently as Windows-1252 and showed wrong characters. The status bar now says
  **Encoding guessed** when the bytes look like such an encoding, and picking an
  encoding offers *Reopen* (read the bytes in it) or *Convert on save*. A file
  whose bytes are not valid in the chosen encoding opens view-only. German,
  French or Spanish ANSI files open without the note. New dependency:
  `golang.org/x/text` v0.44.0; its licence is in `THIRD-PARTY.txt`.
- CMake syntax for `CMakeLists.txt` and `.cmake`.
- README: a table of which files open — encodings and the file names behind each
  syntax; `tools/check-readme-types.py` (in `check.sh`) keeps it in step with the code.

- A link `/modules/notepad/?open=/DATA/<path>` opens that file in a tab;
  `?new=/DATA/<folder>` asks for a name and creates a file in that folder. The
  parameter is removed from the address afterwards, so a reload does not open
  or ask again.
- `tools/files-menu/`: an optional, separate sysext `notepad-files-menu` that
  adds "Open with Notepad" to the right-click and three-dot menu of a file and
  "New text file" to the menu of the empty area and the new-folder drop-down of
  the ZimaOS Files app. The Files app has no extension point, so it overlays
  patched copies of IceWhale's bundles and must be rebuilt for each Files build;
  it is not part of the release.

## 0.1.4 — 2026-11-16

- Hidden and system entries (names starting with a dot, `lost+found`) are no
  longer shown in the file tree; a "Show hidden" checkbox above the tree brings
  them back, and the choice is remembered. Creating a file or folder whose name
  starts with a dot switches the display on. A folder with only hidden entries
  says so instead of looking empty.

## 0.1.3 — 2026-11-15

- After switching the language in the tile, the cursor position and the line
  count in the status bar follow at once; they kept the previous language until
  the cursor moved.

## 0.1.2 — 2026-11-15

- Only the ZimaOS administrator may use the tile; a session of a sub-account
  gets 403 and a visible notice (lintux-modkit `auth`).
- Removing the backup copy after a save no longer turns a finished save into an
  error; a failure is logged.

## 0.1.1 — 2026-11-14

- Access to `/DATA` through one `os.Root` handle: a symlink swapped in between
  check and open can no longer redirect a read, write or chmod outside `/DATA`.
- A file that grew past the limit is no longer read whole when saving; parallel
  saves of one file get a conflict; read-only files open read-only; at most two
  large texts at a time.
- Tile: typing while saving, Enter in dialogs, double click, line ending and
  undo, re-creating a deleted file, keyboard use of the tabs.

## 0.1.0 — 2026-11-14

- First version: file tree of `/DATA`, tabs, syntax colours, find and replace,
  saving in place with encoding, line endings, owner and mode kept, conflict
  check, five languages, light and dark.
