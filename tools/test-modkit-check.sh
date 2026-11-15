#!/bin/sh
# Tests tools/modkit-check.sh against a throwaway git repository: the pinned commit passes;
# another commit, local changes and a directory without git are refused; MODKIT_DEV=1 lets a
# development build through with a warning.
#   sh tools/test-modkit-check.sh
set -eu
cd "$(dirname "$0")/.."
T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT
G="git -C $T/m -c user.name=t -c user.email=t@example.invalid"
git init -q "$T/m"
$G commit -q --allow-empty -m one
ONE=$($G rev-parse HEAD)
$G commit -q --allow-empty -m two
TWO=$($G rev-parse HEAD)
fails=0
check() { # name, expected exit (0|1), ref line, [VAR=value]
  name=$1 want=$2
  echo "$3" > "$T/ref"
  shift 3
  set +e
  out=$(env "$@" MODKIT_REF_FILE="$T/ref" sh tools/modkit-check.sh "$T/m" 2>&1)
  got=$?
  set -e
  if [ "$got" = "$want" ]; then echo "ok   $name: $out"; else echo "FAIL $name: exit $got, want $want: $out"; fails=$((fails + 1)); fi
}
check "pinned commit passes" 0 "v9 $TWO"
check "another commit is refused" 1 "v9 $ONE"
echo x > "$T/m/dirty"
check "local changes are refused" 1 "v9 $TWO"
check "MODKIT_DEV=1 lets a dev build through" 0 "v9 $ONE" MODKIT_DEV=1
rm -rf "$T/m/.git"
check "a directory without git is refused" 1 "v9 $TWO"
echo "modkit-check: $((5 - fails)) ok, $fails FAIL"
[ "$fails" = 0 ]
