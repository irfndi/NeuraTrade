/**
 * Defaults manifest and the min/max/step range builder shared by every group.
 *
 * A triple default of 0 means "use the strategy's internal default"
 * (the range is disabled) — encoded once here, not in each description.
 */
import { Options } from "../kit/kit.ts";

/**
 * Grouped builder for the min/max/step optimization-range triples.
 * A triple default of 0 means "use the strategy's internal default" (the
 * range is disabled) — encoded here once instead of in each description.
 */
export function minMaxStep(
  base: string,
  kind: "float" | "integer",
  label: string,
  defaults: {
    readonly min: number;
    readonly max: number;
    readonly step: number;
  } = {
    min: 0,
    max: 0,
    step: 0,
  },
) {
  return {
    min: Options[kind](`${base}-min`).pipe(
      Options.withDefault(defaults.min),
      Options.withDescription(`Minimum ${label} to test`),
    ),
    max: Options[kind](`${base}-max`).pipe(
      Options.withDefault(defaults.max),
      Options.withDescription(`Maximum ${label} to test`),
    ),
    step: Options[kind](`${base}-step`).pipe(
      Options.withDefault(defaults.step),
      Options.withDescription(`Step size for ${label}`),
    ),
  };
}

export const SCALP_OPTION_DEFAULTS = {
  /** Initial capital in quote currency. */
  capital: 10000,
  /** Position size as percent of capital. */
  positionSize: 100,
  /** Risk per trade as percent of capital (0 = disabled, uses --position-size). */
  riskPerTrade: 0,
  /** Futures leverage multiplier. */
  leverage: 3,
  /** Max position size as percent of capital when risk-based sizing is on. */
  maxPositionSize: 100,
  /** Maximum number of grid levels (0 = disabled). */
  gridMaxGrids: 0,
} as const;
