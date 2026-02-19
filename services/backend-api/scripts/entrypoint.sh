#!/bin/bash
set -e

# Logging
log() {
  echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1"
}

# ============================================
# CRITICAL: Generate ADMIN_API_KEY early
# This must happen before any service starts
# ============================================
ensure_admin_api_key() {
  if [ -z "$ADMIN_API_KEY" ]; then
    log "WARNING: ADMIN_API_KEY is not set. Generating secure default..."
    # Generate a random 32-character hex string (openssl or fallback to /dev/urandom)
    if command -v openssl >/dev/null 2>&1; then
      export ADMIN_API_KEY=$(openssl rand -hex 16)
    else
      export ADMIN_API_KEY=$(cat /dev/urandom | tr -dc 'a-f0-9' | head -c 32)
    fi
    log "Generated ADMIN_API_KEY (first 8 chars): ${ADMIN_API_KEY:0:8}..."
  else
    log "ADMIN_API_KEY is set (first 8 chars): ${ADMIN_API_KEY:0:8}..."
  fi
}

# Call immediately to ensure key is available for all services
ensure_admin_api_key

# Normalize database environment variables from common providers (Coolify, generic Postgres, psql)
normalize_database_env() {
  # Map provider-specific URL first
  if [ -z "$DATABASE_URL" ]; then
    if [ -n "$POSTGRESQL_URL" ]; then
      export DATABASE_URL="$POSTGRESQL_URL"
    elif [ -n "$POSTGRES_URL" ]; then
      export DATABASE_URL="$POSTGRES_URL"
    fi
  fi

  # Map host values
  if [ -z "$DATABASE_HOST" ]; then
    for candidate in "$POSTGRES_HOST" "$POSTGRESQL_HOST" "$PGHOST"; do
      if [ -n "$candidate" ]; then
        export DATABASE_HOST="$candidate"
        break
      fi
    done
  fi

  # Map port values
  if [ -z "$DATABASE_PORT" ]; then
    for candidate in "$POSTGRES_PORT" "$POSTGRESQL_PORT" "$PGPORT"; do
      if [ -n "$candidate" ]; then
        export DATABASE_PORT="$candidate"
        break
      fi
    done
  fi

  # Map user values
  if [ -z "$DATABASE_USER" ]; then
    for candidate in "$POSTGRES_USER" "$POSTGRESQL_USER" "$PGUSER"; do
      if [ -n "$candidate" ]; then
        export DATABASE_USER="$candidate"
        break
      fi
    done
  fi

  # Map password values
  if [ -z "$DATABASE_PASSWORD" ]; then
    for candidate in "$POSTGRES_PASSWORD" "$POSTGRESQL_PASSWORD" "$PGPASSWORD"; do
      if [ -n "$candidate" ]; then
        export DATABASE_PASSWORD="$candidate"
        break
      fi
    done
  fi

  # Map database name values
  if [ -z "$DATABASE_DBNAME" ]; then
    for candidate in "$POSTGRES_DB" "$POSTGRESQL_DATABASE" "$PGDATABASE"; do
      if [ -n "$candidate" ]; then
        export DATABASE_DBNAME="$candidate"
        break
      fi
    done
  fi

  # Debug printout (password redacted)
  log "=== Database Environment Variables ==="
  log "DATABASE_URL=${DATABASE_URL:-<NOT SET>}"
  log "DATABASE_HOST=${DATABASE_HOST:-<NOT SET>}"
  log "DATABASE_PORT=${DATABASE_PORT:-5432}"
  log "DATABASE_USER=${DATABASE_USER:-<NOT SET>}"
  log "DATABASE_DBNAME=${DATABASE_DBNAME:-<NOT SET>}"
  if [ -n "$DATABASE_PASSWORD" ]; then
    log "DATABASE_PASSWORD=<REDACTED>"
  else
    log "DATABASE_PASSWORD=<NOT SET>"
  fi
  log "======================================"
}

