import { describe, expect, it } from "bun:test";
import { Database } from "bun:sqlite";
import { Effect } from "effect";
import { money } from "../../src/utils/money.js";
import { PaperTradingRepositorySQLite } from "../../src/paper-trading/repository.js";

describe("real-money readiness SQLite integration", () => {
  it("adds readiness columns when a writer opens a legacy grid schema", async () => {
    const db = new Database(":memory:");
    db.exec(`
      CREATE TABLE grid_paper_trades (
        id TEXT PRIMARY KEY, exchange TEXT NOT NULL, symbol TEXT NOT NULL,
        timeframe TEXT NOT NULL, side TEXT NOT NULL,
        entry_price REAL NOT NULL, exit_price REAL NOT NULL,
        capital_before REAL NOT NULL, capital_after REAL NOT NULL,
        pnl_pct REAL NOT NULL, exit_reason TEXT NOT NULL,
        opened_at DATETIME NOT NULL, closed_at DATETIME NOT NULL
      );
    `);
    const repository = new PaperTradingRepositorySQLite(db);

    await Effect.runPromise(repository.ensureTables());

    const columns = new Set(
      (
        db.query("PRAGMA table_info(grid_paper_trades)").all() as Array<{
          readonly name: string;
        }>
      ).map((column) => column.name),
    );
    expect([...columns]).toEqual(
      expect.arrayContaining([
        "strategy_config_fingerprint",
        "cohort_id",
        "candidate_lock_at",
        "dataset_cutoff_at",
        "entry_opened_at",
        "execution_environment",
      ]),
    );
    db.close();
  });

  it("persists immutable entry provenance through a real SQLite schema", async () => {
    const db = new Database(":memory:");
    const repository = new PaperTradingRepositorySQLite(db);
    const trade = {
      id: "integration-provenance-1",
      exchange: "bybit-futures",
      symbol: "BTC/USDT:USDT",
      timeframe: "15m",
      side: "long" as const,
      entryPrice: money("70000"),
      exitPrice: money("70100"),
      capitalBefore: money("1000"),
      capitalAfter: money("1001"),
      pnlPct: money("0.1"),
      exitReason: "target" as const,
      openedAt: new Date("2026-08-01T00:00:00.000Z"),
      closedAt: new Date("2026-08-01T01:00:00.000Z"),
      fillSource: "live" as const,
      entryOrderId: "entry-1",
      exitOrderId: "exit-1",
      entryFilledQty: money("0.01"),
      exitFilledQty: money("0.01"),
      entryFee: money("0.1"),
      exitFee: money("0.1"),
      realizedPnlPct: money("0.1"),
      strategyConfigFingerprint: "b".repeat(64),
      cohortId: "cohort-integration",
      candidateLockAt: new Date("2026-07-01T00:00:00.000Z"),
      datasetCutoffAt: new Date("2026-07-31T23:45:00.000Z"),
      entryOpenedAt: new Date("2026-08-01T00:00:00.000Z"),
      executionEnvironment: "bybit-demo" as const,
    };
    await Effect.runPromise(repository.ensureTables());
    await Effect.runPromise(repository.recordGridTrade(trade));
    const rows = await Effect.runPromise(
      repository.listRecentGridTrades(
        trade.exchange,
        trade.symbol,
        trade.timeframe,
        1,
      ),
    );

    expect(rows[0]?.strategyConfigFingerprint).toBe(
      trade.strategyConfigFingerprint,
    );
    expect(rows[0]?.cohortId).toBe(trade.cohortId);
    expect(rows[0]?.entryOpenedAt?.toISOString()).toBe(
      trade.entryOpenedAt.toISOString(),
    );
    expect(rows[0]?.executionEnvironment).toBe("bybit-demo");
    db.close();
  });

  it("preserves provenance fields through a grid-state round trip", async () => {
    const db = new Database(":memory:");
    const repository = new PaperTradingRepositorySQLite(db);
    await Effect.runPromise(repository.ensureTables());
    const state = {
      exchange: "bybit-futures",
      symbol: "BTC/USDT:USDT",
      timeframe: "15m",
      capital: money("100"),
      peakCapital: money("105"),
      paused: 0,
      side: null,
      entryPrice: money("0"),
      strategyConfigFingerprint: "c".repeat(64),
      cohortId: "cohort-roundtrip",
      candidateLockAt: new Date("2026-07-01T00:00:00.000Z"),
      datasetCutoffAt: new Date("2026-07-31T23:45:00.000Z"),
      entryOpenedAt: new Date("2026-08-01T00:00:00.000Z"),
      executionEnvironment: "bybit-demo" as const,
      gridStepPct: 1,
      gridMaxGrids: 2,
      gridPauseAfterLossBars: 0,
      feePct: 0.06,
      slippageBps: 2,
      trendFilterPeriod: 0,
      maxPositionPct: 50,
      maxDrawdownPct: 5,
      leverage: 1,
      killed: false,
      lastTimestamp: new Date("2026-08-01T00:00:00.000Z"),
      updatedAt: new Date("2026-08-01T00:00:00.000Z"),
    };
    await Effect.runPromise(repository.saveGridState(state));
    const loaded = await Effect.runPromise(
      repository.getGridState("bybit-futures", "BTC/USDT:USDT", "15m"),
    );

    expect(loaded?.strategyConfigFingerprint).toBe("c".repeat(64));
    expect(loaded?.cohortId).toBe("cohort-roundtrip");
    expect(loaded?.candidateLockAt?.toISOString()).toBe(
      "2026-07-01T00:00:00.000Z",
    );
    expect(loaded?.datasetCutoffAt?.toISOString()).toBe(
      "2026-07-31T23:45:00.000Z",
    );
    expect(loaded?.entryOpenedAt?.toISOString()).toBe(
      "2026-08-01T00:00:00.000Z",
    );
    expect(loaded?.executionEnvironment).toBe("bybit-demo");
    db.close();
  });

  it("rejects a trade record that drops required provenance columns", async () => {
    const db = new Database(":memory:");
    // Complete base schema (all money/state columns) but WITHOUT the
    // provenance columns (fill_source, strategy_config_fingerprint, cohort_id,
    // candidate_lock_at, dataset_cutoff_at, entry_opened_at,
    // execution_environment). ensureTables() must succeed (backfill only needs
    // the base money columns), while recordGridTrade must fail closed because
    // the provenance columns are absent — proving no fill can enter the cohort
    // unprovenanced.
    db.exec(`
      CREATE TABLE grid_paper_trades (
        id TEXT PRIMARY KEY, exchange TEXT NOT NULL, symbol TEXT NOT NULL,
        timeframe TEXT NOT NULL, side TEXT NOT NULL,
        entry_price REAL NOT NULL, exit_price REAL NOT NULL,
        capital_before REAL NOT NULL, capital_after REAL NOT NULL,
        pnl_pct REAL NOT NULL,
        entry_price_decimal TEXT, exit_price_decimal TEXT,
        capital_before_decimal TEXT, capital_after_decimal TEXT,
        pnl_pct_decimal TEXT,
        entry_order_id TEXT, entry_client_oid TEXT,
        exit_order_id TEXT, exit_client_oid TEXT,
        entry_filled_qty REAL, entry_filled_qty_decimal TEXT,
        exit_filled_qty REAL, exit_filled_qty_decimal TEXT,
        entry_fee REAL, entry_fee_decimal TEXT,
        exit_fee REAL, exit_fee_decimal TEXT,
        realized_pnl_pct REAL, realized_pnl_pct_decimal TEXT,
        exit_reason TEXT NOT NULL, opened_at DATETIME NOT NULL, closed_at DATETIME NOT NULL
      );
    `);
    const repository = new PaperTradingRepositorySQLite(db);
    await Effect.runPromise(repository.ensureTables());
    const trade = {
      id: "integration-provenance-missing",
      exchange: "bybit-futures",
      symbol: "BTC/USDT:USDT",
      timeframe: "15m",
      side: "long" as const,
      entryPrice: money("70000"),
      exitPrice: money("70100"),
      capitalBefore: money("1000"),
      capitalAfter: money("1001"),
      pnlPct: money("0.1"),
      exitReason: "target" as const,
      openedAt: new Date("2026-08-01T00:00:00.000Z"),
      closedAt: new Date("2026-08-01T01:00:00.000Z"),
      fillSource: "live" as const,
      entryOrderId: "entry-1",
      exitOrderId: "exit-1",
      entryFilledQty: money("0.01"),
      exitFilledQty: money("0.01"),
      entryFee: money("0.1"),
      exitFee: money("0.1"),
      realizedPnlPct: money("0.1"),
    };
    const outcome = await Effect.runPromise(
      repository.recordGridTrade(trade).pipe(Effect.exit),
    );
    // The writer fails closed: it cannot record a demo trade without the
    // provenance columns, so no fill can enter the cohort unprovenanced.
    expect(outcome._tag).toBe("Failure");
    db.close();
  });
});
