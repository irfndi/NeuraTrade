-- Migration 057: Add exchange-level fee fallback
-- Description: Creates a table to store default fees per exchange
-- Version: 057
-- Date: 2025-12-23

BEGIN;

CREATE TABLE IF NOT EXISTS exchange_fees (
    id SERIAL PRIMARY KEY,
    exchange_id INTEGER NOT NULL REFERENCES exchanges(id) ON DELETE CASCADE,
    taker_fee DECIMAL(6, 4) NOT NULL DEFAULT 0.001,
    maker_fee DECIMAL(6, 4) NOT NULL DEFAULT 0.001,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE(exchange_id)
);

-- Add updated_at trigger
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = 'update_exchange_fees_updated_at') THEN
        CREATE TRIGGER update_exchange_fees_updated_at
            BEFORE UPDATE ON exchange_fees
            FOR EACH ROW
            EXECUTE FUNCTION update_updated_at_column();
    END IF;
END $$;

-- Populate with some defaults based on known exchanges
INSERT INTO exchange_fees (exchange_id, taker_fee, maker_fee)
SELECT id, 0.001, 0.001 FROM exchanges
ON CONFLICT (exchange_id) DO NOTHING;

-- Specific overrides for common exchanges (using ccxt_id for consistency)
UPDATE exchange_fees
SET taker_fee = 0.0006, maker_fee = 0.0002
WHERE exchange_id IN (SELECT id FROM exchanges WHERE LOWER(ccxt_id) = 'binance' OR LOWER(name) = 'binance');

UPDATE exchange_fees
SET taker_fee = 0.0005, maker_fee = 0.0002
WHERE exchange_id IN (SELECT id FROM exchanges WHERE LOWER(ccxt_id) = 'bybit' OR LOWER(name) = 'bybit');

COMMIT;
