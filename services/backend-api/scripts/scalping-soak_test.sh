#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SOAK_SCRIPT="${SCRIPT_DIR}/scalping-soak.sh"

require_binary() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required binary: $1" >&2
    exit 1
  }
}

require_binary jq

tmp_dir="$(mktemp -d /tmp/neuratrade-scalping-soak-test.XXXXXX)"
trap 'rm -rf "$tmp_dir"' EXIT

fake_bin="${tmp_dir}/fake-neuratrade-scalping-soak"
artifact_path="${tmp_dir}/evidence/failed-gate.json"
db_path="${tmp_dir}/evidence/failed-gate.db"
output_path="${tmp_dir}/failed-gate.out"
log_dir="${tmp_dir}/logs"

cat >"$fake_bin" <<'SH'
#!/usr/bin/env bash
set -euo pipefail

db_path=""
output_path=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --db)
      db_path="$2"
      shift 2
      ;;
    --output)
      output_path="$2"
      shift 2
      ;;
    --min-baseline-win-rate-delta | --min-baseline-net-pnl-delta | --min-baseline-avg-pnl-delta)
      if [ "${EXPECT_NO_BASELINE_ARGS:-false}" = "true" ]; then
        echo "unexpected baseline gate arg: $1" >&2
        exit 3
      fi
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done

[ -n "$db_path" ] || {
  echo "missing --db" >&2
  exit 2
}
mkdir -p "$(dirname "$db_path")"
: >"$db_path"

payload="$(jq -n --arg db_path "$db_path" '{
  db_path: $db_path,
  result: {
    report: {
      total_cycles: 192,
      action_split: {buy: "0.2395833333333333", hold: "0.7604166666666667"},
      regime_split: {neutral: "0.3333333333333333", trend: "0.3697916666666667", chop: "0.296875"},
      rejection_by_reason: {no_directional_edge: 146},
      gate_block_by_code: {no_directional_edge: 146},
      signal_quality: {coverage: "1", missing_signal_quality_cycles: 0},
      trade_summary: {
        closed_trades: 46,
        wins: 46,
        losses: 0,
        win_rate: "1",
        gross_pnl: "0.6171210170245558",
        net_pnl: "0.550870903911027",
        fees: "0.0662501131135288",
        avg_net_pnl_per_trade: "0.0119754544328484",
        max_drawdown_pct: "0"
      },
      ai_provider_degradation: {degraded_cycles: 0},
      baseline_comparison: {delta_win_rate: "0.877", delta_net_pnl: "0.730870903911027", delta_avg_pnl_per_trade: "0.0149754544328484"},
      insufficient_trade_proof: false
    }
  }
}')"

printf '%s\n' "$payload"
if [ -n "$output_path" ]; then
  mkdir -p "$(dirname "$output_path")"
  printf '%s\n' "$payload" >"$output_path"
fi

echo 'scalping-soak: paper realism gate failed: closed_trades=46 wins=46 losses=0 max_drawdown_pct=0 exceeds max_perfect_win_trades=20; perfect paper wins without drawdown are insufficient proof' >&2
exit "${FAKE_EXIT_STATUS:-1}"
SH

chmod +x "$fake_bin"

if SOAK_BIN="$fake_bin" \
  SOAK_OUTPUT_FILE="$artifact_path" \
  SOAK_DB_PATH="$db_path" \
  NEURATRADE_HOME="$tmp_dir/home" \
  LOG_DIR="$log_dir" \
  CYCLES=30 \
  INTERVAL_MS=1000 \
  bash "$SOAK_SCRIPT" run >"$output_path" 2>&1; then
  echo "expected failing soak binary to make scalping-soak.sh fail" >&2
  exit 1
fi

[ -f "$artifact_path" ] || {
  echo "expected failed-gate artifact to be retained: $artifact_path" >&2
  exit 1
}

jq -e \
  --arg db_path "$db_path" \
  '.db_path == $db_path
    and .result.report.action_split.hold == "0.7604166666666667"
    and .result.report.trade_summary.closed_trades == 46
    and .result.report.gate_block_by_code.no_directional_edge == 146' \
  "$artifact_path" >/dev/null

grep -q "retained clean soak result artifact" "$output_path"

telemetry_artifact_path="${tmp_dir}/evidence/telemetry.json"
telemetry_db_path="${tmp_dir}/evidence/telemetry.db"
telemetry_output_path="${tmp_dir}/telemetry.out"

EXPECT_NO_BASELINE_ARGS=true \
  FAKE_EXIT_STATUS=0 \
  SOAK_BIN="$fake_bin" \
  SOAK_OUTPUT_FILE="$telemetry_artifact_path" \
  SOAK_DB_PATH="$telemetry_db_path" \
  NEURATRADE_HOME="$tmp_dir/home" \
  LOG_DIR="$log_dir" \
  REQUIRE_TRADES=false \
  CYCLES=3 \
  INTERVAL_MS=1000 \
  bash "$SOAK_SCRIPT" run >"$telemetry_output_path" 2>&1

[ -f "$telemetry_artifact_path" ] || {
  echo "expected telemetry artifact to be written: $telemetry_artifact_path" >&2
  exit 1
}

echo "scalping-soak tests passed"
