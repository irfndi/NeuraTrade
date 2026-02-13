-- Migration: Create Futures Arbitrage Tables
-- Description: Creates comprehensive tables for futures arbitrage opportunities with APY calculations, risk management, and position sizing
-- Version: 035
-- Date: 2025-01-21

-- Create futures arbitrage opportunities table
CREATE TABLE IF NOT EXISTS futures_arbitrage_opportunities (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    symbol VARCHAR(20) NOT NULL,
    base_currency VARCHAR(10) NOT NULL,
    quote_currency VARCHAR(10) NOT NULL,
    
    -- Exchange information
    long_exchange VARCHAR(50) NOT NULL,
    short_exchange VARCHAR(50) NOT NULL,
    long_exchange_id INTEGER,
    short_exchange_id INTEGER,
    
    -- Funding rate data
    long_funding_rate DECIMAL(10, 8) NOT NULL,
    short_funding_rate DECIMAL(10, 8) NOT NULL,
    net_funding_rate DECIMAL(10, 8) NOT NULL,
    funding_interval INTEGER NOT NULL DEFAULT 8, -- hours
    
    -- Price data
    long_mark_price DECIMAL(20, 8) NOT NULL,
    short_mark_price DECIMAL(20, 8) NOT NULL,
    price_difference DECIMAL(20, 8) NOT NULL,
    price_difference_percentage DECIMAL(8, 4) NOT NULL,
    
    -- Profit calculations
    hourly_rate DECIMAL(8, 4) NOT NULL,
    daily_rate DECIMAL(8, 4) NOT NULL,
    apy DECIMAL(8, 4) NOT NULL,
    
    -- Estimated profits for different time horizons
    estimated_profit_8h DECIMAL(8, 4) NOT NULL,
    estimated_profit_daily DECIMAL(8, 4) NOT NULL,
    estimated_profit_weekly DECIMAL(8, 4) NOT NULL,
    estimated_profit_monthly DECIMAL(8, 4) NOT NULL,
    
    -- Risk management metrics
    risk_score DECIMAL(5, 2) NOT NULL DEFAULT 0,
    volatility_score DECIMAL(5, 2) NOT NULL DEFAULT 0,
    liquidity_score DECIMAL(5, 2) NOT NULL DEFAULT 0,
    
    -- Position sizing recommendations
    recommended_position_size DECIMAL(20, 8),
    max_leverage DECIMAL(5, 2) DEFAULT 1.0,
    recommended_leverage DECIMAL(5, 2) DEFAULT 1.0,
    stop_loss_percentage DECIMAL(5, 2),
    
    -- Position size bounds
    min_position_size DECIMAL(20, 8),
    max_position_size DECIMAL(20, 8),
    optimal_position_size DECIMAL(20, 8),
    
    -- Timing information
    detected_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW() + INTERVAL '1 hour',
    next_funding_time TIMESTAMP WITH TIME ZONE,
    time_to_next_funding INTEGER, -- minutes
    
    -- Status
    is_active BOOLEAN NOT NULL DEFAULT true,
    
    -- Market context
    market_trend VARCHAR(20) DEFAULT 'neutral',
    volume_24h DECIMAL(20, 8),
    open_interest DECIMAL(20, 8),
    
    -- Constraints
    CONSTRAINT valid_funding_rates CHECK (long_funding_rate BETWEEN -1 AND 1 AND short_funding_rate BETWEEN -1 AND 1),
    CONSTRAINT valid_prices CHECK (long_mark_price > 0 AND short_mark_price > 0),
    CONSTRAINT valid_risk_scores CHECK (risk_score >= 0 AND risk_score <= 100),
    CONSTRAINT valid_leverage CHECK (max_leverage >= 1 AND recommended_leverage >= 1),
    CONSTRAINT valid_timing CHECK (expires_at > detected_at)
);

