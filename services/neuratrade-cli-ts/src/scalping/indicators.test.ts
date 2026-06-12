import { describe, expect, it } from "bun:test";
import { calculateEMA, calculateRSI, calculateVolatility } from "./indicators.js";
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
});
