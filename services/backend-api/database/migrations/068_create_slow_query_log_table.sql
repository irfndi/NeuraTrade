-- Migration: 068_create_slow_query_log_table.sql
-- Description: Create table for slow query logging and performance monitoring
-- Created: 2026-02-14

-- Table to store slow query events
CREATE TABLE IF NOT EXISTS slow_query_log (
    id BIGSERIAL PRIMARY KEY,
    query_text TEXT NOT NULL,
    query_hash VARCHAR(64),
    operation VARCHAR(20) NOT NULL,
    table_name VARCHAR(100),
    duration_ms INTEGER NOT NULL,
    rows_affected BIGINT DEFAULT 0,
    service_name VARCHAR(100) DEFAULT 'postgresql',
    context JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Index for efficient querying by created_at
CREATE INDEX IF NOT EXISTS idx_slow_query_log_created_at 
ON slow_query_log(created_at DESC);

-- Index for querying by duration
CREATE INDEX IF NOT EXISTS idx_slow_query_log_duration 
ON slow_query_log(duration_ms DESC);

-- Index for querying by table_name
CREATE INDEX IF NOT EXISTS idx_slow_query_log_table_name 
ON slow_query_log(table_name);

-- Index for querying by query_hash (for aggregation)
CREATE INDEX IF NOT EXISTS idx_slow_query_log_query_hash 
ON slow_query_log(query_hash);

-- Function to clean up old slow query logs (keep last 30 days)
CREATE OR REPLACE FUNCTION cleanup_slow_query_logs()
RETURNS void
LANGUAGE plpgsql
AS $$
BEGIN
    DELETE FROM slow_query_log 
    WHERE created_at < NOW() - INTERVAL '30 days';
END;
$$;

-- Comment for documentation
COMMENT ON TABLE slow_query_log IS 'Stores slow query events for performance monitoring and optimization';
COMMENT ON COLUMN slow_query_log.query_text IS 'The SQL query that was executed';
COMMENT ON COLUMN slow_query_log.query_hash IS 'Hash of the query for aggregation';
COMMENT ON COLUMN slow_query_log.operation IS 'SQL operation type (SELECT, INSERT, UPDATE, DELETE, etc.)';
COMMENT ON COLUMN slow_query_log.table_name IS 'Primary table involved in the query';
COMMENT ON COLUMN slow_query_log.duration_ms IS 'Query execution time in milliseconds';
COMMENT ON COLUMN slow_query_log.rows_affected IS 'Number of rows affected by the query';
COMMENT ON COLUMN slow_query_log.service_name IS 'Service that executed the query';
COMMENT ON COLUMN slow_query_log.context IS 'Additional context (parameters, user info, etc.)';
