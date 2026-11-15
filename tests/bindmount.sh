#!/bin/sh
# A container that mounts one single file (docker run -v /DATA/AppData/x/config.yaml:/etc/x.yaml)
# is bound to that file's inode. Saving in the editor must reach the container: the file is rewritten
# in place, not replaced by rename (Rückmeldung 14.11.2026).
#   sh tests/bindmount.sh       needs docker and build/notepad-dev (sh check.sh builds it)
set -eu
cd "$(dirname "$0")/.."
IMAGE="${BINDMOUNT_IMAGE:-alpine:3.24}"
# Build the dev server from the working tree: a stale build/notepad-dev tested the old save (14.11.2026).
go build -o build/notepad-dev ./cmd/notepad-dev
T=$(mktemp -d)
NAME="notepad-bindmount-$$"
PORT=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1])')
mkdir -p "$T/DATA/AppData/demo"
printf 'level: info\n' > "$T/DATA/AppData/demo/config.yaml"
build/notepad-dev -root "$T/DATA" -web build/dev-web -addr "127.0.0.1:$PORT" >"$T/dev.log" 2>&1 &
DEV=$!
cleanup() {
  docker rm -f "$NAME" >/dev/null 2>&1 || true
  [ "$(ps -p "$DEV" -o comm= 2>/dev/null)" = notepad-dev ] && kill "$DEV"
  rm -rf "${T:?}"
}
trap cleanup EXIT
docker run -d --name "$NAME" -v "$T/DATA/AppData/demo/config.yaml:/etc/demo.yaml:ro" "$IMAGE" sleep 120 >/dev/null
for _ in $(seq 1 50); do curl -s -o /dev/null "http://127.0.0.1:$PORT/notepad/health" && break; sleep 0.1; done
P="$T/DATA/AppData/demo/config.yaml"
INODE_BEFORE=$(stat -c %i "$P")
SHA=$(curl -s "http://127.0.0.1:$PORT/notepad/api/read?path=$(python3 -c 'import sys,urllib.parse;print(urllib.parse.quote(sys.argv[1]))' "$P")" | python3 -c 'import json,sys; print(json.load(sys.stdin)["sha"])')
Q=$(python3 -c 'import sys,urllib.parse;print(urllib.parse.urlencode({"path":sys.argv[1],"sha":sys.argv[2],"encoding":"utf-8","eol":"lf"}))' "$P" "$SHA")
curl -s -o /dev/null -w 'write %{http_code}\n' -X POST --data-binary 'level: debug
' -H 'Content-Type: text/plain' "http://127.0.0.1:$PORT/notepad/api/write?$Q"
INODE_AFTER=$(stat -c %i "$P")
SEEN=$(docker exec "$NAME" cat /etc/demo.yaml)
echo "host: $(cat "$P" | tr -d '\n') (inode $INODE_BEFORE -> $INODE_AFTER) | container: $SEEN"
if [ "$SEEN" = "level: debug" ] && [ "$INODE_BEFORE" = "$INODE_AFTER" ]; then
  echo "bindmount: ok"
else
  echo "bindmount: FAIL — the container still sees the old file" >&2
  exit 1
fi
