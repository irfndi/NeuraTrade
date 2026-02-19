PRAGMA foreign_keys = ON;

-- Add updated_at column to exchange_blacklist if missing
ALTER TABLE exchange_blacklist ADD COLUMN updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP;

-- Create ccxt_exchanges table for SQLite compatibility
CREATE TABLE IF NOT EXISTS ccxt_exchanges (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    exchange_id INTEGER NOT NULL,
    ccxt_id TEXT NOT NULL,
    api_url TEXT,
    ws_url TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (exchange_id) REFERENCES exchanges(id) ON DELETE CASCADE,
    UNIQUE(exchange_id, ccxt_id)
);

-- Create indexes for ccxt_exchanges
CREATE INDEX IF NOT EXISTS idx_ccxt_exchanges_exchange_id ON ccxt_exchanges(exchange_id);
CREATE INDEX IF NOT EXISTS idx_ccxt_exchanges_ccxt_id ON ccxt_exchanges(ccxt_id);
