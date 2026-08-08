import { describe, expect, it } from "bun:test";
import * as fc from "fast-check";
import {
  accountScaledTargetFillsPerDay,
  barsPerDayForTimeframe,
  computeFillFrequencyPct,
  passesStage2Screen,
  selectUniversePortfolio,
  STAGE2_MAX_ATR_PCT,
  STAGE2_MIN_ADX,
  STAGE2_MIN_ATR_PCT,
  type GridUniverseEntry,
} from "./grid-universe.js";

const candle = (open: number, high: number, low: number) => ({
  open,
  high,
  low,
});

const entry = (
  symbol: string,
  edgePerTradePct: number,
  fillsPerDay: number,
): GridUniverseEntry => ({
  symbol,
  candles: 100,
  bestParams: {
    gridStepPct: 0.5,
    gridMaxGrids: 2,
    gridPauseAfterLossBars: 0,
  },
  walkForward: {
    windows: [],
    aggregateReturnPct: edgePerTradePct * 10,
    profitableWindowsPct: 100,
    maxDrawdownPct: 0,
    totalTrades: 10,
  },
  passed: true,
  volatility: 1,
  oosTrades: 10,
  fillsPerDay,
  edgePerTradePct,
});

describe("computeFillFrequencyPct", () => {
  it("reports 100 when the gate is disabled, regardless of candle range", () => {
    const candles = [
      candle(100, 100.5, 99.5), // never touches a 2% step
      candle(100, 101.5, 98.5),
    ];
    expect(computeFillFrequencyPct(candles, 2, 0)).toBe(100);
  });

  it("reports 100 for an empty candle set", () => {
    expect(computeFillFrequencyPct([], 2, 50)).toBe(100);
  });

  it("counts a candle that reaches the grid step downward (buy fill)", () => {
    const candles = [
      candle(100, 100.1, 97.9), // low <= 100 * 0.98 -> touched
      candle(100, 100.1, 99), // never reaches step
    ];
    expect(computeFillFrequencyPct(candles, 2, 50)).toBe(50);
  });

  it("counts a candle that reaches the grid step upward (sell fill)", () => {
    const candles = [
      candle(100, 102.1, 99.9), // high >= 100 * 1.02 -> touched
      candle(100, 101, 99.1), // never reaches step
    ];
    expect(computeFillFrequencyPct(candles, 2, 50)).toBe(50);
  });

  it("a candle reaching the step in either direction counts once", () => {
    const candles = [
      candle(100, 102.1, 97.9), // both directions -> still one touch
      candle(100, 100.5, 99.5), // no touch
      candle(100, 98.5, 97.5), // low <= 98 -> touch
    ];
    expect(computeFillFrequencyPct(candles, 2, 50)).toBeCloseTo(66.67, 2);
  });

  it("compares the percentage against a 0-100 scale, not a 0..1 fraction", () => {
    // All candles touch -> 100, which passes a 80% gate but would fail a 0.8
    // fraction gate (0.8 > 1) if the scale were misread. This guards against
    // regressing the doc'd 0-100 % semantics.
    const candles = [candle(100, 102.1, 97.9)];
    expect(computeFillFrequencyPct(candles, 2, 80)).toBe(100);
  });

  it("stays within 0..100 and never rises as the grid step widens", () => {
    const arbCandle = fc
      .record({
        open: fc.double({ min: 1, max: 1000, noNaN: true }),
        up: fc.double({ min: 0, max: 50, noNaN: true }),
        down: fc.double({ min: 0, max: 50, noNaN: true }),
      })
      .map(({ open, up, down }) => ({
        open,
        high: open + up,
        low: open - down,
      }));
    fc.assert(
      fc.property(
        fc.array(arbCandle, { minLength: 1, maxLength: 50 }),
        fc.double({ min: 0.1, max: 5, noNaN: true }),
        (candles, step) => {
          const narrow = computeFillFrequencyPct(candles, step, 1);
          const wide = computeFillFrequencyPct(candles, step * 2, 1);
          expect(narrow).toBeGreaterThanOrEqual(0);
          expect(narrow).toBeLessThanOrEqual(100);
          expect(wide).toBeLessThanOrEqual(narrow);
        },
      ),
    );
  });
});

