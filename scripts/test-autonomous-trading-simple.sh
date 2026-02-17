#!/bin/bash
#
# NeuraTrade Autonomous Trading Test Script - Simplified
# Tests the complete flow from configuration to autonomous trading
#
# Usage: bash scripts/test-autonomous-trading-simple.sh
#

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

# Counters
TESTS_PASSED=0
TESTS_FAILED=0
TESTS_SKIPPED=0

print_info() { echo -e "${BLUE}[INFO]${NC} $*"; }
print_success() { echo -e "${GREEN}[PASS]${NC} $*"; }
print_warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }
print_error() { echo -e "${RED}[FAIL]${NC} $*"; }
print_header() {
  echo ""
  echo -e "${CYAN}========================================${NC}"
  echo -e "${CYAN}$*${NC}"
  echo -e "${CYAN}========================================${NC}"
  echo ""
}

print_header "NeuraTrade Autonomous Trading Test Suite"
echo "Date: $(date)"
echo ""

# ============================================================================
# 1. CONFIGURATION CHECKS
# ============================================================================
print_header "1. Configuration Checks"

CONFIG_FILE="$HOME/.neuratrade/config.json"

if [ -f "$CONFIG_FILE" ]; then
  print_success "Config file exists: $CONFIG_FILE"

  # Check content
  CONTENT=$(cat "$CONFIG_FILE")
  if [ "$CONTENT" = "{}" ] || [ -z "$CONTENT" ]; then
    print_error "Config file is empty: {}"
    print_info "Fix: Use CLI or edit $CONFIG_FILE manually"
    TESTS_FAILED=$((TESTS_FAILED + 1))
  else
    print_success "Config file has content"
    TESTS_PASSED=$((TESTS_PASSED + 1))

    # Check with jq if available
    if command -v jq &>/dev/null; then
      # Check for key sections
      if jq -e '.services.ccxt' "$CONFIG_FILE" >/dev/null 2>&1; then
        print_success "CCXT config present"
        TESTS_PASSED=$((TESTS_PASSED + 1))
      else
        print_warn "CCXT config missing"
        TESTS_SKIPPED=$((TESTS_SKIPPED + 1))
      fi

      if jq -e '.services.telegram' "$CONFIG_FILE" >/dev/null 2>&1; then
        print_success "Telegram config present"
        TESTS_PASSED=$((TESTS_PASSED + 1))
      else
        print_warn "Telegram config missing"
        TESTS_SKIPPED=$((TESTS_SKIPPED + 1))
      fi

      if jq -e '.ai' "$CONFIG_FILE" >/dev/null 2>&1; then
        print_success "AI config present"
        TESTS_PASSED=$((TESTS_PASSED + 1))
      else
        print_warn "AI config missing"
        TESTS_SKIPPED=$((TESTS_SKIPPED + 1))
      fi

      # Check for Binance keys
      if jq -e '.services.ccxt.exchanges.binance.api_key' "$CONFIG_FILE" >/dev/null 2>&1; then
        print_success "Binance API keys configured"
        TESTS_PASSED=$((TESTS_PASSED + 1))
      else
        print_info "Binance API keys: Use /connect_exchange binance via Telegram"
        TESTS_SKIPPED=$((TESTS_SKIPPED + 1))
      fi
    fi
  fi
else
  print_error "Config file not found: $CONFIG_FILE"
  TESTS_FAILED=$((TESTS_FAILED + 1))
fi

# ============================================================================
# 2. DATABASE CHECKS
# ============================================================================
print_header "2. Database Checks"

DB_PATH="$HOME/.neuratrade/data/neuratrade.db"

if [ -f "$DB_PATH" ]; then
  print_success "SQLite database exists"
  TESTS_PASSED=$((TESTS_PASSED + 1))

  if command -v sqlite3 &>/dev/null; then
    TABLES=$(sqlite3 "$DB_PATH" "SELECT name FROM sqlite_master WHERE type='table';" 2>/dev/null)

    if echo "$TABLES" | grep -q "users"; then
      print_success "Table 'users' exists"
      TESTS_PASSED=$((TESTS_PASSED + 1))
    else
      print_error "Table 'users' missing - run migrations"
      TESTS_FAILED=$((TESTS_FAILED + 1))
    fi

    if echo "$TABLES" | grep -q "wallets"; then
      print_success "Table 'wallets' exists"
      TESTS_PASSED=$((TESTS_PASSED + 1))
    else
      print_warn "Table 'wallets' missing"
      TESTS_SKIPPED=$((TESTS_SKIPPED + 1))
    fi

    if echo "$TABLES" | grep -q "exchange_api_keys"; then
      print_success "Table 'exchange_api_keys' exists"
      TESTS_PASSED=$((TESTS_PASSED + 1))
    else
      print_warn "Table 'exchange_api_keys' missing"
      TESTS_SKIPPED=$((TESTS_SKIPPED + 1))
    fi
  fi
