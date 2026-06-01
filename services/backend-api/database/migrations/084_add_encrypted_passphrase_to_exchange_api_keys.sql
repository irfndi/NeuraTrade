-- Migration 084: Add encrypted_passphrase column to exchange_api_keys
ALTER TABLE exchange_api_keys ADD COLUMN IF NOT EXISTS encrypted_passphrase TEXT NOT NULL DEFAULT '';
