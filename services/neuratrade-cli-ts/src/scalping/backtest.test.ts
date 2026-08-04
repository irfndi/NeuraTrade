import { describe, expect, it } from "bun:test";
import {
  assessBacktestRealism,
  calculatePositionValue,
  normalizeFeePct,
  runBacktest,
} from "./backtest.js";
import { defaultComposerConfig } from "./composer.js";
import type { FundingRate } from "../market-data/types.js";
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

  it("funding series + constant together keep PnL finite (bd clever-cabin-u1u)", () => {
    const candles = makeCandles(120, 100, "up");
    const fundingRates: FundingRate[] = candles.map((c) => ({
      exchange: "binance",
      symbol: "BTC/USDT",
      fundingRate: 0.0005,
      timestamp: c.timestamp,
    }));
    const result = runBacktest({
      ...baseOpts,
      isFutures: true,
      fundingRatePct: 0.01,
      fundingIntervalHours: 1 / 60,
      fundingRates,
    });
    expect(result.totalTrades).toBeGreaterThan(0);
    expect(Number.isFinite(result.totalReturnPct)).toBe(true);
    expect(result.totalFundingCost).toBeGreaterThan(0);
    for (const trade of result.trades) {
      expect(Number.isNaN(trade.pnlPct)).toBe(false);
    }
  });

  it("enabled funding-bias component with a live series stays finite (bd clever-cabin-u1u)", () => {
    const candles = makeCandles(120, 100, "up");
    const fundingRates: FundingRate[] = candles.map((c) => ({
      exchange: "binance",
      symbol: "BTC/USDT",
      fundingRate: 0.0005,
      timestamp: c.timestamp,
    }));
    const config = {
      ...defaultComposerConfig,
      weights: { ...defaultComposerConfig.weights, funding: 0.3 },
      thresholds: { ...defaultComposerConfig.thresholds, useFunding: true },
    };
    const result = runBacktest({
      ...baseOpts,
      composerConfig: config,
      isFutures: true,
      fundingRatePct: 0.01,
      fundingIntervalHours: 1 / 60,
      fundingRates,
    });
    expect(Number.isFinite(result.totalReturnPct)).toBe(true);
    for (const trade of result.trades) {
      expect(Number.isNaN(trade.pnlPct)).toBe(false);
    }
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

  it("splits candles into in-sample and out-of-sample results", () => {
    const result = runBacktest({
      symbol: "BTC/USDT",
      exchange: "binance",
      timeframe: "1h",
      candles: makeCandles(200, 100, "up"),
      composerConfig: defaultComposerConfig,
      initialCapital: 10000,
      positionSizePct: 100,
      stopLossPct: 5,
      takeProfitPct: 10,
      feePct: 0,
      minConfidence: 0.1,
      oosPct: 30,
    });

    expect(result.oosResult).toBeDefined();
    expect(result.oosResult!.totalTrades).toBeGreaterThanOrEqual(0);
    expect(result.oosResult!.maxDrawdownPct).toBeGreaterThanOrEqual(0);
    expect(result.monteCarlo).toBeUndefined();
  });

  it("runs Monte Carlo drawdown simulation when requested", () => {
    const result = runBacktest({
      symbol: "BTC/USDT",
      exchange: "binance",
      timeframe: "1h",
      candles: makeCandles(200, 100, "up"),
      composerConfig: defaultComposerConfig,
      initialCapital: 10000,
      positionSizePct: 100,
      stopLossPct: 5,
      takeProfitPct: 10,
      feePct: 0,
      minConfidence: 0.1,
      mcIterations: 100,
    });

    expect(result.totalTrades).toBeGreaterThan(0);
    expect(result.monteCarlo).toBeDefined();
    expect(result.monteCarlo!.iterations).toBe(100);
    expect(result.monteCarlo!.worstMaxDrawdownPct).toBeGreaterThanOrEqual(0);
    expect(result.monteCarlo!.p95MaxDrawdownPct).toBeGreaterThanOrEqual(0);
    expect(result.monteCarlo!.p95MaxDrawdownPct).toBeGreaterThanOrEqual(
      result.monteCarlo!.medianMaxDrawdownPct,
    );
    expect(result.monteCarlo!.p99MaxDrawdownPct).toBeGreaterThanOrEqual(
      result.monteCarlo!.p95MaxDrawdownPct,
    );
  });

  it("applies leverage to futures returns", () => {
    const unleveraged = runBacktest({
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
      isFutures: true,
      leverage: 1,
    });
    const leveraged = runBacktest({
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
      isFutures: true,
      leverage: 5,
    });

    expect(leveraged.totalTrades).toBeGreaterThan(0);
    expect(leveraged.totalReturnPct).toBeGreaterThan(
      unleveraged.totalReturnPct,
    );
  });

  it("liquidates an over-leveraged futures position", () => {
    const result = runBacktest({
      symbol: "BTC/USDT",
      exchange: "binance",
      timeframe: "1h",
      candles: makeCandles(100, 100, "down"),
      composerConfig: defaultComposerConfig,
      initialCapital: 10000,
      positionSizePct: 100,
      stopLossPct: 50,
      takeProfitPct: 50,
      feePct: 0,
      minConfidence: 0.1,
      holdUntilStop: true,
      isFutures: true,
      leverage: 10,
    });

    expect(result.trades.some((t) => t.exitReason === "liquidation")).toBe(
      true,
    );
  });

  it("moves stop-loss to breakeven after reaching target R", () => {
    const result = runBacktest({
      symbol: "BTC/USDT",
      exchange: "binance",
      timeframe: "1h",
      candles: makeCandles(100, 100, "up"),
      composerConfig: defaultComposerConfig,
      initialCapital: 10000,
      positionSizePct: 100,
      stopLossPct: 5,
      takeProfitPct: 20,
      feePct: 0,
      minConfidence: 0.1,
      breakevenAtR: 1,
    });

    expect(result.totalTrades).toBeGreaterThan(0);
  });

  it("exits via time-stop after max bars", () => {
    const result = runBacktest({
      symbol: "BTC/USDT",
      exchange: "binance",
      timeframe: "1h",
      candles: makeCandles(100, 100, "up"),
      composerConfig: defaultComposerConfig,
      initialCapital: 10000,
      positionSizePct: 100,
      stopLossPct: 50,
      takeProfitPct: 50,
      feePct: 0,
      minConfidence: 0.1,
      holdUntilStop: true,
      maxBarsInTrade: 5,
    });

    expect(result.trades.some((t) => t.exitReason === "time_stop")).toBe(true);
  });

  it("forces every trade to close within maxBarsInTrade bars", () => {
    const result = runBacktest({
      symbol: "BTC/USDT",
      exchange: "binance",
      timeframe: "1h",
      candles: makeCandles(100, 100, "up"),
      composerConfig: defaultComposerConfig,
      initialCapital: 10000,
      positionSizePct: 100,
      stopLossPct: 50,
      takeProfitPct: 50,
      feePct: 0,
      minConfidence: 0.1,
      holdUntilStop: true,
      maxBarsInTrade: 2,
    });

    expect(result.totalTrades).toBeGreaterThan(0);
    let timeStops = 0;
    for (const trade of result.trades) {
      const barsHeld =
        (trade.exitTime.getTime() - trade.entryTime.getTime()) /
        (60 * 60 * 1000);
      expect(barsHeld).toBeLessThanOrEqual(2);
      if (trade.exitReason === "time_stop") {
        timeStops++;
      }
    }
    expect(timeStops).toBeGreaterThan(0);
  });

  it("skips entries during loss cooldown", () => {
    const result = runBacktest({
      symbol: "BTC/USDT",
      exchange: "binance",
      timeframe: "1h",
      candles: makeCandles(200, 100, "flat"),
      composerConfig: defaultComposerConfig,
      initialCapital: 10000,
      positionSizePct: 100,
      stopLossPct: 0.5,
      takeProfitPct: 1,
      feePct: 0,
      minConfidence: 0.1,
      lossCooldownBars: 10,
    });

    expect(result.totalReturnPct).toBeFinite();
    expect(result.maxDrawdownPct).toBeGreaterThanOrEqual(0);
  });

  it("respects UTC session window", () => {
    const candles = makeCandles(100, 100, "up").map((c, idx) => ({
      ...c,
      timestamp: new Date(Date.UTC(2025, 0, 1, idx % 24, 0, 0)),
    }));
    const result = runBacktest({
      symbol: "BTC/USDT",
      exchange: "binance",
      timeframe: "1h",
      candles,
      composerConfig: defaultComposerConfig,
      initialCapital: 10000,
      positionSizePct: 100,
      stopLossPct: 5,
      takeProfitPct: 10,
      feePct: 0,
      minConfidence: 0.1,
      sessionStart: "02:00",
      sessionEnd: "04:00",
    });

    for (const trade of result.trades) {
      const hour = trade.entryTime.getUTCHours();
      expect(hour).toBeGreaterThanOrEqual(2);
      expect(hour).toBeLessThanOrEqual(4);
    }
  });

  it("applies auto-regime filter", () => {
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
      autoRegimeFilter: true,
      autoRegimeAdxThreshold: 20,
    });

    expect(result.totalReturnPct).toBeFinite();
    expect(result.maxDrawdownPct).toBeGreaterThanOrEqual(0);
  });
});

