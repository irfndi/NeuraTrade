import { describe, expect, it } from "bun:test";
import * as fc from "fast-check";
import {
  classifyDeflatedSharpe,
  deflatedSharpe,
  expectedMaxSharpe,
  inverseNormalCdf,
  normalCdf,
  probabilisticSharpe,
  returnMoments,
  sharpeFromReturns,
} from "./deflated-sharpe.js";

describe("inverseNormalCdf", () => {
  // Reference values from the standard normal table (scipy.stats.norm.ppf).
  const table: ReadonlyArray<readonly [number, number]> = [
    [0.5, 0],
    [0.975, 1.959963984540054],
    [0.95, 1.6448536269514722],
    [0.99, 2.3263478740408408],
    [0.999, 3.090232306167813],
    [0.025, -1.959963984540054],
    [0.01, -2.3263478740408408],
    [0.1, -1.2815515655446004],
  ];

  for (const [p, expected] of table) {
    it(`matches scipy for p=${p}`, () => {
      expect(inverseNormalCdf(p)).toBeCloseTo(expected, 8);
    });
  }

  it("is antisymmetric about 0.5", () => {
    for (const p of [0.001, 0.05, 0.2, 0.4]) {
      expect(inverseNormalCdf(p)).toBeCloseTo(-inverseNormalCdf(1 - p), 8);
    }
  });

  it("rejects the open-boundary inputs that have no finite quantile", () => {
    expect(() => inverseNormalCdf(0)).toThrow(RangeError);
    expect(() => inverseNormalCdf(1)).toThrow(RangeError);
    expect(() => inverseNormalCdf(Number.NaN)).toThrow(RangeError);
  });

  it("stays monotonic across the whole domain", () => {
    fc.assert(
      fc.property(
        fc.double({ min: 0.0001, max: 0.9998, noNaN: true }),
        fc.double({ min: 0.0001, max: 0.9998, noNaN: true }),
        (a, b) => {
          const lo = Math.min(a, b);
          const hi = Math.max(a, b);
          if (hi - lo < 1e-9) return true;
          return inverseNormalCdf(lo) <= inverseNormalCdf(hi);
        },
      ),
      { numRuns: 500 },
    );
  });
});

describe("normalCdf", () => {
  it("matches reference values", () => {
    expect(normalCdf(0)).toBeCloseTo(0.5, 7);
    expect(normalCdf(1.96)).toBeCloseTo(0.975002, 6);
    expect(normalCdf(-1.96)).toBeCloseTo(0.024998, 6);
    expect(normalCdf(3)).toBeCloseTo(0.99865, 6);
  });

  it("keeps the tails instead of collapsing them to 0 or 1", () => {
    expect(normalCdf(-6)).toBeGreaterThan(0);
    expect(normalCdf(-6)).toBeLessThan(1e-8);
    expect(normalCdf(6)).toBeGreaterThan(1 - 1e-8);
    expect(normalCdf(6)).toBeLessThan(1);
  });

  it("round-trips with inverseNormalCdf", () => {
    for (const p of [0.02, 0.2, 0.5, 0.8, 0.98]) {
      expect(normalCdf(inverseNormalCdf(p))).toBeCloseTo(p, 5);
    }
  });
});

describe("sharpeFromReturns", () => {
  it("returns the mean over the SAMPLE (n-1) standard deviation", () => {
    // [0, 2]: mean 1, sample sd sqrt(2) => 1/sqrt(2). Using the population sd
    // (n denominator = 1.0) would wrongly give 1.0.
    expect(sharpeFromReturns([0, 2])).toBeCloseTo(1 / Math.SQRT2, 12);
  });

  it("is undefined with too few observations", () => {
    expect(sharpeFromReturns([])).toBeUndefined();
    expect(sharpeFromReturns([0.01])).toBeUndefined();
  });

  it("is undefined for a flat series (zero dispersion)", () => {
    expect(sharpeFromReturns([0.01, 0.01, 0.01])).toBeUndefined();
  });

  it("ignores non-finite observations rather than poisoning the ratio", () => {
    expect(sharpeFromReturns([0, 2, Number.NaN, Infinity])).toBeCloseTo(
      1 / Math.SQRT2,
      12,
    );
  });

  it("is invariant to scaling the returns (ratio statistic)", () => {
    // A Sharpe ratio is scale-free: doubling every return doubles both the mean
    // and the sample sd, so the ratio is unchanged. (This holds for the n-1
    // denominator; the population sd would not have this property and is why
    // that convention must not be used here.)
    const base = [0.01, -0.02, 0.03, 0.005, -0.004];
    const doubled = base.map((r) => r * 2);
    expect(sharpeFromReturns(doubled)).toBeCloseTo(
      sharpeFromReturns(base) as number,
      12,
    );
  });
});

