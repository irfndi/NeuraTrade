/**
 * Mutation + keep/discard helpers for the overnight loop.
 */
import type { AutoresearchKnobs } from "./knobs.ts";

const AXES = [
  "gridStepPct",
  "stopRatio",
  "maxHoldBars",
  "targetRatio",
  "rungs",
  "gridMaxGrids",
  "gridPauseAfterLossBars",
  "chopGateAdxThreshold",
] as const;

type Axis = (typeof AXES)[number];

function clamp(n: number, lo: number, hi: number): number {
  return Math.min(hi, Math.max(lo, n));
}

function round(n: number, digits: number): number {
  const p = 10 ** digits;
  return Math.round(n * p) / p;
}

export function mutateKnobs(
  base: AutoresearchKnobs,
  rng: () => number = Math.random,
): { next: AutoresearchKnobs; axis: Axis } {
  const axis = AXES[Math.floor(rng() * AXES.length)]!;
  const next = { ...base };
  switch (axis) {
    case "gridStepPct":
      next.gridStepPct = round(
        clamp(base.gridStepPct * (0.7 + rng() * 0.8), 0.2, 3.0),
        2,
      );
      break;
    case "stopRatio":
      next.stopRatio = round(
        clamp(base.stopRatio * (0.7 + rng() * 0.8), 0.5, 3.0),
        2,
      );
      break;
    case "maxHoldBars":
      next.maxHoldBars = Math.round(
        clamp(base.maxHoldBars * (0.5 + rng()), 4, 96),
      );
      break;
    case "targetRatio":
      next.targetRatio = round(
        clamp(base.targetRatio * (0.7 + rng() * 0.8), 0.8, 4.0),
        2,
      );
      break;
    case "rungs":
      next.rungs = clamp(Math.round(base.rungs + (rng() < 0.5 ? -1 : 1)), 1, 3);
      break;
    case "gridMaxGrids":
      next.gridMaxGrids = clamp(
        Math.round(base.gridMaxGrids + (rng() < 0.5 ? -1 : 1)),
        2,
        6,
      );
      break;
    case "gridPauseAfterLossBars":
      next.gridPauseAfterLossBars = clamp(
        Math.round(base.gridPauseAfterLossBars + (rng() < 0.5 ? -2 : 2)),
        0,
        12,
      );
      break;
    case "chopGateAdxThreshold": {
      const choices = [0, 20, 25, 30, 35];
      next.chopGateAdxThreshold =
        choices[Math.floor(rng() * choices.length)] ?? 0;
      break;
    }
  }
  return { next, axis };
}

export function shouldKeep(input: {
  candidateScore: number;
  candidateGuardsOk: boolean;
  championScore: number;
  /** Once a champion has passed guards, never regress to a failing candidate. */
  championGuardsOk: boolean;
}): boolean {
  if (!Number.isFinite(input.candidateScore)) return false;
  if (!(input.candidateScore > input.championScore)) return false;
  // Climb from a failing seed on score alone; after first guard-pass, require guards.
  if (input.championGuardsOk && !input.candidateGuardsOk) return false;
  return true;
}

export function renderKnobsModule(k: AutoresearchKnobs): string {
  const body = JSON.stringify(k, null, 2);
  return `/**
 * THE editable surface for autoresearch (Karpathy train.py analogue).
 * Agents / the mutation loop may change these values. Nothing else.
 */
export interface AutoresearchKnobs {
  readonly rungs: number;
  readonly gridStepPct: number;
  readonly gridMaxGrids: number;
  readonly gridPauseAfterLossBars: number;
  readonly stopRatio: number;
  readonly targetRatio: number;
  readonly maxHoldBars: number;
  readonly trendFilterPeriod: number;
  readonly chopGateAdxThreshold: number;
  readonly positionFraction: number;
}

/** Current champion knobs — overwritten only on KEEP. */
export const knobs: AutoresearchKnobs = ${body};
`;
}
