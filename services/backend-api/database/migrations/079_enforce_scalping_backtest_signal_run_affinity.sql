DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'uq_scalping_backtest_signals_id_run'
    ) THEN
        ALTER TABLE scalping_backtest_signals
        ADD CONSTRAINT uq_scalping_backtest_signals_id_run UNIQUE (id, run_id);
    END IF;
END $$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'fk_scalping_backtest_trades_signal'
    ) THEN
        ALTER TABLE scalping_backtest_trades
        DROP CONSTRAINT fk_scalping_backtest_trades_signal;
    END IF;
END $$;

DELETE FROM scalping_backtest_trades t
WHERE signal_id IS NOT NULL
  AND NOT EXISTS (
      SELECT 1
      FROM scalping_backtest_signals s
      WHERE s.id = t.signal_id
        AND s.run_id = t.run_id
  );

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'fk_scalping_backtest_trades_signal_run'
    ) THEN
        ALTER TABLE scalping_backtest_trades
        ADD CONSTRAINT fk_scalping_backtest_trades_signal_run
        FOREIGN KEY (signal_id, run_id) REFERENCES scalping_backtest_signals(id, run_id) ON DELETE CASCADE;
    END IF;
END $$;
