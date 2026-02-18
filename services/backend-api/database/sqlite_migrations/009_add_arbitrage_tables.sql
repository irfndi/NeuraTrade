CREATE TABLE IF NOT EXISTS arbitrage_opportunities (
    id TEXT PRIMARY KEY,
    buy_exchange_id INTEGER NOT NULL,
    sell_exchange_id INTEGER NOT NULL,
    trading_pair_id INTEGER NOT NULL,
    buy_price REAL NOT NULL,
    sell_price REAL NOT NULL,
    profit_percentage REAL NOT NULL,
    detected_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    executed_at TEXT,
    status TEXT DEFAULT 'open'
);

CREATE INDEX IF NOT EXISTS idx_arbitrage_status ON arbitrage_opportunities(status);
CREATE INDEX IF NOT EXISTS idx_arbitrage_detected ON arbitrage_opportunities(detected_at);
CREATE INDEX IF NOT EXISTS idx_arbitrage_trading_pair ON arbitrage_opportunities(trading_pair_id);

CREATE TABLE IF NOT EXISTS futures_arbitrage_opportunities (
    id TEXT PRIMARY KEY,
    symbol TEXT NOT NULL,
    buy_exchange_id INTEGER NOT NULL,
    sell_exchange_id INTEGER NOT NULL,
    funding_rate_buy REAL NOT NULL,
    funding_rate_sell REAL NOT NULL,
    rate_difference REAL NOT NULL,
    apy REAL NOT NULL,
    risk_score REAL NOT NULL,
    volume_24h REAL,
    is_active INTEGER DEFAULT 1,
    expires_at TEXT NOT NULL,
    detected_at TEXT NOT NULL,
    executed_at TEXT,
    profit_realized REAL,
    notes TEXT
);

CREATE INDEX IF NOT EXISTS idx_futures_arbitrage_is_active ON futures_arbitrage_opportunities(is_active);
CREATE INDEX IF NOT EXISTS idx_futures_arbitrage_detected ON futures_arbitrage_opportunities(detected_at);
CREATE INDEX IF NOT EXISTS idx_futures_arbitrage_symbol ON futures_arbitrage_opportunities(symbol);
CREATE INDEX IF NOT EXISTS idx_futures_arbitrage_expires ON futures_arbitrage_opportunities(expires_at);