describe("passesStage2Screen", () => {
  it("accepts a trending, liquid-volatility candidate", () => {
    expect(passesStage2Screen({ adx14: 25, atr14Pct: 1 })).toBe(true);
  });

  it("accepts boundary values exactly at the thresholds", () => {
    expect(
      passesStage2Screen({
        adx14: STAGE2_MIN_ADX,
        atr14Pct: STAGE2_MIN_ATR_PCT,
      }),
    ).toBe(true);
    expect(
      passesStage2Screen({
        adx14: STAGE2_MIN_ADX,
        atr14Pct: STAGE2_MAX_ATR_PCT,
      }),
    ).toBe(true);
  });

  it("rejects chop (ADX below 15)", () => {
    expect(passesStage2Screen({ adx14: STAGE2_MIN_ADX - 1, atr14Pct: 1 })).toBe(
      false,
    );
  });

  it("rejects dead markets (ATR% below 0.02)", () => {
    expect(
      passesStage2Screen({ adx14: STAGE2_MIN_ADX, atr14Pct: 0.019 }),
    ).toBe(false);
  });

  it("rejects moon-shots (ATR% above 10)", () => {
    expect(
      passesStage2Screen({ adx14: STAGE2_MIN_ADX, atr14Pct: 10.001 }),
    ).toBe(false);
  });
});

describe("barsPerDayForTimeframe", () => {
  it("derives bars per day from the timeframe", () => {
    expect(barsPerDayForTimeframe("15m")).toBe(96);
    expect(barsPerDayForTimeframe("5m")).toBe(288);
    expect(barsPerDayForTimeframe("1h")).toBe(24);
    expect(barsPerDayForTimeframe("1d")).toBe(1);
  });

  it("defaults to 15m for unparseable timeframes", () => {
    expect(barsPerDayForTimeframe("fortnight")).toBe(96);
  });
});

describe("accountScaledTargetFillsPerDay", () => {
  it("clamps to the 5/day floor at $100 accounts", () => {
    expect(accountScaledTargetFillsPerDay(100)).toBe(5);
    expect(accountScaledTargetFillsPerDay(10)).toBe(5);
  });

  it("scales linearly in between", () => {
    expect(accountScaledTargetFillsPerDay(200)).toBe(10);
    expect(accountScaledTargetFillsPerDay(500)).toBe(25);
  });

  it("clamps to the 50/day ceiling at $1000+ accounts", () => {
    expect(accountScaledTargetFillsPerDay(1000)).toBe(50);
    expect(accountScaledTargetFillsPerDay(10000)).toBe(50);
  });
});

describe("selectUniversePortfolio", () => {
  it("picks the highest-edge entries first", () => {
    const a = entry("A", 0.2, 10);
    const b = entry("B", 0.9, 10);
    const c = entry("C", 0.5, 10);
    const selected = selectUniversePortfolio([a, b, c], 25);
    expect(selected.map((e) => e.symbol)).toEqual(["B", "C", "A"]);
  });

  it("stops when the cumulative fills/day target is met", () => {
    const a = entry("A", 0.9, 4);
    const b = entry("B", 0.5, 4);
    const c = entry("C", 0.1, 4);
    const selected = selectUniversePortfolio([a, b, c], 5);
    // A + B reach 8 >= 5; C is not needed.
    expect(selected.map((e) => e.symbol)).toEqual(["A", "B"]);
  });

  it("caps each symbol's fills at the per-symbol cap", () => {
    const a = entry("A", 0.9, 100);
    const b = entry("B", 0.5, 100);
    const selected = selectUniversePortfolio([a, b], 15);
    // Each contributes min(100, 10) = 10; two symbols reach 20 >= 15.
    expect(selected.map((e) => e.symbol)).toEqual(["A", "B"]);
  });

  it("never selects entries without a computed edge or fills", () => {
    const a = entry("A", 0.9, 0);
    const b: GridUniverseEntry = { ...entry("B", 0.8, 10), edgePerTradePct: undefined };
    expect(selectUniversePortfolio([a, b], 50)).toEqual([b]);
  });

  it("selects nothing for a zero target", () => {
    expect(selectUniversePortfolio([entry("A", 0.9, 10)], 0)).toEqual([]);
  });
});
