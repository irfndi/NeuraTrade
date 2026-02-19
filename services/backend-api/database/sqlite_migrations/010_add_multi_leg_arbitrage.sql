-- Migration: 010_add_multi_leg_arbitrage.sql
-- Description: Adds tables to store multi-leg (triangular) arbitrage opportunities for SQLite
-- Created: 2026-02-18

-- Multi-leg arbitrage opportunities
CREATE TABLE IF NOT EXISTS multi_leg_opportunities (
    id TEXT PRIMARY KEY,
    exchange_id INTEGER NOT NULL,
    total_profit REAL,
    profit_percentage REAL NOT NULL,
    detected_at TEXT DEFAULT (datetime('now')),
    expires_at TEXT NOT NULL
);

-- Individual legs of a multi-leg opportunity
CREATE TABLE IF NOT EXISTS multi_leg_legs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    opportunity_id TEXT NOT NULL,
    leg_index INTEGER NOT NULL,
    symbol TEXT NOT NULL,
    side TEXT NOT NULL,
    price REAL NOT NULL,
    volume REAL,
    created_at TEXT DEFAULT (datetime('now')),
    FOREIGN KEY (opportunity_id) REFERENCES multi_leg_opportunities(id) ON DELETE CASCADE
);

-- Indexes for performance
CREATE INDEX IF NOT EXISTS idx_multi_leg_opp_expires ON multi_leg_opportunities(expires_at);
CREATE INDEX IF NOT EXISTS idx_multi_leg_legs_opportunity ON multi_leg_legs(opportunity_id);
