#!/usr/bin/env bash

# Verify a clean scalping-soak JSON artifact and its persisted SQLite evidence
# against the broken-live baseline acceptance gates.

set -euo pipefail

MIN_TRADES="${MIN_TRADES-1}"
MIN_WIN_RATE="${MIN_WIN_RATE-0.123}"
MIN_NET_PNL="${MIN_NET_PNL-0}"
MIN_AVG_NET_PNL="${MIN_AVG_NET_PNL-0}"
MIN_SIGNAL_QUALITY_COVERAGE="${MIN_SIGNAL_QUALITY_COVERAGE-1}"
MAX_HOLD_RATIO="${MAX_HOLD_RATIO-0.745}"
MAX_DRAWDOWN_PCT="${MAX_DRAWDOWN_PCT-0.01}"
MAX_AI_PROVIDER_DEGRADED_CYCLES="${MAX_AI_PROVIDER_DEGRADED_CYCLES-0}"
MIN_BASELINE_WIN_RATE_DELTA="${MIN_BASELINE_WIN_RATE_DELTA-0}"
MIN_BASELINE_NET_PNL_DELTA="${MIN_BASELINE_NET_PNL_DELTA-0}"
MIN_BASELINE_AVG_PNL_DELTA="${MIN_BASELINE_AVG_PNL_DELTA-0}"

usage() {
  cat <<USAGE
Usage: $(basename "$0") ARTIFACT_JSON [DB_PATH]

Verifies a clean scalping-soak artifact produced by scalping-soak.sh with
SOAK_OUTPUT_FILE and, when available, verifies persisted SQLite evidence.
DB_PATH defaults to the artifact's db_path field.

Gate environment defaults:
  MIN_TRADES=${MIN_TRADES}
  MIN_WIN_RATE=${MIN_WIN_RATE}
  MIN_NET_PNL=${MIN_NET_PNL}
  MIN_AVG_NET_PNL=${MIN_AVG_NET_PNL}
  MIN_SIGNAL_QUALITY_COVERAGE=${MIN_SIGNAL_QUALITY_COVERAGE}
  MAX_HOLD_RATIO=${MAX_HOLD_RATIO}
  MAX_DRAWDOWN_PCT=${MAX_DRAWDOWN_PCT}
  MAX_AI_PROVIDER_DEGRADED_CYCLES=${MAX_AI_PROVIDER_DEGRADED_CYCLES}
  MIN_BASELINE_WIN_RATE_DELTA=${MIN_BASELINE_WIN_RATE_DELTA}
  MIN_BASELINE_NET_PNL_DELTA=${MIN_BASELINE_NET_PNL_DELTA}
  MIN_BASELINE_AVG_PNL_DELTA=${MIN_BASELINE_AVG_PNL_DELTA}
USAGE
}

fail() {
  echo "verify-scalping-soak-artifact: $*" >&2
  exit 1
}

require_binary() {
  command -v "$1" >/dev/null 2>&1 || fail "missing required binary: $1"
}

jq_number() {
  local filter="$1"
  jq -er "$filter | tonumber" "$artifact"
}

jq_string() {
  local filter="$1"
  jq -er "$filter" "$artifact"
}

decimal_lte() {
  awk -v actual="$1" -v maximum="$2" 'BEGIN { exit !(actual <= maximum) }'
}

decimal_gte() {
  awk -v actual="$1" -v minimum="$2" 'BEGIN { exit !(actual >= minimum) }'
}

validate_min_decimal() {
  local label="$1"
  local actual="$2"
  local minimum="$3"
  [ -z "$minimum" ] && return
  decimal_gte "$actual" "$minimum" || fail "${label}=${actual} below minimum=${minimum}"
}

validate_max_decimal() {
  local label="$1"
  local actual="$2"
  local maximum="$3"
  [ -z "$maximum" ] && return
  decimal_lte "$actual" "$maximum" || fail "${label}=${actual} above maximum=${maximum}"
}

validate_max_int() {
  local label="$1"
  local actual="$2"
  local maximum="$3"
  [ -z "$maximum" ] && return
  [ "$actual" -le "$maximum" ] || fail "${label}=${actual} above maximum=${maximum}"
}

validate_ratio_override() {
  local label="$1"
  local value="$2"
  [ -z "$value" ] && return
  jq -en --arg value "$value" '$value | tonumber' >/dev/null \
    || fail "invalid ${label}=${value}: must be numeric"
  decimal_gte "$value" "0" \
    || fail "invalid ${label}=${value}: must be zero or greater"
  decimal_lte "$value" "1" \
    || fail "invalid ${label}=${value}: must be at most 1"
}

artifact="${1:-}"
case "$artifact" in
  "" | -h | --help | help)
    usage
    [ -n "$artifact" ] || exit 1
    exit 0
    ;;
esac

[ -f "$artifact" ] || fail "artifact not found: $artifact"
require_binary jq
require_binary awk

validate_ratio_override "MAX_HOLD_RATIO" "$MAX_HOLD_RATIO"

doc_count="$(jq -s 'length' "$artifact")"
[ "$doc_count" = "1" ] || fail "expected exactly one JSON document, got ${doc_count}"
jq -e '.db_path? != null and .result.report? != null' "$artifact" >/dev/null \
  || fail "artifact must contain db_path and result.report"

