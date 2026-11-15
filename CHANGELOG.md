# Changelog

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
