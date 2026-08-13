import { describe, expect, it } from "bun:test";
import type { CandleLike } from "./types.js";
import {
  READINESS_STRESS_SEEDS,
  bootstrapBlockConfidence,
  validateCandleDataQuality,
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

  it("accepts 5m-spaced candles when timeframeMinutes is set", () => {
    const series: CandleLike[] = [];
    let price = 100;
    const start = new Date("2026-01-01T00:00:00.000Z");
    for (let index = 0; index < 120; index += 1) {
      const direction = index % 2 === 0 ? 1 : -1;
      const open = price;
      const close = open + direction * 0.35;
      series.push({
        open,
        high: Math.max(open, close) + 0.4,
        low: Math.min(open, close) - 0.4,
        close,
        volume: 10,
        timestamp: new Date(start.getTime() + index * 5 * 60 * 1000),
      });
      price = close;
    }
    const result = validateGridEvidence(series, {
      ...options(new Date("2026-01-02T06:00:00.000Z")),
      timeframeMinutes: 5,
    });
    expect(result.kind).toBe("ok");
    // The same series must FAIL the default 15m spacing check.
    const as15m = validateGridEvidence(
      series,
      options(new Date("2026-01-02T06:00:00.000Z")),
    );
    expect(as15m.kind).toBe("invalid");
  });

  it("pools the 5-seed sequence for the stress LB (amendment 2026-08-07)", () => {
    const series = candles(120);
    const result = validateGridEvidence(
      series,
      options(new Date("2026-01-02T06:00:00.000Z")),
    );
    expect(result.kind).toBe("ok");
    if (result.kind !== "ok") return;

    const pooled = bootstrapBlockConfidence(
      result.stress.runs.flatMap((run) =>
        run.result.trades.map((trade) => trade.pnlPct.toString()),
      ),
      20260802,
    );
    // The gate LB is the bootstrap over the combined seed sequence, in seed
    // order — not the worst per-seed interval.
    expect(result.stress.pooledLowerBoundPct).toBe(pooled.lowerBoundPct);
    expect(Number.isFinite(result.stress.pooledLowerBoundPct)).toBe(true);
    expect(pooled.sampleCount).toBe(
      result.stress.runs.reduce((sum, run) => sum + run.result.totalTrades, 0),
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

  it("produces a non-degenerate interval (regression: all resamples were identical)", () => {
    // High-variance input: a degenerate bootstrap (same seed for every run)
    // collapses to width 0 and makes the confidence gate compare the raw
    // block mean against zero — fail-open vs the intended 95% lower bound.
    const values = [
      "5",
      "-5",
      "4",
      "-4",
      "3",
      "-3",
      "2",
      "-2",
      "1",
      "-1",
      "5",
      "-5",
      "4",
      "-4",
      "3",
      "-3",
      "2",
      "-2",
      "1",
      "-1",
      "5",
      "-5",
      "4",
      "-4",
      "3",
      "-3",
      "2",
      "-2",
      "1",
      "-1",
    ];
    for (const seed of [20260802, 20260803, 1, 42]) {
      const result = bootstrapBlockConfidence(values, seed, 5, 5000);
      expect(result.lowerBoundPct).toBeLessThan(result.upperBoundPct);
    }
  });

  it("matches the checked-in golden vector (independent xorshift32/block implementation)", () => {
    // Reference values computed with an independent Python implementation of
    // the plan protocol (xorshift32, block length 5, seed+run state, 5000
    // resamples, floor/ceil linear-interpolated 0.025/0.975 quantiles).
    const values = [
      "5",
      "-5",
      "4",
      "-4",
      "3",
      "-3",
      "2",
      "-2",
      "1",
      "-1",
      "5",
      "-5",
      "4",
      "-4",
      "3",
      "-3",
      "2",
      "-2",
      "1",
      "-1",
      "5",
      "-5",
      "4",
      "-4",
      "3",
      "-3",
      "2",
      "-2",
      "1",
      "-1",
    ];
    const result = bootstrapBlockConfidence(values, 20260802, 5, 5000);
    expect(result.lowerBoundPct).toBe(-0.5333333333333333);
    expect(result.upperBoundPct).toBe(0.5);
    expect(result.sampleCount).toBe(30);
    expect(result.resamples).toBe(5000);
    expect(result.blockLength).toBe(5);
    expect(result.seed).toBe(20260802);
  });

  it("accepts exponent-form values at the bootstrap boundary (valid finite pnlPct)", () => {
    // pnlPct.toString() legitimately serializes tiny values in exponential
    // notation (e.g. "1e-7"); rejecting them crashed the whole universe
    // scan on valid evidence (2026-08-10). Exponent-form strings are finite
    // numbers and must bootstrap like any other evidence.
    const result = bootstrapBlockConfidence(
      ["1e-3", "2", "3", "4", "5", "6"],
      20260802,
      5,
      10,
    );
    expect(Number.isFinite(result.lowerBoundPct)).toBe(true);
    expect(Number.isFinite(result.upperBoundPct)).toBe(true);
    expect(result.sampleCount).toBe(6);
    expect(result.resamples).toBe(10);
  });

  it("rejects non-finite bootstrap input and a zero seed", () => {
    expect(() =>
      bootstrapBlockConfidence(
        ["1", "2", "NaN", "4", "5", "6"],
        20260802,
        5,
        10,
      ),
    ).toThrow("invalid block-bootstrap input");
    expect(() =>
      bootstrapBlockConfidence(["1", "2", "3", "4", "5", "6"], 0, 5, 10),
    ).toThrow("invalid block-bootstrap input");
    expect(() =>
      bootstrapBlockConfidence(["1", "2"], 20260802, 5, 10),
    ).not.toThrow();
    // Fewer samples than one block is no longer an error: the stress path
    // needs degenerate evidence instead of a crash (2026-08-09).
    expect(
      bootstrapBlockConfidence(["1", "2"], 20260802, 5, 10).resamples,
    ).toBe(0);
  });

  it("returns a degenerate zero-width confidence for fewer than one block of samples", () => {
    const result = bootstrapBlockConfidence(["1", "2", "3"], 20260802, 5, 10);
    expect(result.sampleCount).toBe(3);
    expect(result.resamples).toBe(0);
    expect(result.lowerBoundPct).toBe(result.upperBoundPct);
    expect(result.lowerBoundPct).toBeCloseTo(2, 10); // mean(1, 2, 3)
  });

  it("returns a degenerate confidence for an empty sample without throwing", () => {
    const result = bootstrapBlockConfidence([], 20260802, 5, 10);
    expect(result.sampleCount).toBe(0);
    expect(result.resamples).toBe(0);
    expect(result.lowerBoundPct).toBe(0);
    expect(result.upperBoundPct).toBe(0);
  });

  it("rejects candles with negative volume, invalid timestamps, and bad OHLC structure", () => {
    const now = new Date("2026-01-02T06:00:00.000Z");
    const series = candles(120);

    const negativeVolume = validateGridEvidence(
      series.map((row, index) => (index === 5 ? { ...row, volume: -1 } : row)),
      options(now),
    );
    expect(negativeVolume.kind).toBe("invalid");
    if (negativeVolume.kind === "invalid") {
      expect(negativeVolume.failures).toContain(
        "candle 5 contains negative volume",
      );
    }

    const badTimestamp = validateGridEvidence(
      series.map((row, index) =>
        index === 5 ? { ...row, timestamp: new Date(Number.NaN) } : row,
      ),
      options(now),
    );
    expect(badTimestamp.kind).toBe("invalid");

    const highBelowClose = validateGridEvidence(
      series.map((row, index) =>
        index === 5 ? { ...row, high: Math.min(row.open, row.close) - 1 } : row,
      ),
      options(now),
    );
    expect(highBelowClose.kind).toBe("invalid");
    if (highBelowClose.kind === "invalid") {
      expect(highBelowClose.failures).toContain(
        "candle 5 high is below open or close",
      );
    }

    const lowAboveOpen = validateGridEvidence(
      series.map((row, index) =>
        index === 5 ? { ...row, low: Math.max(row.open, row.close) + 1 } : row,
      ),
      options(now),
    );
    expect(lowAboveOpen.kind).toBe("invalid");
    if (lowAboveOpen.kind === "invalid") {
      expect(lowAboveOpen.failures).toContain(
        "candle 5 low is above open or close",
      );
    }

    const nonPositivePrice = validateGridEvidence(
      series.map((row, index) => (index === 5 ? { ...row, close: 0 } : row)),
      options(now),
    );
    expect(nonPositivePrice.kind).toBe("invalid");
    if (nonPositivePrice.kind === "invalid") {
      expect(nonPositivePrice.failures).toContain(
        "candle 5 contains a non-positive price",
      );
    }
  });

  it("rejects a candle whose timestamp is not exactly 15m after the previous", () => {
    const now = new Date("2026-01-02T06:00:00.000Z");
    const series = candles(120);
    const gapped = series.map((row, index) =>
      index === 10
        ? {
            ...row,
            timestamp: new Date(row.timestamp.getTime() + 10 * 60 * 1000),
          }
        : row,
    );

    const result = validateGridEvidence(gapped, options(now));
    expect(result.kind).toBe("invalid");
    if (result.kind === "invalid") {
      expect(
        result.failures.some((failure) => failure.includes("not exactly 15m")),
      ).toBe(true);
    }
  });

  it("reports insufficient complete windows in the data-quality gate", () => {
    const now = new Date("2026-01-02T06:00:00.000Z");
    // 40 candles with train=20/test=20 yields exactly 1 complete window —
    // below the minimum of 2 required by the test options.
    const result = validateGridEvidence(candles(40), options(now));

    expect(result.kind).toBe("invalid");
    if (result.kind === "invalid") {
      expect(result.failures).toContain("complete window count is below 2");
    }
  });

  it("rejects a monotonic downtrend cohort as unprofitable", () => {
    const now = new Date("2026-01-02T06:00:00.000Z");
    // A monotonic downtrend forces every window's grid to stop out repeatedly
    // until capital is exhausted, so the cohort must be rejected as
    // unprofitable — either for a -100% compounded return, too few fixed-OOS
    // trades, or an insufficient window count. It must never be accepted.
    const downtrend = Array.from({ length: 120 }, (_, index) => {
      const close = 100 * (1 - index * 0.002);
      const open = index === 0 ? close : close * 1.002;
      return {
        open,
        high: Math.max(open, close) * 1.001,
        low: Math.min(open, close) * 0.999,
        close,
        volume: 10,
        timestamp: new Date("2026-01-01T00:00:00.000Z"),
      } satisfies CandleLike;
    }).map((row, index) => ({
      ...row,
      timestamp: new Date(
        new Date("2026-01-01T00:00:00.000Z").getTime() + index * 15 * 60 * 1000,
      ),
    }));

    const result = validateGridEvidence(downtrend, options(now));
    expect(result.kind).toBe("invalid");
    if (result.kind === "invalid") {
      // The rejection must be a profitability/quality failure — never a pass
      // with a plausible-but-false profit claim.
      expect(
        result.failures.some(
          (failure) =>
            failure === "historical return is at or below -100%" ||
            failure.startsWith("fixed OOS trade count is below") ||
            failure.includes("complete window count"),
        ),
      ).toBe(true);
    }
  });

  it("rejects evidence whose fixed OOS trade count is below the floor", () => {
    const now = new Date("2026-01-02T06:00:00.000Z");
    // With a huge grid step, virtually no trades occur in the OOS window,
    // forcing the fixed-OOS trade-count floor to reject the cohort.
    const result = validateGridEvidence(candles(120), {
      ...options(now),
      minimumFixedOosTrades: 1000,
      grid: { ...options(now).grid, gridStepPct: 10 },
    });

    expect(result.kind).toBe("invalid");
    if (result.kind === "invalid") {
      expect(
        result.failures.some((failure) =>
          failure.startsWith("fixed OOS trade count is below"),
        ),
      ).toBe(true);
    }
  });

  it("produces deterministic adverse-selection stress runs per seed", () => {
    const series = candles(120);
    const config = options(new Date("2026-01-02T06:00:00.000Z"));
    const first = validateGridEvidence(series, config);
    const second = validateGridEvidence(series, config);

    expect(first.kind).toBe("ok");
    if (first.kind !== "ok" || second.kind !== "ok") return;

    expect(first.stress.seeds).toEqual([...READINESS_STRESS_SEEDS]);
    expect(JSON.stringify(first.stress)).toBe(JSON.stringify(second.stress));
    // Stress runs apply adverse selection (maker fill probability 0.7) — they
    // must never report a better worst-case than the clean fixed-OOS run.
    expect(first.stress.worstReturnPct).toBeLessThanOrEqual(
      first.fixedOos.totalReturnPct,
    );
  });

  it("reports the complete fixed-OOS and confidence evidence on success", () => {
    const series = candles(120);
    const result = validateGridEvidence(
      series,
      options(new Date("2026-01-02T06:00:00.000Z")),
    );

    expect(result.kind).toBe("ok");
    if (result.kind !== "ok") return;

    expect(result.fixedOos.totalTrades).toBeGreaterThan(0);
    expect(result.fixedOos.winRate).toBeGreaterThanOrEqual(0);
    expect(result.fixedOos.winRate).toBeLessThanOrEqual(100);
    expect(result.confidence.sampleCount).toBe(result.fixedOos.totalTrades);
    expect(result.confidence.lowerBoundPct).toBeLessThanOrEqual(
      result.confidence.upperBoundPct,
    );
    expect(result.confidence.seed).toBe(20260802);
    expect(result.confidence.resamples).toBe(5000);
    expect(result.confidence.blockLength).toBe(5);
    expect(result.executionParity).toEqual({
      passed: true,
      protocolVersion: "execution-parity/v1",
    });
  });

  it("validateCandleDataQuality flags empty, future, and stale series directly", () => {
    const empty = validateCandleDataQuality(
      [],
      new Date("2026-01-02T06:00:00.000Z"),
      20,
      20,
      2,
    );
    expect(empty.valid).toBe(false);
    expect(empty.failures).toContain("candle evidence is empty");

    const future = candles(120);
    const futureResult = validateCandleDataQuality(
      future,
      new Date("2025-12-31T00:00:00.000Z"),
      20,
      20,
      2,
    );
    expect(futureResult.valid).toBe(false);
    expect(futureResult.failures).toContain("latest candle is in the future");

    const stale = validateCandleDataQuality(
      candles(120, new Date("2026-01-01T00:00:00.000Z")),
      new Date("2026-02-10T00:00:00.000Z"),
      20,
      20,
      2,
    );
    expect(stale.valid).toBe(false);
    expect(stale.failures).toContain("latest candle is stale");
  });
});
