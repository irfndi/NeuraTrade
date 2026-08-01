import { describe, expect, it } from "bun:test";
import { Database } from "bun:sqlite";
import { Effect } from "effect";
import { money } from "../utils/money.js";
import { PaperTradingRepositorySQLite } from "./repository.js";
import type { GridPaperTrade, PaperPosition } from "./types.js";

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
});
