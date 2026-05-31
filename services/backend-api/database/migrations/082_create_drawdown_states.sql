CREATE TABLE IF NOT EXISTS drawdown_states (
    chat_id TEXT PRIMARY KEY,
    peak_value TEXT NOT NULL DEFAULT '0',
    current_value TEXT NOT NULL DEFAULT '0',
    current_drawdown TEXT NOT NULL DEFAULT '0',
    max_drawdown_seen TEXT NOT NULL DEFAULT '0',
    status TEXT NOT NULL DEFAULT 'normal',
    trading_halted BOOLEAN NOT NULL DEFAULT FALSE,
    halted_at TIMESTAMP,
    recovered_at TIMESTAMP,
    last_checked TIMESTAMP NOT NULL,
    warning_count INTEGER NOT NULL DEFAULT 0,
    halt_count INTEGER NOT NULL DEFAULT 0,
    updated_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_drawdown_states_trading_halted
    ON drawdown_states(trading_halted, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_drawdown_states_status
    ON drawdown_states(status, updated_at DESC);
