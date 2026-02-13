-- Create ai_reasoning_summaries table for storing AI decision reasoning
-- Created: 2026-02-13

CREATE TABLE IF NOT EXISTS ai_reasoning_summaries (
    id TEXT PRIMARY KEY,
    user_id UUID NOT NULL,
    quest_id BIGINT,
    trade_id BIGINT,
    session_id TEXT,
    category TEXT NOT NULL,
    decision TEXT NOT NULL,
    reasoning TEXT NOT NULL,
    confidence NUMERIC(5, 4) NOT NULL DEFAULT 0,
    factors TEXT[], -- JSON array stored as text array
    market_context TEXT,
    risk_level TEXT,
    model_used TEXT,
    tokens_used INTEGER DEFAULT 0,
    latency_ms INTEGER DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ai_reasoning_user_id ON ai_reasoning_summaries(user_id);
CREATE INDEX IF NOT EXISTS idx_ai_reasoning_quest_id ON ai_reasoning_summaries(quest_id);
CREATE INDEX IF NOT EXISTS idx_ai_reasoning_trade_id ON ai_reasoning_summaries(trade_id);
CREATE INDEX IF NOT EXISTS idx_ai_reasoning_session_id ON ai_reasoning_summaries(session_id);
CREATE INDEX IF NOT EXISTS idx_ai_reasoning_category ON ai_reasoning_summaries(category);
CREATE INDEX IF NOT EXISTS idx_ai_reasoning_created_at ON ai_reasoning_summaries(created_at DESC);
