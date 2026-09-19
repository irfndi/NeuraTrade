-- P3 storage cutover, migration 001: transactional state (Postgres).
-- Vendor-neutral SQL (Neon / PlanetScale Postgres / self-hosted volume —
-- owner picked per GO-all; same DDL everywhere). Mirrors nt-ledger::Totals
-- (fills/gross/fees) and nt-execution::Fill field-for-field so the Rust
-- shadow can dual-write with no translation. Integers only: micro-USDT
-- BIGINT (no float anywhere, P0 money rule). Additive: CREATE IF NOT EXISTS
-- only; safe to re-run. Source of truth: docs/plans/storage-cutover.md.
-- SQLite stays readable until reads cut; Parquet owns candles (not here).

CREATE TABLE IF NOT EXISTS fills (
    id               BIGSERIAL PRIMARY KEY,
    ts               TIMESTAMPTZ NOT NULL DEFAULT now(),
    source           TEXT NOT NULL,          -- paper | demo | rust-shadow
    symbol           TEXT NOT NULL,
    qty_base_micros  BIGINT NOT NULL,        -- signed; buys positive
    price_micros     BIGINT NOT NULL,        -- micro-USDT
    fee_micros       BIGINT NOT NULL,
    proceeds_micros  BIGINT NOT NULL         -- signed pre-fee cash flow
);
CREATE INDEX IF NOT EXISTS fills_symbol_ts ON fills (symbol, ts);

CREATE TABLE IF NOT EXISTS positions (
    symbol           TEXT PRIMARY KEY,
    source           TEXT NOT NULL,
    qty_base_micros  BIGINT NOT NULL,
    avg_price_micros BIGINT NOT NULL,
    realized_micros  BIGINT NOT NULL DEFAULT 0,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS equity_snapshots (
    ts               TIMESTAMPTZ NOT NULL,
    source           TEXT NOT NULL,          -- paper | demo | rust-shadow
    capital_micros   BIGINT NOT NULL,
    equity_micros    BIGINT NOT NULL,
    open_count       INT NOT NULL DEFAULT 0,
    PRIMARY KEY (ts, source)
);

CREATE TABLE IF NOT EXISTS kill_switch (
    id       INT PRIMARY KEY DEFAULT 1,
    engaged  BOOLEAN NOT NULL,
    reason   TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
