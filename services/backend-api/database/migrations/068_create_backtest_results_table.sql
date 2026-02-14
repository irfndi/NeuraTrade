-- Migration: 068_create_backtest_results_table.sql
-- Task: neura-h2p - Store backtest results
-- Description: Create table to persist backtest run results for historical analysis and strategy comparison

BEGIN;

-- Main backtest results table
CREATE TABLE IF NOT EXISTS backtest_results (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    
    -- Configuration used for this backtest
    config JSONB NOT NULL DEFAULT '{}',
    
    -- Performance metrics
    total_return DECIMAL(20, 8) NOT NULL DEFAULT 0,
    total_pnl DECIMAL(20, 8) NOT NULL DEFAULT 0,
    sharpe_ratio DECIMAL(10, 4),
    sortino_ratio DECIMAL(10, 4),
    max_drawdown DECIMAL(20, 8) NOT NULL DEFAULT 0,
    max_drawdown_date TIMESTAMPTZ,
    
    -- Trade statistics
    win_rate DECIMAL(10, 6),
    loss_rate DECIMAL(10, 6),
    profit_factor DECIMAL(20, 8),
    total_trades INTEGER NOT NULL DEFAULT 0,
    winning_trades INTEGER NOT NULL DEFAULT 0,
    losing_trades INTEGER NOT NULL DEFAULT 0,
    avg_win DECIMAL(20, 8),
    avg_loss DECIMAL(20, 8),
    avg_holding_time_seconds INTEGER,
    
    -- Trade breakdown by symbol/exchange (JSON for flexibility)
    trades_by_symbol JSONB DEFAULT '{}',
    trades_by_exchange JSONB DEFAULT '{}',
    
    -- Equity curve (array of {timestamp, equity} points)
    equity_curve JSONB DEFAULT '[]',
    
    -- Daily returns for analysis
    daily_returns JSONB DEFAULT '[]',
    
    -- Timestamps
    started_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ NOT NULL,
    duration_seconds INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Indexes for efficient querying (idempotent)
CREATE INDEX IF NOT EXISTS idx_backtest_results_user_id ON backtest_results(user_id);
CREATE INDEX IF NOT EXISTS idx_backtest_results_created_at ON backtest_results(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_backtest_results_sharpe_ratio ON backtest_results(sharpe_ratio DESC) WHERE sharpe_ratio IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_backtest_results_total_return ON backtest_results(total_return DESC);

-- Comments
COMMENT ON TABLE backtest_results IS 'Persisted backtest run results for historical analysis and strategy comparison';
COMMENT ON COLUMN backtest_results.user_id IS 'User who ran the backtest (NULL for system-wide backtests)';
COMMENT ON COLUMN backtest_results.config IS 'JSON configuration used for this backtest run';
COMMENT ON COLUMN backtest_results.trades_by_symbol IS 'Map of symbol -> trade count';
COMMENT ON COLUMN backtest_results.trades_by_exchange IS 'Map of exchange -> trade count';
COMMENT ON COLUMN backtest_results.equity_curve IS 'Array of {timestamp, equity} points';
COMMENT ON COLUMN backtest_results.daily_returns IS 'Array of {date, return, pnl, equity, trade_count} objects';

-- Trigger for updated_at (idempotent)
DROP TRIGGER IF EXISTS update_backtest_results_updated_at ON backtest_results;
CREATE TRIGGER update_backtest_results_updated_at 
    BEFORE UPDATE ON backtest_results 
    FOR EACH ROW 
    EXECUTE FUNCTION update_updated_at_column();

COMMIT;
