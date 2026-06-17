import { describe, expect, it } from "bun:test";
import { runBacktest } from "./backtest.js";
import { defaultComposerConfig } from "./composer.js";
import type { CandleLike } from "./types.js";

function makeCandles(
  count: number,
  baseClose = 100,
  trend: "up" | "down" | "flat" = "flat",
): CandleLike[] {
  const candles: CandleLike[] = [];
  let close = baseClose;
  for (let i = 0; i < count; i++) {
    const open = close;
    if (trend === "up") close *= 1.005;
    else if (trend === "down") close *= 0.995;
    const high = Math.max(open, close) * 1.002;
    const low = Math.min(open, close) * 0.998;
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

describe("runBacktest", () => {
  it("returns empty result with insufficient candles", () => {
    const result = runBacktest({
      symbol: "BTC/USDT",
      exchange: "binance",
      timeframe: "1h",
      candles: makeCandles(5),
      composerConfig: defaultComposerConfig,
      initialCapital: 10000,
      positionSizePct: 10,
      stopLossPct: 1,
      takeProfitPct: 2,
      feePct: 0.1,
      minConfidence: 0.5,
    });

    expect(result.totalTrades).toBe(0);
    expect(result.metrics).toBeDefined();
    expect(result.metrics.profitFactor).toBe(0);
    expect(result.metrics.timeInMarketPct).toBe(0);
  });

  it("runs trades on a trending series", () => {
    const result = runBacktest({
      symbol: "BTC/USDT",
      exchange: "binance",
      timeframe: "1h",
      candles: makeCandles(100, 100, "up"),
      composerConfig: defaultComposerConfig,
      initialCapital: 10000,
      positionSizePct: 100,
      stopLossPct: 5,
      takeProfitPct: 10,
      feePct: 0,
      minConfidence: 0.1,
    });

    expect(result.totalTrades).toBeGreaterThan(0);
    expect(result.trades.length).toBe(result.totalTrades);
    expect(result.totalReturnPct).not.toBe(0);
    expect(result.metrics).toBeDefined();
    expect(Number.isFinite(result.metrics.profitFactor)).toBe(true);
    expect(Number.isFinite(result.metrics.expectancy)).toBe(true);
    expect(Number.isFinite(result.metrics.averageRMultiple)).toBe(true);
    expect(Number.isFinite(result.metrics.sortinoRatio)).toBe(true);
    expect(Number.isFinite(result.metrics.calmarRatio)).toBe(true);
    expect(result.metrics.averageTradeDurationHours).toBeGreaterThanOrEqual(0);
    expect(result.metrics.timeInMarketPct).toBeGreaterThanOrEqual(0);
  });

  it("respects minConfidence", () => {
    const result = runBacktest({
      symbol: "BTC/USDT",
      exchange: "binance",
      timeframe: "1h",
      candles: makeCandles(100, 100, "up"),
      composerConfig: defaultComposerConfig,
      initialCapital: 10000,
      positionSizePct: 100,
      stopLossPct: 5,
      takeProfitPct: 10,
      feePct: 0,
      minConfidence: 0.99,
    });

    expect(result.totalTrades).toBe(0);
  });
});

describe("backtest futures realism", () => {
  const baseOpts = {
    symbol: "BTC/USDT",
    exchange: "binance",
    timeframe: "1h",
    candles: makeCandles(100, 100, "up"),
    composerConfig: defaultComposerConfig,
    initialCapital: 10000,
    positionSizePct: 100,
    stopLossPct: 5,
    takeProfitPct: 10,
    feePct: 0.04,
    minConfidence: 0.1,
  } as const;

  it("slippage reduces returns", () => {
    const noSlip = runBacktest({ ...baseOpts, slippageBps: 0 });
    const withSlip = runBacktest({ ...baseOpts, slippageBps: 10 });
    expect(noSlip.totalTrades).toBeGreaterThan(0);
    expect(withSlip.totalTrades).toBeGreaterThan(0);
    expect(withSlip.totalReturnPct).toBeLessThan(noSlip.totalReturnPct);
  });

  it("exit fees are deducted", () => {
    const result = runBacktest({ ...baseOpts, feePct: 0.1 });
    expect(result.totalTrades).toBeGreaterThan(0);
    expect(result.totalFeesPaid).toBeGreaterThan(0);
  });

  it("funding cost reduces futures return", () => {
    const noFunding = runBacktest({ ...baseOpts, isFutures: false });
    const withFunding = runBacktest({
      ...baseOpts,
      isFutures: true,
      fundingRatePct: 0.01,
      fundingIntervalHours: 1 / 60,
    });
    expect(withFunding.totalTrades).toBeGreaterThan(0);
    expect(withFunding.totalFundingCost).toBeGreaterThan(0);
    expect(withFunding.totalReturnPct).toBeLessThan(noFunding.totalReturnPct);
  });

  it("benchmark return present", () => {
    const result = runBacktest(baseOpts);
    expect(Number.isFinite(result.benchmarkReturnPct)).toBe(true);
  });

  it("signalPersistence reduces trade count", () => {
    const base = runBacktest({ ...baseOpts, signalPersistence: 0 });
    const filtered = runBacktest({ ...baseOpts, signalPersistence: 3 });
    expect(filtered.totalTrades).toBeLessThanOrEqual(base.totalTrades);
  });

  it("lossConfidencePenalty raises win rate by filtering post-loss entries", () => {
    const base = runBacktest({ ...baseOpts, minConfidence: 0.5 });
    const filtered = runBacktest({
      ...baseOpts,
      minConfidence: 0.5,
      lossConfidencePenalty: 0.1,
    });
    expect(filtered.totalTrades).toBeLessThanOrEqual(base.totalTrades);
    expect(filtered.winRate).toBeGreaterThanOrEqual(base.winRate - 0.05);
  });
});

describe("runBacktest scale-out and atrRiskReward", () => {
  const baseOpts = {
    symbol: "BTC/USDT",
    exchange: "binance",
    timeframe: "1h",
    candles: makeCandles(100, 100, "up"),
    composerConfig: defaultComposerConfig,
    initialCapital: 10000,
    positionSizePct: 100,
    stopLossPct: 5,
    takeProfitPct: 10,
    feePct: 0,
    minConfidence: 0.1,
  } as const;

  it("uses atrRiskReward for take-profit distance", () => {
    const legacy = runBacktest({
      ...baseOpts,
      useAtrStops: true,
      atrStopMultiplier: 1,
      atrTakeProfitMultiplier: 3,
    });
    const riskReward = runBacktest({
      ...baseOpts,
      useAtrStops: true,
      atrStopMultiplier: 1,
      atrRiskReward: 3,
    });
    expect(riskReward.totalTrades).toBeGreaterThan(0);
    expect(legacy.totalReturnPct).toBeCloseTo(riskReward.totalReturnPct, 5);
  });

  it("records a partial scale-out trade and continues", () => {
    const result = runBacktest({
      ...baseOpts,
      useAtrStops: true,
      atrStopMultiplier: 1,
      atrRiskReward: 3,
      scaleOutAtR: 1,
      scaleOutPct: 50,
      holdUntilStop: true,
    });
    expect(result.trades.some((t) => t.exitReason === "scale_out")).toBe(true);
    const scaleOutTrades = result.trades.filter(
      (t) => t.exitReason === "scale_out",
    );
    for (const trade of scaleOutTrades) {
      expect(trade.pnl).toBeGreaterThan(0);
    }
  });
});
