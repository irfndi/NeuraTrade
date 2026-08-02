import { describe, expect, it } from "bun:test";
import * as fc from "fast-check";
import type { CandleLike } from "./types.js";
import { validateCandleDataQuality } from "./grid-validation.js";

describe("grid validation data-quality properties", () => {
  it("never accepts a non-finite OHLCV value", () => {
    fc.assert(
      fc.property(
        fc.constantFrom("open", "high", "low", "close", "volume"),
        (field) => {
          const row: CandleLike = {
            open: 100,
            high: 101,
            low: 99,
            close: 100,
            volume: 1,
            timestamp: new Date("2026-01-01T00:00:00.000Z"),
          };
          const invalid: CandleLike =
            field === "open"
              ? { ...row, open: Number.POSITIVE_INFINITY }
              : field === "high"
                ? { ...row, high: Number.POSITIVE_INFINITY }
                : field === "low"
                  ? { ...row, low: Number.POSITIVE_INFINITY }
                  : field === "close"
                    ? { ...row, close: Number.POSITIVE_INFINITY }
                    : { ...row, volume: Number.POSITIVE_INFINITY };
          expect(
            validateCandleDataQuality(
              [invalid],
              new Date("2026-01-01T01:00:00.000Z"),
            ).valid,
          ).toBe(false);
        },
      ),
      { numRuns: 10 },
    );
  });

  it("rejects every generated cadence gap", () => {
    fc.assert(
      fc.property(fc.integer({ min: 1, max: 96 }), (extraBars) => {
        const rows: CandleLike[] = [
          {
            open: 100,
            high: 101,
            low: 99,
            close: 100,
            volume: 1,
            timestamp: new Date("2026-01-01T00:00:00.000Z"),
          },
          {
            open: 100,
            high: 101,
            low: 99,
            close: 100,
            volume: 1,
            timestamp: new Date(Date.UTC(2026, 0, 1, 0, 15 * (extraBars + 1))),
          },
        ];
        const result = validateCandleDataQuality(
          rows,
          new Date("2026-01-01T12:00:00.000Z"),
          1,
          1,
          0,
        );
        expect(result.valid).toBe(false);
        expect(result.failures).toContain(
          "candle 1 is not exactly 15m after the previous candle",
        );
      }),
      { numRuns: 25 },
    );
  });
});
