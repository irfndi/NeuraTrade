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

ensure_dirs() {
  mkdir -p "${LOG_DIR}" "${NEURATRADE_HOME}/pids" "${NEURATRADE_HOME}/data"
}

find_gateway_cmd() {
  if command -v neuratrade >/dev/null 2>&1; then
    echo "neuratrade gateway start"
    return 0
  fi

  if [ -x "${REPO_ROOT}/bin/neuratrade" ]; then
    echo "${REPO_ROOT}/bin/neuratrade gateway start"
    return 0
  fi

  if [ -f "${REPO_ROOT}/cmd/neuratrade-cli/main.go" ]; then
    echo "go run ./cmd/neuratrade-cli gateway start"
    return 0
  fi

  return 1
}

pid_running() {
  local pid="$1"
  kill -0 "$pid" 2>/dev/null
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
  (
    cd "$REPO_ROOT"
    nohup bash -lc "$gateway_cmd" >>"${GATEWAY_LOG}" 2>&1 &
    echo $! >"${PID_FILE}"
  )

  local pid
  pid="$(gateway_pid)"
  log_info "Gateway launched (pid=${pid}), waiting for backend health..."

  if wait_backend_health 60; then
    log_success "Gateway is running and backend health endpoint is reachable"
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
    return 0
  fi

  if ! pid_running "$pid"; then
    log_warn "Stale gateway pid file found (pid=${pid}), removing"
    rm -f "${PID_FILE}"
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
  log_success "Gateway stopped"
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
    help|-h|--help)
      usage
      ;;
    *)
      usage
      exit 1
      ;;
  esac
}

main "$@"
