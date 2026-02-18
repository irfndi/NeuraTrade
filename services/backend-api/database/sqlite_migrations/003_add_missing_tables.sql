PRAGMA foreign_keys = ON;

-- Create exchanges table for SQLite compatibility
CREATE TABLE IF NOT EXISTS exchanges (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,
    ccxt_id TEXT NOT NULL UNIQUE,
    api_url TEXT,
    status TEXT DEFAULT 'active',
    has_spot BOOLEAN DEFAULT 1,
    has_futures BOOLEAN DEFAULT 0,
    is_active BOOLEAN DEFAULT 1,
    priority INTEGER DEFAULT 0,
    last_ping DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Create trading_pairs table for SQLite compatibility  
CREATE TABLE IF NOT EXISTS trading_pairs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    exchange_id INTEGER NOT NULL,
    symbol TEXT NOT NULL,
    base_currency TEXT NOT NULL,
    quote_currency TEXT NOT NULL,
    is_active BOOLEAN DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (exchange_id) REFERENCES exchanges(id) ON DELETE CASCADE,
    UNIQUE(exchange_id, symbol)
);

-- Create exchange_blacklist table for SQLite compatibility
CREATE TABLE IF NOT EXISTS exchange_blacklist (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    exchange_name TEXT NOT NULL UNIQUE,
    reason TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Create indexes for better performance
CREATE INDEX IF NOT EXISTS idx_exchanges_name ON exchanges(name);
CREATE INDEX IF NOT EXISTS idx_exchanges_ccxt_id ON exchanges(ccxt_id);
CREATE INDEX IF NOT EXISTS idx_exchanges_status ON exchanges(status);
CREATE INDEX IF NOT EXISTS idx_exchanges_active ON exchanges(is_active);
CREATE INDEX IF NOT EXISTS idx_trading_pairs_symbol ON trading_pairs(symbol);
CREATE INDEX IF NOT EXISTS idx_trading_pairs_active ON trading_pairs(is_active);
CREATE INDEX IF NOT EXISTS idx_trading_pairs_exchange ON trading_pairs(exchange_id);