closed_trades="$(jq_number '.result.report.trade_summary.closed_trades')"
wins="$(jq_number '.result.report.trade_summary.wins')"
losses="$(jq_number '.result.report.trade_summary.losses')"
win_rate="$(jq_number '.result.report.trade_summary.win_rate')"
net_pnl="$(jq_number '.result.report.trade_summary.net_pnl')"
fees="$(jq_number '.result.report.trade_summary.fees')"
avg_net_pnl="$(jq_number '.result.report.trade_summary.avg_net_pnl_per_trade')"
max_drawdown_pct="$(jq_number '.result.report.trade_summary.max_drawdown_pct')"
signal_quality_coverage="$(jq_number '.result.report.signal_quality.coverage')"
hold_ratio="$(jq -er '(.result.report.action_split.hold // "0") | tonumber' "$artifact")"
ai_provider_degraded_cycles="$(jq_number '.result.report.ai_provider_degradation.degraded_cycles')"
delta_win_rate="$(jq_number '.result.report.baseline_comparison.delta_win_rate')"
delta_net_pnl="$(jq_number '.result.report.baseline_comparison.delta_net_pnl')"
delta_avg_pnl="$(jq_number '.result.report.baseline_comparison.delta_avg_pnl_per_trade')"

validate_min_decimal "closed_trades" "$closed_trades" "$MIN_TRADES"
validate_min_decimal "win_rate" "$win_rate" "$MIN_WIN_RATE"
validate_min_decimal "net_pnl" "$net_pnl" "$MIN_NET_PNL"
validate_min_decimal "avg_net_pnl_per_trade" "$avg_net_pnl" "$MIN_AVG_NET_PNL"
validate_min_decimal "signal_quality.coverage" "$signal_quality_coverage" "$MIN_SIGNAL_QUALITY_COVERAGE"
validate_max_decimal "action_split.hold" "$hold_ratio" "$MAX_HOLD_RATIO"
validate_max_decimal "max_drawdown_pct" "$max_drawdown_pct" "$MAX_DRAWDOWN_PCT"
validate_max_int "ai_provider_degraded_cycles" "$ai_provider_degraded_cycles" "$MAX_AI_PROVIDER_DEGRADED_CYCLES"
validate_min_decimal "baseline.delta_win_rate" "$delta_win_rate" "$MIN_BASELINE_WIN_RATE_DELTA"
validate_min_decimal "baseline.delta_net_pnl" "$delta_net_pnl" "$MIN_BASELINE_NET_PNL_DELTA"
validate_min_decimal "baseline.delta_avg_pnl_per_trade" "$delta_avg_pnl" "$MIN_BASELINE_AVG_PNL_DELTA"

jq -e '
  .result.report.insufficient_trade_proof == false and
  (.result.report.signal_quality.missing_signal_quality_cycles | tonumber) == 0 and
  (.result.report.signal_quality.avg_bid_ask_spread_pct | tonumber) >= 0 and
  (.result.report.signal_quality.avg_abs_order_book_imbalance | tonumber) >= 0 and
  (.result.report.signal_quality.avg_range_position_24h | tonumber) >= 0 and
  (.result.report.signal_quality.avg_price_change_24h_pct | tonumber | type) == "number"
' "$artifact" >/dev/null || fail "artifact is missing required signal-quality or proof fields"

db_path="${2:-$(jq_string '.db_path')}"
if [ -n "$db_path" ]; then
  require_binary sqlite3
  [ -f "$db_path" ] || fail "SQLite DB not found: $db_path"

  telemetry_rows="$(sqlite3 "$db_path" 'select count(*) from scalping_cycle_telemetry;')"
  realized_rows="$(sqlite3 "$db_path" 'select count(*) from realized_pnl_journal;')"
  positive_realized="$(sqlite3 "$db_path" 'select count(*) from realized_pnl_journal where realized_pnl > 0;')"
  negative_realized="$(sqlite3 "$db_path" 'select count(*) from realized_pnl_journal where realized_pnl < 0;')"
  missing_quality_rows="$(sqlite3 "$db_path" "select count(*) from scalping_cycle_telemetry where bid_ask_spread_pct is null or order_book_imbalance is null or range_position_24h is null or price_change_24h_pct is null;")"
  total_cycles="$(jq_number '.result.report.total_cycles')"

  [ "$telemetry_rows" -eq "$total_cycles" ] \
    || fail "scalping_cycle_telemetry rows=${telemetry_rows} does not match total_cycles=${total_cycles}"
  [ "$realized_rows" -eq "$closed_trades" ] \
    || fail "realized_pnl_journal rows=${realized_rows} does not match closed_trades=${closed_trades}"
  [ "$positive_realized" -eq "$wins" ] \
    || fail "positive realized rows=${positive_realized} does not match wins=${wins}"
  [ "$negative_realized" -eq "$losses" ] \
    || fail "negative realized rows=${negative_realized} does not match losses=${losses}"
  [ "$missing_quality_rows" -eq 0 ] \
    || fail "scalping_cycle_telemetry has ${missing_quality_rows} rows missing signal-quality fields"
fi

cat <<SUMMARY
scalping soak artifact verified
artifact: ${artifact}
db_path: ${db_path:-not checked}
closed_trades: ${closed_trades}
wins: ${wins}
losses: ${losses}
win_rate: ${win_rate}
net_pnl: ${net_pnl}
fees: ${fees}
avg_net_pnl_per_trade: ${avg_net_pnl}
hold_ratio: ${hold_ratio}
signal_quality_coverage: ${signal_quality_coverage}
max_drawdown_pct: ${max_drawdown_pct}
ai_provider_degraded_cycles: ${ai_provider_degraded_cycles}
SUMMARY
