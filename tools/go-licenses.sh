#!/bin/sh
# Writes the licences of every Go module linked into notepad-web (except this
# one) to stdout: path, version, full licence text. Fails when one has none.
# Usage: sh tools/go-licenses.sh >> THIRD-PARTY.txt
set -eu
cd "$(dirname "$0")/.."
mods=$(GOOS=linux CGO_ENABLED=0 go list -deps -f '{{with .Module}}{{if not .Main}}{{.Path}}|{{.Version}}|{{.Dir}}{{end}}{{end}}' ./cmd/notepad-web | sort -u)
[ -n "$mods" ] || { echo "go-licenses: no modules listed" >&2; exit 1; }
printf '\nThird-party Go modules linked into /usr/bin/notepad-web\n\n'
n=0
for m in $mods; do
  p=${m%%|*}; rest=${m#*|}; v=${rest%%|*}; d=${rest#*|}
  lf=$(ls "$d" | /usr/bin/grep -iE '^(licen[cs]e|copying)' | head -n 1 || true)
  [ -n "$lf" ] || { echo "go-licenses: $p has no licence file in $d" >&2; exit 1; }
  printf '%s\n%s %s\n%s\n' "========================================================================" "$p" "${v:-(local checkout)}" "------------------------------------------------------------------------"
  cat "$d/$lf"
  printf '\n'
  n=$((n + 1))
done
echo "$n Go modules" >&2
