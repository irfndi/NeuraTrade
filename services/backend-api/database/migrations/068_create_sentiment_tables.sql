-- Migration: 068_create_sentiment_tables.sql
-- Description: Create tables for news and reddit sentiment integration
-- Tasks: neura-9zc2 (News Sentiment Feed), neura-wjr8 (Reddit Sentiment Integration)

-- Table for news sentiment sources (e.g., CryptoPanic)
CREATE TABLE IF NOT EXISTS news_sentiment_sources (
    id SERIAL PRIMARY KEY,
    source_name VARCHAR(100) NOT NULL UNIQUE,
    source_type VARCHAR(50) NOT NULL, -- 'cryptopanic', 'newsapi', 'rss', etc.
    base_url VARCHAR(500) NOT NULL,
    api_key_secret_name VARCHAR(100), -- Reference to secret manager
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Table for stored news sentiment articles
CREATE TABLE IF NOT EXISTS news_sentiment (
    id SERIAL PRIMARY KEY,
    source_id INTEGER REFERENCES news_sentiment_sources(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    url TEXT NOT NULL UNIQUE,
    published_at TIMESTAMP WITH TIME ZONE,
    sentiment_score DECIMAL(5,4), -- -1.0 to 1.0
    sentiment_label VARCHAR(20), -- 'bullish', 'bearish', 'neutral'
    symbols JSONB DEFAULT '[]', -- List of crypto symbols mentioned
    metadata JSONB DEFAULT '{}',
    fetched_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX idx_news_sentiment_fetched_at ON news_sentiment(fetched_at DESC);
CREATE INDEX idx_news_sentiment_sentiment ON news_sentiment(sentiment_score);
CREATE INDEX idx_news_sentiment_symbols ON news_sentiment USING GIN(symbols);

-- Table for Reddit sentiment sources
CREATE TABLE IF NOT EXISTS reddit_sentiment_sources (
    id SERIAL PRIMARY KEY,
    subreddit VARCHAR(100) NOT NULL UNIQUE,
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Table for stored Reddit sentiment data
CREATE TABLE IF NOT EXISTS reddit_sentiment (
    id SERIAL PRIMARY KEY,
    source_id INTEGER REFERENCES reddit_sentiment_sources(id) ON DELETE CASCADE,
    post_id VARCHAR(50) NOT NULL UNIQUE,
    title TEXT NOT NULL,
    url TEXT NOT NULL,
    author VARCHAR(100),
    score INTEGER DEFAULT 0,
    num_comments INTEGER DEFAULT 0,
    sentiment_score DECIMAL(5,4), -- -1.0 to 1.0
    sentiment_label VARCHAR(20), -- 'bullish', 'bearish', 'neutral'
    symbols JSONB DEFAULT '[]', -- List of crypto symbols mentioned
    metadata JSONB DEFAULT '{}',
    fetched_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX idx_reddit_sentiment_fetched_at ON reddit_sentiment(fetched_at DESC);
CREATE INDEX idx_reddit_sentiment_sentiment ON reddit_sentiment(sentiment_score);
CREATE INDEX idx_reddit_sentiment_subreddit ON reddit_sentiment(source_id, fetched_at DESC);

-- Table for aggregated sentiment (combined news + reddit)
CREATE TABLE IF NOT EXISTS aggregated_sentiment (
    id SERIAL PRIMARY KEY,
    symbol VARCHAR(20) NOT NULL,
    sentiment_source VARCHAR(50) NOT NULL, -- 'news', 'reddit', 'combined'
    sentiment_score DECIMAL(5,4), -- -1.0 to 1.0
    bullish_ratio DECIMAL(5,4), -- Ratio of bullish to total
    total_mentions INTEGER DEFAULT 0,
    sample_size INTEGER DEFAULT 0,
    computed_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE(symbol, sentiment_source)
);

CREATE INDEX idx_aggregated_sentiment_symbol ON aggregated_sentiment(symbol);
CREATE INDEX idx_aggregated_sentiment_computed_at ON aggregated_sentiment(computed_at DESC);

-- Insert default sources
INSERT INTO news_sentiment_sources (source_name, source_type, base_url, is_active) 
VALUES 
    ('cryptopanic', 'cryptopanic', 'https://cryptopanic.com/api/v1/', true)
ON CONFLICT (source_name) DO NOTHING;

INSERT INTO reddit_sentiment_sources (subreddit, is_active)
VALUES 
    ('Cryptocurrency', true),
    ('Bitcoin', true),
    ('ethereum', true),
    ('SOLCrypto', true)
ON CONFLICT (subreddit) DO NOTHING;
