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
portfolio_train_db="${tmp_dir}/portfolio-train.db"
portfolio_validation_db="${tmp_dir}/portfolio-validation.db"
reversal_train_db="${tmp_dir}/reversal-train.db"
reversal_validation_db="${tmp_dir}/reversal-validation.db"
sell_window_train_db="${tmp_dir}/sell-window-train.db"
sell_window_validation_db="${tmp_dir}/sell-window-validation.db"
split_db="${tmp_dir}/split.db"
pass_output="${tmp_dir}/pass.json"
search_output="${tmp_dir}/search.json"
portfolio_output="${tmp_dir}/portfolio.json"
manual_portfolio_file="${tmp_dir}/manual-portfolio.json"
manual_portfolio_output="${tmp_dir}/manual-portfolio.json.out"
reversal_output="${tmp_dir}/reversal.json"
sell_window_output="${tmp_dir}/sell-window.json"
hold_sweep_output="${tmp_dir}/hold-sweep.json"
split_search_output="${tmp_dir}/split-search.json"
fail_output="${tmp_dir}/fail.json"
flat_output="${tmp_dir}/flat.json"
flat_search_output="${tmp_dir}/flat-search.json"
missing_side_output="${tmp_dir}/missing-side.out"
missing_validation_output="${tmp_dir}/missing-validation.out"
split_conflict_output="${tmp_dir}/split-conflict.out"
portfolio_file_conflict_output="${tmp_dir}/portfolio-file-conflict.out"
invalid_split_output="${tmp_dir}/invalid-split.out"
portfolio_grid_conflict_output="${tmp_dir}/portfolio-grid-conflict.out"
invalid_portfolio_rules_output="${tmp_dir}/invalid-portfolio-rules.out"
invalid_hold_sweep_output="${tmp_dir}/invalid-hold-sweep.out"
invalid_near_misses_output="${tmp_dir}/invalid-near-misses.out"
invalid_family_pool_output="${tmp_dir}/invalid-family-pool.out"

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

insert_custom_row() {
  local db="$1"
  local id="$2"
  local symbol="$3"
  local cycle_at="$4"
  local price="$5"
  local spread="$6"
  local imbalance="$7"
  local range_pos="$8"
  local change_24h="$9"
  local recent="${10}"
  sqlite3 "$db" <<SQL
INSERT INTO scalping_cycle_telemetry (
  id, exchange, symbol, cycle_at, signal_price, bid_ask_spread_pct,
  order_book_imbalance, range_position_24h, price_change_24h_pct,
  recent_price_change_pct
) VALUES (
  '${id}', 'bitget', '${symbol}', '${cycle_at}', ${price}, ${spread}, ${imbalance}, ${range_pos}, ${change_24h}, ${recent}
);
SQL
}

create_schema "$train_db"
create_schema "$validation_db"
create_schema "$flat_train_db"
create_schema "$flat_validation_db"
create_schema "$portfolio_train_db"
create_schema "$portfolio_validation_db"
create_schema "$reversal_train_db"
create_schema "$reversal_validation_db"
create_schema "$sell_window_train_db"
create_schema "$sell_window_validation_db"
create_schema "$split_db"

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
insert_row "$split_db" split-train-a-entry AAA/USDT '2026-05-19T00:00:00Z' 100
insert_row "$split_db" split-train-a-exit AAA/USDT '2026-05-19T00:05:00Z' 101
insert_row "$split_db" split-train-b-entry BBB/USDT '2026-05-19T00:10:00Z' 100
insert_row "$split_db" split-train-b-exit BBB/USDT '2026-05-19T00:15:00Z' 99.9
insert_row "$split_db" split-train-c-entry EEE/USDT '2026-05-19T00:20:00Z' 100
insert_row "$split_db" split-train-c-exit EEE/USDT '2026-05-19T00:25:00Z' 100.5
insert_row "$split_db" split-val-a-entry CCC/USDT '2026-05-19T01:00:00Z' 100
insert_row "$split_db" split-val-a-exit CCC/USDT '2026-05-19T01:05:00Z' 100.4
insert_row "$split_db" split-val-b-entry DDD/USDT '2026-05-19T01:10:00Z' 100
insert_row "$split_db" split-val-b-exit DDD/USDT '2026-05-19T01:15:00Z' 99.95
insert_row "$split_db" split-val-c-entry FFF/USDT '2026-05-19T01:20:00Z' 100
insert_row "$split_db" split-val-c-exit FFF/USDT '2026-05-19T01:25:00Z' 100.3

