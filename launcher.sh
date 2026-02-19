#!/bin/bash
set -e

# NeuraTrade Launcher - Starts all services with proper port binding
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
NEURATRADE_HOME="${NEURATRADE_HOME:-$HOME/.neuratrade}"
CONFIG_DIR="$NEURATRADE_HOME"
PID_DIR="$NEURATRADE_HOME/pids"
LOG_DIR="$NEURATRADE_HOME/logs"

# Default ports
CCXT_PORT=3001
TELEGRAM_PORT=3002
BACKEND_PORT=8080

config_get() {
    local query="$1"
    local default="${2:-}"
    local cfg="$NEURATRADE_HOME/config.json"
    if [[ -f "$cfg" ]]; then
        local value
        value=$(jq -r "$query // empty" "$cfg" 2>/dev/null || true)
        if [[ -n "$value" && "$value" != "null" ]]; then
            echo "$value"
            return
        fi
    fi
    echo "$default"
}

# Create directories
mkdir -p "$PID_DIR" "$LOG_DIR"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

cleanup_stale_pids() {
    for pidfile in "$PID_DIR"/*.pid; do
        [ -e "$pidfile" ] || continue
        local pid
        pid="$(cat "$pidfile" 2>/dev/null || true)"
        if [[ -z "$pid" ]] || ! kill -0 "$pid" 2>/dev/null; then
            rm -f "$pidfile"
        fi
    done
}

prepare_logs() {
    : > "$LOG_DIR/ccxt.log"
    : > "$LOG_DIR/telegram.log"
    : > "$LOG_DIR/backend.log"
}

pid_is_alive() {
    local pidfile="$1"
    [ -f "$pidfile" ] || return 1
    local pid
    pid="$(cat "$pidfile" 2>/dev/null || true)"
    [ -n "$pid" ] || return 1
    kill -0 "$pid" 2>/dev/null
}

port_in_use() {
    local port="$1"
    lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1
}

ensure_port_available() {
    local port="$1"
    local service="$2"
    if port_in_use "$port"; then
        log_error "$service port $port is already in use by another process"
        log_error "Run './launcher.sh stop' and ensure old processes are terminated before starting again."
        exit 1
    fi
}

start_services() {
    log_info "Starting NeuraTrade services..."
    cleanup_stale_pids
    prepare_logs
    
    # Load config if exists
    if [ -f "$CONFIG_DIR/.env" ]; then
        set -a
        source "$CONFIG_DIR/.env"
        set +a
    fi

    # Resolve runtime values from persistent config.json (read-only)
    BACKEND_PORT="${BACKEND_PORT:-$(config_get '.server.port' '8080')}"
    local admin_api_key
    admin_api_key="${ADMIN_API_KEY:-$(config_get '.security.admin_api_key' '')}"
    if [[ -z "$admin_api_key" ]]; then
        admin_api_key="$(config_get '.ccxt.admin_api_key' '')"
    fi

    local sqlite_path
    sqlite_path="${SQLITE_PATH:-$(config_get '.database.sqlite_path' "$NEURATRADE_HOME/data/neuratrade.db")}"

    local telegram_token
    telegram_token="${TELEGRAM_BOT_TOKEN:-$(config_get '.telegram.bot_token' '')}"

    local ai_api_key
    ai_api_key="${AI_API_KEY:-$(config_get '.ai.api_key' '')}"
    local ai_base_url
    ai_base_url="${AI_BASE_URL:-$(config_get '.ai.base_url' '')}"
    local ai_provider
    ai_provider="${AI_PROVIDER:-$(config_get '.ai.provider' 'minimax')}"
    local ai_model
    ai_model="${AI_MODEL:-$(config_get '.ai.model' '')}"
    
    # Start CCXT Service (bind to localhost only)
    ensure_port_available "$CCXT_PORT" "CCXT"
    log_info "Starting CCXT Service on 127.0.0.1:$CCXT_PORT..."
    cd "$SCRIPT_DIR/services/ccxt-service"
    BIND_HOST="127.0.0.1" CCXT_GRPC_HOST="127.0.0.1" PORT="$CCXT_PORT" ADMIN_API_KEY="$admin_api_key" bun run index.ts > "$LOG_DIR/ccxt.log" 2>&1 &
    echo $! > "$PID_DIR/ccxt.pid"
    sleep 1
    if ! pid_is_alive "$PID_DIR/ccxt.pid"; then
        log_error "CCXT process exited immediately"
        tail -n 60 "$LOG_DIR/ccxt.log" 2>/dev/null || true
        exit 1
    fi
    
    # Wait for CCXT to fully start
    log_info "Waiting for CCXT service to start..."
    timeout=30
    while [ $timeout -gt 0 ]; do
        if curl -sf "http://127.0.0.1:$CCXT_PORT/health" > /dev/null 2>&1; then
            if ! pid_is_alive "$PID_DIR/ccxt.pid"; then
                log_error "CCXT health is up but launched process is not alive (port likely owned by another process)"
                exit 1
            fi
            log_info "CCXT service ready"
            break
        fi
        sleep 2
        timeout=$((timeout - 2))
    done
    if [ $timeout -le 0 ]; then
        log_error "CCXT service failed to start within timeout"
        exit 1
    fi
    sleep 2  # Extra buffer time
    
    # Start Telegram Service (bind to localhost only)
    ensure_port_available "$TELEGRAM_PORT" "Telegram"
    log_info "Starting Telegram Service on 127.0.0.1:$TELEGRAM_PORT..."
    cd "$SCRIPT_DIR/services/telegram-service"
    BIND_HOST="127.0.0.1" PORT="$TELEGRAM_PORT" TELEGRAM_BOT_TOKEN="$telegram_token" TELEGRAM_API_BASE_URL="http://127.0.0.1:$BACKEND_PORT" ADMIN_API_KEY="$admin_api_key" bun run index.ts > "$LOG_DIR/telegram.log" 2>&1 &
    echo $! > "$PID_DIR/telegram.pid"
    sleep 1
    if ! pid_is_alive "$PID_DIR/telegram.pid"; then
        log_error "Telegram process exited immediately"
        tail -n 60 "$LOG_DIR/telegram.log" 2>/dev/null || true
        exit 1
    fi
    
    # Start Backend API (bind to all interfaces - public)
    ensure_port_available "$BACKEND_PORT" "Backend"
    log_info "Starting Backend API on 0.0.0.0:$BACKEND_PORT..."
    cd "$SCRIPT_DIR"
    
    # Export all required environment variables
    export CCXT_SERVICE_URL="http://127.0.0.1:3001"
    export CCXT_GRPC_ADDRESS="127.0.0.1:50051"
    export CCXT_ADMIN_API_KEY="$admin_api_key"
    export ADMIN_API_KEY="$admin_api_key"
    export TELEGRAM_BOT_TOKEN="$telegram_token"
    export SQLITE_PATH="$sqlite_path"
    export SQLITE_DB_PATH="$sqlite_path"
    export AI_API_KEY="$ai_api_key"
    export AI_BASE_URL="$ai_base_url"
    export AI_PROVIDER="$ai_provider"
    export AI_MODEL="$ai_model"

    PORT="$BACKEND_PORT" ./bin/neuratrade-server > "$LOG_DIR/backend.log" 2>&1 &
    echo $! > "$PID_DIR/backend.pid"
    sleep 1
    if ! pid_is_alive "$PID_DIR/backend.pid"; then
        log_error "Backend process exited immediately"
        tail -n 80 "$LOG_DIR/backend.log" 2>/dev/null || true
        exit 1
    fi

    # Wait for backend health endpoint
    log_info "Waiting for Backend API to become healthy..."
    timeout=45
    while [ $timeout -gt 0 ]; do
        if curl -sf "http://127.0.0.1:$BACKEND_PORT/health" > /dev/null 2>&1; then
            if ! pid_is_alive "$PID_DIR/backend.pid"; then
                log_error "Backend health is up but launched process is not alive (port likely owned by another process)"
                exit 1
            fi
            log_info "Backend API ready"
            log_info "All services started!"
            log_info "Backend API: http://localhost:$BACKEND_PORT"
            log_info "CCXT Service: internal only (127.0.0.1:$CCXT_PORT)"
            log_info "Telegram Service: internal only (127.0.0.1:$TELEGRAM_PORT)"
            return
        fi
        sleep 2
        timeout=$((timeout - 2))
    done

    log_error "Backend API failed to become healthy within timeout"
    log_error "Last backend log lines:"
    tail -n 40 "$LOG_DIR/backend.log" 2>/dev/null || true
    exit 1
}

stop_services() {
    log_info "Stopping NeuraTrade services..."

    local pids_to_wait=()

    for pidfile in "$PID_DIR"/*.pid; do
        if [ -f "$pidfile" ]; then
            pid=$(cat "$pidfile")
            name=$(basename "$pidfile" .pid)
            if kill -0 "$pid" 2>/dev/null; then
                log_info "Stopping $name (PID: $pid)..."
                kill "$pid" 2>/dev/null || true
                pids_to_wait+=("$pid")
            fi
            rm -f "$pidfile"
        fi
    done

    # Wait for graceful shutdown first
    if [ "${#pids_to_wait[@]}" -gt 0 ]; then
        local wait_sec=0
        while [ $wait_sec -lt 12 ]; do
            local alive=0
            for pid in "${pids_to_wait[@]}"; do
                if kill -0 "$pid" 2>/dev/null; then
                    alive=1
                    break
                fi
            done
            [ $alive -eq 0 ] && break
            sleep 1
            wait_sec=$((wait_sec + 1))
        done
    fi

    # Force-kill any remaining matching processes to prevent buildup across restarts
    pkill -9 -f "bun.*index.ts" 2>/dev/null || true
    pkill -9 -f "neuratrade-server" 2>/dev/null || true
    
    log_info "All services stopped."
}

status_services() {
    log_info "Checking service status..."
    cleanup_stale_pids
    BACKEND_PORT="${BACKEND_PORT:-$(config_get '.server.port' '8080')}"
    
    for pidfile in "$PID_DIR"/*.pid; do
        [ -e "$pidfile" ] || continue
        if [ -f "$pidfile" ]; then
            pid=$(cat "$pidfile")
            name=$(basename "$pidfile" .pid)
            if kill -0 "$pid" 2>/dev/null; then
                log_info "$name: RUNNING (PID: $pid)"
            else
                log_warn "$name: NOT RUNNING (stale PID file)"
            fi
        fi
    done
    
    # Check port availability
    if lsof -i :$BACKEND_PORT >/dev/null 2>&1; then
        log_info "Backend port $BACKEND_PORT: IN USE"
    else
        log_warn "Backend port $BACKEND_PORT: FREE"
    fi
}

case "${1:-start}" in
    start)
        start_services
        ;;
    stop)
        stop_services
        ;;
    restart)
        stop_services
        sleep 2
        start_services
        ;;
    status)
        status_services
        ;;
    *)
        echo "Usage: $0 {start|stop|restart|status}"
        exit 1
        ;;
esac
