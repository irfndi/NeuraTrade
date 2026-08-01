import { describe, expect, it } from "bun:test";
import { money } from "../utils/money.js";
import { evaluateDemoSoak, type DemoSoakThresholds } from "./demo-readiness.js";
import type { GridPaperTrade } from "./types.js";

const thresholds: DemoSoakThresholds = {
  minimumTrades: 50,
  minimumDurationDays: 7,
  minimumExpectancyPct: money(0),
  maximumDrawdownPct: money(15),
};

function makeTrade(index: number, pnlPct: string): GridPaperTrade {
  const openedAt = new Date(Date.UTC(2026, 0, 1 + index / 4));
  return {
    id: `demo-${index}`,
    exchange: "bitget-futures",
    symbol: "BTC/USDT:USDT",
    timeframe: "15m",
    side: index % 2 === 0 ? "long" : "short",
    entryPrice: money("70000"),
    exitPrice: money("70010"),
    capitalBefore: money("1000"),
    capitalAfter: money("1000"),
    pnlPct: money(pnlPct),
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
    realizedPnlPct: money(pnlPct),
  };
}

describe("evaluateDemoSoak", () => {
  it("passes only a seven-day, 50-trade live-fill sample with positive expectancy", () => {
    const report = evaluateDemoSoak(
      Array.from({ length: 50 }, (_, index) => makeTrade(index, "0.20")),
      thresholds,
    );

    expect(report.passed).toBe(true);
    expect(report.tradeCount).toBe(50);
    expect(report.durationDays).toBeGreaterThanOrEqual(7);
    expect(report.expectancyPct.toString()).toBe("0.2");
    expect(report.failures).toEqual([]);
  });

  it("rejects adverse-selection results even when the sample is large enough", () => {
    const report = evaluateDemoSoak(
      Array.from({ length: 50 }, (_, index) =>
        makeTrade(index, index % 2 === 0 ? "0.10" : "-0.30"),
      ),
      thresholds,
    );

    expect(report.passed).toBe(false);
    expect(report.expectancyPct.toString()).toBe("-0.1");
    expect(report.failures).toContain("expectancy is below the minimum");
  });

  it("rejects insufficient duration, count, and missing live fill evidence", () => {
    const report = evaluateDemoSoak(
      Array.from({ length: 3 }, (_, index) => ({
        ...makeTrade(index, "0.20"),
        fillSource: "simulated" as const,
      })),
      thresholds,
    );

    expect(report.passed).toBe(false);
    expect(report.failures).toEqual([
      "trade count is below the minimum",
      "duration is below the minimum",
      "one or more trades lack complete live fill evidence",
    ]);
  });
});
