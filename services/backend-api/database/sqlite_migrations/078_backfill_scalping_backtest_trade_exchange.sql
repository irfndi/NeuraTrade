PRAGMA foreign_keys = ON;

UPDATE scalping_backtest_trades
SET exchange = COALESCE(
    (
        SELECT NULLIF(s.exchange, '')
        FROM scalping_backtest_signals s
        WHERE s.id = scalping_backtest_trades.signal_id
    ),
    'unknown'
)
WHERE exchange = 'unknown';
