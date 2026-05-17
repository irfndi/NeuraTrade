#!/usr/bin/env bash

# NeuraTrade native startup orchestrator (no Docker)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BACKEND_ROOT="$(dirname "$SCRIPT_DIR")"
REPO_ROOT="$(dirname "$(dirname "$BACKEND_ROOT")")"
NEURATRADE_HOME="${NEURATRADE_HOME:-$HOME/.neuratrade}"
LOG_DIR="${NEURATRADE_HOME}/logs"
PID_FILE="${NEURATRADE_HOME}/pids/gateway.pid"
GATEWAY_LOG="${LOG_DIR}/gateway.log"
BACKEND_PORT="${BACKEND_HOST_PORT:-${PORT:-8080}}"
STARTUP_HEALTH_TIMEOUT_SECONDS="${NEURATRADE_STARTUP_HEALTH_TIMEOUT_SECONDS:-${NEURATRADE_GATEWAY_HEALTH_TIMEOUT_SECONDS:-150}}"

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

validated_positive_seconds() {
  local value="${1:-}"
  local default_value="${2:-150}"
  local label="${3:-timeout}"

  value="${value//[[:space:]]/}"
  if ! [[ "$value" =~ ^[0-9]+$ ]] || [ "$value" -le 0 ]; then
    log_warn "Invalid ${label} '${1:-}', defaulting to ${default_value}s" >&2
    printf '%s\n' "$default_value"
    return 0
  fi

  printf '%s\n' "$value"
}

ensure_dirs() {
  mkdir -p "${LOG_DIR}" "${NEURATRADE_HOME}/pids" "${NEURATRADE_HOME}/data"
}

run_sqlite_migrations() {
  local driver
  driver="$(printf '%s' "${DATABASE_DRIVER:-sqlite}" | tr '[:upper:]' '[:lower:]')"
  if [ "$driver" != "sqlite" ]; then
    return 0
  fi

  local migrator
  migrator="${BACKEND_ROOT}/database/sqlite-migrate.sh"
  if [ ! -x "$migrator" ]; then
    log_warn "SQLite migrator not found at ${migrator}; skipping schema upgrade"
    return 0
  fi

  local sqlite_db_path
  sqlite_db_path="${SQLITE_PATH:-${SQLITE_DB_PATH:-${NEURATRADE_HOME}/data/neuratrade.db}}"
  log_info "Applying SQLite migrations to ${sqlite_db_path}"
  SQLITE_PATH="$sqlite_db_path" "$migrator" run >>"${LOG_DIR}/sqlite-migrate.log" 2>&1
  ensure_legacy_sqlite_user_schema "$sqlite_db_path"
}

sqlite_table_exists() {
  local db_path="$1"
  local table_name="$2"
  sqlite3 "$db_path" ".schema ${table_name}" 2>/dev/null | grep -q "CREATE TABLE"
}

sqlite_column_exists() {
  local db_path="$1"
  local table_name="$2"
  local column_name="$3"
  sqlite3 "$db_path" "PRAGMA table_info(${table_name});" 2>/dev/null | cut -d'|' -f2 | grep -qx "$column_name"
}

ensure_legacy_sqlite_user_schema() {
  local db_path="$1"
  if [ ! -f "$db_path" ] || ! sqlite_table_exists "$db_path" "users"; then
    return 0
  fi

  local changed=0
  ensure_column() {
    local column_name="$1"
    local column_def="$2"
    if ! sqlite_column_exists "$db_path" "users" "$column_name"; then
      sqlite3 "$db_path" "ALTER TABLE users ADD COLUMN ${column_def};"
      changed=1
    fi
  }

  ensure_column "email" "email TEXT NOT NULL DEFAULT ''"
  ensure_column "password_hash" "password_hash TEXT NOT NULL DEFAULT ''"
  ensure_column "telegram_chat_id" "telegram_chat_id TEXT"
  ensure_column "subscription_tier" "subscription_tier TEXT NOT NULL DEFAULT 'free'"
  ensure_column "updated_at" "updated_at DATETIME"
  ensure_column "selected_ai_model" "selected_ai_model TEXT"
  ensure_column "telegram_blocked" "telegram_blocked INTEGER DEFAULT 0"
  ensure_column "telegram_blocked_at" "telegram_blocked_at DATETIME"

  if sqlite_column_exists "$db_path" "users" "telegram_id" && sqlite_column_exists "$db_path" "users" "telegram_chat_id"; then
    sqlite3 "$db_path" "UPDATE users SET telegram_chat_id = telegram_id, updated_at = CURRENT_TIMESTAMP WHERE COALESCE(telegram_chat_id, '') = '' AND COALESCE(telegram_id, '') != '';"
  fi
  if sqlite_column_exists "$db_path" "users" "updated_at"; then
    sqlite3 "$db_path" "UPDATE users SET updated_at = COALESCE(updated_at, created_at, CURRENT_TIMESTAMP);"
  fi

  sqlite3 "$db_path" "CREATE INDEX IF NOT EXISTS idx_users_telegram_chat_id ON users(telegram_chat_id);"
  sqlite3 "$db_path" "CREATE INDEX IF NOT EXISTS idx_users_telegram_blocked ON users(telegram_blocked) WHERE telegram_blocked = 1;"

  if [ "$changed" -eq 1 ]; then
    log_info "Applied legacy SQLite users schema compatibility upgrades"
  fi
}

