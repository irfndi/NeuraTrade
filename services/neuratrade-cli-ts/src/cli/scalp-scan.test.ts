import { describe, expect, it } from "bun:test";
import { Effect } from "effect";
import { Database } from "bun:sqlite";
import { MarketDataRepository, MarketDataRepositorySQLiteLive } from "../market-data/repository.js";
import type { Candle } from "../market-data/types.js";
import { scanProgram } from "./scalp.js";

function makeCandles(symbol: string, count: number): Candle[] {
  const candles: Candle[] = [];
  let close = 100;
  for (let i = 0; i < count; i++) {
    const open = close;
    close *= i % 7 === 0 ? 1.02 : 0.998;
    const high = Math.max(open, close) * 1.002;
    const low = Math.min(open, close) * 0.998;
    candles.push({
      exchange: "binance",
      symbol,
      timeframe: "1h",
      open,
      high,
      low,
      close,
      volume: 10,
      timestamp: new Date(Date.now() - (count - i) * 3_600_000),
    });
  }
  return candles;
}

describe("scanProgram", () => {
  it("runs a per-symbol optimized scan across stored symbols", async () => {
    const db = new Database(":memory:");
    const repoLayer = MarketDataRepositorySQLiteLive(db);

    await Effect.runPromise(
      Effect.gen(function* () {
        const repo = yield* MarketDataRepository;
        yield* repo.ensureTables();
        yield* repo.saveCandles(makeCandles("BTC/USDT", 120));
        yield* repo.saveCandles(makeCandles("ETH/USDT", 120));
      }).pipe(Effect.provide(repoLayer)),
    );

    const results = await Effect.runPromise(
      scanProgram({
        exchange: "binance",
        timeframe: "1h",
        capital: 10_000,
        positionSize: 100,
        fee: 0.1,
        minConfidence: 0.5,
        useAtrStops: true,
        atrStopMultiplier: 2.0,
        atrTakeProfitMultiplier: 2.5,
        priceOnly: true,
        noRsi: false,
        noTrend: true,
        holdUntilStop: false,
        regimeMode: "reversion",
        minCandles: 50,
        top: 0,
        optimize: true,
      }).pipe(Effect.provide(repoLayer)),
    );

    expect(results.length).toBe(2);
    for (const r of results) {
      expect(r.bestParams).toBeDefined();
      expect(r.totalTrades).toBeGreaterThanOrEqual(0);
    }

    db.close();
  });
});
