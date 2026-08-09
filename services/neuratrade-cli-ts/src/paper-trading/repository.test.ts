import { describe, expect, it } from "bun:test";
import { Database } from "bun:sqlite";
import { Effect } from "effect";
import { money } from "../utils/money.js";
import { PaperTradingRepositorySQLite } from "./repository.js";
import type { WatchlistEntry } from "./repository.js";
import type { GridPaperState, GridPaperTrade, PaperPosition } from "./types.js";

describe("PaperTradingRepositorySQLite", () => {
  it("round-trips monetary values without SQLite REAL precision loss", async () => {
    const db = new Database(":memory:");
    const repository = new PaperTradingRepositorySQLite(db);
    const capital = money("0.123456789012345678901234567890");
    const peakCapital = money("9876543210.12345678901234567890");

    await Effect.runPromise(repository.ensureTables());
    await Effect.runPromise(repository.setPortfolio(capital, peakCapital));
    const portfolio = await Effect.runPromise(repository.getPortfolio());

    expect(portfolio.capital.toString()).toBe(capital.toString());
    expect(portfolio.peakCapital.toString()).toBe(peakCapital.toString());
    db.close();
  });

  it("round-trips grid state including the persisted initial capital", async () => {
    const db = new Database(":memory:");
    const repository = new PaperTradingRepositorySQLite(db);
    const state: GridPaperState = {
      exchange: "binance",
      symbol: "ETH/USDT",
      timeframe: "15m",
      initialCapital: 50,
      capital: money(333),
      peakCapital: money(400),
      paused: 0,
      side: null,
      entryPrice: money(0),
      gridStepPct: 1,
      gridMaxGrids: 2,
      gridPauseAfterLossBars: 0,
      feePct: 0.2,
      slippageBps: 5,
      trendFilterPeriod: 10,
      maxPositionPct: 100,
      maxDrawdownPct: 100,
      leverage: 1,
      killed: false,
      lastTimestamp: null,
      updatedAt: new Date("2026-08-01T00:00:00.000Z"),
    };

    await Effect.runPromise(repository.ensureTables());
    await Effect.runPromise(repository.saveGridState(state));
    const loaded = await Effect.runPromise(
      repository.getGridState("binance", "ETH/USDT", "15m"),
    );

    expect(loaded?.initialCapital).toBe(50);
    expect(loaded?.capital.toString()).toBe(state.capital.toString());
    expect(loaded?.gridStepPct).toBe(1);
    db.close();
  });

  it("round-trips positions and closed trades with exact decimal columns", async () => {
    const db = new Database(":memory:");
    const repository = new PaperTradingRepositorySQLite(db);
    const openedAt = new Date("2026-08-01T00:00:00.000Z");
    const position: PaperPosition = {
      id: "position-1",
      exchange: "bitget-futures",
      symbol: "BTC/USDT:USDT",
      timeframe: "1m",
      side: "long",
      entryPrice: money("70000.12345678901234567890"),
      size: money("0.00012345678901234567890"),
      stopLoss: money("69000.00000000000000000001"),
      takeProfit: money("71000.99999999999999999999"),
      openedAt,
      signalId: "signal-1",
      scaledOut: false,
      scaleOutPrice: money(0),
    };

    await Effect.runPromise(repository.ensureTables());
    await Effect.runPromise(repository.saveOpenPosition(position));
    const loaded = await Effect.runPromise(
      repository.getOpenPosition(position.exchange, position.symbol),
    );

    expect(loaded?.entryPrice.toString()).toBe(position.entryPrice.toString());
    expect(loaded?.size.toString()).toBe(position.size.toString());
    expect(loaded?.stopLoss.toString()).toBe(position.stopLoss.toString());
    expect(loaded?.takeProfit.toString()).toBe(position.takeProfit.toString());

    const trade = await Effect.runPromise(
      repository.closePosition(
        position,
        money("70500.98765432109876543210"),
        "take_profit",
        new Date("2026-08-01T00:01:00.000Z"),
      ),
    );
    const trades = await Effect.runPromise(repository.listRecentTrades(1));

    expect(trade.pnl.toString()).toBe("0.061835085558556622408");
    expect(trades[0]?.pnl.toString()).toBe(trade.pnl.toString());
    expect(
      await Effect.runPromise(
        repository.getOpenPosition(position.exchange, position.symbol),
      ),
    ).toBeNull();
    db.close();
  });

  it("round-trips live grid fill evidence", async () => {
    const db = new Database(":memory:");
    const repository = new PaperTradingRepositorySQLite(db);
    const trade: GridPaperTrade = {
      id: "grid-live-1",
      exchange: "bitget-futures",
      symbol: "BTC/USDT:USDT",
      timeframe: "15m",
      side: "long",
      entryPrice: money("70000.12345678901234567890"),
      exitPrice: money("70100.98765432109876543210"),
      capitalBefore: money("1000.12345678901234567890"),
      capitalAfter: money("1002.12345678901234567890"),
      pnlPct: money("0.2"),
      exitReason: "target",
      openedAt: new Date("2026-08-01T00:00:00.000Z"),
      closedAt: new Date("2026-08-01T01:00:00.000Z"),
      fillSource: "live",
      entryOrderId: "entry-1",
      entryClientOid: "client-entry-1",
      exitOrderId: "exit-1",
      exitClientOid: "client-exit-1",
      entryFilledQty: money("0.01234567890123456789"),
      exitFilledQty: money("0.01234567890123456789"),
      entryFee: money("0.12345678901234567890"),
      exitFee: money("0.23456789012345678901"),
      realizedPnlPct: money("0.164321"),
      strategyConfigFingerprint: "a".repeat(64),
      cohortId: "cohort-1",
      candidateLockAt: new Date("2026-07-01T00:00:00.000Z"),
      datasetCutoffAt: new Date("2026-07-31T23:45:00.000Z"),
      entryOpenedAt: new Date("2026-08-01T00:00:00.000Z"),
      executionEnvironment: "bitget-demo",
    };

    await Effect.runPromise(repository.ensureTables());
    await Effect.runPromise(repository.recordGridTrade(trade));
    const loaded = await Effect.runPromise(
      repository.listRecentGridTrades(
        trade.exchange,
        trade.symbol,
        trade.timeframe,
        1,
      ),
    );

    expect(loaded[0]?.fillSource).toBe("live");
    expect(loaded[0]?.entryOrderId).toBe(trade.entryOrderId);
    expect(loaded[0]?.exitOrderId).toBe(trade.exitOrderId);
    expect(loaded[0]?.entryFilledQty?.toString()).toBe(
      trade.entryFilledQty?.toString(),
    );
    expect(loaded[0]?.exitFee?.toString()).toBe(trade.exitFee?.toString());
    expect(loaded[0]?.realizedPnlPct?.toString()).toBe(
      trade.realizedPnlPct?.toString(),
    );
    expect(loaded[0]?.strategyConfigFingerprint).toBe(
      trade.strategyConfigFingerprint,
    );
    expect(loaded[0]?.cohortId).toBe(trade.cohortId);
    expect(loaded[0]?.candidateLockAt?.toISOString()).toBe(
      trade.candidateLockAt?.toISOString(),
    );
    expect(loaded[0]?.datasetCutoffAt?.toISOString()).toBe(
      trade.datasetCutoffAt?.toISOString(),
    );
    expect(loaded[0]?.entryOpenedAt?.toISOString()).toBe(
      trade.entryOpenedAt?.toISOString(),
    );
    expect(loaded[0]?.executionEnvironment).toBe(trade.executionEnvironment);
    db.close();
  });

  it("includes closed grid trades in daily risk accounting", async () => {
    const db = new Database(":memory:");
    const repository = new PaperTradingRepositorySQLite(db);
    const closedAt = new Date();
    const trade: GridPaperTrade = {
      id: "grid-risk-1",
      exchange: "bitget-futures",
      symbol: "BTC/USDT:USDT",
      timeframe: "15m",
      side: "long",
      entryPrice: money("70000"),
      exitPrice: money("70100"),
      capitalBefore: money("1000"),
      capitalAfter: money("1002"),
      pnlPct: money("0.2"),
      exitReason: "target",
      openedAt: new Date(closedAt.getTime() - 3_600_000),
      closedAt,
    };

    await Effect.runPromise(repository.ensureTables());
    await Effect.runPromise(repository.recordGridTrade(trade));

    expect(
      await Effect.runPromise(repository.countTradesForDate(closedAt)),
    ).toBe(1);
    expect(
      (await Effect.runPromise(repository.getTodayRealizedPnl())).toString(),
    ).toBe("2");
    db.close();
  });

  it("migrates scaled_out/scale_out_price columns onto an existing positions table", async () => {
    // Simulate a DB created before the scale-out columns existed: the
    // paper_positions table is present but lacks scaled_out / scale_out_price.
    // ensureTables must ALTER TABLE to add them, or getOpenPosition /
    // saveOpenPosition fail with "no such column".
    const db = new Database(":memory:");
    db.exec(`
      CREATE TABLE paper_positions (
        id TEXT PRIMARY KEY,
        exchange TEXT NOT NULL,
        symbol TEXT NOT NULL,
        timeframe TEXT NOT NULL,
        side TEXT NOT NULL,
        entry_price REAL NOT NULL,
        size REAL NOT NULL,
        stop_loss REAL NOT NULL,
        take_profit REAL NOT NULL,
        opened_at TEXT NOT NULL,
        signal_id TEXT
      );
    `);
    const repository = new PaperTradingRepositorySQLite(db);

    await Effect.runPromise(repository.ensureTables());

    const position: PaperPosition = {
      id: "migrated-1",
      exchange: "bitget-futures",
      symbol: "BTC/USDT:USDT",
      timeframe: "1m",
      side: "long",
      entryPrice: money("70000"),
      size: money("0.001"),
      stopLoss: money("69000"),
      takeProfit: money("71000"),
      openedAt: new Date("2026-08-01T00:00:00.000Z"),
      signalId: "signal-1",
      scaledOut: true,
      scaleOutPrice: money("70500"),
    };

    await Effect.runPromise(repository.saveOpenPosition(position));
    const loaded = await Effect.runPromise(
      repository.getOpenPosition(position.exchange, position.symbol),
    );
    expect(loaded?.scaledOut).toBe(true);
    expect(loaded?.scaleOutPrice.toString()).toBe("70500");
    db.close();
  });

  it("upsertWatchlist writes entry.updatedAt, not the wall clock", async () => {
    const db = new Database(":memory:");
    const repository = new PaperTradingRepositorySQLite(db);
    await Effect.runPromise(repository.ensureTables());

    const legacyUpdatedAt = new Date("2026-07-01T00:00:00.000Z");
    const freshUpdatedAt = new Date("2026-08-01T00:00:00.000Z");
    const entry = {
      exchange: "bitget-futures",
      symbol: "BTC/USDT:USDT",
      timeframe: "15m",
      returnPct: 12.5,
      profitableWindowsPct: 80,
      aggregateReturnPct: 5.2,
      gridStepPct: 1,
      gridMaxGrids: 2,
      gridPauseAfterLossBars: 0,
      targetRatio: 1,
      chopGateAdx: 0,
      oosTrades: 0,
      fillsPerDay: 0,
      edgePerTradePct: 0,
      volatility: 0,
      allocatedWeight: 0,
      updatedAt: legacyUpdatedAt,
    };

    await Effect.runPromise(repository.upsertWatchlist([entry]));
    const first = await Effect.runPromise(
      repository.listWatchlist(entry.exchange, entry.timeframe),
    );
    expect(first[0]?.updatedAt.toISOString()).toBe(
      legacyUpdatedAt.toISOString(),
    );

    // Re-upsert with a caller-supplied newer timestamp; the persisted value
    // must reflect the entry, not `new Date()` at write time.
    await Effect.runPromise(
      repository.upsertWatchlist([
        { ...entry, returnPct: 99.9, updatedAt: freshUpdatedAt },
      ]),
    );
    const second = await Effect.runPromise(
      repository.listWatchlist(entry.exchange, entry.timeframe),
    );
    expect(second).toHaveLength(1);
    expect(second[0]?.returnPct).toBe(99.9);
    expect(second[0]?.updatedAt.toISOString()).toBe(
      freshUpdatedAt.toISOString(),
    );
    db.close();
  });

  it("migrates the watchlist contract columns onto a legacy watchlist table", async () => {
    // Simulate a DB created before the universe-funnel design: the
    // watchlist table exists but lacks the contract columns. CREATE TABLE
    // IF NOT EXISTS cannot add them, so ensureTables must ALTER TABLE, and
    // the migration must be safe to re-run.
    const db = new Database(":memory:");
    db.exec(`
      CREATE TABLE watchlist (
        exchange TEXT NOT NULL,
        symbol TEXT NOT NULL,
        timeframe TEXT NOT NULL,
        return_pct REAL NOT NULL,
        profitable_windows_pct REAL NOT NULL,
        aggregate_return_pct REAL NOT NULL,
        grid_step_pct REAL NOT NULL,
        grid_max_grids INTEGER NOT NULL,
        grid_pause_after_loss_bars INTEGER NOT NULL,
        updated_at TEXT NOT NULL,
        PRIMARY KEY (exchange, symbol, timeframe)
      );
      INSERT INTO watchlist
        (exchange, symbol, timeframe, return_pct, profitable_windows_pct,
         aggregate_return_pct, grid_step_pct, grid_max_grids,
         grid_pause_after_loss_bars, updated_at)
      VALUES
        ('bitget-futures', 'BTC/USDT:USDT', '15m', 10, 70, 3, 1, 2, 0,
         '2026-07-01T00:00:00.000Z');
    `);
    const repository = new PaperTradingRepositorySQLite(db);

    await Effect.runPromise(repository.ensureTables());
    // Must be re-runnable without error (pragma-guided, only missing
    // columns are altered).
    await Effect.runPromise(repository.ensureTables());

    const columns = (
      db.query("PRAGMA table_info(watchlist)").all() as Array<{
        name: string;
      }>
    ).map((c) => c.name);
    expect(columns).toEqual(
      expect.arrayContaining([
        "target_ratio",
        "chop_gate_adx",
        "oos_trades",
        "fills_per_day",
        "edge_per_trade_pct",
        "volatility",
        "allocated_weight",
      ]),
    );

    const targetRatio = db
      .query(
        `SELECT type, "notnull", dflt_value
         FROM pragma_table_info('watchlist') WHERE name = 'target_ratio'`,
      )
      .get() as {
      type: string;
      notnull: number;
      dflt_value: string | null;
    };
    expect(targetRatio.type).toBe("REAL");
    expect(targetRatio.notnull).toBe(1);
    expect(targetRatio.dflt_value).toBe("1");

    // A pre-existing legacy row reads back with the contract defaults.
    const legacy = await Effect.runPromise(
      repository.listWatchlist("bitget-futures", "15m"),
    );
    expect(legacy[0]?.targetRatio).toBe(1);
    expect(legacy[0]?.chopGateAdx).toBe(0);
    expect(legacy[0]?.oosTrades).toBe(0);
    expect(legacy[0]?.fillsPerDay).toBe(0);
    expect(legacy[0]?.edgePerTradePct).toBe(0);
    expect(legacy[0]?.volatility).toBe(0);
    expect(legacy[0]?.allocatedWeight).toBe(0);
    db.close();
  });

  it("round-trips the funnel contract fields through upsert/list", async () => {
    const db = new Database(":memory:");
    const repository = new PaperTradingRepositorySQLite(db);
    await Effect.runPromise(repository.ensureTables());

    const entry: WatchlistEntry = {
      exchange: "bitget-futures",
      symbol: "BTC/USDT:USDT",
      timeframe: "15m",
      returnPct: 12.5,
      profitableWindowsPct: 80,
      aggregateReturnPct: 5.2,
      gridStepPct: 1,
      gridMaxGrids: 2,
      gridPauseAfterLossBars: 0,
      targetRatio: 0.5,
      chopGateAdx: 14,
      oosTrades: 120,
      fillsPerDay: 3.75,
      edgePerTradePct: 0.21,
      volatility: 2.4,
      allocatedWeight: 0.08,
      updatedAt: new Date("2026-08-01T00:00:00.000Z"),
    };
    await Effect.runPromise(repository.upsertWatchlist([entry]));
    const loaded = await Effect.runPromise(
      repository.listWatchlist(entry.exchange, entry.timeframe),
    );
    expect(loaded).toHaveLength(1);
    expect(loaded[0]?.targetRatio).toBe(0.5);
    expect(loaded[0]?.chopGateAdx).toBe(14);
    expect(loaded[0]?.oosTrades).toBe(120);
    expect(loaded[0]?.fillsPerDay).toBe(3.75);
    expect(loaded[0]?.edgePerTradePct).toBe(0.21);
    expect(loaded[0]?.volatility).toBe(2.4);
    expect(loaded[0]?.allocatedWeight).toBe(0.08);

    // Re-upsert must update the contract fields in place, not duplicate.
    await Effect.runPromise(
      repository.upsertWatchlist([
        { ...entry, targetRatio: 0.75, chopGateAdx: 20 },
      ]),
    );
    const updated = await Effect.runPromise(
      repository.listWatchlist(entry.exchange, entry.timeframe),
    );
    expect(updated).toHaveLength(1);
    expect(updated[0]?.targetRatio).toBe(0.75);
    expect(updated[0]?.chopGateAdx).toBe(20);
    db.close();
  });

  it("replaceWatchlist atomically swaps only the matching exchange/timeframe", async () => {
    const db = new Database(":memory:");
    const repository = new PaperTradingRepositorySQLite(db);
    await Effect.runPromise(repository.ensureTables());

    const now = new Date("2026-08-01T00:00:00.000Z");
    const entry = (symbol: string, returnPct: number) => ({
      exchange: "bitget-futures",
      symbol,
      timeframe: "15m",
      returnPct,
      profitableWindowsPct: 80,
      aggregateReturnPct: 5.2,
      gridStepPct: 1,
      gridMaxGrids: 2,
      gridPauseAfterLossBars: 0,
      targetRatio: 1,
      chopGateAdx: 0,
      oosTrades: 0,
      fillsPerDay: 0,
      edgePerTradePct: 0,
      volatility: 0,
      allocatedWeight: 0,
      updatedAt: now,
    });

    await Effect.runPromise(
      repository.upsertWatchlist([
        entry("BTC/USDT:USDT", 10),
        entry("ETH/USDT:USDT", 20),
      ]),
    );
    // A different timeframe must be untouched by the replace below.
    await Effect.runPromise(
      repository.upsertWatchlist([
        { ...entry("SOL/USDT:USDT", 30), timeframe: "1h" },
      ]),
    );

    await Effect.runPromise(
      repository.replaceWatchlist("bitget-futures", "15m", [
        entry("BTC/USDT:USDT", 42),
      ]),
    );

    const replaced = await Effect.runPromise(
      repository.listWatchlist("bitget-futures", "15m"),
    );
    expect(replaced.map((e) => e.symbol)).toEqual(["BTC/USDT:USDT"]);
    expect(replaced[0]?.returnPct).toBe(42);
    const untouched = await Effect.runPromise(
      repository.listWatchlist("bitget-futures", "1h"),
    );
    expect(untouched.map((e) => e.symbol)).toEqual(["SOL/USDT:USDT"]);
    expect(untouched[0]?.aggregateReturnPct).toBe(5.2);
    db.close();
  });

  it("listAllGridTrades applies the live-only filter before the LIMIT cap", async () => {
    const db = new Database(":memory:");
    const repository = new PaperTradingRepositorySQLite(db);
    await Effect.runPromise(repository.ensureTables());

    const trade = (
      id: string,
      closedAt: string,
      fillSource: "live" | "simulated",
    ) =>
      ({
        id,
        exchange: "bitget-futures",
        symbol: "BTC/USDT:USDT",
        timeframe: "15m",
        side: "long",
        entryPrice: money("70000"),
        exitPrice: money("70100"),
        capitalBefore: money("1000"),
        capitalAfter: money("1002"),
        pnlPct: money("0.2"),
        exitReason: "target",
        openedAt: new Date("2026-08-01T00:00:00.000Z"),
        closedAt: new Date(closedAt),
        fillSource,
      }) as const;

    await Effect.runPromise(
      repository.recordGridTrade(
        trade("live-1", "2026-08-01T02:00:00.000Z", "live"),
      ),
    );
    await Effect.runPromise(
      repository.recordGridTrade(
        trade("live-2", "2026-08-01T01:00:00.000Z", "live"),
      ),
    );
    await Effect.runPromise(
      repository.recordGridTrade(
        trade("sim-1", "2026-08-01T03:00:00.000Z", "simulated"),
      ),
    );

    // With liveOnly, the simulated row must be excluded before LIMIT applies:
    // only live rows are eligible, so cap=2 returns both and cap=1 the newest.
    const all = await Effect.runPromise(
      repository.listAllGridTrades("bitget-futures", "15m", 100),
    );
    expect(all.map((t) => t.id).sort()).toEqual(["live-1", "live-2", "sim-1"]);

    const live = await Effect.runPromise(
      repository.listAllGridTrades("bitget-futures", "15m", 2, true),
    );
    expect(live.map((t) => t.id)).toEqual(["live-1", "live-2"]);
    expect(live.every((t) => t.fillSource === "live")).toBe(true);

    const liveCapped = await Effect.runPromise(
      repository.listAllGridTrades("bitget-futures", "15m", 1, true),
    );
    expect(liveCapped.map((t) => t.id)).toEqual(["live-1"]);
    db.close();
  });
});
