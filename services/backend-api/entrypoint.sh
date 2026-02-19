#!/bin/bash
set -euo pipefail

DATABASE_DRIVER="${DATABASE_DRIVER:-${DATABASE_MODE:-sqlite}}"
DATABASE_DRIVER="$(printf '%s' "$DATABASE_DRIVER" | tr '[:upper:]' '[:lower:]')"
export DATABASE_DRIVER

if [ "$DATABASE_DRIVER" = "sqlite" ]; then
  echo "Running in SQLite mode - migrations handled by application"
  SQLITE_DB_PATH="${SQLITE_DB_PATH:-neuratrade.db}"
  echo "SQLite database path: $SQLITE_DB_PATH"
else
  # PostgreSQL mode - run migrations
  # Fix DATABASE_URL if it has wrong hostname
  if [ -n "${DATABASE_URL:-}" ] && [ -n "${DATABASE_HOST:-}" ]; then
    if [[ "$DATABASE_URL" == *"@postgres:"* ]] && [ "$DATABASE_HOST" != "postgres" ]; then
      echo "Patching DATABASE_URL: Replacing 'postgres' host with '${DATABASE_HOST}'..."
      export DATABASE_URL="${DATABASE_URL/@postgres:/@$DATABASE_HOST:}"
    fi
  fi

  # Run migrations
  echo "Running database migrations..."
  if [ -f "database/migrate.sh" ]; then
    chmod +x database/migrate.sh
    ./database/migrate.sh
  else
    echo "Migration script not found at ./database/migrate.sh"
  fi
fi

# Start application
echo "Starting application..."
exec ./main
