-- Migration 082 (SQLite): Add encrypted_passphrase column to exchange_api_keys
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
