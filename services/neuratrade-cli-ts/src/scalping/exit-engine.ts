import { calculateATR, calculateRSI } from "./indicators.js";
import type { CandleLike } from "./types.js";
import type { SymbolStatistics } from "./symbol-stats.js";

export interface ExitEngineOptions {
  readonly side: "long" | "short";
  readonly entryPrice: number;
  readonly atr: number | null;
  readonly useAtr: boolean;
  readonly atrStopMultiplier: number;
  readonly atrRiskReward: number;
  readonly stopLossPct: number;
  readonly takeProfitPct: number;
  readonly scaleOutAtR: number;
  readonly candles: readonly CandleLike[];
  readonly volatilityLookback: number;
  readonly volatilityLowPct: number;
  readonly volatilityHighPct: number;
  readonly volatilityLowFactor: number;
  readonly volatilityHighFactor: number;
  readonly symbolStats?: SymbolStatistics;
  /** When true, stop/target distances are derived from per-symbol ATR%
   *  statistics instead of fixed percentages. */
  readonly useAdaptiveStops?: boolean;
  /** Stop distance as a multiple of the symbol's median ATR%. Default 1.0. */
  readonly adaptiveStopAtrMultiplier?: number;
  /** Risk:reward ratio applied to the adaptive stop distance. Default 2.0. */
  readonly adaptiveRiskReward?: number;
}

export interface ExitLevels {
  readonly stopLoss: number;
  readonly takeProfit: number;
  readonly scaleOutPrice: number | null;
}

/**
 * Pure function that computes stop-loss, take-profit and optional scale-out
 * levels for a scalping position.
 *
 * When `useAtr` is true and a positive ATR is supplied, stop distance is
 * `ATR * calibratedMultiplier`. The take-profit distance is that stop distance
 * multiplied by `atrRiskReward`. When `atrRiskReward <= 0` callers should
 * convert a legacy `atrTakeProfitMultiplier` to
 * `atrTakeProfitMultiplier / atrStopMultiplier` before calling.
 *
 * Volatility calibration adjusts the ATR stop multiplier up or down when the
 * current ATR% sits outside the configured percentile thresholds of the
 * lookback window.
 */
export function computeExitLevels(options: ExitEngineOptions): ExitLevels {
  let stopLoss: number;
  let takeProfit: number;

  if (options.useAdaptiveStops && options.symbolStats) {
    const atrPct = options.symbolStats.atrPctMedian;
    const multiplier = Math.max(0.01, options.adaptiveStopAtrMultiplier ?? 1);
    const riskReward = Math.max(0.01, options.adaptiveRiskReward ?? 2);
    if (atrPct > 0) {
      const stopDistancePct = atrPct * multiplier;
      const tpDistancePct = stopDistancePct * riskReward;
      stopLoss =
        options.side === "long"
          ? options.entryPrice * (1 - stopDistancePct)
          : options.entryPrice * (1 + stopDistancePct);
      takeProfit =
        options.side === "long"
          ? options.entryPrice * (1 + tpDistancePct)
          : options.entryPrice * (1 - tpDistancePct);
    } else {
      // Fall back to fixed percentages if statistics are unavailable.
      const slPct = options.stopLossPct / 100;
      const tpPct = options.takeProfitPct / 100;
      stopLoss =
        options.side === "long"
          ? options.entryPrice * (1 - slPct)
          : options.entryPrice * (1 + slPct);
      takeProfit =
        options.side === "long"
          ? options.entryPrice * (1 + tpPct)
          : options.entryPrice * (1 - tpPct);
    }
  } else if (options.useAtr && options.atr && options.atr > 0) {
    let calibratedMultiplier = calibrateAtrStopMultiplier(options);
    if (options.symbolStats) {
      calibratedMultiplier = applySymbolStatsToMultiplier(
        calibratedMultiplier,
        options.symbolStats,
      );
    }
    const stopDistance = options.atr * calibratedMultiplier;
    stopLoss =
      options.side === "long"
        ? options.entryPrice - stopDistance
        : options.entryPrice + stopDistance;
    const tpDistance = stopDistance * options.atrRiskReward;
    takeProfit =
      options.side === "long"
        ? options.entryPrice + tpDistance
        : options.entryPrice - tpDistance;
  } else {
    const slPct = options.stopLossPct / 100;
    const tpPct = options.takeProfitPct / 100;
    stopLoss =
      options.side === "long"
        ? options.entryPrice * (1 - slPct)
        : options.entryPrice * (1 + slPct);
    takeProfit =
      options.side === "long"
        ? options.entryPrice * (1 + tpPct)
        : options.entryPrice * (1 - tpPct);
  }

  const scaleOutPrice =
    options.scaleOutAtR > 0
      ? calculateScaleOutPrice(
          options.entryPrice,
          stopLoss,
          options.scaleOutAtR,
          options.side,
        )
      : null;

  return { stopLoss, takeProfit, scaleOutPrice };
}