describe("returnMoments", () => {
  it("reports the raw kurtosis convention (3 = normal), not excess", () => {
    // A symmetric 4-point set has kurtosis strictly below 3.
    const { kurtosis } = returnMoments([-1, -1, 1, 1]);
    expect(kurtosis).toBeLessThan(3);
    expect(kurtosis).toBeGreaterThan(0);
  });

  it("reports zero skewness for a symmetric series", () => {
    expect(returnMoments([-2, -1, 1, 2]).skewness).toBeCloseTo(0, 10);
  });

  it("reports positive skewness for a right tail", () => {
    expect(returnMoments([0, 0, 0, 0, 10]).skewness).toBeGreaterThan(0);
  });

  it("falls back to normal moments when dispersion is zero", () => {
    expect(returnMoments([1, 1, 1])).toEqual({ skewness: 0, kurtosis: 3 });
    expect(returnMoments([1])).toEqual({ skewness: 0, kurtosis: 3 });
  });
});

describe("expectedMaxSharpe", () => {
  it("is zero when there is no selection to correct", () => {
    expect(expectedMaxSharpe(0.5, 1)).toBe(0);
    expect(expectedMaxSharpe(0, 1000)).toBe(0);
  });

  it("grows monotonically with the number of trials", () => {
    const small = expectedMaxSharpe(0.2, 10);
    const medium = expectedMaxSharpe(0.2, 1000);
    const large = expectedMaxSharpe(0.2, 100_000);
    expect(small).toBeGreaterThan(0);
    expect(medium).toBeGreaterThan(small);
    expect(large).toBeGreaterThan(medium);
  });

  it("scales linearly with the trial Sharpe dispersion", () => {
    const a = expectedMaxSharpe(0.1, 500);
    const b = expectedMaxSharpe(0.2, 500);
    expect(b).toBeCloseTo(a * 2, 10);
  });
});

describe("probabilisticSharpe", () => {
  it("is 0.5 when the observed Sharpe equals the benchmark", () => {
    expect(
      probabilisticSharpe({
        observedSharpe: 0.1,
        benchmarkSharpe: 0.1,
        nObs: 252,
      }),
    ).toBeCloseTo(0.5, 6);
  });

  it("rises with more observations behind the same Sharpe", () => {
    const few = probabilisticSharpe({ observedSharpe: 0.05, nObs: 30 });
    const many = probabilisticSharpe({ observedSharpe: 0.05, nObs: 3000 });
    expect(many).toBeGreaterThan(few);
  });

  it("falls for fat tails (raw kurtosis > 3) at the same Sharpe", () => {
    const normal = probabilisticSharpe({
      observedSharpe: 0.1,
      nObs: 500,
      kurtosis: 3,
    });
    const fat = probabilisticSharpe({
      observedSharpe: 0.1,
      nObs: 500,
      kurtosis: 12,
    });
    expect(fat).toBeLessThan(normal);
  });

  it("is 0 when it cannot be assessed", () => {
    expect(probabilisticSharpe({ observedSharpe: 0.2, nObs: 1 })).toBe(0);
    expect(probabilisticSharpe({ observedSharpe: Number.NaN, nObs: 100 })).toBe(
      0,
    );
  });
});

