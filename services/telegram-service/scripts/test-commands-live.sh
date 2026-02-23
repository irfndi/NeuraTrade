#!/bin/bash

# NeuraTrade Bot Live Response Test
# Tests all bot commands and captures actual responses

# SECURITY: Use environment variable for bot token
# Export before running: export TELEGRAM_BOT_TOKEN="your-bot-token"
BOT_TOKEN="${TELEGRAM_BOT_TOKEN:-}"
if [ -z "$BOT_TOKEN" ]; then
  echo "❌ ERROR: TELEGRAM_BOT_TOKEN environment variable is not set"
  echo "   Set it with: export TELEGRAM_BOT_TOKEN=\"your-bot-token\""
  exit 1
fi

# Use environment variable for chat ID or default
CHAT_ID="${TELEGRAM_CHAT_ID:-1082762347}"
LOG_FILE="/tmp/telegram_live_test.log"

echo "═══════════════════════════════════════════════════════════"
echo "  NeuraTrade Bot Live Response Test"
echo "═══════════════════════════════════════════════════════════"
echo ""
echo "This will send commands to @neuratradelocal_bot"
echo "and capture actual responses from the bot."
echo ""
echo "Commands will be sent with 3-second delays"
echo "to allow bot processing time."
echo ""

# Create fresh log
echo "Test started at $(date)" >"$LOG_FILE"

# Function to send command and show result
send_command() {
  local cmd="$1"
  local desc="$2"

  echo ""
  echo "─────────────────────────────────────────"
  echo "Command: $cmd"
  echo "Description: $desc"
  echo "─────────────────────────────────────────"

  # Get line count before sending
  local start_line=$(wc -l <"$LOG_FILE" 2>/dev/null || echo "0")

  # Send command
  local response=$(curl -s "https://api.telegram.org/bot${BOT_TOKEN}/sendMessage" \
    -d "chat_id=${CHAT_ID}" \
    -d "text=${cmd}")

  if echo "$response" | grep -q '"ok":true'; then
    echo "✅ Sent: $cmd"

    # Wait for bot response
    sleep 3

    # Check for bot activity in logs
    local new_content=$(tail -n +$((start_line + 1)) "$LOG_FILE" 2>/dev/null)

    if echo "$new_content" | grep -q "\[BOT\]"; then
      echo "🤖 Bot responded:"
      echo "$new_content" | grep "\[BOT\]" | sed 's/^/   /'
    else
      echo "⏳ No bot response captured in logs (may need more time)"
    fi
  else
    echo "❌ Failed to send: $cmd"
    echo "   Error: $(echo "$response" | grep -o '"description":"[^"]*"' || echo "Unknown")"
  fi
}

echo "═══════════════════════════════════════════════════════════"
echo "PHASE 1: Basic Commands (Should Work)"
echo "═══════════════════════════════════════════════════════════"

send_command "/start" "Welcome message"
send_command "/help" "Help menu"
send_command "/ai_models" "List AI models"
send_command "/ai_status" "AI status"
send_command "/ai_route fast" "Route AI - fast"
send_command "/ai_route balanced" "Route AI - balanced"
send_command "/ai_route accurate" "Route AI - accurate"

echo ""
echo "═══════════════════════════════════════════════════════════"
echo "PHASE 2: System Commands (Should Work)"
echo "═══════════════════════════════════════════════════════════"

send_command "/status" "System status"
send_command "/opportunities" "Trading opportunities"
send_command "/settings" "Bot settings"

echo ""
echo "═══════════════════════════════════════════════════════════"
echo "PHASE 3: Wallet Commands (Requires PostgreSQL)"
echo "═══════════════════════════════════════════════════════════"

send_command "/wallet" "List wallets"
send_command "/add_wallet 0x1234567890abcdef" "Add test wallet"
send_command "/balance" "Check balance"

echo ""
echo "═══════════════════════════════════════════════════════════"
echo "PHASE 4: Trading Commands (Requires Setup)"
echo "═══════════════════════════════════════════════════════════"

send_command "/portfolio" "Portfolio view"
send_command "/performance" "Performance stats"
send_command "/summary" "24h summary"
send_command "/doctor" "System diagnostics"

echo ""
echo "═══════════════════════════════════════════════════════════"
echo "PHASE 5: Autonomous Trading (Requires Wallet)"
echo "═══════════════════════════════════════════════════════════"

send_command "/begin" "Start autonomous mode"
send_command "/quests" "View active quests"
send_command "/pause" "Pause autonomous mode"

echo ""
echo "═══════════════════════════════════════════════════════════"
echo "Test Complete!"
echo "═══════════════════════════════════════════════════════════"
echo ""
echo "Summary:"
echo "- Check your Telegram chat @neuratradelocal_bot for actual responses"
echo "- Log file: $LOG_FILE"
echo ""
echo "For autonomous trading stream test, you need:"
echo "1. PostgreSQL database (not SQLite)"
echo "2. Connected exchange API keys"
echo "3. Trading wallet with funds"
echo "4. Run /begin to start autonomous mode"
echo ""

# Show log location
echo "Full log available at: $LOG_FILE"
echo "To view: tail -f $LOG_FILE | grep '\[BOT\]'"