insert_row "$portfolio_train_db" portfolio-train-buy-a-entry AAA/USDT '2026-05-19T00:00:00Z' 100
insert_row "$portfolio_train_db" portfolio-train-buy-a-exit AAA/USDT '2026-05-19T00:05:00Z' 101
insert_row "$portfolio_train_db" portfolio-train-buy-b-entry BBB/USDT '2026-05-19T00:00:00Z' 100
insert_row "$portfolio_train_db" portfolio-train-buy-b-exit BBB/USDT '2026-05-19T00:05:00Z' 99.9
insert_row "$portfolio_train_db" portfolio-train-buy-c-entry CCC/USDT '2026-05-19T00:00:00Z' 100
insert_row "$portfolio_train_db" portfolio-train-buy-c-exit CCC/USDT '2026-05-19T00:05:00Z' 100.5
insert_custom_row "$portfolio_train_db" portfolio-train-sell-a-entry SAA/USDT '2026-05-19T00:10:00Z' 100 0.02 -0.45 85 -0.10 -0.08
insert_custom_row "$portfolio_train_db" portfolio-train-sell-a-exit SAA/USDT '2026-05-19T00:15:00Z' 99 0.02 -0.45 85 -0.10 -0.08
insert_custom_row "$portfolio_train_db" portfolio-train-sell-b-entry SBB/USDT '2026-05-19T00:10:00Z' 100 0.02 -0.45 85 -0.10 -0.08
insert_custom_row "$portfolio_train_db" portfolio-train-sell-b-exit SBB/USDT '2026-05-19T00:15:00Z' 100.1 0.02 -0.45 85 -0.10 -0.08
insert_custom_row "$portfolio_train_db" portfolio-train-sell-c-entry SCC/USDT '2026-05-19T00:10:00Z' 100 0.02 -0.45 85 -0.10 -0.08
insert_custom_row "$portfolio_train_db" portfolio-train-sell-c-exit SCC/USDT '2026-05-19T00:15:00Z' 99.6 0.02 -0.45 85 -0.10 -0.08

insert_row "$portfolio_validation_db" portfolio-val-buy-a-entry VAA/USDT '2026-05-19T00:00:00Z' 100
insert_row "$portfolio_validation_db" portfolio-val-buy-a-exit VAA/USDT '2026-05-19T00:05:00Z' 100.4
insert_row "$portfolio_validation_db" portfolio-val-buy-b-entry VBB/USDT '2026-05-19T00:00:00Z' 100
insert_row "$portfolio_validation_db" portfolio-val-buy-b-exit VBB/USDT '2026-05-19T00:05:00Z' 99.95
insert_row "$portfolio_validation_db" portfolio-val-buy-c-entry VCC/USDT '2026-05-19T00:00:00Z' 100
insert_row "$portfolio_validation_db" portfolio-val-buy-c-exit VCC/USDT '2026-05-19T00:05:00Z' 100.3
insert_custom_row "$portfolio_validation_db" portfolio-val-sell-a-entry VSA/USDT '2026-05-19T00:10:00Z' 100 0.02 -0.45 85 -0.10 -0.08
insert_custom_row "$portfolio_validation_db" portfolio-val-sell-a-exit VSA/USDT '2026-05-19T00:15:00Z' 99.7 0.02 -0.45 85 -0.10 -0.08
insert_custom_row "$portfolio_validation_db" portfolio-val-sell-b-entry VSB/USDT '2026-05-19T00:10:00Z' 100 0.02 -0.45 85 -0.10 -0.08
insert_custom_row "$portfolio_validation_db" portfolio-val-sell-b-exit VSB/USDT '2026-05-19T00:15:00Z' 100.05 0.02 -0.45 85 -0.10 -0.08
insert_custom_row "$portfolio_validation_db" portfolio-val-sell-c-entry VSC/USDT '2026-05-19T00:10:00Z' 100 0.02 -0.45 85 -0.10 -0.08
insert_custom_row "$portfolio_validation_db" portfolio-val-sell-c-exit VSC/USDT '2026-05-19T00:15:00Z' 99.7 0.02 -0.45 85 -0.10 -0.08

