#!/bin/bash
#
# NeuraTrade Autonomous Trading Test Script
# Tests the complete flow from configuration to autonomous trading
#
# Usage: bash scripts/test-autonomous-trading.sh
#
# This script will:
# 1. Check and validate configuration
# 2. Verify database migrations
# 3. Test CCXT service market data access
# 4. Test Telegram bot commands (Phases 1-4)
# 5. Validate autonomous trading readiness
#

set -euo pipefail

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

# Print functions
print_info() { echo -e "${BLUE}[INFO]${NC} $*"; }
print_success() { echo -e "${GREEN}[PASS]${NC} $*"; }
print_warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }
print_error() { echo -e "${RED}[FAIL]${NC} $*"; }
print_step() { echo -e "${CYAN}[STEP]${NC} $*"; }
print_header() { echo -e "\n${CYAN}═══════════════════════════════════════════════════════════${NC}"; echo -e "${CYAN}$*${NC}"; echo -e "${CYAN}═══════════════════════════════════════════════════════════${NC}\n"; }

# Test result tracking
test_result() {
    local name="$1"
    local result="$2"
    local message="${3:-}"
    
    if [ "$result" = "pass" ]; then
        print_success "$name"
        ((TESTS_PASSED++))
    elif [ "$result" = "skip" ]; then
        print_warn "$name (skipped: $message)"
        ((TESTS_SKIPPED++))
    else
        print_error "$name"
        [ -n "$message" ] && echo "   Details: $message"
        ((TESTS_FAILED++))
    fi
}

# ============================================================================
# CONFIGURATION CHECKS
# ============================================================================
check_configuration() {
    print_header "1. Configuration Checks"
    
    # Check config file exists
    local config_file="$HOME/.neuratrade/config.json"
    if [ -f "$config_file" ]; then
        test_result "Config file exists" "pass"
        
        # Check if config is empty or valid JSON
        local config_size
        config_size=$(stat -f%z "$config_file" 2>/dev/null || stat -c%s "$config_file" 2>/dev/null || echo "0")
        
        if [ "$config_size" -lt 5 ]; then
            test_result "Config file has content" "fail" "File is empty or too small ($config_size bytes)"
            print_info "Fix: Run 'neuratrade config init' or manually edit ~/.neuratrade/config.json"
        elif [ "$config_size" -eq 2 ] && [ "$(cat "$config_file")" = "{}" ]; then
            test_result "Config file has content" "fail" "File contains empty JSON object {}"
            print_info "Fix: Run 'neuratrade config init' or manually edit ~/.neuratrade/config.json"
        else
            # Validate JSON
            if command -v jq &>/dev/null; then
                if jq empty "$config_file" 2>/dev/null; then
                    test_result "Config is valid JSON" "pass"
                    
                    # Check for required fields
                    local has_ccxt has_telegram has_ai has_security
                    has_ccxt=$(jq -r '.services.ccxt // empty' "$config_file" 2>/dev/null)
                    has_telegram=$(jq -r '.services.telegram // empty' "$config_file" 2>/dev/null)
                    has_ai=$(jq -r '.ai // empty' "$config_file" 2>/dev/null)
                    has_security=$(jq -r '.security.admin_api_key // empty' "$config_file" 2>/dev/null)
                    
                    [ -n "$has_ccxt" ] && test_result "CCXT config present" "pass" || test_result "CCXT config present" "fail"
                    [ -n "$has_telegram" ] && test_result "Telegram config present" "pass" || test_result "Telegram config present" "fail"
                    [ -n "$has_ai" ] && test_result "AI config present" "pass" || test_result "AI config present" "fail"
                    [ -n "$has_security" ] && test_result "Security config present" "pass" || test_result "Security config present" "fail"
                    
                    # Check for Binance API keys
                    local has_binance_keys
                    has_binance_keys=$(jq -r '.services.ccxt.exchanges.binance.api_key // empty' "$config_file" 2>/dev/null)
                    if [ -n "$has_binance_keys" ]; then
                        test_result "Binance API keys configured" "pass"
                    else
                        test_result "Binance API keys configured" "skip" "Use /connect_exchange binance via Telegram"
                    fi
                else
                    test_result "Config is valid JSON" "fail" "Invalid JSON format"
                fi
            else
                test_result "Config JSON validation" "skip" "jq not installed"
            fi
        fi
    else
        test_result "Config file exists" "fail" "$config_file not found"
    fi
    
    # Check .env file
    local env_file="$HOME/.neuratrade/.env"
    if [ -f "$env_file" ]; then
        test_result ".env file exists" "pass"
    else
        test_result ".env file exists" "warn" "Using default configuration"
    fi
}

