#!/usr/bin/env bash

# Run the real LLM scalping decision probe against public market/order-book
# signals without placing orders.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BACKEND_ROOT="$(dirname "$SCRIPT_DIR")"
REPO_ROOT="$(dirname "$(dirname "$BACKEND_ROOT")")"
NEURATRADE_HOME="${NEURATRADE_HOME:-$HOME/.neuratrade}"
LOG_DIR="${NEURATRADE_HOME}/logs"
LOG_FILE="${LOG_DIR}/ai-scalping-probe.log"
DATA_DIR="${NEURATRADE_HOME}/data"

PROBE_BIN="${PROBE_BIN:-${REPO_ROOT}/bin/neuratrade-server}"
PROVIDER="${PROVIDER-deepseek}"
MODEL="${MODEL-}"
BASE_URL="${BASE_URL-}"
EXCHANGE="${EXCHANGE:-bitget}"
CYCLES="${CYCLES:-3}"
INTERVAL_MS="${INTERVAL_MS:-1000}"
TIMEOUT_SECONDS="${TIMEOUT_SECONDS:-75}"
CAPITAL="${CAPITAL:-48}"
MIN_SIGNAL_QUALITY="${MIN_SIGNAL_QUALITY-1}"
MIN_ACTIONABLE_CYCLES="${MIN_ACTIONABLE_CYCLES-1}"
MAX_HOLD_RATIO="${MAX_HOLD_RATIO-0.745}"
MIN_PAPER_TRADES="${MIN_PAPER_TRADES-1}"
MIN_PAPER_NET_PNL="${MIN_PAPER_NET_PNL-0}"
MIN_PAPER_AVG_NET_PNL="${MIN_PAPER_AVG_NET_PNL-0}"
MIN_PAPER_PROFIT_FACTOR="${MIN_PAPER_PROFIT_FACTOR-1}"
MAX_PAPER_DRAWDOWN="${MAX_PAPER_DRAWDOWN-}"
MAX_PAPER_DRAWDOWN_PCT="${MAX_PAPER_DRAWDOWN_PCT-0.01}"
MAX_REASONING_DIAGNOSTICS="${MAX_REASONING_DIAGNOSTICS-0}"
OUTPUT_JSON="${OUTPUT_JSON:-true}"
PROBE_OUTPUT_FILE="${PROBE_OUTPUT_FILE:-}"

BLUE='\033[0;34m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

log() {
  mkdir -p "$LOG_DIR"
  echo -e "[$(date '+%Y-%m-%d %H:%M:%S')] ${BLUE}[AI-SCALPING-PROBE]${NC} $1" | tee -a "$LOG_FILE"
}

fail() {
  log "${RED}[FAIL]${NC} $1"
  exit 1
}

usage() {
  cat <<USAGE
Usage: $(basename "$0") [run|help]

Runs the real LLM scalping decision contract against public exchange signals
with AutoExecute disabled. It does not place real exchange orders.

Environment:
  PROBE_BIN       Path to neuratrade-server binary (default: ${PROBE_BIN})
  PROVIDER        Provider override; empty uses runtime config (default: ${PROVIDER:-runtime config})
  MODEL           Model override; empty uses provider/runtime default (default: ${MODEL:-runtime default})
  BASE_URL        Provider base URL override; empty uses runtime default (default: ${BASE_URL:-runtime default})
  EXCHANGE        Public exchange to probe (default: ${EXCHANGE})
  CYCLES          Number of LLM probe cycles (default: ${CYCLES})
  INTERVAL_MS     Delay between cycles in ms (default: ${INTERVAL_MS})
  TIMEOUT_SECONDS Overall timeout in seconds (default: ${TIMEOUT_SECONDS})
  CAPITAL         Paper wallet basis in USDT (default: ${CAPITAL})
  MIN_SIGNAL_QUALITY Minimum signal quality coverage; empty disables (default: ${MIN_SIGNAL_QUALITY})
  MIN_ACTIONABLE_CYCLES Minimum buy/sell cycles; empty disables (default: ${MIN_ACTIONABLE_CYCLES})
  MAX_HOLD_RATIO  Maximum hold cycles / completed cycles; empty disables (default: ${MAX_HOLD_RATIO})
  MIN_PAPER_TRADES Minimum simulated paper trades; empty disables (default: ${MIN_PAPER_TRADES})
  MIN_PAPER_NET_PNL Minimum aggregate paper net PnL; empty disables (default: ${MIN_PAPER_NET_PNL})
  MIN_PAPER_AVG_NET_PNL Minimum avg paper net PnL/trade; empty disables (default: ${MIN_PAPER_AVG_NET_PNL})
  MIN_PAPER_PROFIT_FACTOR Minimum paper profit factor; empty disables (default: ${MIN_PAPER_PROFIT_FACTOR})
  MAX_PAPER_DRAWDOWN Maximum absolute paper drawdown; empty disables (default: ${MAX_PAPER_DRAWDOWN:-disabled})
  MAX_PAPER_DRAWDOWN_PCT Maximum paper drawdown / CAPITAL; empty disables (default: ${MAX_PAPER_DRAWDOWN_PCT})
  MAX_REASONING_DIAGNOSTICS Maximum reasoning diagnostics; empty disables (default: ${MAX_REASONING_DIAGNOSTICS})
  OUTPUT_JSON      true/false JSON output (default: ${OUTPUT_JSON})
  PROBE_OUTPUT_FILE Optional path for clean stdout artifact, usually JSON; empty disables (default: ${PROBE_OUTPUT_FILE:-disabled})

Examples:
  make build
  bash services/backend-api/scripts/ai-scalping-probe.sh run
  PROBE_OUTPUT_FILE="\$HOME/.neuratrade/data/ai-scalping-probe-latest.json" bash services/backend-api/scripts/ai-scalping-probe.sh run
  PROVIDER= CYCLES=12 MAX_HOLD_RATIO=0.745 bash services/backend-api/scripts/ai-scalping-probe.sh run
USAGE
}

