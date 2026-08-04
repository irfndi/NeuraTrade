import { describe, expect, it } from "bun:test";
import type { BacktestResult } from "./backtest.js";
import {
  evaluateReadiness,
  formatReadinessReport,
  defaultReadinessThresholds,
} from "./readiness.js";

function makeResult(overrides: Partial<BacktestResult> = {}): BacktestResult {
  return {
    symbol: "BTC/USDT:USDT",
    totalTrades: 240,
    winningTrades: 130,
    losingTrades: 110,
    winRate: 0.54,
    totalReturnPct: 12,
    maxDrawdownPct: 8,
    sharpeRatio: 1.1,
    trades: [],
    totalFeesPaid: 100,
    totalFundingCost: 5,
    benchmarkReturnPct: 10,
    metrics: {
      profitFactor: 1.5,
      expectancy: 0.05,
      averageRMultiple: 0.2,
      sortinoRatio: 1.2,
      calmarRatio: 1.5,
      maxConsecutiveLosses: 4,
      averageTradeDurationHours: 1.5,
      timeInMarketPct: 20,
    },
    robustnessScore: 50,
    oosResult: {
      symbol: "BTC/USDT:USDT",
      totalTrades: 48,
      winningTrades: 26,
      losingTrades: 22,
      winRate: 0.54,
      totalReturnPct: 2.5,
      maxDrawdownPct: 6,
      sharpeRatio: 0.9,
      trades: [],
      totalFeesPaid: 20,
      totalFundingCost: 1,
      benchmarkReturnPct: 2,
      metrics: {
        profitFactor: 1.4,
        expectancy: 0.04,
        averageRMultiple: 0.15,
        sortinoRatio: 1,
        calmarRatio: 1,
        maxConsecutiveLosses: 3,
        averageTradeDurationHours: 1.4,
        timeInMarketPct: 18,
      },
      robustnessScore: 40,
    },
    monteCarlo: {
      iterations: 200,
      medianMaxDrawdownPct: 10,
      p95MaxDrawdownPct: 15,
      p99MaxDrawdownPct: 18,
      worstMaxDrawdownPct: 20,
      probabilityOfRuinPct: 1,
    },
    ...overrides,
  };
}

