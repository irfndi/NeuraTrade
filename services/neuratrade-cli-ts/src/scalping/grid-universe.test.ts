import { describe, expect, it } from "bun:test";
import * as fc from "fast-check";
import { computeFillFrequencyPct } from "./grid-universe.js";

const candle = (open: number, high: number, low: number) => ({
  open,
  high,
  low,
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
