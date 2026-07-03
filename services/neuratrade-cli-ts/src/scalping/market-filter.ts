import {
  calculateADX,
  calculateATR,
  calculateBollingerBands,
  calculateEMA,
} from "./indicators.js";
import type { CandleLike, ComposerThresholds, Direction } from "./types.js";

/**
 * Non-directional market-quality gate. Returns false when the market is too
 * quiet, too noisy, too thin, or outside session to have a durable edge.
 */
export function passesMarketFilters(
  candles: readonly CandleLike[],
  ob: {
    readonly spreadPercent: number;
    readonly bidDepth: number;
    readonly askDepth: number;
  },
  thresholds: ComposerThresholds,
): boolean {
  if (!passesVolumeFilter(candles, thresholds)) return false;
  if (!passesSpreadLiquidityFilter(ob, thresholds)) return false;
  if (!passesSessionFilter(candles, thresholds)) return false;
  if (!passesAtrFilter(candles, thresholds)) return false;
  if (!passesAdxFilter(candles, thresholds)) return false;
  if (!passesEfficiencyRatioFilter(candles, thresholds)) return false;
  if (!passesBollingerMiddleFilter(candles, thresholds)) return false;
  return true;
}

/**
 * Directional market-quality gate. Returns false when the higher-timeframe
 * trend does not align with the proposed entry direction.
 */
export function passesDirectionalMarketFilter(
  candles: readonly CandleLike[],
  direction: Direction,
  thresholds: ComposerThresholds,
): boolean {
  if (direction === "hold") return true;
  if (!passesDIFilter(candles, direction, thresholds)) return false;
  if (!passesHigherTimeframeTrendFilter(candles, direction, thresholds))
    return false;
  return true;
}

function passesVolumeFilter(
  candles: readonly CandleLike[],
  thresholds: ComposerThresholds,
): boolean {
  const minRatio = thresholds.volumeMinRatio ?? 0;
  if (minRatio <= 0) return true;

  const lookback = thresholds.volumeLookback ?? 20;
  if (candles.length < lookback + 1) return true;

  const current = candles[candles.length - 1].volume;
  const recent = candles.slice(-lookback - 1, -1);
  const avgVolume =
    recent.reduce((sum, c) => sum + c.volume, 0) / recent.length;
  if (avgVolume <= 0) return true;

  return current >= avgVolume * minRatio;
}

function passesSpreadLiquidityFilter(
  ob: {
    readonly spreadPercent: number;
    readonly bidDepth: number;
    readonly askDepth: number;
  },
  thresholds: ComposerThresholds,
): boolean {
  if (thresholds.maxSpreadPct && thresholds.maxSpreadPct > 0) {
    if (ob.spreadPercent > thresholds.maxSpreadPct) return false;
  }
  if (thresholds.minLiquidity && thresholds.minLiquidity > 0) {
    if (ob.bidDepth + ob.askDepth < thresholds.minLiquidity) return false;
  }
  return true;
}

function passesSessionFilter(
  candles: readonly CandleLike[],
  thresholds: ComposerThresholds,
): boolean {
  const start = thresholds.sessionStart ?? "";
  const end = thresholds.sessionEnd ?? "";
  if (!start || !end) return true;

  const timestamp = candles[candles.length - 1].timestamp;
  const mins = timestamp.getUTCHours() * 60 + timestamp.getUTCMinutes();
  const [sh, sm] = start.split(":").map(Number);
  const [eh, em] = end.split(":").map(Number);
  if ([sh, sm, eh, em].some((n) => Number.isNaN(n))) return true;

  const startMins = sh * 60 + sm;
  const endMins = eh * 60 + em;
  if (startMins <= endMins) return mins >= startMins && mins <= endMins;
  return mins >= startMins || mins <= endMins;
}

