import { describe, expect, it } from "bun:test";
import fc from "fast-check";
import { money } from "../utils/money.js";
import { bootstrapMeanConfidenceInterval } from "./expectancy-confidence.js";

describe("bootstrapMeanConfidenceInterval", () => {
  it("returns the exact mean for a constant realized expectancy", () => {
    const result = bootstrapMeanConfidenceInterval(
      Array.from({ length: 50 }, () => money("0.2")),
      { resamples: 500, seed: 7 },
    );

    expect(result.sampleSize).toBe(50);
    expect(result.sampleMean.toString()).toBe("0.2");
    expect(result.lowerBound.toString()).toBe("0.2");
    expect(result.upperBound.toString()).toBe("0.2");
  });

  it("is deterministic for a fixed seed and changes with a different seed", () => {
    const values = [money("-1"), money("0.1"), money("3")];
    const first = bootstrapMeanConfidenceInterval(values, {
      resamples: 10,
      seed: 11,
    });
    const repeat = bootstrapMeanConfidenceInterval(values, {
      resamples: 10,
      seed: 11,
    });
    const different = bootstrapMeanConfidenceInterval(values, {
      resamples: 10,
      seed: 12,
    });

    expect(repeat.lowerBound.equals(first.lowerBound)).toBe(true);
    expect(repeat.upperBound.equals(first.upperBound)).toBe(true);
    expect(
      different.lowerBound.equals(first.lowerBound) &&
        different.upperBound.equals(first.upperBound),
    ).toBe(false);
  });

  it("keeps generated intervals finite and ordered for bounded samples", () => {
    fc.assert(
      fc.property(
        fc.array(fc.integer({ min: -500, max: 500 }), {
          minLength: 1,
          maxLength: 30,
        }),
        (cents) => {
          const result = bootstrapMeanConfidenceInterval(
            cents.map((value) => money(value).div(100)),
            { resamples: 250, seed: 19 },
          );

          expect(result.sampleMean.isFinite()).toBe(true);
          expect(result.lowerBound.isFinite()).toBe(true);
          expect(result.upperBound.isFinite()).toBe(true);
          expect(result.lowerBound.lessThanOrEqualTo(result.upperBound)).toBe(
            true,
          );
        },
      ),
      { numRuns: 100 },
    );
  });

  it("rejects empty samples and invalid confidence settings", () => {
    expect(() => bootstrapMeanConfidenceInterval([])).toThrow(
      "at least one realized expectancy",
    );
    expect(() =>
      bootstrapMeanConfidenceInterval([money("0.1")], {
        confidenceLevel: 1,
      }),
    ).toThrow("confidenceLevel must be between 0 and 1");
    expect(() =>
      bootstrapMeanConfidenceInterval([money("0.1")], { resamples: 0 }),
    ).toThrow("resamples must be positive");
  });

  it("rejects non-finite realized expectancies", () => {
    expect(() =>
      bootstrapMeanConfidenceInterval([money("0.1"), money("Infinity")]),
    ).toThrow("realized expectancy values must be finite");
  });

  it("rejects invalid confidence levels and resample counts", () => {
    expect(() =>
      bootstrapMeanConfidenceInterval([money("0.1")], { confidenceLevel: 0 }),
    ).toThrow("confidenceLevel must be between 0 and 1");
    expect(() =>
      bootstrapMeanConfidenceInterval([money("0.1")], { confidenceLevel: -1 }),
    ).toThrow("confidenceLevel must be between 0 and 1");
    expect(() =>
      bootstrapMeanConfidenceInterval([money("0.1")], {
        resamples: Number.NaN,
      }),
    ).toThrow("resamples must be positive");
    expect(() =>
      bootstrapMeanConfidenceInterval([money("0.1")], { resamples: -5 }),
    ).toThrow("resamples must be positive");
  });

  it("collapses to the median for a degenerate 100% confidence level", () => {
    // A 100% confidence interval must span the full bootstrap distribution:
    // for a two-value sample the bounds are exactly the two observed values.
    const result = bootstrapMeanConfidenceInterval(
      [money("0.1"), money("0.2")],
      { confidenceLevel: 0.999999, resamples: 100, seed: 3 },
    );

    expect(result.sampleMean.toString()).toBe("0.15");
    expect(result.lowerBound.lessThanOrEqualTo(result.sampleMean)).toBe(true);
    expect(result.upperBound.greaterThanOrEqualTo(result.sampleMean)).toBe(
      true,
    );
    expect(result.lowerBound.lessThanOrEqualTo(result.upperBound)).toBe(true);
  });
});
