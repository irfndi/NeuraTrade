#!/bin/bash

# NeuraTrade SQLite Migration Script
# Usage: ./migrate-sqlite.sh [migration_number]
# If no migration_number provided, runs all pending migrations

set -e

# Configuration
DB_PATH="${SQLITE_DB_PATH:-neuratrade.db}"
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

# run_sqlite executes sqlite3 with the database path
run_sqlite() {
  sqlite3 "$DB_PATH" "$@"
}

# migration_applied checks whether a given migration filename is recorded as applied in the database's schema_migrations table.
migration_applied() {
  local migration_name="$1"
  
  # Check if schema_migrations table exists
  if ! run_sqlite ".schema schema_migrations" 2>/dev/null | grep -q "CREATE TABLE"; then
    return 1
  fi
  
  # Check if migration has been applied
  result=$(run_sqlite "SELECT 1 FROM schema_migrations WHERE filename = '$migration_name' AND applied = 1 LIMIT 1;" 2>/dev/null)
  if [ "$result" = "1" ]; then
    return 0
  fi
  return 1
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
  if run_sqlite ".read $migration_file"; then
    log "Successfully applied migration: $migration_name"
    
    # Record migration in schema_migrations table
    run_sqlite "INSERT OR REPLACE INTO schema_migrations (filename, applied, applied_at) VALUES ('$migration_name', 1, datetime('now'));"
  else
    log_error "Failed to apply migration: $migration_name"
    exit 1
  fi
}

# create_migrations_table creates the schema_migrations table if it does not exist.
create_migrations_table() {
  run_sqlite "CREATE TABLE IF NOT EXISTS schema_migrations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    filename VARCHAR(255) UNIQUE NOT NULL,
    applied BOOLEAN DEFAULT 0,
    applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
  );"
}

# list_migrations prints available SQL migration files from the migrations directory and marks each as "applied" or "pending".
list_migrations() {
  log "Available migrations:"
  for file in "$MIGRATIONS_DIR"/*.sql; do
    if [ -f "$file" ]; then
      local filename
      filename=$(basename "$file")
      if migration_applied "$filename"; then
        echo "  ✓ $filename (applied)"
      else
        echo "  ⏳ $filename (pending)"
      fi
    fi
  done | sort -V
}

# show_status shows migration status; if the `schema_migrations` table does not exist it warns and lists available migrations, otherwise it queries and prints `filename`, `applied`, and `applied_at` ordered by `applied_at` (descending) then `filename`.
show_status() {
  log "Migration status:"
  if ! run_sqlite ".schema schema_migrations" 2>/dev/null | grep -q "CREATE TABLE"; then
    log_warn "Schema migrations table does not exist"
    list_migrations
    return
  fi
  
  run_sqlite -header -column "SELECT filename, applied, applied_at FROM schema_migrations ORDER BY applied_at DESC, filename;"
}

# run_specific_migration runs the migration whose filename starts with the given numeric prefix, ensures exactly one matching SQL file exists, creates the migrations tracking table if needed, and applies the migration (exits with an error on failure).
run_specific_migration() {
  local migration_number="$1"
  local migration_files=()
  
  for file in "$MIGRATIONS_DIR"/${migration_number}_*.sql; do
    if [ -f "$file" ]; then
      migration_files+=("$file")
    fi
  done
  
  if [ ${#migration_files[@]} -eq 0 ]; then
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
  
  for file in $(ls -1 "$MIGRATIONS_DIR"/*.sql 2>/dev/null | sort -V); do
    if [ -f "$file" ]; then
      apply_migration "$file"
    fi
  done
  
  log "All migrations completed successfully!"
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
      echo "  <number>        - Run specific migration"
      echo ""
      echo "Environment variables:"
      echo "  SQLITE_DB_PATH  - Path to SQLite database (default: neuratrade.db)"
      exit 1
    fi
    ;;
esac
