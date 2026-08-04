import { describe, expect, it } from "bun:test";
import { money } from "../utils/money.js";
import {
  evaluateDemoSoak,
  serializeDemoSoakReport,
  type DemoSoakThresholds,
} from "./demo-readiness.js";
import type { GridPaperTrade } from "./types.js";

const thresholds: DemoSoakThresholds = {
  minimumTrades: 50,
  minimumDurationDays: 7,
  minimumExpectancyPct: money(0),
  minimumExpectancyLowerBoundPct: money(0),
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
    expect(report.expectancyLowerBoundPct.toString()).toBe("0.2");
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

  it("rejects a positive point estimate without a positive confidence bound", () => {
    const report = evaluateDemoSoak(
      Array.from({ length: 50 }, (_, index) =>
        makeTrade(index, index === 0 ? "-4" : "0.10"),
      ),
      thresholds,
    );

    expect(report.expectancyPct.greaterThan(0)).toBe(true);
    expect(report.expectancyLowerBoundPct.lessThan(0)).toBe(true);
    expect(report.passed).toBe(false);
    expect(report.failures).toContain(
      "expectancy confidence lower bound is below the minimum",
    );
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

  it("serializes decimal metrics into a machine-readable signoff record", () => {
    const report = evaluateDemoSoak(
      Array.from({ length: 50 }, (_, index) => makeTrade(index, "0.20")),
      thresholds,
    );

    const output = serializeDemoSoakReport(report);

    expect(output).toContain('"status":"PASS"');
    expect(output).toContain('"expectancyPct":"0.2"');
    expect(output).toContain('"expectancyLowerBoundPct":"0.2"');
    expect(output).toContain('"profitFactor":null');
  });

  it("rejects live-fill rows with invalid exchange timestamps", () => {
    const trades = Array.from({ length: 50 }, (_, index) =>
      makeTrade(index, "0.20"),
    );
    const corruptTrade = trades[0];
    if (corruptTrade === undefined) throw new Error("fixture is empty");

    const report = evaluateDemoSoak(
      [{ ...corruptTrade, closedAt: new Date("invalid") }, ...trades.slice(1)],
      thresholds,
    );

    expect(report.passed).toBe(false);
    expect(report.failures).toContain(
      "one or more trades lack complete live fill evidence",
    );
  });

  it("reports an infinite profit factor for an all-winning sample", () => {
    const report = evaluateDemoSoak(
      Array.from({ length: 50 }, (_, index) => makeTrade(index, "0.20")),
      thresholds,
    );

    expect(report.profitFactor).toBeNull();
  });

  it("reports a zero profit factor for an all-losing sample", () => {
    const report = evaluateDemoSoak(
      Array.from({ length: 50 }, (_, index) => makeTrade(index, "-0.30")),
      thresholds,
    );

    expect(report.profitFactor?.toString()).toBe("0");
    expect(report.failures).toContain("expectancy is below the minimum");
  });

  it("rejects a sample whose drawdown exceeds the ceiling", () => {
    // One large loss early on, then small wins: the peak-to-trough drawdown
    // exceeds the 15% ceiling while expectancy stays positive.
    const trades = Array.from({ length: 50 }, (_, index) =>
      index === 0 ? makeTrade(index, "-20") : makeTrade(index, "0.10"),
    );
    const report = evaluateDemoSoak(trades, thresholds);

    expect(report.maximumDrawdownPct.greaterThan(money(15))).toBe(true);
    expect(report.failures).toContain("drawdown is above the maximum");
  });

  it("counts the whole cohort duration from the earliest open to the latest close", () => {
    const trades = Array.from({ length: 50 }, (_, index) =>
      makeTrade(index, "0.20"),
    );
    const report = evaluateDemoSoak(trades, thresholds);

    // 50 trades opened a quarter-day apart span 12.25 days.
    expect(report.durationDays).toBeGreaterThan(12);
    expect(report.durationDays).toBeLessThan(13);
  });
});
