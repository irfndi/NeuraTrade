#!/bin/bash

# Environment Variable Synchronization Verification Script
# This script helps verify that all required environment variables are properly set
# and synchronized between local and remote environments.

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to print colored output
print_status() {
  local status=$1
  local message=$2
  case $status in
    "OK")
      echo -e "${GREEN}[✓]${NC} $message"
      ;;
    "WARN")
      echo -e "${YELLOW}[!]${NC} $message"
      ;;
    "ERROR")
      echo -e "${RED}[✗]${NC} $message"
      ;;
    "INFO")
      echo -e "${BLUE}[i]${NC} $message"
      ;;
  esac
}

# Function to check if environment variable is set
check_env_var() {
  local var_name=$1
  local is_required=${2:-true}
  local is_secret=${3:-false}

  if [ -z "${!var_name}" ]; then
    if [ "$is_required" = true ]; then
      print_status "ERROR" "Required environment variable $var_name is not set"
      return 1
    else
      print_status "WARN" "Optional environment variable $var_name is not set"
      return 0
    fi
  else
    if [ "$is_secret" = true ]; then
      print_status "OK" "$var_name is set (value hidden for security)"
    else
      print_status "OK" "$var_name is set: ${!var_name}"
    fi
    return 0
  fi
}

# Load environment variables
if [ -f ".env" ]; then
  print_status "INFO" "Loading environment variables from .env file"
  export $(grep -v '^#' .env | xargs)
else
  print_status "ERROR" ".env file not found"
  exit 1
fi

print_status "INFO" "Starting environment variable verification..."
echo

# Track errors
ERROR_COUNT=0

# Application Environment
echo "=== Application Environment ==="
check_env_var "ENVIRONMENT" || ((ERROR_COUNT++))
check_env_var "LOG_LEVEL" || ((ERROR_COUNT++))
check_env_var "LOG_FORMAT" false || ((ERROR_COUNT++))
echo

# Server Configuration
echo "=== Server Configuration ==="
check_env_var "SERVER_PORT" || ((ERROR_COUNT++))
check_env_var "SERVER_HOST" || ((ERROR_COUNT++))
check_env_var "SERVER_ALLOWED_ORIGINS" false || ((ERROR_COUNT++))
echo

# Database Configuration
echo "=== Database Configuration ==="
if [ -n "$DATABASE_URL" ]; then
  check_env_var "DATABASE_URL" true true || ((ERROR_COUNT++))
else
  check_env_var "DATABASE_HOST" || ((ERROR_COUNT++))
  check_env_var "DATABASE_PORT" || ((ERROR_COUNT++))
  check_env_var "DATABASE_USER" || ((ERROR_COUNT++))
  check_env_var "DATABASE_PASSWORD" true true || ((ERROR_COUNT++))
  check_env_var "DATABASE_DBNAME" || ((ERROR_COUNT++))
  check_env_var "DATABASE_SSLMODE" || ((ERROR_COUNT++))
fi
check_env_var "DATABASE_MAX_OPEN_CONNS" false || ((ERROR_COUNT++))
check_env_var "DATABASE_MAX_IDLE_CONNS" false || ((ERROR_COUNT++))
echo

# Redis Configuration
echo "=== Redis Configuration ==="
check_env_var "REDIS_HOST" || ((ERROR_COUNT++))
check_env_var "REDIS_PORT" || ((ERROR_COUNT++))
check_env_var "REDIS_PASSWORD" false true || ((ERROR_COUNT++))
check_env_var "REDIS_DB" || ((ERROR_COUNT++))
echo

# CCXT Service Configuration
echo "=== CCXT Service Configuration ==="
check_env_var "CCXT_SERVICE_URL" || ((ERROR_COUNT++))
check_env_var "CCXT_TIMEOUT" false || ((ERROR_COUNT++))
check_env_var "CCXT_RETRY_ATTEMPTS" false || ((ERROR_COUNT++))
echo

# Telegram Bot Configuration
echo "=== Telegram Bot Configuration ==="
check_env_var "TELEGRAM_BOT_TOKEN" true true || ((ERROR_COUNT++))
check_env_var "TELEGRAM_WEBHOOK_URL" || ((ERROR_COUNT++))
check_env_var "TELEGRAM_WEBHOOK_SECRET" true true || ((ERROR_COUNT++))
echo

# Security Configuration
echo "=== Security Configuration ==="
check_env_var "JWT_SECRET" true true || ((ERROR_COUNT++))
check_env_var "JWT_EXPIRY" false || ((ERROR_COUNT++))
check_env_var "BCRYPT_COST" false || ((ERROR_COUNT++))
check_env_var "ADMIN_API_KEY" true true || ((ERROR_COUNT++))
echo

# External APIs
echo "=== External APIs ==="
check_env_var "COINMARKETCAP_API_KEY" true true || ((ERROR_COUNT++))
check_env_var "COINGECKO_BASE_URL" false || ((ERROR_COUNT++))
echo

# Feature Flags
echo "=== Feature Flags ==="
check_env_var "FEATURE_TELEGRAM_BOT" false || ((ERROR_COUNT++))
check_env_var "FEATURE_WEB_INTERFACE" false || ((ERROR_COUNT++))
check_env_var "FEATURE_API_V1" false || ((ERROR_COUNT++))
check_env_var "FEATURE_REAL_TRADING" false || ((ERROR_COUNT++))
check_env_var "FEATURE_PAPER_TRADING" false || ((ERROR_COUNT++))
echo

# PostgreSQL Runtime Configuration
echo "=== PostgreSQL Runtime Configuration ==="
check_env_var "POSTGRES_USER" || ((ERROR_COUNT++))
check_env_var "POSTGRES_PASSWORD" true true || ((ERROR_COUNT++))
check_env_var "POSTGRES_DB" || ((ERROR_COUNT++))
echo

# Summary
echo "=== Verification Summary ==="
if [ $ERROR_COUNT -eq 0 ]; then
  print_status "OK" "All required environment variables are properly configured"
  print_status "INFO" "Environment is ready for deployment"
  exit 0
else
  print_status "ERROR" "Found $ERROR_COUNT configuration issues"
  print_status "ERROR" "Please fix the issues above before deployment"
  exit 1
fi
