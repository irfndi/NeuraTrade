#!/bin/bash
#
# NeuraTrade Autonomous Trading Test Script - Simplified
# Tests the complete flow from configuration to autonomous trading
#
# Usage: bash scripts/test-autonomous-trading-simple.sh
#

# shellcheck source=lib/test-lib.sh
source "$(dirname "$0")/lib/test-lib.sh" || { echo "missing test-lib.sh" >&2; exit 1; }

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

if curl -fsS --connect-timeout 3 --max-time 5 http://localhost:3001/health >/dev/null 2>&1; then
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
  BALANCE_TMP=$(mktemp)
  BALANCE_CODE=$(curl -s --max-time 5 -o "$BALANCE_TMP" -w '%{http_code}' "http://localhost:3001/api/balance/binance" || true)
  BALANCE=$(cat "$BALANCE_TMP")
  rm -f "$BALANCE_TMP"
  case "$BALANCE_CODE" in
    2*)
      nt_test_result "GET /api/balance/binance" "pass" "returns data (API keys configured)"
      ;;
    401|403)
      nt_test_result "GET /api/balance/binance" "pass" "correctly requires auth"
      ;;
    *)
      nt_test_result "GET /api/balance/binance" "skip" "service issue (HTTP $balance_code)"
      ;;
  esac
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

if curl -fsS --connect-timeout 3 --max-time 5 http://localhost:8080/health >/dev/null 2>&1; then
  # Test Portfolio safety
  PORTFOLIO_TMP=$(mktemp)
  PORTFOLIO_CODE=$(curl -s --max-time 5 -o "$PORTFOLIO_TMP" -w '%{http_code}' "http://localhost:8080/api/v1/telegram/internal/portfolio?chat_id=test_chat_123" || true)
  PORTFOLIO=$(cat "$PORTFOLIO_TMP")
  rm -f "$PORTFOLIO_TMP"
  case "$PORTFOLIO_CODE" in
    2*)
      if nt_validate_json_field "$PORTFOLIO" "safety_status"; then
        nt_test_result "GET /api/v1/telegram/internal/portfolio" "pass" "contains safety status"
      else
        nt_test_result "GET /api/v1/telegram/internal/portfolio" "fail" "no valid response"
      fi
      ;;
    401|403)
      nt_test_result "GET /api/v1/telegram/internal/portfolio" "pass" "correctly rejects unauthorized"
      ;;
    *)
      nt_test_result "GET /api/v1/telegram/internal/portfolio" "skip" "HTTP $PORTFOLIO_CODE"
      ;;
  esac

  # Test Doctor Check
  DOCTOR_TMP=$(mktemp)
  DOCTOR_CODE=$(curl -s --max-time 5 -o "$DOCTOR_TMP" -w '%{http_code}' "http://localhost:8080/internal/telegram/doctor?chat_id=test_chat_123" || true)
  DOCTOR=$(cat "$DOCTOR_TMP")
  rm -f "$DOCTOR_TMP"
  case "$DOCTOR_CODE" in
    2*)
      if nt_validate_json_field "$DOCTOR" "checks"; then
        nt_test_result "GET /internal/telegram/doctor" "pass" "contains doctor checks"
      else
        nt_test_result "GET /internal/telegram/doctor" "fail" "no valid response"
      fi
      ;;
    401|403)
      nt_test_result "GET /internal/telegram/doctor" "pass" "correctly rejects unauthorized"
      ;;
    *)
      nt_test_result "GET /internal/telegram/doctor" "skip" "HTTP $DOCTOR_CODE"
      ;;
  esac
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
