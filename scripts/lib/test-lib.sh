#!/bin/bash
#
# NeuraTrade Test Library
# Shared functions and utilities for test scripts
#
# Usage: source scripts/lib/test-lib.sh
#

# Exit on error, undefined variables, and pipe failures
set -euo pipefail

# ============================================================================
# COLORS
# ============================================================================
readonly RED='\033[0;31m'
readonly GREEN='\033[0;32m'
readonly YELLOW='\033[1;33m'
readonly BLUE='\033[0;34m'
readonly CYAN='\033[0;36m'
readonly NC='\033[0m'

# ============================================================================
# CONFIGURATION
# ============================================================================
readonly CONFIG_FILE="${CONFIG_FILE:-$HOME/.neuratrade/config.json}"
readonly DB_PATH="${DB_PATH:-$HOME/.neuratrade/data/neuratrade.db}"
readonly BACKEND_URL="${BACKEND_URL:-http://localhost:8080}"
readonly CCXT_URL="${CCXT_URL:-http://localhost:3001}"
readonly TELEGRAM_URL="${TELEGRAM_URL:-http://localhost:3002}"

# ============================================================================
# TEST COUNTERS
# ============================================================================
TESTS_PASSED=0
TESTS_FAILED=0
TESTS_SKIPPED=0

# ============================================================================
# PRINT FUNCTIONS
# ============================================================================
nt_info() { echo -e "${BLUE}[INFO]${NC} $*"; }
nt_success() { echo -e "${GREEN}[PASS]${NC} $*"; }
nt_warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }
nt_error() { echo -e "${RED}[FAIL]${NC} $*"; }
nt_step() { echo -e "${CYAN}[STEP]${NC} $*"; }
nt_header() {
  echo -e "\n${CYAN}═══════════════════════════════════════════════════════════${NC}"
  echo -e "${CYAN}$*${NC}"
  echo -e "${CYAN}═══════════════════════════════════════════════════════════${NC}\n"
}

# ============================================================================
# TEST RESULT TRACKING
# ============================================================================
nt_test_result() {
  local name="$1"
  local result="$2"
  local message="${3:-}"

  case "$result" in
    pass)
      nt_success "$name"
      TESTS_PASSED=$((TESTS_PASSED + 1))
      ;;
    skip)
      nt_warn "$name (skipped: $message)"
      TESTS_SKIPPED=$((TESTS_SKIPPED + 1))
      ;;
    *)
      nt_error "$name"
      [[ -n "$message" ]] && echo "   Details: $message"
      TESTS_FAILED=$((TESTS_FAILED + 1))
      ;;
  esac
}

# ============================================================================
# CONFIGURATION CHECKS
# ============================================================================
nt_check_config_exists() {
  if [[ -f "$CONFIG_FILE" ]]; then
    nt_test_result "Config file exists" "pass"
    return 0
  else
    nt_test_result "Config file exists" "fail" "$CONFIG_FILE not found"
    return 1
  fi
}

nt_get_config_size() {
  if [[ -f "$CONFIG_FILE" ]]; then
    stat -f%z "$CONFIG_FILE" 2>/dev/null || stat -c%s "$CONFIG_FILE" 2>/dev/null || echo "0"
  else
    echo "0"
  fi
}

nt_is_config_valid_json() {
  if command -v jq &>/dev/null; then
    jq empty "$CONFIG_FILE" 2>/dev/null
  else
    return 1
  fi
}

nt_get_config_field() {
  local field="$1"
  if command -v jq &>/dev/null && [[ -f "$CONFIG_FILE" ]]; then
    jq -r "$field // empty" "$CONFIG_FILE" 2>/dev/null || echo ""
  else
    echo ""
  fi
}

# ============================================================================
# SERVICE HEALTH CHECKS
# ============================================================================
nt_check_service_http() {
  local name="$1"
  local url="$2"
  local timeout="${3:-5}"

  if curl -s --connect-timeout "$timeout" "$url" &>/dev/null; then
    nt_test_result "$name HTTP" "pass"
    return 0
  else
    nt_test_result "$name HTTP" "fail" "Service not responding at $url"
    return 1
  fi
}

nt_check_backend_health() {
  nt_check_service_http "Backend API" "$BACKEND_URL/health" "${1:-5}"
}

nt_check_ccxt_health() {
  nt_check_service_http "CCXT service" "$CCXT_URL/health" "${1:-5}"
}

nt_check_telegram_health() {
  nt_check_service_http "Telegram service" "$TELEGRAM_URL/health" "${1:-5}"
}

# ============================================================================
# DATABASE CHECKS
# ============================================================================
nt_check_database_exists() {
  if [[ -f "$DB_PATH" ]]; then
    nt_test_result "SQLite database exists" "pass"
    return 0
  else
    nt_test_result "SQLite database exists" "skip" "Database will be created on first run"
    return 1
  fi
}

nt_check_database_table() {
  local table="$1"

  if ! command -v sqlite3 &>/dev/null; then
    nt_test_result "Table '$table' exists" "skip" "sqlite3 not installed"
    return 1
  fi

  if [[ ! -f "$DB_PATH" ]]; then
    nt_test_result "Table '$table' exists" "fail" "Database not found"
    return 1
  fi

  local tables
  tables=$(sqlite3 "$DB_PATH" "SELECT name FROM sqlite_master WHERE type='table';" 2>/dev/null || echo "")

  if echo "$tables" | grep -q "^${table}$"; then
    nt_test_result "Table '$table' exists" "pass"
    return 0
  else
    nt_test_result "Table '$table' exists" "fail" "Run migrations: make migrate"
    return 1
  fi
}

