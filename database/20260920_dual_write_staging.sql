-- P3 storage cutover, migration 002: dual-write staging (Postgres, additive only).
-- Cutover: (1) deploy this staging DDL.
-- Cutover: (2) dual-write TS soak fills/positions, feature-flagged.
-- Cutover: (3) reconcile staging vs SQLite.
-- Cutover: (4) cut reads to Postgres.
-- Cutover: (5) freeze SQLite writers, bake-off restarts.
-- Vendor-neutral SQL, same DDL everywhere. Mirrors docs/plans/storage-cutover.md fills/positions
-- field-for-field (micro-USDT BIGINT, no float, P0 money rule) plus staging extras only.
-- Additive: CREATE IF NOT EXISTS only; safe to re-run; touches no existing tables.
-- Source of truth: docs/plans/storage-cutover.md. Style ref: database/20260919_rust_shadow_ledger.sql.

CREATE TABLE IF NOT EXISTS fills_staging (
    id               BIGSERIAL PRIMARY KEY,
    ts               TIMESTAMPTZ NOT NULL DEFAULT now(),
    source           TEXT NOT NULL,          -- paper | demo | rust-shadow
    symbol           TEXT NOT NULL,
    qty_base_micros  BIGINT NOT NULL,        -- signed; buys positive
    price_micros     BIGINT NOT NULL,        -- micro-USDT
    fee_micros       BIGINT NOT NULL,
    proceeds_micros  BIGINT NOT NULL,        -- signed pre-fee cash flow
    panel_hash       TEXT,                   -- optional research-panel linkage, nullable until writers send it
    ingested_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS fills_staging_symbol_ts ON fills_staging (symbol, ts);

CREATE TABLE IF NOT EXISTS positions_staging (
    symbol           TEXT PRIMARY KEY,
    source           TEXT NOT NULL,
    qty_base_micros  BIGINT NOT NULL,
    avg_price_micros BIGINT NOT NULL,
    realized_micros  BIGINT NOT NULL DEFAULT 0,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