describe("evaluateReadiness", () => {
  it("passes all gates for a solid scalping result", () => {
    const report = evaluateReadiness({
      result: makeResult(),
      timeframe: "5m",
      inSampleMonths: 9.6, // 240 trades -> 25/month
    });
    expect(report.ready).toBe(true);
    expect(report.gates).toHaveLength(10);
    for (const g of report.gates) expect(g.pass).toBe(true);
  });

  it("fails G1a when trade frequency is too low", () => {
    const report = evaluateReadiness({
      result: makeResult({ totalTrades: 50 }),
      timeframe: "5m",
      inSampleMonths: 9.6, // ~5.2/month
    });
    const g = report.gates.find((x) => x.gate === "G1a")!;
    expect(g.pass).toBe(false);
    expect(report.ready).toBe(false);
  });

  it("fails G1b when OOS trades are missing", () => {
    const r = makeResult();
    const report = evaluateReadiness({
      result: {
        ...r,
        oosResult: { ...r.oosResult!, totalTrades: 4 },
      },
      timeframe: "5m",
      inSampleMonths: 9.6,
    });
    expect(report.gates.find((x) => x.gate === "G1b")!.pass).toBe(false);
  });

  it("fails G1b and G3a/G3b when there is no OOS run at all", () => {
    const r = makeResult();
    const { oosResult: _drop, ...noOos } = r;
    const report = evaluateReadiness({
      result: noOos,
      timeframe: "5m",
      inSampleMonths: 12,
    });
    expect(report.gates.find((x) => x.gate === "G1b")!.pass).toBe(false);
    expect(report.gates.find((x) => x.gate === "G3a")!.pass).toBe(false);
    expect(report.gates.find((x) => x.gate === "G3b")!.pass).toBe(false);
  });

  it("fails G2 gates for losing economics", () => {
    const r = makeResult();
    const report = evaluateReadiness({
      result: {
        ...r,
        winRate: 0.35,
        metrics: { ...r.metrics, profitFactor: 0.9, expectancy: -0.1 },
      },
      timeframe: "5m",
      inSampleMonths: 9.6,
    });
    expect(report.gates.find((x) => x.gate === "G2a")!.pass).toBe(false);
    expect(report.gates.find((x) => x.gate === "G2b")!.pass).toBe(false);
    expect(report.gates.find((x) => x.gate === "G2c")!.pass).toBe(false);
  });

  it("expectancy of exactly 0 fails G2c (must be strictly positive)", () => {
    const r = makeResult();
    const report = evaluateReadiness({
      result: { ...r, metrics: { ...r.metrics, expectancy: 0 } },
      timeframe: "5m",
      inSampleMonths: 9.6,
    });
    expect(report.gates.find((x) => x.gate === "G2c")!.pass).toBe(false);
  });

  it("fails G3 gates for bad OOS/MC robustness", () => {
    const r = makeResult();
    const report = evaluateReadiness({
      result: {
        ...r,
        oosResult: {
          ...r.oosResult!,
          totalReturnPct: -9.15,
          maxDrawdownPct: 25,
        },
        monteCarlo: {
          ...r.monteCarlo!,
          p95MaxDrawdownPct: 26,
          probabilityOfRuinPct: 7,
        },
      },
      timeframe: "5m",
      inSampleMonths: 9.6,
    });
    expect(report.gates.find((x) => x.gate === "G3a")!.pass).toBe(false);
    expect(report.gates.find((x) => x.gate === "G3b")!.pass).toBe(false);
    expect(report.gates.find((x) => x.gate === "G3c")!.pass).toBe(false);
    expect(report.gates.find((x) => x.gate === "G3d")!.pass).toBe(false);
  });

  it("fails G4 when holds are too long", () => {
    const r = makeResult();
    const report = evaluateReadiness({
      result: {
        ...r,
        metrics: { ...r.metrics, averageTradeDurationHours: 9.1 },
      },
      timeframe: "5m",
      inSampleMonths: 9.6,
    });
    expect(report.gates.find((x) => x.gate === "G4")!.pass).toBe(false);
  });

  it("honours threshold overrides", () => {
    const report = evaluateReadiness({
      result: makeResult({ totalTrades: 50 }),
      timeframe: "5m",
      inSampleMonths: 9.6,
      thresholds: { minTradesPerMonth: 5 },
    });
    expect(report.gates.find((x) => x.gate === "G1a")!.pass).toBe(true);
  });

  it("baseline shape fails (documents the current NOT READY state)", () => {
    // Mirrors the recorded BTC 5m defaults baseline: 110 IS trades over
    // 9.6 IS-months, 40% winrate, expectancy 0.096, 9.1h holds, OOS -9.15%.
    const r = makeResult({
      totalTrades: 110,
      winRate: 0.4,
      metrics: {
        ...makeResult().metrics,
        expectancy: 0.096,
        profitFactor: 1.201,
        averageTradeDurationHours: 9.1,
      },
    });
    const report = evaluateReadiness({
      result: {
        ...r,
        oosResult: {
          ...r.oosResult!,
          totalTrades: 17,
          winRate: 0.2353,
          totalReturnPct: -9.15,
          maxDrawdownPct: 11.79,
        },
        monteCarlo: {
          ...r.monteCarlo!,
          p95MaxDrawdownPct: 25.97,
          probabilityOfRuinPct: 0,
        },
      },
      timeframe: "5m",
      inSampleMonths: 9.6,
    });
    expect(report.ready).toBe(false);
    const failed = report.gates.filter((g) => !g.pass).map((g) => g.gate);
    expect(failed).toEqual(["G1a", "G2a", "G2b", "G3a", "G3c", "G4"]);
  });
});

describe("formatReadinessReport", () => {
  it("renders a gate table with verdicts", () => {
    const report = evaluateReadiness({
      result: makeResult(),
      timeframe: "5m",
      inSampleMonths: 9.6,
    });
    const text = formatReadinessReport(report);
    expect(text).toContain("Scalping readiness: BTC/USDT:USDT (5m)");
    expect(text).toContain("G2c");
    expect(text).toContain("PASS");
    expect(text).toContain("READY: all gates pass");
  });

  it("renders NOT READY with the failing count", () => {
    const report = evaluateReadiness({
      result: makeResult({ totalTrades: 5 }),
      timeframe: "5m",
      inSampleMonths: 9.6,
    });
    const text = formatReadinessReport(report);
    expect(text).toContain("NOT READY: 1 gate(s) failing");
  });
});

describe("defaultReadinessThresholds", () => {
  it("matches the design doc gates", () => {
    expect(defaultReadinessThresholds.minProfitFactor).toBe(1.3);
    expect(defaultReadinessThresholds.maxOosDrawdownPct).toBe(15);
    expect(defaultReadinessThresholds.maxMcP95DrawdownPct).toBe(20);
    expect(defaultReadinessThresholds.maxMcRuinPct).toBe(5);
    expect(defaultReadinessThresholds.maxAvgDurationHours).toBe(4);
  });
});
