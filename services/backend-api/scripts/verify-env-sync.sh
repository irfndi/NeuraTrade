#!/usr/bin/env bash

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

NEURATRADE_HOME="${NEURATRADE_HOME:-$HOME/.neuratrade}"
CONFIG_PATH="${NEURATRADE_HOME}/config.json"
ENV_PATH="${ENV_PATH:-.env}"

warn_count=0
error_count=0

print_status() {
  local status="$1"
  local message="$2"
  case "$status" in
    OK) echo -e "${GREEN}[✓]${NC} $message" ;;
    WARN) echo -e "${YELLOW}[!]${NC} $message" ;;
    ERROR) echo -e "${RED}[✗]${NC} $message" ;;
    INFO) echo -e "${BLUE}[i]${NC} $message" ;;
  esac
}

warn() {
  print_status "WARN" "$1"
  warn_count=$((warn_count + 1))
}

fail() {
  print_status "ERROR" "$1"
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

describe_source() {
  local env_name="$1"
  local config_path="$2"

  if [[ -n "${!env_name:-}" ]]; then
    printf 'environment (%s)' "$env_name"
    return 0
  fi

  if [[ -n "$config_path" ]] && [[ -n "$(read_config_value "$config_path" 2>/dev/null || true)" ]]; then
    printf 'config.json (%s)' "$config_path"
    return 0
  fi

  printf 'default'
}

resolve_telegram_api_base_url() {
  local env_value="${TELEGRAM_API_BASE_URL:-}"
  if [[ -n "${env_value// /}" ]]; then
    printf '%s\n' "${env_value%/}"
    return 0
  fi

  local config_value=""
  config_value="$(read_config_value 'telegram.api_base_url' 2>/dev/null || true)"
  if [[ -z "$config_value" ]]; then
    config_value="$(read_config_value 'services.telegram.api_base_url' 2>/dev/null || true)"
  fi

  if [[ -n "$config_value" && "$config_value" != *"api.telegram.org"* ]]; then
    printf '%s\n' "${config_value%/}"
    return 0
  fi

  local server_host server_port
  server_host="$(read_config_value 'server.host' 2>/dev/null || true)"
  server_port="$(read_config_value 'server.port' 2>/dev/null || true)"
  if [[ -n "$server_port" ]]; then
    printf 'http://%s:%s\n' "${server_host:-localhost}" "$server_port"
    return 0
  fi

  printf 'http://localhost:8080\n'
}

load_dotenv_if_present() {
  if [[ ! -f "$ENV_PATH" ]]; then
    print_status "INFO" "No .env file at ${ENV_PATH}; relying on current shell env and config.json"
    return 0
  fi

  print_status "INFO" "Loading environment variables from ${ENV_PATH}"
  set -a
  source "$ENV_PATH"
  set +a
}

report_value() {
  local label="$1"
  local env_name="$2"
  local config_path="${3:-}"
  local required="${4:-false}"
  local secret="${5:-false}"
  shift 5 || true
  local fallback_values=("$@")

  local config_value=""
  if [[ -n "$config_path" ]]; then
    config_value="$(read_config_value "$config_path" 2>/dev/null || true)"
  fi

  local resolved=""
  resolved="$({
    first_non_empty "${!env_name:-}" "$config_value" "${fallback_values[@]}"
  } | head -n 1 || true)"

  if [[ -n "$resolved" ]]; then
    local source_label
    source_label="$(describe_source "$env_name" "$config_path")"
    if [[ "$secret" == "true" ]]; then
      print_status "OK" "${label}: set via ${source_label} (value hidden)"
    else
      print_status "OK" "${label}: ${resolved} [source: ${source_label}]"
    fi
  elif [[ "$required" == "true" ]]; then
    fail "${label}: missing from environment and config.json"
  else
    warn "${label}: not configured"
  fi

  REPORTED_VALUE="$resolved"
}

echo "🔎 Verifying NeuraTrade native runtime configuration sync..."
print_status "INFO" "Runtime home: ${NEURATRADE_HOME}"
print_status "INFO" "Config path: ${CONFIG_PATH}"
load_dotenv_if_present

