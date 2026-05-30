-- Defensive migration: add any missing paper_trades columns for databases
-- created before the full schema was present in 000_sqlite_consolidated.sql.
-- SQLite ALTER TABLE fails if the column already exists; .bail off lets us
-- continue and the final SELECT ensures a successful exit code.

.bail off

ALTER TABLE paper_trades ADD COLUMN strategy_id TEXT NOT NULL DEFAULT '';
ALTER TABLE paper_trades ADD COLUMN quest_id INTEGER;
ALTER TABLE paper_trades ADD COLUMN exchange TEXT NOT NULL DEFAULT '';
ALTER TABLE paper_trades ADD COLUMN symbol TEXT NOT NULL DEFAULT '';
ALTER TABLE paper_trades ADD COLUMN side TEXT NOT NULL DEFAULT 'buy';
ALTER TABLE paper_trades ADD COLUMN size DECIMAL(20, 8) NOT NULL DEFAULT 0;
ALTER TABLE paper_trades ADD COLUMN fees DECIMAL(20, 8) NOT NULL DEFAULT 0;
ALTER TABLE paper_trades ADD COLUMN cost_basis DECIMAL(20, 8) NOT NULL DEFAULT 0;
ALTER TABLE paper_trades ADD COLUMN updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP;

-- Ensure the migration ends with a successful statement
SELECT 1;
