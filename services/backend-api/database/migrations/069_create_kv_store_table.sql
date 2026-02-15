-- Create kv_store table for key-value state persistence
-- Used by TradingStateStore for crash recovery

CREATE TABLE IF NOT EXISTS kv_store (
    key VARCHAR(255) PRIMARY KEY,
    value JSONB,
    updated_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_kv_store_updated_at ON kv_store(updated_at DESC);

-- Grant permissions
GRANT SELECT, INSERT, UPDATE, DELETE ON kv_store TO authenticated;
GRANT SELECT ON kv_store TO anon;

-- Add metadata table entry
INSERT INTO schema_metadata (key, value, description)
VALUES ('migration_069_completed', 'true', 'Migration 069: Create kv_store table')
ON CONFLICT (key) DO NOTHING;

-- Add to migration log
INSERT INTO migration_log (migration_number, migration_name, applied_at)
VALUES (69, '069_create_kv_store_table.sql', NOW())
ON CONFLICT (migration_number) DO NOTHING;
