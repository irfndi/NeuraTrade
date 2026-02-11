#!/bin/bash

# NeuraTrade Database Migration Script
# Usage: ./migrate.sh [migration_number]
# If no migration_number provided, runs all pending migrations

set -e

# Configuration - Support both DB_* and DATABASE_* env var naming conventions
DB_HOST="${DATABASE_HOST:-${DB_HOST:-localhost}}"
DB_PORT="${DATABASE_PORT:-${DB_PORT:-5432}}"
DB_NAME="${DATABASE_DBNAME:-${DB_NAME:-neuratrade}}"
DB_USER="${DATABASE_USER:-${DB_USER:-neuratrade_user}}"
DB_PASSWORD="${DATABASE_PASSWORD:-${DB_PASSWORD:-}}"
MIGRATIONS_DIR="$(dirname "$0")/migrations"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# log writes a timestamped informational message to stdout formatted in green.
log() {
  echo -e "${GREEN}[$(date +'%Y-%m-%d %H:%M:%S')]${NC} $1"
}

# log_warn prints a timestamped WARNING message prefixed with "WARNING:" in yellow to stdout.
log_warn() {
  echo -e "${YELLOW}[$(date +'%Y-%m-%d %H:%M:%S')] WARNING:${NC} $1"
}

# log_error prints a timestamped "ERROR" message in red to stdout.
log_error() {
  echo -e "${RED}[$(date +'%Y-%m-%d %H:%M:%S')] ERROR:${NC} $1"
}

# run_psql executes psql with the appropriate connection parameters
# Prioritizes individual DATABASE_* vars over DATABASE_URL to avoid hostname conflicts
run_psql() {
  # Use individual vars if DATABASE_HOST is explicitly set (not default localhost)
  # This handles cases where DATABASE_URL has a different hostname than DATABASE_HOST
  if [ -n "$DATABASE_HOST" ] && [ "$DATABASE_HOST" != "localhost" ]; then
    PGPASSWORD="$DB_PASSWORD" psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" "$@"
  elif [ -n "$DATABASE_URL" ]; then
    psql "$DATABASE_URL" "$@"
  else
    PGPASSWORD="$DB_PASSWORD" psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" "$@"
  fi
}

# migration_applied checks whether a given migration filename is recorded as applied in the database's schema_migrations table.
# Takes the migration filename as the first argument.
# Returns exit status 0 if the schema_migrations table exists and the filename is marked applied; returns non-zero otherwise.
migration_applied() {
  local migration_name="$1"

  # Check if schema_migrations table exists
  if ! run_psql -c "\dt" | grep -q schema_migrations; then
    return 1
  fi

  # Check if migration has been applied
  run_psql -c "SELECT 1 FROM schema_migrations WHERE filename = '$migration_name' AND applied = true" -t -A 2>/dev/null | grep -q 1
}

# apply_migration applies a SQL migration file to the configured database, records the migration in the `schema_migrations` table, skips the file if it is already recorded as applied, and exits with a non-zero status on failure.
apply_migration() {
  local migration_file="$1"
  local migration_name
  migration_name=$(basename "$migration_file")

  if migration_applied "$migration_name"; then
    log_warn "Migration $migration_name already applied, skipping"
    return 0
  fi

  log "Applying migration: $migration_name"

  # Apply the migration
  if run_psql -f "$migration_file"; then
    log "Successfully applied migration: $migration_name"

    # Record migration in schema_migrations table
    run_psql -c "INSERT INTO schema_migrations (filename, applied) VALUES ('$migration_name', true) ON CONFLICT (filename) DO UPDATE SET applied = true, applied_at = NOW()"
  else
    log_error "Failed to apply migration: $migration_name"
    exit 1
  fi
}

# create_migrations_table creates the schema_migrations table if it does not exist.
# The table tracks applied migrations with columns: id (primary key), filename (unique, not null), applied (boolean), and applied_at (timestamp with time zone).
create_migrations_table() {
  run_psql -c "
        CREATE TABLE IF NOT EXISTS schema_migrations (
            id SERIAL PRIMARY KEY,
            filename VARCHAR(255) UNIQUE NOT NULL,
            applied BOOLEAN DEFAULT false,
            applied_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
        )
    "
}

