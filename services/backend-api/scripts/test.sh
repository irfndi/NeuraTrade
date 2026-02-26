#!/usr/bin/env bash

# Native unified test runner for NeuraTrade (no Docker)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BACKEND_ROOT="$(dirname "$SCRIPT_DIR")"
REPO_ROOT="$(dirname "$(dirname "$BACKEND_ROOT")")"
LOG_FILE="${BACKEND_ROOT}/logs/testing.log"
BASE_URL="${BASE_URL:-http://127.0.0.1:${BACKEND_HOST_PORT:-8080}}"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log() {
  mkdir -p "$(dirname "$LOG_FILE")"
  echo -e "[$(date '+%Y-%m-%d %H:%M:%S')] ${BLUE}[TEST]${NC} $1" | tee -a "$LOG_FILE"
}

pass() {
  log "${GREEN}[PASS]${NC} $1"
}

warn() {
  log "${YELLOW}[WARN]${NC} $1"
}

fail() {
  log "${RED}[FAIL]${NC} $1"
}

check_tools() {
  local missing=()
  for tool in go curl; do
    command -v "$tool" >/dev/null 2>&1 || missing+=("$tool")
  done

  if [ ${#missing[@]} -gt 0 ]; then
    fail "Missing required tools: ${missing[*]}"
    return 1
  fi

  pass "Core tools available"
}

backend_unit() {
  log "Running backend unit tests"
  local test_home
  test_home="$(mktemp -d /tmp/neuratrade-test-home.XXXXXX)"
  (
    cd "$BACKEND_ROOT"
    NEURATRADE_HOME="$test_home" ADMIN_API_KEY= go test -v -race -timeout=15m ./cmd/... ./internal/... ./pkg/...
  )
  rm -rf "$test_home"
  pass "Backend unit tests passed"
}

backend_integration() {
  log "Running backend integration tests (host/native)"
  local test_home
  test_home="$(mktemp -d /tmp/neuratrade-test-home.XXXXXX)"
  (
    cd "$BACKEND_ROOT"
    NEURATRADE_HOME="$test_home" \
      ADMIN_API_KEY= \
    DATABASE_DRIVER="${DATABASE_DRIVER:-sqlite}" \
      SQLITE_PATH="${SQLITE_PATH:-/tmp/neuratrade-test.db}" \
      go test -v -timeout=10m ./test/integration/...
  )
  rm -rf "$test_home"
  pass "Backend integration tests passed"
}

backend_e2e() {
  log "Running backend e2e tests (host/native)"
  local test_home
  test_home="$(mktemp -d /tmp/neuratrade-test-home.XXXXXX)"
  (
    cd "$BACKEND_ROOT"
    NEURATRADE_HOME="$test_home" \
      ADMIN_API_KEY= \
    DATABASE_DRIVER="${DATABASE_DRIVER:-sqlite}" \
      SQLITE_PATH="${SQLITE_PATH:-/tmp/neuratrade-test.db}" \
      go test -v -timeout=10m ./test/e2e/...
  )
  rm -rf "$test_home"
  pass "Backend e2e tests passed"
}

frontend_tests() {
  if [ ! -d "$REPO_ROOT/services/telegram-service" ]; then
    warn "telegram-service folder not found, skipping frontend tests"
    return 0
  fi

  if ! command -v bun >/dev/null 2>&1; then
    warn "bun not available, skipping frontend tests"
    return 0
  fi

  log "Running telegram-service tests"
  (
    cd "$REPO_ROOT/services/telegram-service"
    bun install --frozen-lockfile
    bun test
  )
  pass "Frontend tests passed"
}

test_health() {
  local ok=0

  if curl -fsS "${BASE_URL}/health" >/dev/null 2>&1; then
    pass "Health endpoint reachable at ${BASE_URL}/health"
    ok=$((ok + 1))
  else
    warn "Health endpoint not reachable at ${BASE_URL}/health"
  fi

  if curl -fsS "${BASE_URL}/api/market-data" >/dev/null 2>&1; then
    pass "Market data endpoint reachable"
    ok=$((ok + 1))
  else
    warn "Market data endpoint not reachable"
  fi

  [ "$ok" -gt 0 ]
}

show_status() {
  bash "$SCRIPT_DIR/startup-orchestrator.sh" status || true
}

start_stack() {
  bash "$SCRIPT_DIR/startup-orchestrator.sh" start
}

stop_stack() {
  bash "$SCRIPT_DIR/startup-orchestrator.sh" stop
}

run_all() {
  check_tools
  backend_unit
  backend_integration
  backend_e2e
  frontend_tests
}

usage() {
  cat <<USAGE
Usage: $0 [test|backend|frontend|health|start|status|cleanup|help]

Commands:
  test      Run backend + frontend test suites (default)
  backend   Run backend unit + integration + e2e tests
  frontend  Run telegram-service tests
  health    Run endpoint health smoke checks
  start     Start native services via startup-orchestrator
  status    Show runtime status
  cleanup   Stop native services
  help      Show this help message
USAGE
}

main() {
  case "${1:-test}" in
    test)
      run_all
      ;;
    backend)
      check_tools
      backend_unit
      backend_integration
      backend_e2e
      ;;
    frontend)
      frontend_tests
      ;;
    health)
      test_health
      ;;
    start)
      start_stack
      ;;
    status)
      show_status
      ;;
    cleanup)
      stop_stack
      ;;
    help|-h|--help)
      usage
      ;;
    *)
      usage
      exit 1
      ;;
  esac
}

main "$@"
