-- Migration 087: Add unique index on market_data to prevent duplicate OHLCV inserts
-- Created: 2026-06-01
-- Addresses GitHub #311 / NeuraTrade-cpv4: OHLCV collection interval (30s)
-- shorter than candle timeframe (1m) produces duplicate candles. Adding a
-- unique constraint on (exchange_id, trading_pair_id, timestamp) makes
-- re-runs of backfill safe and dedup-on-insert the new norm.

PRAGMA foreign_keys = ON;

CREATE UNIQUE INDEX IF NOT EXISTS uq_market_data_exchange_pair_timestamp
    ON market_data(exchange_id, trading_pair_id, timestamp);
