PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS semantic_memory (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    symbol TEXT NOT NULL,
    timeframe TEXT,
    strategy_lane TEXT,
    feature_hash TEXT,
    embedding BLOB NOT NULL,
    metadata_json TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_semantic_memory_symbol_time ON semantic_memory(symbol, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_semantic_memory_feature_hash ON semantic_memory(feature_hash);
