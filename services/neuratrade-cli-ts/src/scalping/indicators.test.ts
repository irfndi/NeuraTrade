import { describe, expect, it } from "bun:test";
import {
  calculateADX,
  calculateATR,
  calculateBollingerBands,
  calculateEMA,
  calculateRSI,
  calculateVolatility,
} from "./indicators.js";
import type { CandleLike } from "./types.js";

function candle(close: number, prevClose = close): CandleLike {
  return {
    open: prevClose,
    high: Math.max(close, prevClose),
    low: Math.min(close, prevClose),
    close,
    volume: 1,
    timestamp: new Date(),
  };
}

describe("indicators", () => {
  it("calculates EMA", () => {
    const values = [10, 11, 12, 13, 14, 15, 16];
    const ema = calculateEMA(values, 3);
    expect(ema.length).toBe(7);
    expect(Number.isNaN(ema[0])).toBe(true);
    expect(Number.isNaN(ema[1])).toBe(true);
    expect(ema[ema.length - 1]).toBeGreaterThan(10);
  });

  it("calculates RSI for a strong uptrend", () => {
    const candles: CandleLike[] = [];
    let close = 100;
    for (let i = 0; i < 20; i++) {
      candles.push(candle(close, close - 1));
      close += 2;
    }
    const rsi = calculateRSI(candles, 14);
    expect(rsi).not.toBeNull();
    expect(rsi!).toBeGreaterThan(70);
  });

  it("calculates RSI for a strong downtrend", () => {
    const candles: CandleLike[] = [];
    let close = 100;
    for (let i = 0; i < 20; i++) {
      candles.push(candle(close, close + 1));
      close -= 2;
    }
    const rsi = calculateRSI(candles, 14);
    expect(rsi).not.toBeNull();
    expect(rsi!).toBeLessThan(30);
  });

  it("returns null RSI with insufficient data", () => {
    const candles = [candle(100), candle(101)];
    expect(calculateRSI(candles, 14)).toBeNull();
  });

  it("calculates volatility", () => {
    const candles = [candle(100), candle(102)];
    expect(calculateVolatility(candles)).toBe(0.02);
  });

  it("calculates ATR", () => {
    const candles: CandleLike[] = [
      { open: 100, high: 105, low: 95, close: 100, volume: 1, timestamp: new Date() },
      { open: 100, high: 110, low: 98, close: 105, volume: 1, timestamp: new Date() },
      { open: 105, high: 108, low: 100, close: 102, volume: 1, timestamp: new Date() },
      { open: 102, high: 115, low: 101, close: 110, volume: 1, timestamp: new Date() },
      { open: 110, high: 112, low: 105, close: 108, volume: 1, timestamp: new Date() },
    ];
    const atr = calculateATR(candles, 3);
    expect(atr).not.toBeNull();
    expect(atr!).toBeGreaterThan(0);
  });

  it("calculates ADX for a strong trend", () => {
    const candles: CandleLike[] = [];
    let close = 100;
    for (let i = 0; i < 50; i++) {
      const open = close;
      close *= 1.01;
      candles.push({ open, high: close * 1.002, low: open * 0.998, close, volume: 1, timestamp: new Date() });
    }
    const adx = calculateADX(candles, 14);
    expect(adx.adx).not.toBeNull();
    expect(adx.adx!).toBeGreaterThan(25);
    expect(adx.plusDI!).toBeGreaterThan(adx.minusDI!);
  });

  it("calculates Bollinger Bands", () => {
    const candles: CandleLike[] = [];
    let close = 100;
    for (let i = 0; i < 25; i++) {
      close += Math.sin(i) * 2;
      candles.push({
        open: close - 1,
        high: close + 2,
        low: close - 2,
        close,
        volume: 1,
        timestamp: new Date(),
      });
    }
    const bb = calculateBollingerBands(candles, 20);
    expect(bb).not.toBeNull();
    expect(bb!.upper).toBeGreaterThan(bb!.middle);
    expect(bb!.middle).toBeGreaterThan(bb!.lower);
    expect(bb!.percentB).toBeGreaterThanOrEqual(0);
    expect(bb!.percentB).toBeLessThanOrEqual(1);
  });
});