# list_migrations prints available SQL migration files from the migrations directory and marks each as "applied" or "pending".
list_migrations() {
  log "Available migrations:"
  ls -1 "$MIGRATIONS_DIR"/*.sql | sort -V | while read -r file; do
    local filename
    filename=$(basename "$file")
    if migration_applied "$filename"; then
      echo "  ✓ $filename (applied)"
    else
      echo "  ⏳ $filename (pending)"
    fi
  done
}

# show_status shows migration status; if the `schema_migrations` table does not exist it warns and lists available migrations, otherwise it queries and prints `filename`, `applied`, and `applied_at` ordered by `applied_at` (descending) then `filename`.
show_status() {
  log "Migration status:"
  if ! PGPASSWORD="$DB_PASSWORD" psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -c "\dt" | grep -q schema_migrations; then
    log_warn "Schema migrations table does not exist"
    list_migrations
    return
  fi

  PGPASSWORD="$DB_PASSWORD" psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -c "
        SELECT filename, applied, applied_at 
        FROM schema_migrations 
        ORDER BY applied_at DESC, filename
    "
}

# run_specific_migration runs the migration whose filename starts with the given numeric prefix, ensures exactly one matching SQL file exists, creates the migrations tracking table if needed, and applies the migration (exits with an error on failure).
run_specific_migration() {
  local migration_number="$1"
  local migration_files=("$MIGRATIONS_DIR"/${migration_number}_*.sql)

  if [ ${#migration_files[@]} -eq 0 ] || [ ! -f "${migration_files[0]}" ]; then
    log_error "Migration file not found for number: $migration_number"
    exit 1
  fi

  if [ ${#migration_files[@]} -gt 1 ]; then
    log_error "Multiple migration files found for number: $migration_number"
    exit 1
  fi

  local migration_file="${migration_files[0]}"

  create_migrations_table
  apply_migration "$migration_file"
}

# run_all_migrations runs all pending SQL migrations from the migrations directory in version order, ensuring the migrations tracking table exists and applying each migration while recording successful applications.
run_all_migrations() {
  log "Running all pending migrations..."
  create_migrations_table

  ls -1 "$MIGRATIONS_DIR"/*.sql | sort -V | while read -r file; do
    apply_migration "$file"
  done

  log "All migrations completed successfully!"
}

# rollback_migration removes the migration's record from `schema_migrations` for the given migration number and warns that an actual SQL rollback must be provided separately.
rollback_migration() {
  local migration_number="$1"
  local migration_files=("$MIGRATIONS_DIR"/${migration_number}_*.sql)

  if [ ${#migration_files[@]} -eq 0 ] || [ ! -f "${migration_files[0]}" ]; then
    log_error "Migration file not found for number: $migration_number"
    exit 1
  fi

  if [ ${#migration_files[@]} -gt 1 ]; then
    log_error "Multiple migration files found for number: $migration_number"
    exit 1
  fi

  local migration_file="${migration_files[0]}"
  local migration_name
  migration_name=$(basename "$migration_file")

  log "Rolling back migration: $migration_name"

  # This is a simplified rollback - in practice, you'd need rollback scripts
  log_warn "Rollback functionality requires specific rollback scripts"
  log "Consider creating rollback scripts for complex migrations"

  # Remove migration record (using parameterized query to prevent SQL injection)
  PGPASSWORD="$DB_PASSWORD" psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" \
    -c "DELETE FROM schema_migrations WHERE filename = \$1" \
    "$migration_name"

  log "Migration record removed: $migration_name"
}

# Main script logic
case "${1:-run}" in
  "status")
    show_status
    ;;
  "list")
    list_migrations
    ;;
  "run")
    run_all_migrations
    ;;
  "rollback")
    if [ -z "$2" ]; then
      log_error "Usage: $0 rollback <migration_number>"
      exit 1
    fi
    rollback_migration "$2"
    ;;
  *)
    if [[ "$1" =~ ^[0-9]+$ ]]; then
      run_specific_migration "$1"
    else
      echo "Usage: $0 [command]"
      echo ""
      echo "Commands:"
      echo "  status          - Show migration status"
      echo "  list            - List available migrations"
      echo "  run             - Run all pending migrations (default)"
      echo "  rollback <num>  - Rollback specific migration"
      echo "  <number>        - Run specific migration"
      echo ""
      echo "Environment variables:"
      echo "  DB_HOST, DB_PORT, DB_NAME, DB_USER, DB_PASSWORD"
      exit 1
    fi
    ;;
esac
