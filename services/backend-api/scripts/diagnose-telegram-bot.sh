#!/bin/bash
# Telegram Bot Diagnostic Script
# This script diagnoses Telegram bot connectivity and configuration issues

# Note: Not using 'set -e' to allow all diagnostic checks to run even if some fail

echo "🔍 Telegram Bot Diagnostics"
echo "============================"
echo ""

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Check if .env file exists
if [ ! -f .env ]; then
  echo -e "${RED}❌ .env file not found${NC}"
  echo "Please copy .env.example to .env and configure your settings"
  exit 1
fi

# Load environment variables
set -a
source .env
set +a

echo "📋 Step 1: Configuration Check"
echo "------------------------------"

# Check if bot token is set
if [ -z "$TELEGRAM_BOT_TOKEN" ]; then
  echo -e "${RED}❌ TELEGRAM_BOT_TOKEN is not set${NC}"
  exit 1
else
  TOKEN_LENGTH=${#TELEGRAM_BOT_TOKEN}
  echo -e "${GREEN}✅ TELEGRAM_BOT_TOKEN is set (length: $TOKEN_LENGTH)${NC}"
  # Security: Not displaying any part of the token
fi

# Check webhook URL
if [ -z "$TELEGRAM_WEBHOOK_URL" ]; then
  echo -e "${YELLOW}⚠️  TELEGRAM_WEBHOOK_URL is not set${NC}"
  echo "   Bot will use polling mode"
else
  echo -e "${GREEN}✅ TELEGRAM_WEBHOOK_URL is set${NC}"
  echo "   Webhook URL: $TELEGRAM_WEBHOOK_URL"
fi

echo ""
echo "🌐 Step 2: Telegram API Connectivity Check"
echo "------------------------------------------"

# Test bot token validity by calling getMe
RESPONSE=$(curl -s "https://api.telegram.org/bot${TELEGRAM_BOT_TOKEN}/getMe")

# Check if response contains error
if echo "$RESPONSE" | grep -q '"ok":false'; then
  echo -e "${RED}❌ Bot API call failed${NC}"
  echo "Response: $RESPONSE"
  ERROR_DESC=$(echo "$RESPONSE" | grep -o '"description":"[^"]*"' | cut -d'"' -f4)
  echo "Error: $ERROR_DESC"
  exit 1
else
  echo -e "${GREEN}✅ Bot API connection successful${NC}"
  BOT_NAME=$(echo "$RESPONSE" | grep -o '"first_name":"[^"]*"' | cut -d'"' -f4)
  BOT_USERNAME=$(echo "$RESPONSE" | grep -o '"username":"[^"]*"' | cut -d'"' -f4)
  BOT_ID=$(echo "$RESPONSE" | grep -o '"id":[0-9]*' | cut -d':' -f2)
  echo "   Bot Name: $BOT_NAME"
  echo "   Bot Username: @$BOT_USERNAME"
  echo "   Bot ID: $BOT_ID"
fi

echo ""
echo "📨 Step 3: Webhook Status Check"
echo "-------------------------------"

# Get webhook info
WEBHOOK_RESPONSE=$(curl -s "https://api.telegram.org/bot${TELEGRAM_BOT_TOKEN}/getWebhookInfo")

if echo "$WEBHOOK_RESPONSE" | grep -q '"url":""'; then
  echo -e "${YELLOW}⚠️  No webhook is currently set${NC}"
  echo "   Bot should be running in polling mode"
else
  WEBHOOK_URL=$(echo "$WEBHOOK_RESPONSE" | grep -o '"url":"[^"]*"' | cut -d'"' -f4)
  echo -e "${GREEN}✅ Webhook is configured${NC}"
  echo "   Webhook URL: $WEBHOOK_URL"

  # Check pending updates
  PENDING_COUNT=$(echo "$WEBHOOK_RESPONSE" | grep -o '"pending_update_count":[0-9]*' | cut -d':' -f2)
  if [[ -n "$PENDING_COUNT" ]] && [ "$PENDING_COUNT" -gt 0 ]; then
    echo -e "${YELLOW}⚠️  Pending updates: $PENDING_COUNT${NC}"
  fi

  # Check last error
  LAST_ERROR=$(echo "$WEBHOOK_RESPONSE" | grep -o '"last_error_message":"[^"]*"' | cut -d'"' -f4)
  if [[ -n "$LAST_ERROR" ]]; then
    echo -e "${RED}❌ Last webhook error: $LAST_ERROR${NC}"
  fi
fi

echo ""
echo "🔄 Step 4: Recent Updates Check"
echo "-------------------------------"

# Get recent updates (if polling mode is possible)
UPDATES_RESPONSE=$(curl -s "https://api.telegram.org/bot${TELEGRAM_BOT_TOKEN}/getUpdates?limit=5")

if echo "$UPDATES_RESPONSE" | grep -q '"ok":true'; then
  UPDATE_COUNT=$(echo "$UPDATES_RESPONSE" | grep -o '"result":\[' | wc -l)
  echo -e "${GREEN}✅ Can fetch updates from Telegram${NC}"

  # Count number of updates
  RESULT=$(echo "$UPDATES_RESPONSE" | grep -o '"update_id":[0-9]*' | wc -l)
  echo "   Available updates: $RESULT"

  if [ "$RESULT" -gt 0 ]; then
    echo "   Recent update IDs:"
    echo "$UPDATES_RESPONSE" | grep -o '"update_id":[0-9]*' | cut -d':' -f2 | head -5 | sed 's/^/   - /'
  fi
else
  echo -e "${RED}❌ Failed to fetch updates${NC}"
fi

echo ""
echo "🏥 Step 5: Application Health Check"
echo "-----------------------------------"

# Check if application is running
if [ -z "$SERVER_PORT" ]; then
  SERVER_PORT=8080
fi

HEALTH_URL="http://localhost:${SERVER_PORT}/health"
echo "Checking health endpoint: $HEALTH_URL"

if curl -s -f "$HEALTH_URL" >/dev/null 2>&1; then
  echo -e "${GREEN}✅ Application is running${NC}"
  HEALTH_RESPONSE=$(curl -s "$HEALTH_URL")
  echo "   Health status: $(echo $HEALTH_RESPONSE | grep -o '"status":"[^"]*"' | cut -d'"' -f4)"
else
  echo -e "${YELLOW}⚠️  Application health endpoint not responding${NC}"
  echo "   The application may not be running on port $SERVER_PORT"
fi

echo ""
echo "📊 Step 6: Database Connection Check"
echo "------------------------------------"

# Check database connection
if [[ -n "$DATABASE_URL" ]]; then
  echo "Using DATABASE_URL for connection"
  DB_URL="$DATABASE_URL"
else
  DB_HOST="${DATABASE_HOST:-localhost}"
  DB_PORT="${DATABASE_PORT:-5432}"
  DB_USER="${DATABASE_USER:-postgres}"
  DB_NAME="${DATABASE_DBNAME:-neuratrade}"
  echo "Database: $DB_HOST:$DB_PORT/$DB_NAME as $DB_USER"
fi

# Test database connectivity (requires psql)
if command -v psql &>/dev/null; then
  if [[ -n "$DATABASE_URL" ]]; then
    if psql "$DATABASE_URL" -c "SELECT 1" >/dev/null 2>&1; then
      echo -e "${GREEN}✅ Database connection successful${NC}"
    else
      echo -e "${RED}❌ Database connection failed${NC}"
    fi
  else
    if PGPASSWORD="$DATABASE_PASSWORD" psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -c "SELECT 1" >/dev/null 2>&1; then
      echo -e "${GREEN}✅ Database connection successful${NC}"
    else
      echo -e "${RED}❌ Database connection failed${NC}"
    fi
  fi
else
  echo -e "${YELLOW}⚠️  psql not found, skipping database check${NC}"
fi

echo ""
echo "🔧 Step 7: Configuration Recommendations"
echo "---------------------------------------"

# Check if using polling or webhook
if [ -z "$TELEGRAM_WEBHOOK_URL" ]; then
  echo "📌 Bot Mode: POLLING"
  echo "   Recommendations:"
  echo "   1. Ensure TELEGRAM_USE_POLLING=true in config"
  echo "   2. Bot will poll Telegram servers for updates"
  echo "   3. No external URL required"
  echo "   4. Good for development/testing"
else
  echo "📌 Bot Mode: WEBHOOK"
  echo "   Recommendations:"
  echo "   1. Ensure webhook URL is publicly accessible"
  echo "   2. Webhook must use HTTPS (not HTTP)"
  echo "   3. Set webhook using: scripts/set-telegram-webhook.sh"
  echo "   4. Better for production environments"
fi

echo ""
echo "✅ Diagnostics Complete"
echo "====================="
echo ""
echo "📝 Next Steps:"
echo "1. If bot token is invalid, get a new token from @BotFather"
echo "2. If webhook errors exist, reset webhook with scripts/set-telegram-webhook.sh"
echo "3. If application is not running, start it with: make run"
echo "4. Test the bot by sending /start in Telegram"
echo ""
