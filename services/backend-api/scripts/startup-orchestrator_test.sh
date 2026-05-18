#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ORCHESTRATOR="${SCRIPT_DIR}/startup-orchestrator.sh"

require_binary() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required binary: $1" >&2
    exit 1
  }
}

require_binary lsof
require_binary python3

tmp_dir="$(mktemp -d /tmp/neuratrade-startup-orchestrator-test.XXXXXX)"
fake_server="${tmp_dir}/neuratrade-server"
ready_file="${tmp_dir}/ready"
output_file="${tmp_dir}/stop.out"
port_file="${tmp_dir}/port"
trap 'rm -rf "$tmp_dir"' EXIT

python3 - <<'PY' >"$port_file"
import socket

sock = socket.socket()
sock.bind(("127.0.0.1", 0))
print(sock.getsockname()[1])
sock.close()
PY
port="$(cat "$port_file")"

cat >"$fake_server" <<'PY'
#!/usr/bin/env python3
import os
import signal
import socket
import sys
import time

port = int(os.environ["FAKE_BACKEND_PORT"])
ready_file = os.environ["FAKE_READY_FILE"]
sock = socket.socket()
sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
sock.bind(("127.0.0.1", port))
sock.listen(1)

with open(ready_file, "w", encoding="utf-8") as handle:
    handle.write(str(os.getpid()))

def stop(_signum, _frame):
    sock.close()
    sys.exit(0)

signal.signal(signal.SIGTERM, stop)
signal.signal(signal.SIGINT, stop)

while True:
    time.sleep(0.2)
PY
chmod +x "$fake_server"

FAKE_BACKEND_PORT="$port" FAKE_READY_FILE="$ready_file" "$fake_server" &
fake_pid="$!"

for _ in $(seq 1 50); do
  [ -f "$ready_file" ] && break
  sleep 0.1
done
[ -f "$ready_file" ] || {
  echo "fake backend did not become ready" >&2
  kill "$fake_pid" 2>/dev/null || true
  exit 1
}

lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null || {
  echo "fake backend is not listening on $port" >&2
  kill "$fake_pid" 2>/dev/null || true
  exit 1
}

NEURATRADE_HOME="$tmp_dir/home" \
  PORT="$port" \
  BACKEND_HOST_PORT="$port" \
  bash "$ORCHESTRATOR" stop >"$output_file" 2>&1

for _ in $(seq 1 50); do
  if ! kill -0 "$fake_pid" 2>/dev/null; then
    break
  fi
  sleep 0.1
done

if kill -0 "$fake_pid" 2>/dev/null; then
  echo "startup-orchestrator stop left fake backend running" >&2
  cat "$output_file" >&2
  kill "$fake_pid" 2>/dev/null || true
  exit 1
fi

if lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then
  echo "backend port still has a listener after stop" >&2
  cat "$output_file" >&2
  exit 1
fi

grep -q "Backend pid file missing or stale" "$output_file"

echo "startup-orchestrator tests passed"