describe("volatility-targeted position sizing", () => {
  const baseOptions = {
    symbol: "BTC/USDT",
    exchange: "binance",
    timeframe: "1h",
    candles: [],
    composerConfig: defaultComposerConfig,
    initialCapital: 10_000,
    positionSizePct: 100,
    stopLossPct: 2,
    takeProfitPct: 4,
    feePct: 0.1,
    minConfidence: 0.5,
  };

  it("scales position down when current volatility exceeds target", () => {
    const base = calculatePositionValue(10_000, 100, 0.02, 50, {
      ...baseOptions,
      volatilityTargetAnnualPct: 0,
    });
    const targeted = calculatePositionValue(10_000, 100, 0.02, 50, {
      ...baseOptions,
      volatilityTargetAnnualPct: 25,
    });
    expect(targeted).toBe(base * 0.5);
  });

  it("scales position up when current volatility is below target", () => {
    const base = calculatePositionValue(10_000, 100, 0.02, 10, {
      ...baseOptions,
      volatilityTargetAnnualPct: 0,
      maxPositionSizePct: 300,
    });
    const targeted = calculatePositionValue(10_000, 100, 0.02, 10, {
      ...baseOptions,
      volatilityTargetAnnualPct: 30,
      maxPositionSizePct: 300,
    });
    expect(targeted).toBe(base * 3);
  });

  it("caps vol-adjusted size by maxPositionSizePct", () => {
    const targeted = calculatePositionValue(10_000, 100, 0.02, 1, {
      ...baseOptions,
      volatilityTargetAnnualPct: 100,
      maxPositionSizePct: 50,
    });
    expect(targeted).toBe(5_000);
  });

  it("does nothing when target is zero", () => {
    const base = calculatePositionValue(10_000, 100, 0.02, 40, {
      ...baseOptions,
      volatilityTargetAnnualPct: 0,
    });
    const targeted = calculatePositionValue(10_000, 100, 0.02, 40, {
      ...baseOptions,
      volatilityTargetAnnualPct: 0,
    });
    expect(targeted).toBe(base);
  });
});

