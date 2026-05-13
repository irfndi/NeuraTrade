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
CAPITAL="${CAPITAL:-48}"
FEE_RATE="${FEE_RATE:-0.0006}"
REQUIRE_TRADES="${REQUIRE_TRADES:-true}"
MIN_TRADES="${MIN_TRADES-1}"
MIN_WIN_RATE="${MIN_WIN_RATE-0.123}"
MIN_NET_PNL="${MIN_NET_PNL-0}"
MIN_AVG_NET_PNL="${MIN_AVG_NET_PNL-0}"
MIN_SIGNAL_QUALITY_COVERAGE="${MIN_SIGNAL_QUALITY_COVERAGE-1}"
MAX_DRAWDOWN="${MAX_DRAWDOWN-}"
MAX_DRAWDOWN_PCT="${MAX_DRAWDOWN_PCT-0.01}"
MAX_AI_PROVIDER_DEGRADED_CYCLES="${MAX_AI_PROVIDER_DEGRADED_CYCLES-0}"
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
  CAPITAL         Initial paper capital in USDT (default: ${CAPITAL})
  FEE_RATE        Paper simulator fee rate (default: ${FEE_RATE})
  REQUIRE_TRADES  true/false; fail when no paper trades are produced (default: ${REQUIRE_TRADES})
  MIN_TRADES      Minimum closed paper trades required (default: ${MIN_TRADES})
  MIN_WIN_RATE    Minimum win rate required as decimal; empty disables (default: ${MIN_WIN_RATE})
  MIN_NET_PNL     Minimum net PnL required; empty disables (default: ${MIN_NET_PNL})
  MIN_AVG_NET_PNL Minimum avg net PnL per trade required; empty disables (default: ${MIN_AVG_NET_PNL})
  MIN_SIGNAL_QUALITY_COVERAGE Minimum signal quality coverage; empty disables (default: ${MIN_SIGNAL_QUALITY_COVERAGE})
  MAX_DRAWDOWN    Maximum absolute drawdown; empty disables (default: ${MAX_DRAWDOWN:-disabled})
  MAX_DRAWDOWN_PCT Maximum drawdown as fraction of baseline balance; empty disables (default: ${MAX_DRAWDOWN_PCT})
  MAX_AI_PROVIDER_DEGRADED_CYCLES Maximum AI provider degraded cycles; empty disables (default: ${MAX_AI_PROVIDER_DEGRADED_CYCLES})
  SOAK_CHAT_ID    Chat id label for persisted soak telemetry (default: ${SOAK_CHAT_ID})
  SOAK_ORDER_PREFIX Order prefix label for persisted soak telemetry (default: ${SOAK_ORDER_PREFIX})
  SOAK_OUTPUT_FILE Optional path for clean stdout artifact, usually JSON; empty disables (default: ${SOAK_OUTPUT_FILE:-disabled})

Examples:
  make build
  bash services/backend-api/scripts/scalping-soak.sh run
  SOAK_OUTPUT_FILE="\$HOME/.neuratrade/data/scalping-soak-latest.json" bash services/backend-api/scripts/scalping-soak.sh run
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
  if [ -n "$MAX_DRAWDOWN" ]; then
    args+=("--max-drawdown" "$MAX_DRAWDOWN")
  fi
  if [ -n "$MAX_DRAWDOWN_PCT" ]; then
    args+=("--max-drawdown-pct" "$MAX_DRAWDOWN_PCT")
  fi
  if [ -n "$MAX_AI_PROVIDER_DEGRADED_CYCLES" ]; then
    args+=("--max-ai-provider-degraded-cycles" "$MAX_AI_PROVIDER_DEGRADED_CYCLES")
  fi
  if [ "$TIMEOUT_SECONDS" != "0" ]; then
    args+=("--timeout-seconds" "$TIMEOUT_SECONDS")
  fi
  if [ "$REQUIRE_TRADES" = "true" ] || [ "$REQUIRE_TRADES" = "1" ]; then
    args+=("--require-trades")
  fi

  log "running no-order scalping paper soak exchange=${EXCHANGE} cycles=${CYCLES} interval_ms=${INTERVAL_MS} db=${SOAK_DB_PATH} \
min_trades=${MIN_TRADES:-disabled} min_win_rate=${MIN_WIN_RATE:-disabled} min_net_pnl=${MIN_NET_PNL:-disabled} min_avg_net_pnl=${MIN_AVG_NET_PNL:-disabled} \
min_signal_quality_coverage=${MIN_SIGNAL_QUALITY_COVERAGE:-disabled} max_drawdown=${MAX_DRAWDOWN:-disabled} max_drawdown_pct=${MAX_DRAWDOWN_PCT:-disabled} \
max_ai_provider_degraded_cycles=${MAX_AI_PROVIDER_DEGRADED_CYCLES:-disabled}"
  if [ -n "$SOAK_OUTPUT_FILE" ]; then
    mkdir -p "$(dirname "$SOAK_OUTPUT_FILE")"
    "$SOAK_BIN" "${args[@]}" | tee "$SOAK_OUTPUT_FILE" | tee -a "$LOG_FILE"
    log "wrote clean soak stdout artifact to ${SOAK_OUTPUT_FILE}"
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
