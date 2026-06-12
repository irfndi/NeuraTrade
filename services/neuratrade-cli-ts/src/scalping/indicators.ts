import type { CandleLike } from "./types.js";

export function calculateEMA(values: readonly number[], period: number): number[] {
  if (values.length === 0 || period <= 0) return [];

  const multiplier = 2 / (period + 1);
  const result: number[] = [];

  // Seed with SMA
  let sum = 0;
  let count = 0;
  for (let i = 0; i < values.length; i++) {
    if (i < period) {
      sum += values[i];
      count += 1;
      if (i === period - 1) {
        const ema = sum / count;
        result.push(ema);
      } else {
        result.push(NaN);
      }
    } else {
      const ema = (values[i] - result[i - 1]) * multiplier + result[i - 1];
      result.push(ema);
    }
  }

  return result;
}

export function calculateRSI(candles: readonly CandleLike[], period = 14): number | null {
  if (candles.length < period + 1) return null;

  let avgGain = 0;
  let avgLoss = 0;

  for (let i = 1; i <= period; i++) {
    const change = candles[i].close - candles[i - 1].close;
    avgGain += Math.max(0, change);
    avgLoss += Math.max(0, -change);
  }

  avgGain /= period;
  avgLoss /= period;

  for (let i = period + 1; i < candles.length; i++) {
    const change = candles[i].close - candles[i - 1].close;
    const gain = Math.max(0, change);
    const loss = Math.max(0, -change);
    avgGain = (avgGain * (period - 1) + gain) / period;
    avgLoss = (avgLoss * (period - 1) + loss) / period;
  }

  if (avgLoss === 0) return 100;
  const rs = avgGain / avgLoss;
  return 100 - 100 / (1 + rs);
}

export function calculateVolatility(candles: readonly CandleLike[]): number | null {
  if (candles.length < 2) return null;
  const prev = candles[candles.length - 2].close;
  const curr = candles[candles.length - 1].close;
  if (prev === 0) return null;
  return Math.abs(curr - prev) / prev;
}

export function calculateATR(candles: readonly CandleLike[], period = 14): number | null {
  if (candles.length < period + 1) return null;

  const trValues: number[] = [];
  for (let i = 1; i < candles.length; i++) {
    const high = candles[i].high;
    const low = candles[i].low;
    const prevClose = candles[i - 1].close;
    const tr = Math.max(high - low, Math.abs(high - prevClose), Math.abs(low - prevClose));
    trValues.push(tr);
  }

  if (trValues.length < period) return null;

  let atr = 0;
  for (let i = 0; i < period; i++) {
    atr += trValues[i];
  }
  atr /= period;

  for (let i = period; i < trValues.length; i++) {
    atr = (atr * (period - 1) + trValues[i]) / period;
  }

  return atr;
}

export function calculateADX(
  candles: readonly CandleLike[],
  period = 14,
): { adx: number | null; plusDI: number | null; minusDI: number | null } {
  if (candles.length < period * 2 + 1) {
    return { adx: null, plusDI: null, minusDI: null };
  }

  const plusDM: number[] = [];
  const minusDM: number[] = [];
  const trValues: number[] = [];

  for (let i = 1; i < candles.length; i++) {
    const high = candles[i].high;
    const low = candles[i].low;
    const prevHigh = candles[i - 1].high;
    const prevLow = candles[i - 1].low;
    const prevClose = candles[i - 1].close;

    const upMove = high - prevHigh;
    const downMove = prevLow - low;

    plusDM.push(upMove > downMove && upMove > 0 ? upMove : 0);
    minusDM.push(downMove > upMove && downMove > 0 ? downMove : 0);

    const tr = Math.max(high - low, Math.abs(high - prevClose), Math.abs(low - prevClose));
    trValues.push(tr);
  }

  let smoothedPlusDM = 0;
  let smoothedMinusDM = 0;
  let smoothedTR = 0;

  for (let i = 0; i < period; i++) {
    smoothedPlusDM += plusDM[i];
    smoothedMinusDM += minusDM[i];
    smoothedTR += trValues[i];
  }

  const plusDIValues: number[] = [];
  const minusDIValues: number[] = [];
  const dxValues: number[] = [];

  for (let i = period; i < plusDM.length; i++) {
    smoothedPlusDM = smoothedPlusDM * (period - 1) / period + plusDM[i];
    smoothedMinusDM = smoothedMinusDM * (period - 1) / period + minusDM[i];
    smoothedTR = smoothedTR * (period - 1) / period + trValues[i];

    const plusDI = smoothedTR === 0 ? 0 : (smoothedPlusDM / smoothedTR) * 100;
    const minusDI = smoothedTR === 0 ? 0 : (smoothedMinusDM / smoothedTR) * 100;
    plusDIValues.push(plusDI);
    minusDIValues.push(minusDI);

    const dx = plusDI + minusDI === 0 ? 0 : (Math.abs(plusDI - minusDI) / (plusDI + minusDI)) * 100;
    dxValues.push(dx);
  }

  if (dxValues.length < period) {
    return { adx: null, plusDI: null, minusDI: null };
  }

  let adx = 0;
  for (let i = 0; i < period; i++) {
    adx += dxValues[i];
  }
  adx /= period;

  for (let i = period; i < dxValues.length; i++) {
    adx = (adx * (period - 1) + dxValues[i]) / period;
  }

  return {
    adx,
    plusDI: plusDIValues[plusDIValues.length - 1],
    minusDI: minusDIValues[minusDIValues.length - 1],
  };
}

export interface BollingerBands {
  readonly upper: number;
  readonly middle: number;
  readonly lower: number;
  readonly percentB: number;
}

export function calculateBollingerBands(
  candles: readonly CandleLike[],
  period = 20,
  stdDev = 2,
): BollingerBands | null {
  if (candles.length < period) return null;

  const closes = candles.slice(-period).map((c) => c.close);
  const sum = closes.reduce((a, b) => a + b, 0);
  const middle = sum / period;

  const variance = closes.reduce((acc, c) => acc + (c - middle) ** 2, 0) / period;
  const std = Math.sqrt(variance);

  const upper = middle + stdDev * std;
  const lower = middle - stdDev * std;

  const lastClose = candles[candles.length - 1].close;
  const range = upper - lower;
  const percentB = range === 0 ? 0.5 : (lastClose - lower) / range;

  return { upper, middle, lower, percentB };
}