ensure_binary() {
  if [ -x "$PROBE_BIN" ]; then
    return
  fi

  log "${YELLOW}[WARN]${NC} ${PROBE_BIN} not found; building it with make build"
  (cd "$REPO_ROOT" && make build)
  [ -x "$PROBE_BIN" ] || fail "probe binary still missing after build: ${PROBE_BIN}"
}

append_optional_arg() {
  local flag="$1"
  local value="$2"
  if [ -n "$value" ]; then
    args+=("$flag" "$value")
  fi
}

run_probe() {
  ensure_binary
  mkdir -p "$DATA_DIR"

  local args=(
    "ai"
    "scalping-probe"
    "--exchange" "$EXCHANGE"
    "--cycles" "$CYCLES"
    "--interval-ms" "$INTERVAL_MS"
    "--timeout-seconds" "$TIMEOUT_SECONDS"
    "--capital" "$CAPITAL"
  )
  if [ "$OUTPUT_JSON" = "true" ] || [ "$OUTPUT_JSON" = "1" ]; then
    args+=("--json")
  fi
  append_optional_arg "--provider" "$PROVIDER"
  append_optional_arg "--model" "$MODEL"
  append_optional_arg "--base-url" "$BASE_URL"
  if [ -n "$MIN_SIGNAL_QUALITY" ]; then
    args+=("--min-signal-quality" "$MIN_SIGNAL_QUALITY")
  else
    args+=("--min-signal-quality" "0")
  fi
  append_optional_arg "--min-actionable-cycles" "$MIN_ACTIONABLE_CYCLES"
  append_optional_arg "--max-hold-ratio" "$MAX_HOLD_RATIO"
  append_optional_arg "--min-paper-trades" "$MIN_PAPER_TRADES"
  append_optional_arg "--min-paper-net-pnl" "$MIN_PAPER_NET_PNL"
  append_optional_arg "--min-paper-avg-net-pnl" "$MIN_PAPER_AVG_NET_PNL"
  append_optional_arg "--min-paper-profit-factor" "$MIN_PAPER_PROFIT_FACTOR"
  append_optional_arg "--max-paper-drawdown" "$MAX_PAPER_DRAWDOWN"
  append_optional_arg "--max-paper-drawdown-pct" "$MAX_PAPER_DRAWDOWN_PCT"
  args+=("--max-reasoning-diagnostics" "$MAX_REASONING_DIAGNOSTICS")

  log "running real LLM scalping probe provider=${PROVIDER:-runtime-config} exchange=${EXCHANGE} cycles=${CYCLES} interval_ms=${INTERVAL_MS} \
min_signal_quality=${MIN_SIGNAL_QUALITY:-disabled} min_actionable_cycles=${MIN_ACTIONABLE_CYCLES:-disabled} max_hold_ratio=${MAX_HOLD_RATIO:-disabled} \
min_paper_trades=${MIN_PAPER_TRADES:-disabled} min_paper_net_pnl=${MIN_PAPER_NET_PNL:-disabled} min_paper_avg_net_pnl=${MIN_PAPER_AVG_NET_PNL:-disabled} \
min_paper_profit_factor=${MIN_PAPER_PROFIT_FACTOR:-disabled} max_paper_drawdown=${MAX_PAPER_DRAWDOWN:-disabled} max_paper_drawdown_pct=${MAX_PAPER_DRAWDOWN_PCT:-disabled} \
max_reasoning_diagnostics=${MAX_REASONING_DIAGNOSTICS:-disabled}"
  if [ -n "$PROBE_OUTPUT_FILE" ]; then
    mkdir -p "$(dirname "$PROBE_OUTPUT_FILE")"
    "$PROBE_BIN" "${args[@]}" | tee "$PROBE_OUTPUT_FILE" | tee -a "$LOG_FILE"
    log "wrote clean probe stdout artifact to ${PROBE_OUTPUT_FILE}"
  else
    "$PROBE_BIN" "${args[@]}" | tee -a "$LOG_FILE"
  fi
  log "${GREEN}[OK]${NC} AI scalping probe complete"
}

main() {
  case "${1:-run}" in
    run)
      run_probe
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
