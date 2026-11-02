// Growth E3 cohort expansion — unit tests for the pure helpers.
// No DB, no live workers, no champion files: synthetic GridResult inputs only.
import { describe, expect, test } from "bun:test";
import {
  BASELINE_SYMBOLS,
  FROZEN_CHAMPION_GRID,
  HOLDOUT_FRACTION,
  MIN_CANDLES,
  TIMEFRAME,
  VENUE,
  baselineGrowthEstimate,
  meanTradeExpectancyPct,
  passesCohortGate,
  tradesPerMonth,
  unionGrowthEstimate,
  venueEligible,
  type CohortGateMetrics,
  type CohortRow,
} from "./growth-e3-cohort-expansion.js";
import type { GridResult } from "../src/scalping/grid.js";

function syntheticResult(pnlPcts: number[]): GridResult {
  return {
    totalReturnPct: 1,
    maxDrawdownPct: 1,
    winRate: 50,
    totalTrades: pnlPcts.length,
    profitFactor: 1,
    trades: pnlPcts.map((pnlPct, i) => ({
      side: "long" as const,
      entryBar: i,
      exitBar: i + 1,
      entryPrice: 100,
      exitPrice: 100 * (1 + pnlPct),
      pnlPct,
      pnlQuote: pnlPct,
      win: pnlPct > 0,
      isLiquidation: false,
      initialRiskPct: 2,
    })),
  };
}

function goodGates(over: Partial<CohortGateMetrics> = {}): CohortGateMetrics {
  return {
    profitableWindowPct: 60,
    compoundedReturnPct: 5,
    maxDrawdownPct: 8,
    fixedOosTrades: 40,
    confidenceLowerBoundPct: 0.001,
    stressWorstReturnPct: 1,
    stressLowerBoundPct: 0.001,
    windowCount: 12,
    ...over,
  };
}

function okRow(symbol: string, ret: number, pass = true): CohortRow {
  return {
    symbol,
    status: "ok",
    candles: 69120,
    gates: goodGates({ compoundedReturnPct: ret }),
    pass,
    failures: [],
  };
}

describe("growth-e3 frozen champion config", () => {
  test("maps champion-soak grid knobs with honest fees", () => {
    expect(FROZEN_CHAMPION_GRID.gridStepPct).toBe(1.3);
    expect(FROZEN_CHAMPION_GRID.gridMaxGrids).toBe(2);
    expect(FROZEN_CHAMPION_GRID.gridPauseAfterLossBars).toBe(2);
    expect(FROZEN_CHAMPION_GRID.targetRatio).toBe(1.95);
    expect(FROZEN_CHAMPION_GRID.chopGateAdxThreshold).toBe(0);
    expect(FROZEN_CHAMPION_GRID.feePct).toBe(0.02);
    expect(FROZEN_CHAMPION_GRID.takerExitFeePct).toBe(0.06);
    expect(FROZEN_CHAMPION_GRID.slippageBps).toBe(1);
    expect(FROZEN_CHAMPION_GRID.leverage).toBe(1);
  });
  test("venue constants pin bybit-futures 15m", () => {
    expect(VENUE).toBe("bybit-futures");
    expect(TIMEFRAME).toBe("15m");
    expect(MIN_CANDLES).toBe(55000);
    expect(HOLDOUT_FRACTION).toBe(0.2);
    expect([...BASELINE_SYMBOLS]).toEqual(["BTC/USDT:USDT", "ETH/USDT:USDT"]);
  });
});

describe("meanTradeExpectancyPct", () => {
  test("mean of pnlPct fractions scaled to percent", () => {
    expect(
      meanTradeExpectancyPct(syntheticResult([0.01, -0.005, 0.02])),
    ).toBeCloseTo(0.833333, 5);
  });
  test("empty trade list is zero, not NaN", () => {
    expect(meanTradeExpectancyPct(syntheticResult([]))).toBe(0);
  });
});

describe("tradesPerMonth", () => {
  test("holdout months derive from frozen 20% of 15m bars", () => {
    // 69120 bars * 0.2 * 15min / 43200 = 4.8 months
    expect(tradesPerMonth(48, 69120)).toBeCloseTo(10, 9);
  });
  test("zero candles guard", () => {
    expect(tradesPerMonth(10, 0)).toBe(0);
  });
});

describe("venueEligible", () => {
  test("accepts deep bybit-futures 15m history", () => {
    expect(venueEligible("bybit-futures", "15m", 69120)).toBe(true);
  });
  test("rejects wrong venue, timeframe, or shallow history", () => {
    expect(venueEligible("bitget-futures", "15m", 69120)).toBe(false);
    expect(venueEligible("bybit-futures", "1h", 69120)).toBe(false);
    expect(venueEligible("bybit-futures", "15m", 1000)).toBe(false);
  });
});

describe("passesCohortGate", () => {
  test("good gates pass with no failures", () => {
    const r = passesCohortGate(goodGates());
    expect(r.pass).toBe(true);
    expect(r.failures).toEqual([]);
  });
  test("each breach is named", () => {
    const r = passesCohortGate(
      goodGates({
        profitableWindowPct: 40,
        compoundedReturnPct: -1,
        maxDrawdownPct: 20,
        fixedOosTrades: 5,
        confidenceLowerBoundPct: -0.1,
        stressWorstReturnPct: -2,
        stressLowerBoundPct: -0.1,
        windowCount: 3,
      }),
    );
    expect(r.pass).toBe(false);
    expect(r.failures).toEqual([
      "windows<50%",
      "compounded<=0",
      "dd>15%",
      "oos<30",
      "confLB<0",
      "stressRet<0",
      "stressLB<0",
      "windows<10",
    ]);
  });
});

describe("union and baseline estimates", () => {
  const rows: CohortRow[] = [
    okRow("BTC/USDT:USDT", 4),
    okRow("ETH/USDT:USDT", 6),
    okRow("SOL/USDT:USDT", 10),
    { ...okRow("DOGE/USDT:USDT", 99), pass: false },
    { symbol: "XRP/USDT:USDT", status: "invalid", detail: "stale" },
  ];
  test("union means only PASS rows", () => {
    const u = unionGrowthEstimate(rows);
    expect(u.symbols).toEqual([
      "BTC/USDT:USDT",
      "ETH/USDT:USDT",
      "SOL/USDT:USDT",
    ]);
    expect(u.meanReturnPct).toBeCloseTo((4 + 6 + 10) / 3, 9);
  });
  test("baseline means only BTC/ETH valid rows", () => {
    const b = baselineGrowthEstimate(rows);
    expect(b.symbols).toEqual(["BTC/USDT:USDT", "ETH/USDT:USDT"]);
    expect(b.meanReturnPct).toBeCloseTo(5, 9);
  });
  test("empty union is zero, not NaN", () => {
    expect(unionGrowthEstimate([]).meanReturnPct).toBe(0);
    expect(baselineGrowthEstimate([]).symbols).toEqual([]);
  });
});