describe("deflatedSharpe", () => {
  /** Trial Sharpes with a realistic spread (sd 0.1) centered on zero. */
  const trials = Array.from(
    { length: 500 },
    (_, i) => (((i * 37) % 500) - 250) / 2500,
  );

  it("deflates a candidate that would look good against a zero benchmark", () => {
    // The naive PSR (no selection correction) is high...
    const naive = probabilisticSharpe({ observedSharpe: 0.1, nObs: 320 });
    expect(naive).toBeGreaterThan(0.95);
    // ...but after correcting for 500 trials it is not.
    const dsr = deflatedSharpe({
      observedSharpe: 0.1,
      trialSharpes: trials,
      nObs: 320,
    });
    expect(dsr).toBeLessThan(naive);
    expect(classifyDeflatedSharpe(dsr)).not.toBe("robust");
  });

  it("improves as the observed Sharpe rises above the expected maximum", () => {
    const weak = deflatedSharpe({
      observedSharpe: 0.05,
      trialSharpes: trials,
      nObs: 320,
    });
    const strong = deflatedSharpe({
      observedSharpe: 0.6,
      trialSharpes: trials,
      nObs: 320,
    });
    expect(strong).toBeGreaterThan(weak);
    expect(classifyDeflatedSharpe(strong)).toBe("robust");
  });

  it("gets harsher as the trial count grows", () => {
    const few = deflatedSharpe({
      observedSharpe: 0.15,
      trialSharpes: trials.slice(0, 10),
      nObs: 320,
    });
    const many = deflatedSharpe({
      observedSharpe: 0.15,
      trialSharpes: trials,
      nObs: 320,
    });
    expect(many).toBeLessThanOrEqual(few);
  });

  it("reduces to PSR (not to the raw Sharpe) when there is nothing to deflate", () => {
    const dsr = deflatedSharpe({
      observedSharpe: 0.1,
      trialSharpes: [0.1],
      nObs: 320,
      benchmarkSharpe: 0,
    });
    expect(dsr).toBeCloseTo(
      probabilisticSharpe({ observedSharpe: 0.1, nObs: 320 }),
      10,
    );
  });

  it("honours a benchmark Sharpe above the observed one", () => {
    const dsr = deflatedSharpe({
      observedSharpe: 0.05,
      trialSharpes: trials,
      nObs: 320,
      benchmarkSharpe: 0.5,
    });
    expect(dsr).toBeLessThan(0.5);
  });

  it("accepts an effective trial count below the raw count", () => {
    const raw = deflatedSharpe({
      observedSharpe: 0.15,
      trialSharpes: trials,
      nObs: 320,
    });
    const effective = deflatedSharpe({
      observedSharpe: 0.15,
      trialSharpes: trials,
      nObs: 320,
      effectiveTrials: 25,
    });
    // Fewer effective trials => lower SR0 => less deflation => higher DSR.
    expect(effective).toBeGreaterThanOrEqual(raw);
  });

  it("rejects an effective trial count that exceeds the evidence", () => {
    expect(() =>
      deflatedSharpe({
        observedSharpe: 0.1,
        trialSharpes: trials.slice(0, 5),
        nObs: 100,
        effectiveTrials: 50,
      }),
    ).toThrow(RangeError);
    expect(() =>
      deflatedSharpe({
        observedSharpe: 0.1,
        trialSharpes: trials,
        nObs: 100,
        effectiveTrials: 0,
      }),
    ).toThrow(RangeError);
  });

  it("always returns a probability in [0, 1]", () => {
    fc.assert(
      fc.property(
        fc.double({ min: -1, max: 1, noNaN: true }),
        fc.array(fc.double({ min: -1, max: 1, noNaN: true }), {
          minLength: 2,
          maxLength: 200,
        }),
        fc.integer({ min: 2, max: 5000 }),
        (observed, trialSharpes, nObs) => {
          const dsr = deflatedSharpe({
            observedSharpe: observed,
            trialSharpes,
            nObs,
          });
          return dsr >= 0 && dsr <= 1;
        },
      ),
      { numRuns: 300 },
    );
  });

  it("never rewards a losing candidate with a robust verdict", () => {
    fc.assert(
      fc.property(
        fc.double({ min: -0.9, max: -0.01, noNaN: true }),
        fc.array(fc.double({ min: -1, max: 1, noNaN: true }), {
          minLength: 2,
          maxLength: 200,
        }),
        (losing, trialSharpes) => {
          const dsr = deflatedSharpe({
            observedSharpe: losing,
            trialSharpes,
            nObs: 320,
          });
          return classifyDeflatedSharpe(dsr) !== "robust";
        },
      ),
      { numRuns: 300 },
    );
  });
});

describe("classifyDeflatedSharpe", () => {
  it("applies the Bailey-Lopez de Prado thresholds", () => {
    expect(classifyDeflatedSharpe(0.99)).toBe("robust");
    expect(classifyDeflatedSharpe(0.95)).toBe("robust");
    expect(classifyDeflatedSharpe(0.94)).toBe("marginal");
    expect(classifyDeflatedSharpe(0.8)).toBe("marginal");
    expect(classifyDeflatedSharpe(0.79)).toBe("likely-false-positive");
    expect(classifyDeflatedSharpe(Number.NaN)).toBe("likely-false-positive");
  });
});
