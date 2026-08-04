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
 * Build a per-bar causal statistics provider: `provider(i)` returns statistics
 * computed only from candles[0..i], so backtests never see future data
 * (bd clever-cabin-dt8). ATR and ADX series are precomputed in a single pass
 * with the exact Wilder semantics of `calculateATR`/`calculateADX` over each
 * prefix, so a full simulation costs O(n · lookback) instead of O(n²).
 *
 * Percentile fields use the trailing `lookback` causal ATR% values. Note the
 * batch `computeSymbolStats` seeds its ATR series from the start of its
 * trailing window while this provider carries the whole prefix — values
 * converge after ~2× the ATR period and the causal variant is the honest one.
 */
export function makeCausalSymbolStats(
  candles: readonly CandleLike[],
  timeframe: string,
  lookback = 100,
): (barIndex: number) => SymbolStatistics {
  const period = 14;
  const n = candles.length;

  const atrPctSeries = new Float64Array(n).fill(NaN);
  const adxSeries = new Float64Array(n).fill(NaN);
  const volumeRatioSeries = new Float64Array(n).fill(1);

  if (n >= 2) {
    const tr = new Float64Array(n);
    const plusDM = new Float64Array(n);
    const minusDM = new Float64Array(n);
    for (let i = 1; i < n; i++) {
      const high = candles[i].high;
      const low = candles[i].low;
      const prevHigh = candles[i - 1].high;
      const prevLow = candles[i - 1].low;
      const prevClose = candles[i - 1].close;
      tr[i] = Math.max(
        high - low,
        Math.abs(high - prevClose),
        Math.abs(low - prevClose),
      );
      const upMove = high - prevHigh;
      const downMove = prevLow - low;
      plusDM[i] = upMove > downMove && upMove > 0 ? upMove : 0;
      minusDM[i] = downMove > upMove && downMove > 0 ? downMove : 0;
    }

    // Wilder ATR — mirrors calculateATR over each prefix.
    if (n > period) {
      let atr = 0;
      for (let i = 1; i <= period; i++) atr += tr[i];
      atr /= period;
      atrPctSeries[period] =
        candles[period].close > 0 ? atr / candles[period].close : 0;
      for (let i = period + 1; i < n; i++) {
        atr = (atr * (period - 1) + tr[i]) / period;
        atrPctSeries[i] = candles[i].close > 0 ? atr / candles[i].close : 0;
      }
    }

    // Wilder ADX — mirrors calculateADX over each prefix.
    if (n > period * 2) {
      let smoothedPlus = 0;
      let smoothedMinus = 0;
      let smoothedTR = 0;
      for (let i = 1; i <= period; i++) {
        smoothedPlus += plusDM[i];
        smoothedMinus += minusDM[i];
        smoothedTR += tr[i];
      }
      const dxValues: number[] = [];
      let adx = 0;
      for (let i = period + 1; i < n; i++) {
        smoothedPlus = (smoothedPlus * (period - 1)) / period + plusDM[i];
        smoothedMinus = (smoothedMinus * (period - 1)) / period + minusDM[i];
        smoothedTR = (smoothedTR * (period - 1)) / period + tr[i];
        const plusDI = smoothedTR === 0 ? 0 : (smoothedPlus / smoothedTR) * 100;
        const minusDI =
          smoothedTR === 0 ? 0 : (smoothedMinus / smoothedTR) * 100;
        const dx =
          plusDI + minusDI === 0
            ? 0
            : (Math.abs(plusDI - minusDI) / (plusDI + minusDI)) * 100;
        dxValues.push(dx);
        if (dxValues.length === period) {
          for (const v of dxValues) adx += v;
          adx /= period;
          adxSeries[i] = adx;
        } else if (dxValues.length > period) {
          adx = (adx * (period - 1) + dx) / period;
          adxSeries[i] = adx;
        }
      }
    }

    // Rolling mean volume (last 20 candles, matching computeSymbolStats).
    let volumeSum = 0;
    for (let i = 0; i < n; i++) {
      volumeSum += candles[i].volume;
      if (i >= 20) volumeSum -= candles[i - 20].volume;
      const count = Math.min(20, i + 1);
      const avg = count > 0 ? volumeSum / count : 0;
      volumeRatioSeries[i] = avg > 0 ? candles[i].volume / avg : 1;
    }
  }

  void timeframe; // timeframe currently unused (annualizedVolatility is 0, matching batch behavior)

  return (barIndex: number): SymbolStatistics => {
    const i = Math.max(0, Math.min(n - 1, barIndex));
    const from = Math.max(0, i - Math.max(lookback, 20) + 1);
    const windowValues: number[] = [];
    for (let j = from; j <= i; j++) {
      if (!Number.isNaN(atrPctSeries[j])) windowValues.push(atrPctSeries[j]);
    }
    const sorted = windowValues.sort((a, b) => a - b);
    const adx14 = Number.isNaN(adxSeries[i]) ? 0 : adxSeries[i];
    return {
      atr14Pct: Number.isNaN(atrPctSeries[i]) ? 0 : atrPctSeries[i],
      atrPctMedian: percentile(sorted, 50),
      atrPct20: percentile(sorted, 20),
      atrPct80: percentile(sorted, 80),
      // Matches computeSymbolStats(candles, tf): it calls
      // calculateAnnualizedVolatility with lookback 0, which returns 0.
      annualizedVolatility: 0,
      adx14,
      isTrending: adx14 >= 25,
      volumeRatio: volumeRatioSeries[i],
      sampleSize: i + 1,
    };
  };
}

/**
 * Compute per-symbol market statistics from a candle history.
 * These values are used to adapt stop distances and position sizes so that
 * one strategy config can work across many symbols without manual tuning.
 *
 * NOTE: this batch variant evaluates statistics at the END of the given
 * series. Backtests must use `makeCausalSymbolStats` instead so each bar's
 * signal sees only data available at that bar (bd clever-cabin-dt8).
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