else
  print_warn "SQLite database not found (will be created on first run)"
  TESTS_SKIPPED=$((TESTS_SKIPPED + 1))
fi

# Redis check
if command -v redis-cli &>/dev/null; then
  if redis-cli ping &>/dev/null; then
    print_success "Redis connection OK"
    TESTS_PASSED=$((TESTS_PASSED + 1))
  else
    print_error "Redis not running on localhost:6379"
    TESTS_FAILED=$((TESTS_FAILED + 1))
  fi
else
  print_warn "redis-cli not installed"
  TESTS_SKIPPED=$((TESTS_SKIPPED + 1))
fi

# ============================================================================
# 3. SERVICE CHECKS
# ============================================================================
print_header "3. Service Availability"

# CCXT service
print_info "Checking CCXT service (port 3001)..."
if curl -s --connect-timeout 3 http://localhost:3001/health >/dev/null 2>&1; then
  print_success "CCXT service HTTP OK"
  TESTS_PASSED=$((TESTS_PASSED + 1))

  # Test public market data (no auth needed)
  TICKER=$(curl -s --connect-timeout 5 "http://localhost:3001/api/ticker/binance/BTC/USDT" 2>/dev/null)
  if [ -n "$TICKER" ]; then
    print_success "CCXT public market data (ticker) - NO AUTH REQUIRED"
    TESTS_PASSED=$((TESTS_PASSED + 1))
  else
    print_warn "CCXT ticker endpoint - no data yet"
    TESTS_SKIPPED=$((TESTS_SKIPPED + 1))
  fi
else
  print_error "CCXT service NOT running on port 3001"
  print_info "Start: cd services/ccxt-service && bun run start"
  TESTS_FAILED=$((TESTS_FAILED + 1))
fi

# Backend API
print_info "Checking Backend API (port 8080)..."
if curl -s --connect-timeout 3 http://localhost:8080/health >/dev/null 2>&1; then
  print_success "Backend API health OK"
  TESTS_PASSED=$((TESTS_PASSED + 1))
else
  print_error "Backend API NOT running on port 8080"
  print_info "Start: cd services/backend-api && make run"
  TESTS_FAILED=$((TESTS_FAILED + 1))
fi

# Telegram service
print_info "Checking Telegram service (port 3002)..."
if curl -s --connect-timeout 3 http://localhost:3002/health >/dev/null 2>&1; then
  print_success "Telegram service HTTP OK"
  TESTS_PASSED=$((TESTS_PASSED + 1))
else
  print_warn "Telegram service NOT running (needed for bot)"
  print_info "Start: cd services/telegram-service && bun run start"
  TESTS_SKIPPED=$((TESTS_SKIPPED + 1))
fi

# ============================================================================
# 4. CCXT MARKET DATA ACCESS TEST
# ============================================================================
print_header "4. CCXT Market Data (Auth vs Non-Auth)"

print_info "IMPORTANT: CCXT provides PUBLIC market data WITHOUT authentication"
print_info "Authentication is ONLY required for: trading, balances, orders"

if curl -s --connect-timeout 3 http://localhost:3001/health >/dev/null 2>&1; then
  # Test public endpoints
  print_info "Testing PUBLIC endpoints (no API keys needed)..."

  # Markets
  MARKETS=$(curl -s --connect-timeout 5 "http://localhost:3001/api/markets/binance" 2>/dev/null)
  if [ -n "$MARKETS" ]; then
    print_success "GET /api/markets/binance (public)"
    TESTS_PASSED=$((TESTS_PASSED + 1))
  else
    print_warn "GET /api/markets/binance - no data"
    TESTS_SKIPPED=$((TESTS_SKIPPED + 1))
  fi

  # Orderbook
  ORDERBOOK=$(curl -s --connect-timeout 5 "http://localhost:3001/api/orderbook/binance/BTC/USDT" 2>/dev/null)
  if [ -n "$ORDERBOOK" ]; then
    print_success "GET /api/orderbook/binance/BTC/USDT (public)"
    TESTS_PASSED=$((TESTS_PASSED + 1))
  else
    print_warn "GET /api/orderbook/binance/BTC/USDT - no data"
    TESTS_SKIPPED=$((TESTS_SKIPPED + 1))
  fi

  # Test protected endpoint
  print_info "Testing PROTECTED endpoint (API keys required)..."
  BALANCE=$(curl -s --connect-timeout 5 "http://localhost:3001/api/balance/binance" 2>/dev/null)
  if echo "$BALANCE" | grep -qi "error.*auth\|error.*key\|error.*API"; then
    print_success "GET /api/balance/binance correctly requires auth"
    TESTS_PASSED=$((TESTS_PASSED + 1))
  elif [ -n "$BALANCE" ]; then
    print_success "GET /api/balance/binance returns data (API keys configured)"
    TESTS_PASSED=$((TESTS_PASSED + 1))
  else
    print_warn "GET /api/balance/binance - service issue"
    TESTS_SKIPPED=$((TESTS_SKIPPED + 1))
  fi
