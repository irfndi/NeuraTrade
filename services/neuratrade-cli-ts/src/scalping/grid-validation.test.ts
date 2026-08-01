import { describe, expect, it } from "bun:test";
import type { CandleLike } from "./types.js";
import {
  bootstrapBlockConfidence,
  validateGridEvidence,
  type GridValidationOptions,
} from "./grid-validation.js";

function candles(
  count: number,
  start = new Date("2026-01-01T00:00:00.000Z"),
): CandleLike[] {
  const rows: CandleLike[] = [];
  let price = 100;
  for (let index = 0; index < count; index += 1) {
    const direction = index % 2 === 0 ? 1 : -1;
    const open = price;
    const close = open + direction * 0.35;
    rows.push({
      open,
      high: Math.max(open, close) + 0.4,
      low: Math.min(open, close) - 0.4,
      close,
      volume: 10,
      timestamp: new Date(start.getTime() + index * 15 * 60 * 1000),
    });
    price = close;
  }
  return rows;
}

function options(now: Date): GridValidationOptions {
  return {
    now,
    trainBars: 20,
    testBars: 20,
    minimumWindows: 2,
    minimumFixedOosTrades: 5,
    grid: {
      gridStepPct: 0.5,
      gridMaxGrids: 2,
      gridPauseAfterLossBars: 0,
      feePct: 0.01,
      slippageBps: 0,
      initialCapital: 100,
      trendFilterPeriod: 5,
      leverage: 1,
      positionFraction: 0.5,
    },
    executionParityPassed: true,
  };
}

describe("deterministic grid validation", () => {
  it("produces populated rolling, compounded, confidence, and stress evidence", () => {
    const series = candles(120);
    const result = validateGridEvidence(
      series,
      options(new Date("2026-01-02T06:00:00.000Z")),
    );

    expect(result.kind).toBe("ok");
    if (result.kind !== "ok") return;
    expect(result.historical.windows.length).toBeGreaterThanOrEqual(2);
    expect(Number.isFinite(result.historical.compoundedReturnPct)).toBe(true);
    expect(result.confidence.resamples).toBe(5000);
    expect(result.stress.seeds).toEqual([
      20260802, 20260803, 20260804, 20260805, 20260806,
    ]);
    expect(result.stress.runs).toHaveLength(5);
    expect(result.dataQuality.valid).toBe(true);
  });

  it("is byte-stable for repeated fixed-candidate evaluation", () => {
    const series = candles(120);
    const config = options(new Date("2026-01-02T06:00:00.000Z"));

    expect(JSON.stringify(validateGridEvidence(series, config))).toBe(
      JSON.stringify(validateGridEvidence(series, config)),
    );
  });

  it("rejects empty, duplicate, gapped, stale, and malformed evidence", () => {
    const now = new Date("2026-01-02T06:00:00.000Z");
    expect(validateGridEvidence([], options(now)).kind).toBe("invalid");
    expect(
      validateGridEvidence(
        candles(120).toSpliced(10, 0, candles(1)[0]),
        options(now),
      ).kind,
    ).toBe("invalid");
    expect(
      validateGridEvidence(candles(120).toSpliced(10, 1), options(now)).kind,
    ).toBe("invalid");
    expect(
      validateGridEvidence(
        candles(120).map((row, index) =>
          index === 10 ? { ...row, close: Number.NaN } : row,
        ),
        options(now),
      ).kind,
    ).toBe("invalid");
    expect(
      validateGridEvidence(
        candles(120),
        options(new Date("2026-01-05T00:00:00.000Z")),
      ).kind,
    ).toBe("invalid");
  });

  it("uses the specified moving-block bootstrap protocol", () => {
    const values = ["1", "2", "3", "4", "5", "6", "7", "8", "9", "10"];
    const first = bootstrapBlockConfidence(values, 20260802, 5, 5000);
    const second = bootstrapBlockConfidence(values, 20260802, 5, 5000);

    expect(first).toEqual(second);
    expect(first.resamples).toBe(5000);
    expect(first.lowerBoundPct).toBeLessThanOrEqual(first.upperBoundPct);
  });
});
