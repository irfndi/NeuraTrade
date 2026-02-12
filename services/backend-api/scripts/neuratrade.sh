#!/bin/bash

# NeuraTrade CLI - Bootstrap and manage your trading platform
# Usage: ./cli.sh [command] [options]

VERSION="1.0.0"
COMMIT="${GIT_COMMIT:-unknown}"
DATE="${BUILD_DATE:-$(date +%Y-%m-%d)}"

print_version() {
  echo "NeuraTrade CLI v${VERSION}"
  echo "  Commit: ${COMMIT}"
  echo "  Date:   ${DATE}"
}

print_help() {
  echo "NeuraTrade CLI - Bootstrap and manage your trading platform"
  echo ""
  echo "Usage: neuratrade [command] [options]"
  echo ""
  echo "Available Commands:"
  echo "  bootstrap    Initialize the application (database, services)"
  echo "  health       Check system health and dependencies"
  echo "  status       Show application status"
  echo "  help        Show this help message"
  echo ""
  echo "Examples:"
  echo "  ./cli.sh bootstrap"
  echo "  ./cli.sh health"
  echo ""
  echo "Options:"
  echo "  -h, --help     Show help for a command"
  echo "  -v, --version  Show version information"
}

run_bootstrap() {
  echo "Starting NeuraTrade bootstrap process..."

  # Check database connection
  DB_HOST="${DATABASE_HOST:-localhost}"
  DB_PORT="${DATABASE_PORT:-5432}"
  DB_NAME="${DATABASE_NAME:-neuratrade}"
  DB_USER="${DATABASE_USER:-neuratrade_user}"

  echo "Checking database connection..."
  if command -v pg_isready &>/dev/null; then
    if pg_isready -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME"; then
      echo "✓ Database: healthy"
    else
      echo "✗ Database: connection failed"
      exit 1
    fi
  else
    echo "⚠ pg_isready not available, skipping database check"
  fi

  # Check Redis connection
  if command -v redis-cli &>/dev/null; then
    REDIS_HOST="${REDIS_HOST:-localhost}"
    REDIS_PORT="${REDIS_PORT:-6379}"

    if redis-cli -h "$REDIS_HOST" -p "$REDIS_PORT" ping &>/dev/null; then
      echo "✓ Redis: healthy"
    else
      echo "⚠ Redis: not available (non-critical)"
    fi
  else
    echo "⚠ redis-cli not available, skipping Redis check"
  fi

  echo ""
  echo "Bootstrap completed successfully"
}

run_health() {
  echo "Running system health check..."
  echo ""

  # Check database
  DB_HOST="${DATABASE_HOST:-localhost}"
  DB_PORT="${DATABASE_PORT:-5432}"
  DB_NAME="${DATABASE_NAME:-neuratrade}"
  DB_USER="${DATABASE_USER:-neuratrade_user}"

  if command -v pg_isready &>/dev/null; then
    if pg_isready -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" &>/dev/null; then
      echo "✓ Database: healthy"
    else
      echo "✗ Database: unhealthy"
    fi
  else
    echo "⚠ Database: pg_isready not available"
  fi

  # Check Redis
  REDIS_HOST="${REDIS_HOST:-localhost}"
  REDIS_PORT="${REDIS_PORT:-6379}"

  if command -v redis-cli &>/dev/null; then
    if redis-cli -h "$REDIS_HOST" -p "$REDIS_PORT" ping &>/dev/null; then
      echo "✓ Redis: healthy"
    else
      echo "✗ Redis: unhealthy"
    fi
  else
    echo "⚠ Redis: redis-cli not available"
  fi

  echo ""
  echo "Health check completed"
}

run_status() {
  echo "NeuraTrade Status"
  echo "=================="
  echo ""
  echo "Version: ${VERSION}"
  echo "Environment: ${ENVIRONMENT:-development}"
  echo ""

  DB_HOST="${DATABASE_HOST:-localhost}"
  echo "Database: ${DB_HOST}:${DATABASE_PORT:-5432}/${DATABASE_NAME:-neuratrade}"

  REDIS_HOST="${REDIS_HOST:-localhost}"
  echo "Redis: ${REDIS_HOST}:${REDIS_PORT:-6379}"
}

# Main command dispatcher
case "${1:-help}" in
  version | --version | -v)
    print_version
    ;;
  bootstrap)
    run_bootstrap
    ;;
  health)
    run_health
    ;;
  status)
    run_status
    ;;
  help | --help | -h | "")
    print_help
    ;;
  *)
    echo "Unknown command: $1"
    echo ""
    print_help
    exit 1
    ;;
esac