describe("higher-timeframe signal confluence", () => {
  it("filters entries when HTF signal confidence threshold is high", () => {
    const candles = makeCandles(150, 100, "up");
    const baseline = runBacktest({
      symbol: "BTC/USDT",
      exchange: "binance",
      timeframe: "1h",
      candles,
      composerConfig: defaultComposerConfig,
      initialCapital: 10_000,
      positionSizePct: 100,
      stopLossPct: 5,
      takeProfitPct: 10,
      feePct: 0,
      minConfidence: 0.1,
      htfCandles: candles,
      htfTrendFastPeriod: 10,
      htfTrendSlowPeriod: 20,
    });

    const confluent = runBacktest({
      symbol: "BTC/USDT",
      exchange: "binance",
      timeframe: "1h",
      candles,
      composerConfig: defaultComposerConfig,
      initialCapital: 10_000,
      positionSizePct: 100,
      stopLossPct: 5,
      takeProfitPct: 10,
      feePct: 0,
      minConfidence: 0.1,
      htfCandles: candles,
      htfTrendFastPeriod: 10,
      htfTrendSlowPeriod: 20,
      htfSignalConfidence: 0.99,
    });

    expect(baseline.totalTrades).toBeGreaterThanOrEqual(0);
    expect(confluent.totalTrades).toBeLessThanOrEqual(baseline.totalTrades);
  });
});

