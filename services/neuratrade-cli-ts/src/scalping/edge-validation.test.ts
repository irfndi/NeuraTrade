import { describe, expect, it } from "bun:test";
import * as fc from "fast-check";
import {
  EG_CRITICAL_5PCT_2VAR,
  assessEdge,
  barrierHitRate,
  costAdjustedBreakeven,
  engleGrangerAdf,
  isCointegrated,
  ols,
  varianceRatio,
} from "./edge-validation.js";

/** Deterministic pseudo-random walk (no Math.random, reproducible). */
function walk(
  seed: number,
  n: number,
  start: number,
  volPct: number,
): number[] {
  let p = start;
  let s = seed;
  const r = () => {
    s = (s * 1103515245 + 12345) % 2147483648;
    return s / 2147483648;
  };
  return Array.from({ length: n }, () => {
    p = Math.max(0.01, p * (1 + (r() - 0.5) * 2 * volPct));
    return p;
  });
}

describe("costAdjustedBreakeven", () => {
  it("matches hand arithmetic for the live champion geometry", () => {
    // target 1.95 * 1.3% = 2.535%, stop 2 * 1.3% = 2.6%
    // win leg  = 2.535 - (0.02*2 + 0.02*2) = 2.455
    // loss leg = 2.600 + (0.02 + 0.06 + 0.02*2) = 2.720
    const be = costAdjustedBreakeven(0.0195 * 1.3, 0.02 * 1.3, {
      makerFeePct: 0.02,
      takerExitFeePct: 0.06,
      slippageBps: 2,
    });
    expect(be).toBeCloseTo(2.72 / (2.455 + 2.72), 10);
    expect(be * 100).toBeGreaterThan(52);
  });

  it("is always above the gross 1/(1+R) breakeven, because costs only hurt", () => {
    for (const r of [0.5, 1, 2, 3]) {
      const target = 0.02 * r;
      const stop = 0.02;
      const gross = 1 / (1 + target / stop);
      const net = costAdjustedBreakeven(target, stop, {
        makerFeePct: 0.02,
        takerExitFeePct: 0.06,
        slippageBps: 2,
      });
      expect(net).toBeGreaterThan(gross);
    }
  });

  it("rejects a target too small to cover its own round trip", () => {
    expect(() =>
      costAdjustedBreakeven(0.0001, 0.02, {
        makerFeePct: 0.02,
        takerExitFeePct: 0.06,
        slippageBps: 2,
      }),
    ).toThrow(RangeError);
  });
});

describe("barrierHitRate", () => {
  it("reports ~50% for a symmetric barrier on a random walk", () => {
    // This is the finding that killed the grid: symmetric barriers cannot
    // clear a 50% breakeven, let alone a cost-adjusted one above it.
    const rates: number[] = [];
    for (const seed of [7, 11, 42, 99, 123]) {
      const px = walk(seed, 6000, 100, 0.006);
      const r = barrierHitRate(px, {
        targetPct: 0.013,
        stopPct: 0.013,
        horizonBars: 200,
      });
      rates.push(r.hitRate);
    }
    const mean = rates.reduce((a, b) => a + b, 0) / rates.length;
    expect(mean).toBeGreaterThan(0.45);
    expect(mean).toBeLessThan(0.55);
  });

  it("defaults the stride to the horizon, so samples never overlap", () => {
    const px = walk(7, 4000, 100, 0.006);
    const opts = { targetPct: 0.013, stopPct: 0.013, horizonBars: 100 };
    const overlapping = barrierHitRate(px, { ...opts, stride: 1 });
    const nonOverlapping = barrierHitRate(px, opts);
    // Overlap buys many more samples; that is precisely the inflation risk.
    expect(
      overlapping.targetFirst + overlapping.stopFirst + overlapping.undecided,
    ).toBeGreaterThan(
      nonOverlapping.targetFirst +
        nonOverlapping.stopFirst +
        nonOverlapping.undecided,
    );
  });

  it("uses only bars strictly after the observation (no look-ahead)", () => {
    // Price jumps at index 1 only. A sample taken at index 1 must not see it.
    const px = [100, 200, 200, 200, 200];
    const r = barrierHitRate(px, {
      targetPct: 0.5,
      stopPct: 0.5,
      horizonBars: 2,
      stride: 1,
    });
    // i=0 sees the jump -> target. i=1 and i=2 have a flat forward path.
    expect(r.targetFirst).toBe(1);
    expect(r.undecided).toBe(2);
  });

  it("a wider target is strictly harder to hit (monotonicity)", () => {
    // The property a strategy author actually relies on: raising the target
    // while holding the stop fixed cannot increase the hit rate. A barrier test
    // that violated this would silently flatter any tuned target.
    const px = walk(42, 4000, 100, 0.008);
    const rates = [0.005, 0.01, 0.02, 0.04].map(
      (targetPct) =>
        barrierHitRate(px, { targetPct, stopPct: 0.01, horizonBars: 100 })
          .hitRate,
    );
    for (let i = 1; i < rates.length; i += 1) {
      expect(rates[i]).toBeLessThanOrEqual(rates[i - 1] + 1e-12);
    }
  });

  it("returns a hit rate inside [0,1] for arbitrary input", () => {
    fc.assert(
      fc.property(
        fc.array(fc.double({ min: 0.5, max: 1000, noNaN: true }), {
          minLength: 50,
          maxLength: 300,
        }),
        fc.double({ min: 0.001, max: 0.1, noNaN: true }),
        (px, pct) => {
          const r = barrierHitRate(px, {
            targetPct: pct,
            stopPct: pct,
            horizonBars: 20,
          });
          return r.hitRate >= 0 && r.hitRate <= 1;
        },
      ),
      { numRuns: 100 },
    );
  });

  it("rejects nonsense options instead of returning a silent zero", () => {
    expect(() =>
      barrierHitRate([1, 2, 3], { targetPct: 0, stopPct: 1, horizonBars: 1 }),
    ).toThrow(RangeError);
    expect(() =>
      barrierHitRate([1, 2, 3], {
        targetPct: 1,
        stopPct: 1,
        horizonBars: 1,
        stride: 0,
      }),
    ).toThrow(RangeError);
  });
});

