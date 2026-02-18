PRAGMA foreign_keys = ON;

-- Add is_active column to exchange_blacklist
ALTER TABLE exchange_blacklist ADD COLUMN is_active INTEGER DEFAULT 1;
