#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
DB_PATH="${SQLITE_PATH:-${SQLITE_DB_PATH:-${SCRIPT_DIR}/../data/neuratrade.db}}"
MIGRATIONS_DIR="${SQLITE_MIGRATIONS_DIR:-${SCRIPT_DIR}/sqlite_migrations}"
VEC_EXTENSION_PATH="${SQLITE_VEC_EXTENSION_PATH:-}"
CMD="${1:-run}"

mkdir -p "$(dirname "$DB_PATH")"

sqlite3 "$DB_PATH" "PRAGMA foreign_keys = ON; PRAGMA journal_mode = WAL;"

if [ -n "$VEC_EXTENSION_PATH" ]; then
  sqlite3 "$DB_PATH" "SELECT load_extension('$VEC_EXTENSION_PATH');" >/dev/null
fi

sqlite3 "$DB_PATH" "CREATE TABLE IF NOT EXISTS schema_migrations (filename TEXT PRIMARY KEY, applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP);"

sqlite_table_exists() {
  local table_name="$1"
  sqlite3 "$DB_PATH" "SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = '$table_name' LIMIT 1;" | grep -q "1"
}

sqlite_column_exists() {
  local table_name="$1"
  local column_name="$2"
  sqlite3 "$DB_PATH" "SELECT 1 FROM pragma_table_info('$table_name') WHERE name = '$column_name' LIMIT 1;" | grep -q "1"
}

repair_legacy_funding_arbitrage_opportunities() {
  sqlite_table_exists "funding_arbitrage_opportunities" || return 0
  sqlite_column_exists "funding_arbitrage_opportunities" "estimated_profit_percentage" && return 0

  local legacy_table
  legacy_table="funding_arbitrage_opportunities_legacy_$(date +%Y%m%d%H%M%S)"
  printf "repair funding_arbitrage_opportunities -> %s\n" "$legacy_table"

  sqlite3 "$DB_PATH" <<SQL
PRAGMA foreign_keys = OFF;
DROP INDEX IF EXISTS idx_funding_arb_is_active;
DROP INDEX IF EXISTS idx_funding_arb_detected;
DROP INDEX IF EXISTS idx_funding_arb_symbol;
DROP INDEX IF EXISTS idx_funding_arbitrage_active;
DROP INDEX IF EXISTS idx_funding_arbitrage_profit;
DROP INDEX IF EXISTS idx_funding_arbitrage_active_filter;
DROP INDEX IF EXISTS idx_funding_arbitrage_expires;
ALTER TABLE funding_arbitrage_opportunities RENAME TO ${legacy_table};
PRAGMA foreign_keys = ON;
SQL
}

apply_file() {
  local file="$1"
  local name
  name=$(basename "$file")

  if sqlite3 "$DB_PATH" "SELECT 1 FROM schema_migrations WHERE filename = '$name' LIMIT 1;" | grep -q "1"; then
    printf "skip %s\n" "$name"
    return 0
  fi

  if [ "$name" = "011_create_funding_arbitrage_opportunities.sql" ]; then
    repair_legacy_funding_arbitrage_opportunities
  fi

  sqlite3 "$DB_PATH" <"$file"
  sqlite3 "$DB_PATH" "INSERT INTO schema_migrations(filename) VALUES('$name');"
  printf "applied %s\n" "$name"
}

list_files() {
  ls -1 "$MIGRATIONS_DIR"/*.sql 2>/dev/null | sort -V
}

case "$CMD" in
  run)
    while IFS= read -r f; do
      apply_file "$f"
    done < <(list_files)
    ;;
  status)
    while IFS= read -r f; do
      n=$(basename "$f")
      if sqlite3 "$DB_PATH" "SELECT 1 FROM schema_migrations WHERE filename = '$n' LIMIT 1;" | grep -q "1"; then
        printf "[applied] %s\n" "$n"
      else
        printf "[pending] %s\n" "$n"
      fi
    done < <(list_files)
    ;;
  list)
    list_files | xargs -n1 basename
    ;;
  *)
    printf "usage: %s [run|status|list]\n" "$0"
    exit 1
    ;;
esac
