-- Migration: Create AI Usage Table for Cost Tracking
-- Description: Creates table for tracking AI API usage and costs for budget enforcement
-- Version: 064
-- Date: 2026-02-12

-- Create ai_usage table for cost tracking
CREATE TABLE IF NOT EXISTS ai_usage (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    
    -- Provider information
    provider VARCHAR(50) NOT NULL,  -- 'openai', 'anthropic', 'local', etc.
    model VARCHAR(100) NOT NULL,    -- 'gpt-4', 'claude-3-opus', 'llama-3', etc.
    request_type VARCHAR(50) NOT NULL DEFAULT 'chat',  -- 'chat', 'completion', 'embedding', etc.
    
    -- Token usage
    input_tokens INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    total_tokens INTEGER GENERATED ALWAYS AS (input_tokens + output_tokens) STORED,
    
    -- Cost tracking (in USD)
    input_cost_usd DECIMAL(12, 8) NOT NULL DEFAULT 0,
    output_cost_usd DECIMAL(12, 8) NOT NULL DEFAULT 0,
    total_cost_usd DECIMAL(12, 8) GENERATED ALWAYS AS (input_cost_usd + output_cost_usd) STORED,
    
    -- Context information
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    session_id VARCHAR(100),        -- For grouping related requests
    request_id VARCHAR(100),        -- External request ID for correlation
    
    -- Request/Response metadata
    latency_ms INTEGER,             -- Request latency in milliseconds
    status VARCHAR(20) NOT NULL DEFAULT 'success',  -- 'success', 'error', 'timeout'
    error_message TEXT,
    
    -- Additional metadata
    metadata JSONB DEFAULT '{}',
    
    -- Timestamps
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    
    -- Constraints
    CONSTRAINT valid_tokens CHECK (input_tokens >= 0 AND output_tokens >= 0),
    CONSTRAINT valid_cost CHECK (input_cost_usd >= 0 AND output_cost_usd >= 0),
    CONSTRAINT valid_status CHECK (status IN ('success', 'error', 'timeout', 'cancelled'))
);

