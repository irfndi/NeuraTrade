PRAGMA foreign_keys = ON;

-- Add expires_at column to exchange_blacklist
ALTER TABLE exchange_blacklist ADD COLUMN expires_at DATETIME;
