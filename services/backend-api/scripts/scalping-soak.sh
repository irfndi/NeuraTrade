#!/usr/bin/env bash

# Run a no-order public-data scalping paper soak and persist telemetry/lifecycle
# rows into the selected SQLite database.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BACKEND_ROOT="$(dirname "$SCRIPT_DIR")"
REPO_ROOT="$(dirname "$(dirname "$BACKEND_ROOT")")"
NEURATRADE_HOME="${NEURATRADE_HOME:-$HOME/.neuratrade}"
LOG_DIR="${NEURATRADE_HOME}/logs"
LOG_FILE="${LOG_DIR}/scalping-soak.log"

EXCHANGE="${EXCHANGE:-bitget}"
CYCLES="${CYCLES:-12}"
INTERVAL_MS="${INTERVAL_MS:-5000}"
TIMEOUT_SECONDS="${TIMEOUT_SECONDS:-0}"
HOLD_PERIOD_SECONDS="${HOLD_PERIOD_SECONDS:-300}"
MAX_PAIRS="${MAX_PAIRS:-${NEURATRADE_SCALPING_MAX_PAIRS:-0}}"
MAX_CANDIDATES="${MAX_CANDIDATES:-${NEURATRADE_SCALPING_MAX_CANDIDATES:-0}}"
ORDERBOOK_PAIRS="${ORDERBOOK_PAIRS:-${NEURATRADE_SCALPING_ORDERBOOK_PAIRS:-0}}"
CAPITAL="${CAPITAL:-48}"
FEE_RATE="${FEE_RATE:-0.0006}"
REQUIRE_TRADES="${REQUIRE_TRADES:-true}"
MIN_TRADES="${MIN_TRADES-1}"
MIN_WIN_RATE="${MIN_WIN_RATE-0.123}"
MIN_NET_PNL="${MIN_NET_PNL-0}"
MIN_AVG_NET_PNL="${MIN_AVG_NET_PNL-0}"
MIN_SIGNAL_QUALITY_COVERAGE="${MIN_SIGNAL_QUALITY_COVERAGE-1}"
MAX_HOLD_RATIO="${MAX_HOLD_RATIO-}"
MAX_DRAWDOWN="${MAX_DRAWDOWN-}"
MAX_DRAWDOWN_PCT="${MAX_DRAWDOWN_PCT-0.01}"
MAX_AI_PROVIDER_DEGRADED_CYCLES="${MAX_AI_PROVIDER_DEGRADED_CYCLES-0}"
MAX_PERFECT_WIN_TRADES="${MAX_PERFECT_WIN_TRADES-20}"
MIN_BASELINE_WIN_RATE_DELTA="${MIN_BASELINE_WIN_RATE_DELTA-0}"
MIN_BASELINE_NET_PNL_DELTA="${MIN_BASELINE_NET_PNL_DELTA-0}"
MIN_BASELINE_AVG_PNL_DELTA="${MIN_BASELINE_AVG_PNL_DELTA-0}"
REQUIRE_LIVE_TRIAL_READY="${REQUIRE_LIVE_TRIAL_READY:-false}"
RECORD_ROLLOUT_PROOF="${RECORD_ROLLOUT_PROOF:-false}"
STRATEGY_ID="${STRATEGY_ID:-}"
SOAK_CHAT_ID="${SOAK_CHAT_ID:-operator-scalping-soak}"
SOAK_ORDER_PREFIX="${SOAK_ORDER_PREFIX:-operator-scalping-soak}"
SOAK_DB_PATH="${SOAK_DB_PATH:-${NEURATRADE_HOME}/data/scalping-soak.db}"
SOAK_BIN="${SOAK_BIN:-${REPO_ROOT}/bin/neuratrade-scalping-soak}"
SOAK_OUTPUT_FILE="${SOAK_OUTPUT_FILE:-}"

BLUE='\033[0;34m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

log() {
  mkdir -p "$LOG_DIR"
  echo -e "[$(date '+%Y-%m-%d %H:%M:%S')] ${BLUE}[SCALPING-SOAK]${NC} $1" | tee -a "$LOG_FILE"
}

fail() {
  log "${RED}[FAIL]${NC} $1"
  exit 1
}

