-- Migration 054: Fix schema issues
-- Description:
--   1. Add missing 'priority' column to exchanges table
--   2. Increase column sizes for trading_pairs (symbol, base_currency, quote_currency)
-- This fixes:
--   - "column e.priority does not exist" error
--   - "value too long for type character varying(10)" error
--   - "value too long for type character varying(20)" error

-- =====================================================
-- Fix 1: Add priority column to exchanges table
-- =====================================================

-- Add priority column if it doesn't exist
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'exchanges' AND column_name = 'priority'
    ) THEN
        ALTER TABLE exchanges ADD COLUMN priority INTEGER DEFAULT 100;

        -- Set initial priorities for common exchanges
        UPDATE exchanges SET priority = 1 WHERE LOWER(name) = 'binance' OR LOWER(ccxt_id) = 'binance';
        UPDATE exchanges SET priority = 2 WHERE LOWER(name) = 'bybit' OR LOWER(ccxt_id) = 'bybit';
        UPDATE exchanges SET priority = 3 WHERE LOWER(name) = 'okx' OR LOWER(ccxt_id) = 'okx';
        UPDATE exchanges SET priority = 4 WHERE LOWER(name) = 'kucoin' OR LOWER(ccxt_id) = 'kucoin';
        UPDATE exchanges SET priority = 5 WHERE LOWER(name) = 'kraken' OR LOWER(ccxt_id) = 'kraken';
        UPDATE exchanges SET priority = 6 WHERE LOWER(name) = 'coinbase' OR LOWER(ccxt_id) = 'coinbase' OR LOWER(ccxt_id) = 'coinbasepro';
        UPDATE exchanges SET priority = 7 WHERE LOWER(name) = 'gate' OR LOWER(ccxt_id) = 'gateio';
        UPDATE exchanges SET priority = 8 WHERE LOWER(name) = 'mexc' OR LOWER(ccxt_id) = 'mexc';
        UPDATE exchanges SET priority = 9 WHERE LOWER(name) = 'bitget' OR LOWER(ccxt_id) = 'bitget';
        UPDATE exchanges SET priority = 10 WHERE LOWER(name) = 'htx' OR LOWER(ccxt_id) = 'htx' OR LOWER(ccxt_id) = 'huobi';

        -- Create index for priority ordering
        CREATE INDEX IF NOT EXISTS idx_exchanges_priority ON exchanges(priority);

        RAISE NOTICE 'Added priority column to exchanges table';
    ELSE
        RAISE NOTICE 'Priority column already exists in exchanges table';
    END IF;
END $$;

-- =====================================================
-- Fix 2: Increase column sizes in trading_pairs table
-- =====================================================

DO $$
DECLARE
    symbol_size INTEGER;
    base_size INTEGER;
    quote_size INTEGER;
BEGIN
    SELECT character_maximum_length INTO symbol_size
    FROM information_schema.columns
    WHERE table_name = 'trading_pairs' AND column_name = 'symbol';

    SELECT character_maximum_length INTO base_size
    FROM information_schema.columns
    WHERE table_name = 'trading_pairs' AND column_name = 'base_currency';

    SELECT character_maximum_length INTO quote_size
    FROM information_schema.columns
    WHERE table_name = 'trading_pairs' AND column_name = 'quote_currency';

    RAISE NOTICE 'Current column sizes: symbol=%, base_currency=%, quote_currency=%',
        symbol_size, base_size, quote_size;

    IF symbol_size < 100 OR base_size < 50 OR quote_size < 50 THEN
        DROP VIEW IF EXISTS v_trading_pairs_debug CASCADE;
        DROP VIEW IF EXISTS v_active_trading_pairs CASCADE;
        DROP VIEW IF EXISTS active_exchange_trading_pairs CASCADE;
        DROP VIEW IF EXISTS blacklisted_exchange_trading_pairs CASCADE;
    END IF;

    IF symbol_size < 100 THEN
        ALTER TABLE trading_pairs ALTER COLUMN symbol TYPE VARCHAR(100);
        RAISE NOTICE 'Updated symbol column to VARCHAR(100)';
    END IF;

    IF base_size < 50 THEN
        ALTER TABLE trading_pairs ALTER COLUMN base_currency TYPE VARCHAR(50);
        RAISE NOTICE 'Updated base_currency column to VARCHAR(50)';
    END IF;

    IF quote_size < 50 THEN
        ALTER TABLE trading_pairs ALTER COLUMN quote_currency TYPE VARCHAR(50);
        RAISE NOTICE 'Updated quote_currency column to VARCHAR(50)';
    END IF;
END $$;

-- Also update exchange_trading_pairs table if it exists
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'exchange_trading_pairs') THEN
        -- Update symbol column
        IF EXISTS (SELECT 1 FROM information_schema.columns
                   WHERE table_name = 'exchange_trading_pairs' AND column_name = 'symbol') THEN
            ALTER TABLE exchange_trading_pairs ALTER COLUMN symbol TYPE VARCHAR(100);
        END IF;

        -- Update base_currency column
        IF EXISTS (SELECT 1 FROM information_schema.columns
                   WHERE table_name = 'exchange_trading_pairs' AND column_name = 'base_currency') THEN
            ALTER TABLE exchange_trading_pairs ALTER COLUMN base_currency TYPE VARCHAR(50);
        END IF;

        -- Update quote_currency column
        IF EXISTS (SELECT 1 FROM information_schema.columns
                   WHERE table_name = 'exchange_trading_pairs' AND column_name = 'quote_currency') THEN
            ALTER TABLE exchange_trading_pairs ALTER COLUMN quote_currency TYPE VARCHAR(50);
        END IF;

        RAISE NOTICE 'Updated exchange_trading_pairs table column sizes';
    END IF;
END $$;

-- Also update market_data table symbol column if it exists
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_name = 'market_data' AND column_name = 'symbol') THEN
        ALTER TABLE market_data ALTER COLUMN symbol TYPE VARCHAR(100);
        RAISE NOTICE 'Updated market_data.symbol column to VARCHAR(100)';
    END IF;
END $$;

-- Also update funding_rates table symbol column if it exists
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_name = 'funding_rates' AND column_name = 'symbol') THEN
        ALTER TABLE funding_rates ALTER COLUMN symbol TYPE VARCHAR(100);
        RAISE NOTICE 'Updated funding_rates.symbol column to VARCHAR(100)';
    END IF;
END $$;

-- Also update market_data table symbol column if it exists
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_name = 'market_data' AND column_name = 'symbol') THEN
        ALTER TABLE market_data ALTER COLUMN symbol TYPE VARCHAR(100);
        RAISE NOTICE 'Updated market_data.symbol column to VARCHAR(100)';
    END IF;
END $$;

-- Also update funding_rates table symbol column if it exists
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_name = 'funding_rates' AND column_name = 'symbol') THEN
        ALTER TABLE funding_rates ALTER COLUMN symbol TYPE VARCHAR(100);
        RAISE NOTICE 'Updated funding_rates.symbol column to VARCHAR(100)';
    END IF;
END $$;

-- =====================================================
-- Record migration
-- =====================================================

INSERT INTO schema_migrations (filename, applied, applied_at)
VALUES ('054_fix_schema_issues.sql', true, NOW())
ON CONFLICT (filename) DO UPDATE SET
    applied = true,
    applied_at = NOW();
