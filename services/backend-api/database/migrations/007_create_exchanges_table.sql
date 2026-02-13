-- Migration: 007_create_exchanges_table.sql
-- Description: Ensure exchanges table has all required columns and data
-- This migration is idempotent - handles both fresh installs and upgrades

-- Create exchanges table if it doesn't exist
CREATE TABLE IF NOT EXISTS exchanges (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) UNIQUE NOT NULL,
    display_name VARCHAR(100),
    ccxt_id VARCHAR(50),
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

-- Make api_url nullable
ALTER TABLE exchanges ALTER COLUMN api_url DROP NOT NULL;

-- Fix duplicate ccxt_id values FIRST before any other operations
-- Handle any duplicates that may exist from previous migrations
WITH duplicates AS (
    SELECT id, ccxt_id, 
           ROW_NUMBER() OVER (PARTITION BY ccxt_id ORDER BY id) as rn
    FROM exchanges
    WHERE ccxt_id IS NOT NULL
)
UPDATE exchanges 
SET ccxt_id = duplicates.ccxt_id || '_dup' || (duplicates.rn - 1)
FROM duplicates
WHERE exchanges.id = duplicates.id AND duplicates.rn > 1;

-- Update display_name for rows where it's NULL
UPDATE exchanges 
SET display_name = CASE 
    WHEN name IN ('binance', 'binance_us') THEN 'Binance'
    WHEN name = 'coinbasepro' THEN 'Coinbase Pro'
    WHEN name = 'kraken' THEN 'Kraken'
    WHEN name = 'okx' THEN 'OKX'
    WHEN name = 'bybit' THEN 'Bybit'
    WHEN name = 'mexc' THEN 'MEXC'
    WHEN name = 'gateio' THEN 'Gate.io'
    WHEN name = 'kucoin' THEN 'KuCoin'
    ELSE INITCAP(name)
END
WHERE display_name IS NULL AND name IS NOT NULL;

-- Update ccxt_id for rows where it's NULL - use LOWER(name) to avoid duplicates
UPDATE exchanges 
SET ccxt_id = LOWER(name)
WHERE ccxt_id IS NULL AND name IS NOT NULL;

-- Fix any remaining duplicate ccxt_id after the updates
WITH duplicates2 AS (
    SELECT id, ccxt_id, 
           ROW_NUMBER() OVER (PARTITION BY ccxt_id ORDER BY id) as rn
    FROM exchanges
    WHERE ccxt_id IS NOT NULL
)
UPDATE exchanges 
SET ccxt_id = duplicates2.ccxt_id || '_dup' || (duplicates2.rn - 1)
FROM duplicates2
WHERE exchanges.id = duplicates2.id AND duplicates2.rn > 1;

-- Make columns NOT NULL after ensuring values exist
DO $$
BEGIN
    -- Make display_name NOT NULL if column has values
    IF EXISTS (SELECT 1 FROM exchanges WHERE display_name IS NOT NULL LIMIT 1) THEN
        ALTER TABLE exchanges ALTER COLUMN display_name SET NOT NULL;
    END IF;
    
    -- Make ccxt_id NOT NULL if column has values
    IF EXISTS (SELECT 1 FROM exchanges WHERE ccxt_id IS NOT NULL LIMIT 1) THEN
        ALTER TABLE exchanges ALTER COLUMN ccxt_id SET NOT NULL;
    END IF;
END $$;

-- Add unique constraint on ccxt_id if it doesn't exist
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint 
        WHERE conrelid = 'exchanges'::regclass
        AND contype = 'u'
        AND conname LIKE '%ccxt_id%'
    ) THEN
        ALTER TABLE exchanges ADD CONSTRAINT exchanges_ccxt_id_key UNIQUE (ccxt_id);
    END IF;
END $$;

-- Insert initial exchanges data with all required fields
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