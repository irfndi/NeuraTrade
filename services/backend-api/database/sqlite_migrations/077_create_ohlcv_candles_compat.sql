PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS ohlcv_data (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    exchange_id INTEGER NOT NULL,
    trading_pair_id INTEGER NOT NULL,
    timeframe TEXT NOT NULL,
    open_price NUMERIC NOT NULL,
    high_price NUMERIC NOT NULL,
    low_price NUMERIC NOT NULL,
    close_price NUMERIC NOT NULL,
    volume NUMERIC NOT NULL,
    timestamp DATETIME NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (exchange_id) REFERENCES exchanges(id) ON DELETE CASCADE,
    FOREIGN KEY (trading_pair_id) REFERENCES trading_pairs(id) ON DELETE CASCADE,
    UNIQUE(exchange_id, trading_pair_id, timeframe, timestamp)
);

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

INSERT OR IGNORE INTO ohlcv_candles (
    exchange_id,
    trading_pair_id,
    timeframe,
    open,
    high,
    low,
    close,
    volume,
    timestamp,
    created_at
)
SELECT
    exchange_id,
    trading_pair_id,
    timeframe,
    open_price,
    high_price,
    low_price,
    close_price,
    volume,
    timestamp,
    created_at
FROM ohlcv_data;
