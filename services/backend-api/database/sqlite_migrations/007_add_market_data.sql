-- Migration 007: Add market_data table
-- Created: 2026-02-17
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS market_data (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    exchange_id INTEGER NOT NULL,
    trading_pair_id INTEGER NOT NULL,
    bid DECIMAL(20, 8),
    bid_volume DECIMAL(20, 8),
    ask DECIMAL(20, 8),
    ask_volume DECIMAL(20, 8),
    last_price DECIMAL(20, 8) NOT NULL,
    volume_24h DECIMAL(20, 8),
    timestamp DATETIME NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (exchange_id) REFERENCES exchanges(id) ON DELETE CASCADE,
    FOREIGN KEY (trading_pair_id) REFERENCES trading_pairs(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_market_data_exchange ON market_data(exchange_id);
CREATE INDEX IF NOT EXISTS idx_market_data_pair ON market_data(trading_pair_id);
CREATE INDEX IF NOT EXISTS idx_market_data_timestamp ON market_data(timestamp);
