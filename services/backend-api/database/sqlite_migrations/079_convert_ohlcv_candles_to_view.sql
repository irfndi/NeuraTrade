PRAGMA foreign_keys = OFF;

DROP VIEW IF EXISTS ohlcv_candles;
DROP TABLE IF EXISTS ohlcv_candles;

CREATE INDEX IF NOT EXISTS idx_ohlcv_data_exchange_pair_timeframe_timestamp
    ON ohlcv_data(exchange_id, trading_pair_id, timeframe, timestamp DESC);

CREATE VIEW IF NOT EXISTS ohlcv_candles AS
SELECT
    id,
    exchange_id,
    trading_pair_id,
    timeframe,
    open_price AS open,
    high_price AS high,
    low_price AS low,
    close_price AS close,
    volume,
    timestamp,
    created_at
FROM ohlcv_data;

PRAGMA foreign_keys = ON;
