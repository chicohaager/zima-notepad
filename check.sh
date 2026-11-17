#!/bin/sh
# Everything that must be green before a commit:
#   gofmt, go vet, go test, translations, editor bundle, browser test (Playwright).
#   sh check.sh
# PLAYWRIGHT_PYTHON points to a Python with playwright (default ~/.venvs/playwright/bin/python3).
set -eu
cd "$(dirname "$0")"
MODKIT="${LINTUX_MODKIT:-../lintux-modkit}"
PY="${PLAYWRIGHT_PYTHON:-$HOME/.venvs/playwright/bin/python3}"

unformatted=$(gofmt -l cmd internal)
[ -z "$unformatted" ] || { echo "gofmt: $unformatted" >&2; exit 1; }
go vet ./...
go test -count=1 ./...
python3 tools/check-i18n.py
python3 tools/check-readme-types.py
python3 tools/files-menu/test_patch.py
sh tools/test-modkit-check.sh
sh tools/modkit-check.sh "$MODKIT"
for f in files/usr/share/casaos/www/modules/notepad/app.js files/usr/share/casaos/www/modules/notepad/i18n.js; do node --check "$f"; done

# UI as the browser gets it from the dev server (same handler as on ZimaOS, no gateway, no login).
( cd web && { [ -d node_modules ] || npm ci --no-audit --no-fund; } && npm run --silent build )
node --check files/usr/share/casaos/www/modules/notepad/editor.js
rm -rf build/dev-web && mkdir -p build/dev-web
cp -a files/usr/share/casaos/www/modules/notepad/. build/dev-web/
cp "$MODKIT/web/common.js" "$MODKIT/web/styles.css" build/dev-web/
sed -i 's/@VERSION@/dev/g' build/dev-web/index.html
go build -o build/notepad-dev ./cmd/notepad-dev
"$PY" tests/e2e.py
if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
  sh tests/bindmount.sh
else
  echo "check.sh: SKIPPED tests/bindmount.sh — no docker here; the single-file bind mount is NOT checked" >&2
  exit 1
fi
echo "check.sh: all green"