insert_custom_row "$reversal_train_db" reversal-train-a-entry RAA/USDT '2026-05-19T00:00:00Z' 100 0.04 -0.20 15 -6.0 -0.10
insert_custom_row "$reversal_train_db" reversal-train-a-exit RAA/USDT '2026-05-19T00:05:00Z' 101 0.04 -0.20 15 -6.0 -0.10
insert_custom_row "$reversal_train_db" reversal-train-b-entry RBB/USDT '2026-05-19T00:00:00Z' 100 0.04 -0.10 15 -6.0 -0.10
insert_custom_row "$reversal_train_db" reversal-train-b-exit RBB/USDT '2026-05-19T00:05:00Z' 99.9 0.04 -0.10 15 -6.0 -0.10
insert_custom_row "$reversal_train_db" reversal-train-c-entry RCC/USDT '2026-05-19T00:00:00Z' 100 0.04 0.00 15 -6.0 -0.10
insert_custom_row "$reversal_train_db" reversal-train-c-exit RCC/USDT '2026-05-19T00:05:00Z' 100.5 0.04 0.00 15 -6.0 -0.10
insert_custom_row "$reversal_validation_db" reversal-val-a-entry RVA/USDT '2026-05-19T00:00:00Z' 100 0.04 -0.20 15 -6.0 -0.10
insert_custom_row "$reversal_validation_db" reversal-val-a-exit RVA/USDT '2026-05-19T00:05:00Z' 100.4 0.04 -0.20 15 -6.0 -0.10
insert_custom_row "$reversal_validation_db" reversal-val-b-entry RVB/USDT '2026-05-19T00:00:00Z' 100 0.04 -0.10 15 -6.0 -0.10
insert_custom_row "$reversal_validation_db" reversal-val-b-exit RVB/USDT '2026-05-19T00:05:00Z' 99.95 0.04 -0.10 15 -6.0 -0.10
insert_custom_row "$reversal_validation_db" reversal-val-c-entry RVC/USDT '2026-05-19T00:00:00Z' 100 0.04 0.00 15 -6.0 -0.10
insert_custom_row "$reversal_validation_db" reversal-val-c-exit RVC/USDT '2026-05-19T00:05:00Z' 100.3 0.04 0.00 15 -6.0 -0.10

