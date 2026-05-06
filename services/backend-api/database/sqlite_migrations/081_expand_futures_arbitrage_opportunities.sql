-- Rebuild SQLite futures arbitrage opportunities to match runtime inserts.
-- The original SQLite table had compact funding columns with NOT NULL
-- constraints that conflict with the current service/API insert contract.

DROP INDEX IF EXISTS idx_futures_arbitrage_is_active;
DROP INDEX IF EXISTS idx_futures_arbitrage_detected;
DROP INDEX IF EXISTS idx_futures_arbitrage_symbol;
DROP INDEX IF EXISTS idx_futures_arbitrage_expires;
DROP INDEX IF EXISTS idx_futures_arbitrage_unique;
DROP INDEX IF EXISTS idx_futures_arbitrage_opportunities_apy;
DROP INDEX IF EXISTS idx_futures_arbitrage_opportunities_active;

CREATE TABLE futures_arbitrage_opportunities_new (
    id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
    symbol TEXT NOT NULL,
    base_currency TEXT NOT NULL DEFAULT '',
    quote_currency TEXT NOT NULL DEFAULT '',
    long_exchange TEXT NOT NULL DEFAULT '',
    short_exchange TEXT NOT NULL DEFAULT '',
    -- Legacy numeric exchange IDs are preserved for migrated rows only; runtime
    -- inserts write exchange slugs and intentionally leave these nullable.
    long_exchange_id INTEGER,
    short_exchange_id INTEGER,
    long_funding_rate NUMERIC NOT NULL DEFAULT 0,
    short_funding_rate NUMERIC NOT NULL DEFAULT 0,
    net_funding_rate NUMERIC NOT NULL DEFAULT 0,
    funding_interval INTEGER NOT NULL DEFAULT 8,
    long_mark_price NUMERIC NOT NULL DEFAULT 0,
    short_mark_price NUMERIC NOT NULL DEFAULT 0,
    price_difference NUMERIC NOT NULL DEFAULT 0,
    price_difference_percentage NUMERIC NOT NULL DEFAULT 0,
    hourly_rate NUMERIC NOT NULL DEFAULT 0,
    daily_rate NUMERIC NOT NULL DEFAULT 0,
    apy NUMERIC NOT NULL DEFAULT 0,
    estimated_profit_8h NUMERIC NOT NULL DEFAULT 0,
    estimated_profit_daily NUMERIC NOT NULL DEFAULT 0,
    estimated_profit_weekly NUMERIC NOT NULL DEFAULT 0,
    estimated_profit_monthly NUMERIC NOT NULL DEFAULT 0,
    risk_score NUMERIC NOT NULL DEFAULT 0,
    volatility_score NUMERIC NOT NULL DEFAULT 0,
    liquidity_score NUMERIC NOT NULL DEFAULT 0,
    recommended_position_size NUMERIC,
    max_leverage NUMERIC NOT NULL DEFAULT 1,
    recommended_leverage NUMERIC DEFAULT 1,
    stop_loss_percentage NUMERIC,
    min_position_size NUMERIC,
    max_position_size NUMERIC,
    optimal_position_size NUMERIC,
    detected_at TEXT NOT NULL DEFAULT (datetime('now')),
    expires_at TEXT NOT NULL DEFAULT (datetime('now', '+1 hour')),
    next_funding_time TEXT,
    time_to_next_funding INTEGER,
    is_active INTEGER NOT NULL DEFAULT 1,
    market_trend TEXT,
    volume_24h NUMERIC,
    open_interest NUMERIC
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_futures_arbitrage_unique
ON futures_arbitrage_opportunities_new(symbol, long_exchange, short_exchange);

INSERT OR REPLACE INTO futures_arbitrage_opportunities_new (
    id, symbol, long_exchange, short_exchange, long_exchange_id, short_exchange_id,
    long_funding_rate, short_funding_rate, net_funding_rate,
    apy, risk_score, volume_24h, is_active, expires_at, detected_at
)
SELECT
    COALESCE(id, lower(hex(randomblob(16)))),
    symbol,
    COALESCE(CAST(buy_exchange_id AS TEXT), ''),
    COALESCE(CAST(sell_exchange_id AS TEXT), ''),
    buy_exchange_id,
    sell_exchange_id,
    funding_rate_buy,
    funding_rate_sell,
    rate_difference,
    apy,
    risk_score,
    volume_24h,
    COALESCE(is_active, 1),
    expires_at,
    detected_at
FROM futures_arbitrage_opportunities;

DROP TABLE futures_arbitrage_opportunities;
ALTER TABLE futures_arbitrage_opportunities_new RENAME TO futures_arbitrage_opportunities;

CREATE INDEX IF NOT EXISTS idx_futures_arbitrage_opportunities_symbol
ON futures_arbitrage_opportunities(symbol);

CREATE INDEX IF NOT EXISTS idx_futures_arbitrage_opportunities_apy
ON futures_arbitrage_opportunities(apy DESC);

CREATE INDEX IF NOT EXISTS idx_futures_arbitrage_opportunities_active
ON futures_arbitrage_opportunities(is_active, expires_at);

CREATE INDEX IF NOT EXISTS idx_futures_arbitrage_opportunities_detected_at
ON futures_arbitrage_opportunities(detected_at DESC);
