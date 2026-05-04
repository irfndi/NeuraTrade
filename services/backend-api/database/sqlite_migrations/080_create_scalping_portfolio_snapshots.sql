CREATE TABLE IF NOT EXISTS scalping_portfolio_snapshots (
    id TEXT PRIMARY KEY,
    chat_id TEXT,
    exchange TEXT,
    snapshot_at TIMESTAMP NOT NULL,
    usdt_balance NUMERIC NOT NULL DEFAULT 0,
    total_value NUMERIC NOT NULL DEFAULT 0,
    open_positions INT NOT NULL DEFAULT 0,
    unrealized_pnl NUMERIC NOT NULL DEFAULT 0,
    current_drawdown NUMERIC NOT NULL DEFAULT 0,
    risk_sharpe NUMERIC NOT NULL DEFAULT 0,
    risk_sortino NUMERIC NOT NULL DEFAULT 0,
    risk_drawdown NUMERIC NOT NULL DEFAULT 0,
    risk_max_drawdown NUMERIC NOT NULL DEFAULT 0,
    risk_expectancy NUMERIC NOT NULL DEFAULT 0,
    risk_expectancy_gross NUMERIC NOT NULL DEFAULT 0,
    risk_fee_drag_expectancy NUMERIC NOT NULL DEFAULT 0,
    risk_sample_size INT NOT NULL DEFAULT 0,
    strategy_phase TEXT,
    account_tier TEXT,
    recent_consecutive_losses INT NOT NULL DEFAULT 0,
    recovery_mode TEXT,
    drift_active BOOLEAN NOT NULL DEFAULT FALSE,
    no_fill_minutes NUMERIC NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_scalping_portfolio_snapshots_chat_time
    ON scalping_portfolio_snapshots(chat_id, snapshot_at DESC);
CREATE INDEX IF NOT EXISTS idx_scalping_portfolio_snapshots_exchange_time
    ON scalping_portfolio_snapshots(exchange, snapshot_at DESC);
