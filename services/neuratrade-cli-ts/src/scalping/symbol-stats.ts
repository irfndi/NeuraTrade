import type { CandleLike } from "./types.js";
import {
  calculateADX,
  calculateAnnualizedVolatility,
  calculateATR,
} from "./indicators.js";

export interface SymbolStatistics {
  /** ATR(14) as a percentage of the latest close. */
  readonly atr14Pct: number;
  /** Median ATR% over the lookback window. */
  readonly atrPctMedian: number;
  /** 20th percentile ATR% over the lookback window. */
  readonly atrPct20: number;
  /** 80th percentile ATR% over the lookback window. */
  readonly atrPct80: number;
  /** Annualized volatility derived from the timeframe. */
  readonly annualizedVolatility: number;
  /** Latest ADX(14) value. */
  readonly adx14: number;
  /** True if the market is currently trending (ADX above threshold). */
  readonly isTrending: boolean;
  /** Ratio of latest volume to its simple moving average. */
  readonly volumeRatio: number;
  /** Number of candles used for the statistics. */
  readonly sampleSize: number;
}

function percentile(sorted: readonly number[], p: number): number {
  if (sorted.length === 0) return 0;
  if (sorted.length === 1) return sorted[0];
  const index = (p / 100) * (sorted.length - 1);
  const lower = Math.floor(index);
  const upper = Math.ceil(index);
  const weight = index - lower;
  return sorted[lower] * (1 - weight) + sorted[upper] * weight;
}

/**
 * Compute per-symbol market statistics from a candle history.
 * These values are used to adapt stop distances and position sizes so that
 * one strategy config can work across many symbols without manual tuning.
 */
export function computeSymbolStats(
  candles: readonly CandleLike[],
  timeframe: string,
  lookback = 100,
): SymbolStatistics {
  if (candles.length < 20) {
    return {
      atr14Pct: 0,
      atrPctMedian: 0,
      atrPct20: 0,
      atrPct80: 0,
      annualizedVolatility: 0,
      adx14: 0,
      isTrending: false,
      volumeRatio: 1,
      sampleSize: candles.length,
    };
  }

  const last = candles[candles.length - 1];
  const atr = calculateATR(candles, 14);
  const atr14Pct = atr && last.close > 0 ? atr / last.close : 0;

  const recent = candles.slice(-Math.max(lookback, 20));
  const atrPctSeries: number[] = [];
  for (let i = 14; i < recent.length; i++) {
    const window = recent.slice(0, i + 1);
    const windowAtr = calculateATR(window, 14);
    const close = recent[i].close;
    if (windowAtr && close > 0) {
      atrPctSeries.push(windowAtr / close);
    }
  }
  const sorted = [...atrPctSeries].sort((a, b) => a - b);
  const atrPctMedian = percentile(sorted, 50);
  const atrPct20 = percentile(sorted, 20);
  const atrPct80 = percentile(sorted, 80);

  const annualizedVolatility = calculateAnnualizedVolatility(
    candles,
    0,
    timeframe,
  );

  const adxResult = calculateADX(candles, 14);
  const adx14 = adxResult.adx ?? 0;

  const volumeLookback = Math.min(20, candles.length);
  const volumes = candles.slice(-volumeLookback).map((c) => c.volume);
  const avgVolume =
    volumes.reduce((sum, v) => sum + v, 0) / Math.max(1, volumes.length);
  const volumeRatio = avgVolume > 0 ? last.volume / avgVolume : 1;

  return {
    atr14Pct,
    atrPctMedian,
    atrPct20,
    atrPct80,
    annualizedVolatility,
    adx14,
    isTrending: adx14 >= 25,
    volumeRatio,
    sampleSize: candles.length,
  };
}
