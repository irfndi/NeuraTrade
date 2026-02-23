-- Migration 008: Add funding_rates table
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS funding_rates (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    exchange_id INTEGER NOT NULL,
    trading_pair_id INTEGER NOT NULL,
    funding_rate DECIMAL(20, 8) NOT NULL,
    funding_time DATETIME NOT NULL,
    next_funding_time DATETIME,
    mark_price DECIMAL(20, 8),
    index_price DECIMAL(20, 8),
    timestamp DATETIME NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (exchange_id) REFERENCES exchanges(id) ON DELETE CASCADE,
    FOREIGN KEY (trading_pair_id) REFERENCES trading_pairs(id) ON DELETE CASCADE,
    UNIQUE(exchange_id, trading_pair_id, funding_time)
);

CREATE INDEX IF NOT EXISTS idx_funding_rates_exchange ON funding_rates(exchange_id);
CREATE INDEX IF NOT EXISTS idx_funding_rates_pair ON funding_rates(trading_pair_id);
CREATE INDEX IF NOT EXISTS idx_funding_rates_time ON funding_rates(funding_time);
