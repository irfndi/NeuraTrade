#!/bin/bash
set -e

# Check database mode
DATABASE_MODE="${DATABASE_MODE:-postgres}"

if [ "$DATABASE_MODE" = "sqlite" ]; then
  echo "Running in SQLite mode - skipping PostgreSQL migrations"
  # SQLite migrations are handled by the application at startup
else
  # PostgreSQL mode - run migrations
  # Fix DATABASE_URL if it has wrong hostname
  # When DATABASE_URL uses 'postgres' hostname but DATABASE_HOST is set to external host,
  # patch DATABASE_URL or unset it to let the app use individual components
  if [ -n "$DATABASE_URL" ] && [ -n "$DATABASE_HOST" ]; then
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
