-- Migration 056: Exchange Reliability Metrics and 90-Day Data Retention
-- Description: Creates exchange reliability tracking table and sets up data retention policies
-- Version: 056
-- Date: 2025-01-21

BEGIN;

-- =====================================================
-- Step 1: Create Exchange Reliability Metrics Table
-- =====================================================

CREATE TABLE IF NOT EXISTS exchange_reliability_metrics (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    exchange VARCHAR(50) NOT NULL,

    -- 24-hour metrics
    uptime_percent_24h DECIMAL(6, 3) NOT NULL DEFAULT 100.0,
    success_count_24h INTEGER NOT NULL DEFAULT 0,
    failure_count_24h INTEGER NOT NULL DEFAULT 0,
    avg_latency_ms_24h INTEGER NOT NULL DEFAULT 0,

    -- 7-day metrics
    uptime_percent_7d DECIMAL(6, 3) NOT NULL DEFAULT 100.0,
    success_count_7d INTEGER NOT NULL DEFAULT 0,
    failure_count_7d INTEGER NOT NULL DEFAULT 0,
    avg_latency_ms_7d INTEGER NOT NULL DEFAULT 0,

    -- 30-day metrics
    uptime_percent_30d DECIMAL(6, 3) NOT NULL DEFAULT 100.0,
    success_count_30d INTEGER NOT NULL DEFAULT 0,
    failure_count_30d INTEGER NOT NULL DEFAULT 0,
    avg_latency_ms_30d INTEGER NOT NULL DEFAULT 0,

    -- Risk scoring (0-20 scale to match existing risk model)
    risk_score DECIMAL(5, 2) NOT NULL DEFAULT 10.0,

    -- Incident tracking
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    last_failure TIMESTAMP WITH TIME ZONE,
    last_success TIMESTAMP WITH TIME ZONE,
    last_incident_type VARCHAR(100),

    -- Metadata
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),

    -- Constraints
    CONSTRAINT valid_uptime_24h CHECK (uptime_percent_24h BETWEEN 0 AND 100),
    CONSTRAINT valid_uptime_7d CHECK (uptime_percent_7d BETWEEN 0 AND 100),
    CONSTRAINT valid_uptime_30d CHECK (uptime_percent_30d BETWEEN 0 AND 100),
    CONSTRAINT valid_reliability_risk_score CHECK (risk_score BETWEEN 0 AND 20),
    CONSTRAINT unique_exchange UNIQUE (exchange)
);

-- =====================================================
-- Step 2: Create Exchange API Call Log Table
-- =====================================================

CREATE TABLE IF NOT EXISTS exchange_api_call_log (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    exchange VARCHAR(50) NOT NULL,
    endpoint VARCHAR(200),
    success BOOLEAN NOT NULL,
    latency_ms INTEGER NOT NULL DEFAULT 0,
    error_message TEXT,
    called_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),

    -- Constraints
    CONSTRAINT valid_latency CHECK (latency_ms >= 0)
);

-- =====================================================
-- Step 3: Create Order Book Snapshot Table (Optional Historical)
-- =====================================================

CREATE TABLE IF NOT EXISTS order_book_snapshots (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    exchange VARCHAR(50) NOT NULL,
    symbol VARCHAR(100) NOT NULL,

    -- Order book summary metrics
    best_bid DECIMAL(20, 8) NOT NULL,
    best_ask DECIMAL(20, 8) NOT NULL,
    mid_price DECIMAL(20, 8) NOT NULL,
    bid_ask_spread_pct DECIMAL(8, 6) NOT NULL,

    -- Depth metrics (USD value within X% of mid price)
    bid_depth_1pct DECIMAL(20, 2) NOT NULL DEFAULT 0,
    ask_depth_1pct DECIMAL(20, 2) NOT NULL DEFAULT 0,
    bid_depth_2pct DECIMAL(20, 2) NOT NULL DEFAULT 0,
    ask_depth_2pct DECIMAL(20, 2) NOT NULL DEFAULT 0,

    -- Order book imbalance
    imbalance_1pct DECIMAL(8, 6) NOT NULL DEFAULT 0,
    imbalance_2pct DECIMAL(8, 6) NOT NULL DEFAULT 0,

    -- Level counts
    bid_levels INTEGER NOT NULL DEFAULT 0,
    ask_levels INTEGER NOT NULL DEFAULT 0,

    -- Liquidity score (0-100)
    liquidity_score DECIMAL(5, 2) NOT NULL DEFAULT 50,

    -- Timestamp
    snapshot_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),

    -- Constraints
    CONSTRAINT valid_order_book_prices CHECK (best_bid > 0 AND best_ask > 0 AND mid_price > 0),
    CONSTRAINT valid_spread CHECK (bid_ask_spread_pct >= 0),
    CONSTRAINT valid_liquidity_score CHECK (liquidity_score BETWEEN 0 AND 100)
);

