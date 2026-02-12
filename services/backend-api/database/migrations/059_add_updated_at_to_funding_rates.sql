-- Migration: 059_add_updated_at_to_funding_rates.sql
-- Description: Add missing updated_at column to funding_rates table
-- This fixes: ERROR: record "new" has no field "updated_at" (SQLSTATE 42703)
-- The trigger update_funding_rates_updated_at was created in 012_minimal_schema.sql
-- but the funding_rates table was missing the updated_at column

-- Add updated_at column to funding_rates if it doesn't exist
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'funding_rates'
        AND column_name = 'updated_at'
        AND table_schema = 'public'
    ) THEN
        ALTER TABLE funding_rates ADD COLUMN updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW();

        -- Set updated_at to created_at for existing rows
        UPDATE funding_rates SET updated_at = COALESCE(created_at, NOW()) WHERE updated_at IS NULL;

        RAISE NOTICE 'Added updated_at column to funding_rates table';
    ELSE
        RAISE NOTICE 'updated_at column already exists in funding_rates table';
    END IF;
END $$;

-- Ensure the trigger exists (recreate if needed for safety)
DO $$
BEGIN
    -- Check if trigger function exists
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
        RAISE NOTICE 'Created update_updated_at_column function';
    END IF;
END $$;

-- Recreate trigger to ensure it's properly attached
DROP TRIGGER IF EXISTS update_funding_rates_updated_at ON funding_rates;
CREATE TRIGGER update_funding_rates_updated_at
    BEFORE UPDATE ON funding_rates
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Verify the column was added
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'funding_rates'
        AND column_name = 'updated_at'
        AND table_schema = 'public'
    ) THEN
        RAISE NOTICE 'Migration 059 completed: funding_rates now has updated_at column';
    ELSE
        RAISE EXCEPTION 'Migration 059 failed: updated_at column still missing from funding_rates';
    END IF;
END $$;
