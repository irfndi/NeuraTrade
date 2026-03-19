PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS scalping_backtest_runs (
    id TEXT PRIMARY KEY,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at DATETIME,
    config TEXT NOT NULL DEFAULT '{}',
    status TEXT NOT NULL CHECK (status IN ('running', 'completed', 'failed')),
    summary TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS scalping_backtest_signals (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL,
    timestamp DATETIME NOT NULL,
    symbol TEXT NOT NULL,
    exchange TEXT NOT NULL,
    signal TEXT NOT NULL DEFAULT '{}',
    regime TEXT NOT NULL,
    regime_volatility TEXT NOT NULL,
    funnel_stage TEXT NOT NULL,
    rejection_reason TEXT,
    gate_results TEXT NOT NULL DEFAULT '{}',
    FOREIGN KEY (run_id) REFERENCES scalping_backtest_runs(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS scalping_backtest_trades (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL,
    signal_id TEXT NOT NULL,
    symbol TEXT NOT NULL,
    side TEXT NOT NULL,
    size NUMERIC NOT NULL,
    notional NUMERIC NOT NULL,
    entry_price NUMERIC NOT NULL,
    exit_price NUMERIC NOT NULL,
    entry_timestamp DATETIME NOT NULL,
    exit_timestamp DATETIME NOT NULL,
    pnl NUMERIC NOT NULL,
    pnl_pct NUMERIC NOT NULL,
    fees NUMERIC NOT NULL,
    outcome TEXT NOT NULL,
    exit_reason TEXT NOT NULL,
    regime_at_entry TEXT NOT NULL,
    regime_at_exit TEXT NOT NULL,
    hold_duration_seconds INTEGER NOT NULL,
    FOREIGN KEY (run_id) REFERENCES scalping_backtest_runs(id) ON DELETE CASCADE,
    FOREIGN KEY (signal_id) REFERENCES scalping_backtest_signals(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS scalping_backtest_gate_summary (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL,
    gate_name TEXT NOT NULL,
    pass_count INTEGER NOT NULL DEFAULT 0,
    reject_count INTEGER NOT NULL DEFAULT 0,
    top_rejection_reasons TEXT NOT NULL DEFAULT '[]',
    breakdown_by_symbol TEXT NOT NULL DEFAULT '{}',
    breakdown_by_regime TEXT NOT NULL DEFAULT '{}',
    FOREIGN KEY (run_id) REFERENCES scalping_backtest_runs(id) ON DELETE CASCADE
);

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