else
  print_warn "CCXT service not running - skipping market data tests"
  TESTS_SKIPPED=$((TESTS_SKIPPED + 3))
fi

# ============================================================================
# 5. TELEGRAM BOT TESTS
# ============================================================================
print_header "5. Telegram Bot Commands"

# Get bot token from config
if command -v jq &>/dev/null && [ -f "$CONFIG_FILE" ]; then
  BOT_TOKEN=$(jq -r '.services.telegram.bot_token // empty' "$CONFIG_FILE" 2>/dev/null)

  if [ -n "$BOT_TOKEN" ] && [ "$BOT_TOKEN" != "null" ] && [ "$BOT_TOKEN" != "YOUR_TELEGRAM_BOT_TOKEN_HERE" ]; then
    print_success "Telegram bot token configured"
    TESTS_PASSED=$((TESTS_PASSED + 1))

    # Check bot
    BOT_INFO=$(curl -s "https://api.telegram.org/bot${BOT_TOKEN}/getMe" 2>/dev/null)
    if echo "$BOT_INFO" | grep -q '"ok":true'; then
      BOT_NAME=$(echo "$BOT_INFO" | jq -r '.result.username' 2>/dev/null)
      print_success "Telegram bot reachable: @$BOT_NAME"
      TESTS_PASSED=$((TESTS_PASSED + 1))
    else
      print_error "Telegram bot token may be invalid"
      TESTS_FAILED=$((TESTS_FAILED + 1))
    fi
  else
    print_warn "Telegram bot token not configured"
    TESTS_SKIPPED=$((TESTS_SKIPPED + 1))
  fi
fi

# ============================================================================
# 6. AUTONOMOUS TRADING READINESS
# ============================================================================
print_header "6. Autonomous Trading Readiness"

READY=true

if [ ! -f "$CONFIG_FILE" ] || [ "$(cat "$CONFIG_FILE")" = "{}" ]; then
  READY=false
  print_error "Missing: Configuration file content"
fi

if ! curl -s --connect-timeout 2 http://localhost:3001/health >/dev/null 2>&1; then
  READY=false
  print_error "Missing: CCXT service (port 3001)"
fi

if ! curl -s --connect-timeout 2 http://localhost:8080/health >/dev/null 2>&1; then
  READY=false
  print_error "Missing: Backend API (port 8080)"
fi

if command -v sqlite3 &>/dev/null && [ -f "$DB_PATH" ]; then
  TABLES=$(sqlite3 "$DB_PATH" "SELECT name FROM sqlite_master WHERE type='table';" 2>/dev/null)
  if ! echo "$TABLES" | grep -q "users"; then
    READY=false
    print_error "Missing: Database migrations"
  fi
fi

if [ "$READY" = true ]; then
  print_success "System READY for autonomous trading!"
  print_info "Next steps:"
  print_info "  1. Connect exchange: /connect_exchange binance"
  print_info "  2. Start autonomous: /begin"
  print_info "  3. Monitor: /status, /portfolio"
  TESTS_PASSED=$((TESTS_PASSED + 1))
else
  print_error "System NOT ready - fix issues above"
  TESTS_FAILED=$((TESTS_FAILED + 1))
fi

# ============================================================================
# SUMMARY
# ============================================================================
print_header "Test Summary"

echo "Tests Passed:  $TESTS_PASSED"
echo "Tests Failed:  $TESTS_FAILED"
echo "Tests Skipped: $TESTS_SKIPPED"
echo ""

if [ $TESTS_FAILED -eq 0 ]; then
  print_success "All critical tests passed!"
else
  print_error "Some tests failed - review output above"
fi

echo ""
print_info "Quick Start Commands:"
print_info "  1. neuratrade gateway start    # Start all services"
print_info "  2. Send /start to bot          # Test Telegram"
print_info "  3. /connect_exchange binance   # Connect exchange"
print_info "  4. /begin                      # Start autonomous trading"