insert_custom_row "$sell_window_train_db" sell-window-train-a-entry SWA/USDT '2026-05-19T00:00:00Z' 100 0.04 -0.45 55 0.30 -0.10
insert_custom_row "$sell_window_train_db" sell-window-train-a-exit SWA/USDT '2026-05-19T00:05:00Z' 99 0.04 -0.45 55 0.30 -0.10
insert_custom_row "$sell_window_train_db" sell-window-train-b-entry SWB/USDT '2026-05-19T00:00:00Z' 100 0.04 -0.45 55 0.30 -0.10
insert_custom_row "$sell_window_train_db" sell-window-train-b-exit SWB/USDT '2026-05-19T00:05:00Z' 100.1 0.04 -0.45 55 0.30 -0.10
insert_custom_row "$sell_window_train_db" sell-window-train-c-entry SWC/USDT '2026-05-19T00:00:00Z' 100 0.04 -0.45 55 0.30 -0.10
insert_custom_row "$sell_window_train_db" sell-window-train-c-exit SWC/USDT '2026-05-19T00:05:00Z' 99.6 0.04 -0.45 55 0.30 -0.10
insert_custom_row "$sell_window_validation_db" sell-window-val-a-entry SWVA/USDT '2026-05-19T00:00:00Z' 100 0.04 -0.45 55 0.30 -0.10
insert_custom_row "$sell_window_validation_db" sell-window-val-a-exit SWVA/USDT '2026-05-19T00:05:00Z' 99.7 0.04 -0.45 55 0.30 -0.10
insert_custom_row "$sell_window_validation_db" sell-window-val-b-entry SWVB/USDT '2026-05-19T00:00:00Z' 100 0.04 -0.45 55 0.30 -0.10
insert_custom_row "$sell_window_validation_db" sell-window-val-b-exit SWVB/USDT '2026-05-19T00:05:00Z' 100.05 0.04 -0.45 55 0.30 -0.10
insert_custom_row "$sell_window_validation_db" sell-window-val-c-entry SWVC/USDT '2026-05-19T00:00:00Z' 100 0.04 -0.45 55 0.30 -0.10
insert_custom_row "$sell_window_validation_db" sell-window-val-c-exit SWVC/USDT '2026-05-19T00:05:00Z' 99.7 0.04 -0.45 55 0.30 -0.10

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

python3 "$VALIDATOR" \
  --train-db "$train_db" \
  --validation-db "$validation_db" \
  --search-grid \
  --side buy \
  --include-oracle-summary \
  --max-results 3 \
  --min-trades 3 \
  --min-validation-trades 3 \
  --min-symbols 3 \
  --min-validation-symbols 3 \
  --min-drawdown-pct 0.2 \
  --min-validation-drawdown-pct 0.1 \
  >"$search_output"

jq -e \
  '.search_grid == true
    and .passed == true
    and .hold_seconds == 300
    and .candidate_count == 3
    and .evaluated_rules > 0
    and (.candidates | length) == 3
    and all(.candidates[]; .rule.side == "buy")
    and all(.candidates[]; .train.trades == 3)
    and all(.candidates[]; .train.losses == 1)
    and all(.candidates[]; .validation.trades == 3)
    and all(.candidates[]; .validation.losses == 1)
    and .oracle_summary.note == "hindsight upper bound; diagnostic only, not an admissible trading rule"
    and .oracle_summary.train.observations == 3
    and .oracle_summary.train.opportunities == 6
    and .oracle_summary.train.top_reaches_min_trades == true
    and .oracle_summary.train.top_net_positive == true
    and .oracle_summary.train.top_trades == 3
    and .oracle_summary.train.top_side_counts.buy == 2
    and .oracle_summary.validation.observations == 3
    and .oracle_summary.validation.opportunities == 6
    and .oracle_summary.validation.top_reaches_min_trades == true
    and .oracle_summary.validation.top_net_positive == true
    and (.failures | length) == 0' \
  "$search_output" >/dev/null

python3 "$VALIDATOR" \
  --train-db "$portfolio_train_db" \
  --validation-db "$portfolio_validation_db" \
  --search-portfolio \
  --side both \
  --max-results 2 \
  --portfolio-pool-size 16 \
  --portfolio-family-pool-size 4 \
  --max-portfolio-rules 2 \
  --min-trades 6 \
  --min-validation-trades 6 \
  --min-symbols 6 \
  --min-validation-symbols 6 \
  --min-drawdown-pct 0.2 \
  --min-validation-drawdown-pct 0.1 \
  >"$portfolio_output"

