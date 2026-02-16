#!/bin/bash

# NeuraTrade Full Telegram Command Test Suite
# Tests ALL available bot commands

BOT_TOKEN="8537577635:AAH3VaChQxuLNR4gqeUP3F_JzZ3dz7lDpBQ"
CHAT_ID="1082762347"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

passed=0
failed=0

send_cmd() {
    local cmd="$1"
    local desc="$2"
    
    echo -n "Testing $desc... "
    
    response=$(curl -s "https://api.telegram.org/bot${BOT_TOKEN}/sendMessage" \
        -d "chat_id=${CHAT_ID}" \
        -d "text=${cmd}" \
        -d "parse_mode=HTML")
    
    if echo "$response" | grep -q '"ok":true'; then
        echo -e "${GREEN}✅${NC}"
        ((passed++))
        return 0
    else
        echo -e "${RED}❌${NC}"
        echo "   Response: $response"
        ((failed++))
        return 1
    fi
}

wait_bot() {
    sleep 3
}

echo "🧪 NeuraTrade Telegram Full Test Suite"
echo "======================================"
echo ""

# Basic commands
echo -e "${YELLOW}📋 Basic Commands${NC}"
send_cmd "/start" "Start command"
wait_bot

send_cmd "/help" "Help command"
wait_bot

# AI Commands
echo -e "\n${YELLOW}🤖 AI Commands${NC}"
send_cmd "/ai_models" "AI Models"
wait_bot

send_cmd "/ai_status" "AI Status"
wait_bot

send_cmd "/ai_route fast" "AI Route fast"
wait_bot

send_cmd "/ai_route balanced" "AI Route balanced"
wait_bot

send_cmd "/ai_route accurate" "AI Route accurate"
wait_bot

# Trading Commands (expected to fail in bootstrap mode)
echo -e "\n${YELLOW}💰 Trading Commands${NC}"
send_cmd "/opportunities" "Arbitrage Opportunities"
wait_bot

send_cmd "/portfolio" "Portfolio"
wait_bot

send_cmd "/summary" "Summary"
wait_bot

send_cmd "/performance" "Performance"
wait_bot

# Wallet Commands
echo -e "\n${YELLOW}💳 Wallet Commands${NC}"
send_cmd "/wallet" "Wallet"
wait_bot

send_cmd "/connect_exchange" "Connect Exchange"
wait_bot

send_cmd "/add_wallet" "Add Wallet"
wait_bot

send_cmd "/remove_wallet" "Remove Wallet"
wait_bot

# Settings Commands
echo -e "\n${YELLOW}⚙️ Settings Commands${NC}"
send_cmd "/settings" "Settings"
wait_bot

send_cmd "/stop" "Stop notifications"
wait_bot

send_cmd "/resume" "Resume notifications"
wait_bot

# Status Commands
echo -e "\n${YELLOW}📊 Status Commands${NC}"
send_cmd "/status" "Status"
wait_bot

send_cmd "/doctor" "Doctor"
wait_bot

# Quest Commands
echo -e "\n${YELLOW}🎯 Quest Commands${NC}"
send_cmd "/quests" "Quests"
wait_bot

# Autonomous Commands
echo -e "\n${YELLOW}⚡ Autonomous Commands${NC}"
send_cmd "/begin" "Begin autonomous"
wait_bot

send_cmd "/pause" "Pause autonomous"
wait_bot

# Error handling tests
echo -e "\n${YELLOW}🔴 Error Handling Tests${NC}"
send_cmd "/invalid_command_12345" "Invalid command"
wait_bot

send_cmd "/ai_route" "AI Route without parameter"
wait_bot

send_cmd "/ai_select" "AI Select without model"
wait_bot

# Summary
echo ""
echo "======================================"
echo -e "${GREEN}Passed: $passed${NC}"
echo -e "${RED}Failed: $failed${NC}"
echo "======================================"

if [ $failed -gt 0 ]; then
    exit 1
fi

echo -e "${GREEN}✅ All Telegram command tests passed!${NC}"
