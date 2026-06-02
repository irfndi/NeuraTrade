-- Migration 086: Add comprehensive columns to trade_audit_log for structured audit trail.
-- This extends the existing trade_audit_log table with structured JSON snapshots
-- and metadata fields needed for full trade decision reconstruction.
--
-- Retention policy:
-- Hot (main table): 90 days. Cold (archive): 1 year.
-- TODO: Implement archive job that moves rows older than 90 days to
--       trade_audit_log_archive table, then purges from main table.
--       See services/backend-api/internal/services/trade_audit_logger.go

-- Add new columns if they don't exist (idempotent via DO block)
DO $$
BEGIN
    -- user_id for operator attribution
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'trade_audit_log' AND column_name = 'user_id') THEN
        ALTER TABLE trade_audit_log ADD COLUMN user_id TEXT;
    END IF;

    -- signal_id cross-reference
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'trade_audit_log' AND column_name = 'signal_id') THEN
        ALTER TABLE trade_audit_log ADD COLUMN signal_id TEXT;
    END IF;

    -- ai_reasoning_snapshot: JSON decision chain
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'trade_audit_log' AND column_name = 'ai_reasoning_snapshot') THEN
        ALTER TABLE trade_audit_log ADD COLUMN ai_reasoning_snapshot TEXT;
    END IF;

    -- pre_trade_risk_snapshot: JSON risk check result
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'trade_audit_log' AND column_name = 'pre_trade_risk_snapshot') THEN
        ALTER TABLE trade_audit_log ADD COLUMN pre_trade_risk_snapshot TEXT;
    END IF;

    -- order_request: JSON payload sent to exchange
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'trade_audit_log' AND column_name = 'order_request') THEN
        ALTER TABLE trade_audit_log ADD COLUMN order_request TEXT;
    END IF;

    -- order_response: JSON exchange response
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'trade_audit_log' AND column_name = 'order_response') THEN
        ALTER TABLE trade_audit_log ADD COLUMN order_response TEXT;
    END IF;

    -- position_state: JSON post-order position snapshot
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'trade_audit_log' AND column_name = 'position_state') THEN
        ALTER TABLE trade_audit_log ADD COLUMN position_state TEXT;
    END IF;

    -- realized_pnl: decimal string
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'trade_audit_log' AND column_name = 'realized_pnl') THEN
        ALTER TABLE trade_audit_log ADD COLUMN realized_pnl TEXT;
    END IF;

    -- holding_seconds: integer
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'trade_audit_log' AND column_name = 'holding_seconds') THEN
        ALTER TABLE trade_audit_log ADD COLUMN holding_seconds INTEGER;
    END IF;

    -- size: decimal string (aliases from legacy amount column)
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'trade_audit_log' AND column_name = 'size') THEN
        ALTER TABLE trade_audit_log ADD COLUMN size TEXT;
    END IF;

    -- requested_price: decimal string
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'trade_audit_log' AND column_name = 'requested_price') THEN
        ALTER TABLE trade_audit_log ADD COLUMN requested_price TEXT;
    END IF;
END $$;

-- Create RULE to prevent UPDATE on trade_audit_log (append-only enforcement)
-- Using a DO block to make it idempotent
DO $$
BEGIN
    -- Drop existing rule if it exists (Postgres doesn't have IF NOT EXISTS for rules)
    DROP RULE IF EXISTS prevent_trade_audit_log_update ON trade_audit_log;
    
    -- Create the rule
    CREATE RULE prevent_trade_audit_log_update AS
    ON UPDATE TO trade_audit_log
    DO INSTEAD NOTHING;
    
    DROP RULE IF EXISTS prevent_trade_audit_log_delete ON trade_audit_log;
    
    CREATE RULE prevent_trade_audit_log_delete AS
    ON DELETE TO trade_audit_log
    DO INSTEAD NOTHING;
END $$;

-- Revoke UPDATE and DELETE from public (defense in depth)
REVOKE UPDATE, DELETE ON trade_audit_log FROM PUBLIC;
