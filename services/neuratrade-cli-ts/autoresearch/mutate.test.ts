import { describe, expect, it } from "bun:test";
import { checkGuards, PHASE_GEOM } from "./prepare.ts";
import { mutateKnobs, shouldKeep, renderKnobsModule } from "./mutate.ts";
import { withFileLock, writeJsonFile, readJsonFile } from "./lock.ts";
import type { AutoresearchKnobs } from "./knobs.ts";
import { mkdtempSync, rmSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";

const base: AutoresearchKnobs = {
  rungs: 1,
  gridStepPct: 1.0,
  gridMaxGrids: 3,
  gridPauseAfterLossBars: 4,
  stopRatio: 1.5,
  targetRatio: 2.0,
  maxHoldBars: 48,
  trendFilterPeriod: 0,
  chopGateAdxThreshold: 0,
  positionFraction: 1.0,
};

describe("autoresearch guards", () => {
  it("rejects throughput-only wins without edge", () => {
    const g = checkGuards({
      medianLogReturn: -0.01,
      winRatePct: 55,
      medianDrawdownPct: 8,
      tradesPerSymMonth: 40,
      expectancyPct: -0.1,
    });
    expect(g.ok).toBe(false);
    expect(g.reason).toContain("log_return_nonpositive");
  });

  it("accepts growth under all claim bars", () => {
    const g = checkGuards({
      medianLogReturn: 0.01,
      winRatePct: 50,
      medianDrawdownPct: 10,
      tradesPerSymMonth: 5,
      expectancyPct: 0.2,
    });
    expect(g.ok).toBe(true);
  });
});

describe("autoresearch keep/discard", () => {
  it("climbs a failing seed on score alone", () => {
    expect(
      shouldKeep({
        candidateScore: -0.2,
        candidateGuardsOk: false,
        championScore: -0.3,
        championGuardsOk: false,
      }),
    ).toBe(true);
  });

  it("never regresses from a guard-passing champion to a failing candidate", () => {
    expect(
      shouldKeep({
        candidateScore: 0.5,
        candidateGuardsOk: false,
        championScore: 0.01,
        championGuardsOk: true,
      }),
    ).toBe(false);
  });

  it("keeps only strict score improvement once guards are green", () => {
    expect(
      shouldKeep({
        candidateScore: 0.02,
        candidateGuardsOk: true,
        championScore: 0.01,
        championGuardsOk: true,
      }),
    ).toBe(true);
    expect(
      shouldKeep({
        candidateScore: 0.01,
        candidateGuardsOk: true,
        championScore: 0.01,
        championGuardsOk: true,
      }),
    ).toBe(false);
  });
});

describe("mutateKnobs", () => {
  it("changes exactly one axis under deterministic rng", () => {
    let i = 0;
    const seq = [0.0, 0.9]; // pick first axis, then scale
    const rng = () => seq[i++] ?? 0.5;
    const { next, axis } = mutateKnobs(base, rng);
    expect(axis).toBe("gridStepPct");
    expect(next.gridStepPct).not.toBe(base.gridStepPct);
    expect(next.stopRatio).toBe(base.stopRatio);
  });
});

describe("renderKnobsModule", () => {
  it("emits importable knobs export", () => {
    const src = renderKnobsModule(base);
    expect(src).toContain("export const knobs");
    expect(src).toContain('"gridStepPct": 1');
  });
});

describe("phase geometry", () => {
  it("screen forward window is much shorter than confirm", () => {
    expect(PHASE_GEOM.screen.forwardBars).toBeLessThan(
      PHASE_GEOM.confirm.forwardBars,
    );
  });
});

describe("withFileLock", () => {
  it("serializes writes", () => {
    const dir = mkdtempSync(join(tmpdir(), "ar-lock-"));
    const lock = join(dir, "x.lock");
    const file = join(dir, "x.json");
    try {
      withFileLock(lock, () => writeJsonFile(file, { n: 1 }));
      withFileLock(lock, () => {
        const cur = readJsonFile<{ n: number }>(file)!;
        writeJsonFile(file, { n: cur.n + 1 });
      });
      expect(readJsonFile<{ n: number }>(file)?.n).toBe(2);
    } finally {
      rmSync(dir, { recursive: true, force: true });
    }
  });
});
