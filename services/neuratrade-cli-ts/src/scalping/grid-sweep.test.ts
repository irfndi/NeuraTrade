import { describe, expect, it } from "bun:test";
import {
  DEFAULT_GRID_SWEEP_FLOORS,
  passesGridSweepFloors,
  type GridSweepRow,
} from "./grid-sweep.js";

const passingRow: GridSweepRow = {
  tradesPerMonth: 10,
  profitFactor: 1.3,
  winRate: 50,
  oosTrades: 10,
  oosReturnPct: 0,
  maxDdPct: 15,
};

describe("grid sweep readiness floors", () => {
  it("uses percentage-point win-rate semantics at the boundary", () => {
    expect(passesGridSweepFloors(passingRow)).toBe(true);
    expect(passesGridSweepFloors({ ...passingRow, winRate: 49.99 })).toBe(
      false,
    );
    expect(passesGridSweepFloors({ ...passingRow, winRate: 0.75 })).toBe(false);
  });

  it("rejects every candidate that misses a configured floor", () => {
    const fields: readonly [keyof GridSweepRow, number][] = [
      ["tradesPerMonth", 9.99],
      ["profitFactor", 1.299],
      ["winRate", 49.99],
      ["oosTrades", 9],
      ["oosReturnPct", -0.01],
      ["maxDdPct", 15.01],
    ];

    for (const [field, value] of fields) {
      expect(passesGridSweepFloors({ ...passingRow, [field]: value })).toBe(
        false,
      );
    }
  });

  it("exposes the documented default floors", () => {
    expect(DEFAULT_GRID_SWEEP_FLOORS).toEqual({
      minimumTradesPerMonth: 10,
      minimumProfitFactor: 1.3,
      minimumWinRatePct: 50,
      minimumOosTrades: 10,
      minimumOosReturnPct: 0,
      maximumDrawdownPct: 15,
    });
  });
});
