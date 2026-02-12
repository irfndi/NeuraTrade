#!/bin/bash

# Simple migration script for Docker containers
# This script doesn't require docker/docker-compose tools inside the container

set -euo pipefail

# Configuration
DB_HOST="${DB_HOST:-postgres}"
DB_PORT="${DB_PORT:-5432}"
DB_NAME="${DB_NAME:-neuratrade}"
DB_USER="${DB_USER:-postgres}"
DB_PASSWORD="${DB_PASSWORD:-}"
MIGRATIONS_DIR="${MIGRATIONS_DIR:-/database/migrations}"
MAX_WAIT_TIME="${MAX_WAIT_TIME:-300}"
CONNECTION_TIMEOUT="${CONNECTION_TIMEOUT:-30}"

# Logging functions
log() {
  echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*"
}

log_error() {
  echo "[$(date '+%Y-%m-%d %H:%M:%S')] ERROR: $*" >&2
}

log_success() {
  echo "[$(date '+%Y-%m-%d %H:%M:%S')] SUCCESS: $*"
}

log_warn() {
  echo "[$(date '+%Y-%m-%d %H:%M:%S')] WARNING: $*"
}

# Check if required tools are available (only psql needed in container)
check_prerequisites() {
  if ! command -v psql &>/dev/null; then
    log_error "psql is not available in the container"
    return 1
  fi

  # Check if migrations directory exists
  if [ ! -d "$MIGRATIONS_DIR" ]; then
    log_warn "Migrations directory does not exist: $MIGRATIONS_DIR"
    log_warn "Creating migrations directory..."
    mkdir -p "$MIGRATIONS_DIR"
  fi

  return 0
}

# Wait for database to be ready
wait_for_database() {
  local max_attempts=$((MAX_WAIT_TIME / 5))
  local attempt=1

  log "Waiting for database to be ready..."

  while [ $attempt -le $max_attempts ]; do
    if check_database_connection; then
      log_success "Database is ready"
      return 0
    fi

    log "Database not ready, waiting... (attempt $attempt/$max_attempts)"
    sleep 5
    ((attempt++))
  done

  log_error "Database failed to become ready within $MAX_WAIT_TIME seconds"
  return 1
}

# Check database connection
check_database_connection() {
  local connection_string="postgresql://$DB_USER:$DB_PASSWORD@$DB_HOST:$DB_PORT/$DB_NAME"

  if timeout $CONNECTION_TIMEOUT psql "$connection_string" -c "SELECT 1;" &>/dev/null; then
    return 0
  fi

  return 1
}

# Execute SQL command
execute_sql() {
  local sql="$1"
  local database="${2:-$DB_NAME}"
  local connection_string="postgresql://$DB_USER:$DB_PASSWORD@$DB_HOST:$DB_PORT/$database"

  echo "$sql" | psql "$connection_string" -v ON_ERROR_STOP=1
}

# Execute SQL file
execute_sql_file() {
  local file="$1"
  local database="${2:-$DB_NAME}"
  local connection_string="postgresql://$DB_USER:$DB_PASSWORD@$DB_HOST:$DB_PORT/$database"

  if [ ! -f "$file" ]; then
    log_error "SQL file not found: $file"
    return 1
  fi

  psql "$connection_string" -v ON_ERROR_STOP=1 -f "$file"
}

# Create migrations table
create_migrations_table() {
  log "Creating migrations table if not exists..."

  local sql="
CREATE TABLE IF NOT EXISTS schema_migrations (
    id SERIAL PRIMARY KEY,
    filename VARCHAR(255) UNIQUE,
    checksum VARCHAR(64),
    applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    execution_time_ms INTEGER
);

-- Ensure filename column exists and is enforced unique
ALTER TABLE schema_migrations ADD COLUMN IF NOT EXISTS filename VARCHAR(255);
ALTER TABLE schema_migrations ALTER COLUMN filename SET NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS schema_migrations_filename_key ON schema_migrations(filename);
CREATE INDEX IF NOT EXISTS idx_schema_migrations_applied_at ON schema_migrations(applied_at);
"

  if execute_sql "$sql"; then
    log_success "Migrations table ready"
    return 0
  else
    log_error "Failed to create migrations table"
    return 1
  fi
}

# Get applied migrations
get_applied_migrations() {
  execute_sql "SELECT filename FROM schema_migrations ORDER BY applied_at;" | grep -v "filename" | grep -v "^-" | grep -v "^(" | grep -v "^$" | sed 's/^[[:space:]]*//' || true
}

# Calculate file checksum
calculate_checksum() {
  local file="$1"

  if [ -f "$file" ]; then
    sha256sum "$file" | cut -d' ' -f1
  else
    echo ""
  fi
}

# Apply migration
apply_migration() {
  local file="$1"
  local filename=$(basename "$file")
  local checksum=$(calculate_checksum "$file")
  local start_time=$(date +%s%3N)

  log "Applying migration: $filename"

  if execute_sql_file "$file"; then
    local end_time=$(date +%s%3N)
    local execution_time=$((end_time - start_time))

    # Record migration (idempotent)
    local record_sql="INSERT INTO schema_migrations (filename, checksum, execution_time_ms) VALUES ('$filename', '$checksum', $execution_time) ON CONFLICT (filename) DO UPDATE SET checksum = EXCLUDED.checksum, execution_time_ms = EXCLUDED.execution_time_ms, applied_at = CURRENT_TIMESTAMP;"
    if execute_sql "$record_sql"; then
      log_success "Migration applied successfully: $filename (${execution_time}ms)"
      return 0
    else
      log_error "Failed to record migration: $filename"
      return 1
    fi
  else
    log_error "Failed to apply migration: $filename"
    return 1
  fi
}

# Run migrations
run_migrations() {
  log "Starting migration process..."

  # Get list of applied migrations
  local applied_migrations
  applied_migrations=$(get_applied_migrations)

  # Get list of migration files
  local migration_files
  migration_files=$(find "$MIGRATIONS_DIR" -name "*.sql" | sort)

  if [ -z "$migration_files" ]; then
    log "No migration files found in $MIGRATIONS_DIR"
    return 0
  fi

  local migrations_applied=0

  for file in $migration_files; do
    local filename=$(basename "$file")

    # Check if migration has already been applied
    if echo "$applied_migrations" | grep -q "^$filename$"; then
      log "Migration already applied: $filename"
      continue
    fi

    # Apply migration
    if apply_migration "$file"; then
      ((migrations_applied++))
    else
      log_error "Migration failed, stopping: $filename"
      return 1
    fi
  done

  if [ $migrations_applied -eq 0 ]; then
    log "No new migrations to apply"
  else
    log "Applied $migrations_applied migration(s)"
  fi

  return 0
}

# Main function
main() {
  log "Simple migration script starting..."
  log "Database: $DB_HOST:$DB_PORT/$DB_NAME"
  log "Migrations directory: $MIGRATIONS_DIR"

  # Check prerequisites
  if ! check_prerequisites; then
    log_error "Prerequisites check failed"
    exit 1
  fi

  # Wait for database
  if ! wait_for_database; then
    log_error "Database connection failed"
    exit 1
  fi

  # Create migrations table
  if ! create_migrations_table; then
    log_error "Failed to create migrations table"
    exit 1
  fi

  # Run migrations
  if ! run_migrations; then
    log_error "Migration process failed"
    exit 1
  fi

  log_success "Migration process completed successfully"
}

# Run main function
main "$@"
