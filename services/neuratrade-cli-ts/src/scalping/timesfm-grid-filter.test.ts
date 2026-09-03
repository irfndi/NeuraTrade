import { describe, expect, it } from "bun:test";
import {
  buildTimesFmEntryOverlay,
  type TimesFmGridForecast,
} from "./timesfm-grid-filter.js";

const forecasts: TimesFmGridForecast[] = [
  {
    originIndex: 2,
    pointReturnPct: 0.4,
    q10ReturnPct: -0.2,
    q90ReturnPct: 0.8,
  },
  {
    originIndex: 5,
    pointReturnPct: -0.3,
    q10ReturnPct: -0.7,
    q90ReturnPct: 0.1,
  },
];

describe("TimesFM grid filter", () => {
  it("applies a forecast only after its causal origin", () => {
    const overlay = buildTimesFmEntryOverlay(8, forecasts, {
      kind: "standAsidePoint",
      thresholdPct: 0.5,
    });

    expect(overlay.slice(0, 3)).toEqual(["flat", "flat", "flat"]);
    expect(overlay.slice(3, 6)).toEqual([undefined, undefined, undefined]);
    expect(overlay.slice(6)).toEqual([undefined, undefined]);
  });

  it("restricts direction or reverses it without allowing weak forecasts", () => {
    const direct = buildTimesFmEntryOverlay(8, forecasts, {
      kind: "directionalPoint",
      thresholdPct: 0.25,
    });
    const contrarian = buildTimesFmEntryOverlay(8, forecasts, {
      kind: "directionalPoint",
      thresholdPct: 0.25,
      contrarian: true,
    });

    expect(direct.slice(3, 6)).toEqual(["long", "long", "long"]);
    expect(direct.slice(6)).toEqual(["short", "short"]);
    expect(contrarian.slice(3, 6)).toEqual(["short", "short", "short"]);
    expect(contrarian.slice(6)).toEqual(["long", "long"]);
  });

  it("fails closed when a volatility-band forecast is incomplete", () => {
    const overlay = buildTimesFmEntryOverlay(
      6,
      [{ ...forecasts[0]!, q90ReturnPct: null }],
      { kind: "standAsideBand", thresholdPct: 1 },
    );

    expect(overlay).toEqual(["flat", "flat", "flat", "flat", "flat", "flat"]);
  });

  it("rejects out-of-order forecast origins", () => {
    expect(() =>
      buildTimesFmEntryOverlay(8, [forecasts[1]!, forecasts[0]!], {
        kind: "standAsidePoint",
        thresholdPct: 0.5,
      }),
    ).toThrow("strictly ordered");
  });
});
