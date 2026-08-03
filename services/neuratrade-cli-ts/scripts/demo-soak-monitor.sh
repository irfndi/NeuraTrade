#!/usr/bin/env bash

# Demo-soak monitor for the Bitget PAPTRADING grid soak (neuratrade-demo-soak).
# Checks the pm2 process is alive, prints the accumulated live-fill evidence,
# and evaluates the demo-readiness gate. Intended to run on a schedule
# (launchd/cron) logging to $NEURATRADE_HOME/logs/demo-soak-monitor.log.
#
# The gate needs >= 50 live fills over >= 7 days before it can pass; until
# then this script is a progress reporter, not a verdict.

set -euo pipefail

# launchd runs with a minimal PATH; resolve the toolchain explicitly.
export PATH="/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:$PATH"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLI_TS_DIR="$(dirname "$SCRIPT_DIR")"
NEURATRADE_HOME="${NEURATRADE_HOME:-$HOME/.neuratrade}"
LOG_DIR="${NEURATRADE_HOME}/logs"
LOG_FILE="${LOG_DIR}/demo-soak-monitor.log"
PM2_NAME="${PM2_NAME:-neuratrade-demo-soak}"
DB_PATH="${DB_PATH:-$NEURATRADE_HOME/data/neuratrade.db}"
MIN_TRADES="${MIN_TRADES:-50}"
MIN_DURATION_DAYS="${MIN_DURATION_DAYS:-7}"

log() {
  mkdir -p "$LOG_DIR"
  echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1" | tee -a "$LOG_FILE"
}

# 1. Process liveness.
if ! command -v pm2 >/dev/null 2>&1; then
  log "[WARN] pm2 not found; cannot verify soak process"
else
  if pm2 jlist 2>/dev/null | grep -q "\"name\":\"${PM2_NAME}\""; then
    STATUS=$(pm2 jlist 2>/dev/null | grep -o "\"name\":\"${PM2_NAME}\"" | tail -1 >/dev/null; pm2 jlist 2>/dev/null | python3 -c "import json,sys; [print(p['pm2_env']['status']) for p in json.load(sys.stdin) if p.get('name')=='${PM2_NAME}']" 2>/dev/null | head -1)
    log "[INFO] pm2 ${PM2_NAME}: ${STATUS:-unknown}"
  else
    log "[WARN] pm2 process ${PM2_NAME} not found"
  fi
fi

# 2. Live-fill evidence from the persisted grid trades.
if command -v sqlite3 >/dev/null 2>&1 && [ -f "$DB_PATH" ]; then
  LIVE_FILLS=$(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM grid_paper_trades WHERE exchange='bitget-futures' AND symbol='BTC/USDT:USDT' AND timeframe='15m' AND fill_source='live';" 2>/dev/null || echo "0")
  TOTAL_TRADES=$(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM grid_paper_trades WHERE exchange='bitget-futures' AND symbol='BTC/USDT:USDT' AND timeframe='15m';" 2>/dev/null || echo "0")
  log "[INFO] grid trades: ${TOTAL_TRADES} total, ${LIVE_FILLS} live fills"
else
  log "[WARN] sqlite3 or DB not found at ${DB_PATH}"
fi

# 3. Evaluate the demo-readiness gate.
if [ -f "$CLI_TS_DIR/index.ts" ]; then
  cd "$CLI_TS_DIR"
  REPORT=$(bun run index.ts scalp demo-readiness \
    --exchange bitget-futures --symbol BTC/USDT:USDT --timeframe 15m \
    --min-trades "$MIN_TRADES" --min-duration-days "$MIN_DURATION_DAYS" 2>&1 | grep -E '^\{"status' | head -1 || true)
  if [ -n "$REPORT" ]; then
    log "[INFO] demo-readiness: $REPORT"
  else
    log "[WARN] demo-readiness produced no parseable report"
  fi
else
  log "[WARN] CLI entrypoint not found at ${CLI_TS_DIR}/index.ts"
fi