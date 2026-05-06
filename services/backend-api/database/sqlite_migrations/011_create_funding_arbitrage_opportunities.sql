PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS funding_arbitrage_opportunities (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    trading_pair_id INTEGER NOT NULL REFERENCES trading_pairs(id) ON DELETE CASCADE,
    long_exchange_id INTEGER NOT NULL REFERENCES exchanges(id) ON DELETE CASCADE,
    short_exchange_id INTEGER NOT NULL REFERENCES exchanges(id) ON DELETE CASCADE,
    long_funding_rate REAL NOT NULL,
    short_funding_rate REAL NOT NULL,
    net_funding_rate REAL NOT NULL,
    estimated_profit_8h REAL NOT NULL CHECK (estimated_profit_8h >= 0),
    estimated_profit_daily REAL NOT NULL CHECK (estimated_profit_daily >= 0),
    estimated_profit_percentage REAL NOT NULL CHECK (estimated_profit_percentage >= 0),
    long_mark_price REAL,
    short_mark_price REAL,
    price_difference REAL,
    price_difference_percentage REAL,
    risk_score REAL DEFAULT 1.0 CHECK (risk_score >= 1.0 AND risk_score <= 5.0),
    is_active INTEGER DEFAULT 1,
    detected_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(trading_pair_id, long_exchange_id, short_exchange_id, detected_at),
    CHECK (long_exchange_id <> short_exchange_id)
);

CREATE INDEX IF NOT EXISTS idx_funding_arbitrage_active
    ON funding_arbitrage_opportunities(is_active, detected_at DESC)
    WHERE is_active = 1;
CREATE INDEX IF NOT EXISTS idx_funding_arbitrage_profit
    ON funding_arbitrage_opportunities(estimated_profit_percentage DESC);
CREATE INDEX IF NOT EXISTS idx_funding_arbitrage_active_filter
    ON funding_arbitrage_opportunities(is_active)
    WHERE is_active = 1;
CREATE INDEX IF NOT EXISTS idx_funding_arbitrage_expires
    ON funding_arbitrage_opportunities(expires_at)
    WHERE expires_at IS NOT NULL;
