import { describe, expect, it } from "bun:test";
import * as fc from "fast-check";
import { money } from "../utils/money.js";
import { evaluateDemoSoak } from "./demo-readiness.js";
import type { GridPaperTrade } from "./types.js";

function makeTrade(index: number, basisPoints: number): GridPaperTrade {
  const openedAt = new Date(Date.UTC(2026, 0, 1 + index / 4));
  const realizedPnlPct = money(basisPoints).div(100);
  return {
    id: `property-${index}`,
    exchange: "bitget-futures",
    symbol: "BTC/USDT:USDT",
    timeframe: "15m",
    side: "long",
    entryPrice: money(70000),
    exitPrice: money(70001),
    capitalBefore: money(1000),
    capitalAfter: money(1000),
    pnlPct: realizedPnlPct,
    exitReason: "target",
    openedAt,
    closedAt: new Date(openedAt.getTime() + 60 * 60 * 1000),
    fillSource: "live",
    entryOrderId: `entry-${index}`,
    exitOrderId: `exit-${index}`,
    entryFilledQty: money("0.01"),
    exitFilledQty: money("0.01"),
    entryFee: money("0.01"),
    exitFee: money("0.01"),
    realizedPnlPct,
  };
}

describe("evaluateDemoSoak property coverage", () => {
  it("never produces non-finite metrics for bounded live-fill PnL sequences", () => {
    fc.assert(
      fc.property(
        fc.array(fc.integer({ min: -50, max: 50 }), {
          minLength: 0,
          maxLength: 100,
        }),
        (basisPoints) => {
          const report = evaluateDemoSoak(
            basisPoints.map((value, index) => makeTrade(index, value)),
            {
              minimumTrades: 1,
              minimumDurationDays: 0,
              minimumExpectancyPct: money(-100),
              maximumDrawdownPct: money(100),
            },
          );

          expect(report.tradeCount).toBe(basisPoints.length);
          expect(report.expectancyPct.isFinite()).toBe(true);
          expect(report.maximumDrawdownPct.isFinite()).toBe(true);
          expect(report.maximumDrawdownPct.greaterThanOrEqualTo(0)).toBe(true);
        },
      ),
    );
  });
});
