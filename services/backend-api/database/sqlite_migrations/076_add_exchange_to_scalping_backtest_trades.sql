PRAGMA foreign_keys = ON;

-- Add column as nullable first so the UPDATE below can match existing rows.
-- SQLite does not support ALTER COLUMN ... SET NOT NULL, so the column remains
-- nullable here (application code always provides exchange on insert).
ALTER TABLE scalping_backtest_trades ADD COLUMN exchange TEXT;

-- Backfill from linked signal or fall back to 'unknown'.
UPDATE scalping_backtest_trades
SET exchange = COALESCE(
    (
        SELECT NULLIF(s.exchange, '')
        FROM scalping_backtest_signals s
        WHERE s.id = scalping_backtest_trades.signal_id
    ),
    'unknown'
)
WHERE exchange IS NULL OR exchange = '';

CREATE INDEX IF NOT EXISTS idx_scalping_backtest_trades_exchange
    ON scalping_backtest_trades(exchange);