-- Create futures arbitrage strategies table
CREATE TABLE IF NOT EXISTS futures_arbitrage_strategies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    opportunity_id UUID NOT NULL REFERENCES futures_arbitrage_opportunities(id) ON DELETE CASCADE,
    
    -- Strategy details
    strategy_name VARCHAR(100) NOT NULL,
    strategy_type VARCHAR(50) NOT NULL DEFAULT 'funding_rate_arbitrage',
    risk_tolerance VARCHAR(20) NOT NULL DEFAULT 'moderate',
    
    -- Capital allocation
    total_capital_required DECIMAL(20, 8) NOT NULL,
    long_position_size DECIMAL(20, 8) NOT NULL,
    short_position_size DECIMAL(20, 8) NOT NULL,
    
    -- Leverage settings
    long_leverage DECIMAL(5, 2) NOT NULL DEFAULT 1.0,
    short_leverage DECIMAL(5, 2) NOT NULL DEFAULT 1.0,
    
    -- Risk management
    stop_loss_long DECIMAL(20, 8),
    stop_loss_short DECIMAL(20, 8),
    take_profit_percentage DECIMAL(5, 2),
    max_drawdown_percentage DECIMAL(5, 2),
    
    -- Expected returns
    expected_hourly_return DECIMAL(8, 4),
    expected_daily_return DECIMAL(8, 4),
    expected_apy DECIMAL(8, 4),
    
    -- Execution details
    entry_conditions TEXT,
    exit_conditions TEXT,
    monitoring_requirements TEXT,
    
    -- Metadata
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    is_active BOOLEAN NOT NULL DEFAULT true,
    
    -- Constraints
    CONSTRAINT valid_capital CHECK (total_capital_required > 0),
    CONSTRAINT valid_position_sizes CHECK (long_position_size > 0 AND short_position_size > 0),
    CONSTRAINT valid_strategy_leverage CHECK (long_leverage >= 1 AND short_leverage >= 1)
);

-- Create funding rate history table for risk analysis
CREATE TABLE IF NOT EXISTS funding_rate_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    symbol VARCHAR(20) NOT NULL,
    exchange VARCHAR(50) NOT NULL,
    funding_rate DECIMAL(10, 8) NOT NULL,
    funding_time TIMESTAMP WITH TIME ZONE NOT NULL,
    mark_price DECIMAL(20, 8),
    index_price DECIMAL(20, 8),
    
    -- Metadata
    collected_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    
    -- Constraints
    CONSTRAINT valid_funding_rate_history CHECK (funding_rate BETWEEN -1 AND 1),
    CONSTRAINT valid_prices_history CHECK (mark_price > 0 AND index_price > 0),
    UNIQUE(symbol, exchange, funding_time)
);

-- Create futures arbitrage execution log
CREATE TABLE IF NOT EXISTS futures_arbitrage_executions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    strategy_id UUID NOT NULL REFERENCES futures_arbitrage_strategies(id) ON DELETE CASCADE,
    
    -- Execution details
    execution_type VARCHAR(20) NOT NULL, -- 'entry', 'exit', 'adjustment'
    execution_status VARCHAR(20) NOT NULL DEFAULT 'pending', -- 'pending', 'executed', 'failed', 'cancelled'
    
    -- Position details
    long_exchange VARCHAR(50) NOT NULL,
    short_exchange VARCHAR(50) NOT NULL,
    long_position_size DECIMAL(20, 8),
    short_position_size DECIMAL(20, 8),
    long_entry_price DECIMAL(20, 8),
    short_entry_price DECIMAL(20, 8),
    
    -- Profit/Loss tracking
    realized_pnl DECIMAL(20, 8) DEFAULT 0,
    unrealized_pnl DECIMAL(20, 8) DEFAULT 0,
    funding_received DECIMAL(20, 8) DEFAULT 0,
    fees_paid DECIMAL(20, 8) DEFAULT 0,
    
    -- Timing
    executed_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    
    -- Notes and metadata
    execution_notes TEXT,
    error_message TEXT
);