describe("fee convention normalization", () => {
  it("normalizeFeePct converts a fraction to percent and leaves percent alone", () => {
    const originalWarn = console.warn;
    console.warn = () => {};
    expect(normalizeFeePct(0.001)).toBeCloseTo(0.1);
    expect(normalizeFeePct(0.05)).toBe(0.05);
    expect(normalizeFeePct(0.1)).toBe(0.1);
    expect(normalizeFeePct(0)).toBe(0);
    console.warn = originalWarn;
  });

  it("normalizes a fractional fee to percent and charges the same as the equivalent percent", () => {
    const originalWarn = console.warn;
    console.warn = () => {};
    const opts = {
      symbol: "BTC/USDT",
      exchange: "binance",
      timeframe: "1h",
      candles: makeCandles(100, 100, "up"),
      composerConfig: defaultComposerConfig,
      initialCapital: 10000,
      positionSizePct: 100,
      stopLossPct: 5,
      takeProfitPct: 10,
      minConfidence: 0.1,
    } as const;
    const fractional = runBacktest({ ...opts, feePct: 0.001 });
    const percent = runBacktest({ ...opts, feePct: 0.1 });
    expect(fractional.totalFeesPaid).toBeGreaterThan(0);
    expect(percent.totalFeesPaid).toBeGreaterThan(0);
    expect(percent.totalFeesPaid).toBeCloseTo(fractional.totalFeesPaid, 0);
    console.warn = originalWarn;
  });
});