nt_check_redis() {
  if ! command -v redis-cli &>/dev/null; then
    nt_test_result "Redis connection" "skip" "redis-cli not installed"
    return 1
  fi

  if redis-cli ping &>/dev/null; then
    nt_test_result "Redis connection" "pass"
    return 0
  else
    nt_test_result "Redis connection" "fail" "Redis not running on localhost:6379"
    return 1
  fi
}

# ============================================================================
# TELEGRAM BOT CHECKS
# ============================================================================
nt_get_bot_token() {
  nt_get_config_field '.services.telegram.bot_token'
}

nt_check_bot_token_configured() {
  local token
  token=$(nt_get_bot_token)

  if [[ -n "$token" && "$token" != "null" && "$token" != "YOUR_TELEGRAM_BOT_TOKEN_HERE" ]]; then
    nt_test_result "Telegram bot token" "pass" "Token configured (masked)"
    return 0
  else
    nt_test_result "Telegram bot token" "fail" "Bot token not configured"
    return 1
  fi
}

nt_check_bot_reachable() {
  local token="$1"

  if [[ -z "$token" ]]; then
    nt_test_result "Telegram bot reachable" "skip" "No bot token provided"
    return 1
  fi

  local bot_info
  bot_info=$(curl -s "https://api.telegram.org/bot${token}/getMe" 2>/dev/null || echo "")

  if echo "$bot_info" | grep -q '"ok":true'; then
    local bot_name
    bot_name=$(echo "$bot_info" | jq -r '.result.username' 2>/dev/null || echo "unknown")
    nt_test_result "Telegram bot reachable" "pass" "@$bot_name"
    return 0
  else
    nt_test_result "Telegram bot reachable" "fail" "Bot token may be invalid"
    return 1
  fi
}

# ============================================================================
# CCXT MARKET DATA
# ============================================================================
nt_fetch_ccxt_ticker() {
  local exchange="${1:-binance}"
  local symbol="${2:-BTC/USDT}"

  curl -s --connect-timeout 5 "$CCXT_URL/api/ticker/$exchange/$symbol" 2>/dev/null || echo ""
}

nt_fetch_ccxt_orderbook() {
  local exchange="${1:-binance}"
  local symbol="${2:-BTC/USDT}"

  curl -s --connect-timeout 5 "$CCXT_URL/api/orderbook/$exchange/$symbol" 2>/dev/null || echo ""
}

nt_fetch_ccxt_markets() {
  local exchange="${1:-binance}"

  curl -s --connect-timeout 5 "$CCXT_URL/api/markets/$exchange" 2>/dev/null || echo ""
}

nt_validate_json_field() {
  local json="$1"
  local field="$2"
  local expected_type="${3:-string}"

  if [[ -z "$json" ]]; then
    return 1
  fi

  if ! command -v jq &>/dev/null; then
    # Fallback to grep if jq not available
    echo "$json" | grep -q "\"$field\""
    return $?
  fi

  case "$expected_type" in
    array)
      jq -e "${field} | type == \"array\"" <<<"$json" &>/dev/null
      ;;
    number)
      jq -e "${field} | type == \"number\"" <<<"$json" &>/dev/null
      ;;
    boolean)
      jq -e "${field} | type == \"boolean\"" <<<"$json" &>/dev/null
      ;;
    *)
      jq -e "getpath(path(.${field}))" <<<"$json" &>/dev/null
      ;;
  esac
}

# ============================================================================
# SUMMARY
# ============================================================================
nt_print_summary() {
  nt_header "Test Summary"

  echo "Tests Passed:  $TESTS_PASSED"
  echo "Tests Failed:  $TESTS_FAILED"
  echo "Tests Skipped: $TESTS_SKIPPED"
  echo ""

  if [[ $TESTS_FAILED -eq 0 ]]; then
    nt_success "All critical tests passed!"
  else
    nt_error "Some tests failed - review output above"
  fi
}

nt_is_system_ready() {
  [[ $TESTS_FAILED -eq 0 ]]
}

# ============================================================================
# INITIALIZATION
# ============================================================================
nt_init() {
  local title="${1:-NeuraTrade Test Suite}"

  nt_header "$title"
  echo "Version: 1.0.0"
  echo "Date: $(date -u +'%Y-%m-%dT%H:%M:%SZ')"
  echo ""

  # Reset counters
  TESTS_PASSED=0
  TESTS_FAILED=0
  TESTS_SKIPPED=0
}

# Export functions for use in other scripts
export -f nt_info nt_success nt_warn nt_error nt_step nt_header
export -f nt_test_result
export -f nt_check_config_exists nt_get_config_size nt_is_config_valid_json nt_get_config_field
export -f nt_check_service_http nt_check_backend_health nt_check_ccxt_health nt_check_telegram_health
export -f nt_check_database_exists nt_check_database_table nt_check_redis
export -f nt_get_bot_token nt_check_bot_token_configured nt_check_bot_reachable
export -f nt_fetch_ccxt_ticker nt_fetch_ccxt_orderbook nt_fetch_ccxt_markets nt_validate_json_field
export -f nt_print_summary nt_is_system_ready nt_init

# Export configuration
export CONFIG_FILE DB_PATH BACKEND_URL CCXT_URL TELEGRAM_URL
