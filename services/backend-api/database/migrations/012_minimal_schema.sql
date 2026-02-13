-- Migration: 006_minimal_schema.sql
-- Description: Complete schema setup - combines all fixes from 007-028
-- This replaces all migrations 007-028 with a single robust schema

BEGIN;

-- Add missing columns to exchanges table (safe to run multiple times)
ALTER TABLE exchanges 
ADD COLUMN IF NOT EXISTS display_name VARCHAR(100),
ADD COLUMN IF NOT EXISTS ccxt_id VARCHAR(50),
ADD COLUMN IF NOT EXISTS status VARCHAR(20) DEFAULT 'active',
ADD COLUMN IF NOT EXISTS has_spot BOOLEAN DEFAULT true,
ADD COLUMN IF NOT EXISTS has_futures BOOLEAN DEFAULT false,
ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW();

-- Update existing exchanges with proper values
UPDATE exchanges SET 
    display_name = CASE 
        WHEN name = 'binance' THEN 'Binance'
        WHEN name = 'coinbasepro' THEN 'Coinbase Pro'
        WHEN name = 'kraken' THEN 'Kraken'
        WHEN name = 'okx' THEN 'OKX'
        WHEN name = 'bybit' THEN 'Bybit'
        WHEN name = 'mexc' THEN 'MEXC'
        WHEN name = 'gateio' THEN 'Gate.io'
        WHEN name = 'kucoin' THEN 'KuCoin'
        ELSE INITCAP(name)
    END,
    ccxt_id = name,
    status = 'active',
    has_spot = true,
    has_futures = CASE 
        WHEN name IN ('binance', 'kraken', 'okx', 'bybit', 'mexc', 'gateio', 'kucoin') THEN true
        ELSE false
    END
WHERE display_name IS NULL OR ccxt_id IS NULL;

-- Make columns NOT NULL after updating
ALTER TABLE exchanges 
ALTER COLUMN display_name SET NOT NULL,
ALTER COLUMN ccxt_id SET NOT NULL;

-- Make api_url nullable (application doesn't always provide it)
ALTER TABLE exchanges ALTER COLUMN api_url DROP NOT NULL;

-- Make market_data price and volume nullable (application may not always have these values)
ALTER TABLE market_data ALTER COLUMN price DROP NOT NULL;
ALTER TABLE market_data ALTER COLUMN volume DROP NOT NULL;

-- Add unique constraint on ccxt_id (safe to run multiple times)
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint 
        WHERE conname = 'exchanges_ccxt_id_key'
    ) THEN
        ALTER TABLE exchanges ADD CONSTRAINT exchanges_ccxt_id_key UNIQUE (ccxt_id);
    END IF;
END $$;

-- Create trading_pairs table
CREATE TABLE IF NOT EXISTS trading_pairs (
    id SERIAL PRIMARY KEY,
    exchange_id INTEGER NOT NULL REFERENCES exchanges(id) ON DELETE CASCADE,
    symbol VARCHAR(50) NOT NULL,
    base_currency VARCHAR(10) NOT NULL,
    quote_currency VARCHAR(10) NOT NULL,
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE(exchange_id, symbol)
);

-- Create market_data table with all necessary columns
CREATE TABLE IF NOT EXISTS market_data (
    id SERIAL PRIMARY KEY,
    exchange_id INTEGER NOT NULL REFERENCES exchanges(id) ON DELETE CASCADE,
    trading_pair_id INTEGER NOT NULL REFERENCES trading_pairs(id) ON DELETE CASCADE,
    timestamp TIMESTAMP WITH TIME ZONE NOT NULL,
    bid DECIMAL(20,8),
    ask DECIMAL(20,8),
    price DECIMAL(20,8),
    volume DECIMAL(20,8),
    high DECIMAL(20,8),
    low DECIMAL(20,8),
    open DECIMAL(20,8),
    close DECIMAL(20,8),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW() NOT NULL,
    UNIQUE(exchange_id, trading_pair_id, timestamp)
);

-- Create funding_rates table
CREATE TABLE IF NOT EXISTS funding_rates (
    id SERIAL PRIMARY KEY,
    exchange_id INTEGER NOT NULL REFERENCES exchanges(id) ON DELETE CASCADE,
    trading_pair_id INTEGER NOT NULL REFERENCES trading_pairs(id) ON DELETE CASCADE,
    funding_rate DECIMAL(10,8),
    funding_time TIMESTAMP WITH TIME ZONE,
    next_funding_time TIMESTAMP WITH TIME ZONE,
    mark_price DECIMAL(20,8),
    index_price DECIMAL(20,8),
    timestamp TIMESTAMP WITH TIME ZONE NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE(exchange_id, trading_pair_id, funding_time)
);

