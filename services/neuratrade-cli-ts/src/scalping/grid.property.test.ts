import { describe, expect, it } from "bun:test";
import * as fc from "fast-check";
import type { CandleLike } from "./types.js";
import { runGridBacktest } from "./grid.js";

const returnsArbitrary = fc.array(
  fc.double({ min: -0.01, max: 0.01, noNaN: true, noDefaultInfinity: true }),
  { minLength: 40, maxLength: 120 },
);

function candlesFromReturns(returns: readonly number[]): CandleLike[] {
  let price = 100;
  return returns.map((change, index) => {
    const open = price;
    const close = open * (1 + change);
    const high = Math.max(open, close) * 1.002;
    const low = Math.min(open, close) * 0.998;
    price = close;
    return {
      open,
      high,
      low,
      close,
      volume: 1,
      timestamp: new Date(index * 15 * 60 * 1000),
    };
  });
}

describe("runGridBacktest property invariants", () => {
  it("returns finite equity metrics and coherent trade records for valid candles", () => {
    fc.assert(
      fc.property(returnsArbitrary, (returns) => {
        const result = runGridBacktest(candlesFromReturns(returns), {
          gridStepPct: 0.5,
          gridMaxGrids: 2,
          gridPauseAfterLossBars: 2,
          feePct: 0.04,
          slippageBps: 1,
          initialCapital: 100,
          trendFilterPeriod: 14,
          leverage: 1,
          makerFillProb: 0.75,
          adverseSelection: true,
          takerExitFeePct: 0.06,
          fillSeed: 42,
        });

        expect(Number.isFinite(result.totalReturnPct)).toBe(true);
        expect(Number.isFinite(result.maxDrawdownPct)).toBe(true);
        expect(Number.isFinite(result.winRate)).toBe(true);
        expect(result.maxDrawdownPct).toBeGreaterThanOrEqual(0);
        expect(result.winRate).toBeGreaterThanOrEqual(0);
        expect(result.winRate).toBeLessThanOrEqual(100);
        expect(result.profitFactor).toBeGreaterThanOrEqual(0);
        expect(result.totalTrades).toBe(result.trades.length);

        for (const trade of result.trades) {
          expect(trade.entryBar).toBeGreaterThanOrEqual(0);
          expect(trade.exitBar).toBeGreaterThanOrEqual(trade.entryBar);
          expect(Number.isFinite(trade.entryPrice)).toBe(true);
          expect(Number.isFinite(trade.exitPrice)).toBe(true);
          expect(Number.isFinite(trade.pnlPct)).toBe(true);
          expect(Number.isFinite(trade.pnlQuote)).toBe(true);
          expect(trade.win).toBe(trade.pnlPct > 0);
        }
      }),
      { numRuns: 60 },
    );
  });

  it("replays the same fill sequence for the same seed", () => {
    fc.assert(
      fc.property(returnsArbitrary, fc.integer(), (returns, fillSeed) => {
        const candles = candlesFromReturns(returns);
        const options = {
          gridStepPct: 0.5,
          gridMaxGrids: 2,
          gridPauseAfterLossBars: 0,
          feePct: 0.04,
          slippageBps: 1,
          initialCapital: 100,
          trendFilterPeriod: 14,
          leverage: 1,
          makerFillProb: 0.5,
          adverseSelection: true,
          fillSeed,
        } as const;

        expect(runGridBacktest(candles, options)).toEqual(
          runGridBacktest(candles, options),
        );
      }),
      { numRuns: 40 },
    );
  });
});
