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
artifact_path="${tmp_dir}/evidence/scalping-soak-acceptance-fixed.json"
db_path="${tmp_dir}/evidence/scalping-soak-acceptance-fixed.db"
manifest_path="${tmp_dir}/evidence/scalping-soak-acceptance-fixed.acceptance.json"
log_path="${tmp_dir}/logs/acceptance.log"

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
[ "$MAX_HOLD_RATIO" = "0.745" ] || {
  echo "unexpected MAX_HOLD_RATIO=$MAX_HOLD_RATIO" >&2
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

chmod +x "$fake_soak" "$fake_verifier"

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
    and .evidence.artifact == $artifact
    and .evidence.db_path == $db_path
    and .evidence.log_file == $log_file
    and .gates.max_hold_ratio == "0.745"
    and .report.trade_summary.closed_trades == 1
    and .report.insufficient_trade_proof == false' \
  "$manifest_path" >/dev/null

echo "scalping-soak-acceptance tests passed"
