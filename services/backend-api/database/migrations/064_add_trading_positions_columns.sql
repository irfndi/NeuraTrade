-- Migration: 064_add_trading_positions_columns.sql
-- Description: Add missing columns to trading_positions table for controlled liquidation
-- Created: 2026-02-13

BEGIN;

-- Add missing columns to trading_positions table
-- These columns are needed by the controlled_liquidation service

ALTER TABLE trading_positions ADD COLUMN IF NOT EXISTS mark_price NUMERIC;
ALTER TABLE trading_positions ADD COLUMN IF NOT EXISTS unrealized_pnl NUMERIC;
ALTER TABLE trading_positions ADD COLUMN IF NOT EXISTS leverage NUMERIC DEFAULT 1;
ALTER TABLE trading_positions ADD COLUMN IF NOT EXISTS margin NUMERIC;
ALTER TABLE trading_positions ADD COLUMN IF NOT EXISTS liquidation_price NUMERIC;

-- Create index for liquidation queries
CREATE INDEX IF NOT EXISTS idx_trading_positions_liquidation 
    ON trading_positions(symbol, exchange, status) 
    WHERE status = 'OPEN';

COMMIT;
