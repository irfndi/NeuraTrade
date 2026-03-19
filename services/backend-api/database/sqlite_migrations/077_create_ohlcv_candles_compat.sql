PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS ohlcv_candles (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    exchange_id INTEGER NOT NULL,
    trading_pair_id INTEGER NOT NULL,
    timeframe TEXT,
    open NUMERIC NOT NULL,
    high NUMERIC NOT NULL,
    low NUMERIC NOT NULL,
    close NUMERIC NOT NULL,
    volume NUMERIC NOT NULL,
    timestamp DATETIME NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (exchange_id) REFERENCES exchanges(id) ON DELETE CASCADE,
    FOREIGN KEY (trading_pair_id) REFERENCES trading_pairs(id) ON DELETE CASCADE,
    UNIQUE(exchange_id, trading_pair_id, timeframe, timestamp)
);

CREATE INDEX IF NOT EXISTS idx_ohlcv_candles_exchange_pair_timeframe_timestamp
    ON ohlcv_candles(exchange_id, trading_pair_id, timeframe, timestamp DESC);
