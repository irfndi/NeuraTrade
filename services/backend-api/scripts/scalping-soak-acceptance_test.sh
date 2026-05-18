#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ACCEPTANCE_SCRIPT="${SCRIPT_DIR}/scalping-soak-acceptance.sh"

require_binary() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required binary: $1" >&2
    exit 1
  }
}

require_binary jq

tmp_dir="$(mktemp -d /tmp/neuratrade-scalping-acceptance-test.XXXXXX)"
trap 'rm -rf "$tmp_dir"' EXIT

fake_soak="${tmp_dir}/fake-scalping-soak.sh"
fake_verifier="${tmp_dir}/fake-verifier.sh"
fake_gateway="${tmp_dir}/fake-neuratrade"
fake_bin_dir="${tmp_dir}/bin"
curl_hits="${tmp_dir}/curl-hits.txt"
artifact_path="${tmp_dir}/evidence/scalping-soak-acceptance-fixed.json"
db_path="${tmp_dir}/evidence/scalping-soak-acceptance-fixed.db"
manifest_path="${tmp_dir}/evidence/scalping-soak-acceptance-fixed.acceptance.json"
log_path="${tmp_dir}/logs/acceptance.log"
default_artifact_path="${tmp_dir}/default-evidence/scalping-soak-acceptance-default.json"
default_db_path="${tmp_dir}/default-evidence/scalping-soak-acceptance-default.db"
default_manifest_path="${tmp_dir}/default-evidence/scalping-soak-acceptance-default.acceptance.json"
default_log_path="${tmp_dir}/default-logs/acceptance.log"
empty_artifact_path="${tmp_dir}/empty-gate-evidence/scalping-soak-acceptance-empty-gate.json"
empty_manifest_path="${tmp_dir}/empty-gate-evidence/scalping-soak-acceptance-empty-gate.acceptance.json"
empty_log_path="${tmp_dir}/empty-gate-logs/acceptance.log"
invalid_output="${tmp_dir}/invalid.out"

cat >"$fake_soak" <<'SH'
#!/usr/bin/env bash
set -euo pipefail

[ "${1:-}" = "run" ] || {
  echo "expected run command" >&2
  exit 1
}
[ "$CYCLES" = "30" ] || {
  echo "unexpected CYCLES=$CYCLES" >&2
  exit 1
}
[ "$CAPITAL" = "48" ] || {
  echo "unexpected CAPITAL=$CAPITAL" >&2
  exit 1
}
expected_max_hold_ratio="${EXPECTED_MAX_HOLD_RATIO-0.745}"
[ "$MAX_HOLD_RATIO" = "$expected_max_hold_ratio" ] || {
  echo "unexpected MAX_HOLD_RATIO=$MAX_HOLD_RATIO expected=$expected_max_hold_ratio" >&2
  exit 1
}

mkdir -p "$(dirname "$SOAK_OUTPUT_FILE")" "$(dirname "$SOAK_DB_PATH")"
: >"$SOAK_DB_PATH"
jq -n --arg db_path "$SOAK_DB_PATH" '{
  db_path: $db_path,
  result: {
    report: {
      total_cycles: 2,
      action_split: {buy: "0.5", hold: "0.5"},
      regime_split: {neutral: "1"},
      rejection_by_reason: {no_directional_edge: 1},
      gate_block_by_code: {},
      signal_quality: {
        coverage: "1",
        missing_signal_quality_cycles: 0,
        avg_bid_ask_spread_pct: "0.01",
        avg_abs_order_book_imbalance: "0.2",
        avg_range_position_24h: "50",
        avg_price_change_24h_pct: "0.1"
      },
      trade_summary: {
        closed_trades: 1,
        wins: 1,
        losses: 0,
        win_rate: "1",
        net_pnl: "0.1",
        fees: "0.01",
        avg_net_pnl_per_trade: "0.1",
        max_drawdown_pct: "0"
      },
      ai_provider_degradation: {degraded_cycles: 0},
      baseline_comparison: {
        delta_win_rate: "0.877",
        delta_net_pnl: "0.28",
        delta_avg_pnl_per_trade: "0.103"
      },
      insufficient_trade_proof: false
    }
  }
}' >"$SOAK_OUTPUT_FILE"
echo "fake soak complete"
SH

cat >"$fake_verifier" <<'SH'
#!/usr/bin/env bash
set -euo pipefail

[ "$1" = "$SOAK_OUTPUT_FILE" ] || {
  echo "artifact argument mismatch: $1 != $SOAK_OUTPUT_FILE" >&2
  exit 1
}
[ "$2" = "$SOAK_DB_PATH" ] || {
  echo "db argument mismatch: $2 != $SOAK_DB_PATH" >&2
  exit 1
}
echo "fake verifier complete"
SH

cat >"$fake_gateway" <<'SH'
#!/usr/bin/env bash
set -euo pipefail

[ "${1:-}" = "gateway" ] || {
  echo "expected gateway command" >&2
  exit 1
}
[ "${2:-}" = "status" ] || {
  echo "expected gateway status command" >&2
  exit 1
}
echo "Gateway Status: RUNNING"
SH

mkdir -p "$fake_bin_dir"
cat >"${fake_bin_dir}/curl" <<'SH'
#!/usr/bin/env bash
set -euo pipefail

last_arg=""
for arg in "$@"; do
  last_arg="$arg"
done
printf '%s\n' "$last_arg" >>"$CURL_HITS_FILE"