-- Create indexes for performance
CREATE INDEX IF NOT EXISTS idx_futures_arbitrage_opportunities_symbol ON futures_arbitrage_opportunities(symbol);
CREATE INDEX IF NOT EXISTS idx_futures_arbitrage_opportunities_exchanges ON futures_arbitrage_opportunities(long_exchange, short_exchange);
CREATE INDEX IF NOT EXISTS idx_futures_arbitrage_opportunities_apy ON futures_arbitrage_opportunities(apy DESC);
CREATE INDEX IF NOT EXISTS idx_futures_arbitrage_opportunities_risk_score ON futures_arbitrage_opportunities(risk_score);
CREATE INDEX IF NOT EXISTS idx_futures_arbitrage_opportunities_active ON futures_arbitrage_opportunities(is_active, expires_at);
CREATE INDEX IF NOT EXISTS idx_futures_arbitrage_opportunities_detected_at ON futures_arbitrage_opportunities(detected_at DESC);

CREATE INDEX IF NOT EXISTS idx_futures_arbitrage_strategies_opportunity ON futures_arbitrage_strategies(opportunity_id);
CREATE INDEX IF NOT EXISTS idx_futures_arbitrage_strategies_type ON futures_arbitrage_strategies(strategy_type);
CREATE INDEX IF NOT EXISTS idx_futures_arbitrage_strategies_active ON futures_arbitrage_strategies(is_active);

CREATE INDEX IF NOT EXISTS idx_funding_rate_history_symbol_exchange ON funding_rate_history(symbol, exchange);
CREATE INDEX IF NOT EXISTS idx_funding_rate_history_funding_time ON funding_rate_history(funding_time DESC);
CREATE INDEX IF NOT EXISTS idx_funding_rate_history_collected_at ON funding_rate_history(collected_at DESC);

CREATE INDEX IF NOT EXISTS idx_futures_arbitrage_executions_strategy ON futures_arbitrage_executions(strategy_id);
CREATE INDEX IF NOT EXISTS idx_futures_arbitrage_executions_status ON futures_arbitrage_executions(execution_status);
CREATE INDEX IF NOT EXISTS idx_futures_arbitrage_executions_executed_at ON futures_arbitrage_executions(executed_at DESC);

-- Create views for easier querying
CREATE OR REPLACE VIEW active_futures_arbitrage_opportunities AS
SELECT 
    *,
    CASE 
        WHEN apy >= 20 AND risk_score <= 30 THEN 'excellent'
        WHEN apy >= 15 AND risk_score <= 50 THEN 'good'
        WHEN apy >= 10 AND risk_score <= 70 THEN 'moderate'
        ELSE 'poor'
    END as opportunity_quality,
    
    CASE 
        WHEN time_to_next_funding <= 60 THEN 'urgent'
        WHEN time_to_next_funding <= 180 THEN 'soon'
        ELSE 'later'
    END as funding_urgency
FROM futures_arbitrage_opportunities 
WHERE is_active = true 
  AND expires_at > NOW()
  AND apy > 0
ORDER BY apy DESC, risk_score ASC;

CREATE OR REPLACE VIEW futures_arbitrage_market_summary AS
SELECT 
    COUNT(*) as total_opportunities,
    AVG(apy) as average_apy,
    MAX(apy) as highest_apy,
    AVG(risk_score) as average_risk_score,
    AVG(volatility_score) as average_volatility,
    COUNT(DISTINCT symbol) as unique_symbols,
    COUNT(DISTINCT long_exchange || '-' || short_exchange) as unique_exchange_pairs,
    
    -- Market condition assessment
    CASE 
        WHEN AVG(apy) >= 15 AND AVG(risk_score) <= 50 THEN 'favorable'
        WHEN AVG(apy) >= 8 AND AVG(risk_score) <= 70 THEN 'neutral'
        ELSE 'unfavorable'
    END as market_condition,
    
    -- Funding rate trend
    CASE 
        WHEN AVG(net_funding_rate) > 0.01 THEN 'high_positive'
        WHEN AVG(net_funding_rate) > 0.005 THEN 'moderate_positive'
        WHEN AVG(net_funding_rate) > -0.005 THEN 'neutral'
        WHEN AVG(net_funding_rate) > -0.01 THEN 'moderate_negative'
        ELSE 'high_negative'
    END as funding_rate_trend
    
