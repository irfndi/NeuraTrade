import { describe, expect, it } from "bun:test";
import { runBacktest } from "./backtest.js";
import { defaultComposerConfig } from "./composer.js";
import type { CandleLike } from "./types.js";

function makeCandles(count: number, baseClose = 100, trend: "up" | "down" | "flat" = "flat"): CandleLike[] {
  const candles: CandleLike[] = [];
  let close = baseClose;
  for (let i = 0; i < count; i++) {
    const open = close;
    if (trend === "up") close *= 1.005;
    else if (trend === "down") close *= 0.995;
    const high = Math.max(open, close) * 1.002;
    const low = Math.min(open, close) * 0.998;
    candles.push({ open, high, low, close, volume: 10, timestamp: new Date(Date.now() + i * 60000) });
  }
  return candles;
}

describe("runBacktest", () => {
  it("returns empty result with insufficient candles", () => {
    const result = runBacktest({
      symbol: "BTC/USDT",
      exchange: "binance",
      timeframe: "1h",
      candles: makeCandles(5),
      composerConfig: defaultComposerConfig,
      initialCapital: 10000,
      positionSizePct: 10,
      stopLossPct: 1,
      takeProfitPct: 2,
      feePct: 0.1,
      minConfidence: 0.5,
    });

    expect(result.totalTrades).toBe(0);
  });

  it("runs trades on a trending series", () => {
    const result = runBacktest({
      symbol: "BTC/USDT",
      exchange: "binance",
      timeframe: "1h",
      candles: makeCandles(100, 100, "up"),
      composerConfig: defaultComposerConfig,
      initialCapital: 10000,
      positionSizePct: 100,
      stopLossPct: 5,
      takeProfitPct: 10,
      feePct: 0,
      minConfidence: 0.1,
    });

    expect(result.totalTrades).toBeGreaterThan(0);
    expect(result.trades.length).toBe(result.totalTrades);
    expect(result.totalReturnPct).not.toBe(0);
  });

  it("respects minConfidence", () => {
    const result = runBacktest({
      symbol: "BTC/USDT",
      exchange: "binance",
      timeframe: "1h",
      candles: makeCandles(100, 100, "up"),
      composerConfig: defaultComposerConfig,
      initialCapital: 10000,
      positionSizePct: 100,
      stopLossPct: 5,
      takeProfitPct: 10,
      feePct: 0,
      minConfidence: 0.99,
    });

    expect(result.totalTrades).toBe(0);
  });
});
