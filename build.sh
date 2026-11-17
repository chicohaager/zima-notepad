#!/bin/sh
# Build the Notepad sysext for ZimaOS.
#   sh build.sh [amd64|arm64]      (default: amd64)
# Output in dist/: notepad-<arch>.raw (+ .sha256), for amd64 also notepad.raw.
set -eu
cd "$(dirname "$0")"
ARCH="${1:-amd64}"
case "$ARCH" in
  amd64|arm64) ;;
  *) echo "unsupported arch: $ARCH (amd64|arm64)" >&2; exit 2 ;;
esac
MODKIT="${LINTUX_MODKIT:-../lintux-modkit}"
[ -f "$MODKIT/sysext/add-web.sh" ] || { echo "lintux-modkit with sysext/add-web.sh not found at $MODKIT (set LINTUX_MODKIT)" >&2; exit 1; }
# Go builds against the replace target in go.mod; the web files and scripts must come from the same checkout.
GOMODKIT=$(go list -m -f '{{.Dir}}' github.com/chicohaager/lintux-modkit)
[ "$(cd "$MODKIT" && pwd -P)" = "$(cd "$GOMODKIT" && pwd -P)" ] || { echo "MODKIT=$MODKIT but Go builds against $GOMODKIT" >&2; exit 1; }
sh tools/modkit-check.sh "$MODKIT"   # the commit of lintux-modkit.ref, no local changes
VERSION=$(cat VERSION)
WEB=build/root/usr/share/casaos/www/modules/notepad

go vet ./...
go test -count=1 ./...

# The editor bundle: exact versions from web/package-lock.json, no CDN at run time.
( cd web && { [ -d node_modules ] || npm ci --no-audit --no-fund; } && npm run --silent build )

rm -rf build/root
mkdir -p build/root
cp -a files/. build/root/
rm -f "$WEB/editor.LEGAL.txt"
mkdir -p build/root/usr/share/doc/notepad
cp README.md build/root/usr/share/doc/notepad/README
cp LICENSE build/root/usr/share/doc/notepad/LICENSE
node tools/third-party.mjs build/editor-meta.json > build/root/usr/share/doc/notepad/THIRD-PARTY.txt
sh tools/go-licenses.sh >> build/root/usr/share/doc/notepad/THIRD-PARTY.txt
# The tile: module JSON (its "services" make zpkg enable and start notepad-web), UI files, shared common.js/styles.css.
sh "$MODKIT/sysext/add-web.sh" notepad build/root "$VERSION"
# -buildvcs=false: without it Go stamps the commit and "dirty" into the binary, so a rebuild from the
# release tag would differ from the release (measured for restic-web, 2026-10-23).
CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" go build -trimpath -buildvcs=false -ldflags="-s -w -X main.version=$VERSION" \
  -o build/root/usr/bin/notepad-web ./cmd/notepad-web
mkdir -p dist
sh "$MODKIT/sysext/build-sysext.sh" notepad build/root "$ARCH" dist "$VERSION"
cat "dist/notepad-$ARCH.raw.sha256"
