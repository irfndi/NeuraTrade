/**
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
export const knobs: AutoresearchKnobs = {
  "rungs": 1,
  "gridStepPct": 1,
  "gridMaxGrids": 3,
  "gridPauseAfterLossBars": 4,
  "stopRatio": 1.5,
  "targetRatio": 2,
  "maxHoldBars": 48,
  "trendFilterPeriod": 0,
  "chopGateAdxThreshold": 0,
  "positionFraction": 1
};