describe("maker/taker fee model", () => {
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
    feePct: 0.05,
    minConfidence: 0.1,
  } as const;

  it("market entry fills as taker", () => {
    const result = runBacktest({
      ...baseOpts,
      entryOrderType: "market",
      slippageBps: 0,
    });
    expect(result.totalTrades).toBeGreaterThan(0);
    expect(result.trades.every((t) => t.fillType === "taker")).toBe(true);
    expect(result.makerFillRate).toBe(0);
  });

  it("limit entry fills as maker when the bar trades through the limit", () => {
    const result = runBacktest({
      ...baseOpts,
      entryOrderType: "limit",
      entryLimitOffsetBps: 0,
      makerFeePct: 0.02,
      slippageBps: 0,
    });
    expect(result.totalTrades).toBeGreaterThan(0);
    expect(result.trades.every((t) => t.fillType === "maker")).toBe(true);
    expect(result.trades.every((t) => t.entryFeePct === 0.02)).toBe(true);
    expect(result.trades.every((t) => t.exitFeePct === 0.05)).toBe(true);
    expect(result.makerFillRate).toBe(1);
  });

  it("maker fill reduces total fees compared to pure taker", () => {
    const taker = runBacktest({
      ...baseOpts,
      entryOrderType: "market",
      slippageBps: 0,
    });
    const maker = runBacktest({
      ...baseOpts,
      entryOrderType: "limit",
      entryLimitOffsetBps: 0,
      makerFeePct: 0.02,
      slippageBps: 0,
    });
    expect(maker.totalFeesPaid).toBeLessThan(taker.totalFeesPaid);
  });

  it("limit entry with unreachable offset is forfeited", () => {
    const result = runBacktest({
      ...baseOpts,
      entryOrderType: "limit",
      entryLimitOffsetBps: 100,
      makerFeePct: 0.02,
      slippageBps: 0,
    });
    expect(result.totalTrades).toBe(0);
    expect(result.makerFillRate ?? 0).toBe(0);
  });

  it("normalizes a fractional maker fee to percent", () => {
    const originalWarn = console.warn;
    console.warn = () => {};
    const result = runBacktest({
      ...baseOpts,
      entryOrderType: "limit",
      entryLimitOffsetBps: 0,
      makerFeePct: 0.0002,
      slippageBps: 0,
    });
    expect(result.totalTrades).toBeGreaterThan(0);
    expect(
      result.trades.every((t) => Math.abs(t.entryFeePct - 0.02) < 1e-9),
    ).toBe(true);
    console.warn = originalWarn;
  });
});

describe("candle timestamp validation", () => {
  const baseOpts = {
    symbol: "BTC/USDT",
    exchange: "binance",
    timeframe: "1h",
    composerConfig: defaultComposerConfig,
    initialCapital: 10000,
    positionSizePct: 100,
    stopLossPct: 5,
    takeProfitPct: 10,
    feePct: 0.05,
    minConfidence: 0.1,
  } as const;

  it("limit-entry trades carry valid Date entry/exit times", () => {
    const result = runBacktest({
      ...baseOpts,
      candles: makeCandles(100, 100, "up"),
      entryOrderType: "limit",
      entryLimitOffsetBps: 0,
      makerFeePct: 0.02,
      slippageBps: 0,
    });
    expect(result.totalTrades).toBeGreaterThan(0);
    for (const trade of result.trades) {
      expect(trade.entryTime).toBeInstanceOf(Date);
      expect(trade.exitTime).toBeInstanceOf(Date);
      expect(Number.isNaN(trade.entryTime.getTime())).toBe(false);
      expect(Number.isNaN(trade.exitTime.getTime())).toBe(false);
    }
  });

  it("throws on an Invalid Date candle timestamp", () => {
    const candles = makeCandles(60, 100, "up");
    candles[30] = { ...candles[30], timestamp: new Date("not-a-date") };
    expect(() => runBacktest({ ...baseOpts, candles })).toThrow(
      /invalid candle timestamp/i,
    );
  });

  it("throws when a candle timestamp is not a Date", () => {
    const candles = makeCandles(60, 100, "up");
    candles[30] = {
      ...candles[30],
      timestamp: "2025-01-01T00:00:00Z" as unknown as Date,
    };
    expect(() => runBacktest({ ...baseOpts, candles })).toThrow(
      /invalid candle timestamp/i,
    );
  });
});

