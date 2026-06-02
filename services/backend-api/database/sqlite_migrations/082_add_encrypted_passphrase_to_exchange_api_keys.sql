-- Migration 082 (SQLite): Add encrypted_passphrase column to exchange_api_keys.
-- Fresh install: CREATE TABLE creates the schema with the column.
-- Existing install: 082 was a no-op (CREATE TABLE IF NOT EXISTS), so the
-- column was never added. The fix for that is in 086 (idempotent ALTER)
-- which uses PRAGMA table_info to skip when the column already exists.
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
