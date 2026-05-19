#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VALIDATOR="${SCRIPT_DIR}/validate-scalping-rule-candidate.py"

require_binary() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required binary: $1" >&2
    exit 1
  }
}

require_binary python3
require_binary jq
require_binary sqlite3

tmp_dir="$(mktemp -d /tmp/neuratrade-scalping-rule-validator-test.XXXXXX)"
trap 'rm -rf "$tmp_dir"' EXIT

train_db="${tmp_dir}/train.db"
validation_db="${tmp_dir}/validation.db"
flat_train_db="${tmp_dir}/flat-train.db"
flat_validation_db="${tmp_dir}/flat-validation.db"
pass_output="${tmp_dir}/pass.json"
fail_output="${tmp_dir}/fail.json"
flat_output="${tmp_dir}/flat.json"

create_schema() {
  sqlite3 "$1" <<'SQL'
CREATE TABLE scalping_cycle_telemetry (
  id TEXT PRIMARY KEY,
  exchange TEXT,
  symbol TEXT,
  cycle_at TIMESTAMP,
  signal_price REAL,
  bid_ask_spread_pct REAL,
  order_book_imbalance REAL,
  range_position_24h REAL,
  price_change_24h_pct REAL,
  recent_price_change_pct REAL
);
SQL
}

insert_row() {
  local db="$1"
  local id="$2"
  local symbol="$3"
  local cycle_at="$4"
  local price="$5"
  sqlite3 "$db" <<SQL
INSERT INTO scalping_cycle_telemetry (
  id, exchange, symbol, cycle_at, signal_price, bid_ask_spread_pct,
  order_book_imbalance, range_position_24h, price_change_24h_pct,
  recent_price_change_pct
) VALUES (
  '${id}', 'bitget', '${symbol}', '${cycle_at}', ${price}, 0.02, 0.45, 20, 0.10, 0.08
);
SQL
}

create_schema "$train_db"
create_schema "$validation_db"
create_schema "$flat_train_db"
create_schema "$flat_validation_db"

insert_row "$train_db" train-a-entry AAA/USDT '2026-05-19T00:00:00Z' 100
insert_row "$train_db" train-a-exit AAA/USDT '2026-05-19T00:05:00Z' 101
insert_row "$train_db" train-b-entry BBB/USDT '2026-05-19T00:00:00Z' 100
insert_row "$train_db" train-b-exit BBB/USDT '2026-05-19T00:05:00Z' 99.9
insert_row "$train_db" train-c-entry EEE/USDT '2026-05-19T00:00:00Z' 100
insert_row "$train_db" train-c-exit EEE/USDT '2026-05-19T00:05:00Z' 100.5
insert_row "$validation_db" val-a-entry CCC/USDT '2026-05-19T00:00:00Z' 100
insert_row "$validation_db" val-a-exit CCC/USDT '2026-05-19T00:05:00Z' 100.4
insert_row "$validation_db" val-b-entry DDD/USDT '2026-05-19T00:00:00Z' 100
insert_row "$validation_db" val-b-exit DDD/USDT '2026-05-19T00:05:00Z' 99.95
insert_row "$validation_db" val-c-entry FFF/USDT '2026-05-19T00:00:00Z' 100
insert_row "$validation_db" val-c-exit FFF/USDT '2026-05-19T00:05:00Z' 100.3
insert_row "$flat_train_db" flat-train-a-entry AAA/USDT '2026-05-19T00:00:00Z' 100
insert_row "$flat_train_db" flat-train-a-exit AAA/USDT '2026-05-19T00:05:00Z' 101
insert_row "$flat_train_db" flat-train-b-entry BBB/USDT '2026-05-19T00:00:00Z' 100
insert_row "$flat_train_db" flat-train-b-exit BBB/USDT '2026-05-19T00:05:00Z' 100
insert_row "$flat_validation_db" flat-val-a-entry CCC/USDT '2026-05-19T00:00:00Z' 100
insert_row "$flat_validation_db" flat-val-a-exit CCC/USDT '2026-05-19T00:05:00Z' 101
insert_row "$flat_validation_db" flat-val-b-entry DDD/USDT '2026-05-19T00:00:00Z' 100
insert_row "$flat_validation_db" flat-val-b-exit DDD/USDT '2026-05-19T00:05:00Z' 100

python3 "$VALIDATOR" \
  --train-db "$train_db" \
  --validation-db "$validation_db" \
  --side buy \
  --min-imbalance 0.35 \
  --max-spread 0.06 \
  --max-range 35 \
  --min-recent 0.05 \
  --min-24h 0.02 \
  --min-trades 3 \
  --min-validation-trades 3 \
  --min-symbols 3 \
  --min-validation-symbols 3 \
  --min-drawdown-pct 0.2 \
  --min-validation-drawdown-pct 0.1 \
  >"$pass_output"

jq -e \
  '.passed == true
    and .train.trades == 3
    and .train.wins == 2
    and .train.losses == 1
    and .train.breakevens == 0
    and .train.net_pct > 0
    and .train.net_pct_excluding_best > 0
    and .train.max_drawdown_pct > 0
    and .validation.trades == 3
    and .validation.wins == 2
    and .validation.losses == 1
    and .validation.breakevens == 0
    and .validation.net_pct > 0
    and .validation.net_pct_excluding_best > 0
    and .validation.max_drawdown_pct > 0
    and (.failures | length) == 0' \
  "$pass_output" >/dev/null

if python3 "$VALIDATOR" \
  --train-db "$train_db" \
  --validation-db "$validation_db" \
  --side buy \
  --min-imbalance 0.35 \
  --max-spread 0.06 \
  --max-range 35 \
  --min-recent 0.05 \
  --min-24h 0.02 \
  --min-trades 4 \
  --min-validation-trades 4 \
  --min-symbols 3 \
  --min-validation-symbols 3 \
  --min-drawdown-pct 0.5 \
  --min-validation-drawdown-pct 0.5 \
  >"$fail_output"; then
  echo "expected candidate validator to fail minimum-trade gates" >&2
  exit 1
fi

jq -e \
  '.passed == false
    and any(.failures[]; contains("train.trades=3 below minimum=4"))
    and any(.failures[]; contains("validation.trades=3 below minimum=4"))
    and any(.failures[]; contains("train.max_drawdown_pct="))
    and any(.failures[]; contains("validation.max_drawdown_pct="))' \
  "$fail_output" >/dev/null

if python3 "$VALIDATOR" \
  --train-db "$flat_train_db" \
  --validation-db "$flat_validation_db" \
  --side buy \
  --min-imbalance 0.35 \
  --max-spread 0.06 \
  --max-range 35 \
  --min-recent 0.05 \
  --min-24h 0.02 \
  --fee-pct 0 \
  --min-trades 2 \
  --min-validation-trades 2 \
  --min-symbols 2 \
  --min-validation-symbols 2 \
  >"$flat_output"; then
  echo "expected candidate validator to reject breakevens as loss proof" >&2
  exit 1
fi

jq -e \
  '.passed == false
    and .train.wins == 1
    and .train.losses == 0
    and .train.breakevens == 1
    and .validation.wins == 1
    and .validation.losses == 0
    and .validation.breakevens == 1
    and any(.failures[]; contains("train.losses=0 below minimum=1"))
    and any(.failures[]; contains("validation.losses=0 below minimum=1"))' \
  "$flat_output" >/dev/null

echo "validate-scalping-rule-candidate tests passed"
