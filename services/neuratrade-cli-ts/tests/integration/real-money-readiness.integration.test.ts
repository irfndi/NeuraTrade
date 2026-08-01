import { describe, expect, it } from "bun:test";
import { Database } from "bun:sqlite";
import { Effect } from "effect";
import { money } from "../../src/utils/money.js";
import { PaperTradingRepositorySQLite } from "../../src/paper-trading/repository.js";

describe("real-money readiness SQLite integration", () => {
  it("persists immutable entry provenance through a real SQLite schema", async () => {
    const db = new Database(":memory:");
    const repository = new PaperTradingRepositorySQLite(db);
    const trade = {
      id: "integration-provenance-1",
      exchange: "bitget-futures",
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
      executionEnvironment: "bitget-demo" as const,
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
    expect(rows[0]?.executionEnvironment).toBe("bitget-demo");
    db.close();
  });
});
