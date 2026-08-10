#!/bin/bash
# Minimal regression tests for scripts/neuratrade-cli/neuratrade
#
# Covers:
#   1. doctor completes on a broken install (issue counter survives set -e)
#   2. doctor flags the known-default admin_api_key
#   3. start resolves the server binary from the repo root regardless of cwd
#      and records the server PID
#   4. start refuses to run with the default admin key on 0.0.0.0
#   5. stop kills only the recorded PIDs and never invokes pkill/pgrep
#
# Run: bash scripts/test-neuratrade-cli.sh
# NOTE: deliberately NOT `set -e` - failures are counted and reported.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLI="${SCRIPT_DIR}/neuratrade-cli/neuratrade"
TEST_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/neuratrade-cli-test.XXXXXX")"
TEST_ROOT="$(cd "$TEST_ROOT" && pwd)" # normalize (TMPDIR ends with /)
trap 'rm -rf "$TEST_ROOT"' EXIT

PASS=0
FAIL=0

pass() { echo "ok - $1"; PASS=$((PASS + 1)); }
fail() { echo "FAIL - $1"; FAIL=$((FAIL + 1)); }

# PATH containing only our shims plus the real tools
FAKE_BIN="${TEST_ROOT}/bin"
mkdir -p "$FAKE_BIN"

write_shim() { # name script
    cat > "${FAKE_BIN}/$1" <<EOF
#!/bin/bash
$2
EOF
    chmod +x "${FAKE_BIN}/$1"
}

# pkill/pgrep shims log any invocation - the fixed CLI must never call them
write_shim pkill 'echo "pkill called: $*" >> "${FAKE_BIN}/calls.log"'
write_shim pgrep 'echo "pgrep called: $*" >> "${FAKE_BIN}/calls.log"'
# Mock curl: pretend the server is healthy
write_shim curl 'exit 0'

# --- fake repo: a real-looking services/backend-api/bin/server ---
make_fake_repo() { # repo_dir
    local repo="$1"
    mkdir -p "${repo}/services/backend-api/bin" \
             "${repo}/services/ccxt" \
             "${repo}/services/telegram-service"
    cat > "${repo}/services/backend-api/bin/server" <<'EOF'
#!/bin/bash
# Fake server: record how it was invoked, then stay alive until killed
echo "$0 $PWD" > "${FAKE_SERVER_INVOCATION}"
exec sleep 300
EOF
    chmod +x "${repo}/services/backend-api/bin/server"
}

# --- a NEURATRADE_DIR config that passes the security gate ---
make_neuratrade_home() { # home_dir
    local home="$1"
    mkdir -p "$home" "$home/data"
    cat > "$home/config.json" <<EOF
{
  "database": {
    "sqlite_path": "$home/data/neuratrade.db"
  },
  "server": {
    "host": "127.0.0.1",
    "port": 8080,
    "environment": "development"
  },
  "security": {
    "admin_api_key": "test-key-not-default"
  }
}
EOF
}

echo "== doctor: broken install =="
empty="${TEST_ROOT}/empty-home"
out="$(NEURATRADE_DIR="$empty" bash "$CLI" doctor 2>&1 || true)"
if echo "$out" | grep -q "Found 2 issue(s)"; then
    pass "doctor completes and reports 2 issues (no set -e abort)"
else
    fail "doctor on broken install; got: $out"
fi

echo "== doctor: default admin key flagged =="
home2="${TEST_ROOT}/onboard-home"
mkdir -p "$home2"
cat > "$home2/config.json" <<'EOF'
{
  "security": { "admin_api_key": "change-me-in-production" }
}
EOF
out="$(NEURATRADE_DIR="$home2" bash "$CLI" doctor 2>&1 || true)"
if echo "$out" | grep -q "Found 1 issue(s)"; then
    pass "doctor flags the default admin key as 1 issue"
else
    fail "doctor default-key; got: $out"
fi

