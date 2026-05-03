#!/usr/bin/env bash

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

BIN_DIR="$ROOT/bin"
SERVER_BIN="$BIN_DIR/server"
CHATD_BIN="$BIN_DIR/chatd"

WORK_DIR="$(mktemp -d -t chatd-smoke.XXXXXX)"
SERVER_LOG="$WORK_DIR/server.log"
WATCH1_OUT="$WORK_DIR/watch1.out"
WATCH2_OUT="$WORK_DIR/watch2.out"
WATCH1_ERR="$WORK_DIR/watch1.err"
WATCH2_ERR="$WORK_DIR/watch2.err"

SERVER_PID=""
WATCH1_PID=""
WATCH2_PID=""

cleanup() {
  local rc=$?
  set +e
  # Bounded TERM-then-KILL: a wedged child that ignores SIGTERM (deadlock,
  # blocked syscall, masked signal) would otherwise let `wait` block until
  # the workflow-level timeout. Pure bash so we don't depend on coreutils
  # `timeout` (BSD `wait` lacks `-t`; macOS `coreutils` is not standard).
  for pid in "$WATCH1_PID" "$WATCH2_PID" "$SERVER_PID"; do
    [[ -z "$pid" ]] && continue
    kill -0 "$pid" 2>/dev/null || continue
    kill "$pid" 2>/dev/null
    for _ in $(seq 1 50); do
      kill -0 "$pid" 2>/dev/null || break
      sleep 0.1
    done
    if kill -0 "$pid" 2>/dev/null; then
      kill -KILL "$pid" 2>/dev/null
    fi
    wait "$pid" 2>/dev/null
  done
  if [[ $rc -ne 0 ]]; then
    echo "--- server.log ---" >&2
    [[ -f "$SERVER_LOG" ]] && cat "$SERVER_LOG" >&2
    echo "--- watch1.out ---" >&2
    [[ -f "$WATCH1_OUT" ]] && cat "$WATCH1_OUT" >&2
    echo "--- watch1.err ---" >&2
    [[ -f "$WATCH1_ERR" ]] && cat "$WATCH1_ERR" >&2
    echo "--- watch2.out ---" >&2
    [[ -f "$WATCH2_OUT" ]] && cat "$WATCH2_OUT" >&2
    echo "--- watch2.err ---" >&2
    [[ -f "$WATCH2_ERR" ]] && cat "$WATCH2_ERR" >&2
  fi
  rm -rf "$WORK_DIR"
  exit $rc
}
trap cleanup EXIT INT TERM HUP

# Ask the OS for a free TCP port. Falling back to a fixed port would risk
# collisions in CI runners with concurrent jobs.
pick_free_port() {
  if ! command -v python3 >/dev/null 2>&1; then
    echo "[smoke] python3 is required to pick a free port (or set CHAT_SERVER_PORT)" >&2
    exit 1
  fi
  python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
}

mkdir -p "$BIN_DIR"
echo "[smoke] building server + chatd..."
go build -o "$SERVER_BIN" ./apps/server
go build -o "$CHATD_BIN" ./apps/cli

PORT="${CHAT_SERVER_PORT:-$(pick_free_port)}"
WS_URL="ws://127.0.0.1:${PORT}/ws"
export CHAT_SERVER="$WS_URL"

echo "[smoke] starting server on :${PORT}"
CHAT_SERVER_PORT="$PORT" "$SERVER_BIN" >"$SERVER_LOG" 2>&1 &
SERVER_PID=$!

# Wait for the listening port (up to ~5s).
for _ in $(seq 1 50); do
  if (echo >/dev/tcp/127.0.0.1/"$PORT") 2>/dev/null; then
    break
  fi
  sleep 0.1
done
if ! (echo >/dev/tcp/127.0.0.1/"$PORT") 2>/dev/null; then
  echo "[smoke] server did not open port ${PORT} within 5s" >&2
  exit 1
fi

echo "[smoke] starting two watchers"
"$CHATD_BIN" watch >"$WATCH1_OUT" 2>"$WATCH1_ERR" &
WATCH1_PID=$!
"$CHATD_BIN" watch >"$WATCH2_OUT" 2>"$WATCH2_ERR" &
WATCH2_PID=$!

# Wait for both watchers to register with the hub. Polling /debug/subs avoids
# the race where a slow CI runner's WebSocket dial takes longer than a fixed
# sleep and the publish below misses one or both subscribers. The %23 is a
# URL-encoded '#' so curl doesn't truncate the query at a fragment.
EXPECTED_SUBS=2
SUBS_URL="http://127.0.0.1:${PORT}/debug/subs?channel=%23general"
subs_ready=0
for _ in $(seq 1 50); do
  count=$(curl -fsS "$SUBS_URL" 2>/dev/null || echo "")
  if [[ "$count" == "$EXPECTED_SUBS" ]]; then
    subs_ready=1
    break
  fi
  sleep 0.1
done
if [[ $subs_ready -ne 1 ]]; then
  echo "[smoke] expected ${EXPECTED_SUBS} subscribers within 5s (last count: ${count:-<none>})" >&2
  exit 1
fi

MSG="smoke-$$-$(date +%s%N)"
echo "[smoke] sending message: ${MSG}"
"$CHATD_BIN" send "$MSG"

# Poll up to ~5s for both files to contain the message.
deadline=$(( $(date +%s) + 5 ))
got1=0
got2=0
while [[ $(date +%s) -lt $deadline ]]; do
  [[ -f "$WATCH1_OUT" ]] && grep -Fq -- "$MSG" "$WATCH1_OUT" && got1=1
  [[ -f "$WATCH2_OUT" ]] && grep -Fq -- "$MSG" "$WATCH2_OUT" && got2=1
  if [[ $got1 -eq 1 && $got2 -eq 1 ]]; then
    break
  fi
  sleep 0.1
done

if [[ $got1 -ne 1 || $got2 -ne 1 ]]; then
  echo "[smoke] FAIL: watcher(s) did not receive ${MSG}" >&2
  echo "  watcher1 received: $got1" >&2
  echo "  watcher2 received: $got2" >&2
  exit 1
fi

echo "[smoke] OK: both watchers received ${MSG}"
