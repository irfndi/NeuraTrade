import { describe, expect, it } from "bun:test";
import {
  calculateScaleOutPrice,
  calibrateAtrStopMultiplier,
  checkRsiExit,
  computeExitLevels,
} from "./exit-engine.js";
import type { CandleLike } from "./types.js";

function makeCandles(
  count: number,
  baseClose = 100,
  volatility = 0.01,
): CandleLike[] {
  const candles: CandleLike[] = [];
  let close = baseClose;
  for (let i = 0; i < count; i++) {
    const open = close;
    close *= 1 + (Math.random() - 0.5) * volatility;
    const high = Math.max(open, close) * (1 + volatility / 2);
    const low = Math.min(open, close) * (1 - volatility / 2);
    candles.push({
      open,
      high,
      low,
      close,
      volume: 10,
      timestamp: new Date(Date.now() + i * 60000),
    });
  }
  return candles;
}

function baseOptions(
  overrides: Partial<Parameters<typeof computeExitLevels>[0]> = {},
): Parameters<typeof computeExitLevels>[0] {
  return {
    side: "long",
    entryPrice: 100,
    atr: 2,
    useAtr: true,
    atrStopMultiplier: 1.5,
    atrRiskReward: 2,
    stopLossPct: 1.5,
    takeProfitPct: 3,
    scaleOutAtR: 0,
    candles: makeCandles(50),
    volatilityLookback: 0,
    volatilityLowPct: 20,
    volatilityHighPct: 80,
    volatilityLowFactor: 0.8,
    volatilityHighFactor: 1.2,
    ...overrides,
  };
}

describe("computeExitLevels", () => {
  it("computes ATR-based stop and take-profit with risk/reward ratio", () => {
    const levels = computeExitLevels(baseOptions());
    expect(levels.stopLoss).toBeCloseTo(97, 5); // 100 - 2*1.5
    expect(levels.takeProfit).toBeCloseTo(106, 5); // 100 + 2*1.5*2
  });

  it("falls back to fixed-pct stops when useAtr is false", () => {
    const levels = computeExitLevels(
      baseOptions({ useAtr: false, stopLossPct: 2, takeProfitPct: 4 }),
    );
    expect(levels.stopLoss).toBeCloseTo(98, 5);
    expect(levels.takeProfit).toBeCloseTo(104, 5);
  });

  it("returns a scale-out price when configured", () => {
    const levels = computeExitLevels(baseOptions({ scaleOutAtR: 1 }));
    expect(levels.scaleOutPrice).toBeCloseTo(103, 5); // 100 + (100-97)*1
  });

  it("returns no scale-out price when disabled", () => {
    const levels = computeExitLevels(baseOptions({ scaleOutAtR: 0 }));
    expect(levels.scaleOutPrice).toBeNull();
  });

  it("handles short side", () => {
    const levels = computeExitLevels(
      baseOptions({ side: "short", scaleOutAtR: 1 }),
    );
    expect(levels.stopLoss).toBeCloseTo(103, 5);
    expect(levels.takeProfit).toBeCloseTo(94, 5);
    expect(levels.scaleOutPrice).toBeCloseTo(97, 5);
  });

  it("uses atrRiskReward for take-profit when positive", () => {
    const levels = computeExitLevels(baseOptions({ atrRiskReward: 1.5 }));
    expect(levels.takeProfit).toBeCloseTo(104.5, 5); // 100 + 3*1.5
  });

  it("calibrates stop multiplier in low volatility regime", () => {
    const candles = makeCandles(50, 100, 0.005);
    const mult = calibrateAtrStopMultiplier({
      entryPrice: 100,
      atr: 2,
      candles,
      atrStopMultiplier: 1.5,
      volatilityLookback: 30,
      volatilityLowPct: 20,
      volatilityHighPct: 80,
      volatilityLowFactor: 0.8,
      volatilityHighFactor: 1.2,
    });
    expect(mult).toBeGreaterThan(0);
  });

  it("calibrates stop multiplier in high volatility regime", () => {
    const candles = makeCandles(50, 100, 0.05);
    const mult = calibrateAtrStopMultiplier({
      entryPrice: 100,
      atr: 2,
      candles,
      atrStopMultiplier: 1.5,
      volatilityLookback: 30,
      volatilityLowPct: 20,
      volatilityHighPct: 80,
      volatilityLowFactor: 0.8,
      volatilityHighFactor: 1.2,
    });
    expect(mult).toBeGreaterThan(0);
  });

  it("uses per-symbol ATR% stats for adaptive stops", () => {
    const levels = computeExitLevels(
      baseOptions({
        useAdaptiveStops: true,
        adaptiveStopAtrMultiplier: 1,
        adaptiveRiskReward: 2,
        symbolStats: {
          atr14Pct: 0.02,
          atrPctMedian: 0.005,
          atrPct20: 0.003,
          atrPct80: 0.008,
          annualizedVolatility: 0.05,
          adx14: 20,
          isTrending: false,
          volumeRatio: 1,
          sampleSize: 100,
        },
      }),
    );
    // stop = 100 * (1 - 0.005 * 1) = 99.5; tp = 100 * (1 + 0.005 * 1 * 2) = 101
    expect(levels.stopLoss).toBeCloseTo(99.5, 5);
    expect(levels.takeProfit).toBeCloseTo(101, 5);
  });

  it("skips calibration when lookback is zero", () => {
    const mult = calibrateAtrStopMultiplier({
      entryPrice: 100,
      atr: 2,
      candles: makeCandles(50),
      atrStopMultiplier: 1.5,
      volatilityLookback: 0,
      volatilityLowPct: 20,
      volatilityHighPct: 80,
      volatilityLowFactor: 0.8,
      volatilityHighFactor: 1.2,
    });
    expect(mult).toBe(1.5);
  });
});

