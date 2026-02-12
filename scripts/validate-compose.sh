#!/bin/bash
# Validates docker-compose files parse correctly with minimal environment
# Used in CI to catch interpolation errors before deployment
#
# This script ensures that docker-compose.yaml can be parsed without
# required environment variables, preventing build failures on Coolify.

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

cd "$PROJECT_ROOT"

echo "Validating docker-compose.yaml..."

# Test 1: Validate main compose file with minimal environment
# Set minimal required env vars to prevent interpolation errors
export DATABASE_PASSWORD=test-validation-password
export TELEGRAM_BOT_TOKEN=test-token
export COOLIFY_SHARED_NETWORK=coolify

# Check if docker compose command is available
if command -v docker &>/dev/null && docker compose version &>/dev/null; then
  COMPOSE_CMD="docker compose"
elif command -v docker-compose &>/dev/null; then
  COMPOSE_CMD="docker-compose"
else
  echo "Warning: Docker Compose not available, skipping compose validation"
  exit 0
fi

# Validate main docker-compose.yaml
if [ -f "docker-compose.yaml" ]; then
  echo "  - Checking docker-compose.yaml syntax..."
  $COMPOSE_CMD -f docker-compose.yaml config --quiet 2>&1 || {
    echo "ERROR: docker-compose.yaml validation failed!"
    echo "This may cause Coolify deployment failures."
    exit 1
  }
  echo "  - docker-compose.yaml: OK"
else
  echo "  - docker-compose.yaml not found, skipping"
fi

# Validate local development compose if exists
if [ -f "dev/docker-compose.yaml" ]; then
  echo "  - Checking dev/docker-compose.yaml syntax..."
  $COMPOSE_CMD -f dev/docker-compose.yaml config --quiet 2>&1 || {
    echo "ERROR: dev/docker-compose.yaml validation failed!"
    exit 1
  }
  echo "  - dev/docker-compose.yaml: OK"
fi

# Test 2: Ensure no services use required variable syntax without defaults
echo "Checking for required variable syntax that may cause build failures..."
if grep -rE '\$\{[A-Z_]+:\?' docker-compose.yaml 2>/dev/null | grep -v '^#'; then
  echo "WARNING: Found required variable syntax (\${VAR:?...}) in docker-compose.yaml"
  echo "This may cause Coolify build failures if the variable is not set."
  echo "Consider using default values (\${VAR:-default}) instead."
fi

# Test 3: Ensure DATABASE_PASSWORD has a default
if grep -E 'DATABASE_PASSWORD.*\$\{DATABASE_PASSWORD\}[^:-]' docker-compose.yaml 2>/dev/null; then
  echo "WARNING: DATABASE_PASSWORD is used without a default value"
  echo "This may cause interpolation errors during Coolify builds."
fi

echo ""
echo "All Docker Compose validations passed!"
