#!/usr/bin/env bash

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

NEURATRADE_HOME="${NEURATRADE_HOME:-$HOME/.neuratrade}"
CONFIG_PATH="${NEURATRADE_HOME}/config.json"

warn_count=0
error_count=0

info() {
  echo -e "${BLUE}ℹ${NC} $1"
}

ok() {
  echo -e "${GREEN}✅${NC} $1"
}

warn() {
  echo -e "${YELLOW}⚠️${NC}  $1"
  warn_count=$((warn_count + 1))
}

fail() {
  echo -e "${RED}❌${NC} $1"
  error_count=$((error_count + 1))
}

read_config_value() {
  local path="$1"

  if [[ ! -f "$CONFIG_PATH" ]]; then
    return 0
  fi

  python3 - "$CONFIG_PATH" "$path" <<'PY'
import json
import sys

config_path = sys.argv[1]
path = sys.argv[2].split('.')

try:
    with open(config_path, 'r', encoding='utf-8') as handle:
        data = json.load(handle)
except Exception:
    sys.exit(2)

value = data
for part in path:
    if isinstance(value, dict) and part in value:
        value = value[part]
    else:
        sys.exit(0)

if value is None:
    sys.exit(0)
if isinstance(value, bool):
    print(str(value).lower())
elif isinstance(value, (int, float)):
    print(value)
elif isinstance(value, str):
    print(value)
PY
}

first_non_empty() {
  local value
  for value in "$@"; do
    if [[ -n "${value// /}" ]]; then
      printf '%s\n' "$value"
      return 0
    fi
  done
  return 1
}

echo "🔍 Validating NeuraTrade native runtime configuration..."
info "Primary local runtime config: ${CONFIG_PATH}"

if [[ -f "$CONFIG_PATH" ]]; then
  if read_config_value "server.port" >/dev/null 2>&1; then
    ok "config.json is present and readable"
  else
    fail "config.json exists but is not valid JSON"
  fi
else
  warn "config.json not found; startup will rely on environment variables and built-in defaults"
fi

database_driver="$({
  first_non_empty \
    "${DATABASE_DRIVER:-}" \
    "$(read_config_value database.driver 2>/dev/null || true)" \
    "sqlite"
} | head -n 1)"
database_driver="$(printf '%s' "$database_driver" | tr '[:upper:]' '[:lower:]')"
ok "database driver resolved to '${database_driver}'"

case "$database_driver" in
  sqlite)
    sqlite_path="$({
      first_non_empty \
        "${SQLITE_PATH:-}" \
        "${SQLITE_DB_PATH:-}" \
        "$(read_config_value database.sqlite_path 2>/dev/null || true)" \
        "$(read_config_value database.path 2>/dev/null || true)" \
        "${NEURATRADE_HOME}/data/neuratrade.db"
    } | head -n 1)"
    ok "SQLite path resolved to '${sqlite_path}'"
    ;;
  postgres)
    database_url="$({
      first_non_empty \
        "${DATABASE_URL:-}" \
        "$(read_config_value database.database_url 2>/dev/null || true)"
    } | head -n 1 || true)"
    postgres_user="$({
      first_non_empty \
        "${POSTGRES_USER:-}" \
        "$(read_config_value database.user 2>/dev/null || true)"
    } | head -n 1 || true)"
    postgres_password="$({
      first_non_empty \
        "${POSTGRES_PASSWORD:-}" \
        "$(read_config_value database.password 2>/dev/null || true)"
    } | head -n 1 || true)"
    postgres_db="$({
      first_non_empty \
        "${POSTGRES_DB:-}" \
        "$(read_config_value database.dbname 2>/dev/null || true)"
    } | head -n 1 || true)"

    if [[ -n "$database_url" ]]; then
      ok "PostgreSQL connection string is configured"
    else
      [[ -n "$postgres_user" ]] && ok "POSTGRES_USER/database.user is configured" || fail "PostgreSQL driver selected but database user is missing"
      [[ -n "$postgres_password" ]] && ok "POSTGRES_PASSWORD/database.password is configured" || fail "PostgreSQL driver selected but database password is missing"
      [[ -n "$postgres_db" ]] && ok "POSTGRES_DB/database.dbname is configured" || fail "PostgreSQL driver selected but database name is missing"
    fi
    ;;
  *)
    fail "Unsupported database driver '${database_driver}'"
    ;;
esac

telegram_token="$({
  first_non_empty \
    "${TELEGRAM_BOT_TOKEN:-}" \
    "${TELEGRAM_TOKEN:-}" \
    "$(read_config_value telegram.bot_token 2>/dev/null || true)" \
    "$(read_config_value services.telegram.bot_token 2>/dev/null || true)"
} | head -n 1 || true)"
if [[ -n "$telegram_token" ]]; then
  ok "Telegram bot token is configured"
else
  warn "Telegram bot token is missing; telegram-service will start in degraded mode"
fi

jwt_secret="$({
  first_non_empty \
    "${JWT_SECRET:-}" \
    "$(read_config_value security.jwt_secret 2>/dev/null || true)"
} | head -n 1 || true)"
if [[ -n "$jwt_secret" && ${#jwt_secret} -ge 32 ]]; then
  ok "JWT secret is configured"
else
  warn "JWT secret is missing or short; gateway will generate an ephemeral secret for this session"
fi

admin_api_key="$({
  first_non_empty \
    "${ADMIN_API_KEY:-}" \
    "$(read_config_value security.admin_api_key 2>/dev/null || true)" \
    "$(read_config_value admin_api_key 2>/dev/null || true)" \
    "$(read_config_value ccxt.admin_api_key 2>/dev/null || true)"
} | head -n 1 || true)"
if [[ -n "$admin_api_key" && ${#admin_api_key} -ge 32 ]]; then
  ok "Admin API key is configured"
else
  warn "Admin API key is missing or short; gateway will generate an ephemeral key for this session"
fi

redis_password="$({
  first_non_empty \
    "${REDIS_PASSWORD:-}" \
    "$(read_config_value redis.password 2>/dev/null || true)"
} | head -n 1 || true)"
if [[ -n "$redis_password" ]]; then
  ok "Redis password is configured"
else
  info "Redis password is unset; this is acceptable for local/native setups"
fi

if [[ $error_count -gt 0 ]]; then
  echo -e "${RED}❌ Validation failed with ${error_count} error(s) and ${warn_count} warning(s).${NC}"
  exit 1
fi

if [[ $warn_count -gt 0 ]]; then
  echo -e "${YELLOW}⚠️  Validation passed with ${warn_count} warning(s).${NC}"
else
  echo -e "${GREEN}✅ Validation passed with no issues.${NC}"
fi
