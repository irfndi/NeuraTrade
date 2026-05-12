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
SOAK_DB_PATH="${SOAK_DB_PATH:-${NEURATRADE_HOME}/data/scalping-soak.db}"
SOAK_BIN="${SOAK_BIN:-${REPO_ROOT}/bin/neuratrade-scalping-soak}"

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

Examples:
  make build
  bash services/backend-api/scripts/scalping-soak.sh run
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
    "--capital" "$CAPITAL"
    "--fee-rate" "$FEE_RATE"
  )
  if [ "$TIMEOUT_SECONDS" != "0" ]; then
    args+=("--timeout-seconds" "$TIMEOUT_SECONDS")
  fi
  if [ "$REQUIRE_TRADES" = "true" ] || [ "$REQUIRE_TRADES" = "1" ]; then
    args+=("--require-trades")
  fi

  log "running no-order scalping paper soak exchange=${EXCHANGE} cycles=${CYCLES} interval_ms=${INTERVAL_MS} db=${SOAK_DB_PATH}"
  "$SOAK_BIN" "${args[@]}" | tee -a "$LOG_FILE"
  log "${GREEN}[OK]${NC} scalping paper soak complete"
}

main() {
  case "${1:-run}" in
    run)
      run_soak
      ;;
    help|-h|--help)
      usage
      ;;
    *)
      usage
      fail "unknown command: $1"
      ;;
  esac
}

main "$@"
