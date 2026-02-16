#!/bin/bash

# NeuraTrade Full E2E Test Runner
# Runs all E2E tests: API + Telegram Commands

set -e

echo "🚀 Starting NeuraTrade E2E Test Suite"
echo "======================================"
echo ""

# Check if services are running
echo "Checking services..."

# Check backend
if ! curl -s http://localhost:8080/health > /dev/null 2>&1; then
    echo "❌ Backend not running on port 8080"
    echo "   Run: cd services/backend-api && go run ./cmd/server"
    exit 1
fi
echo "✅ Backend: OK"

# Check CCXT
if ! curl -s http://localhost:3001/health > /dev/null 2>&1; then
    echo "❌ CCXT service not running on port 3001"
    echo "   Run: cd services/ccxt-service && bun run index.ts"
    exit 1
fi
echo "✅ CCXT: OK"

# Check Telegram
if ! curl -s http://localhost:3002/health > /dev/null 2>&1; then
    echo "❌ Telegram service not running on port 3002"
    echo "   Run: cd services/telegram-service && bun run index.ts"
    exit 1
fi
echo "✅ Telegram: OK"

echo ""
echo "======================================"
echo "1. Running API Tests (Go)"
echo "======================================"

cd "$(dirname "$0")/../backend-api"

# Build and run API tests
go run e2e/api_test.go

echo ""
echo "======================================"
echo "2. Running Telegram Command Tests"
echo "======================================"

# Note: Telegram tests require actual bot to be polling
echo "Sending commands to Telegram bot..."
echo ""

# Source the command test script
source "$(dirname "$0")/telegram-service/scripts/test-commands.sh"

echo ""
echo "======================================"
echo "✅ E2E Test Suite Completed!"
echo "======================================"
