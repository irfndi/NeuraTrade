-- Migration: 058_add_multi_leg_arbitrage.sql
-- Description: Adds tables to store multi-leg (triangular) arbitrage opportunities.

-- Multi-leg arbitrage opportunities
-- Uses gen_random_uuid() which is built-in to PostgreSQL 13+
CREATE TABLE IF NOT EXISTS multi_leg_opportunities (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    exchange_id INTEGER NOT NULL REFERENCES exchanges(id),
    total_profit DECIMAL(20, 8),
    profit_percentage DECIMAL(10, 6) NOT NULL,
    detected_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL
);

-- Individual legs of a multi-leg opportunity
CREATE TABLE IF NOT EXISTS multi_leg_legs (
    id BIGSERIAL PRIMARY KEY,
    opportunity_id UUID NOT NULL REFERENCES multi_leg_opportunities(id) ON DELETE CASCADE,
    leg_index INTEGER NOT NULL, -- 0, 1, 2...
    symbol VARCHAR(100) NOT NULL, -- Matches trading_pairs.symbol column size
    side VARCHAR(10) NOT NULL, -- 'buy' or 'sell'
    price DECIMAL(20, 8) NOT NULL,
    volume DECIMAL(20, 8),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Index for performance
CREATE INDEX IF NOT EXISTS idx_multi_leg_opp_expires ON multi_leg_opportunities(expires_at);
CREATE INDEX IF NOT EXISTS idx_multi_leg_legs_opportunity ON multi_leg_legs(opportunity_id);
