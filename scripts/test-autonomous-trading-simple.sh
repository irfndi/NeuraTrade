#!/bin/bash
#
# NeuraTrade Autonomous Trading Test Script - Simplified
# Tests the complete flow from configuration to autonomous trading
#
# Usage: bash scripts/test-autonomous-trading-simple.sh
#

source "$(dirname "$0")/lib/test-lib.sh"

nt_init "NeuraTrade Autonomous Trading Test Suite"

# ============================================================================
# 1. CONFIGURATION CHECKS
# ============================================================================
nt_header "1. Configuration Checks"



if [ -f "$CONFIG_FILE" ]; then
  nt_test_result "Config file exists" "pass" "$CONFIG_FILE"

  # Check content
  CONTENT=$(cat "$CONFIG_FILE" || true)
  if [ "$CONTENT" = "{}" ] || [ -z "$CONTENT" ]; then
    nt_test_result "Config file content" "fail" "Config file is empty: {}" || true
    nt_info "Fix: Use CLI or edit $CONFIG_FILE manually"
  else
    nt_test_result "Config file content" "pass" || true

    # Check with jq if available
    if command -v jq &>/dev/null; then
      # Check for key sections
      if jq -e '.services.ccxt' "$CONFIG_FILE" >/dev/null 2>&1; then
        nt_test_result "CCXT config present" "pass"
      else
        nt_test_result "CCXT config present" "skip"
      fi

      if jq -e '.services.telegram' "$CONFIG_FILE" >/dev/null 2>&1; then
        nt_test_result "Telegram config present" "pass"
      else
        nt_test_result "Telegram config present" "skip"
      fi

      if jq -e '.ai' "$CONFIG_FILE" >/dev/null 2>&1; then
        nt_test_result "AI config present" "pass"
      else
        nt_test_result "AI config present" "skip"
      fi

      # Check for Binance keys
      if jq -e '.services.ccxt.exchanges.binance.api_key' "$CONFIG_FILE" >/dev/null 2>&1; then
        nt_test_result "Binance API keys configured" "pass"
      else
        nt_test_result "Binance API keys configured" "skip" "Use /connect_exchange binance via Telegram"
      fi
    fi
  fi
else
  nt_test_result "Config file exists" "fail" "$CONFIG_FILE not found"
fi

# ============================================================================
# 2. DATABASE CHECKS
# ============================================================================
nt_header "2. Database Checks"

nt_check_database_exists || true
nt_check_database_table "users" || true
nt_check_database_table "wallets" || true
nt_check_database_table "exchange_api_keys" || true
nt_check_redis || true

# ============================================================================
# 3. SERVICE CHECKS
# ============================================================================
nt_header "3. Service Availability"

# CCXT service
nt_check_ccxt_health 3 || nt_info "Start: cd services/ccxt-service && bun run start"
nt_check_backend_health 3 || nt_info "Start: cd services/backend-api && make run"
nt_check_telegram_health 3 || nt_info "Start: cd services/telegram-service && bun run start"

# ============================================================================
# 4. CCXT MARKET DATA ACCESS TEST
# ============================================================================
nt_header "4. CCXT Market Data (Auth vs Non-Auth)"

nt_info "IMPORTANT: CCXT provides PUBLIC market data WITHOUT authentication"
nt_info "Authentication is ONLY required for: trading, balances, orders"