-- =====================================================
-- Step 4: Create Indexes for Performance
-- =====================================================

-- Exchange reliability metrics indexes
CREATE INDEX IF NOT EXISTS idx_exchange_reliability_exchange
    ON exchange_reliability_metrics(exchange);
CREATE INDEX IF NOT EXISTS idx_exchange_reliability_risk_score
    ON exchange_reliability_metrics(risk_score);
CREATE INDEX IF NOT EXISTS idx_exchange_reliability_updated_at
    ON exchange_reliability_metrics(updated_at DESC);

-- Exchange API call log indexes
CREATE INDEX IF NOT EXISTS idx_exchange_api_call_log_exchange
    ON exchange_api_call_log(exchange);
CREATE INDEX IF NOT EXISTS idx_exchange_api_call_log_called_at
    ON exchange_api_call_log(called_at DESC);
CREATE INDEX IF NOT EXISTS idx_exchange_api_call_log_success
    ON exchange_api_call_log(success);

-- Order book snapshots indexes
CREATE INDEX IF NOT EXISTS idx_order_book_snapshots_exchange_symbol
    ON order_book_snapshots(exchange, symbol);
CREATE INDEX IF NOT EXISTS idx_order_book_snapshots_snapshot_at
    ON order_book_snapshots(snapshot_at DESC);

-- Funding rate history index
CREATE INDEX IF NOT EXISTS idx_funding_rate_history_collected_at
    ON funding_rate_history(collected_at DESC);

-- =====================================================
-- Step 5: Create 90-Day Retention Cleanup Function
-- =====================================================

CREATE OR REPLACE FUNCTION cleanup_old_data(retention_days INTEGER DEFAULT 90)
RETURNS TABLE(
    table_name TEXT,
    rows_deleted BIGINT
) AS $$
DECLARE
    cutoff_date TIMESTAMP WITH TIME ZONE;
    deleted_count BIGINT;
BEGIN
    cutoff_date := NOW() - (retention_days || ' days')::INTERVAL;

    -- Cleanup exchange_api_call_log
    DELETE FROM exchange_api_call_log WHERE called_at < cutoff_date;
    GET DIAGNOSTICS deleted_count = ROW_COUNT;
    table_name := 'exchange_api_call_log';
    rows_deleted := deleted_count;
    RETURN NEXT;

    -- Cleanup order_book_snapshots
    DELETE FROM order_book_snapshots WHERE snapshot_at < cutoff_date;
    GET DIAGNOSTICS deleted_count = ROW_COUNT;
    table_name := 'order_book_snapshots';
    rows_deleted := deleted_count;
    RETURN NEXT;

    -- Cleanup funding_rate_history
    DELETE FROM funding_rate_history WHERE collected_at < cutoff_date;
    GET DIAGNOSTICS deleted_count = ROW_COUNT;
    table_name := 'funding_rate_history';
    rows_deleted := deleted_count;
    RETURN NEXT;

    -- Cleanup old inactive futures_arbitrage_opportunities (older than retention period)
    DELETE FROM futures_arbitrage_opportunities
    WHERE is_active = false AND detected_at < cutoff_date;
    GET DIAGNOSTICS deleted_count = ROW_COUNT;
    table_name := 'futures_arbitrage_opportunities';
    rows_deleted := deleted_count;
    RETURN NEXT;

    RETURN;
END;
$$ LANGUAGE plpgsql;

-- =====================================================
-- Step 6: Create View for Exchange Reliability Summary
-- =====================================================

