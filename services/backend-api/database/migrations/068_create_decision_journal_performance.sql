-- Create decision_journal table for tracking trade decision reasoning
-- neura-y146: Performance Feedback Pipeline

CREATE TABLE IF NOT EXISTS decision_journal (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    symbol TEXT NOT NULL,
    exchange TEXT NOT NULL,
    skill_id TEXT NOT NULL,
    decision_type TEXT NOT NULL, -- 'entry', 'exit', 'adjustment', 'rejection'
    action TEXT, -- 'open_long', 'open_short', 'close_long', 'close_short', 'hold', 'wait'
    side TEXT, -- 'long', 'short', 'flat'
    size_percent REAL,
    entry_price REAL,
    stop_loss REAL,
    take_profit REAL,
    confidence REAL,
    reasoning TEXT NOT NULL,
    regime_trend TEXT,
    regime_volatility TEXT,
    market_conditions JSONB,
    signals_used JSONB,
    risk_assessment JSONB,
    created_at TIMESTAMP NOT NULL DEFAULT (now() AT TIME ZONE 'utc'),
    updated_at TIMESTAMP NOT NULL DEFAULT (now() AT TIME ZONE 'utc')
);

CREATE INDEX IF NOT EXISTS idx_decision_journal_session ON decision_journal(session_id);
CREATE INDEX IF NOT EXISTS idx_decision_journal_symbol ON decision_journal(symbol);
CREATE INDEX IF NOT EXISTS idx_decision_journal_created ON decision_journal(created_at);
CREATE INDEX IF NOT EXISTS idx_decision_journal_skill ON decision_journal(skill_id);

-- Create trade_outcomes table for tracking actual trade results
CREATE TABLE IF NOT EXISTS trade_outcomes (
    id TEXT PRIMARY KEY,
    decision_journal_id TEXT REFERENCES decision_journal(id),
    symbol TEXT NOT NULL,
    exchange TEXT NOT NULL,
    skill_id TEXT NOT NULL,
    side TEXT NOT NULL, -- 'long', 'short'
    entry_price REAL NOT NULL,
    exit_price REAL,
    size REAL NOT NULL,
    pnl REAL, -- profit/loss in quote currency
    pnl_percent REAL, -- percentage gain/loss
    fees REAL DEFAULT 0,
    hold_duration_seconds INTEGER,
    outcome TEXT NOT NULL, -- 'win', 'loss', 'breakeven', 'pending', 'cancelled'
    exit_reason TEXT, -- 'take_profit', 'stop_loss', 'manual', 'timeout', 'liquidation'
    regime_at_entry TEXT,
    regime_at_exit TEXT,
    volatility_at_entry REAL,
    volatility_at_exit REAL,
    created_at TIMESTAMP NOT NULL DEFAULT (now() AT TIME ZONE 'utc'),
    updated_at TIMESTAMP NOT NULL DEFAULT (now() AT TIME ZONE 'utc')
);

CREATE INDEX IF NOT EXISTS idx_trade_outcomes_decision ON trade_outcomes(decision_journal_id);
CREATE INDEX IF NOT EXISTS idx_trade_outcomes_symbol ON trade_outcomes(symbol);
CREATE INDEX IF NOT EXISTS idx_trade_outcomes_skill ON trade_outcomes(skill_id);
CREATE INDEX IF NOT EXISTS idx_trade_outcomes_outcome ON trade_outcomes(outcome);
CREATE INDEX IF NOT EXISTS idx_trade_outcomes_created ON trade_outcomes(created_at);

-- Create failure_patterns table for tracking recurring failure modes
CREATE TABLE IF NOT EXISTS failure_patterns (
    id TEXT PRIMARY KEY,
    skill_id TEXT NOT NULL,
    pattern_type TEXT NOT NULL, -- 'consecutive_loss', 'stop_loss_hit', 'wrong_direction', 'timing', 'regime_mismatch'
    description TEXT NOT NULL,
    occurrence_count INTEGER NOT NULL DEFAULT 1,
    first_observed TIMESTAMP NOT NULL DEFAULT (now() AT TIME ZONE 'utc'),
    last_observed TIMESTAMP NOT NULL DEFAULT (now() AT TIME ZONE 'utc'),
    total_pnl_impact REAL DEFAULT 0,
    affected_symbols JSONB,
    suggested_fix TEXT,
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMP NOT NULL DEFAULT (now() AT TIME ZONE 'utc'),
    updated_at TIMESTAMP NOT NULL DEFAULT (now() AT TIME ZONE 'utc')
);

CREATE INDEX IF NOT EXISTS idx_failure_patterns_skill ON failure_patterns(skill_id);
CREATE INDEX IF NOT EXISTS idx_failure_patterns_type ON failure_patterns(pattern_type);
CREATE INDEX IF NOT EXISTS idx_failure_patterns_enabled ON failure_patterns(enabled);

-- Create strategy_parameters table for adaptive parameter tuning
CREATE TABLE IF NOT EXISTS strategy_parameters (
    id TEXT PRIMARY KEY,
    skill_id TEXT NOT NULL,
    symbol TEXT, -- NULL means applies to all symbols
    parameter_name TEXT NOT NULL,
    parameter_value REAL NOT NULL,
    min_value REAL,
    max_value REAL,
    tuning_reason TEXT,
    based_on_outcome_id TEXT REFERENCES trade_outcomes(id),
    regime_context TEXT, -- regime this parameter was tuned for
    confidence REAL DEFAULT 0.5,
    sample_size INTEGER DEFAULT 0,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMP NOT NULL DEFAULT (now() AT TIME ZONE 'utc'),
    updated_at TIMESTAMP NOT NULL DEFAULT (now() AT TIME ZONE 'utc'),
    UNIQUE(skill_id, symbol, parameter_name, is_active) 
    WHERE is_active = true
);

CREATE INDEX IF NOT EXISTS idx_strategy_params_skill ON strategy_parameters(skill_id);
CREATE INDEX IF NOT EXISTS idx_strategy_params_symbol ON strategy_parameters(symbol);
CREATE INDEX IF NOT EXISTS idx_strategy_params_active ON strategy_parameters(is_active);

-- Add foreign key constraint for trade_outcomes to decision_journal
ALTER TABLE trade_outcomes ADD CONSTRAINT fk_trade_outcomes_decision 
    FOREIGN KEY (decision_journal_id) REFERENCES decision_journal(id) ON DELETE SET NULL;