echo "== start: refuses default key on 0.0.0.0 =="
home4="${TEST_ROOT}/home4"
mkdir -p "$home4"
cat > "$home4/config.json" <<'EOF'
{
  "server": { "host": "0.0.0.0", "environment": "development" },
  "security": { "admin_api_key": "change-me-in-production" }
}
EOF
out="$(NEURATRADE_DIR="$home4" bash "$CLI" start 2>&1 || true)"
if echo "$out" | grep -q "Refusing to start"; then
    pass "start refuses to run with default key on 0.0.0.0"
else
    fail "default-key gate did not trigger; got: $out"
fi

echo "== start: resolves binary from repo root, records PID =="
repo="${TEST_ROOT}/repo"
home="${TEST_ROOT}/home"
make_fake_repo "$repo"
make_neuratrade_home "$home"
# Run a COPY of the CLI inside the fake repo so project_root resolution
# (derived from BASH_SOURCE) targets the fake tree, not the real repo.
mkdir -p "${repo}/scripts/neuratrade-cli"
cp "$CLI" "${repo}/scripts/neuratrade-cli/neuratrade"
export FAKE_SERVER_INVOCATION="${TEST_ROOT}/server-invocation.txt"
(
    cd "$TEST_ROOT" || exit 1 # deliberately NOT the repo
    NEURATRADE_DIR="$home" PATH="$FAKE_BIN:$PATH" \
        bash "${repo}/scripts/neuratrade-cli/neuratrade" start
) > "${TEST_ROOT}/start.out" 2>&1
inv="$(cat "$FAKE_SERVER_INVOCATION" 2>/dev/null || true)"
if [[ "$inv" == *"/services/backend-api/bin/server ${home}" ]]; then
    pass "start launched the repo-root binary with NEURATRADE_DIR as server cwd"
else
    fail "start binary/cwd mismatch; got: '$inv'; output: $(cat "${TEST_ROOT}/start.out")"
fi
pid="$(cat "${home}/run/server.pid" 2>/dev/null || true)"
if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
    pass "start recorded a live server PID ($pid)"
else
    fail "server.pid missing or dead: '$pid'"
fi
kill "$pid" 2>/dev/null || true
rm -f "${home}/run/server.pid"

echo "== stop: kills only recorded PIDs =="
home3="${TEST_ROOT}/home3"
mkdir -p "${home3}/run"
sleep 300 & p1=$!
sleep 300 & p2=$!
sleep 300 & decoy=$!                      # unrecorded: must survive
bash -c 'exec -a bun sleep 300' & bun_decoy=$!   # argv0=bun: old pkill -f bun would kill it
echo "$p1" > "${home3}/run/server.pid"
echo "$p2" > "${home3}/run/ccxt.pid"
rm -f "${FAKE_BIN}/calls.log"
out="$(NEURATRADE_DIR="$home3" PATH="$FAKE_BIN:$PATH" bash "$CLI" stop 2>&1 || true)"
sleep 0.2
if ! kill -0 "$p1" 2>/dev/null && ! kill -0 "$p2" 2>/dev/null; then
    pass "stop killed the recorded PIDs"
else
    fail "recorded PIDs still alive; out: $out"
fi
if kill -0 "$decoy" 2>/dev/null && kill -0 "$bun_decoy" 2>/dev/null; then
    pass "stop left unrecorded processes alive (incl. argv0=bun)"
else
    fail "stop killed unrecorded processes"
fi
if [[ ! -f "${FAKE_BIN}/calls.log" ]]; then
    pass "stop never invoked pkill/pgrep"
else
    fail "pkill/pgrep used: $(cat "${FAKE_BIN}/calls.log")"
fi
if [[ ! -f "${home3}/run/server.pid" ]] && [[ ! -f "${home3}/run/ccxt.pid" ]]; then
    pass "stop removed the PID files"
else
    fail "PID files remain after stop"
fi
kill "$decoy" "$bun_decoy" 2>/dev/null || true

echo ""
echo "== summary: ${PASS} passed, ${FAIL} failed =="
[[ "$FAIL" -eq 0 ]]
