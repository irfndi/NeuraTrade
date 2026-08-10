#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GEN_PROTO="${SCRIPT_DIR}/gen-proto.sh"

require_binary() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required binary: $1" >&2
    exit 1
  }
}

require_binary grep
require_binary mkdir
require_binary sed

tmp_dir="$(mktemp -d /tmp/neuratrade-gen-proto-test.XXXXXX)"
trap 'rm -rf "$tmp_dir"' EXIT

failures=0

# --- Failure path: plugin missing from PATH (and not in fallback locations) ---
# gen-proto.sh falls back to /usr/local/bin and /usr/bin; skip this branch if
# the plugin happens to live there so the test is deterministic.
if [ ! -x /usr/local/bin/protoc-gen-ts_proto ] && [ ! -x /usr/bin/protoc-gen-ts_proto ]; then
  empty_bin="${tmp_dir}/empty-bin"
  mkdir -p "$empty_bin"
  output="$(cd "$tmp_dir" && PATH="$empty_bin:/usr/bin:/bin" "$GEN_PROTO" 2>&1)" && status=0 || status=$?
  if [ "$status" -ne 1 ]; then
    echo "FAIL: gen-proto.sh without plugin should exit 1 (got $status)" >&2
    failures=$((failures + 1))
  fi
  if ! echo "$output" | grep -q "protoc-gen-ts_proto not found in PATH"; then
    echo "FAIL: expected 'protoc-gen-ts_proto not found in PATH' error, got: $output" >&2
    failures=$((failures + 1))
  fi
fi

# --- Happy path: fake protoc + plugin, assert generated output dirs exist ---
work_dir="${tmp_dir}/work"
fake_bin="${tmp_dir}/fake-bin"
mkdir -p "$work_dir" "$fake_bin"

cat >"${fake_bin}/protoc" <<'SH'
#!/bin/sh
exit 0
SH
cat >"${fake_bin}/protoc-gen-ts_proto" <<'SH'
#!/bin/sh
exit 0
SH
chmod +x "${fake_bin}/protoc" "${fake_bin}/protoc-gen-ts_proto"

if ! (cd "$work_dir" && PATH="$fake_bin:$PATH" "$GEN_PROTO" >/dev/null 2>&1); then
  echo "FAIL: gen-proto.sh happy path exited non-zero" >&2
  failures=$((failures + 1))
fi

for dir in \
  "services/backend-api/pkg/pb/ccxt" \
  "services/backend-api/pkg/pb/telegram" \
  "services/telegram-service/proto" \
  "services/ccxt-service/proto"; do
  if [ ! -d "${work_dir}/${dir}" ]; then
    echo "FAIL: expected output directory missing: ${dir}" >&2
    failures=$((failures + 1))
  fi
done

# The bug under test: mkdir previously created <svc>/src/proto which the
# protoc --ts_proto_out=...<svc>/proto invocations never write to.
if [ -d "${work_dir}/services/telegram-service/src/proto" ] || [ -d "${work_dir}/services/ccxt-service/src/proto" ]; then
  echo "FAIL: stale src/proto directory was created (mkdir/protoc paths misaligned)" >&2
  failures=$((failures + 1))
fi

if [ "$failures" -gt 0 ]; then
  echo "gen-proto test: $failures failure(s)" >&2
  exit 1
fi

echo "gen-proto test: OK"