describe("assessEdge", () => {
  const be = 0.5256;

  it("reports the overlap-inflation trap as inconclusive, not edge", () => {
    // The real shape of the false positive: a small margin with few samples.
    const r = barrierHitRate(walk(7, 1200, 100, 0.006), {
      targetPct: 0.013,
      stopPct: 0.013,
      horizonBars: 200,
    });
    const verdict = assessEdge(r, be);
    expect(verdict.verdict).not.toBe("edge");
  });

  it("calls a clearly good hit rate an edge", () => {
    const verdict = assessEdge(
      {
        targetFirst: 700,
        stopFirst: 300,
        undecided: 0,
        hitRate: 0.7,
        standardError: Math.sqrt((0.7 * 0.3) / 1000),
      },
      be,
    );
    expect(verdict.verdict).toBe("edge");
    expect(verdict.marginPp).toBeGreaterThan(0);
  });

  it("calls a clearly bad hit rate no-edge", () => {
    const verdict = assessEdge(
      {
        targetFirst: 400,
        stopFirst: 600,
        undecided: 0,
        hitRate: 0.4,
        standardError: Math.sqrt((0.4 * 0.6) / 1000),
      },
      be,
    );
    expect(verdict.verdict).toBe("no-edge");
  });

  it("says inconclusive when there is no data, rather than guessing", () => {
    const verdict = assessEdge(
      {
        targetFirst: 0,
        stopFirst: 0,
        undecided: 0,
        hitRate: 0,
        standardError: 0,
      },
      be,
    );
    expect(verdict.verdict).toBe("inconclusive");
    expect(verdict.samples).toBe(0);
  });
});

