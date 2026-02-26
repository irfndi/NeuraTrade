# Database Migrations

This directory contains ordered SQL migrations for NeuraTrade schema evolution.

## Migration Rules

- Filename format: `NNN_descriptive_name.sql`
- Migrations run in version order
- Prefer additive, forward-only changes
- Use idempotent SQL where possible (`IF NOT EXISTS`, guarded updates)

## SQLite-First Entry Point

```bash
# Apply all pending SQLite migrations
./sqlite-migrate.sh run

# Show applied/pending SQLite migrations
./sqlite-migrate.sh status

# List migration files
./sqlite-migrate.sh list
```

Default DB path is `database/neuratrade.db` and can be overridden with `SQLITE_DB_PATH`.

## PostgreSQL Entry Point

```bash
# Run all pending migrations
./migrate.sh run

# Check status
./migrate.sh status

# List files
./migrate.sh list

# Run to target migration version
./migrate.sh 052
```

## Environment Variables

```bash
export DB_HOST=localhost
export DB_PORT=5432
export DB_NAME=neuratrade
export DB_USER=neuratrade_user
export DB_PASSWORD=your_password
```

## Creating New Migrations

1. Use sequential numbering and clear names, for example `072_add_trade_events.sql`.
2. Keep each migration deterministic and re-runnable where possible.
3. Validate both fresh install and upgrade paths.
