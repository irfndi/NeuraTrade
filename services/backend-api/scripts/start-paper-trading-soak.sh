#!/usr/bin/env bash

# NeuraTrade 30+ day paper-trading soak for real-money readiness.
# Configures the system to paper-trade the same 5 symbols the readiness
# backtest covers (BTC/ETH/SOL/BNB/XRP on binance), fetches 30 days of
# real OHLCV candles, and starts the gateway in paper mode.
#
# After 30+ days of running, evaluate real-money readiness with:
#   ./scripts/verify-real-money-readiness.sh
#
# That script returns exit 0 only when paper trading NetPnL > 0 (the
# hard escalation gate, per the readiness criteria enforced by
# services.ReadinessManifestGenerator).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
NEURATRADE_HOME="${NEURATRADE_HOME:-$HOME/.neuratrade}"

# Backtest universe (must match READINESS_ASSESSMENT readiness test config).
PAPER_SYMBOLS="${NEURATRADE_PAPER_SYMBOLS:-BTC/USDT,ETH/USDT,SOL/USDT,BNB/USDT,XRP/USDT}"
PAPER_EXCHANGE="${NEURATRADE_PAPER_EXCHANGE:-binance}"
PAPER_DAYS="${NEURATRADE_PAPER_DAYS:-30}"
TIMEFRAMES="${NEURATRADE_PAPER_TIMEFRAMES:-5m,15m,1h,4h,1d}"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log() { echo -e "[$(date '+%Y-%m-%d %H:%M:%S')] $1"; }

log "${BLUE}=== NeuraTrade Paper-Trading Soak ===${NC}"
log "Home:    ${NEURATRADE_HOME}"
log "Symbols: ${PAPER_SYMBOLS}"
log "Exchange: ${PAPER_EXCHANGE}"
log "Days: ${PAPER_DAYS} (need >= 30 for readiness manifest to be Ready)"
log "Timeframes: ${TIMEFRAMES}"

if [[ "${PAPER_DAYS}" -lt 30 ]]; then
  log "${YELLOW}WARNING: paper trading requires >= 30 days of continuous validation.${NC}"
  log "${YELLOW}         current=${PAPER_DAYS}; readiness manifest will NOT be Ready until 30+ days.${NC}"
fi

# 1. Force operational mode to paper (runtime override consumed by trading_mode.go)
export FEATURES_PAPER_TRADING=true
export FEATURE_PAPER_TRADING=true
# 2. Constrain live scalping to the backtest universe (consumed by ai_scalping.go)
export NEURATRADE_SCALPING_SYMBOLS="${PAPER_SYMBOLS}"
log "${GREEN}Exported FEATURES_PAPER_TRADING=true and NEURATRADE_SCALPING_SYMBOLS=${PAPER_SYMBOLS}${NC}"

mkdir -p "${NEURATRADE_HOME}/data" "${NEURATRADE_HOME}/logs" "${NEURATRADE_HOME}/pids"

# 3. Build the fetch-real-candles binary (binaries may not be built yet)
cd "${REPO_ROOT}"
log "Building fetch-real-candles binary..."
(cd services/backend-api && go build -o ../../bin/fetch-real-candles ./cmd/fetch-real-candles/)

# 4. Bulk-load 30 days of real OHLCV for the paper trading universe
log "Fetching ${PAPER_DAYS} days of ${PAPER_EXCHANGE} OHLCV for ${PAPER_SYMBOLS}..."
./bin/fetch-real-candles \
  -exchange "${PAPER_EXCHANGE}" \
  -symbols "${PAPER_SYMBOLS}" \
  -timeframes "${TIMEFRAMES}" \
  -days "${PAPER_DAYS}"

log "${GREEN}OHLCV data loaded.${NC}"

# 5. Build the gateway binary
log "Building neuratrade gateway binary..."
make build >/dev/null

# 6. Start the gateway in the background. The gateway inherits the
# NEURATRADE_SCALPING_SYMBOLS and FEATURES_PAPER_TRADING env vars we set above.
log "Starting gateway (in background; logs at ${NEURATRADE_HOME}/logs/gateway.log)..."
nohup ./bin/neuratrade gateway start >>"${NEURATRADE_HOME}/logs/gateway.log" 2>&1 &
GATEWAY_PID=$!
log "Gateway PID: ${GATEWAY_PID}"

# 7. Wait for the backend to be healthy
log "Waiting for backend to be healthy (up to 150s)..."
for i in $(seq 1 30); do
  if curl -fsS "http://localhost:${SERVER_PORT:-8080}/health" >/dev/null 2>&1; then
    log "${GREEN}Backend is healthy.${NC}"
    break
  fi
  if [[ "$i" -eq 30 ]]; then
    log "${RED}Backend failed to become healthy in time.${NC}"
    log "${RED}Check logs: tail -f ${NEURATRADE_HOME}/logs/gateway.log${NC}"
    exit 1
  fi
  sleep 5
done

# 8. Set operational mode to paper for the default chat (if not already)
log "Setting operational mode to paper for the default chat..."
DEFAULT_CHAT_ID="${NEURATRADE_DEFAULT_CHAT_ID:-default}"
curl -fsS -X POST \
  -H "Content-Type: application/json" \
  -H "X-API-Key: ${ADMIN_API_KEY:-test-admin-key}" \
  -d '{"mode": "paper", "changed_by": "start-paper-trading-soak"}' \
  "http://localhost:${SERVER_PORT:-8080}/api/v1/telegram/internal/mode/${DEFAULT_CHAT_ID}" \
  >/dev/null 2>&1 || log "${YELLOW}Note: mode API may require existing chat — this is non-fatal; the env var will still force paper.${NC}"

log ""
log "${GREEN}=== Paper-trading soak started ===${NC}"
log "Symbols: ${PAPER_SYMBOLS}"
log "Mode: paper (env override: FEATURES_PAPER_TRADING=true)"
log ""
log "${YELLOW}>>> Run for at least 30 days, then verify readiness with:${NC}"
log "    ${REPO_ROOT}/services/backend-api/scripts/verify-real-money-readiness.sh"
log ""
log "Continuous monitoring:"
log "    tail -f ${NEURATRADE_HOME}/logs/gateway.log"
log "    curl -s http://localhost:8080/api/v1/readiness/paper-trading | jq ."