find_gateway_cmd() {
  if [ -x "${REPO_ROOT}/bin/neuratrade" ]; then
    echo "${REPO_ROOT}/bin/neuratrade gateway start"
    return 0
  fi

  if command -v neuratrade >/dev/null 2>&1; then
    echo "neuratrade gateway start"
    return 0
  fi

  if [ -f "${REPO_ROOT}/cmd/neuratrade-cli/main.go" ]; then
    echo "go run ./cmd/neuratrade-cli gateway start"
    return 0
  fi

  return 1
}

launch_gateway_detached() {
  local gateway_cmd="$1"

  if command -v setsid >/dev/null 2>&1; then
    nohup setsid bash -c "exec ${gateway_cmd}" >>"${GATEWAY_LOG}" 2>&1 </dev/null &
    printf '%s\n' "$!"
    return 0
  fi

  if command -v python3 >/dev/null 2>&1; then
    GATEWAY_CMD="$gateway_cmd" GATEWAY_LOG="$GATEWAY_LOG" python3 - <<'PY'
import os
import subprocess

cmd = "exec " + os.environ["GATEWAY_CMD"]
log_path = os.environ["GATEWAY_LOG"]
log = open(log_path, "ab", buffering=0)
process = subprocess.Popen(
    ["bash", "-c", cmd],
    stdin=subprocess.DEVNULL,
    stdout=log,
    stderr=log,
    close_fds=True,
    start_new_session=True,
)
print(process.pid)
PY
    return 0
  fi

  nohup bash -c "exec ${gateway_cmd}" >>"${GATEWAY_LOG}" 2>&1 </dev/null &
  printf '%s\n' "$!"
}

pid_running() {
  local pid="$1"
  kill -0 "$pid" 2>/dev/null
}

pid_command() {
  local pid="$1"
  ps -p "$pid" -o command= 2>/dev/null || true
}

pid_command_matches() {
  local pid="$1"
  local pattern="$2"
  local command
  command="$(pid_command "$pid")"
  [ -n "$command" ] && printf '%s' "$command" | grep -Fq "$pattern"
}

gateway_pid() {
  if [ -f "${PID_FILE}" ]; then
    tr -d '[:space:]' <"${PID_FILE}"
  fi
}

wait_backend_health() {
  local timeout="${1:-45}"
  local elapsed=0
  local url="http://127.0.0.1:${BACKEND_PORT}/health"

  while [ "$elapsed" -lt "$timeout" ]; do
    if curl -fsS "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
    elapsed=$((elapsed + 1))
  done

  return 1
}

start_gateway() {
  ensure_dirs
  run_sqlite_migrations

  local current_pid
  current_pid="$(gateway_pid || true)"
  if [ -n "${current_pid:-}" ] && pid_running "$current_pid"; then
    log_warn "Gateway is already running (pid=${current_pid})"
    status_gateway
    return 0
  fi

  local gateway_cmd
  if ! gateway_cmd="$(find_gateway_cmd)"; then
    log_error "Unable to find neuratrade CLI. Install it or build ./bin/neuratrade first."
    return 1
  fi

	log_info "Starting gateway using: ${gateway_cmd}"
	local launched_pid
	local launched_pid_file
	launched_pid_file="$(mktemp /tmp/neuratrade-gateway-pid.XXXXXX)"
	if ! (
		set -e
		cd "$REPO_ROOT"
		export PATH="${REPO_ROOT}/bin:${PATH}"
		launch_gateway_detached "$gateway_cmd" >"$launched_pid_file"
	); then
		rm -f "$launched_pid_file"
		log_error "Failed to launch gateway"
		return 1
	fi
	IFS= read -r launched_pid <"$launched_pid_file" || launched_pid=""
	rm -f "$launched_pid_file"
	if ! printf '%s' "$launched_pid" | grep -Eq '^[0-9]+$'; then
		log_error "Failed to capture launched gateway pid"
		return 1
	fi
	printf '%s\n' "$launched_pid" >"${PID_FILE}"

  local pid
  pid="$(gateway_pid)"
  log_info "Gateway launched (pid=${pid}), waiting for backend health..."
  local startup_timeout
  startup_timeout="$(validated_positive_seconds "${STARTUP_HEALTH_TIMEOUT_SECONDS}" 150 "startup health timeout")"

  sleep 1
  if ! pid_running "$pid"; then
    log_error "Gateway process exited immediately (pid=${pid})"
    log_error "Inspect logs: ${GATEWAY_LOG}"
    rm -f "${PID_FILE}"
    return 1
  fi

  if wait_backend_health "${startup_timeout}"; then
    if pid_running "$pid"; then
      log_success "Gateway is running and backend health endpoint is reachable"
    else
      log_error "Backend health is reachable but gateway process exited (pid=${pid})"
      log_error "Inspect logs: ${GATEWAY_LOG}"
      rm -f "${PID_FILE}"
      return 1
    fi
  else
    log_warn "Gateway started, but backend health endpoint is not ready yet"
    log_warn "Check logs: tail -f ${GATEWAY_LOG}"
  fi
}

