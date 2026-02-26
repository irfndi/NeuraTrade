#!/usr/bin/env bash

# Telegram webhook + external-connection control (native mode)

set -euo pipefail

ENV_FILE="${ENV_FILE:-.env}"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log() {
  echo -e "[$(date '+%Y-%m-%d %H:%M:%S')] $1"
}

log_info() {
  log "${BLUE}[INFO]${NC} $1"
}

log_success() {
  log "${GREEN}[SUCCESS]${NC} $1"
}

log_warn() {
  log "${YELLOW}[WARN]${NC} $1"
}

log_error() {
  log "${RED}[ERROR]${NC} $1"
}

load_env() {
  if [ -f "$ENV_FILE" ]; then
    set -a
    # shellcheck disable=SC1090
    source "$ENV_FILE"
    set +a
  fi
}

update_env_file() {
  local key="$1"
  local value="$2"

  touch "$ENV_FILE"
  if grep -q "^${key}=" "$ENV_FILE"; then
    sed -i.bak "s|^${key}=.*|${key}=${value}|" "$ENV_FILE"
  else
    echo "${key}=${value}" >>"$ENV_FILE"
  fi
}

require_telegram_token() {
  if [ -z "${TELEGRAM_BOT_TOKEN:-}" ]; then
    log_error "TELEGRAM_BOT_TOKEN is not set (env or ${ENV_FILE})"
    return 1
  fi
  return 0
}

register_telegram_webhook() {
  require_telegram_token || return 1

  if [ -z "${TELEGRAM_WEBHOOK_URL:-}" ]; then
    log_error "TELEGRAM_WEBHOOK_URL is not set"
    return 1
  fi

  local payload="url=${TELEGRAM_WEBHOOK_URL}"
  if [ -n "${TELEGRAM_WEBHOOK_SECRET:-}" ]; then
    payload="${payload}&secret_token=${TELEGRAM_WEBHOOK_SECRET}"
  fi

  if curl -fsS -X POST "https://api.telegram.org/bot${TELEGRAM_BOT_TOKEN}/setWebhook" -d "$payload" | grep -q '"ok":true'; then
    log_success "Telegram webhook registered: ${TELEGRAM_WEBHOOK_URL}"
  else
    log_error "Failed to register Telegram webhook"
    return 1
  fi
}

unregister_telegram_webhook() {
  require_telegram_token || return 1

  if curl -fsS -X POST "https://api.telegram.org/bot${TELEGRAM_BOT_TOKEN}/deleteWebhook" | grep -q '"ok":true'; then
    log_success "Telegram webhook unregistered"
  else
    log_error "Failed to unregister Telegram webhook"
    return 1
  fi
}

show_webhook_info() {
  if ! require_telegram_token; then
    return 0
  fi

  log_info "Telegram webhook info:"
  curl -fsS "https://api.telegram.org/bot${TELEGRAM_BOT_TOKEN}/getWebhookInfo" || true
  echo
}

enable_external_connections() {
  log_info "Enabling external connections flags in ${ENV_FILE}"
  update_env_file "EXTERNAL_CONNECTIONS_ENABLED" "true"
  update_env_file "TELEGRAM_WEBHOOK_ENABLED" "true"

  load_env
  if [ -n "${TELEGRAM_WEBHOOK_URL:-}" ]; then
    register_telegram_webhook
  else
    log_warn "TELEGRAM_WEBHOOK_URL not configured; skipping webhook registration"
  fi

  log_success "External connection flags enabled"
}

disable_external_connections() {
  log_info "Disabling external connections flags in ${ENV_FILE}"
  update_env_file "EXTERNAL_CONNECTIONS_ENABLED" "false"
  update_env_file "TELEGRAM_WEBHOOK_ENABLED" "false"

  load_env
  if [ -n "${TELEGRAM_BOT_TOKEN:-}" ]; then
    unregister_telegram_webhook || true
  fi

  log_success "External connection flags disabled"
}

status() {
  load_env

  local external_enabled="false"
  local webhook_enabled="false"

  if [ -f "$ENV_FILE" ]; then
    external_enabled="$(grep '^EXTERNAL_CONNECTIONS_ENABLED=' "$ENV_FILE" | tail -n1 | cut -d'=' -f2- || true)"
    webhook_enabled="$(grep '^TELEGRAM_WEBHOOK_ENABLED=' "$ENV_FILE" | tail -n1 | cut -d'=' -f2- || true)"
  fi

  echo "External Connections: ${external_enabled:-false}"
  echo "Telegram Webhook Flag: ${webhook_enabled:-false}"
  show_webhook_info
}

usage() {
  cat <<USAGE
Usage: $0 {enable|disable|status|webhook-register|webhook-unregister|help}

Commands:
  enable              Set EXTERNAL_CONNECTIONS_ENABLED=true and register webhook
  disable             Set EXTERNAL_CONNECTIONS_ENABLED=false and unregister webhook
  status              Show flag status and Telegram webhook info
  webhook-register    Register webhook only
  webhook-unregister  Unregister webhook only
USAGE
}

main() {
  load_env

  case "${1:-status}" in
    enable)
      enable_external_connections
      ;;
    disable)
      disable_external_connections
      ;;
    status)
      status
      ;;
    webhook-register)
      register_telegram_webhook
      ;;
    webhook-unregister)
      unregister_telegram_webhook
      ;;
    help | -h | --help)
      usage
      ;;
    *)
      usage
      exit 1
      ;;
  esac
}

main "$@"