describe("calculateScaleOutPrice", () => {
  it("returns null when scaleOutAtR is zero", () => {
    expect(calculateScaleOutPrice(100, 97, 0, "long")).toBeNull();
  });

  it("computes +1R for long", () => {
    expect(calculateScaleOutPrice(100, 97, 1, "long")).toBe(103);
  });

  it("computes +2R for long", () => {
    expect(calculateScaleOutPrice(100, 97, 2, "long")).toBe(106);
  });

  it("computes -1R for short", () => {
    expect(calculateScaleOutPrice(100, 103, 1, "short")).toBe(97);
  });
});

function rsiExitCandles(closes: number[]): CandleLike[] {
  return closes.map((close, i) => ({
    open: close,
    high: close,
    low: close,
    close,
    volume: 10,
    timestamp: new Date(Date.now() + i * 60000),
  }));
}

describe("checkRsiExit", () => {
  it("exits a long when RSI rises above the long exit level", () => {
    const candles = rsiExitCandles([100, 110, 120]);
    expect(
      checkRsiExit({
        side: "long",
        candles,
        exitRsiPeriod: 2,
        exitRsiLongLevel: 70,
        exitRsiShortLevel: 30,
      }),
    ).toBe(true);
  });

  it("does not exit a long when RSI is below the long exit level", () => {
    const candles = rsiExitCandles([120, 110, 100]);
    expect(
      checkRsiExit({
        side: "long",
        candles,
        exitRsiPeriod: 2,
        exitRsiLongLevel: 70,
        exitRsiShortLevel: 30,
      }),
    ).toBe(false);
  });

  it("exits a short when RSI falls below the short exit level", () => {
    const candles = rsiExitCandles([120, 110, 100]);
    expect(
      checkRsiExit({
        side: "short",
        candles,
        exitRsiPeriod: 2,
        exitRsiLongLevel: 70,
        exitRsiShortLevel: 30,
      }),
    ).toBe(true);
  });

  it("does not exit a short when RSI is above the short exit level", () => {
    const candles = rsiExitCandles([100, 110, 120]);
    expect(
      checkRsiExit({
        side: "short",
        candles,
        exitRsiPeriod: 2,
        exitRsiLongLevel: 70,
        exitRsiShortLevel: 30,
      }),
    ).toBe(false);
  });

  it("is disabled when exitRsiPeriod is zero", () => {
    const candles = rsiExitCandles([100, 110, 120]);
    expect(
      checkRsiExit({
        side: "long",
        candles,
        exitRsiPeriod: 0,
        exitRsiLongLevel: 70,
        exitRsiShortLevel: 30,
      }),
    ).toBe(false);
  });
});
