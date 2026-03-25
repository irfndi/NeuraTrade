BEGIN;

CREATE TABLE IF NOT EXISTS scalping_backtest_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    config JSONB NOT NULL DEFAULT '{}',
    status TEXT NOT NULL,
    summary JSONB NOT NULL DEFAULT '{}',
    CONSTRAINT chk_scalping_backtest_runs_status
        CHECK (status IN ('running', 'completed', 'failed'))
);

CREATE TABLE IF NOT EXISTS scalping_backtest_signals (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id UUID NOT NULL,
    timestamp TIMESTAMPTZ NOT NULL,
    symbol TEXT NOT NULL,
    exchange TEXT NOT NULL,
    signal JSONB NOT NULL DEFAULT '{}',
    regime TEXT NOT NULL,
    regime_volatility TEXT NOT NULL,
    funnel_stage TEXT NOT NULL,
    rejection_reason TEXT,
    gate_results JSONB NOT NULL DEFAULT '{}',
    CONSTRAINT uq_scalping_backtest_signals_id_run UNIQUE (id, run_id),
    CONSTRAINT fk_scalping_backtest_signals_run
        FOREIGN KEY (run_id) REFERENCES scalping_backtest_runs(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS scalping_backtest_trades (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id UUID NOT NULL,
    signal_id UUID NOT NULL,
    symbol TEXT NOT NULL,
    side TEXT NOT NULL,
    size NUMERIC NOT NULL,
    notional NUMERIC NOT NULL,
    entry_price NUMERIC NOT NULL,
    exit_price NUMERIC NOT NULL,
    entry_timestamp TIMESTAMPTZ NOT NULL,
    exit_timestamp TIMESTAMPTZ NOT NULL,
    pnl NUMERIC NOT NULL,
    pnl_pct NUMERIC NOT NULL,
    fees NUMERIC NOT NULL,
    outcome TEXT NOT NULL,
    exit_reason TEXT NOT NULL,
    regime_at_entry TEXT NOT NULL,
    regime_at_exit TEXT NOT NULL,
    hold_duration_seconds INTEGER NOT NULL,
    CONSTRAINT fk_scalping_backtest_trades_run
        FOREIGN KEY (run_id) REFERENCES scalping_backtest_runs(id) ON DELETE CASCADE,
    CONSTRAINT fk_scalping_backtest_trades_signal_run
        FOREIGN KEY (signal_id, run_id) REFERENCES scalping_backtest_signals(id, run_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS scalping_backtest_gate_summary (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id UUID NOT NULL,
    gate_name TEXT NOT NULL,
    pass_count INTEGER NOT NULL DEFAULT 0,
    reject_count INTEGER NOT NULL DEFAULT 0,
    top_rejection_reasons JSONB NOT NULL DEFAULT '[]',
    breakdown_by_symbol JSONB NOT NULL DEFAULT '{}',
    breakdown_by_regime JSONB NOT NULL DEFAULT '{}',
    CONSTRAINT fk_scalping_backtest_gate_summary_run
        FOREIGN KEY (run_id) REFERENCES scalping_backtest_runs(id) ON DELETE CASCADE
);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'fk_scalping_backtest_signals_run'
    ) THEN
        ALTER TABLE scalping_backtest_signals
        ADD CONSTRAINT fk_scalping_backtest_signals_run
        FOREIGN KEY (run_id) REFERENCES scalping_backtest_runs(id) ON DELETE CASCADE;
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'fk_scalping_backtest_trades_run'
    ) THEN
        ALTER TABLE scalping_backtest_trades
        ADD CONSTRAINT fk_scalping_backtest_trades_run
        FOREIGN KEY (run_id) REFERENCES scalping_backtest_runs(id) ON DELETE CASCADE;
    END IF;
END $$;

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

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'fk_scalping_backtest_gate_summary_run'
    ) THEN
        ALTER TABLE scalping_backtest_gate_summary
        ADD CONSTRAINT fk_scalping_backtest_gate_summary_run
        FOREIGN KEY (run_id) REFERENCES scalping_backtest_runs(id) ON DELETE CASCADE;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_scalping_backtest_runs_created_at
    ON scalping_backtest_runs(created_at DESC);

CREATE INDEX IF NOT EXISTS idx_scalping_backtest_runs_status
    ON scalping_backtest_runs(status);

CREATE INDEX IF NOT EXISTS idx_scalping_backtest_runs_status_created_at
    ON scalping_backtest_runs(status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_scalping_backtest_signals_run_id
    ON scalping_backtest_signals(run_id);

CREATE INDEX IF NOT EXISTS idx_scalping_backtest_signals_timestamp
    ON scalping_backtest_signals(timestamp DESC);

CREATE INDEX IF NOT EXISTS idx_scalping_backtest_signals_symbol
    ON scalping_backtest_signals(symbol);

CREATE INDEX IF NOT EXISTS idx_scalping_backtest_signals_regime
    ON scalping_backtest_signals(regime);

CREATE INDEX IF NOT EXISTS idx_scalping_backtest_signals_run_symbol_timestamp
    ON scalping_backtest_signals(run_id, symbol, timestamp DESC);

CREATE INDEX IF NOT EXISTS idx_scalping_backtest_trades_run_id
    ON scalping_backtest_trades(run_id);

CREATE INDEX IF NOT EXISTS idx_scalping_backtest_trades_signal_id
    ON scalping_backtest_trades(signal_id);

CREATE INDEX IF NOT EXISTS idx_scalping_backtest_trades_symbol
    ON scalping_backtest_trades(symbol);

CREATE INDEX IF NOT EXISTS idx_scalping_backtest_trades_regime_entry
    ON scalping_backtest_trades(regime_at_entry);

CREATE INDEX IF NOT EXISTS idx_scalping_backtest_trades_regime_exit
    ON scalping_backtest_trades(regime_at_exit);

CREATE INDEX IF NOT EXISTS idx_scalping_backtest_trades_entry_timestamp
    ON scalping_backtest_trades(entry_timestamp DESC);

CREATE INDEX IF NOT EXISTS idx_scalping_backtest_gate_summary_run_id
    ON scalping_backtest_gate_summary(run_id);

CREATE INDEX IF NOT EXISTS idx_scalping_backtest_gate_summary_gate_name
    ON scalping_backtest_gate_summary(gate_name);

COMMIT;
