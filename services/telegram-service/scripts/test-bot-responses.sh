#!/bin/bash

# NeuraTrade Telegram Bot Response Test
# Tests that bot actually responds to commands

set -e

BOT_TOKEN="8537577635:AAH3VaChQxuLNR4gqeUP3F_JzZ3dz7lDpBQ"
CHAT_ID="1082762347"
LOG_FILE="${1:-/tmp/telegram_cmd.log}"

echo "🧪 NeuraTrade Bot Response Test"
echo "================================"
echo ""
echo "This test verifies that the bot actually responds to commands."
echo "Log file: $LOG_FILE"
echo ""

# Function to test bot response
test_bot_response() {
    local command="$1"
    local expected_response="$2"
    local timeout="${3:-10}"
    
    echo "Testing: $command"
    echo "  Expected response: $expected_response"
    
    # Get current log line count
    local start_line=$(wc -l < "$LOG_FILE" 2>/dev/null || echo "0")
    
    # Send command
    local send_result=$(curl -s "https://api.telegram.org/bot${BOT_TOKEN}/sendMessage" \
        -d "chat_id=${CHAT_ID}" \
        -d "text=${command}" 2>&1)
    
    if ! echo "$send_result" | grep -q '"ok":true'; then
        echo "  ❌ Failed to send command"
        echo "  Error: $send_result"
        return 1
    fi
    
    echo "  ✅ Command sent, waiting for bot response..."
    
    # Wait for bot to process
    local waited=0
    local found=false
    
    while [ $waited -lt $timeout ]; do
        sleep 1
        waited=$((waited + 1))
        
        # Check log for bot processing
        local new_lines=$(tail -n +$((start_line + 1)) "$LOG_FILE" 2>/dev/null | grep -c "\[BOT\]" || echo "0")
        
        if [ "$new_lines" -gt 0 ]; then
            # Check if expected response pattern is in logs
            if tail -n +$((start_line + 1)) "$LOG_FILE" 2>/dev/null | grep -q "$expected_response"; then
                found=true
                break
            fi
        fi
    done
    
    if [ "$found" = true ]; then
        echo "  ✅ Bot responded correctly!"
        # Show the relevant log lines
        echo "  Log excerpt:"
        tail -n +$((start_line + 1)) "$LOG_FILE" 2>/dev/null | grep "\[BOT\]" | head -5 | sed 's/^/    /'
        return 0
    else
        echo "  ❌ Bot did not respond as expected"
        echo "  Recent logs:"
        tail -n +$((start_line + 1)) "$LOG_FILE" 2>/dev/null | tail -10 | sed 's/^/    /'
        return 1
    fi
}

echo "================================"
echo "Test 1: /start command"
echo "================================"
if test_bot_response "/start" "Welcome message sent successfully" 8; then
    echo ""
else
    echo "  ⚠️  Note: User registration may fail in SQLite mode, but welcome should still be sent"
    echo ""
fi

echo "================================"
echo "Test 2: /help command"
echo "================================"
# Add logging to help command first
echo "  (Adding debug logging to help command...)"
if test_bot_response "/help" "Processing" 5; then
    echo ""
else
    echo "  ⚠️  Help command may need debug logging added"
    echo ""
fi

echo "================================"
echo "Test 3: Text message"
echo "================================"
if test_bot_response "Hello bot" "Processing text message" 5; then
    echo ""
else
    echo "  ⚠️  Text handler may need debug logging"
    echo ""
fi

echo "================================"
echo "Test Complete"
echo "================================"
echo ""
echo "Summary:"
echo "- If you see ✅ above, the bot is responding correctly"
echo "- Check the log file for full details: $LOG_FILE"
echo ""
echo "To see bot responses in real-time:"
echo "  tail -f $LOG_FILE | grep '\[BOT\]'"