-- Create indexes for common query patterns
CREATE INDEX IF NOT EXISTS idx_ai_usage_created_at ON ai_usage(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_usage_provider ON ai_usage(provider);
CREATE INDEX IF NOT EXISTS idx_ai_usage_model ON ai_usage(model);
CREATE INDEX IF NOT EXISTS idx_ai_usage_user_id ON ai_usage(user_id);
CREATE INDEX IF NOT EXISTS idx_ai_usage_session_id ON ai_usage(session_id);
CREATE INDEX IF NOT EXISTS idx_ai_usage_status ON ai_usage(status);
CREATE INDEX IF NOT EXISTS idx_ai_usage_provider_created ON ai_usage(provider, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_usage_user_created ON ai_usage(user_id, created_at DESC);

-- Create view for daily cost summary
CREATE OR REPLACE VIEW ai_usage_daily_summary AS
SELECT
    DATE(created_at) as usage_date,
    provider,
    model,
    COUNT(*) as total_requests,
    SUM(input_tokens) as total_input_tokens,
    SUM(output_tokens) as total_output_tokens,
    SUM(total_tokens) as grand_total_tokens,
    SUM(input_cost_usd) as total_input_cost,
    SUM(output_cost_usd) as total_output_cost,
    SUM(total_cost_usd) as grand_total_cost,
    AVG(latency_ms) as avg_latency_ms,
    COUNT(*) FILTER (WHERE status = 'error') as error_count,
    COUNT(*) FILTER (WHERE status = 'timeout') as timeout_count
FROM ai_usage
GROUP BY DATE(created_at), provider, model
ORDER BY usage_date DESC, grand_total_cost DESC;

-- Create view for monthly cost summary
CREATE OR REPLACE VIEW ai_usage_monthly_summary AS
SELECT
    DATE_TRUNC('month', created_at) as usage_month,
    provider,
    COUNT(*) as total_requests,
    SUM(input_tokens) as total_input_tokens,
    SUM(output_tokens) as total_output_tokens,
    SUM(total_tokens) as grand_total_tokens,
    SUM(total_cost_usd) as grand_total_cost,
    AVG(latency_ms) as avg_latency_ms
FROM ai_usage
GROUP BY DATE_TRUNC('month', created_at), provider
ORDER BY usage_month DESC, grand_total_cost DESC;

-- Create view for user cost summary
CREATE OR REPLACE VIEW ai_usage_user_summary AS
SELECT
    user_id,
    provider,
    COUNT(*) as total_requests,
    SUM(total_tokens) as grand_total_tokens,
    SUM(total_cost_usd) as grand_total_cost
FROM ai_usage
WHERE user_id IS NOT NULL
GROUP BY user_id, provider
ORDER BY grand_total_cost DESC;

-- Create function to get cost for a date range
CREATE OR REPLACE FUNCTION get_ai_cost_for_period(
    start_date TIMESTAMP WITH TIME ZONE,
    end_date TIMESTAMP WITH TIME ZONE,
    p_user_id UUID DEFAULT NULL
) RETURNS DECIMAL AS $$
DECLARE
    total_cost DECIMAL;
BEGIN
    IF p_user_id IS NULL THEN
        SELECT COALESCE(SUM(total_cost_usd), 0) INTO total_cost
        FROM ai_usage
        WHERE created_at >= start_date AND created_at < end_date;
    ELSE
        SELECT COALESCE(SUM(total_cost_usd), 0) INTO total_cost
        FROM ai_usage
        WHERE created_at >= start_date AND created_at < end_date
          AND user_id = p_user_id;
    END IF;
    
    RETURN total_cost;
END;
$$ LANGUAGE plpgsql STABLE;

-- Create function to check if daily budget is exceeded
CREATE OR REPLACE FUNCTION check_daily_budget_exceeded(
    daily_budget_usd DECIMAL,
    p_user_id UUID DEFAULT NULL
) RETURNS BOOLEAN AS $$
DECLARE
    today_cost DECIMAL;
BEGIN
    today_cost := get_ai_cost_for_period(
        DATE_TRUNC('day', NOW()),
        DATE_TRUNC('day', NOW()) + INTERVAL '1 day',
        p_user_id
    );
    
    RETURN today_cost >= daily_budget_usd;
END;
$$ LANGUAGE plpgsql STABLE;

-- Create function to check if monthly budget is exceeded
CREATE OR REPLACE FUNCTION check_monthly_budget_exceeded(
    monthly_budget_usd DECIMAL,
    p_user_id UUID DEFAULT NULL
) RETURNS BOOLEAN AS $$
DECLARE
    month_cost DECIMAL;
BEGIN
    month_cost := get_ai_cost_for_period(
        DATE_TRUNC('month', NOW()),
        DATE_TRUNC('month', NOW()) + INTERVAL '1 month',
        p_user_id
    );
    
    RETURN month_cost >= monthly_budget_usd;
END;
$$ LANGUAGE plpgsql STABLE;

-- Grant permissions
GRANT SELECT, INSERT, UPDATE, DELETE ON ai_usage TO authenticated;
GRANT SELECT ON ai_usage_daily_summary TO authenticated;
GRANT SELECT ON ai_usage_monthly_summary TO authenticated;
GRANT SELECT ON ai_usage_user_summary TO authenticated;
GRANT EXECUTE ON FUNCTION get_ai_cost_for_period TO authenticated;
GRANT EXECUTE ON FUNCTION check_daily_budget_exceeded TO authenticated;
GRANT EXECUTE ON FUNCTION check_monthly_budget_exceeded TO authenticated;

GRANT SELECT ON ai_usage TO anon;
GRANT SELECT ON ai_usage_daily_summary TO anon;
GRANT SELECT ON ai_usage_monthly_summary TO anon;

-- Add comments for documentation
COMMENT ON TABLE ai_usage IS 'Tracks AI API usage and costs for budget enforcement';
COMMENT ON COLUMN ai_usage.provider IS 'AI provider name (openai, anthropic, local, etc.)';
COMMENT ON COLUMN ai_usage.model IS 'Model identifier (gpt-4, claude-3-opus, etc.)';
COMMENT ON COLUMN ai_usage.input_tokens IS 'Number of input/prompt tokens';
COMMENT ON COLUMN ai_usage.output_tokens IS 'Number of output/completion tokens';
COMMENT ON COLUMN ai_usage.total_tokens IS 'Generated column: total input + output tokens';
COMMENT ON COLUMN ai_usage.input_cost_usd IS 'Cost for input tokens in USD';
COMMENT ON COLUMN ai_usage.output_cost_usd IS 'Cost for output tokens in USD';
COMMENT ON COLUMN ai_usage.total_cost_usd IS 'Generated column: total cost in USD';
COMMENT ON COLUMN ai_usage.session_id IS 'Session identifier for grouping related requests';
COMMENT ON COLUMN ai_usage.metadata IS 'Additional JSON metadata for the request';

COMMENT ON VIEW ai_usage_daily_summary IS 'Daily aggregated AI usage and costs by provider and model';
COMMENT ON VIEW ai_usage_monthly_summary IS 'Monthly aggregated AI usage and costs by provider';
COMMENT ON VIEW ai_usage_user_summary IS 'User-specific AI usage and cost summary';

COMMENT ON FUNCTION get_ai_cost_for_period IS 'Returns total AI cost for a given time period, optionally filtered by user';
COMMENT ON FUNCTION check_daily_budget_exceeded IS 'Checks if daily AI budget has been exceeded';
COMMENT ON FUNCTION check_monthly_budget_exceeded IS 'Checks if monthly AI budget has been exceeded';

-- Insert initial configuration for budget limits
INSERT INTO system_config (config_key, config_value, description) VALUES 
('ai_daily_budget_usd', '10.00', 'Daily budget limit for AI API calls in USD'),
('ai_monthly_budget_usd', '200.00', 'Monthly budget limit for AI API calls in USD'),
('ai_cost_tracking_enabled', 'true', 'Enable/disable AI cost tracking'),
('ai_budget_enforcement_enabled', 'true', 'Enable/disable budget enforcement (blocks requests over budget)')
ON CONFLICT (config_key) DO UPDATE SET 
    config_value = EXCLUDED.config_value,
    updated_at = NOW();

-- Migration completion record
INSERT INTO schema_migrations (version, filename, description, applied_at) 
VALUES (64, '064_create_ai_usage_table.sql', 'Create AI usage table', NOW())
ON CONFLICT (version) DO UPDATE SET 
    applied_at = NOW();
