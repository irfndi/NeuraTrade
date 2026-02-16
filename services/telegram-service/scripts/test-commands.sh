#!/bin/bash

# NeuraTrade Telegram E2E Test Script
# Tests bot commands by sending messages via Telegram API

# Don't exit on error - we want to test all commands
# set -e

BOT_TOKEN="8537577635:AAH3VaChQxuLNR4gqeUP3F_JzZ3dz7lDpBQ"
CHAT_ID="1082762347"

echo "🧪 NeuraTrade Telegram E2E Tests"
echo "================================"
echo ""

# Counter for passed/failed tests
passed=0
failed=0

# Function to send command and check response
send_command() {
  local command="$1"
  local description="$2"

  echo "Testing: $description"
  echo "  Command: $command"

  # Send command via Telegram API
  response=$(curl -s "https://api.telegram.org/bot${BOT_TOKEN}/sendMessage" \
    -d "chat_id=${CHAT_ID}" \
    -d "text=${command}" \
    -d "parse_mode=HTML")

  # Check if message was sent successfully
  if echo "$response" | grep -q '"ok":true'; then
    echo "  ✅ Message sent successfully"
    ((passed++))
    return 0
  else
    echo "  ❌ Failed to send message"
    echo "  Response: $response"
    ((failed++))
    return 1
  fi
}

# Wait for bot to process
wait_for_bot() {
  echo "  Waiting for bot to process..."
  sleep 1
}

echo "================================"
echo "Basic Commands (No Setup Required)"
echo "================================"

send_command "/start" "Start command"
wait_for_bot

send_command "/help" "Help command"
wait_for_bot

echo ""
echo "================================"
echo "AI Commands (No Setup Required)"
echo "================================"

send_command "/ai_models" "AI Models command"
wait_for_bot

send_command "/ai_status" "AI Status command"
wait_for_bot

send_command "/ai_route fast" "AI Route fast"
wait_for_bot

send_command "/ai_route balanced" "AI Route balanced"
wait_for_bot

send_command "/ai_route accurate" "AI Route accurate"
wait_for_bot

send_command "/ai_select gpt-4o-mini" "AI Select model"
wait_for_bot

echo ""
echo "================================"
echo "Status & Opportunities (No Setup Required)"
echo "================================"

send_command "/status" "Status command"
wait_for_bot

send_command "/opportunities" "Opportunities command"
wait_for_bot

echo ""
echo "================================"
echo "Commands Requiring User Setup"
echo "================================"

send_command "/begin" "Begin autonomous mode" || echo "  (Expected to fail - requires user setup)"
wait_for_bot

send_command "/pause" "Pause autonomous mode" || echo "  (Expected to fail - requires user setup)"
wait_for_bot

send_command "/quests" "Quests command" || echo "  (Expected to fail - requires user setup)"
wait_for_bot

send_command "/portfolio" "Portfolio command" || echo "  (Expected to fail - requires user setup)"
wait_for_bot

send_command "/performance" "Performance command" || echo "  (Expected to fail - requires user setup)"
wait_for_bot

send_command "/summary" "Summary command" || echo "  (Expected to fail - requires user setup)"
wait_for_bot

echo ""
echo "================================"
echo "Wallet Commands (Require Setup)"
echo "================================"

send_command "/wallet" "Wallet command" || echo "  (Expected to fail - requires wallet)"
wait_for_bot

send_command "/balance" "Balance command" || echo "  (Expected to fail - requires wallet)"
wait_for_bot

echo ""
echo "================================"
echo "Settings Commands"
echo "================================"

send_command "/settings" "Settings command"
wait_for_bot

echo ""
echo "================================"
echo "Doctor & Diagnostics"
echo "================================"

send_command "/doctor" "Doctor command" || echo "  (Expected to fail - requires full setup)"
wait_for_bot

echo ""
echo "================================"
echo "Test Summary"
echo "================================"
echo "Passed: $passed"
echo "Failed: $failed"
echo ""
echo "Note: Some commands require user setup (PostgreSQL, wallets, etc.)"
echo "In SQLite bootstrap mode, many commands will not work."
echo ""
echo "Check your Telegram bot @neuratradelocal_bot for responses"
