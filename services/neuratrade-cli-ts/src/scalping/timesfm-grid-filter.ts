/**
 * Causal TimesFM overlays for research backtests.
 *
 * The overlay only controls NEW grid entries. Open positions keep the grid's
 * normal exit rules. Forecasts are applied to bars strictly after their
 * origin so a close-based forecast cannot leak into the origin bar.
 */

export interface TimesFmGridForecast {
  readonly originIndex: number;
  readonly pointReturnPct: number;
  readonly q10ReturnPct: number | null;
  readonly q90ReturnPct: number | null;
}

export type TimesFmGridPolicy =
  | { readonly kind: "standAsidePoint"; readonly thresholdPct: number }
  | {
      readonly kind: "directionalPoint";
      readonly thresholdPct: number;
      readonly contrarian?: boolean;
    }
  | { readonly kind: "standAsideBand"; readonly thresholdPct: number };

type EntryDirection = "long" | "short" | "flat" | undefined;

function requireFinite(value: number, label: string): number {
  if (!Number.isFinite(value)) throw new Error(`${label} must be finite`);
  return value;
}

function requireNonNegative(value: number, label: string): number {
  requireFinite(value, label);
  if (value < 0) throw new Error(`${label} cannot be negative`);
  return value;
}

function policyDirection(
  forecast: TimesFmGridForecast,
  policy: TimesFmGridPolicy,
): EntryDirection {
  const thresholdPct = requireNonNegative(
    policy.thresholdPct,
    "policy thresholdPct",
  );
  if (policy.kind === "standAsidePoint") {
    return Math.abs(forecast.pointReturnPct) >= thresholdPct
      ? "flat"
      : undefined;
  }

  if (policy.kind === "directionalPoint") {
    if (Math.abs(forecast.pointReturnPct) < thresholdPct) return "flat";
    const positive = forecast.pointReturnPct > 0;
    const long = policy.contrarian ? !positive : positive;
    return long ? "long" : "short";
  }

  const { q10ReturnPct, q90ReturnPct } = forecast;
  if (q10ReturnPct === null || q90ReturnPct === null) return "flat";
  const widthPct = requireFinite(q90ReturnPct - q10ReturnPct, "forecast band");
  if (widthPct < 0) throw new Error("forecast q90 must not be below q10");
  return widthPct >= thresholdPct ? "flat" : undefined;
}

function validateForecasts(
  candleCount: number,
  forecasts: readonly TimesFmGridForecast[],
): void {
  if (!Number.isInteger(candleCount) || candleCount < 1) {
    throw new Error("candleCount must be a positive integer");
  }
  let previousIndex = -1;
  for (const forecast of forecasts) {
    if (
      !Number.isInteger(forecast.originIndex) ||
      forecast.originIndex < 0 ||
      forecast.originIndex >= candleCount
    ) {
      throw new Error("forecast originIndex is outside the candle data");
    }
    if (forecast.originIndex <= previousIndex) {
      throw new Error("forecasts must be strictly ordered by originIndex");
    }
    requireFinite(forecast.pointReturnPct, "point forecast return");
    if (forecast.q10ReturnPct !== null)
      requireFinite(forecast.q10ReturnPct, "q10 forecast return");
    if (forecast.q90ReturnPct !== null)
      requireFinite(forecast.q90ReturnPct, "q90 forecast return");
    previousIndex = forecast.originIndex;
  }
}

/**
 * Build a per-bar entry overlay from causal, already-scored forecasts.
 *
 * The returned array is initialized to `flat` before the first forecast and
 * after every forecast origin until its policy is known. For a non-flat
 * policy, `undefined` means allow the underlying grid to choose either side.
 */
export function buildTimesFmEntryOverlay(
  candleCount: number,
  forecasts: readonly TimesFmGridForecast[],
  policy: TimesFmGridPolicy,
): readonly EntryDirection[] {
  validateForecasts(candleCount, forecasts);
  const overlay: EntryDirection[] = Array(candleCount).fill("flat");
  for (let index = 0; index < forecasts.length; index += 1) {
    const forecast = forecasts[index]!;
    const nextOrigin = forecasts[index + 1]?.originIndex ?? candleCount;
    const direction = policyDirection(forecast, policy);
    for (
      let barIndex = forecast.originIndex + 1;
      barIndex <= nextOrigin && barIndex < candleCount;
      barIndex += 1
    ) {
      overlay[barIndex] = direction;
    }
  }
  return overlay;
}