FROM active_futures_arbitrage_opportunities;

-- Create functions for calculations (with ownership check)
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_proc WHERE proname = 'calculate_futures_arbitrage_apy') THEN
        EXECUTE '
        CREATE FUNCTION calculate_futures_arbitrage_apy(
            net_funding_rate DECIMAL,
            funding_interval INTEGER DEFAULT 8
        ) RETURNS DECIMAL AS $func$
        BEGIN
            -- Calculate APY from funding rate
            -- APY = (1 + rate_per_period)^periods_per_year - 1
            -- periods_per_year = 365 * 24 / funding_interval
            RETURN (POWER(1 + net_funding_rate, 365.0 * 24.0 / funding_interval) - 1) * 100;
        END;
        $func$ LANGUAGE plpgsql IMMUTABLE;';
    ELSE
        -- Function exists, skip creation to avoid ownership issues
        NULL;
    END IF;
EXCEPTION
    WHEN OTHERS THEN
        -- Ignore ownership errors
        NULL;
END;
$$;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_proc WHERE proname = 'calculate_position_size_recommendation') THEN
        EXECUTE '
        CREATE FUNCTION calculate_position_size_recommendation(
            available_capital DECIMAL,
            risk_tolerance VARCHAR DEFAULT ''moderate'',
            risk_score DECIMAL DEFAULT 50,
            max_leverage DECIMAL DEFAULT 1.0
        ) RETURNS DECIMAL AS $func$
        DECLARE
            risk_multiplier DECIMAL;
            leverage_factor DECIMAL;
        BEGIN
            -- Determine risk multiplier based on tolerance
            risk_multiplier := CASE risk_tolerance
                WHEN ''conservative'' THEN 0.1
                WHEN ''moderate'' THEN 0.2
                WHEN ''aggressive'' THEN 0.4
                ELSE 0.2
            END;
            
            -- Adjust for risk score (higher risk = smaller position)
            risk_multiplier := risk_multiplier * (100 - risk_score) / 100.0;
            
            -- Apply leverage factor
            leverage_factor := LEAST(max_leverage, 3.0); -- Cap at 3x for safety
            
            RETURN available_capital * risk_multiplier * leverage_factor;
        END;
        $func$ LANGUAGE plpgsql IMMUTABLE;';
    ELSE
        -- Function exists, skip creation to avoid ownership issues
        NULL;
    END IF;
EXCEPTION
    WHEN OTHERS THEN
        -- Ignore ownership errors
        NULL;
END;
$$;

-- Create trigger to update updated_at timestamp (with ownership check)
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_proc WHERE proname = 'update_updated_at_column') THEN
        EXECUTE '
        CREATE FUNCTION update_updated_at_column()
        RETURNS TRIGGER AS $func$
        BEGIN
            NEW.updated_at = NOW();
            RETURN NEW;
        END;
        $func$ LANGUAGE plpgsql;';
    ELSE
        -- Function exists, skip creation to avoid ownership issues
        NULL;
    END IF;
EXCEPTION
    WHEN OTHERS THEN
        -- Ignore ownership errors
        NULL;
END;
$$;

-- Create trigger with existence check
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = 'update_futures_arbitrage_strategies_updated_at') THEN
        EXECUTE '
        CREATE TRIGGER update_futures_arbitrage_strategies_updated_at
            BEFORE UPDATE ON futures_arbitrage_strategies
            FOR EACH ROW
            EXECUTE FUNCTION update_updated_at_column();';
    END IF;
EXCEPTION
    WHEN OTHERS THEN
        -- Ignore trigger creation errors
        NULL;
END;
$$;

