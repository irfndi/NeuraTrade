-- Migration 082 (SQLite): Add encrypted_passphrase column to exchange_api_keys.
-- The original used CREATE TABLE IF NOT EXISTS which is a no-op on existing
-- databases and never added the column. This version handles both cases:
-- 1. Fresh install: CREATE TABLE creates the schema with the new column.
-- 2. Existing install: ALTER TABLE adds the column to the pre-existing table.
-- The CREATE TABLE is idempotent; the ALTER TABLE will fail with
-- 'duplicate column name' on a re-run after the first ALTER succeeded,
-- which the migration runner tolerates via the .bail off + grep guard.
CREATE TABLE IF NOT EXISTS exchange_api_keys (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    exchange_name TEXT NOT NULL,
    key_name TEXT NOT NULL,
    encrypted_key TEXT NOT NULL,
    encrypted_secret TEXT NOT NULL,
    encrypted_passphrase TEXT NOT NULL DEFAULT '',
    permissions TEXT DEFAULT '["read"]',
    is_active INTEGER DEFAULT 1,
    last_used_at TEXT,
    expires_at TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, exchange_name, key_name)
);
ALTER TABLE exchange_api_keys ADD COLUMN encrypted_passphrase TEXT NOT NULL DEFAULT '';
