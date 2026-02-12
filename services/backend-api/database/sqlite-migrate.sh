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

apply_file() {
  local file="$1"
  local name
  name=$(basename "$file")

  if sqlite3 "$DB_PATH" "SELECT 1 FROM schema_migrations WHERE filename = '$name' LIMIT 1;" | grep -q "1"; then
    printf "skip %s\n" "$name"
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
