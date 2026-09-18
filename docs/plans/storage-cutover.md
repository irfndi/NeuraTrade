# Storage cutover — Postgres (ledger) + Parquet (candles)

Draft for P3 (plan Task 5). Vendor still an owner call (managed vs second
volume); schema below is vendor-neutral Postgres. SQLite stays readable until
reads cut; the 9.4 GB monolith is never extended with new writers.

**Considered and rejected for the ledger role (2026-09-18):**
`rusqlite` and `tursodatabase/turso` (SQLite rewritten in Rust, formerly
Limbo). `rusqlite` is bindings to real SQLite — it does not fix this repo's
actual pain point (the single-writer bottleneck starving paper/demo soaks
under concurrent research writers), so it solves nothing Postgres doesn't
already solve better here. `turso` is more interesting — pure Rust (fits
"Rust owns everything"), and now ships real concurrent writes (MVCC +
row-level conflict detection, preview since Aug 2026) — but its own
maintainers describe it as beta and explicitly not yet at "SQLite-level
reliability," caution advised for anything mission-critical. The `fills`/
`positions`/`kill_switch` tables below are exactly that (real money math,
P0 no-float rule) — not the place to carry beta-database risk. Postgres
stays the ledger store.

`turso` IS worth a second look for the **candle/research** role below, as an
alternative to raw Parquet — SQL-queryable, concurrent-safe, no separate
DuckDB hop, same low-stakes / not-money-critical profile Parquet already
has. Not decided; flagging for whoever picks up Task 5 Step 2+.

## Postgres — transactional state only

```sql
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
```

Mirrors `nt-ledger::Totals` (fills/fees/net) and `nt-execution::Fill`
field-for-field so the Rust shadow can dual-write with no translation.

## Parquet — candles / research panels

- One dataset per `(exchange, symbol, timeframe)`, partitioned by month.
- Columns: `open_ts_ms INT64, open/high/low/close INT64 (micro-USDT),
  volume_base_micros INT64`.
- Writers: candle-sync job (replaces SQLite candle growth); readers:
  autoresearch + Bend kernel via DuckDB or native readers.
- SQLite keeps no candles after cutover — ephemeral cache only.

## Cutover order (matches plan)

1. Dual-write fills/positions from TS soak (feature-flagged).
2. Backfill candles to Parquet; point readers at Parquet.
3. Cut reads; freeze SQLite; drop the same-host `.bak` policy.
