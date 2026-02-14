-- neura-h2p: Store backtest results
-- Migration: 068_create_backtest_results_table.sql

CREATE TABLE IF NOT EXISTS backtest_results (
    id SERIAL PRIMARY KEY,
    strategy_name VARCHAR(255) NOT NULL,
    symbol VARCHAR(50) NOT NULL,
    exchange VARCHAR(100) NOT NULL,
    timeframe VARCHAR(20) NOT NULL,
    
    -- Backtest parameters
    start_date TIMESTAMP NOT NULL,
    end_date TIMESTAMP NOT NULL,
    initial_capital DECIMAL(20, 8) NOT NULL,
    final_capital DECIMAL(20, 8) NOT NULL,
    
    -- Performance metrics
    total_return DECIMAL(20, 8) NOT NULL,
    total_return_pct DECIMAL(10, 4) NOT NULL,
    sharpe_ratio DECIMAL(10, 4),
    max_drawdown DECIMAL(10, 4),
    win_rate DECIMAL(10, 4),
    profit_factor DECIMAL(10, 4),
    total_trades INTEGER NOT NULL,
    winning_trades INTEGER NOT NULL,
    losing_trades INTEGER NOT NULL,
    
    -- Risk metrics
    avg_win DECIMAL(20, 8),
    avg_loss DECIMAL(20, 8),
    largest_win DECIMAL(20, 8),
    largest_loss DECIMAL(20, 8),
    
    -- Additional data
    params JSONB,
    trades JSONB,
    equity_curve JSONB,
    
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_backtest_results_symbol ON backtest_results(symbol);
CREATE INDEX idx_backtest_results_exchange ON backtest_results(exchange);
CREATE INDEX idx_backtest_results_strategy ON backtest_results(strategy_name);
CREATE INDEX idx_backtest_results_created_at ON backtest_results(created_at DESC);

CREATE OR REPLACE FUNCTION update_backtest_results_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER backtest_results_updated_at
    BEFORE UPDATE ON backtest_results
    FOR EACH ROW
    EXECUTE FUNCTION update_backtest_results_updated_at();
