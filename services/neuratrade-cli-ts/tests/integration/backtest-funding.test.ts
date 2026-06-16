import { describe, expect, it } from "bun:test";
import { runBacktest } from "../../src/scalping/backtest.js";
import { defaultComposerConfig } from "../../src/scalping/composer.js";
import type { CandleLike } from "../../src/scalping/types.js";

function makeCandles(count: number): CandleLike[] {
  const candles: CandleLike[] = [];
  let close = 100;
  for (let i = 0; i < count; i++) {
    const open = close;
    close *= 1.005;
    const high = Math.max(open, close) * 1.002;
    const low = Math.min(open, close) * 0.998;
    candles.push({
      open,
      high,
      low,
      close,
      volume: 50,
      timestamp: new Date(Date.UTC(2025, 0, 1, 0, i)),
    });
  }
  return candles;
}

const baseOpts = {
  symbol: "BTC/USDT",
  exchange: "binance",
  timeframe: "1h",
  composerConfig: defaultComposerConfig,
  initialCapital: 10000,
  positionSizePct: 100,
  stopLossPct: 2,
  takeProfitPct: 4,
  feePct: 0.1,
  minConfidence: 0.1,
  isFutures: true,
  fundingIntervalHours: 1 / 60,
} as const;

describe("backtest funding cost integration", () => {
  it("funding reduces return vs zero-funding baseline", () => {
    const candles = makeCandles(200);

    const runA = runBacktest({ ...baseOpts, candles, fundingRatePct: 0 });
    const runB = runBacktest({ ...baseOpts, candles, fundingRatePct: 0.01 });

    expect(runA.totalTrades).toBeGreaterThan(0);
    expect(runB.totalTrades).toBeGreaterThan(0);
    expect(runB.totalFundingCost).toBeGreaterThan(0);
    expect(runB.totalReturnPct).toBeLessThan(runA.totalReturnPct);
    expect(runB.totalFundingCost).toBeLessThan(20_000);
  });
});
