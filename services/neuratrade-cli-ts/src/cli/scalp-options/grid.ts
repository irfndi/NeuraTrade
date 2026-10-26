/**
 * scalp CLI options — grid
 *
 * Grid geometry flags shared by the grid backtest, live grid and readiness paths.
 */
import { Options } from "../kit/kit.ts";
import { SCALP_OPTION_DEFAULTS } from "./defaults.js";

export const gridStepPctOption = Options.float("grid-step-pct").pipe(
  Options.withDefault(0),
  Options.withDescription("Grid step percentage (0 disables grid)"),
);

export const gridMaxGridsOption = Options.integer("grid-max-grids").pipe(
  Options.withDefault(SCALP_OPTION_DEFAULTS.gridMaxGrids),
  Options.withDescription("Maximum number of grid levels"),
);

export const gridPauseAfterLossBarsOption = Options.integer(
  "grid-pause-after-loss-bars",
).pipe(
  Options.withDefault(0),
  Options.withDescription("Bars to pause grid after a loss"),
);

export const onlyWithTrendOption = Options.boolean("only-with-trend").pipe(
  Options.withDefault(false),
  Options.withDescription(
    "Grid: only enter long above trend SMA and short below it",
  ),
);

export const targetRatioOption = Options.float("target-ratio").pipe(
  Options.withDefault(1),
  Options.withDescription(
    "Grid: target distance as a multiple of the grid step (default 1.0)",
  ),
);

export const chopGateAdxOption = Options.float("chop-gate-adx").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Grid: skip new entries while causal ADX(14) >= this threshold (0 = disabled)",
  ),
);