jq -e \
  '.search_portfolio == true
    and .passed == true
    and .candidate_count >= 1
    and .evaluated_rules > 0
    and .portfolio_pool_size > 0
    and .evaluated_portfolios > 0
    and (.candidates | length) == 2
    and all(.candidates[]; (.rules | length) == 2)
    and all(.candidates[]; ([.rules[].side] | contains(["buy"]) and contains(["sell"])))
    and all(.candidates[]; .train.trades == 6)
    and all(.candidates[]; .validation.trades == 6)
    and all(.candidates[]; .train.side_counts.buy == 3 and .train.side_counts.sell == 3)
    and all(.candidates[]; .validation.side_counts.buy == 3 and .validation.side_counts.sell == 3)
    and (.failures | length) == 0' \
  "$portfolio_output" >/dev/null

cat >"$manual_portfolio_file" <<'JSON'
[
  {
    "side": "buy",
    "family": "manual_buy",
    "max_spread": 0.06,
    "min_imbalance": 0.35,
    "max_range": 35,
    "min_recent": 0.05,
    "min_24h": 0.02
  },
  {
    "side": "sell",
    "family": "manual_sell",
    "max_spread": 0.06,
    "max_imbalance": -0.35,
    "min_range": 80,
    "max_recent": -0.05,
    "max_24h": -0.05
  }
]
JSON

python3 "$VALIDATOR" \
  --train-db "$portfolio_train_db" \
  --validation-db "$portfolio_validation_db" \
  --portfolio-rule-file "$manual_portfolio_file" \
  --min-trades 6 \
  --min-validation-trades 6 \
  --min-symbols 6 \
  --min-validation-symbols 6 \
  --min-drawdown-pct 0.2 \
  --min-validation-drawdown-pct 0.1 \
  >"$manual_portfolio_output"

jq -e \
  '.passed == true
    and (.portfolio | length) == 2
    and .portfolio[0].family == "manual_buy"
    and .portfolio[1].family == "manual_sell"
    and .train.trades == 6
    and .validation.trades == 6
    and .train.side_counts.buy == 3
    and .train.side_counts.sell == 3
    and .validation.side_counts.buy == 3
    and .validation.side_counts.sell == 3
    and (.failures | length) == 0' \
  "$manual_portfolio_output" >/dev/null

python3 "$VALIDATOR" \
  --train-db "$reversal_train_db" \
  --validation-db "$reversal_validation_db" \
  --search-grid \
  --include-reversal-rules \
  --side buy \
  --max-results 1 \
  --min-trades 3 \
  --min-validation-trades 3 \
  --min-symbols 3 \
  --min-validation-symbols 3 \
  --min-drawdown-pct 0.2 \
  --min-validation-drawdown-pct 0.1 \
  >"$reversal_output"

jq -e \
  '.search_grid == true
    and .passed == true
    and .candidate_count == 1
    and .evaluated_rules > 2940
    and .candidates[0].rule.side == "buy"
    and .candidates[0].rule.family == "reversal"
    and .candidates[0].rule.max_24h <= -5
    and .candidates[0].train.trades == 3
    and .candidates[0].train.losses == 1
    and .candidates[0].validation.trades == 3
    and .candidates[0].validation.losses == 1' \
  "$reversal_output" >/dev/null

python3 "$VALIDATOR" \
  --train-db "$sell_window_train_db" \
  --validation-db "$sell_window_validation_db" \
  --search-grid \
  --include-sell-window-rules \
  --side sell \
  --max-results 1 \
  --min-trades 3 \
  --min-validation-trades 3 \
  --min-symbols 3 \
  --min-validation-symbols 3 \
  --min-drawdown-pct 0.2 \
  --min-validation-drawdown-pct 0.1 \
  >"$sell_window_output"