function passesAtrFilter(
  candles: readonly CandleLike[],
  thresholds: ComposerThresholds,
): boolean {
  const atr = calculateATR(candles, 14);
  if (atr === null) return true;
  const close = candles[candles.length - 1].close;
  if (close <= 0) return true;
  const atrPct = atr / close;

  const adaptive =
    thresholds.useAdaptiveMarketFilters && thresholds.symbolStats;
  const maxPct = adaptive
    ? thresholds.symbolStats!.atrPct80
    : thresholds.atrMaxPctOfPrice;
  if (maxPct > 0 && atrPct > maxPct) return false;

  const minPct = adaptive
    ? thresholds.symbolStats!.atrPct20
    : (thresholds.atrMinPctOfPrice ?? 0);
  if (minPct > 0 && atrPct < minPct) return false;

  return true;
}

function passesAdxFilter(
  candles: readonly CandleLike[],
  thresholds: ComposerThresholds,
): boolean {
  const minAdx = thresholds.adxMin ?? 0;
  if (minAdx <= 0) return true;
  if (candles.length < 28) return false;

  const { adx } = calculateADX(candles, 14);
  if (adx === null) return false;
  return adx >= minAdx;
}

function passesEfficiencyRatioFilter(
  candles: readonly CandleLike[],
  thresholds: ComposerThresholds,
): boolean {
  const minER = thresholds.minEfficiencyRatio ?? 0;
  if (minER <= 0) return true;
  const lookback = 14;
  if (candles.length < lookback + 1) return false;

  const window = candles.slice(-lookback - 1);
  const netChange = Math.abs(window[window.length - 1].close - window[0].close);
  let totalMove = 0;
  for (let i = 1; i < window.length; i++) {
    totalMove += Math.abs(window[i].close - window[i - 1].close);
  }
  if (totalMove <= 0) return false;
  return netChange / totalMove >= minER;
}

function passesBollingerMiddleFilter(
  candles: readonly CandleLike[],
  thresholds: ComposerThresholds,
): boolean {
  const minPct = thresholds.bollingerEntryMinPct;
  const maxPct = thresholds.bollingerEntryMaxPct;
  if (minPct === undefined || maxPct === undefined || minPct >= maxPct) {
    return true;
  }
  if (candles.length < 20) return true;
  const bb = calculateBollingerBands(candles, 20);
  if (bb === null) return true;

  if (bb.percentB > minPct && bb.percentB < maxPct) {
    return false;
  }
  return true;
}

function passesDIFilter(
  candles: readonly CandleLike[],
  direction: Direction,
  thresholds: ComposerThresholds,
): boolean {
  const minAdx = thresholds.adxMin ?? 0;
  if (minAdx <= 0) return true;
  if (candles.length < 28) return false;

  const { plusDI, minusDI } = calculateADX(candles, 14);
  if (plusDI === null || minusDI === null) return false;

  if (direction === "buy") return plusDI > minusDI;
  if (direction === "sell") return minusDI > plusDI;
  return true;
}

function passesHigherTimeframeTrendFilter(
  candles: readonly CandleLike[],
  direction: Direction,
  thresholds: ComposerThresholds,
): boolean {
  const fastPeriod = thresholds.trendFilterFastPeriod ?? 0;
  const slowPeriod = thresholds.trendFilterSlowPeriod ?? 0;
  if (fastPeriod <= 0 || slowPeriod <= 0) return true;
  if (candles.length < slowPeriod + 1) return true;

  const closes = candles.map((c) => c.close);
  const fastEMA = calculateEMA(closes, fastPeriod);
  const slowEMA = calculateEMA(closes, slowPeriod);
  const lastFast = fastEMA[fastEMA.length - 1];
  const lastSlow = slowEMA[slowEMA.length - 1];
  if (Number.isNaN(lastFast) || Number.isNaN(lastSlow) || lastSlow === 0) {
    return true;
  }

  if (direction === "buy") return lastFast > lastSlow;
  if (direction === "sell") return lastFast < lastSlow;
  return true;
}