-- Create arbitrage_opportunities table with is_active column
CREATE TABLE IF NOT EXISTS arbitrage_opportunities (
    id SERIAL PRIMARY KEY,
    buy_exchange_id INTEGER NOT NULL REFERENCES exchanges(id) ON DELETE CASCADE,
    sell_exchange_id INTEGER NOT NULL REFERENCES exchanges(id) ON DELETE CASCADE,
    symbol VARCHAR(50) NOT NULL,
    buy_price DECIMAL(20,8) NOT NULL,
    sell_price DECIMAL(20,8) NOT NULL,
    spread DECIMAL(10,6) NOT NULL,
    profit_percentage DECIMAL(10,6) NOT NULL,
    is_active BOOLEAN DEFAULT true,
    detected_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Create users table with telegram_user_id
CREATE TABLE IF NOT EXISTS users (
    id SERIAL PRIMARY KEY,
    username VARCHAR(100) UNIQUE,
    email VARCHAR(255) UNIQUE,
    telegram_user_id BIGINT UNIQUE,
    telegram_chat_id BIGINT,
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Create technical_indicators table with all required columns
CREATE TABLE IF NOT EXISTS technical_indicators (
    id SERIAL PRIMARY KEY,
    exchange_id INTEGER REFERENCES exchanges(id) ON DELETE CASCADE,
    trading_pair_id INTEGER REFERENCES trading_pairs(id) ON DELETE CASCADE,
    timeframe VARCHAR(10),
    indicator_type VARCHAR(50) NOT NULL,
    value DECIMAL(20,8),
    timestamp TIMESTAMP WITH TIME ZONE NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE(exchange_id, trading_pair_id, timeframe, indicator_type, timestamp)
);

-- Create or update schema_migrations tracking table
-- First create table if not exists, then add columns if they don't exist
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    filename VARCHAR(255),
    description TEXT,
    checksum VARCHAR(64),
    applied_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Add missing columns to existing schema_migrations table (from migrate.sh baseline)
ALTER TABLE schema_migrations ADD COLUMN IF NOT EXISTS checksum VARCHAR(64);
ALTER TABLE schema_migrations ADD COLUMN IF NOT EXISTS description TEXT;

-- Create indexes for better performance
CREATE INDEX IF NOT EXISTS idx_market_data_exchange_timestamp ON market_data(exchange_id, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_market_data_pair_timestamp ON market_data(trading_pair_id, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_market_data_timestamp ON market_data(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_funding_rates_exchange_pair_time ON funding_rates(exchange_id, trading_pair_id, funding_time DESC);
CREATE INDEX IF NOT EXISTS idx_funding_rates_next_funding_time ON funding_rates(next_funding_time DESC);
CREATE INDEX IF NOT EXISTS idx_arbitrage_opportunities_detected_at ON arbitrage_opportunities(detected_at DESC);
CREATE INDEX IF NOT EXISTS idx_arbitrage_opportunities_active ON arbitrage_opportunities(is_active, detected_at DESC);
-- Skip exchange_id index as trading_pairs table doesn't have exchange_id column
-- CREATE INDEX IF NOT EXISTS idx_trading_pairs_exchange ON trading_pairs(exchange_id);
-- Create index on telegram_chat_id instead of telegram_user_id
CREATE INDEX IF NOT EXISTS idx_users_telegram ON users(telegram_chat_id) WHERE telegram_chat_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_technical_indicators_lookup ON technical_indicators(exchange_id, trading_pair_id, timeframe, timestamp DESC);

-- Add updated_at triggers (conditional creation to avoid ownership issues)
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_proc 
        WHERE proname = 'update_updated_at_column'
    ) THEN
        CREATE FUNCTION update_updated_at_column()
        RETURNS TRIGGER AS $func$
        BEGIN
            NEW.updated_at = CURRENT_TIMESTAMP;
            RETURN NEW;
        END;
        $func$ LANGUAGE plpgsql;
    END IF;
END $$;

-- Apply updated_at triggers to all relevant tables
DROP TRIGGER IF EXISTS update_exchanges_updated_at ON exchanges;
CREATE TRIGGER update_exchanges_updated_at BEFORE UPDATE ON exchanges
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

DROP TRIGGER IF EXISTS update_trading_pairs_updated_at ON trading_pairs;
CREATE TRIGGER update_trading_pairs_updated_at BEFORE UPDATE ON trading_pairs
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

DROP TRIGGER IF EXISTS update_market_data_updated_at ON market_data;
CREATE TRIGGER update_market_data_updated_at BEFORE UPDATE ON market_data
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

DROP TRIGGER IF EXISTS update_funding_rates_updated_at ON funding_rates;
CREATE TRIGGER update_funding_rates_updated_at BEFORE UPDATE ON funding_rates
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

DROP TRIGGER IF EXISTS update_users_updated_at ON users;
CREATE TRIGGER update_users_updated_at BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Insert initial exchanges data with all required fields
-- Only insert if exchanges table is empty or missing these records
INSERT INTO exchanges (name, display_name, ccxt_id, has_spot, has_futures) 
SELECT * FROM (VALUES
    ('binance', 'Binance', 'binance', true, true),
    ('coinbasepro', 'Coinbase Pro', 'coinbasepro', true, false),
    ('kraken', 'Kraken', 'kraken', true, true),
    ('okx', 'OKX', 'okx', true, true),
    ('bybit', 'Bybit', 'bybit', true, true),
    ('mexc', 'MEXC', 'mexc', true, true),
    ('gateio', 'Gate.io', 'gateio', true, true),
    ('kucoin', 'KuCoin', 'kucoin', true, true)
) AS t(name, display_name, ccxt_id, has_spot, has_futures)
WHERE NOT EXISTS (SELECT 1 FROM exchanges WHERE name = t.name);

-- All migrations are already recorded in schema_migrations table
-- No need to insert duplicates

COMMIT;