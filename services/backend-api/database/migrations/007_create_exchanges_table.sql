-- Migration: 007_create_exchanges_table.sql
-- Description: Create exchanges table if it doesn't exist and ensure all required columns
-- This fixes the "relation exchanges does not exist" error

-- Create exchanges table if it doesn't exist
CREATE TABLE IF NOT EXISTS exchanges (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) UNIQUE NOT NULL,
    display_name VARCHAR(100) NOT NULL,
    ccxt_id VARCHAR(50) UNIQUE NOT NULL,
    api_url VARCHAR(255),
    status VARCHAR(20) DEFAULT 'active',
    has_spot BOOLEAN DEFAULT true,
    has_futures BOOLEAN DEFAULT false,
    is_active BOOLEAN DEFAULT true,
    last_ping TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Add missing columns if they don't exist (for existing tables)
DO $$
BEGIN
    -- Add display_name column if it doesn't exist
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns 
                   WHERE table_name = 'exchanges' AND column_name = 'display_name') THEN
        ALTER TABLE exchanges ADD COLUMN display_name VARCHAR(100);
    END IF;
    
    -- Add ccxt_id column if it doesn't exist
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns 
                   WHERE table_name = 'exchanges' AND column_name = 'ccxt_id') THEN
        ALTER TABLE exchanges ADD COLUMN ccxt_id VARCHAR(50);
    END IF;
    
    -- Add status column if it doesn't exist
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns 
                   WHERE table_name = 'exchanges' AND column_name = 'status') THEN
        ALTER TABLE exchanges ADD COLUMN status VARCHAR(20) DEFAULT 'active';
    END IF;
    
    -- Add has_spot column if it doesn't exist
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns 
                   WHERE table_name = 'exchanges' AND column_name = 'has_spot') THEN
        ALTER TABLE exchanges ADD COLUMN has_spot BOOLEAN DEFAULT true;
    END IF;
    
    -- Add has_futures column if it doesn't exist
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns 
                   WHERE table_name = 'exchanges' AND column_name = 'has_futures') THEN
        ALTER TABLE exchanges ADD COLUMN has_futures BOOLEAN DEFAULT false;
    END IF;
    
    -- Add updated_at column if it doesn't exist
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns 
                   WHERE table_name = 'exchanges' AND column_name = 'updated_at') THEN
        ALTER TABLE exchanges ADD COLUMN updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW();
    END IF;
END $$;

-- Make api_url nullable (application doesn't always provide it)
ALTER TABLE exchanges ALTER COLUMN api_url DROP NOT NULL;

-- Update existing exchanges with proper values if they exist
-- Only update rows where we can safely set values without violating unique constraints
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
    status = COALESCE(status, 'active'),
    has_spot = COALESCE(has_spot, true),
    has_futures = CASE 
        WHEN name IN ('binance', 'kraken', 'okx', 'bybit', 'mexc', 'gateio', 'kucoin') THEN true
        ELSE COALESCE(has_futures, false)
    END
WHERE display_name IS NULL AND ccxt_id IS NULL
AND name IN ('binance', 'coinbasepro', 'kraken', 'okx', 'bybit', 'mexc', 'gateio', 'kucoin')
AND (
    SELECT COUNT(*) FROM exchanges e2 
    WHERE e2.ccxt_id = exchanges.name
) <= 1;

-- Make required columns NOT NULL after updating
DO $$
BEGIN
    -- Make display_name NOT NULL if it has values
    IF EXISTS (SELECT 1 FROM exchanges WHERE display_name IS NOT NULL) THEN
        ALTER TABLE exchanges ALTER COLUMN display_name SET NOT NULL;
    END IF;
    
    -- Make ccxt_id NOT NULL if it has values
    IF EXISTS (SELECT 1 FROM exchanges WHERE ccxt_id IS NOT NULL) THEN
        ALTER TABLE exchanges ALTER COLUMN ccxt_id SET NOT NULL;
    END IF;
END $$;

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

-- Insert initial exchanges data with all required fields
-- Only insert if exchanges table is empty or missing these records
INSERT INTO exchanges (name, display_name, ccxt_id, has_spot, has_futures, status) 
SELECT * FROM (VALUES
    ('binance', 'Binance', 'binance', true, true, 'active'),
    ('coinbasepro', 'Coinbase Pro', 'coinbasepro', true, false, 'active'),
    ('kraken', 'Kraken', 'kraken', true, true, 'active'),
    ('okx', 'OKX', 'okx', true, true, 'active'),
    ('bybit', 'Bybit', 'bybit', true, true, 'active'),
    ('mexc', 'MEXC', 'mexc', true, true, 'active'),
    ('gateio', 'Gate.io', 'gateio', true, true, 'active'),
    ('kucoin', 'KuCoin', 'kucoin', true, true, 'active')
) AS new_exchanges(name, display_name, ccxt_id, has_spot, has_futures, status)
WHERE NOT EXISTS (
    SELECT 1 FROM exchanges WHERE exchanges.ccxt_id = new_exchanges.ccxt_id
);

-- Create indexes for better performance
CREATE INDEX IF NOT EXISTS idx_exchanges_ccxt_id ON exchanges(ccxt_id);
CREATE INDEX IF NOT EXISTS idx_exchanges_status ON exchanges(status);
CREATE INDEX IF NOT EXISTS idx_exchanges_active ON exchanges(is_active);

-- Add updated_at trigger (conditional creation)
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_proc WHERE proname = 'update_updated_at_column') THEN
        CREATE FUNCTION update_updated_at_column()
        RETURNS TRIGGER AS $func$
        BEGIN
            NEW.updated_at = CURRENT_TIMESTAMP;
            RETURN NEW;
        END;
        $func$ LANGUAGE plpgsql;
    END IF;
END $$;

DROP TRIGGER IF EXISTS update_exchanges_updated_at ON exchanges;
CREATE TRIGGER update_exchanges_updated_at BEFORE UPDATE ON exchanges
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();