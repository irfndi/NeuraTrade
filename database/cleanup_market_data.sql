-- NeuraTrade SQLite Database Cleanup Script
-- Run periodically to clean up old market data and maintain database size
-- Usage: sqlite3 data/neuratrade.db < cleanup_market_data.sql

-- Enable foreign keys
PRAGMA foreign_keys = ON;

-- ========================================
-- Market Data Cleanup
-- ========================================

-- Delete market data older than 30 days (adjust as needed)
DELETE FROM market_data WHERE created_at < datetime('now', '-30 days');

-- Delete OHLCV data older than 90 days
DELETE FROM ohlcv_data WHERE timestamp < datetime('now', '-90 days');

-- Delete old arbitrage opportunities (already expired)
DELETE FROM arbitrage_opportunities
WHERE expires_at < datetime('now', '-7 days')
  AND status != 'executed';

-- ========================================
-- Signal Data Cleanup
-- ========================================

-- Delete old signals (keep last 1000)
DELETE FROM signals
WHERE rowid NOT IN (
  SELECT rowid FROM signals
  ORDER BY created_at DESC
  LIMIT 1000
);

-- Delete processed signals older than 30 days
DELETE FROM signals
WHERE status = 'processed'
  AND created_at < datetime('now', '-30 days');

-- ========================================
-- Session/Cache Cleanup
-- ========================================

-- Delete expired sessions
DELETE FROM user_sessions WHERE expires_at < datetime('now');

-- Delete old cache entries
DELETE FROM cache_entries WHERE expires_at < datetime('now');

-- ========================================
-- Trade/Quest Cleanup
-- ========================================

-- Archive closed trades older than 1 year (optional: move to archive table first)
-- DELETE FROM trades WHERE status = 'closed' AND closed_at < datetime('now', '-1 year');

-- Delete completed quests older than 90 days
DELETE FROM quests
WHERE status = 'completed'
  AND completed_at < datetime('now', '-90 days');

-- ========================================
-- Database Maintenance
-- ========================================

-- Vacuum database to reclaim space
VACUUM;

-- Analyze tables for query optimization
ANALYZE;

-- Check database integrity
PRAGMA integrity_check;

-- Log cleanup completion
INSERT INTO cleanup_logs (cleanup_type, cleaned_at, notes)
VALUES ('scheduled_cleanup', datetime('now'), 'Automated cleanup completed');