usage() {
  cat <<USAGE
Usage: $(basename "$0") [run|help]

Runs the no-order public-data scalping paper soak added by bin/neuratrade-scalping-soak.
The command persists paper telemetry/lifecycle rows into SOAK_DB_PATH, but does not
place real exchange orders.

Environment:
  SOAK_BIN         Path to neuratrade-scalping-soak binary (default: ${SOAK_BIN})
  SOAK_DB_PATH    SQLite DB for persisted soak rows (default: ${SOAK_DB_PATH})
  EXCHANGE        Public exchange to probe (default: ${EXCHANGE})
  CYCLES          Number of probe cycles, capped by the binary (default: ${CYCLES})
  INTERVAL_MS     Delay between cycles in ms (default: ${INTERVAL_MS})
  TIMEOUT_SECONDS Overall timeout; 0 lets the binary calculate it (default: ${TIMEOUT_SECONDS})
  HOLD_PERIOD_SECONDS Paper position hold period; 0 uses binary default (default: ${HOLD_PERIOD_SECONDS})
  MAX_PAIRS       Maximum pairs to analyze per cycle; 0 uses scalping config default (default: ${MAX_PAIRS})
  MAX_CANDIDATES  Maximum discovered candidates to score; 0 uses scalping config default (default: ${MAX_CANDIDATES})
  ORDERBOOK_PAIRS Maximum pairs with orderbook quality per cycle; 0 uses scalping config default (default: ${ORDERBOOK_PAIRS})
  CAPITAL         Initial paper capital in USDT (default: ${CAPITAL})
  FEE_RATE        Paper simulator fee rate (default: ${FEE_RATE})
  REQUIRE_TRADES  true/false; fail when no paper trades are produced (default: ${REQUIRE_TRADES})
  MIN_TRADES      Minimum closed paper trades required (default: ${MIN_TRADES})
  MIN_WIN_RATE    Minimum win rate required as decimal; empty disables (default: ${MIN_WIN_RATE})
  MIN_NET_PNL     Minimum net PnL required; empty disables (default: ${MIN_NET_PNL})
  MIN_AVG_NET_PNL Minimum avg net PnL per trade required; empty disables (default: ${MIN_AVG_NET_PNL})
  MIN_SIGNAL_QUALITY_COVERAGE Minimum signal quality coverage; empty disables (default: ${MIN_SIGNAL_QUALITY_COVERAGE})
  MAX_HOLD_RATIO  Maximum hold action split allowed; empty disables (default: ${MAX_HOLD_RATIO})
  MAX_DRAWDOWN    Maximum absolute drawdown; empty disables (default: ${MAX_DRAWDOWN:-disabled})
  MAX_DRAWDOWN_PCT Maximum drawdown as fraction of baseline balance; empty disables (default: ${MAX_DRAWDOWN_PCT})
  MAX_AI_PROVIDER_DEGRADED_CYCLES Maximum AI provider degraded cycles; empty disables (default: ${MAX_AI_PROVIDER_DEGRADED_CYCLES})
  MAX_PERFECT_WIN_TRADES Maximum closed trades allowed with 100% wins and zero drawdown; empty disables (default: ${MAX_PERFECT_WIN_TRADES:-disabled})
  MIN_BASELINE_WIN_RATE_DELTA Minimum win-rate improvement versus baseline; empty disables (default: ${MIN_BASELINE_WIN_RATE_DELTA:-disabled})
  MIN_BASELINE_NET_PNL_DELTA Minimum net-PnL improvement versus baseline; empty disables (default: ${MIN_BASELINE_NET_PNL_DELTA:-disabled})
  MIN_BASELINE_AVG_PNL_DELTA Minimum avg-PnL/trade improvement versus baseline; empty disables (default: ${MIN_BASELINE_AVG_PNL_DELTA:-disabled})
  REQUIRE_LIVE_TRIAL_READY true/false; fail unless paper evidence can approve a tiny live/testnet trial (default: ${REQUIRE_LIVE_TRIAL_READY})
  RECORD_ROLLOUT_PROOF true/false; persist live-ready proof metrics into autonomy rollout state (default: ${RECORD_ROLLOUT_PROOF})
  STRATEGY_ID      Strategy id for RECORD_ROLLOUT_PROOF; empty uses chat scalping strategy id (default: ${STRATEGY_ID:-derived})
  SOAK_CHAT_ID    Chat id label for persisted soak telemetry (default: ${SOAK_CHAT_ID})
  SOAK_ORDER_PREFIX Order prefix label for persisted soak telemetry (default: ${SOAK_ORDER_PREFIX})
  SOAK_OUTPUT_FILE Optional path for clean result JSON artifact; empty disables (default: ${SOAK_OUTPUT_FILE:-disabled})

Examples:
  make build
  bash services/backend-api/scripts/scalping-soak.sh run
  SOAK_OUTPUT_FILE="\$HOME/.neuratrade/data/scalping-soak-latest.json" bash services/backend-api/scripts/scalping-soak.sh run
  bash services/backend-api/scripts/verify-scalping-soak-artifact.sh "\$HOME/.neuratrade/data/scalping-soak-latest.json"
  SOAK_DB_PATH="\$HOME/.neuratrade/data/neuratrade.db" CYCLES=24 bash services/backend-api/scripts/scalping-soak.sh run
USAGE
}

