import { describe, expect, it } from "bun:test";
import * as fc from "fast-check";
import {
  DEFAULT_GRID_SWEEP_FLOORS,
  passesGridSweepFloors,
  type GridSweepFloors,
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

const rowArbitrary = fc.record<GridSweepRow>({
  tradesPerMonth: fc.double({ min: 0, max: 500, noNaN: true }),
  profitFactor: fc.double({ min: 0, max: 5, noNaN: true }),
  winRate: fc.double({ min: 0, max: 100, noNaN: true }),
  oosTrades: fc.integer({ min: 0, max: 2000 }),
  oosReturnPct: fc.double({ min: -100, max: 100, noNaN: true }),
  maxDdPct: fc.double({ min: 0, max: 100, noNaN: true }),
});

const floorsArbitrary = fc.record<GridSweepFloors>({
  minimumTradesPerMonth: fc.double({ min: 0, max: 500, noNaN: true }),
  minimumProfitFactor: fc.double({ min: 0, max: 5, noNaN: true }),
  minimumWinRatePct: fc.double({ min: 0, max: 100, noNaN: true }),
  minimumOosTrades: fc.integer({ min: 0, max: 2000 }),
  minimumOosReturnPct: fc.double({ min: -100, max: 100, noNaN: true }),
  maximumDrawdownPct: fc.double({ min: 0, max: 100, noNaN: true }),
});

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

  it("is monotone: improving any metric never flips a PASS to FAIL", () => {
    fc.assert(
      fc.property(rowArbitrary, (row) => {
        const base = passesGridSweepFloors(row);
        if (!base) return; // only meaningful for rows that already pass

        // Improve every metric in every combination of fields: a PASS under
        // the base row must remain a PASS under any strictly-better row.
        const deltas: readonly (readonly (keyof GridSweepRow)[])[] = [
          [],
          ["tradesPerMonth"],
          ["profitFactor"],
          ["winRate"],
          ["oosTrades"],
          ["oosReturnPct"],
          ["maxDdPct"],
          ["tradesPerMonth", "profitFactor"],
          ["winRate", "oosReturnPct"],
          ["tradesPerMonth", "oosTrades", "maxDdPct"],
          [
            "tradesPerMonth",
            "profitFactor",
            "winRate",
            "oosTrades",
            "oosReturnPct",
            "maxDdPct",
          ],
        ];
        for (const fields of deltas) {
          let improved: GridSweepRow = { ...row };
          for (const field of fields) {
            if (field === "maxDdPct") {
              improved = { ...improved, [field]: row[field] / 2 }; // lower drawdown is better
            } else {
              improved = { ...improved, [field]: row[field] * 1.5 }; // higher is better
            }
          }
          expect(passesGridSweepFloors(improved)).toBe(true);
        }
      }),
      { numRuns: 200 },
    );
  });

  it("is monotone: relaxing any floor never flips a PASS to FAIL", () => {
    fc.assert(
      fc.property(rowArbitrary, floorsArbitrary, (row, floors) => {
        const base = passesGridSweepFloors(row, floors);
        if (!base) return;

        // Relax each floor (raise the maximum, lower the minimums): a PASS
        // under the strict floors must remain a PASS.
        const relaxed = {
          ...floors,
          minimumTradesPerMonth: floors.minimumTradesPerMonth / 2,
          minimumProfitFactor: floors.minimumProfitFactor / 2,
          minimumWinRatePct: floors.minimumWinRatePct / 2,
          minimumOosTrades: Math.floor(floors.minimumOosTrades / 2),
          minimumOosReturnPct: floors.minimumOosReturnPct - 10,
          maximumDrawdownPct: floors.maximumDrawdownPct * 1.5,
        };
        expect(passesGridSweepFloors(row, relaxed)).toBe(true);
      }),
      { numRuns: 200 },
    );
  });
});