# Trap signals
cleanup() {
  log "Received termination signal, shutting down..."
  kill -TERM "$PID_GO" 2>/dev/null
  kill -TERM "$PID_BUN" 2>/dev/null
  kill -TERM "$PID_TELEGRAM" 2>/dev/null
  wait "$PID_GO"
  wait "$PID_BUN"
  wait "$PID_TELEGRAM"
  exit 0
}

trap cleanup INT TERM

# Default to running CCXT unless explicitly disabled
PID_BUN=""
PID_TELEGRAM=""

# Start CCXT Service (Bun) in background
if [ "${CCXT_DISABLED}" = "true" ] || [ "${RUN_CCXT_SERVICE}" = "false" ]; then
  log "CCXT service startup disabled (CCXT_DISABLED=true or RUN_CCXT_SERVICE=false)"
else
  log "Starting CCXT Service..."

  # ADMIN_API_KEY is already ensured at script start

  # Set default port for CCXT service if not set
  export PORT="${PORT:-3001}"
  export CCXT_SERVICE_PORT="${PORT}"

  # Check if port is already in use and kill stale process if found
  if command -v lsof >/dev/null 2>&1; then
    EXISTING_PID=$(lsof -ti:${PORT} 2>/dev/null || true)
    if [ -n "$EXISTING_PID" ]; then
      log "WARNING: Port ${PORT} is already in use by PID ${EXISTING_PID}. Cleaning up stale process..."
      kill -9 $EXISTING_PID 2>/dev/null || true
      sleep 1
    fi
  fi

  # Use absolute path for directory change to ensure we know where we are
  cd /app/ccxt
  bun run dist/index.js &
  PID_BUN=$!

  # Wait for CCXT to be ready with health check
  log "Waiting for CCXT service to be ready..."
  MAX_WAIT=30
  WAIT_COUNT=0
  while [ $WAIT_COUNT -lt $MAX_WAIT ]; do
    if curl -s -f http://localhost:${PORT}/health >/dev/null 2>&1; then
      log "CCXT service is ready on port ${PORT}"
      break
    fi
    WAIT_COUNT=$((WAIT_COUNT + 1))
    sleep 1
  done

  if [ $WAIT_COUNT -ge $MAX_WAIT ]; then
    log "WARNING: CCXT service health check timed out after ${MAX_WAIT} seconds"
    log "Continuing with startup, but CCXT service may not be available"
  fi
fi

# Start Telegram Service (Bun) in background
if [ "${TELEGRAM_DISABLED}" = "true" ] || [ "${RUN_TELEGRAM_SERVICE}" = "false" ]; then
  log "Telegram service startup disabled (TELEGRAM_DISABLED=true or RUN_TELEGRAM_SERVICE=false)"
elif [ -z "$TELEGRAM_BOT_TOKEN" ]; then
  log "Telegram service not started (TELEGRAM_BOT_TOKEN not set)"
else
  log "Starting Telegram Service..."

  # ADMIN_API_KEY is already ensured at script start

  export TELEGRAM_PORT="${TELEGRAM_PORT:-3002}"

  if command -v lsof >/dev/null 2>&1; then
    EXISTING_PID=$(lsof -ti:${TELEGRAM_PORT} 2>/dev/null || true)
    if [ -n "$EXISTING_PID" ]; then
      log "WARNING: Port ${TELEGRAM_PORT} is already in use by PID ${EXISTING_PID}. Cleaning up stale process..."
      kill -9 $EXISTING_PID 2>/dev/null || true
      sleep 1
    fi
  fi

  cd /app/telegram
  bun run dist/index.js &
  PID_TELEGRAM=$!

  log "Waiting for Telegram service to be ready..."
  MAX_WAIT=30
  WAIT_COUNT=0
  while [ $WAIT_COUNT -lt $MAX_WAIT ]; do
    if curl -s -f http://localhost:${TELEGRAM_PORT}/health >/dev/null 2>&1; then
      log "Telegram service is ready on port ${TELEGRAM_PORT}"
      break
    fi
    WAIT_COUNT=$((WAIT_COUNT + 1))
    sleep 1
  done

  if [ $WAIT_COUNT -ge $MAX_WAIT ]; then
    log "WARNING: Telegram service health check timed out after ${MAX_WAIT} seconds"
    log "Continuing with startup, but Telegram service may not be available"
  fi
