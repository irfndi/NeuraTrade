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
