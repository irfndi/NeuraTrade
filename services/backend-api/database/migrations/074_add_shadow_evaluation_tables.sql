CREATE TABLE IF NOT EXISTS shadow_decisions (
    id BIGSERIAL PRIMARY KEY,
    variant_id TEXT NOT NULL,
    live_decision_id BIGINT,
    symbol TEXT NOT NULL,
    action TEXT NOT NULL CHECK (action IN ('buy', 'sell', 'hold')),
    confidence DOUBLE PRECISION,
    size_pct DOUBLE PRECISION,
    entry_price NUMERIC(20, 8),
    stop_loss NUMERIC(20, 8),
    take_profit NUMERIC(20, 8),
    gate_allowed BOOLEAN NOT NULL DEFAULT FALSE,
    gate_reason TEXT,
    gate_code TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_shadow_decisions_variant_created_at
    ON shadow_decisions (variant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_shadow_decisions_live_decision_id
    ON shadow_decisions (live_decision_id);
CREATE INDEX IF NOT EXISTS idx_shadow_decisions_symbol_created_at
    ON shadow_decisions (symbol, created_at DESC);

CREATE TABLE IF NOT EXISTS shadow_outcomes (
    id BIGSERIAL PRIMARY KEY,
    shadow_decision_id BIGINT REFERENCES shadow_decisions(id) ON DELETE CASCADE,
    exit_price NUMERIC(20, 8),
    realized_pnl NUMERIC(20, 8),
    max_favor NUMERIC(20, 8),
    max_adverse NUMERIC(20, 8),
    closed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_shadow_outcomes_shadow_decision_id
    ON shadow_outcomes (shadow_decision_id);
CREATE INDEX IF NOT EXISTS idx_shadow_outcomes_closed_at
    ON shadow_outcomes (closed_at DESC);

CREATE TABLE IF NOT EXISTS live_shadow_comparisons (
    id BIGSERIAL PRIMARY KEY,
    variant_id TEXT NOT NULL,
    comparison_window_start TIMESTAMPTZ,
    comparison_window_end TIMESTAMPTZ,
    live_pnl NUMERIC(20, 8) NOT NULL DEFAULT 0,
    shadow_pnl NUMERIC(20, 8) NOT NULL DEFAULT 0,
    pnl_divergence NUMERIC(20, 8) NOT NULL DEFAULT 0,
    live_win_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
    shadow_win_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
    live_trade_count INT NOT NULL DEFAULT 0,
    shadow_trade_count INT NOT NULL DEFAULT 0,
    live_rejection_count INT NOT NULL DEFAULT 0,
    shadow_rejection_count INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_live_shadow_comparisons_variant_created_at
    ON live_shadow_comparisons (variant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_live_shadow_comparisons_window
    ON live_shadow_comparisons (comparison_window_start, comparison_window_end);