case "$last_arg" in
  */health | */ready)
    exit 0
    ;;
  *)
    echo "unexpected curl URL: $last_arg" >&2
    exit 1
    ;;
esac
SH

chmod +x "$fake_soak" "$fake_verifier" "$fake_gateway" "${fake_bin_dir}/curl"

RUN_HEALTH_PREFLIGHT=false \
  CHECK_GATEWAY_STATUS=false \
  SCALPING_SOAK_SCRIPT="$fake_soak" \
  SCALPING_SOAK_VERIFIER="$fake_verifier" \
  DATA_DIR="${tmp_dir}/evidence" \
  LOG_DIR="${tmp_dir}/logs" \
  STAMP=fixed \
  LOG_FILE="$log_path" \
  bash "$ACCEPTANCE_SCRIPT" run

[ -f "$artifact_path" ] || {
  echo "expected artifact was not created: $artifact_path" >&2
  exit 1
}
[ -f "$db_path" ] || {
  echo "expected DB was not created: $db_path" >&2
  exit 1
}
[ -f "$manifest_path" ] || {
  echo "expected manifest was not created: $manifest_path" >&2
  exit 1
}

jq -e \
  --arg artifact "$artifact_path" \
  --arg db_path "$db_path" \
  --arg log_file "$log_path" \
  '.runtime.health_preflight == "false"
    and .runtime.gateway_status_check == "false"
    and .runtime.backend_url == "http://127.0.0.1:8080"
    and .evidence.artifact == $artifact
    and .evidence.db_path == $db_path
    and .evidence.log_file == $log_file
    and .gates.max_hold_ratio == "0.745"
    and .report.rejection_by_reason.no_directional_edge == 1
    and .report.trade_summary.closed_trades == 1
    and .report.insufficient_trade_proof == false' \
  "$manifest_path" >/dev/null

env -u RUN_HEALTH_PREFLIGHT -u CHECK_GATEWAY_STATUS -u BACKEND_URL \
  PATH="${fake_bin_dir}:$PATH" \
  CURL_HITS_FILE="$curl_hits" \
  GATEWAY_BIN="$fake_gateway" \
  SCALPING_SOAK_SCRIPT="$fake_soak" \
  SCALPING_SOAK_VERIFIER="$fake_verifier" \
  DATA_DIR="${tmp_dir}/default-evidence" \
  LOG_DIR="${tmp_dir}/default-logs" \
  STAMP=default \
  LOG_FILE="$default_log_path" \
  bash "$ACCEPTANCE_SCRIPT" run

[ -f "$default_artifact_path" ] || {
  echo "expected default artifact was not created: $default_artifact_path" >&2
  exit 1
}
[ -f "$default_db_path" ] || {
  echo "expected default DB was not created: $default_db_path" >&2
  exit 1
}
[ -f "$default_manifest_path" ] || {
  echo "expected default manifest was not created: $default_manifest_path" >&2
  exit 1
}

jq -e \
  --arg artifact "$default_artifact_path" \
  --arg db_path "$default_db_path" \
  --arg log_file "$default_log_path" \
  '.runtime.health_preflight == "true"
    and .runtime.gateway_status_check == "true"
    and .runtime.backend_url == "http://127.0.0.1:8080"
    and .evidence.artifact == $artifact
    and .evidence.db_path == $db_path
    and .evidence.log_file == $log_file
    and .gates.max_hold_ratio == "0.745"
    and .report.rejection_by_reason.no_directional_edge == 1
    and .report.trade_summary.closed_trades == 1
    and .report.insufficient_trade_proof == false' \
  "$default_manifest_path" >/dev/null

grep -q 'http://127.0.0.1:8080/health' "$curl_hits"
grep -q 'http://127.0.0.1:8080/ready' "$curl_hits"

RUN_HEALTH_PREFLIGHT=false \
  CHECK_GATEWAY_STATUS=false \
  MAX_HOLD_RATIO= \
  EXPECTED_MAX_HOLD_RATIO= \
  SCALPING_SOAK_SCRIPT="$fake_soak" \
  SCALPING_SOAK_VERIFIER="$fake_verifier" \
  DATA_DIR="${tmp_dir}/empty-gate-evidence" \
  LOG_DIR="${tmp_dir}/empty-gate-logs" \
  STAMP=empty-gate \
  LOG_FILE="$empty_log_path" \
  bash "$ACCEPTANCE_SCRIPT" run

[ -f "$empty_artifact_path" ] || {
  echo "expected empty-gate artifact was not created: $empty_artifact_path" >&2
  exit 1
}
[ -f "$empty_manifest_path" ] || {
  echo "expected empty-gate manifest was not created: $empty_manifest_path" >&2
  exit 1
}

jq -e '.gates.max_hold_ratio == ""' "$empty_manifest_path" >/dev/null

if RUN_HEALTH_PREFLIGHT=treu \
  CHECK_GATEWAY_STATUS=false \
  SCALPING_SOAK_SCRIPT="$fake_soak" \
  SCALPING_SOAK_VERIFIER="$fake_verifier" \
  DATA_DIR="${tmp_dir}/invalid-evidence" \
  LOG_DIR="${tmp_dir}/invalid-logs" \
  STAMP=invalid \
  bash "$ACCEPTANCE_SCRIPT" run >"$invalid_output" 2>&1; then
  echo "expected invalid RUN_HEALTH_PREFLIGHT value to fail" >&2
  exit 1
fi

grep -q 'RUN_HEALTH_PREFLIGHT must be true or false, got treu' "$invalid_output"

echo "scalping-soak-acceptance tests passed"
