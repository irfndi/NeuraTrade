-- Add remaining missing tables from Postgres migrations
-- Tables from PG 035, 056, 057, 068, 072

-- Futures arbitrage strategies (from PG 035)
CREATE TABLE IF NOT EXISTS futures_arbitrage_strategies (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    symbol TEXT NOT NULL,
    exchange TEXT NOT NULL,
    capital_allocation DECIMAL(20,8) DEFAULT 0,
    leverage INTEGER DEFAULT 1,
    stop_loss_pct DECIMAL(10,4) DEFAULT 0,
    take_profit_pct DECIMAL(10,4) DEFAULT 0,
    status TEXT DEFAULT 'active',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Funding rate history (from PG 035)
CREATE TABLE IF NOT EXISTS funding_rate_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    symbol TEXT NOT NULL,
    exchange TEXT NOT NULL,
    rate DECIMAL(20,8) NOT NULL,
    recorded_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Futures arbitrage executions (from PG 035)
CREATE TABLE IF NOT EXISTS futures_arbitrage_executions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    strategy_id INTEGER,
    symbol TEXT NOT NULL,
    exchange TEXT NOT NULL,
    side TEXT NOT NULL,
    entry_price DECIMAL(20,8),
    exit_price DECIMAL(20,8),
    size DECIMAL(20,8),
    pnl DECIMAL(20,8) DEFAULT 0,
    status TEXT DEFAULT 'open',
    opened_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    closed_at DATETIME
);

-- Exchange fees (from PG 057)
CREATE TABLE IF NOT EXISTS exchange_fees (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    exchange TEXT NOT NULL,
    symbol TEXT,
    taker_fee DECIMAL(10,6) DEFAULT 0,
    maker_fee DECIMAL(10,6) DEFAULT 0,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(exchange, symbol)
);

-- Trade outcomes (from PG 068)
CREATE TABLE IF NOT EXISTS trade_outcomes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    trade_id INTEGER,
    strategy TEXT,
    symbol TEXT,
    pnl DECIMAL(20,8) DEFAULT 0,
    hold_duration_seconds INTEGER DEFAULT 0,
    exit_reason TEXT,
    regime TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Failure patterns (from PG 068)
CREATE TABLE IF NOT EXISTS failure_patterns (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    skill TEXT NOT NULL,
    pattern_type TEXT NOT NULL,
    count INTEGER DEFAULT 1,
    last_seen DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    details TEXT
);

-- Strategy parameters (from PG 068)
CREATE TABLE IF NOT EXISTS strategy_parameters (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    strategy TEXT NOT NULL,
    parameter_name TEXT NOT NULL,
    parameter_value TEXT NOT NULL,
    regime TEXT,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(strategy, parameter_name, regime)
);

-- Order intents (from PG 072)
CREATE TABLE IF NOT EXISTS order_intents (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    intent_id TEXT UNIQUE NOT NULL,
    exchange TEXT NOT NULL,
    symbol TEXT NOT NULL,
    side TEXT NOT NULL,
    order_type TEXT NOT NULL,
    amount DECIMAL(20,8) NOT NULL,
    price DECIMAL(20,8),
    status TEXT DEFAULT 'pending',
    exchange_order_id TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Order audit log (from PG 072)
CREATE TABLE IF NOT EXISTS order_audit_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    intent_id TEXT,
    exchange TEXT NOT NULL,
    event_type TEXT NOT NULL,
    exchange_order_id TEXT,
    details TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Sentiment tables (from PG 068)
CREATE TABLE IF NOT EXISTS news_sentiment_sources (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    source_type TEXT NOT NULL,
    url TEXT,
    is_active INTEGER DEFAULT 1,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS news_sentiment (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    source_id INTEGER,
    symbol TEXT NOT NULL,
    title TEXT,
    sentiment_score DECIMAL(5,4),
    published_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS reddit_sentiment_sources (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    subreddit TEXT NOT NULL,
    is_active INTEGER DEFAULT 1,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS reddit_sentiment (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    source_id INTEGER,
    symbol TEXT NOT NULL,
    title TEXT,
    sentiment_score DECIMAL(5,4),
    score INTEGER DEFAULT 0,
    num_comments INTEGER DEFAULT 0,
    published_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS aggregated_sentiment (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    symbol TEXT NOT NULL,
    news_score DECIMAL(5,4) DEFAULT 0,
    reddit_score DECIMAL(5,4) DEFAULT 0,
    combined_score DECIMAL(5,4) DEFAULT 0,
    sample_count INTEGER DEFAULT 0,
    calculated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(symbol)
);
