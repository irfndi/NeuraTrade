#!/bin/bash

# Load environment variables
if [ -f .env ]; then
  export $(grep -v '^#' .env | xargs)
fi

# Database connection parameters
DB_HOST=${DB_HOST:-localhost}
DB_PORT=${DB_PORT:-5432}
DB_NAME=${DB_NAME:-neuratrade}
DB_USER=${DB_USER:-postgres}

# Security: DB_PASSWORD must be provided via environment variable
if [ -z "$DB_PASSWORD" ]; then
  echo "❌ Error: DB_PASSWORD environment variable is required for security."
  echo "   Please set DB_PASSWORD before running this script."
  echo "   Example: export DB_PASSWORD=your_password"
  exit 1
fi

echo "🔍 Checking NeuraTrade Database Status..."
echo "=====================================\n"

# Function to execute SQL and format output
execute_sql() {
  local query="$1"
  local description="$2"

  echo "📊 $description"
  echo "-----------------------------------"

  PGPASSWORD="$DB_PASSWORD" psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -t -c "$query" 2>/dev/null | sed 's/^[ \t]*//;s/[ \t]*$//' || echo "❌ Connection failed"
  echo ""
}

# Test database connection
echo "🔌 Testing database connection..."
execute_sql "SELECT version();" "PostgreSQL Version"

# Check if database exists
execute_sql "SELECT current_database();" "Current Database"

# Check table count
execute_sql "SELECT count(*) as table_count FROM information_schema.tables WHERE table_schema = 'public';" "Number of Tables"

# Check recent activity
execute_sql "SELECT count(*) as active_connections FROM pg_stat_activity WHERE state = 'active';" "Active Connections"

echo "✅ Database status check completed!"
