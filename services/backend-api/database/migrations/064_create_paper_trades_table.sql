-- Create paper_trades table for paper trading recording
-- Created: 2026-02-13

CREATE TABLE IF NOT EXISTS paper_trades (
    id BIGSERIAL PRIMARY KEY,
    user_id UUID NOT NULL,
    quest_id BIGINT,
    strategy_id TEXT NOT NULL,
    exchange TEXT NOT NULL,
    symbol TEXT NOT NULL,
    side TEXT NOT NULL CHECK (side IN ('buy', 'sell')),
    entry_price NUMERIC(20, 8) NOT NULL,
    exit_price NUMERIC(20, 8),
    size NUMERIC(20, 8) NOT NULL,
    fees NUMERIC(20, 8) NOT NULL DEFAULT 0,
    pnl NUMERIC(20, 8),
    cost_basis NUMERIC(20, 8),
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'closed', 'cancelled')),
    opened_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    closed_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_paper_trades_user_id ON paper_trades(user_id);
CREATE INDEX IF NOT EXISTS idx_paper_trades_status ON paper_trades(status);
CREATE INDEX IF NOT EXISTS idx_paper_trades_opened_at ON paper_trades(opened_at DESC);
CREATE INDEX IF NOT EXISTS idx_paper_trades_closed_at ON paper_trades(closed_at DESC);
CREATE INDEX IF NOT EXISTS idx_paper_trades_quest_id ON paper_trades(quest_id);
CREATE INDEX IF NOT EXISTS idx_paper_trades_symbol ON paper_trades(symbol);
