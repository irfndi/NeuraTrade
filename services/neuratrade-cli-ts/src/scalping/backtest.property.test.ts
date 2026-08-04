import { describe, it } from "bun:test";
import * as fc from "fast-check";
import { runBacktest, type BacktestOptions } from "./backtest.js";
import { defaultComposerConfig } from "./composer.js";
import type { CandleLike } from "./types.js";

const baseOpts = {
  symbol: "BTC/USDT",
  exchange: "binance",
  timeframe: "1h",
  composerConfig: defaultComposerConfig,
  initialCapital: 10_000,
  positionSizePct: 10,
  stopLossPct: 5,
  takeProfitPct: 10,
  minConfidence: 0.1,
} as const;

function withExitEngineOverrides(
  opts: BacktestOptions,
  useAtrStops: boolean,
  atrStopMultiplier: number,
  atrTakeProfitMultiplier: number,
  atrRiskReward: number,
  scaleOutAtR: number,
  scaleOutPct: number,
): BacktestOptions {
  return {
    ...opts,
    useAtrStops,
    atrStopMultiplier,
    atrTakeProfitMultiplier,
    atrRiskReward,
    scaleOutAtR,
    scaleOutPct,
  };
}

function makeCandleArb(): fc.Arbitrary<CandleLike[]> {
  return fc
    .array(
      fc.record({
        open: fc.float({ min: 1, max: 10_000, noNaN: true }),
        close: fc.float({ min: 1, max: 10_000, noNaN: true }),
        volume: fc.float({ min: Math.fround(0.1), max: 1000, noNaN: true }),
      }),
      { minLength: 21, maxLength: 100 },
    )
    .map((raw) =>
      raw.map((r, i) => {
        const mid = Math.max(r.open, r.close);
        const lo = Math.min(r.open, r.close);
        return {
          open: r.open,
          high: mid * 1.001 + 0.01,
          low: Math.max(0.01, lo * 0.999),
          close: r.close,
          volume: r.volume,
          timestamp: new Date(Date.now() + i * 3_600_000),
        };
      }),
    );
}

describe("Backtest property invariants", () => {
  it("non-empty candles produce finite totalReturnPct, maxDrawdownPct, sharpeRatio", () => {
    fc.assert(
      fc.property(makeCandleArb(), (candles) => {
        const result = runBacktest({ ...baseOpts, candles, feePct: 0 });
        return (
          Number.isFinite(result.totalReturnPct) &&
          Number.isFinite(result.maxDrawdownPct) &&
          Number.isFinite(result.sharpeRatio)
        );
      }),
      { numRuns: 50 },
    );
  });

  it("feePct=0 and slippageBps=0 means totalFeesPaid === 0", () => {
    fc.assert(
      fc.property(makeCandleArb(), (candles) => {
        const result = runBacktest({
          ...baseOpts,
          candles,
          feePct: 0,
          slippageBps: 0,
        });
        return result.totalFeesPaid === 0;
      }),
      { numRuns: 50 },
    );
  });

  it("isFutures=false means totalFundingCost === 0", () => {
    fc.assert(
      fc.property(makeCandleArb(), (candles) => {
        const result = runBacktest({
          ...baseOpts,
          candles,
          feePct: 0,
          isFutures: false,
          fundingRatePct: 0.01,
        });
        return result.totalFundingCost === 0;
      }),
      { numRuns: 50 },
    );
  });

  it("random exit-engine configs produce finite results", () => {
    fc.assert(
      fc.property(
        makeCandleArb(),
        fc.boolean(),
        fc.float({ min: 0.5, max: 5, noNaN: true }),
        fc.float({ min: 1, max: 6, noNaN: true }),
        fc.float({ min: 0, max: 4, noNaN: true }),
        fc.float({ min: 0, max: 3, noNaN: true }),
        fc.float({ min: 10, max: 90, noNaN: true }),
        (
          candles,
          useAtrStops,
          atrStopMultiplier,
          atrTakeProfitMultiplier,
          atrRiskReward,
          scaleOutAtR,
          scaleOutPct,
        ) => {
          const result = runBacktest(
            withExitEngineOverrides(
              { ...baseOpts, candles, feePct: 0 },
              useAtrStops,
              atrStopMultiplier,
              atrTakeProfitMultiplier,
              atrRiskReward,
              scaleOutAtR,
              scaleOutPct,
            ),
          );
          return (
            Number.isFinite(result.totalReturnPct) &&
            Number.isFinite(result.maxDrawdownPct) &&
            Number.isFinite(result.sharpeRatio) &&
            result.trades.every((t) => Number.isFinite(t.pnl))
          );
        },
      ),
      { numRuns: 50 },
    );
  });
});