/**
 * Adjust the ATR stop multiplier based on the current ATR% relative to a
 * lookback window of ATR% percentiles.
 */
export function calibrateAtrStopMultiplier(
  options: Pick<
    ExitEngineOptions,
    | "entryPrice"
    | "atr"
    | "candles"
    | "atrStopMultiplier"
    | "volatilityLookback"
    | "volatilityLowPct"
    | "volatilityHighPct"
    | "volatilityLowFactor"
    | "volatilityHighFactor"
  >,
): number {
  if (
    !options.atr ||
    options.atr <= 0 ||
    options.entryPrice <= 0 ||
    options.volatilityLookback <= 0 ||
    options.candles.length < 15
  ) {
    return options.atrStopMultiplier;
  }

  const atrPctValues = calculateAtrPctSeries(options.candles).slice(
    -options.volatilityLookback,
  );
  if (atrPctValues.length === 0) return options.atrStopMultiplier;

  const sorted = [...atrPctValues].sort((a, b) => a - b);
  const lowThreshold = percentile(sorted, options.volatilityLowPct);
  const highThreshold = percentile(sorted, options.volatilityHighPct);
  const currentAtrPct = options.atr / options.entryPrice;

  let multiplier = options.atrStopMultiplier;
  if (currentAtrPct <= lowThreshold) {
    multiplier *= options.volatilityLowFactor;
  } else if (currentAtrPct >= highThreshold) {
    multiplier *= options.volatilityHighFactor;
  }

  return Math.max(0.01, multiplier);
}

function applySymbolStatsToMultiplier(
  multiplier: number,
  stats: SymbolStatistics,
): number {
  const range = stats.atrPct80 - stats.atrPct20;
  const zScore = range > 0 ? (stats.atr14Pct - stats.atrPctMedian) / range : 0;
  // Widen stops slightly in high-vol regimes, tighten in low-vol regimes.
  const factor = Math.max(0.5, Math.min(2.0, 1 + zScore * 0.2));
  return Math.max(0.01, multiplier * factor);
}

/**
 * Return the price at which a partial scale-out should occur. The distance from
 * entry is `scaleOutAtR * |entryPrice - stopLoss|` in the profitable direction.
 */
export function calculateScaleOutPrice(
  entryPrice: number,
  stopLoss: number,
  scaleOutAtR: number,
  side: "long" | "short",
): number | null {
  if (scaleOutAtR <= 0 || entryPrice === stopLoss) return null;
  const stopDistance = Math.abs(entryPrice - stopLoss);
  const distance = stopDistance * scaleOutAtR;
  return side === "long" ? entryPrice + distance : entryPrice - distance;
}

function calculateAtrPctSeries(candles: readonly CandleLike[]): number[] {
  const values: number[] = [];
  for (let i = 14; i < candles.length; i++) {
    const atr = calculateATR(candles.slice(0, i + 1), 14);
    const close = candles[i].close;
    if (atr && close > 0) {
      values.push(atr / close);
    }
  }
  return values;
}

function percentile(sorted: readonly number[], pct: number): number {
  if (sorted.length === 0) return 0;
  if (sorted.length === 1) return sorted[0];
  const index = (pct / 100) * (sorted.length - 1);
  const lower = Math.floor(index);
  const upper = Math.ceil(index);
  const weight = index - lower;
  return sorted[lower] * (1 - weight) + sorted[upper] * weight;
}

export interface RsiExitOptions {
  readonly side: "long" | "short";
  readonly candles: readonly CandleLike[];
  readonly exitRsiPeriod?: number;
  readonly exitRsiLongLevel?: number;
  readonly exitRsiShortLevel?: number;
}

/**
 * Check whether an open position should be exited because RSI has normalized.
 *
 * Long positions exit when RSI >= exitRsiLongLevel; short positions exit when
 * RSI <= exitRsiShortLevel. When any required option is missing or RSI cannot
 * be computed, no exit is signaled so existing stop/target logic is preserved.
 */
export function checkRsiExit(options: RsiExitOptions): boolean {
  const period = options.exitRsiPeriod ?? 0;
  const longLevel = options.exitRsiLongLevel ?? 0;
  const shortLevel = options.exitRsiShortLevel ?? 0;
  if (
    period <= 0 ||
    longLevel <= 0 ||
    shortLevel <= 0 ||
    options.candles.length < period + 1
  ) {
    return false;
  }

  const rsi = calculateRSI(options.candles, period);
  if (rsi === null) return false;

  if (options.side === "long") {
    return rsi >= longLevel;
  }
  return rsi <= shortLevel;
}
