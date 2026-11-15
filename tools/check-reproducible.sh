#!/usr/bin/env bash
# Builds the image three times and compares the bytes: as checked out under umask 022, with
# group-writable files under umask 002 (a desktop checkout), and under umask 077. Different
# bytes mean a release image depends on the machine that built it (as zima-cron, which found
# file modes and VCS info in the Go binary this way, 2026-10-26).
#   tools/check-reproducible.sh [amd64|arm64]
set -euo pipefail
cd "$(dirname "$0")/.."
ARCH="${1:-amd64}"
OUT="dist/notepad-$ARCH.raw"
before=$(mktemp)
trap 'rm -f "$before"' EXIT
find files -printf '%m\t%p\n' > "$before"
chmod -R g-w files
(umask 022 && sh build.sh "$ARCH" > /dev/null)
a=$(sha256sum "$OUT" | cut -d' ' -f1)
chmod -R g+w files
(umask 002 && sh build.sh "$ARCH" > /dev/null)
b=$(sha256sum "$OUT" | cut -d' ' -f1)
chmod -R go-rwx files
(umask 077 && sh build.sh "$ARCH" > /dev/null)
c=$(sha256sum "$OUT" | cut -d' ' -f1)
# put every mode back as it was
while IFS="$(printf '\t')" read -r mode path; do chmod "$mode" "$path"; done < "$before"
if [ "$a" != "$b" ] || [ "$a" != "$c" ]; then
  echo "not reproducible: $OUT is $a with umask 022, $b with umask 002 and group-writable files, $c with umask 077" >&2
  exit 1
fi
echo "reproducible: $OUT $a (umask 022, 002 and 077)"