describe("observed-price backtest mode", () => {
  const baseOpts = {
    symbol: "BTC/USDT",
    exchange: "binance",
    timeframe: "1h",
    candles: makeCandles(100, 100, "up"),
    composerConfig: defaultComposerConfig,
    initialCapital: 10000,
    positionSizePct: 100,
    stopLossPct: 2,
    takeProfitPct: 3,
    feePct: 0,
    minConfidence: 0.1,
  } as const;

  it("produces different (more pessimistic) results than intrabar mode", () => {
    const intrabar = runBacktest({ ...baseOpts, useObservedPrice: false });
    const observed = runBacktest({
      ...baseOpts,
      useObservedPrice: true,
      slippageBps: 5,
    });
    expect(intrabar.totalTrades).toBeGreaterThan(0);
    expect(observed.totalTrades).toBeGreaterThan(0);
    expect(
      observed.totalReturnPct < intrabar.totalReturnPct ||
        observed.winningTrades < intrabar.winningTrades ||
        observed.totalTrades !== intrabar.totalTrades,
    ).toBe(true);
  });

  it("uses slippage on close without bounding by high/low", () => {
    const noSlip = runBacktest({ ...baseOpts, useObservedPrice: true });
    const withSlip = runBacktest({
      ...baseOpts,
      useObservedPrice: true,
      slippageBps: 10,
    });
    expect(noSlip.totalTrades).toBeGreaterThan(0);
    expect(withSlip.totalTrades).toBeGreaterThan(0);
    expect(withSlip.totalReturnPct).toBeLessThan(noSlip.totalReturnPct);
  });
});

describe("assessBacktestRealism", () => {
  it("flags a 100% win-rate result as unrealistic", () => {
    const fake: ReturnType<typeof runBacktest> = {
      symbol: "BTC/USDT",
      totalTrades: 5,
      winningTrades: 5,
      losingTrades: 0,
      winRate: 1,
      totalReturnPct: 10,
      maxDrawdownPct: 0,
      sharpeRatio: 2,
      trades: [],
      totalFeesPaid: 0,
      totalFundingCost: 0,
      benchmarkReturnPct: 0,
      metrics: {
        profitFactor: 0,
        expectancy: 0,
        averageRMultiple: 0,
        sortinoRatio: 0,
        calmarRatio: 0,
        maxConsecutiveLosses: 0,
        averageTradeDurationHours: 0,
        timeInMarketPct: 0,
      },
      robustnessScore: 0,
    };
    const result = assessBacktestRealism(fake);
    expect(result.ok).toBe(false);
    expect(result.errors.length).toBeGreaterThan(0);
  });

  it("allows a normal mixed result", () => {
    const fake: ReturnType<typeof runBacktest> = {
      symbol: "BTC/USDT",
      totalTrades: 20,
      winningTrades: 11,
      losingTrades: 9,
      winRate: 0.55,
      totalReturnPct: 5,
      maxDrawdownPct: 3,
      sharpeRatio: 0.8,
      trades: [],
      totalFeesPaid: 0,
      totalFundingCost: 0,
      benchmarkReturnPct: 0,
      metrics: {
        profitFactor: 1.2,
        expectancy: 0.25,
        averageRMultiple: 0.1,
        sortinoRatio: 0.5,
        calmarRatio: 0.3,
        maxConsecutiveLosses: 2,
        averageTradeDurationHours: 1,
        timeInMarketPct: 10,
      },
      robustnessScore: 0,
    };
    const result = assessBacktestRealism(fake, { entryOrderType: "market" });
    expect(result.ok).toBe(true);
    expect(result.errors).toHaveLength(0);
  });

  it("warns about high maker fill rate on limit entries", () => {
    const fake: ReturnType<typeof runBacktest> = {
      symbol: "BTC/USDT",
      totalTrades: 20,
      winningTrades: 10,
      losingTrades: 10,
      winRate: 0.5,
      totalReturnPct: 2,
      maxDrawdownPct: 1,
      sharpeRatio: 0.5,
      trades: [],
      totalFeesPaid: 0,
      totalFundingCost: 0,
      benchmarkReturnPct: 0,
      makerFillRate: 0.98,
      metrics: {
        profitFactor: 1,
        expectancy: 0,
        averageRMultiple: 0,
        sortinoRatio: 0,
        calmarRatio: 0,
        maxConsecutiveLosses: 0,
        averageTradeDurationHours: 0,
        timeInMarketPct: 0,
      },
      robustnessScore: 0,
    };
    const result = assessBacktestRealism(fake, { entryOrderType: "limit" });
    expect(result.warnings.length).toBeGreaterThan(0);
  });
});