# ============================================================================
# DATABASE CHECKS
# ============================================================================
check_database() {
    print_header "2. Database Checks"
    
    local db_path="$HOME/.neuratrade/data/neuratrade.db"
    
    # Check if database exists
    if [ -f "$db_path" ]; then
        test_result "SQLite database exists" "pass"
        
        # Check for required tables
        if command -v sqlite3 &>/dev/null; then
            local tables
            tables=$(sqlite3 "$db_path" "SELECT name FROM sqlite_master WHERE type='table';" 2>/dev/null || echo "")
            
            local required_tables=("users" "wallets" "exchange_api_keys" "trading_pairs" "exchanges")
            
            for table in "${required_tables[@]}"; do
                if echo "$tables" | grep -q "^${table}$"; then
                    test_result "Table '$table' exists" "pass"
                else
                    test_result "Table '$table' exists" "fail" "Run migrations: make migrate"
                fi
            done
        else
            test_result "Database table check" "skip" "sqlite3 not installed"
        fi
    else
        test_result "SQLite database exists" "warn" "Database will be created on first run"
    fi
    
    # Check Redis connectivity
    if command -v redis-cli &>/dev/null; then
        if redis-cli ping &>/dev/null; then
            test_result "Redis connection" "pass"
        else
            test_result "Redis connection" "fail" "Redis not running on localhost:6379"
        fi
    else
        test_result "Redis connection" "skip" "redis-cli not installed"
    fi
}

# ============================================================================
# SERVICE CHECKS
# ============================================================================
check_services() {
    print_header "3. Service Availability Checks"
    
    # Check CCXT service
    print_step "Checking CCXT service (port 3001)..."
    if curl -s --connect-timeout 5 http://localhost:3001/health &>/dev/null; then
        test_result "CCXT service HTTP" "pass"
        
        # Test public market data access (no auth required)
        local ticker_response
        ticker_response=$(curl -s --connect-timeout 5 "http://localhost:3001/api/ticker/binance/BTC/USDT" 2>/dev/null || echo "")
        
        if [ -n "$ticker_response" ]; then
            test_result "CCXT public market data (ticker)" "pass" "No auth required for public data"
        else
            test_result "CCXT public market data (ticker)" "fail" "CCXT not returning data"
        fi
        
        # Test orderbook (public data)
        local orderbook_response
        orderbook_response=$(curl -s --connect-timeout 5 "http://localhost:3001/api/orderbook/binance/BTC/USDT" 2>/dev/null || echo "")
        
        if [ -n "$orderbook_response" ]; then
            test_result "CCXT public market data (orderbook)" "pass" "No auth required for public data"
        else
            test_result "CCXT public market data (orderbook)" "warn" "May need exchange initialization"
        fi
    else
        test_result "CCXT service HTTP" "fail" "Service not running on port 3001"
        print_info "Start CCXT service: cd services/ccxt-service && bun run start"
    fi
    
    # Check backend API
    print_step "Checking Backend API (port 8080)..."
    if curl -s --connect-timeout 5 http://localhost:8080/health &>/dev/null; then
        test_result "Backend API health" "pass"
    else
        test_result "Backend API health" "fail" "Service not running on port 8080"
        print_info "Start backend: cd services/backend-api && make run"
    fi
    
    # Check Telegram service
    print_step "Checking Telegram service (port 3002)..."
    if curl -s --connect-timeout 5 http://localhost:3002/health &>/dev/null; then
        test_result "Telegram service HTTP" "pass"
    else
        test_result "Telegram service HTTP" "warn" "Service not running (needed for bot commands)"
        print_info "Start Telegram: cd services/telegram-service && bun run start"
    fi
}