ensure_binary() {
  if [ -x "$SOAK_BIN" ]; then
    return
  fi

  log "${YELLOW}[WARN]${NC} ${SOAK_BIN} not found; building it with make build"
  (cd "$REPO_ROOT" && make build)
  [ -x "$SOAK_BIN" ] || fail "soak binary still missing after build: ${SOAK_BIN}"
}

run_soak() {
  ensure_binary
  mkdir -p "$(dirname "$SOAK_DB_PATH")"

  local args=(
    "--db" "$SOAK_DB_PATH"
    "--exchange" "$EXCHANGE"
    "--cycles" "$CYCLES"
    "--interval-ms" "$INTERVAL_MS"
    "--hold-period-seconds" "$HOLD_PERIOD_SECONDS"
    "--max-pairs" "$MAX_PAIRS"
    "--max-candidates" "$MAX_CANDIDATES"
    "--orderbook-pairs" "$ORDERBOOK_PAIRS"
    "--chat-id" "$SOAK_CHAT_ID"
    "--order-prefix" "$SOAK_ORDER_PREFIX"
    "--capital" "$CAPITAL"
    "--fee-rate" "$FEE_RATE"
  )
  if [ -n "$MIN_TRADES" ]; then
    args+=("--min-trades" "$MIN_TRADES")
  fi
  if [ -n "$MIN_WIN_RATE" ]; then
    args+=("--min-win-rate" "$MIN_WIN_RATE")
  fi
  if [ -n "$MIN_NET_PNL" ]; then
    args+=("--min-net-pnl" "$MIN_NET_PNL")
  fi
  if [ -n "$MIN_AVG_NET_PNL" ]; then
    args+=("--min-avg-net-pnl" "$MIN_AVG_NET_PNL")
  fi
  if [ -n "$MIN_SIGNAL_QUALITY_COVERAGE" ]; then
    args+=("--min-signal-quality-coverage" "$MIN_SIGNAL_QUALITY_COVERAGE")
  fi
  if [ -n "$MAX_HOLD_RATIO" ]; then
    args+=("--max-hold-ratio" "$MAX_HOLD_RATIO")
  fi
  if [ -n "$MAX_DRAWDOWN" ]; then
    args+=("--max-drawdown" "$MAX_DRAWDOWN")
  fi
  if [ -n "$MAX_DRAWDOWN_PCT" ]; then
    args+=("--max-drawdown-pct" "$MAX_DRAWDOWN_PCT")
  fi
  if [ -n "$MAX_AI_PROVIDER_DEGRADED_CYCLES" ]; then
    args+=("--max-ai-provider-degraded-cycles" "$MAX_AI_PROVIDER_DEGRADED_CYCLES")
  fi
  if [ -n "$MAX_PERFECT_WIN_TRADES" ]; then
    args+=("--max-perfect-win-trades" "$MAX_PERFECT_WIN_TRADES")
  fi
  if [ -n "$MIN_BASELINE_WIN_RATE_DELTA" ]; then
    args+=("--min-baseline-win-rate-delta" "$MIN_BASELINE_WIN_RATE_DELTA")
  fi
  if [ -n "$MIN_BASELINE_NET_PNL_DELTA" ]; then
    args+=("--min-baseline-net-pnl-delta" "$MIN_BASELINE_NET_PNL_DELTA")
  fi
  if [ -n "$MIN_BASELINE_AVG_PNL_DELTA" ]; then
    args+=("--min-baseline-avg-pnl-delta" "$MIN_BASELINE_AVG_PNL_DELTA")
  fi
  if [ "$TIMEOUT_SECONDS" != "0" ]; then
    args+=("--timeout-seconds" "$TIMEOUT_SECONDS")
  fi
  if [ "$REQUIRE_TRADES" = "true" ] || [ "$REQUIRE_TRADES" = "1" ]; then
    args+=("--require-trades")
  fi
  case "$(printf '%s' "$REQUIRE_LIVE_TRIAL_READY" | tr '[:upper:]' '[:lower:]')" in
    true | 1 | yes | on)
      args+=("--require-live-trial-ready")
      ;;
  esac
  case "$(printf '%s' "$RECORD_ROLLOUT_PROOF" | tr '[:upper:]' '[:lower:]')" in
    true | 1 | yes | on)
      args+=("--record-rollout-proof")
      if [ -n "$STRATEGY_ID" ]; then
        args+=("--strategy-id" "$STRATEGY_ID")
      fi
      ;;
  esac

  log "running no-order scalping paper soak exchange=${EXCHANGE} cycles=${CYCLES} interval_ms=${INTERVAL_MS} hold_period_seconds=${HOLD_PERIOD_SECONDS} db=${SOAK_DB_PATH} \
min_trades=${MIN_TRADES:-disabled} min_win_rate=${MIN_WIN_RATE:-disabled} min_net_pnl=${MIN_NET_PNL:-disabled} min_avg_net_pnl=${MIN_AVG_NET_PNL:-disabled} \
min_signal_quality_coverage=${MIN_SIGNAL_QUALITY_COVERAGE:-disabled} max_hold_ratio=${MAX_HOLD_RATIO:-disabled} max_drawdown=${MAX_DRAWDOWN:-disabled} max_drawdown_pct=${MAX_DRAWDOWN_PCT:-disabled} \
max_ai_provider_degraded_cycles=${MAX_AI_PROVIDER_DEGRADED_CYCLES:-disabled} max_perfect_win_trades=${MAX_PERFECT_WIN_TRADES:-disabled} min_baseline_win_rate_delta=${MIN_BASELINE_WIN_RATE_DELTA:-disabled} \
min_baseline_net_pnl_delta=${MIN_BASELINE_NET_PNL_DELTA:-disabled} min_baseline_avg_pnl_delta=${MIN_BASELINE_AVG_PNL_DELTA:-disabled} require_live_trial_ready=${REQUIRE_LIVE_TRIAL_READY} \
record_rollout_proof=${RECORD_ROLLOUT_PROOF} strategy_id=${STRATEGY_ID:-derived}"
  if [ -n "$SOAK_OUTPUT_FILE" ]; then
    if ! command -v jq >/dev/null 2>&1; then
      fail "jq is required to write clean SOAK_OUTPUT_FILE artifacts"
    fi
    local raw_output
    local artifact_tmp
    raw_output="$(mktemp "${TMPDIR:-/tmp}/neuratrade-scalping-soak-output.XXXXXX")"
    artifact_tmp="${SOAK_OUTPUT_FILE}.tmp"
    mkdir -p "$(dirname "$SOAK_OUTPUT_FILE")"
    set +e
    "$SOAK_BIN" "${args[@]}" | tee "$raw_output" | tee -a "$LOG_FILE"
    local soak_status=${PIPESTATUS[0]}
    set -e
    if ! jq -s 'map(select(.db_path? != null and .result? != null)) | if length == 1 then .[0] else error("expected exactly one soak result JSON document, got \(length)") end' "$raw_output" >"$artifact_tmp"; then
      rm -f "$raw_output" "$artifact_tmp"
      if [ "$soak_status" -ne 0 ]; then
        fail "scalping soak binary failed and no clean soak result artifact could be extracted"
      fi
      fail "failed to extract clean soak result artifact from stdout"
    fi
    mv "$artifact_tmp" "$SOAK_OUTPUT_FILE"
    rm -f "$raw_output"
    log "wrote clean soak result artifact to ${SOAK_OUTPUT_FILE}"
    if [ "$soak_status" -ne 0 ]; then
      fail "scalping soak binary failed; retained clean soak result artifact at ${SOAK_OUTPUT_FILE}"
    fi
  else
    "$SOAK_BIN" "${args[@]}" | tee -a "$LOG_FILE"
  fi
  log "${GREEN}[OK]${NC} scalping paper soak complete"
}

main() {
  case "${1:-run}" in
    run)
      run_soak
      ;;
    help | -h | --help)
      usage
      ;;
    *)
      usage
      fail "unknown command: $1"
      ;;
  esac
}

main "$@"
