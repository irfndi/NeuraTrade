-- NeuraTrade SQLite Schema
-- Consolidated migration for SQLite (replaces PostgreSQL migrations 001-069)
-- Generated: 2026-02-17

-- Enable foreign keys
PRAGMA foreign_keys = ON;

-- Users table
CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
    email TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    telegram_chat_id TEXT(50),
    subscription_tier TEXT DEFAULT 'free' CHECK (subscription_tier IN ('free', 'premium', 'enterprise')),
    telegram_blocked INTEGER DEFAULT 0,
    telegram_blocked_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
CREATE INDEX IF NOT EXISTS idx_users_telegram_chat_id ON users(telegram_chat_id);
CREATE INDEX IF NOT EXISTS idx_users_telegram_blocked ON users(telegram_blocked) WHERE telegram_blocked = 1;

-- Exchanges table
CREATE TABLE IF NOT EXISTS exchanges (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT UNIQUE NOT NULL,
    api_url TEXT NOT NULL,
    is_active INTEGER DEFAULT 1,
    last_ping DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- CCXT Configuration table
CREATE TABLE IF NOT EXISTS ccxt_exchanges (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    exchange_id INTEGER REFERENCES exchanges(id),
    ccxt_id TEXT NOT NULL,
    is_testnet INTEGER DEFAULT 0,
    api_key_required INTEGER DEFAULT 0,
    rate_limit INTEGER DEFAULT 1000,
    has_futures INTEGER DEFAULT 0,
    websocket_enabled INTEGER DEFAULT 0,
    last_health_check DATETIME,
    status TEXT DEFAULT 'active'
);

-- Trading pairs table
CREATE TABLE IF NOT EXISTS trading_pairs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    symbol TEXT UNIQUE NOT NULL,
    base_currency TEXT NOT NULL,
    quote_currency TEXT NOT NULL,
    is_futures INTEGER DEFAULT 0,
    is_active INTEGER DEFAULT 1,
    volume_24h REAL DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_trading_pairs_symbol ON trading_pairs(symbol);
CREATE INDEX IF NOT EXISTS idx_trading_pairs_base_quote ON trading_pairs(base_currency, quote_currency);
CREATE INDEX IF NOT EXISTS idx_trading_pairs_is_futures ON trading_pairs(is_futures);

-- Market data table
CREATE TABLE IF NOT EXISTS market_data (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    exchange_id INTEGER REFERENCES exchanges(id),
    trading_pair_id INTEGER REFERENCES trading_pairs(id),
    price REAL NOT NULL,
    volume REAL NOT NULL,
    bid REAL,
    ask REAL,
    high_24h REAL,
    low_24h REAL,
    volume_24h REAL,
    timestamp DATETIME NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_market_data_timestamp ON market_data(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_market_data_exchange_pair ON market_data(exchange_id, trading_pair_id);
CREATE INDEX IF NOT EXISTS idx_market_data_exchange_time ON market_data(exchange_id, timestamp DESC);

-- Technical indicators table
CREATE TABLE IF NOT EXISTS technical_indicators (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    exchange_id INTEGER REFERENCES exchanges(id),
    trading_pair_id INTEGER REFERENCES trading_pairs(id),
    timeframe TEXT NOT NULL,
    rsi REAL,
    macd REAL,
    macd_signal REAL,
    macd_histogram REAL,
    bb_upper REAL,
    bb_middle REAL,
    bb_lower REAL,
    ema_9 REAL,
    ema_21 REAL,
    ema_50 REAL,
    ema_200 REAL,
    sma_20 REAL,
    sma_50 REAL,
    sma_200 REAL,
    stochastic_k REAL,
    stochastic_d REAL,
    atr REAL,
    adx REAL,
    cci REAL,
    williams_r REAL,
    momentum REAL,
    roc REAL,
    timestamp DATETIME NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_technical_indicators_pair_timeframe ON technical_indicators(trading_pair_id, timeframe);
CREATE INDEX IF NOT EXISTS idx_technical_indicators_timestamp ON technical_indicators(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_technical_indicators_exchange_pair ON technical_indicators(exchange_id, trading_pair_id);

-- Arbitrage opportunities table
CREATE TABLE IF NOT EXISTS arbitrage_opportunities (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    trading_pair_id INTEGER REFERENCES trading_pairs(id),
    buy_exchange_id INTEGER REFERENCES exchanges(id),
    sell_exchange_id INTEGER REFERENCES exchanges(id),
    buy_price REAL NOT NULL,
    sell_price REAL NOT NULL,
    price_difference REAL NOT NULL,
    price_difference_percentage REAL NOT NULL,
    estimated_profit_percentage REAL NOT NULL,
    volume_available REAL,
    risk_score REAL DEFAULT 1.0 CHECK (risk_score >= 1.0 AND risk_score <= 5.0),
    is_active INTEGER DEFAULT 1,
    detected_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_arbitrage_active ON arbitrage_opportunities(is_active, detected_at DESC) WHERE is_active = 1;
CREATE INDEX IF NOT EXISTS idx_arbitrage_profit ON arbitrage_opportunities(estimated_profit_percentage DESC);
CREATE INDEX IF NOT EXISTS idx_arbitrage_trading_pair ON arbitrage_opportunities(trading_pair_id);

-- Funding rates table
CREATE TABLE IF NOT EXISTS funding_rates (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    exchange_id INTEGER NOT NULL REFERENCES exchanges(id) ON DELETE CASCADE,
    trading_pair_id INTEGER NOT NULL REFERENCES trading_pairs(id) ON DELETE CASCADE,
    funding_rate REAL NOT NULL,
    funding_time DATETIME NOT NULL,
    next_funding_time DATETIME,
    mark_price REAL,
    index_price REAL,
    timestamp DATETIME NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(exchange_id, trading_pair_id, funding_time)
);

CREATE INDEX IF NOT EXISTS idx_funding_rates_exchange_pair_time ON funding_rates(exchange_id, trading_pair_id, funding_time DESC);
CREATE INDEX IF NOT EXISTS idx_funding_rates_timestamp ON funding_rates(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_funding_rates_next_funding ON funding_rates(next_funding_time) WHERE next_funding_time IS NOT NULL;

-- Funding arbitrage opportunities table
CREATE TABLE IF NOT EXISTS funding_arbitrage_opportunities (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    trading_pair_id INTEGER NOT NULL REFERENCES trading_pairs(id) ON DELETE CASCADE,
    long_exchange_id INTEGER NOT NULL REFERENCES exchanges(id) ON DELETE CASCADE,
    short_exchange_id INTEGER NOT NULL REFERENCES exchanges(id) ON DELETE CASCADE,
    long_funding_rate REAL NOT NULL,
    short_funding_rate REAL NOT NULL,
    net_funding_rate REAL NOT NULL,
    estimated_profit_8h REAL NOT NULL CHECK (estimated_profit_8h >= 0),
    estimated_profit_daily REAL NOT NULL CHECK (estimated_profit_daily >= 0),
    estimated_profit_percentage REAL NOT NULL CHECK (estimated_profit_percentage >= 0),
    long_mark_price REAL,
    short_mark_price REAL,
    price_difference REAL,
    price_difference_percentage REAL,
    risk_score REAL DEFAULT 1.0 CHECK (risk_score >= 1.0 AND risk_score <= 5.0),
    is_active INTEGER DEFAULT 1,
    detected_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(trading_pair_id, long_exchange_id, short_exchange_id, detected_at),
    CHECK (long_exchange_id <> short_exchange_id)
);

CREATE INDEX IF NOT EXISTS idx_funding_arbitrage_active ON funding_arbitrage_opportunities(is_active, detected_at DESC) WHERE is_active = 1;
CREATE INDEX IF NOT EXISTS idx_funding_arbitrage_profit ON funding_arbitrage_opportunities(estimated_profit_percentage DESC);
CREATE INDEX IF NOT EXISTS idx_funding_arbitrage_active_filter ON funding_arbitrage_opportunities(is_active) WHERE is_active = 1;
CREATE INDEX IF NOT EXISTS idx_funding_arbitrage_expires ON funding_arbitrage_opportunities(expires_at) WHERE expires_at IS NOT NULL;

-- System config table
CREATE TABLE IF NOT EXISTS system_config (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    config_key TEXT UNIQUE NOT NULL,
    config_value TEXT,
    description TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Exchange blacklist table
CREATE TABLE IF NOT EXISTS exchange_blacklist (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    exchange_id INTEGER REFERENCES exchanges(id) ON DELETE CASCADE,
    reason TEXT NOT NULL,
    is_active INTEGER DEFAULT 1,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME
);

CREATE INDEX IF NOT EXISTS idx_exchange_blacklist_active ON exchange_blacklist(is_active) WHERE is_active = 1;

-- Exchange trading pairs table
CREATE TABLE IF NOT EXISTS exchange_trading_pairs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    exchange_id INTEGER NOT NULL REFERENCES exchanges(id) ON DELETE CASCADE,
    trading_pair_id INTEGER NOT NULL REFERENCES trading_pairs(id) ON DELETE CASCADE,
    is_active INTEGER DEFAULT 1,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(exchange_id, trading_pair_id)
);

CREATE INDEX IF NOT EXISTS idx_exchange_trading_pairs_exchange ON exchange_trading_pairs(exchange_id);
CREATE INDEX IF NOT EXISTS idx_exchange_trading_pairs_pair ON exchange_trading_pairs(trading_pair_id);

-- Aggregated signals table
CREATE TABLE IF NOT EXISTS aggregated_signals (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    trading_pair_id INTEGER NOT NULL REFERENCES trading_pairs(id) ON DELETE CASCADE,
    signal_type TEXT NOT NULL CHECK (signal_type IN ('BUY', 'SELL', 'HOLD')),
    signal_strength REAL NOT NULL CHECK (signal_strength >= 0 AND signal_strength <= 100),
    confidence REAL NOT NULL CHECK (confidence >= 0 AND confidence <= 100),
    technical_score REAL,
    arbitrage_score REAL,
    funding_rate_score REAL,
    overall_score REAL NOT NULL,
    metadata TEXT,
    is_active INTEGER DEFAULT 1,
    detected_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_aggregated_signals_active ON aggregated_signals(is_active, detected_at DESC) WHERE is_active = 1;
CREATE INDEX IF NOT EXISTS idx_aggregated_signals_score ON aggregated_signals(overall_score DESC);
CREATE INDEX IF NOT EXISTS idx_aggregated_signals_pair ON aggregated_signals(trading_pair_id);

-- Alert notifications table
CREATE TABLE IF NOT EXISTS alert_notifications (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id TEXT REFERENCES users(id) ON DELETE CASCADE,
    signal_id INTEGER REFERENCES aggregated_signals(id) ON DELETE SET NULL,
    notification_type TEXT NOT NULL,
    title TEXT NOT NULL,
    message TEXT NOT NULL,
    is_read INTEGER DEFAULT 0,
    sent_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_alert_notifications_user ON alert_notifications(user_id);
CREATE INDEX IF NOT EXISTS idx_alert_notifications_read ON alert_notifications(is_read) WHERE is_read = 0;
CREATE INDEX IF NOT EXISTS idx_alert_notifications_created ON alert_notifications(created_at DESC);

-- Notification dead letters table
CREATE TABLE IF NOT EXISTS notification_dead_letters (
    id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
    user_id TEXT NOT NULL,
    chat_id TEXT NOT NULL,
    message_type TEXT NOT NULL,
    message_content TEXT NOT NULL,
    error_code TEXT,
    error_message TEXT,
    attempts INTEGER DEFAULT 1,
    status TEXT DEFAULT 'pending' CHECK (status IN ('pending', 'retrying', 'failed', 'success')),
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_attempt_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    next_retry_at DATETIME,
    CONSTRAINT fk_dlq_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_dlq_status ON notification_dead_letters(status);
CREATE INDEX IF NOT EXISTS idx_dlq_next_retry ON notification_dead_letters(next_retry_at) WHERE status IN ('pending', 'retrying');
CREATE INDEX IF NOT EXISTS idx_dlq_user_id ON notification_dead_letters(user_id);
CREATE INDEX IF NOT EXISTS idx_dlq_created_at ON notification_dead_letters(created_at);

-- Exchange API keys table
CREATE TABLE IF NOT EXISTS exchange_api_keys (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    exchange_id INTEGER NOT NULL REFERENCES exchanges(id) ON DELETE CASCADE,
    api_key_encrypted TEXT NOT NULL,
    api_secret_encrypted TEXT NOT NULL,
    passphrase_encrypted TEXT,
    is_testnet INTEGER DEFAULT 0,
    is_active INTEGER DEFAULT 1,
    last_used_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, exchange_id)
);

CREATE INDEX IF NOT EXISTS idx_exchange_api_keys_user ON exchange_api_keys(user_id);
CREATE INDEX IF NOT EXISTS idx_exchange_api_keys_exchange ON exchange_api_keys(exchange_id);

-- One-time codes table
CREATE TABLE IF NOT EXISTS one_time_codes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code TEXT NOT NULL,
    code_type TEXT NOT NULL,
    is_used INTEGER DEFAULT 0,
    expires_at DATETIME NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    used_at DATETIME
);

CREATE INDEX IF NOT EXISTS idx_one_time_codes_code ON one_time_codes(code);
CREATE INDEX IF NOT EXISTS idx_one_time_codes_user ON one_time_codes(user_id);
CREATE INDEX IF NOT EXISTS idx_one_time_codes_expires ON one_time_codes(expires_at) WHERE is_used = 0;

-- Paper trades table
CREATE TABLE IF NOT EXISTS paper_trades (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    exchange_id INTEGER REFERENCES exchanges(id) ON DELETE SET NULL,
    trading_pair_id INTEGER REFERENCES trading_pairs(id) ON DELETE SET NULL,
    trade_type TEXT NOT NULL CHECK (trade_type IN ('BUY', 'SELL')),
    entry_price REAL NOT NULL,
    exit_price REAL,
    amount REAL NOT NULL,
    pnl REAL DEFAULT 0,
    pnl_percentage REAL DEFAULT 0,
    status TEXT DEFAULT 'open' CHECK (status IN ('open', 'closed', 'cancelled')),
    opened_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    closed_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    metadata TEXT
);

CREATE INDEX IF NOT EXISTS idx_paper_trades_user ON paper_trades(user_id);
CREATE INDEX IF NOT EXISTS idx_paper_trades_status ON paper_trades(status) WHERE status = 'open';
CREATE INDEX IF NOT EXISTS idx_paper_trades_opened ON paper_trades(opened_at DESC);

-- AI usage table
CREATE TABLE IF NOT EXISTS ai_usage (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    model_id TEXT NOT NULL,
    request_type TEXT NOT NULL,
    tokens_used INTEGER DEFAULT 0,
    response_time_ms INTEGER DEFAULT 0,
    success INTEGER DEFAULT 1,
    error_message TEXT,
    metadata TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_ai_usage_user ON ai_usage(user_id);
CREATE INDEX IF NOT EXISTS idx_ai_usage_model ON ai_usage(model_id);
CREATE INDEX IF NOT EXISTS idx_ai_usage_created ON ai_usage(created_at DESC);

-- AI reasoning summaries table
CREATE TABLE IF NOT EXISTS ai_reasoning_summaries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    session_id TEXT,
    summary_type TEXT NOT NULL,
    summary_content TEXT NOT NULL,
    confidence REAL,
    metadata TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_ai_reasoning_user ON ai_reasoning_summaries(user_id);
CREATE INDEX IF NOT EXISTS idx_ai_reasoning_session ON ai_reasoning_summaries(session_id);

-- AI sessions table
CREATE TABLE IF NOT EXISTS ai_sessions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    session_token TEXT UNIQUE NOT NULL,
    model_id TEXT NOT NULL,
    is_active INTEGER DEFAULT 1,
    metadata TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_ai_sessions_user ON ai_sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_ai_sessions_token ON ai_sessions(session_token);
CREATE INDEX IF NOT EXISTS idx_ai_sessions_active ON ai_sessions(is_active) WHERE is_active = 1;

-- Backtest results table
CREATE TABLE IF NOT EXISTS backtest_results (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    strategy_name TEXT NOT NULL,
    start_date DATETIME NOT NULL,
    end_date DATETIME NOT NULL,
    initial_capital REAL NOT NULL,
    final_capital REAL NOT NULL,
    total_return REAL NOT NULL,
    sharpe_ratio REAL,
    max_drawdown REAL,
    win_rate REAL,
    total_trades INTEGER DEFAULT 0,
    winning_trades INTEGER DEFAULT 0,
    losing_trades INTEGER DEFAULT 0,
    metadata TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_backtest_results_user ON backtest_results(user_id);
CREATE INDEX IF NOT EXISTS idx_backtest_results_strategy ON backtest_results(strategy_name);
CREATE INDEX IF NOT EXISTS idx_backtest_results_created ON backtest_results(created_at DESC);

-- Decision journal table
CREATE TABLE IF NOT EXISTS decision_journal (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    decision_type TEXT NOT NULL,
    context TEXT,
    reasoning TEXT,
    outcome TEXT,
    lessons_learned TEXT,
    metadata TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_decision_journal_user ON decision_journal(user_id);
CREATE INDEX IF NOT EXISTS idx_decision_journal_type ON decision_journal(decision_type);

-- Sentiment analysis table
CREATE TABLE IF NOT EXISTS sentiment_analysis (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    symbol TEXT NOT NULL,
    source TEXT NOT NULL,
    sentiment_score REAL NOT NULL CHECK (sentiment_score >= -1 AND sentiment_score <= 1),
    sentiment_label TEXT CHECK (sentiment_label IN ('positive', 'neutral', 'negative')),
    confidence REAL,
    article_title TEXT,
    article_url TEXT,
    published_at DATETIME,
    analyzed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_sentiment_symbol ON sentiment_analysis(symbol);
CREATE INDEX IF NOT EXISTS idx_sentiment_score ON sentiment_analysis(sentiment_score);
CREATE INDEX IF NOT EXISTS idx_sentiment_analyzed ON sentiment_analysis(analyzed_at DESC);

-- Slow query log table
CREATE TABLE IF NOT EXISTS slow_query_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    query_hash TEXT NOT NULL,
    query_text TEXT NOT NULL,
    execution_time_ms INTEGER NOT NULL,
    rows_affected INTEGER,
    error_message TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_slow_query_hash ON slow_query_log(query_hash);
CREATE INDEX IF NOT EXISTS idx_slow_query_time ON slow_query_log(execution_time_ms DESC);
CREATE INDEX IF NOT EXISTS idx_slow_query_created ON slow_query_log(created_at DESC);

-- KV store table
CREATE TABLE IF NOT EXISTS kv_store (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    key TEXT UNIQUE NOT NULL,
    value TEXT NOT NULL,
    value_type TEXT DEFAULT 'text' CHECK (value_type IN ('text', 'json', 'integer', 'real', 'boolean')),
    expires_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_kv_store_key ON kv_store(key);
CREATE INDEX IF NOT EXISTS idx_kv_store_expires ON kv_store(expires_at) WHERE expires_at IS NOT NULL;

-- Futures table
CREATE TABLE IF NOT EXISTS futures (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    exchange_id INTEGER NOT NULL REFERENCES exchanges(id) ON DELETE CASCADE,
    trading_pair_id INTEGER NOT NULL REFERENCES trading_pairs(id) ON DELETE CASCADE,
    mark_price REAL,
    index_price REAL,
    open_interest REAL,
    funding_rate REAL,
    next_funding_time DATETIME,
    contract_type TEXT,
    contract_size REAL,
    tick_size REAL,
    is_active INTEGER DEFAULT 1,
    last_updated DATETIME DEFAULT CURRENT_TIMESTAMP,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(exchange_id, trading_pair_id)
);

CREATE INDEX IF NOT EXISTS idx_futures_exchange_pair ON futures(exchange_id, trading_pair_id);
CREATE INDEX IF NOT EXISTS idx_futures_active ON futures(is_active) WHERE is_active = 1;

-- Schema migrations tracking table
CREATE TABLE IF NOT EXISTS schema_migrations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    filename TEXT UNIQUE NOT NULL,
    applied INTEGER DEFAULT 0,
    applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Insert initial exchanges
INSERT OR IGNORE INTO exchanges (name, api_url) VALUES
('binance', 'https://api.binance.com'),
('bybit', 'https://api.bybit.com'),
('okx', 'https://www.okx.com/api'),
('coinbase', 'https://api.exchange.coinbase.com'),
('kraken', 'https://api.kraken.com');

-- Insert initial CCXT configurations
INSERT OR IGNORE INTO ccxt_exchanges (exchange_id, ccxt_id, rate_limit, has_futures, websocket_enabled) VALUES
(1, 'binance', 1200, 1, 1),
(2, 'bybit', 600, 1, 1),
(3, 'okx', 600, 1, 1),
(4, 'coinbasepro', 300, 0, 1),
(5, 'kraken', 300, 1, 0);

-- Insert initial trading pairs
INSERT OR IGNORE INTO trading_pairs (symbol, base_currency, quote_currency, is_futures) VALUES
('BTCUSDT', 'BTC', 'USDT', 0),
('ETHUSDT', 'ETH', 'USDT', 0),
('ADAUSDT', 'ADA', 'USDT', 0),
('SOLUSDT', 'SOL', 'USDT', 0),
('DOTUSDT', 'DOT', 'USDT', 0),
('LINKUSDT', 'LINK', 'USDT', 0),
('AVAXUSDT', 'AVAX', 'USDT', 0),
('MATICUSDT', 'MATIC', 'USDT', 0),
('ATOMUSDT', 'ATOM', 'USDT', 0),
('NEARUSDT', 'NEAR', 'USDT', 0),
('BTCUSDT-PERP', 'BTC', 'USDT', 1),
('ETHUSDT-PERP', 'ETH', 'USDT', 1),
('SOLUSDT-PERP', 'SOL', 'USDT', 1);

-- Insert system configuration
INSERT OR REPLACE INTO system_config (config_key, config_value, description) VALUES
('funding_rate_min_profit', '0.01', 'Minimum funding rate profit percentage for arbitrage'),
('funding_rate_max_risk', '3.0', 'Maximum risk score for funding rate arbitrage'),
('funding_rate_collection_enabled', 'true', 'Enable funding rate data collection'),
('funding_rate_arbitrage_enabled', 'true', 'Enable funding rate arbitrage detection'),
('migration_sqlite_consolidated', 'true', 'SQLite consolidated migration completed');