stop_gateway() {
  local pid
  pid="$(gateway_pid || true)"

  if [ -z "${pid:-}" ]; then
    log_warn "No gateway pid file found"
    stop_gateway_child_services
    log_success "Gateway stopped"
    return 0
  fi

  if ! pid_running "$pid"; then
    log_warn "Stale gateway pid file found (pid=${pid}), removing"
    rm -f "${PID_FILE}"
    stop_gateway_child_services
    log_success "Gateway stopped"
    return 0
  fi

  log_info "Stopping gateway (pid=${pid})"
  kill "$pid" 2>/dev/null || true

  local waited=0
  while pid_running "$pid" && [ "$waited" -lt 20 ]; do
    sleep 1
    waited=$((waited + 1))
  done

  if pid_running "$pid"; then
    log_warn "Gateway still running after grace period, forcing kill"
    kill -9 "$pid" 2>/dev/null || true
  fi

  rm -f "${PID_FILE}"
  stop_gateway_child_services
  log_success "Gateway stopped"
}

stop_gateway_child_services() {
  stop_child_service "Backend API" "${NEURATRADE_HOME}/pids/backend.pid" "neuratrade-server"
  stop_child_service "CCXT Service" "${NEURATRADE_HOME}/pids/ccxt.pid" "ccxt-service"
  stop_child_service "Telegram Service" "${NEURATRADE_HOME}/pids/telegram.pid" "telegram-service" "bun run index.ts"
}

stop_child_service() {
  local name="$1"
  local pid_file="$2"
  shift 2
  local expected_patterns=("$@")

  if [ ! -f "$pid_file" ]; then
    return 0
  fi

  local child_pid
  IFS= read -r child_pid <"$pid_file" || child_pid=""
  if [ -z "$child_pid" ] || ! [[ "$child_pid" =~ ^[0-9]+$ ]]; then
    log_warn "${name} pid file is invalid, removing: ${pid_file}"
    rm -f "$pid_file"
    return 0
  fi

  if ! pid_running "$child_pid"; then
    rm -f "$pid_file"
    return 0
  fi

  local pattern
  local matches=0
  for pattern in "${expected_patterns[@]}"; do
    if pid_command_matches "$child_pid" "$pattern"; then
      matches=1
      break
    fi
  done
  if [ "$matches" -ne 1 ]; then
    log_warn "${name} pid ${child_pid} does not match expected command patterns; leaving process running and removing stale pid file"
    rm -f "$pid_file"
    return 0
  fi

  log_info "Stopping ${name} (pid=${child_pid})"
  kill "$child_pid" 2>/dev/null || true

  local waited=0
  while pid_running "$child_pid" && [ "$waited" -lt 20 ]; do
    sleep 1
    waited=$((waited + 1))
  done

  if pid_running "$child_pid"; then
    log_warn "${name} still running after grace period, forcing kill"
    kill -9 "$child_pid" 2>/dev/null || true
  fi

  rm -f "$pid_file"
}

status_gateway() {
  local pid
  pid="$(gateway_pid || true)"

  if [ -n "${pid:-}" ] && pid_running "$pid"; then
    log_success "Gateway process is running (pid=${pid})"
  else
    log_warn "Gateway process is not running"
  fi

  local health_url="http://127.0.0.1:${BACKEND_PORT}/health"
  if curl -fsS "$health_url" >/dev/null 2>&1; then
    log_success "Backend health is reachable at ${health_url}"
  else
    log_warn "Backend health is not reachable at ${health_url}"
  fi

  echo ""
  echo "Recent gateway logs:"
  if [ -f "${GATEWAY_LOG}" ]; then
    tail -n 20 "${GATEWAY_LOG}"
  else
    echo "No gateway log file found at ${GATEWAY_LOG}"
  fi
}

logs_gateway() {
  ensure_dirs
  touch "${GATEWAY_LOG}"
  tail -f "${GATEWAY_LOG}"
}

usage() {
  cat <<USAGE
Usage: $0 [start|stop|restart|status|logs|help]

Commands:
  start    Start NeuraTrade gateway in background (native mode)
  stop     Stop gateway and managed services gracefully
  restart  Stop then start gateway
  status   Show process + backend health status
  logs     Follow gateway logs
  help     Show this message
USAGE
}

main() {
  case "${1:-start}" in
    start)
      start_gateway
      ;;
    stop)
      stop_gateway
      ;;
    restart)
      stop_gateway
      start_gateway
      ;;
    status)
      status_gateway
      ;;
    logs)
      logs_gateway
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
