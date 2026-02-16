-- NeuraTrade SQLite Schema
-- Run this to initialize the SQLite database

-- Users table
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    telegram_id TEXT,
    email TEXT,
    username TEXT,
    risk_level TEXT DEFAULT 'medium',
    mode TEXT DEFAULT 'live',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Wallets table
CREATE TABLE IF NOT EXISTS wallets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER,
    name TEXT NOT NULL,
    exchange TEXT NOT NULL,
    api_key_encrypted TEXT,
    api_secret_encrypted TEXT,
    is_active INTEGER DEFAULT 1,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id)
);

-- Exchange API Keys table
CREATE TABLE IF NOT EXISTS exchange_api_keys (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER,
    exchange TEXT NOT NULL,
    api_key_encrypted TEXT,
    api_secret_encrypted TEXT,
    testnet INTEGER DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id)
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_users_telegram_id ON users(telegram_id);
CREATE INDEX IF NOT EXISTS idx_wallets_user_id ON wallets(user_id);
CREATE INDEX IF NOT EXISTS idx_wallets_exchange ON wallets(exchange);
CREATE INDEX IF NOT EXISTS idx_exchange_api_keys_user_id ON exchange_api_keys(user_id);