jq -e \
  '.search_grid == true
    and .passed == true
    and .candidate_count == 1
    and .candidates[0].rule.side == "sell"
    and .candidates[0].rule.family == "sell_window"
    and .candidates[0].rule.min_recent <= -0.1
    and .candidates[0].rule.max_recent >= -0.1
    and .candidates[0].rule.min_24h <= 0.3
    and .candidates[0].rule.max_24h >= 0.3
    and .candidates[0].train.trades == 3
    and .candidates[0].train.losses == 1
    and .candidates[0].validation.trades == 3
    and .candidates[0].validation.losses == 1' \
  "$sell_window_output" >/dev/null

python3 "$VALIDATOR" \
  --train-db "$portfolio_train_db" \
  --validation-db "$portfolio_validation_db" \
  --search-portfolio \
  --side both \
  --hold-seconds-candidates 60,300 \
  --max-results 1 \
  --portfolio-pool-size 16 \
  --max-portfolio-rules 2 \
  --min-trades 6 \
  --min-validation-trades 6 \
  --min-symbols 6 \
  --min-validation-symbols 6 \
  --min-drawdown-pct 0.2 \
  --min-validation-drawdown-pct 0.1 \
  >"$hold_sweep_output"

jq -e \
  '.hold_sweep == true
    and .search_mode == "portfolio"
    and .passed == true
    and .candidate_count == 2
    and .holds == [60, 300]
    and (.results | length) == 2
    and all(.results[]; .search_portfolio == true and .passed == true and .candidate_count == 1)
    and all(.results[]; (.candidates[0].rules | length) == 2)
    and (.results[0].hold_seconds == 60)
    and (.results[1].hold_seconds == 300)
    and (.results[0].candidates[0].rules[0].hold_seconds == 60)
    and (.results[1].candidates[0].rules[0].hold_seconds == 300)
    and (.failures | length) == 0' \
  "$hold_sweep_output" >/dev/null

python3 "$VALIDATOR" \
  --train-db "$split_db" \
  --validation-split-ratio 0.5 \
  --search-grid \
  --side buy \
  --max-results 2 \
  --min-trades 3 \
  --min-validation-trades 3 \
  --min-symbols 3 \
  --min-validation-symbols 3 \
  --min-drawdown-pct 0.2 \
  --min-validation-drawdown-pct 0.1 \
  >"$split_search_output"

jq -e \
  '.search_grid == true
    and .passed == true
    and .candidate_count == 2
    and (.candidates | length) == 2
    and all(.candidates[]; .train.trades == 3)
    and all(.candidates[]; .validation.trades == 3)' \
  "$split_search_output" >/dev/null

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
  --near-misses 2 \
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

if python3 "$VALIDATOR" \
  --train-db "$flat_train_db" \
  --validation-db "$flat_validation_db" \
  --search-grid \
  --side buy \
  --fee-pct 0 \
  --near-misses 2 \
  --min-trades 2 \
  --min-validation-trades 2 \
  --min-symbols 2 \
  --min-validation-symbols 2 \
  >"$flat_search_output"; then
  echo "expected search mode to reject no-loss flat candidates" >&2
  exit 1
fi

jq -e \
  '.search_grid == true
    and .passed == false
    and .candidate_count == 0
    and (.near_misses | length) == 2
    and all(.near_misses[]; (.failures | length) > 0)
    and all(.near_misses[]; .gate_deficit >= 0)
    and .near_misses[0].gate_deficit <= .near_misses[1].gate_deficit
    and any(.near_misses[].failures[]; contains("losses=0 below minimum=1"))
    and any(.failures[]; . == "no_candidate_rule_passed_train_validation_gates")' \
  "$flat_search_output" >/dev/null

if python3 "$VALIDATOR" \
  --train-db "$train_db" \
  --validation-db "$validation_db" \
  --min-imbalance 0.35 \
  >"$missing_side_output" 2>&1; then
  echo "expected validator to require --side outside search mode" >&2
  exit 1
fi

grep -q -- "--side buy|sell is required unless --search-grid is enabled" "$missing_side_output"

