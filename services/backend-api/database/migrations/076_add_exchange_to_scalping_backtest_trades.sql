BEGIN;

ALTER TABLE scalping_backtest_trades
    ADD COLUMN IF NOT EXISTS exchange TEXT;

UPDATE scalping_backtest_trades t
SET exchange = s.exchange
FROM scalping_backtest_signals s
WHERE t.signal_id = s.id
  AND (t.exchange IS NULL OR t.exchange = '');

ALTER TABLE scalping_backtest_trades
    ALTER COLUMN exchange SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_scalping_backtest_trades_exchange
    ON scalping_backtest_trades(exchange);

COMMIT;
