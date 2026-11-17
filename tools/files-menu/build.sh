#!/bin/sh
# Build the sysext "notepad-files-menu": "Open with Notepad" and "New text file" in the ZimaOS Files menus.
#
#   sh tools/files-menu/build.sh <amd64|arm64> <Files-*.js|main-*.js> ...
#
# The Files app has no extension point for its menus, so this image carries patched copies of
# its bundles, Files-<hash>.js and main-<hash>.js of each Files build (tools/files-menu/patch.py). systemd-sysext stacks the
# extensions in version order of their names, the last on top (src/sysext/sysext.c, v258.7);
# "notepad-files-menu" sorts after "icewhale_files", so its copy of the same path wins.
# When a Files update brings a bundle with a new hash, the old copy is simply not loaded any
# more: the Files app keeps working, the entry is gone until this is rebuilt from the new bundle.
#
# The bundles are IceWhale's code: the image is for one's own hosts, never a release asset,
# and it is written to build/files-menu/out, not dist/.
# Get a bundle from a host:
#   ssh <user>@<host> 'ls /usr/share/casaos/www/modules/icewhale_files/assets/Files-*.js /usr/share/casaos/www/modules/icewhale_files/assets/main-*.js'
set -eu
cd "$(dirname "$0")/../.."
ARCH="${1:?amd64|arm64}"; shift
[ $# -ge 1 ] || { echo "usage: build.sh <amd64|arm64> <Files-*.js|main-*.js> ..." >&2; exit 2; }
MODKIT="${LINTUX_MODKIT:-../lintux-modkit}"
[ -f "$MODKIT/sysext/build-sysext.sh" ] || { echo "lintux-modkit not found at $MODKIT (set LINTUX_MODKIT)" >&2; exit 1; }
NAME=notepad-files-menu
VERSION=$(cat VERSION)
ROOT=build/files-menu/root
ASSETS=$ROOT/usr/share/casaos/www/modules/icewhale_files/assets

python3 tools/files-menu/test_patch.py >/dev/null || { echo "tools/files-menu/test_patch.py is red" >&2; exit 1; }
rm -rf "$ROOT"
mkdir -p "$ASSETS" "$ROOT/usr/share/casaos/modules" "$ROOT/usr/share/doc/$NAME"
for f in "$@"; do
  python3 tools/files-menu/patch.py "$f" "$ASSETS"
done
# zpkg counts a module only with modules/<name>.json; without "ui" there is no tile (like IceWhale's doconverter).
printf '{\n  "name": "%s",\n  "version": "v%s"\n}\n' "$NAME" "$VERSION" > "$ROOT/usr/share/casaos/modules/$NAME.json"
{
  echo "Adds \"Open with Notepad\" and \"New text file\" to the ZimaOS Files menus by overlaying patched copies of its bundles:"
  for f in "$@"; do echo "  $(basename "$f")  original sha256 $(sha256sum < "$f" | cut -d' ' -f1)"; done
  echo "Built by tools/files-menu/build.sh of zima-notepad v$VERSION. Remove: zpkg remove $NAME"
} > "$ROOT/usr/share/doc/$NAME/README"
sh "$MODKIT/sysext/build-sysext.sh" "$NAME" "$ROOT" "$ARCH" build/files-menu/out "$VERSION"
cat "build/files-menu/out/$NAME-$ARCH.raw.sha256"
