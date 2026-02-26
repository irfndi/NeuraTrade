#!/usr/bin/env bash

# Native health monitor for NeuraTrade services

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
NEURATRADE_HOME="${NEURATRADE_HOME:-$HOME/.neuratrade}"
LOG_DIR="${NEURATRADE_HOME}/logs"
LOG_FILE="${LOG_DIR}/health-monitor.log"
PIDS_DIR="${NEURATRADE_HOME}/pids"
MONITOR_INTERVAL="${MONITOR_INTERVAL:-30}"
BACKEND_PORT="${BACKEND_HOST_PORT:-${PORT:-8080}}"
HEALTH_URL="${HEALTH_URL:-http://127.0.0.1:${BACKEND_PORT}/health}"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log() {
  mkdir -p "$LOG_DIR"
  echo -e "[$(date '+%Y-%m-%d %H:%M:%S')] $1" | tee -a "$LOG_FILE"
}

log_info() {
  log "${BLUE}[INFO]${NC} $1"
}

log_success() {
  log "${GREEN}[OK]${NC} $1"
}

log_warn() {
  log "${YELLOW}[WARN]${NC} $1"
}

log_error() {
  log "${RED}[ERR]${NC} $1"
}

pid_running() {
  local pid="$1"
  kill -0 "$pid" 2>/dev/null
}

check_pid_file() {
  local name="$1"
  local file="$2"

  if [ ! -f "$file" ]; then
    log_warn "$name: pid file missing"
    return 1
  fi

  local pid
  pid="$(tr -d '[:space:]' <"$file")"
  if [ -z "$pid" ]; then
    log_warn "$name: pid file empty"
    return 1
  fi

  if pid_running "$pid"; then
    log_success "$name: running (pid=$pid)"
    return 0
  fi

  log_warn "$name: stale pid file (pid=$pid)"
  return 1
}

check_backend_health() {
  if curl -fsS "$HEALTH_URL" >/dev/null 2>&1; then
    log_success "backend health reachable (${HEALTH_URL})"
    return 0
  fi

  log_error "backend health unreachable (${HEALTH_URL})"
  return 1
}

system_resources() {
  local load
  load="$(uptime | sed 's/.*load averages*: //')"
  local disk
  disk="$(df -h "$NEURATRADE_HOME" | awk 'NR==2 {print $5}')"
  log_info "system load=${load} disk=${disk}"
}

single_check() {
  local failures=0

  check_pid_file "gateway" "$PIDS_DIR/gateway.pid" || failures=$((failures + 1))
  check_pid_file "backend" "$PIDS_DIR/backend.pid" || true
  check_pid_file "ccxt" "$PIDS_DIR/ccxt.pid" || true
  check_pid_file "telegram" "$PIDS_DIR/telegram.pid" || true
  check_backend_health || failures=$((failures + 1))
  system_resources

  return "$failures"
}

restart_gateway() {
  log_warn "Restarting gateway via startup-orchestrator"
  if bash "$SCRIPT_DIR/startup-orchestrator.sh" restart; then
    log_success "Gateway restart finished"
    return 0
  fi
  log_error "Gateway restart failed"
  return 1
}

monitor_loop() {
  local duration="${1:-0}"
  local start_ts
  start_ts="$(date +%s)"

  log_info "starting monitor loop interval=${MONITOR_INTERVAL}s duration=${duration}s"
  while true; do
    set +e
    single_check
    local rc=$?
    set -e

    if [ "$rc" -ne 0 ]; then
      log_warn "health check found ${rc} failure(s)"
    fi

    if [ "$duration" -gt 0 ]; then
      local now
      now="$(date +%s)"
      if [ $((now - start_ts)) -ge "$duration" ]; then
        log_info "monitor duration reached"
        break
      fi
    fi

    sleep "$MONITOR_INTERVAL"
  done
}

report() {
  mkdir -p "$LOG_DIR"
  local out="${LOG_DIR}/health-report-$(date +%Y%m%d_%H%M%S).json"
  local gateway_status="down"
  local backend_status="down"

  if check_pid_file "gateway" "$PIDS_DIR/gateway.pid" >/dev/null 2>&1; then
    gateway_status="up"
  fi
  if curl -fsS "$HEALTH_URL" >/dev/null 2>&1; then
    backend_status="up"
  fi

  cat >"$out" <<JSON
{
  "timestamp": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "gateway": "${gateway_status}",
  "backend_health": "${backend_status}",
  "health_url": "${HEALTH_URL}"
}
JSON
  log_success "health report generated: $out"
}

usage() {
  cat <<USAGE
Usage: $0 [monitor|check|restart|status|report|verify|help] [duration_seconds]

Commands:
  monitor   Continuous checks (default interval via MONITOR_INTERVAL)
  check     Run one health check cycle
  restart   Restart gateway through startup-orchestrator
  status    Alias for check
  report    Emit a JSON status report in ~/.neuratrade/logs
  verify    Exit non-zero if critical health checks fail
  help      Show this message
USAGE
}

main() {
  mkdir -p "$LOG_DIR"
  local cmd="${1:-status}"
  local duration="${2:-0}"

  case "$cmd" in
    monitor)
      monitor_loop "$duration"
      ;;
    check | status)
      single_check
      ;;
    restart)
      restart_gateway
      ;;
    report)
      report
      ;;
    verify)
      single_check
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
