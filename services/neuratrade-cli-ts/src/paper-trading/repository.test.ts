import { describe, expect, it } from "bun:test";
import { Database } from "bun:sqlite";
import { Effect } from "effect";
import { money } from "../utils/money.js";
import { PaperTradingRepositorySQLite } from "./repository.js";
import type { PaperPosition } from "./types.js";

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
});
