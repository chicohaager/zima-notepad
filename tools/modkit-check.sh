#!/bin/sh
# A release is built against the lintux-modkit commit CI checks out (lintux-modkit.ref:
# "<tag> <commit>"): build.sh refuses a checkout on another commit or with local changes,
# so a local build cannot carry other modkit code than the tested one (as zima-modmanager).
#   sh tools/modkit-check.sh <modkit dir>     MODKIT_DEV=1 allows it, with a warning (not for a release)
set -eu
M="$1"
REF="${MODKIT_REF_FILE:-$(dirname "$0")/../lintux-modkit.ref}"
read -r TAG COMMIT < "$REF"
if HEAD=$(git -C "$M" rev-parse HEAD 2>/dev/null) && DIRTY=$(git -C "$M" status --porcelain 2>/dev/null); then
  if [ "$HEAD" = "$COMMIT" ] && [ -z "$DIRTY" ]; then
    echo "lintux-modkit $TAG ($COMMIT)"
    exit 0
  fi
  CHANGES=""
  [ -z "$DIRTY" ] || CHANGES=" with local changes"
  MSG="lintux-modkit at $HEAD$CHANGES, not $TAG ($COMMIT) from lintux-modkit.ref"
else
  MSG="$M is not a git checkout of lintux-modkit, so its commit is unknown (want $TAG $COMMIT)"
fi
if [ "${MODKIT_DEV:-}" = 1 ]; then
  echo "WARNING: $MSG — development build (MODKIT_DEV=1), not for a release" >&2
  exit 0
fi
echo "refused: $MSG; check out $TAG in $M, or set MODKIT_DEV=1 for a development build" >&2
exit 1
