BEGIN;

CREATE OR REPLACE VIEW ohlcv_candles AS
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

COMMIT;