-- Grant permissions to application roles
GRANT SELECT, INSERT, UPDATE, DELETE ON futures_arbitrage_opportunities TO authenticated;
GRANT SELECT, INSERT, UPDATE, DELETE ON futures_arbitrage_strategies TO authenticated;
GRANT SELECT, INSERT, UPDATE, DELETE ON funding_rate_history TO authenticated;
GRANT SELECT, INSERT, UPDATE, DELETE ON futures_arbitrage_executions TO authenticated;

GRANT SELECT ON active_futures_arbitrage_opportunities TO authenticated;
GRANT SELECT ON futures_arbitrage_market_summary TO authenticated;

GRANT SELECT ON futures_arbitrage_opportunities TO anon;
GRANT SELECT ON active_futures_arbitrage_opportunities TO anon;
GRANT SELECT ON futures_arbitrage_market_summary TO anon;

-- Grant usage on sequences (if any are created automatically)
GRANT USAGE ON ALL SEQUENCES IN SCHEMA public TO authenticated;

-- Add comments for documentation
COMMENT ON TABLE futures_arbitrage_opportunities IS 'Stores futures arbitrage opportunities with comprehensive risk and profit metrics';
COMMENT ON TABLE futures_arbitrage_strategies IS 'Stores complete trading strategies for futures arbitrage execution';
COMMENT ON TABLE funding_rate_history IS 'Historical funding rate data for risk analysis and backtesting';
COMMENT ON TABLE futures_arbitrage_executions IS 'Logs of actual arbitrage strategy executions and their results';

COMMENT ON VIEW active_futures_arbitrage_opportunities IS 'Active opportunities with quality and urgency classifications';
COMMENT ON VIEW futures_arbitrage_market_summary IS 'Real-time market summary for futures arbitrage conditions';

-- Add comments for functions (with ownership check)
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_proc p JOIN pg_namespace n ON p.pronamespace = n.oid 
               WHERE n.nspname = 'public' AND p.proname = 'calculate_futures_arbitrage_apy' 
               AND pg_has_role(current_user, p.proowner, 'USAGE')) THEN
        EXECUTE 'COMMENT ON FUNCTION calculate_futures_arbitrage_apy IS ''Calculates annualized percentage yield from funding rates''';
    END IF;
EXCEPTION
    WHEN OTHERS THEN
        -- Ignore ownership errors
        NULL;
END;
$$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_proc p JOIN pg_namespace n ON p.pronamespace = n.oid 
               WHERE n.nspname = 'public' AND p.proname = 'calculate_position_size_recommendation' 
               AND pg_has_role(current_user, p.proowner, 'USAGE')) THEN
        EXECUTE 'COMMENT ON FUNCTION calculate_position_size_recommendation IS ''Recommends position size based on capital, risk tolerance, and market conditions''';
    END IF;
EXCEPTION
    WHEN OTHERS THEN
        -- Ignore ownership errors
        NULL;
END;
$$;

-- Insert initial configuration data
INSERT INTO system_config (config_key, config_value, description) VALUES 
('futures_arbitrage_enabled', 'true', 'Enable futures arbitrage functionality'),
('futures_arbitrage_min_apy', '5.0', 'Minimum APY threshold for futures arbitrage opportunities'),
('futures_arbitrage_max_risk_score', '80.0', 'Maximum risk score for futures arbitrage opportunities'),
('futures_arbitrage_default_leverage', '2.0', 'Default leverage for futures arbitrage strategies'),
('futures_arbitrage_funding_interval', '8', 'Default funding interval in hours')
ON CONFLICT (config_key) DO UPDATE SET 
    config_value = EXCLUDED.config_value,
    updated_at = NOW();

-- Migration completion
INSERT INTO schema_migrations (filename, applied, applied_at) 
VALUES ('035_create_futures_arbitrage_tables.sql', true, NOW())
ON CONFLICT (filename) DO UPDATE SET 
    applied = true,
    applied_at = NOW();