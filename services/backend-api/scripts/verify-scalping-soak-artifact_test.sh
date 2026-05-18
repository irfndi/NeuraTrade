#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VERIFIER="${SCRIPT_DIR}/verify-scalping-soak-artifact.sh"

require_binary() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required binary: $1" >&2
    exit 1
  }
}

require_binary jq
require_binary sqlite3

tmp_dir="$(mktemp -d /tmp/neuratrade-scalping-verifier-test.XXXXXX)"
trap 'rm -rf "$tmp_dir"' EXIT

db_path="${tmp_dir}/soak.db"
artifact_path="${tmp_dir}/soak.json"
positive_output="${tmp_dir}/positive.out"
negative_output="${tmp_dir}/negative.out"

sqlite3 "$db_path" <<'SQL'
CREATE TABLE scalping_cycle_telemetry (
  id TEXT PRIMARY KEY,
  bid_ask_spread_pct REAL,
  order_book_imbalance REAL,
  range_position_24h REAL,
  price_change_24h_pct REAL
);
CREATE TABLE realized_pnl_journal (
  id TEXT PRIMARY KEY,
  realized_pnl NUMERIC NOT NULL DEFAULT 0
);
INSERT INTO scalping_cycle_telemetry VALUES ('cycle-1', 0.05, 0.32, 28.5, 0.4);
INSERT INTO scalping_cycle_telemetry VALUES ('cycle-2', 0.07, -0.18, 66.2, -0.2);
INSERT INTO realized_pnl_journal VALUES ('trade-1', 0.12);
SQL

jq -n --arg db_path "$db_path" '{
  db_path: $db_path,
  result: {
    report: {
      total_cycles: 2,
      action_split: {
        buy: "0.5",
        hold: "0.5"
      },
      regime_split: {
        neutral: "1"
      },
      rejection_by_reason: {
        no_directional_edge: 1
      },
      gate_block_by_code: {},
      signal_quality: {
        known_cycles: 2,
        coverage: "1",
        avg_bid_ask_spread_pct: "0.06",
        avg_abs_order_book_imbalance: "0.25",
        avg_range_position_24h: "47.35",
        avg_price_change_24h_pct: "0.1",
        missing_signal_quality_cycles: 0
      },
      trade_summary: {
        closed_trades: 1,
        wins: 1,
        losses: 0,
        win_rate: "1",
        gross_pnl: "0.13",
        net_pnl: "0.12",
        fees: "0.01",
        avg_net_pnl_per_trade: "0.12",
        max_drawdown_pct: "0"
      },
      ai_provider_degradation: {
        degraded_cycles: 0
      },
      baseline_comparison: {
        delta_win_rate: "0.877",
        delta_net_pnl: "0.3",
        delta_avg_pnl_per_trade: "0.123"
      },
      insufficient_trade_proof: false
    }
  }
}' >"$artifact_path"

"$VERIFIER" "$artifact_path" >"$positive_output"

if MAX_HOLD_RATIO=0.1 "$VERIFIER" "$artifact_path" >"$negative_output" 2>&1; then
  echo "expected verifier to fail when MAX_HOLD_RATIO is below artifact hold ratio" >&2
  exit 1
fi

if ! grep -q "action_split.hold=0.5 above maximum=0.1" "$negative_output"; then
  echo "negative verifier output did not contain hold-ratio failure" >&2
  cat "$negative_output" >&2
  exit 1
fi

if MAX_HOLD_RATIO=74.5 "$VERIFIER" "$artifact_path" >"$negative_output" 2>&1; then
  echo "expected verifier to reject percent-style MAX_HOLD_RATIO values" >&2
  exit 1
fi

if ! grep -q "invalid MAX_HOLD_RATIO=74.5: must be at most 1" "$negative_output"; then
  echo "negative verifier output did not contain ratio-bound failure" >&2
  cat "$negative_output" >&2
  exit 1
fi

missing_gross_artifact="${tmp_dir}/missing-gross-pnl.json"
jq 'del(.result.report.trade_summary.gross_pnl)' "$artifact_path" >"$missing_gross_artifact"
if "$VERIFIER" "$missing_gross_artifact" >"$negative_output" 2>&1; then
  echo "expected verifier to fail when gross_pnl is missing" >&2
  exit 1
fi

if ! grep -q "trade_summary.gross_pnl" "$negative_output"; then
  echo "negative verifier output did not mention missing gross_pnl" >&2
  cat "$negative_output" >&2
  exit 1
fi

empty_db_artifact="${tmp_dir}/empty-db-path.json"
jq '.db_path = ""' "$artifact_path" >"$empty_db_artifact"
if "$VERIFIER" "$empty_db_artifact" >"$negative_output" 2>&1; then
  echo "expected verifier to fail when db_path is empty" >&2
  exit 1
fi

if ! grep -q "db_path is required for persisted SQLite evidence" "$negative_output"; then
  echo "negative verifier output did not mention required db_path" >&2
  cat "$negative_output" >&2
  exit 1
fi

missing_regime_artifact="${tmp_dir}/missing-regime-split.json"
jq 'del(.result.report.regime_split)' "$artifact_path" >"$missing_regime_artifact"
if "$VERIFIER" "$missing_regime_artifact" >"$negative_output" 2>&1; then
  echo "expected verifier to fail when regime_split is missing" >&2
  exit 1
fi

if ! grep -q "action/regime split" "$negative_output"; then
  echo "negative verifier output did not mention required regime split" >&2
  cat "$negative_output" >&2
  exit 1
fi

missing_rejection_artifact="${tmp_dir}/missing-rejection-reasons.json"
jq 'del(.result.report.rejection_by_reason)' "$artifact_path" >"$missing_rejection_artifact"
if "$VERIFIER" "$missing_rejection_artifact" >"$negative_output" 2>&1; then
  echo "expected verifier to fail when rejection_by_reason is missing" >&2
  exit 1
fi

if ! grep -q "gate/rejection reason" "$negative_output"; then
  echo "negative verifier output did not mention required rejection reasons" >&2
  cat "$negative_output" >&2
  exit 1
fi

echo "verify-scalping-soak-artifact tests passed"
