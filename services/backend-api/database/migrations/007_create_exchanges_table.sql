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
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns 
                   WHERE table_name = 'exchanges' AND column_name = 'display_name') THEN
        ALTER TABLE exchanges ADD COLUMN display_name VARCHAR(100);
    END IF;
    
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns 
                   WHERE table_name = 'exchanges' AND column_name = 'ccxt_id') THEN
        ALTER TABLE exchanges ADD COLUMN ccxt_id VARCHAR(50);
    END IF;
    
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns 
                   WHERE table_name = 'exchanges' AND column_name = 'status') THEN
        ALTER TABLE exchanges ADD COLUMN status VARCHAR(20) DEFAULT 'active';
    END IF;
    
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns 
                   WHERE table_name = 'exchanges' AND column_name = 'has_spot') THEN
        ALTER TABLE exchanges ADD COLUMN has_spot BOOLEAN DEFAULT true;
    END IF;
    
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns 
                   WHERE table_name = 'exchanges' AND column_name = 'has_futures') THEN
        ALTER TABLE exchanges ADD COLUMN has_futures BOOLEAN DEFAULT false;
    END IF;
    
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns 
                   WHERE table_name = 'exchanges' AND column_name = 'updated_at') THEN
        ALTER TABLE exchanges ADD COLUMN updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW();
    END IF;
END $$;

-- Make api_url nullable
ALTER TABLE exchanges ALTER COLUMN api_url DROP NOT NULL;

-- Update display_name where NULL - ignore if constraint prevents
DO $$
BEGIN
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
EXCEPTION WHEN unique_violation THEN
    RAISE NOTICE 'Skipping display_name update due to constraint conflict';
END $$;

-- Update ccxt_id where NULL - ignore if constraint prevents
DO $$
BEGIN
    UPDATE exchanges 
    SET ccxt_id = LOWER(name)
    WHERE ccxt_id IS NULL AND name IS NOT NULL;
EXCEPTION WHEN unique_violation THEN
    RAISE NOTICE 'Skipping ccxt_id update due to constraint conflict';
END $$;

-- Make columns NOT NULL after ensuring values exist
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM exchanges WHERE display_name IS NOT NULL LIMIT 1) THEN
        ALTER TABLE exchanges ALTER COLUMN display_name SET NOT NULL;
    END IF;
    
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

-- Insert initial exchanges data if not exists
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