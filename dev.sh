#!/bin/sh
# Run the tile on this machine (no ZimaOS, no authentication, loopback only):
#   sh dev.sh [root-folder]     default: build/dev-data with a few sample files
# Then open http://127.0.0.1:8471/modules/notepad/
set -eu
cd "$(dirname "$0")"
MODKIT="${LINTUX_MODKIT:-../lintux-modkit}"
ROOT="${1:-build/dev-data}"
WEB=build/dev-web
(cd web && npm run --silent build)
rm -rf "$WEB" && mkdir -p "$WEB"
cp -a files/usr/share/casaos/www/modules/notepad/. "$WEB/"
cp "$MODKIT/web/common.js" "$MODKIT/web/styles.css" "$WEB/"
sed -i 's/@VERSION@/dev/g' "$WEB/index.html"
if [ "$ROOT" = build/dev-data ] && [ ! -d "$ROOT" ]; then
  mkdir -p "$ROOT/AppData/demo" "$ROOT/Documents"
  cp -a testdata/sample/. "$ROOT/"
fi
go build -o build/notepad-dev ./cmd/notepad-dev
exec build/notepad-dev -root "$ROOT" -web "$WEB" -addr "${NOTEPAD_DEV_ADDR:-127.0.0.1:8471}"