# ============================================================================
# TELEGRAM BOT COMMAND TESTS
# ============================================================================
test_telegram_commands() {
    print_header "4. Telegram Bot Command Tests"
    
    # Check for bot token
    local bot_token
    bot_token=$(jq -r '.services.telegram.bot_token // empty' "$HOME/.neuratrade/config.json" 2>/dev/null || echo "")
    
    if [ -z "$bot_token" ] || [ "$bot_token" = "null" ] || [ "$bot_token" = "YOUR_TELEGRAM_BOT_TOKEN_HERE" ]; then
        test_result "Telegram bot token" "fail" "Bot token not configured"
        print_info "Set TELEGRAM_BOT_TOKEN in ~/.neuratrade/config.json"
        return
    fi
    
    test_result "Telegram bot token" "pass" "Token configured (masked)"
    
    # Check if bot is reachable
    local bot_info
    bot_info=$(curl -s "https://api.telegram.org/bot${bot_token}/getMe" 2>/dev/null || echo "")
    
    if echo "$bot_info" | grep -q '"ok":true'; then
        local bot_name
        bot_name=$(echo "$bot_info" | jq -r '.result.username' 2>/dev/null || echo "unknown")
        test_result "Telegram bot reachable" "pass" "@$bot_name"
    else
        test_result "Telegram bot reachable" "fail" "Bot token may be invalid"
        return
    fi
    
    # Get chat ID from environment or use default
    local chat_id="${TELEGRAM_TEST_CHAT_ID:-}"
    if [ -z "$chat_id" ]; then
        # Try to get from config or use test ID
        chat_id=$(jq -r '.telegram_test_chat_id // empty' "$HOME/.neuratrade/config.json" 2>/dev/null || echo "")
    fi
    
    if [ -z "$chat_id" ]; then
        test_result "Telegram chat ID" "skip" "Set TELEGRAM_TEST_CHAT_ID env var for automated testing"
        print_info "Manual testing: Send commands to @neuratradelocal_bot"
        return
    fi
    
    test_result "Telegram chat ID" "pass" "Using chat ID: $chat_id"
    
    # Phase 1: Basic Commands
    print_step "Phase 1: Basic Commands (No Setup Required)"
    
    local backend_url
    backend_url=$(jq -r '.services.telegram.api_base_url // "http://localhost:8080"' "$HOME/.neuratrade/config.json" 2>/dev/null || echo "http://localhost:8080")
    
    # Test /start command via internal API
    local start_response
    start_response=$(curl -s -X POST "$backend_url/api/v1/telegram/internal/command" \
        -H "Content-Type: application/json" \
        -d "{\"chat_id\":\"$chat_id\",\"command\":\"/start\",\"telegram_user_id\":\"$chat_id\"}" \
        2>/dev/null || echo "")
    
    if echo "$start_response" | grep -q '"ok":true\|"success":true\|"message"'; then
        test_result "POST /start command" "pass"
    else
        test_result "POST /start command" "fail" "Backend may not be running"
    fi
    
    # Test /help command
    local help_response
    help_response=$(curl -s -X POST "$backend_url/api/v1/telegram/internal/command" \
        -H "Content-Type: application/json" \
        -d "{\"chat_id\":\"$chat_id\",\"command\":\"/help\",\"telegram_user_id\":\"$chat_id\"}" \
        2>/dev/null || echo "")
    
    if echo "$help_response" | grep -q '"ok":true\|"success":true\|"message"'; then
        test_result "POST /help command" "pass"
    else
        test_result "POST /help command" "warn" "Backend may not be running"
    fi
    
    # Phase 2: AI Commands
    print_step "Phase 2: AI Commands"
    
    # Test /ai_models
    local ai_models_response
    ai_models_response=$(curl -s -X POST "$backend_url/api/v1/telegram/internal/command" \
        -H "Content-Type: application/json" \
        -d "{\"chat_id\":\"$chat_id\",\"command\":\"/ai_models\",\"telegram_user_id\":\"$chat_id\"}" \
        2>/dev/null || echo "")
    
    if echo "$ai_models_response" | grep -q '"ok":true\|"success":true\|"models"'; then
        test_result "POST /ai_models command" "pass"
    else
        test_result "POST /ai_models command" "warn" "AI service may not be configured"
    fi
    
    # Phase 3: System Status
    print_step "Phase 3: System Status Commands"
    
    # Test /status
    local status_response
    status_response=$(curl -s -X POST "$backend_url/api/v1/telegram/internal/command" \
        -H "Content-Type: application/json" \
        -d "{\"chat_id\":\"$chat_id\",\"command\":\"/status\",\"telegram_user_id\":\"$chat_id\"}" \
        2>/dev/null || echo "")
    
    if echo "$status_response" | grep -q '"ok":true\|"success":true\|"status"'; then
        test_result "POST /status command" "pass"
    else
        test_result "POST /status command" "warn" "Backend may not be running"
    fi
    
    # Test /opportunities
    local opp_response
    opp_response=$(curl -s -X POST "$backend_url/api/v1/telegram/internal/command" \
        -H "Content-Type: application/json" \
        -d "{\"chat_id\":\"$chat_id\",\"command\":\"/opportunities\",\"telegram_user_id\":\"$chat_id\"}" \
        2>/dev/null || echo "")
    
    if echo "$opp_response" | grep -q '"ok":true\|"success":true\|"opportunities"'; then
        test_result "POST /opportunities command" "pass"
    else
        test_result "POST /opportunities command" "warn" "No opportunities or service not running"
    fi
}