fi

# Normalize env vars early so migrations and app boot see the correct values
normalize_database_env

# Skip database wait for SQLite (no external database to wait for)
if [ "${DATABASE_DRIVER:-sqlite}" = "sqlite" ]; then
  log "Using SQLite - skipping PostgreSQL wait..."
else
  # PostgreSQL connection code continues below...
  if echo "${DATABASE_HOST}" | grep -qE "^postgres(ql)?://"; then
    export DATABASE_URL="${DATABASE_HOST}"
  fi

  # Credential Sync:
  # If we have explicit DATABASE_HOST and DATABASE_PASSWORD, we prefer using them
  # to reconstruct the connection, as DATABASE_URL might contain stale/generated credentials
  # from the platform that don't match the actual container environment.
  if [ -n "$DATABASE_HOST" ] && [ -n "$DATABASE_PASSWORD" ] && [ -n "$DATABASE_USER" ]; then
    if [ -n "$DATABASE_URL" ]; then
      # Only unset if DATABASE_HOST is NOT a URL itself
      if ! echo "${DATABASE_HOST}" | grep -qE "^postgres(ql)?://"; then
        log "Detected explicit DATABASE components with PASSWORD. Ignoring DATABASE_URL to ensure fresh credentials..."
        unset DATABASE_URL
      fi
    fi
  fi

  # Coolify/Docker DNS Fix:
  # If DATABASE_URL uses generic 'postgres' hostname but we have a specific DATABASE_HOST,
  # we patch the URL to use the correct hostname.
  if [ -n "$DATABASE_URL" ] && [ -n "$DATABASE_HOST" ]; then
    if [[ "$DATABASE_URL" == *"@postgres:"* ]] && [ "$DATABASE_HOST" != "postgres" ]; then
      log "Patching DATABASE_URL: Replacing generic 'postgres' host with '${DATABASE_HOST}'..."
      export DATABASE_URL="${DATABASE_URL/@postgres:/@$DATABASE_HOST:}"
    fi
  fi

  # Auto-recovery: If DATABASE_URL is set but unreachable, and DATABASE_HOST works, prefer DATABASE_HOST
  # This fixes common issues where Coolify/Platform provides a broken DATABASE_URL but correct component vars
  if [ -n "$DATABASE_URL" ]; then
    # Try a quick check on DATABASE_URL (timeout 2s)
    if ! pg_isready -d "$DATABASE_URL" -t 2 >/dev/null 2>&1; then
      log "DATABASE_URL check failed. Checking fallback to DATABASE_HOST=${DATABASE_HOST}..."

      # Define credentials for checks
      export CHECK_DB_PORT="${DATABASE_PORT:-5432}"
      export CHECK_DB_USER="${DATABASE_USER:-postgres}"
      export CHECK_DB_PASSWORD="${DATABASE_PASSWORD:-postgres}"

      # Check if DATABASE_HOST is reachable, if not try postgresql-develop
      HOST_TO_CHECK="${DATABASE_HOST}"
      if [ -n "$HOST_TO_CHECK" ] && ! echo "${HOST_TO_CHECK}" | grep -qE "^postgres(ql)?://"; then
        if ! PGPASSWORD="${CHECK_DB_PASSWORD}" pg_isready -h "${HOST_TO_CHECK}" -p "${CHECK_DB_PORT}" -U "${CHECK_DB_USER}" -t 2 >/dev/null 2>&1; then
          log "Host '${HOST_TO_CHECK}' unreachable. Checking 'postgresql-develop'..."
          if PGPASSWORD="${CHECK_DB_PASSWORD}" pg_isready -h "postgresql-develop" -p "${CHECK_DB_PORT}" -U "${CHECK_DB_USER}" -t 2 >/dev/null 2>&1; then
            log "Found reachable database at 'postgresql-develop'. Switching configuration..."
            export DATABASE_HOST="postgresql-develop"
            unset DATABASE_URL
          fi
        fi
      fi

      # If DATABASE_URL fails, check if we have a valid DATABASE_HOST component
      if [ -n "$DATABASE_HOST" ] && ! echo "${DATABASE_HOST}" | grep -qE "^postgres(ql)?://"; then
        # Check connectivity to DATABASE_HOST for logging purposes
        log "Testing connection to DATABASE_HOST ($DATABASE_HOST)..."
        # We run this just to print the error to logs (remove -q/silence)
        PGPASSWORD="${CHECK_DB_PASSWORD}" pg_isready -h "${DATABASE_HOST}" -p "${CHECK_DB_PORT}" -U "${CHECK_DB_USER}" -t 2 || true

        log "WARNING: DATABASE_URL is unreachable. Forcing switch to DATABASE_HOST configuration to retry connection loop there."
        unset DATABASE_URL
      fi
    fi
  fi

  if [ -n "$DATABASE_URL" ]; then
    log "Using DATABASE_URL for connection..."
    export DATABASE_URL
    # Wait for database to be ready using URL
    until pg_isready -d "$DATABASE_URL"; do
      log "Database not ready, waiting..."
      # Try to print the actual error message using psql
      psql "$DATABASE_URL" -c "SELECT 1" 2>&1 | head -n 1 || true
      sleep 2
    done
  else
    # Export DB_ vars from DATABASE_ vars for migrate.sh
    export DB_HOST="${DATABASE_HOST:-localhost}"
    export DB_PORT="${DATABASE_PORT:-5432}"
    export DB_NAME="${DATABASE_DBNAME:-neuratrade}"
    export DB_USER="${DATABASE_USER:-postgres}"
    export DB_PASSWORD="${DATABASE_PASSWORD:-postgres}"

    # Wait for database to be ready
    log "Waiting for database at ${DB_HOST}:${DB_PORT}..."
    until PGPASSWORD="${DB_PASSWORD}" pg_isready -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}"; do
      log "Database not ready, waiting..."
      # Try to print the actual error message using psql
      PGPASSWORD="${DB_PASSWORD}" psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${DB_NAME}" -c "SELECT 1" 2>&1 | head -n 1 || true
      sleep 2
    done

    # Verify credentials explicitly before proceeding
    log "Verifying database credentials..."
    if ! PGPASSWORD="${DB_PASSWORD}" psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${DB_NAME}" -c "SELECT 1" >/dev/null 2>&1; then
      log "ERROR: Database authentication failed. The provided DATABASE_PASSWORD does not match the database user '${DB_USER}'."
      log "Detailed Error:"
      PGPASSWORD="${DB_PASSWORD}" psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${DB_NAME}" -c "SELECT 1" || true
      exit 1
    fi
  fi
  log "Database is ready and authenticated."

  if "$MIGRATE_SCRIPT"; then
    log "Migrations completed successfully."
  else
    log "Migration failed. Exiting."
    exit 1
  fi
fi

# Start Go App in background
log "Starting Main Application..."
cd /app
./main &
PID_GO=$!

# Monitor processes
log "All services started. Monitoring..."
if [ -n "$PID_BUN" ] && [ -n "$PID_TELEGRAM" ]; then
  wait "$PID_GO" "$PID_BUN" "$PID_TELEGRAM"
elif [ -n "$PID_BUN" ]; then
  wait "$PID_GO" "$PID_BUN"
elif [ -n "$PID_TELEGRAM" ]; then
  wait "$PID_GO" "$PID_TELEGRAM"
else
  wait "$PID_GO"
fi