CREATE OR REPLACE VIEW exchange_reliability_summary AS
SELECT
    exchange,
    uptime_percent_24h,
    uptime_percent_7d,
    uptime_percent_30d,
    avg_latency_ms_24h,
    risk_score,
    consecutive_failures,
    last_failure,
    last_success,
    updated_at,

    -- Health status classification
    CASE
        WHEN uptime_percent_24h >= 99.5 AND avg_latency_ms_24h < 200 THEN 'excellent'
        WHEN uptime_percent_24h >= 98.0 AND avg_latency_ms_24h < 500 THEN 'good'
        WHEN uptime_percent_24h >= 95.0 AND avg_latency_ms_24h < 1000 THEN 'fair'
        ELSE 'poor'
    END as health_status,

    -- Recent failure indicator
    CASE
        WHEN last_failure IS NULL THEN false
        WHEN last_failure > NOW() - INTERVAL '5 minutes' THEN true
        ELSE false
    END as has_recent_failure,

    -- Risk level classification
    CASE
        WHEN risk_score <= 5 THEN 'low'
        WHEN risk_score <= 10 THEN 'medium'
        WHEN risk_score <= 15 THEN 'high'
        ELSE 'critical'
    END as risk_level

FROM exchange_reliability_metrics
ORDER BY risk_score ASC, uptime_percent_24h DESC;

-- =====================================================
-- Step 7: Create Trigger for Updated At
-- =====================================================

-- Create trigger for exchange_reliability_metrics
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = 'update_exchange_reliability_metrics_updated_at') THEN
        CREATE TRIGGER update_exchange_reliability_metrics_updated_at
            BEFORE UPDATE ON exchange_reliability_metrics
            FOR EACH ROW
            EXECUTE FUNCTION update_updated_at_column();
    END IF;
EXCEPTION
    WHEN OTHERS THEN
        -- Ignore trigger creation errors
        NULL;
END;
$$;

-- =====================================================
-- Step 8: Insert Initial Data for Common Exchanges
-- =====================================================

INSERT INTO exchange_reliability_metrics (exchange, risk_score) VALUES
    ('binance', 5.0),
    ('bybit', 5.0),
    ('okx', 7.0),
    ('bitget', 8.0),
    ('kucoin', 8.0),
    ('gate', 9.0)
ON CONFLICT (exchange) DO NOTHING;

-- =====================================================
-- Step 9: Grant Permissions
-- =====================================================

-- Grant permissions to application roles (if they exist)
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'authenticated') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON exchange_reliability_metrics TO authenticated;
        GRANT SELECT, INSERT ON exchange_api_call_log TO authenticated;
        GRANT SELECT, INSERT ON order_book_snapshots TO authenticated;
        GRANT SELECT ON exchange_reliability_summary TO authenticated;
    END IF;

    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'anon') THEN
        GRANT SELECT ON exchange_reliability_metrics TO anon;
        GRANT SELECT ON exchange_reliability_summary TO anon;
    END IF;
EXCEPTION
    WHEN OTHERS THEN
        -- Ignore permission errors
        NULL;
END;
$$;

-- =====================================================
-- Step 10: Add Configuration for Data Retention
-- =====================================================

INSERT INTO system_config (config_key, config_value, description) VALUES
    ('data_retention_days', '90', 'Number of days to retain historical data'),
    ('exchange_reliability_tracking_enabled', 'true', 'Enable exchange reliability tracking'),
    ('order_book_snapshot_interval_minutes', '15', 'Interval for order book snapshots in minutes'),
    ('api_call_log_enabled', 'true', 'Enable API call logging for reliability tracking')
ON CONFLICT (config_key) DO UPDATE SET
    config_value = EXCLUDED.config_value,
    updated_at = NOW();

-- =====================================================
-- Step 11: Add Comments for Documentation
-- =====================================================

COMMENT ON TABLE exchange_reliability_metrics IS
    'Stores aggregated exchange reliability metrics for risk scoring (24h, 7d, 30d windows)';
COMMENT ON TABLE exchange_api_call_log IS
    'Logs individual API calls for exchange reliability analysis (90-day retention)';
COMMENT ON TABLE order_book_snapshots IS
    'Periodic snapshots of order book metrics for historical analysis (90-day retention)';

COMMENT ON FUNCTION cleanup_old_data IS
    'Deletes data older than retention period (default 90 days) from historical tables';
COMMENT ON VIEW exchange_reliability_summary IS
    'Real-time exchange reliability summary with health status classification';

-- =====================================================
-- Step 12: Record Migration
-- =====================================================

INSERT INTO schema_migrations (version, filename, description, applied_at)
VALUES (56, '056_exchange_reliability_and_retention.sql', 'Exchange reliability metrics and 90-day data retention', NOW())
ON CONFLICT (version) DO UPDATE SET
    filename = EXCLUDED.filename,
    applied_at = NOW();

COMMIT;