describe("varianceRatio", () => {
  it("is near 1 for a random walk", () => {
    const px = walk(7, 20000, 100, 0.006);
    for (const q of [4, 16]) {
      const vr = varianceRatio(px, q);
      expect(vr).toBeGreaterThan(0.8);
      expect(vr).toBeLessThan(1.2);
    }
  });

  it("is below 1 for a genuinely mean-reverting series", () => {
    // AR(1) with negative coefficient: x_t = -0.5 * x_{t-1} + noise, so price
    // oscillates around its level rather than trending.
    const px: number[] = [];
    let level = 0;
    let s = 7;
    const r = () => {
      s = (s * 1103515245 + 12345) % 2147483648;
      return s / 2147483648 - 0.5;
    };
    for (let i = 0; i < 20000; i++) {
      level = -0.5 * level + r() * 0.01;
      px.push(100 * Math.exp(level));
    }
    const vr = varianceRatio(px, 16);
    expect(vr).toBeLessThan(0.8);
  });

  it("is above 1 for trending returns", () => {
    // Persistence must live in RETURNS and accumulate into the level. A positive
    // AR(1) on the level itself is stationary (it oscillates inside a band), so
    // it mean-reverts: that is what produced VR 0.12 for a series I had labelled
    // "trending".
    const build = (coef: number): number[] => {
      let seed = 11;
      let prev = 0;
      let level = 0;
      const px: number[] = [];
      const next = () => {
        seed = (seed * 1103515245 + 12345) % 2147483648;
        return seed / 2147483648 - 0.5;
      };
      for (let i = 0; i < 20000; i++) {
        prev = coef * prev + next() * 0.01;
        level += prev;
        px.push(100 * Math.exp(level));
      }
      return px;
    };
    expect(varianceRatio(build(0.5), 16)).toBeGreaterThan(1.2);
    expect(varianceRatio(build(-0.5), 16)).toBeLessThan(0.8);
  });

  it("returns NaN instead of a number when it cannot be computed", () => {
    expect(varianceRatio([1, 2, 3], 16)).toBeNaN();
    expect(varianceRatio([100, 100, 100, 100], 2)).toBeNaN();
  });
});

describe("ols", () => {
  it("recovers an exact linear relationship", () => {
    const x = [1, 2, 3, 4, 5];
    const y = x.map((v) => 3 + 2 * v);
    const fit = ols(y, x);
    expect(fit.intercept).toBeCloseTo(3, 10);
    expect(fit.slope).toBeCloseTo(2, 10);
  });

  it("refuses a constant x rather than returning NaN", () => {
    expect(() => ols([1, 2, 3], [5, 5, 5])).toThrow(RangeError);
  });

  it("rejects mismatched lengths", () => {
    expect(() => ols([1, 2], [1, 2, 3])).toThrow(RangeError);
  });
});

describe("engleGrangerAdf / isCointegrated", () => {
  /** Deterministic noise in [-0.5, 0.5). */
  function noiseFactory(seed: number) {
    let s = seed;
    return () => {
      s = (s * 1103515245 + 12345) % 2147483648;
      return s / 2147483648 - 0.5;
    };
  }

  it("finds cointegration when the spread really is stationary", () => {
    // Two series sharing a common stochastic trend, plus a mean-reverting gap.
    // This is the case a pairs trader would legitimately bet on.
    const next = noiseFactory(7);
    const common: number[] = [];
    const gap: number[] = [];
    let level = 0;
    let g = 0;
    for (let i = 0; i < 3000; i += 1) {
      level += next() * 0.01;
      g = 0.7 * g + next() * 0.002; // strongly mean-reverting gap
      common.push(level);
      gap.push(g);
    }
    const y = common.map((c, i) => c + gap[i]);
    const x = common.map((c) => c);
    const { intercept, slope } = ols(y, x);
    const resid = y.map((v, i) => v - (intercept + slope * x[i]));
    expect(isCointegrated(resid)).toBe(true);
  });

  it("rejects a spread built from two independent random walks", () => {
    // No shared trend: the residual is a random walk and must not pass. This is
    // the exact false positive that makes naive pairs screens look profitable.
    const next = noiseFactory(42);
    const a: number[] = [];
    const b: number[] = [];
    let la = 0;
    let lb = 0;
    for (let i = 0; i < 3000; i += 1) {
      la += next() * 0.01;
      lb += next() * 0.01;
      a.push(la);
      b.push(lb);
    }
    const { intercept, slope } = ols(a, b);
    const resid = a.map((v, i) => v - (intercept + slope * b[i]));
    expect(isCointegrated(resid)).toBe(false);
  });

  it("returns undefined instead of a number when the sample is too short", () => {
    expect(engleGrangerAdf([1, 2, 3])).toBeUndefined();
  });

  it("uses the Engle-Granger critical value, not the single-series ADF one", () => {
    // The distinction matters: a t of -3.0 passes a naive -2.86 threshold and
    // fails the correct -3.34 one. Getting this wrong is how a non-tradeable
    // spread gets waved through.
    expect(EG_CRITICAL_5PCT_2VAR).toBeLessThan(-2.86);
    const borderline = { t: -3.0 };
    expect(borderline.t < EG_CRITICAL_5PCT_2VAR).toBe(false);
  });
});