# ============================================================================
# WALLET & EXCHANGE TESTS (Phase 2)
# ============================================================================
test_wallet_exchange() {
    print_header "5. Wallet & Exchange Tests (Phase 2)"
    
    local backend_url
    backend_url=$(jq -r '.services.telegram.api_base_url // "http://localhost:8080"' "$HOME/.neuratrade/config.json" 2>/dev/null || echo "http://localhost:8080")
    
    local chat_id="${TELEGRAM_TEST_CHAT_ID:-test_chat_123}"
    
    # Test /wallet command
    print_step "Testing wallet commands..."
    
    local wallet_response
    wallet_response=$(curl -s -X POST "$backend_url/api/v1/telegram/internal/command" \
        -H "Content-Type: application/json" \
        -d "{\"chat_id\":\"$chat_id\",\"command\":\"/wallet\",\"telegram_user_id\":\"$chat_id\"}" \
        2>/dev/null || echo "")
    
    if echo "$wallet_response" | grep -q '"ok":true\|"success":true\|"wallets"'; then
        test_result "POST /wallet command" "pass"
    else
        test_result "POST /wallet command" "warn" "Requires database setup"
    fi
    
    # Test /connect_exchange (without actual API keys - just validation)
    print_info "Testing /connect_exchange validation..."
    
    local connect_response
    connect_response=$(curl -s -X POST "$backend_url/api/v1/telegram/internal/connect-exchange" \
        -H "Content-Type: application/json" \
        -d "{\"chat_id\":\"$chat_id\",\"exchange\":\"binance\",\"account_label\":\"test\"}" \
        2>/dev/null || echo "")
    
    if echo "$connect_response" | grep -q '"ok":true\|"success":true\|"message"'; then
        test_result "POST /connect_exchange (wallet creation)" "pass"
    else
        test_result "POST /connect_exchange (wallet creation)" "warn" "Requires database and running services"
    fi
}

# ============================================================================
# AUTONOMOUS TRADING READINESS
# ============================================================================
test_autonomous_readiness() {
    print_header "6. Autonomous Trading Readiness"
    
    local ready=true
    local missing=()
    
    # Check config
    if [ ! -f "$HOME/.neuratrade/config.json" ] || [ "$(stat -f%z "$HOME/.neuratrade/config.json" 2>/dev/null || stat -c%s "$HOME/.neuratrade/config.json" 2>/dev/null || echo "0")" -lt 5 ]; then
        ready=false
        missing+=("Configuration file")
    fi
    
    # Check CCXT service
    if ! curl -s --connect-timeout 3 http://localhost:3001/health &>/dev/null; then
        ready=false
        missing+=("CCXT service (port 3001)")
    fi
    
    # Check backend
    if ! curl -s --connect-timeout 3 http://localhost:8080/health &>/dev/null; then
        ready=false
        missing+=("Backend API (port 8080)")
    fi
    
    # Check database tables
    if command -v sqlite3 &>/dev/null && [ -f "$HOME/.neuratrade/data/neuratrade.db" ]; then
        local tables
        tables=$(sqlite3 "$HOME/.neuratrade/data/neuratrade.db" "SELECT name FROM sqlite_master WHERE type='table';" 2>/dev/null || echo "")
        
        if ! echo "$tables" | grep -q "^users$"; then
            ready=false
            missing+=("Database migrations")
        fi
    fi
    
    if [ "$ready" = true ]; then
        test_result "Autonomous trading readiness" "pass" "All prerequisites met"
        print_success "System is ready for autonomous trading!"
        print_info "Next steps:"
        print_info "1. Connect exchange: /connect_exchange binance"
        print_info "2. Start autonomous mode: /begin"
        print_info "3. Monitor: /status, /portfolio, /performance"
    else
        test_result "Autonomous trading readiness" "fail" "Missing: ${missing[*]}"
        print_info "To enable autonomous trading:"
        for item in "${missing[@]}"; do
            print_info "  - $item"
        done
    fi
}

