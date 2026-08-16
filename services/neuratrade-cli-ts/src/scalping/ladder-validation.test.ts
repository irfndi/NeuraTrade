import { describe, expect, it } from "bun:test";
import type { CandleLike } from "./types.js";
import { bootstrapBlockConfidence } from "./grid-validation.js";
import {
  validateLadderEvidence,
  type LadderValidationOptions,
} from "./ladder-validation.js";

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

function options(now: Date): LadderValidationOptions {
  return {
    now,
    trainBars: 20,
    testBars: 20,
    minimumWindows: 2,
    minimumFixedOosTrades: 5,
    ladder: {
      rungs: 2,
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

describe("deterministic ladder validation", () => {
  it("produces populated rolling, compounded, confidence, and stress evidence", () => {
    const series = candles(120);
    const result = validateLadderEvidence(
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
    // Every window and stress run must actually trade through the ladder.
    expect(
      result.historical.windows.every((w) => w.result.totalTrades > 0),
    ).toBe(true);
  });

  it("is byte-stable for repeated fixed-candidate evaluation", () => {
    const series = candles(120);
    const config = options(new Date("2026-01-02T06:00:00.000Z"));

    expect(JSON.stringify(validateLadderEvidence(series, config))).toBe(
      JSON.stringify(validateLadderEvidence(series, config)),
    );
  });

  it("pools the 5-seed sequence for the stress LB (grid amendment 2026-08-07)", () => {
    const series = candles(120);
    const result = validateLadderEvidence(
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
    expect(result.stress.pooledLowerBoundPct).toBe(pooled.lowerBoundPct);
    expect(Number.isFinite(result.stress.pooledLowerBoundPct)).toBe(true);
    expect(pooled.sampleCount).toBe(
      result.stress.runs.reduce((sum, run) => sum + run.result.totalTrades, 0),
    );
  });

  it("rejects empty, duplicate, gapped, stale, and malformed evidence", () => {
    const now = new Date("2026-01-02T06:00:00.000Z");
    expect(validateLadderEvidence([], options(now)).kind).toBe("invalid");
    expect(
      validateLadderEvidence(
        candles(120).toSpliced(10, 0, candles(1)[0]),
        options(now),
      ).kind,
    ).toBe("invalid");
    expect(
      validateLadderEvidence(candles(120).toSpliced(10, 1), options(now)).kind,
    ).toBe("invalid");
    expect(
      validateLadderEvidence(
        candles(120).map((row, index) =>
          index === 10 ? { ...row, close: Number.NaN } : row,
        ),
        options(now),
      ).kind,
    ).toBe("invalid");
    expect(
      validateLadderEvidence(
        candles(120),
        options(new Date("2026-01-05T00:00:00.000Z")),
      ).kind,
    ).toBe("invalid");
  });

  it("fails closed when the fixed-OOS ladder produces too few trades", () => {
    const now = new Date("2026-01-02T06:00:00.000Z");
    // A step far wider than the ±0.4 swing never fills -> near-zero trades,
    // which must be a typed invalid failure, not a zero-valued pass.
    const result = validateLadderEvidence(candles(120), {
      ...options(now),
      ladder: {
        ...options(now).ladder,
        gridStepPct: 50,
        rungs: 1,
      },
    });
    expect(result.kind).toBe("invalid");
    if (result.kind !== "ok") {
      expect(result.failures.join(" ")).toContain("fixed OOS trade count");
    }
  });
});