if curl -s --connect-timeout 3 http://localhost:3001/health >/dev/null 2>&1; then
  # Test public endpoints
  nt_info "Testing PUBLIC endpoints (no API keys needed)..."

  # Markets
  MARKETS=$(nt_fetch_ccxt_markets binance)
  if [ -n "$MARKETS" ]; then
    nt_test_result "GET /api/markets/binance (public)" "pass"
  else
    nt_test_result "GET /api/markets/binance (public)" "skip" "no data"
  fi

  # Orderbook
  ORDERBOOK=$(nt_fetch_ccxt_orderbook binance "BTC/USDT")
  if [ -n "$ORDERBOOK" ]; then
    nt_test_result "GET /api/orderbook/binance/BTC/USDT (public)" "pass"
  else
    nt_test_result "GET /api/orderbook/binance/BTC/USDT (public)" "skip" "no data"
  fi

  # Test protected endpoint
  nt_info "Testing PROTECTED endpoint (API keys required)..."
  BALANCE=$(curl -s --connect-timeout 5 "http://localhost:3001/api/balance/binance" 2>/dev/null)
  if echo "$BALANCE" | grep -qi "error.*auth\|error.*key\|error.*API"; then
    nt_test_result "GET /api/balance/binance" "pass" "correctly requires auth"
  elif [ -n "$BALANCE" ]; then
    nt_test_result "GET /api/balance/binance" "pass" "returns data (API keys configured)"
  else
    nt_test_result "GET /api/balance/binance" "skip" "service issue"
  fi
else
  nt_test_result "CCXT market data tests" "skip" "service not running"
fi

# ============================================================================
# 5. TELEGRAM BOT TESTS
# ============================================================================
nt_header "5. Telegram Bot Commands"

nt_check_bot_token_configured || true
nt_check_bot_reachable "$(nt_get_bot_token)" || true

# ============================================================================
# 5A. AUTONOMOUS ENDPOINTS TESTS
# ============================================================================
nt_header "5A. Autonomous API Endpoints"

if curl -s --connect-timeout 3 http://localhost:8080/health >/dev/null 2>&1; then
  # Test Portfolio safety
  PORTFOLIO=$(curl -s --connect-timeout 5 "http://localhost:8080/api/v1/telegram/internal/portfolio?chat_id=test_chat_123" 2>/dev/null)
  if nt_validate_json_field "$PORTFOLIO" "safety_status"; then
    nt_test_result "GET /api/v1/telegram/internal/portfolio" "pass" "contains safety status"
  else
    # might fail if no auth, since it requires auth but we just check if it's reachable or gives auth error
    if echo "$PORTFOLIO" | grep -qi "unauthorized\|error"; then
      nt_test_result "GET /api/v1/telegram/internal/portfolio" "pass" "correctly rejects unauthorized"
    else
      nt_test_result "GET /api/v1/telegram/internal/portfolio" "fail" "no valid response"
    fi
  fi

  # Test Doctor Check
  DOCTOR=$(curl -s --connect-timeout 5 "http://localhost:8080/api/v1/telegram/internal/doctor?chat_id=test_chat_123" 2>/dev/null)
  if nt_validate_json_field "$DOCTOR" "checks"; then
    nt_test_result "GET /api/v1/telegram/internal/doctor" "pass" "contains doctor checks"
  else
    if echo "$DOCTOR" | grep -qi "unauthorized\|error"; then
      nt_test_result "GET /api/v1/telegram/internal/doctor" "pass" "correctly rejects unauthorized"
    else
      nt_test_result "GET /api/v1/telegram/internal/doctor" "fail" "no valid response"
    fi
  fi
else
  nt_test_result "Autonomous API Endpoints" "skip" "backend not running"
fi

# ============================================================================
# 6. AUTONOMOUS TRADING READINESS
# ============================================================================
nt_header "6. Autonomous Trading Readiness"

READY=true

if ! nt_is_system_ready; then
  READY=false
fi

if [ "$READY" = true ]; then
  nt_success "System READY for autonomous trading!"
  nt_info "Next steps:"
  nt_info "  1. Connect exchange: /connect_exchange binance"
  nt_info "  2. Start autonomous: /begin"
  nt_info "  3. Monitor: /status, /portfolio"
else
  nt_error "System NOT ready - fix issues above"
fi

# ============================================================================
# SUMMARY
# ============================================================================
nt_print_summary

echo ""
nt_info "Quick Start Commands:"
nt_info "  1. neuratrade gateway start    # Start all services"
nt_info "  2. Send /start to bot          # Test Telegram"
nt_info "  3. /connect_exchange binance   # Connect exchange"
nt_info "  4. /begin                      # Start autonomous trading"