if python3 "$VALIDATOR" \
  --train-db "$train_db" \
  --side buy \
  --min-imbalance 0.35 \
  >"$missing_validation_output" 2>&1; then
  echo "expected validator to require validation data" >&2
  exit 1
fi

grep -q -- "--validation-db is required unless --validation-split-ratio is set" "$missing_validation_output"

if python3 "$VALIDATOR" \
  --train-db "$train_db" \
  --validation-db "$validation_db" \
  --validation-split-ratio 0.5 \
  --side buy \
  --min-imbalance 0.35 \
  >"$split_conflict_output" 2>&1; then
  echo "expected validator to reject validation split plus validation DB" >&2
  exit 1
fi

grep -q -- "--validation-split-ratio cannot be combined with --validation-db" "$split_conflict_output"

if python3 "$VALIDATOR" \
  --train-db "$train_db" \
  --validation-db "$validation_db" \
  --validation-split-ratio -0.5 \
  --side buy \
  --min-imbalance 0.35 \
  >"$invalid_split_output" 2>&1; then
  echo "expected validator to reject invalid validation split ratio" >&2
  exit 1
fi

grep -q -- "--validation-split-ratio must be greater than 0 and less than 1" "$invalid_split_output"

if python3 "$VALIDATOR" \
  --train-db "$train_db" \
  --validation-db "$validation_db" \
  --search-grid \
  --search-portfolio \
  >"$portfolio_grid_conflict_output" 2>&1; then
  echo "expected validator to reject grid plus portfolio search" >&2
  exit 1
fi

grep -q -- "--search-grid cannot be combined with --search-portfolio" "$portfolio_grid_conflict_output"

if python3 "$VALIDATOR" \
  --train-db "$train_db" \
  --validation-db "$validation_db" \
  --search-grid \
  --portfolio-rule-file "$manual_portfolio_file" \
  >"$portfolio_file_conflict_output" 2>&1; then
  echo "expected validator to reject portfolio rule file plus search mode" >&2
  exit 1
fi

grep -q -- "--portfolio-rule-file cannot be combined with search modes" "$portfolio_file_conflict_output"

if python3 "$VALIDATOR" \
  --train-db "$train_db" \
  --validation-db "$validation_db" \
  --search-portfolio \
  --max-portfolio-rules 1 \
  >"$invalid_portfolio_rules_output" 2>&1; then
  echo "expected validator to reject invalid max portfolio rule count" >&2
  exit 1
fi

grep -q -- "--max-portfolio-rules must be at least 2" "$invalid_portfolio_rules_output"

if python3 "$VALIDATOR" \
  --train-db "$train_db" \
  --validation-db "$validation_db" \
  --search-grid \
  --hold-seconds-candidates 15,nope \
  >"$invalid_hold_sweep_output" 2>&1; then
  echo "expected validator to reject invalid hold sweep values" >&2
  exit 1
fi

grep -q -- "--hold-seconds-candidates must contain positive integers" "$invalid_hold_sweep_output"

if python3 "$VALIDATOR" \
  --train-db "$train_db" \
  --validation-db "$validation_db" \
  --search-grid \
  --near-misses -1 \
  >"$invalid_near_misses_output" 2>&1; then
  echo "expected validator to reject invalid near-miss count" >&2
  exit 1
fi

grep -q -- "--near-misses must be zero or greater" "$invalid_near_misses_output"

if python3 "$VALIDATOR" \
  --train-db "$train_db" \
  --validation-db "$validation_db" \
  --search-portfolio \
  --portfolio-family-pool-size -1 \
  >"$invalid_family_pool_output" 2>&1; then
  echo "expected validator to reject invalid family pool size" >&2
  exit 1
fi

grep -q -- "--portfolio-family-pool-size must be zero or greater" "$invalid_family_pool_output"

echo "validate-scalping-rule-candidate tests passed"
