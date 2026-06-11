#!/usr/bin/env bash

# NeuraTrade real-money readiness verification.
# Runs the paper-readiness CLI against the live system and checks that:
#   - ContinuousValidationHours >= 720 (30 days)
#   - ClosedTrades >= 10
#   - StrategyCount >= 1
#   - RiskLimitsEnforced == true
#   - BacktestComparisonVerified == true
#   - NetPnL > 0  (the hard escalation gate added in Wave 4.2)
#
# Exits 0 only when ALL gates are met.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
NEURATRADE_HOME="${NEURATRADE_HOME:-$HOME/.neuratrade}"
SERVER_PORT="${SERVER_PORT:-8080}"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log() { echo -e "[$(date '+%Y-%m-%d %H:%M:%S')] $1"; }

cd "${REPO_ROOT}"

# Build the paper-readiness binary
log "Building paper-readiness binary..."
(cd services/backend-api && go build -o ../../bin/paper-readiness ./cmd/paper-readiness/)

START="${NEURATRADE_READINESS_START:-}"
END="${NEURATRADE_READINESS_END:-}"
EXTRA_ARGS=()
if [[ -n "${START}" ]]; then EXTRA_ARGS+=(--start "${START}"); fi
if [[ -n "${END}" ]]; then EXTRA_ARGS+=(--end "${END}"); fi

log "Querying paper trading readiness manifest..."
MANIFEST_OUTPUT="$(./bin/paper-readiness --json "${EXTRA_ARGS[@]}")"
echo "${MANIFEST_OUTPUT}" | jq . 2>/dev/null || echo "${MANIFEST_OUTPUT}"

READY="$(echo "${MANIFEST_OUTPUT}" | jq -r '.acceptance.ready // empty' 2>/dev/null || true)"
NET_PNL="$(echo "${MANIFEST_OUTPUT}" | jq -r '.net_pnl // empty' 2>/dev/null || true)"
HOURS="$(echo "${MANIFEST_OUTPUT}" | jq -r '.continuous_validation_hours // empty' 2>/dev/null || true)"
CLOSED_TRADES="$(echo "${MANIFEST_OUTPUT}" | jq -r '.closed_trades // empty' 2>/dev/null || true)"

echo ""
log "=== Summary ==="
log "  Continuous validation hours: ${HOURS:-<unknown>}"
log "  Closed trades:               ${CLOSED_TRADES:-<unknown>}"
log "  Net PnL:                     ${NET_PNL:-<unknown>}"
log "  Ready:                       ${READY:-<unknown>}"

if [[ "${READY}" == "true" ]]; then
  echo ""
  log "${GREEN}✓ Real-money escalation criteria MET.${NC}"
  log "${GREEN}  Paper trading is profitable. Set the chat to live mode to escalate:${NC}"
  log "    curl -X POST -H 'X-API-Key: \${ADMIN_API_KEY}' \\"
  log "      -d '{\"mode\":\"live\"}' \\"
  log "      http://localhost:${SERVER_PORT}/api/v1/telegram/internal/mode/\${CHAT_ID}"
  exit 0
fi

echo ""
log "${YELLOW}✗ Real-money escalation criteria NOT YET met.${NC}"
log "${YELLOW}  Review the manifest failures above and continue paper trading.${NC}"
exit 1