if [[ -f "$CONFIG_PATH" ]]; then
  if read_config_value "server.port" >/dev/null 2>&1; then
    print_status "OK" "config.json is present and readable"
  else
    fail "config.json exists but is not valid JSON"
  fi
else
  warn "config.json is missing; native runtime will rely on env/defaults only"
fi
echo

echo "=== Core Runtime ==="
report_value "NEURATRADE_HOME" "NEURATRADE_HOME" "" false false "$HOME/.neuratrade"
report_value "Backend port" "PORT" "server.port" false false "8080"
report_value "Admin API key" "ADMIN_API_KEY" "security.admin_api_key" false true
admin_api_key="$REPORTED_VALUE"
if [[ -z "$admin_api_key" ]]; then
  admin_api_key="$(read_config_value 'admin_api_key' 2>/dev/null || true)"
fi
if [[ -z "$admin_api_key" ]]; then
  admin_api_key="$(read_config_value 'ccxt.admin_api_key' 2>/dev/null || true)"
fi
if [[ -z "$admin_api_key" || ${#admin_api_key} -lt 32 ]]; then
  warn "Admin API key is missing or short; gateway will generate an ephemeral key for local sessions"
fi
report_value "JWT secret" "JWT_SECRET" "security.jwt_secret" false true
if [[ -z "$REPORTED_VALUE" || ${#REPORTED_VALUE} -lt 32 ]]; then
  warn "JWT secret is missing or short; gateway will generate an ephemeral secret for local sessions"
fi
echo

echo "=== Database ==="
report_value "Database driver" "DATABASE_DRIVER" "database.driver" false false "sqlite"
database_driver="$(printf '%s' "$REPORTED_VALUE" | tr '[:upper:]' '[:lower:]')"
case "$database_driver" in
  sqlite | '')
    report_value "SQLite path" "SQLITE_PATH" "database.sqlite_path" false false "$(read_config_value 'database.path' 2>/dev/null || true)" "${NEURATRADE_HOME}/data/neuratrade.db"
    ;;
  postgres)
    report_value "Database URL" "DATABASE_URL" "database.database_url" false true
    database_url="$REPORTED_VALUE"
    if [[ -z "$database_url" ]]; then
      report_value "Postgres user" "POSTGRES_USER" "database.user" true false
      report_value "Postgres password" "POSTGRES_PASSWORD" "database.password" true true
      report_value "Postgres database" "POSTGRES_DB" "database.dbname" true false
    fi
    ;;
  *)
    fail "Unsupported database driver '${database_driver}'"
    ;;
esac
echo

echo "=== Redis ==="
report_value "Redis host" "REDIS_HOST" "redis.host" false false "localhost"
report_value "Redis port" "REDIS_PORT" "redis.port" false false "6379"
report_value "Redis password" "REDIS_PASSWORD" "redis.password" false true
print_status "INFO" "Redis password may be empty in local/native setups"
echo

echo "=== Telegram ==="
report_value "Telegram bot token" "TELEGRAM_BOT_TOKEN" "telegram.bot_token" false true "$(read_config_value 'services.telegram.bot_token' 2>/dev/null || true)"
if [[ -z "$REPORTED_VALUE" ]]; then
  warn "Telegram bot token missing; telegram-service will run in degraded mode"
fi
telegram_api_base_url="$(resolve_telegram_api_base_url)"
print_status "OK" "Telegram API base URL: ${telegram_api_base_url} [source: runtime-resolved]"
report_value "Telegram webhook URL" "TELEGRAM_WEBHOOK_URL" "telegram.webhook_url" false false
report_value "Telegram webhook secret" "TELEGRAM_WEBHOOK_SECRET" "" false true
echo

echo "=== AI ==="
report_value "AI provider" "AI_PROVIDER" "ai.provider" false false
report_value "AI model" "AI_MODEL" "ai.model" false false
report_value "AI API key" "AI_API_KEY" "ai.api_key" false true
echo

echo "=== Summary ==="
if [[ $error_count -gt 0 ]]; then
  fail "Found ${error_count} blocking configuration issue(s) and ${warn_count} warning(s)"
  exit 1
fi

if [[ $warn_count -gt 0 ]]; then
  print_status "WARN" "Configuration is usable with ${warn_count} warning(s)"
else
  print_status "OK" "Configuration sources are synchronized for native runtime"
fi
