#!/bin/bash
# =============================================================================
# QMD Retrieval Prototype - Test Suite
# =============================================================================
# Tests for the QMD retrieval prototype script

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SCRIPT="$SCRIPT_DIR/qmd-retrieval.sh"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

PASSED=0
FAILED=0

test_status() {
  local test_name="$1"
  local result

  if result=$("$SCRIPT" status 2>&1); then
    if echo "$result" | grep -q "QMD Retrieval Status"; then
      echo -e "${GREEN}✓ PASSED${NC}: $test_name"
      PASSED=$((PASSED + 1))
    else
      echo -e "${RED}✗ FAILED${NC}: $test_name - unexpected output"
      FAILED=$((FAILED + 1))
    fi
  else
    echo -e "${RED}✗ FAILED${NC}: $test_name - command failed"
    FAILED=$((FAILED + 1))
  fi
}

test_search() {
  local test_name="$1"
  local query="$2"
  local result

  if result=$("$SCRIPT" search "$query" 2>&1); then
    if echo "$result" | grep -q "search time:"; then
      echo -e "${GREEN}✓ PASSED${NC}: $test_name"
      PASSED=$((PASSED + 1))
    else
      echo -e "${RED}✗ FAILED${NC}: $test_name - no timing info"
      FAILED=$((FAILED + 1))
    fi
  else
    echo -e "${RED}✗ FAILED${NC}: $test_name - command failed"
    FAILED=$((FAILED + 1))
  fi
}

test_help() {
  local test_name="$1"
  local result

  if result=$("$SCRIPT" help 2>&1); then
    if echo "$result" | grep -q "Usage:"; then
      echo -e "${GREEN}✓ PASSED${NC}: $test_name"
      PASSED=$((PASSED + 1))
    else
      echo -e "${RED}✗ FAILED${NC}: $test_name - no help output"
      FAILED=$((FAILED + 1))
    fi
  else
    echo -e "${RED}✗ FAILED${NC}: $test_name - command failed"
    FAILED=$((FAILED + 1))
  fi
}

# Run tests
echo "Running QMD Retrieval Prototype Tests..."
echo "========================================"
echo ""

test_status "Status command works"
test_search "Search with 'database'" "database"
test_search "Search with 'health'" "health"
test_help "Help command works"

echo ""
echo "========================================"
echo "Results: $PASSED passed, $FAILED failed"
echo "========================================"

if [[ $FAILED -gt 0 ]]; then
  exit 1
fi

exit 0