# ============================================================================
# CCXT MARKET DATA ACCESS TEST
# ============================================================================
test_ccxt_market_data() {
    print_header "7. CCXT Market Data Access (Auth vs Non-Auth)"
    
    print_info "Testing: CCXT provides PUBLIC market data WITHOUT authentication"
    print_info "Authentication is ONLY required for: trading, balances, orders"
    
    if ! curl -s --connect-timeout 3 http://localhost:3001/health &>/dev/null; then
        test_result "CCXT service availability" "fail" "Service not running"
        return
    fi
    
    test_result "CCXT service availability" "pass"
    
    # Test public endpoints (no auth required)
    print_step "Testing PUBLIC endpoints (no API keys needed)..."
    
    # Markets endpoint
    local markets
    markets=$(curl -s --connect-timeout 5 "http://localhost:3001/api/markets/binance" 2>/dev/null || echo "")
    if [ -n "$markets" ] && echo "$markets" | grep -q '"BTC/USDT"\|"BTCUSDT"'; then
        test_result "GET /api/markets/binance (public)" "pass" "Returns trading pairs"
    else
        test_result "GET /api/markets/binance (public)" "warn" "No data or exchange not initialized"
    fi
    
    # Ticker endpoint
    local ticker
    ticker=$(curl -s --connect-timeout 5 "http://localhost:3001/api/ticker/binance/BTC/USDT" 2>/dev/null || echo "")
    if [ -n "$ticker" ] && echo "$ticker" | grep -q '"last"\|"bid"\|"ask"'; then
        test_result "GET /api/ticker/binance/BTC/USDT (public)" "pass" "Returns price data"
    else
        test_result "GET /api/ticker/binance/BTC/USDT (public)" "warn" "No ticker data"
    fi
    
    # Orderbook endpoint
    local orderbook
    orderbook=$(curl -s --connect-timeout 5 "http://localhost:3001/api/orderbook/binance/BTC/USDT" 2>/dev/null || echo "")
    if [ -n "$orderbook" ] && echo "$orderbook" | grep -q '"bids"\|"asks"'; then
        test_result "GET /api/orderbook/binance/BTC/USDT (public)" "pass" "Returns orderbook"
    else
        test_result "GET /api/orderbook/binance/BTC/USDT (public)" "warn" "No orderbook data"
    fi
    
    # OHLCV endpoint
    local ohlcv
    ohlcv=$(curl -s --connect-timeout 5 "http://localhost:3001/api/ohlcv/binance/BTC/USDT?timeframe=1h" 2>/dev/null || echo "")
    if [ -n "$ohlcv" ] && echo "$ohlcv" | grep -q '\[.*\]'; then
        test_result "GET /api/ohlcv/binance/BTC/USDT (public)" "pass" "Returns candlestick data"
    else
        test_result "GET /api/ohlcv/binance/BTC/USDT (public)" "warn" "No OHLCV data"
    fi
    
    print_step "Testing PROTECTED endpoints (API keys required)..."
    
    # Balance endpoint (requires auth)
    local balance
    balance=$(curl -s --connect-timeout 5 "http://localhost:3001/api/balance/binance" 2>/dev/null || echo "")
    if echo "$balance" | grep -q '"error".*auth\|"error".*key\|"error".*API'; then
        test_result "GET /api/balance/binance (protected)" "pass" "Correctly requires authentication"
    elif echo "$balance" | grep -q '"BTC"\|"USDT"\|"free"'; then
        test_result "GET /api/balance/binance (protected)" "pass" "Returns balance (API keys configured)"
    else
        test_result "GET /api/balance/binance (protected)" "warn" "Service may not be running"
    fi
}

# ============================================================================
# SUMMARY
# ============================================================================
print_summary() {
    print_header "Test Summary"
    
    echo "Tests Passed:  $TESTS_PASSED"
    echo "Tests Failed:  $TESTS_FAILED"
    echo "Tests Skipped: $TESTS_SKIPPED"
    echo ""
    
    if [ $TESTS_FAILED -eq 0 ]; then
        print_success "All tests passed! System is ready for autonomous trading."
    else
        print_error "Some tests failed. Review the output above and fix issues."
    fi
    
    echo ""
    print_info "Next Steps:"
    print_info "1. Fix any failed tests above"
    print_info "2. Run: neuratrade gateway start (or make dev-up-orchestrated)"
    print_info "3. Test Telegram bot: Send /start to @your_bot"
    print_info "4. Connect exchange: /connect_exchange binance"
    print_info "5. Start autonomous: /begin"
}

# ============================================================================
# MAIN
# ============================================================================
main() {
    print_header "NeuraTrade Autonomous Trading Test Suite"
    echo "Version: 1.0.0"
    echo "Date: $(date)"
    echo ""
    
    check_configuration
    check_database
    check_services
    test_ccxt_market_data
    test_telegram_commands
    test_wallet_exchange
    test_autonomous_readiness
    
    print_summary
}

# Run main
main "$@"
