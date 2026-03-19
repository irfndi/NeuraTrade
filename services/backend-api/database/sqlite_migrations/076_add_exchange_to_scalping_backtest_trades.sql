PRAGMA foreign_keys = ON;

ALTER TABLE scalping_backtest_trades ADD COLUMN exchange TEXT;

UPDATE scalping_backtest_trades
SET exchange = (
    SELECT s.exchange
    FROM scalping_backtest_signals s
    WHERE s.id = scalping_backtest_trades.signal_id
)
WHERE exchange IS NULL OR exchange = '';

CREATE INDEX IF NOT EXISTS idx_scalping_backtest_trades_exchange
    ON scalping_backtest_trades(exchange);
