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

## Rollback Convention (`.down.sql`)

Every new migration should include a companion `.down.sql` file that reverses its changes.
This enables clean rollbacks when a deployment fails.

### Convention

| File | Purpose |
|------|---------|
| `089_add_feature_table.sql` | Forward migration (create table, add columns, etc.) |
| `089_add_feature_table.down.sql` | Reverse migration (DROP TABLE, DROP COLUMN, etc.) |

### Rules

- Name the `.down.sql` file identically to the forward migration, with `.down` before `.sql`.
- Place it in the same directory as the forward migration file.
- Write reversible SQL for every DDL operation.
- Use `IF EXISTS` guards to make rollbacks idempotent.
- Keep the down migration scoped to exactly what the forward migration introduced.

### Example

**Forward** (`migrations/089_add_feature_table.sql`):
```sql
CREATE TABLE IF NOT EXISTS feature_flags (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) UNIQUE NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
```

**Reverse** (`migrations/089_add_feature_table.down.sql`):
```sql
DROP TABLE IF EXISTS feature_flags;
```

### Existing rollback scripts

The script `migrate.sh` supports a `rollback` subcommand that removes the migration record
from `schema_migrations`. It does NOT execute the `.down.sql` automatically — you must
run it separately:

```bash
# 1. Apply the down SQL manually
psql -h $DB_HOST -U $DB_USER -d $DB_NAME -f migrations/089_add_feature_table.down.sql

# 2. Remove the migration record
./migrate.sh rollback 089
```

We recommend automating this in the future by extending `migrate.sh` to detect and apply
`.down.sql` files automatically when rolling back.

## Pre-Migration Backups

Before any migration that runs as part of a deployment (see `docs/DEPLOYMENT.md`), a database
backup is automatically created. The backup is stored in `/opt/neuratrade/backups/` on the
target VPS with a timestamp suffix.

### Backup commands

**SQLite:**
```bash
sqlite3 /path/to/neuratrade.db ".backup /opt/neuratrade/backups/neuratrade_$(date +%Y%m%d_%H%M%S).db"
```

**PostgreSQL:**
```bash
pg_dump -U neuratrade_user -d neuratrade > /opt/neuratrade/backups/neuratrade_$(date +%Y%m%d_%H%M%S).sql
```

### Restore commands

**SQLite:**
```bash
sqlite3 /path/to/neuratrade.db ".restore /opt/neuratrade/backups/neuratrade_20260101_120000.db"
```

**PostgreSQL:**
```bash
psql -U neuratrade_user -d neuratrade -f /opt/neuratrade/backups/neuratrade_20260101_120000.sql
```
