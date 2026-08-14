import { expect, describe, it } from "bun:test";
import { isLadderSurvivorRow } from "./scalp.js";

describe("isLadderSurvivorRow (soak routing)", () => {
  it("routes a grid watchlist row carrying rungs to the ladder engine", () => {
    expect(
      isLadderSurvivorRow(
        {
          gridStepPct: 0.4,
          gridMaxGrids: 4,
          gridPauseAfterLossBars: 2,
          rungs: 3,
        },
        "grid",
      ),
    ).toBe(true);
    expect(
      isLadderSurvivorRow(
        {
          gridStepPct: 0.4,
          gridMaxGrids: 4,
          gridPauseAfterLossBars: 2,
          rungs: 1,
        },
        "grid",
      ),
    ).toBe(true);
  });

  it("keeps single-position grid rows (no rungs) on the grid engine", () => {
    expect(
      isLadderSurvivorRow(
        { gridStepPct: 0.4, gridMaxGrids: 4, gridPauseAfterLossBars: 2 },
        "grid",
      ),
    ).toBe(false);
    expect(isLadderSurvivorRow(undefined, "grid")).toBe(false);
  });

  it("never routes to the ladder engine outside the grid strategy", () => {
    expect(
      isLadderSurvivorRow(
        {
          gridStepPct: 0.4,
          gridMaxGrids: 4,
          gridPauseAfterLossBars: 2,
          rungs: 3,
        },
        "signal",
      ),
    ).toBe(false);
  });
});
