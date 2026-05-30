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

apply_paper_trades_column_migrations() {
  if ! sqlite_table_exists "paper_trades"; then
    printf "error: paper_trades table missing; 015 depends on 012_create_paper_trades_table.sql\n" >&2
    return 1
  fi

  # Defensive migration: add missing columns only if they don't already exist.
  # On fresh databases created after 012_create_paper_trades_table.sql, all
  # columns are already present, so this becomes a no-op.
  local -a cols=("strategy_id" "quest_id" "exchange" "symbol" "side" "size" "fees" "cost_basis" "updated_at")
  local -a defs=(
    "TEXT NOT NULL DEFAULT ''"
    "INTEGER"
    "TEXT NOT NULL DEFAULT ''"
    "TEXT NOT NULL DEFAULT ''"
    "TEXT NOT NULL DEFAULT 'buy'"
    "DECIMAL(20, 8) NOT NULL DEFAULT 0"
    "DECIMAL(20, 8) NOT NULL DEFAULT 0"
    "DECIMAL(20, 8) NOT NULL DEFAULT 0"
    "TEXT NOT NULL DEFAULT '1970-01-01T00:00:00Z'"
  )

  if [ ${#cols[@]} -ne ${#defs[@]} ]; then
    printf "error: cols/defs length mismatch (%d vs %d)\n" "${#cols[@]}" "${#defs[@]}" >&2
    return 1
  fi

  for i in "${!cols[@]}"; do
    if ! sqlite_column_exists "paper_trades" "${cols[$i]}"; then
      sqlite3 "$DB_PATH" "ALTER TABLE paper_trades ADD COLUMN ${cols[$i]} ${defs[$i]};"
    fi
  done

  # Backfill updated_at for existing rows that got the epoch placeholder default.
  sqlite3 "$DB_PATH" "UPDATE paper_trades SET updated_at = CURRENT_TIMESTAMP WHERE updated_at = '1970-01-01T00:00:00Z';"
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

  # 015_alter_paper_trades_columns.sql is intentionally bypassed here.
  # The raw SQL uses .bail off to tolerate duplicate columns, but sqlite3
  # still returns non-zero when statements fail. We run a shell-side
  # conditional check (pragma_table_info) so the migration is idempotent
  # on both fresh and legacy databases.
  if [ "$name" = "015_alter_paper_trades_columns.sql" ]; then
    apply_paper_trades_column_migrations
    sqlite3 "$DB_PATH" "INSERT INTO schema_migrations(filename) VALUES('$name');"
    printf "applied %s\n" "$name"
    return 0
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
