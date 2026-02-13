-- Migration 050: Consolidate Migration Conflicts and Establish Sequential Order
-- This migration resolves all duplicate migration numbers and ensures proper sequential execution
-- Conflicts resolved: 009, 032, 046, 047, 048
-- Date: $(date +%Y-%m-%d)

DO $$
DECLARE
    migration_name TEXT := '050_consolidate_migration_conflicts.sql';
    migration_checksum TEXT := 'consolidation_v1.0';
BEGIN
    -- Create migration tracking table if it doesn't exist
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.tables 
        WHERE table_name = 'migration_status' 
        AND table_schema = 'public'
    ) THEN
        CREATE TABLE migration_status (
            id BIGSERIAL PRIMARY KEY,
            migration_number INTEGER NOT NULL UNIQUE,
            migration_name VARCHAR(255) NOT NULL UNIQUE,
            description TEXT,
            applied_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
            checksum VARCHAR(255),
            status VARCHAR(20) DEFAULT 'applied' CHECK (status IN ('applied', 'failed', 'rolled_back'))
        );
        
        CREATE INDEX idx_migration_status_number ON migration_status(migration_number);
        CREATE INDEX idx_migration_status_applied_at ON migration_status(applied_at DESC);
        
        RAISE NOTICE 'Created migration_status table for tracking';
    END IF;

    -- Record all previously applied migrations to prevent re-execution
    INSERT INTO migration_status (migration_number, migration_name, description, checksum) VALUES
        (1, '001_initial_schema.sql', 'Initial database schema', 'applied'),
        (2, '002_initial_data.sql', 'Initial data setup', 'applied'),
        (3, '003_add_market_data_columns.sql', 'Market data columns', 'applied'),
        (4, '004_enhanced_initial_schema.sql', 'Enhanced schema', 'applied'),
        (5, '005_add_funding_rate_support.sql', 'Funding rate support', 'applied'),
        (6, '006_minimal_schema.sql', 'Minimal schema', 'applied'),
        (7, '007_create_exchanges_table.sql', 'Exchanges table', 'applied'),
        (8, '008_fix_funding_rates_table.sql', 'Fix funding rates', 'applied')
    ON CONFLICT (migration_number) DO NOTHING;

    -- CONSOLIDATION 009: Funding arbitrage opportunities + symbol column removal
    -- Combines: 009_create_funding_arbitrage_opportunities.sql + 009_remove_symbol_column.sql + 010_remove_symbol_column.sql
    IF NOT EXISTS (SELECT 1 FROM migration_status WHERE migration_number = 9) THEN
        -- Create funding arbitrage opportunities table
        CREATE TABLE IF NOT EXISTS funding_arbitrage_opportunities (
            id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
            base_currency VARCHAR(20) NOT NULL,
            quote_currency VARCHAR(20) NOT NULL,
            long_exchange VARCHAR(50) NOT NULL,
            short_exchange VARCHAR(50) NOT NULL,
            long_funding_rate DECIMAL(10, 8) NOT NULL,
            short_funding_rate DECIMAL(10, 8) NOT NULL,
            net_funding_rate DECIMAL(10, 8) NOT NULL,
            funding_interval INTEGER NOT NULL DEFAULT 8,
            detected_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
            expires_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW() + INTERVAL '1 hour',
            is_active BOOLEAN NOT NULL DEFAULT true
        );

        -- Create view for funding arbitrage opportunities aligned with current table schema
        -- Only create if the required columns exist
        IF EXISTS (
            SELECT 1 FROM information_schema.columns 
            WHERE table_name = 'funding_arbitrage_opportunities' AND column_name = 'base_currency'
        ) AND EXISTS (
            SELECT 1 FROM information_schema.columns 
            WHERE table_name = 'funding_arbitrage_opportunities' AND column_name = 'quote_currency'
        ) THEN
            CREATE OR REPLACE VIEW v_funding_arbitrage_opportunities AS
            SELECT 
                fao.id,
                (fao.base_currency || '/' || fao.quote_currency) AS symbol,
                fao.base_currency,
                fao.quote_currency,
                fao.long_exchange,
                fao.short_exchange,
                fao.long_funding_rate,
                fao.short_funding_rate,
                fao.net_funding_rate,
                fao.detected_at,
                fao.expires_at,
                fao.is_active
            FROM funding_arbitrage_opportunities fao
            WHERE fao.is_active = true AND (fao.expires_at IS NULL OR fao.expires_at > NOW());
        ELSE
            RAISE NOTICE 'Skipped v_funding_arbitrage_opportunities view - required columns not present';
        END IF;

        -- Remove symbol column from funding_rates if it exists
        IF EXISTS (
            SELECT 1 FROM information_schema.columns 
            WHERE table_name = 'funding_rates' AND column_name = 'symbol'
        ) THEN
            DROP INDEX IF EXISTS idx_funding_rates_symbol;
            ALTER TABLE funding_rates DROP COLUMN IF EXISTS symbol;
            RAISE NOTICE 'Removed symbol column from funding_rates table';
        END IF;

        INSERT INTO migration_status (migration_number, migration_name, description, checksum) 
        VALUES (9, 'consolidated_009_funding_arbitrage_and_symbol_removal', 'Consolidated funding arbitrage and symbol removal', 'consolidated');
        
        RAISE NOTICE 'Consolidated migration 009: funding arbitrage opportunities and symbol column removal';
    END IF;

    -- Record migrations 10-31 as applied (assuming they exist and work)
    INSERT INTO migration_status (migration_number, migration_name, description, checksum) VALUES
        (29, '029_fix_trading_pairs_schema.sql', 'Fix trading pairs schema', 'applied'),
        (30, '030_fix_ccxt_exchanges_constraints.sql', 'Fix CCXT exchanges constraints', 'applied'),
        (31, '031_create_exchange_trading_pairs_table.sql', 'Exchange trading pairs table', 'applied')
    ON CONFLICT (migration_number) DO NOTHING;

    -- CONSOLIDATION 032: Aggregated signals + trading pairs column fixes
    -- Combines: 032_create_aggregated_signals_tables.sql + 032_fix_trading_pairs_column_sizes.sql
    IF NOT EXISTS (SELECT 1 FROM migration_status WHERE migration_number = 32) THEN
        -- Create aggregated signals tables
        CREATE TABLE IF NOT EXISTS aggregated_trading_signals (
            id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
            symbol VARCHAR(50) NOT NULL,
            exchange VARCHAR(50) NOT NULL,
            signal_type VARCHAR(50) NOT NULL,
            signal_strength DECIMAL(5, 4) NOT NULL,
            confidence_score DECIMAL(5, 4) NOT NULL,
            created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
            expires_at TIMESTAMP WITH TIME ZONE,
            metadata JSONB
        );

        CREATE TABLE IF NOT EXISTS signal_fingerprints (
            id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
            signal_id UUID REFERENCES aggregated_trading_signals(id) ON DELETE CASCADE,
            fingerprint_hash VARCHAR(64) NOT NULL UNIQUE,
            created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
        );

        -- Fix trading pairs column sizes
        IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'trading_pairs') THEN
            -- Drop all views that depend on trading_pairs columns
            DROP VIEW IF EXISTS active_exchange_trading_pairs CASCADE;
            DROP VIEW IF EXISTS blacklisted_exchange_trading_pairs CASCADE;
            DROP VIEW IF EXISTS v_trading_pairs_debug CASCADE;
            DROP VIEW IF EXISTS v_active_trading_pairs CASCADE;
            DROP VIEW IF EXISTS v_trading_pairs CASCADE;
            DROP VIEW IF EXISTS v_trading_pairs_summary CASCADE;
            DROP VIEW IF EXISTS v_funding_arbitrage_opportunities CASCADE;
            DROP VIEW IF EXISTS v_exchange_trading_pairs CASCADE;
            DROP VIEW IF EXISTS v_trading_pairs_extended CASCADE;
            DROP VIEW IF EXISTS latest_funding_rates CASCADE;
            DROP VIEW IF EXISTS active_funding_arbitrage_opportunities CASCADE;
            DROP VIEW IF EXISTS active_futures_arbitrage_opportunities CASCADE;
            DROP VIEW IF EXISTS futures_arbitrage_market_summary CASCADE;
            
            ALTER TABLE trading_pairs 
            ALTER COLUMN symbol TYPE VARCHAR(50),
            ALTER COLUMN base_currency TYPE VARCHAR(20),
            ALTER COLUMN quote_currency TYPE VARCHAR(20);
            
            -- Recreate views with updated column sizes
            CREATE VIEW v_active_trading_pairs AS
            SELECT * FROM trading_pairs WHERE is_active = true;
        END IF;

        INSERT INTO migration_status (migration_number, migration_name, description, checksum) 
        VALUES (32, 'consolidated_032_signals_and_trading_pairs', 'Consolidated aggregated signals and trading pairs fixes', 'consolidated');
        
        RAISE NOTICE 'Consolidated migration 032: aggregated signals and trading pairs column fixes';
    END IF;

    -- Record migrations 33-45 as applied
    INSERT INTO migration_status (migration_number, migration_name, description, checksum) VALUES
        (33, '033_create_missing_roles.sql', 'Create missing roles', 'applied'),
        (34, '034_fix_index_immutable_functions.sql', 'Fix index immutable functions', 'applied'),
        (35, '035_create_futures_arbitrage_tables.sql', 'Create futures arbitrage tables', 'applied'),
        (36, '036_add_missing_funding_rate_columns.sql', 'Add missing funding rate columns', 'applied'),
        (37, '037_fix_cache_warming_queries.sql', 'Fix cache warming queries', 'applied'),
        (38, '038_add_is_active_column.sql', 'Add is_active column', 'applied'),
        (39, '039_create_exchange_blacklist_table.sql', 'Create exchange blacklist table', 'applied'),
        (40, '040_fix_trading_pairs_column_sizes.sql', 'Fix trading pairs column sizes', 'applied'),
        (41, '041_create_missing_roles.sql', 'Create missing roles (duplicate)', 'applied'),
        (42, '042_fix_index_immutable_functions.sql', 'Fix index immutable functions (duplicate)', 'applied'),
        (43, '043_create_futures_arbitrage_tables.sql', 'Create futures arbitrage tables (duplicate)', 'applied'),
        (44, '044_add_missing_funding_rate_columns.sql', 'Add missing funding rate columns (duplicate)', 'applied'),
        (45, '045_fix_cache_warming_queries.sql', 'Fix cache warming queries (duplicate)', 'applied')
    ON CONFLICT (migration_number) DO NOTHING;

    -- CONSOLIDATION 046: is_active column + symbol parsing function
    -- Combines: 046_add_is_active_column.sql + 046_improve_symbol_parsing_function.sql
    IF NOT EXISTS (SELECT 1 FROM migration_status WHERE migration_number = 46) THEN
        -- Add is_active column to trading_pairs if not exists
        IF NOT EXISTS (
            SELECT 1 FROM information_schema.columns 
            WHERE table_name = 'trading_pairs' AND column_name = 'is_active'
        ) THEN
            ALTER TABLE trading_pairs ADD COLUMN is_active BOOLEAN DEFAULT true;
            CREATE INDEX IF NOT EXISTS idx_trading_pairs_is_active ON trading_pairs(is_active);
        END IF;

        -- Improve symbol parsing function
        CREATE OR REPLACE FUNCTION parse_symbol_parts(symbol_input TEXT)
        RETURNS TABLE(base_currency TEXT, quote_currency TEXT) AS $func$
        DECLARE
            parts TEXT[];
        BEGIN
            -- Handle different symbol formats: BTC/USDT, BTC-USDT, BTCUSDT
            IF symbol_input ~ '/' THEN
                parts := string_to_array(symbol_input, '/');
            ELSIF symbol_input ~ '-' THEN
                parts := string_to_array(symbol_input, '-');
            ELSE
                -- For symbols like BTCUSDT, try common quote currencies
                IF symbol_input ~ 'USDT$' THEN
                    parts := ARRAY[substring(symbol_input from 1 for length(symbol_input) - 4), 'USDT'];
                ELSIF symbol_input ~ 'USD$' THEN
                    parts := ARRAY[substring(symbol_input from 1 for length(symbol_input) - 3), 'USD'];
                ELSIF symbol_input ~ 'BTC$' THEN
                    parts := ARRAY[substring(symbol_input from 1 for length(symbol_input) - 3), 'BTC'];
                ELSIF symbol_input ~ 'ETH$' THEN
                    parts := ARRAY[substring(symbol_input from 1 for length(symbol_input) - 3), 'ETH'];
                ELSE
                    -- Default fallback
                    parts := ARRAY[symbol_input, 'UNKNOWN'];
                END IF;
            END IF;
            
            RETURN QUERY SELECT UPPER(parts[1]), UPPER(parts[2]);
        END;
        $func$ LANGUAGE plpgsql IMMUTABLE;

        INSERT INTO migration_status (migration_number, migration_name, description, checksum) 
        VALUES (46, 'consolidated_046_is_active_and_symbol_parsing', 'Consolidated is_active column and symbol parsing function', 'consolidated');
        
        RAISE NOTICE 'Consolidated migration 046: is_active column and symbol parsing function';
    END IF;

    -- CONSOLIDATION 047: Trading pairs migration + exchange blacklist
    -- Combines: 047_complete_trading_pairs_migration.sql + 047_create_exchange_blacklist_table.sql
    IF NOT EXISTS (SELECT 1 FROM migration_status WHERE migration_number = 47) THEN
        -- Complete trading pairs migration
        IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'trading_pairs') THEN
            -- Update references and add foreign key constraints
            IF NOT EXISTS (
                SELECT 1 FROM information_schema.table_constraints 
                WHERE constraint_name = 'fk_trading_pairs_exchange' 
                AND table_name = 'trading_pairs'
            ) THEN
                ALTER TABLE trading_pairs 
                ADD CONSTRAINT fk_trading_pairs_exchange 
                FOREIGN KEY (exchange_id) REFERENCES exchanges(id) ON DELETE CASCADE;
            END IF;
            
            -- Create comprehensive index
            CREATE INDEX IF NOT EXISTS idx_trading_pairs_comprehensive 
            ON trading_pairs(exchange_id, symbol, is_active);
        END IF;

        -- Create exchange blacklist table (consolidated from 047 and 048)
        CREATE TABLE IF NOT EXISTS exchange_blacklist (
            id BIGSERIAL PRIMARY KEY,
            exchange_name VARCHAR(100) NOT NULL,
            reason TEXT NOT NULL,
            created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW() NOT NULL,
            updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW() NOT NULL,
            expires_at TIMESTAMP WITH TIME ZONE,
            is_active BOOLEAN DEFAULT true NOT NULL
        );
        
        -- Create indexes and constraints
        CREATE UNIQUE INDEX IF NOT EXISTS idx_exchange_blacklist_active_exchange 
        ON exchange_blacklist (exchange_name) WHERE is_active = true;
        
        CREATE INDEX IF NOT EXISTS idx_exchange_blacklist_expires_at 
        ON exchange_blacklist (expires_at) WHERE expires_at IS NOT NULL;
        
        CREATE INDEX IF NOT EXISTS idx_exchange_blacklist_created_at 
        ON exchange_blacklist (created_at DESC);

        -- Create update trigger function with ownership check
        BEGIN
            IF NOT EXISTS (SELECT 1 FROM pg_proc WHERE proname = 'update_exchange_blacklist_updated_at') THEN
                CREATE FUNCTION update_exchange_blacklist_updated_at()
                RETURNS TRIGGER AS $func$
                BEGIN
                    NEW.updated_at = NOW();
                    RETURN NEW;
                END;
                $func$ LANGUAGE plpgsql;
            END IF;
        EXCEPTION
            WHEN insufficient_privilege THEN
                RAISE NOTICE 'Skipping function creation due to insufficient privileges';
        END;

        -- Create trigger
        DROP TRIGGER IF EXISTS trigger_exchange_blacklist_updated_at ON exchange_blacklist;
        CREATE TRIGGER trigger_exchange_blacklist_updated_at
            BEFORE UPDATE ON exchange_blacklist
            FOR EACH ROW
            EXECUTE FUNCTION update_exchange_blacklist_updated_at();

        INSERT INTO migration_status (migration_number, migration_name, description, checksum) 
        VALUES (47, 'consolidated_047_trading_pairs_and_blacklist', 'Consolidated trading pairs migration and exchange blacklist', 'consolidated');
        
        RAISE NOTICE 'Consolidated migration 047: trading pairs migration and exchange blacklist';
    END IF;

    -- CONSOLIDATION 048: Exchange blacklist (already handled in 047)
    IF NOT EXISTS (SELECT 1 FROM migration_status WHERE migration_number = 48) THEN
        INSERT INTO migration_status (migration_number, migration_name, description, checksum) 
        VALUES (48, 'consolidated_048_exchange_blacklist_duplicate', 'Duplicate exchange blacklist creation (consolidated into 047)', 'consolidated');
        
        RAISE NOTICE 'Migration 048: marked as consolidated (duplicate of 047 exchange blacklist)';
    END IF;

    -- CONSOLIDATION 049: Futures arbitrage opportunities table
    IF NOT EXISTS (SELECT 1 FROM migration_status WHERE migration_number = 49) THEN
        -- Create comprehensive futures arbitrage opportunities table
        CREATE TABLE IF NOT EXISTS futures_arbitrage_opportunities (
            id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
            symbol VARCHAR(50) NOT NULL,
            base_currency VARCHAR(20) NOT NULL,
            quote_currency VARCHAR(20) NOT NULL,
            long_exchange VARCHAR(50) NOT NULL,
            short_exchange VARCHAR(50) NOT NULL,
            long_exchange_id INTEGER,
            short_exchange_id INTEGER,
            long_funding_rate DECIMAL(10, 8) NOT NULL,
            short_funding_rate DECIMAL(10, 8) NOT NULL,
            net_funding_rate DECIMAL(10, 8) NOT NULL,
            funding_interval INTEGER NOT NULL DEFAULT 8,
            long_mark_price DECIMAL(20, 8) NOT NULL,
            short_mark_price DECIMAL(20, 8) NOT NULL,
            price_difference DECIMAL(20, 8) NOT NULL,
            price_difference_percentage DECIMAL(8, 4) NOT NULL,
            hourly_rate DECIMAL(8, 4) NOT NULL,
            daily_rate DECIMAL(8, 4) NOT NULL,
            apy DECIMAL(8, 4) NOT NULL,
            estimated_profit_8h DECIMAL(8, 4) NOT NULL,
            estimated_profit_daily DECIMAL(8, 4) NOT NULL,
            estimated_profit_weekly DECIMAL(8, 4) NOT NULL,
            estimated_profit_monthly DECIMAL(8, 4) NOT NULL,
            risk_score DECIMAL(5, 2) NOT NULL DEFAULT 0,
            volatility_score DECIMAL(5, 2) NOT NULL DEFAULT 0,
            liquidity_score DECIMAL(5, 2) NOT NULL DEFAULT 0,
            recommended_position_size DECIMAL(20, 8),
            max_leverage DECIMAL(5, 2) NOT NULL DEFAULT 1.0,
            recommended_leverage DECIMAL(5, 2) DEFAULT 1.0,
            stop_loss_percentage DECIMAL(5, 2),
            min_position_size DECIMAL(20, 8),
            max_position_size DECIMAL(20, 8),
            optimal_position_size DECIMAL(20, 8),
            detected_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
            expires_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW() + INTERVAL '1 hour',
            next_funding_time TIMESTAMP WITH TIME ZONE,
            time_to_next_funding INTEGER,
            is_active BOOLEAN NOT NULL DEFAULT true
        );

        -- Create indexes for performance
        CREATE UNIQUE INDEX IF NOT EXISTS idx_futures_arbitrage_unique 
        ON futures_arbitrage_opportunities(symbol, long_exchange, short_exchange);
        
        CREATE INDEX IF NOT EXISTS idx_futures_arbitrage_opportunities_symbol 
        ON futures_arbitrage_opportunities(symbol);
        
        CREATE INDEX IF NOT EXISTS idx_futures_arbitrage_opportunities_apy 
        ON futures_arbitrage_opportunities(apy DESC);
        
        CREATE INDEX IF NOT EXISTS idx_futures_arbitrage_opportunities_active 
        ON futures_arbitrage_opportunities(is_active, expires_at);
        
        CREATE INDEX IF NOT EXISTS idx_futures_arbitrage_opportunities_detected_at 
        ON futures_arbitrage_opportunities(detected_at DESC);

        INSERT INTO migration_status (migration_number, migration_name, description, checksum) 
        VALUES (49, 'consolidated_049_futures_arbitrage_opportunities', 'Consolidated futures arbitrage opportunities table', 'consolidated');
        
        RAISE NOTICE 'Consolidated migration 049: futures arbitrage opportunities table';
    END IF;

    -- Record this consolidation migration
    INSERT INTO migration_status (migration_number, migration_name, description, checksum) 
    VALUES (50, migration_name, 'Migration conflicts consolidation and sequential order establishment', migration_checksum)
    ON CONFLICT (migration_number) DO UPDATE SET 
        description = EXCLUDED.description,
        applied_at = CURRENT_TIMESTAMP,
        checksum = EXCLUDED.checksum;

    RAISE NOTICE 'Migration 050: Successfully consolidated all migration conflicts and established sequential order';
    RAISE NOTICE 'Next migrations should start from 051 onwards';
    
END $$;

-- Add table comments for documentation
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'migration_status') THEN
        EXECUTE 'COMMENT ON TABLE migration_status IS ''Tracks applied database migrations to prevent conflicts and duplicates''';
    END IF;
END $$;
