import { describe, expect, it } from "bun:test";
import {
  assessBacktestRealism,
  attachMonteCarlo,
  calculatePositionValue,
  composerSweepCandidate,
  normalizeFeePct,
  runBacktest,
  sweepComposerConfigs,
} from "./backtest.js";
import { defaultComposerConfig } from "./composer.js";
import type { FundingRate } from "../market-data/types.js";
import type { CandleLike } from "./types.js";
import type { BacktestResult, BacktestTrade } from "./backtest.js";

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

/** Build a minimal `BacktestResult` carrying only the given trades. */
function makeResult(trades: BacktestTrade[]): BacktestResult {
  return {
    symbol: "BTC/USDT",
    totalTrades: trades.length,
    winningTrades: 0,
    losingTrades: 0,
    winRate: 0,
    totalReturnPct: 0,
    maxDrawdownPct: 0,
    sharpeRatio: 0,
    trades,
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

  it("accrues funding from historical rates (mean over window) instead of the constant", () => {
    const candles = makeCandles(120, 100, "up");
    const fundingRates: FundingRate[] = candles.map((c) => ({
      exchange: "binance",
      symbol: "BTC/USDT",
      fundingRate: 0.0005,
      timestamp: c.timestamp,
    }));
    const viaRates = runBacktest({
      ...baseOpts,
      isFutures: true,
      fundingRatePct: 0,
      fundingIntervalHours: 1 / 60,
      fundingRates,
    });
    const viaConstant = runBacktest({
      ...baseOpts,
      isFutures: true,
      fundingRatePct: 0.05,
      fundingIntervalHours: 1 / 60,
    });
    expect(viaRates.totalTrades).toBeGreaterThan(0);
    // Rates drive funding even when the flat constant is zero.
    expect(viaRates.totalFundingCost).toBeGreaterThan(0);
    // Identical trade sequence (rates feed only the signal when useFunding is
    // on): the mean in-window rate 0.0005 decimal = 0.05 pct-units, so the
    // rate-driven accrual matches the equivalent constant run exactly.
    expect(viaRates.totalFundingCost).toBeCloseTo(
      viaConstant.totalFundingCost,
      6,
    );
  });

  it("falls back to the flat constant when no rates fall inside the funding window", () => {
    const candles = makeCandles(120, 100, "up");
    const staleRates: FundingRate[] = [
      {
        exchange: "binance",
        symbol: "BTC/USDT",
        fundingRate: 0.0005,
        timestamp: new Date(candles[0].timestamp.getTime() - 3_600_000),
      },
    ];
    const withStaleRates = runBacktest({
      ...baseOpts,
      isFutures: true,
      fundingRatePct: 0.01,
      fundingIntervalHours: 1 / 60,
      fundingRates: staleRates,
    });
    const withoutRates = runBacktest({
      ...baseOpts,
      isFutures: true,
      fundingRatePct: 0.01,
      fundingIntervalHours: 1 / 60,
    });
    expect(withStaleRates.totalFundingCost).toBeCloseTo(
      withoutRates.totalFundingCost,
      6,
    );
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

  it("counts ruin when capital crosses zero at any step, not just at the end", () => {
    // A big early loss that later recovers: the unshuffled cumulative sum is
    // positive (1000 - 1500 + 7*500 = 3000), so the pre-fix code reported 0%
    // ruin. Some shuffled paths dip below zero mid-way, so ruin must be > 0.
    const t = (netPnl: number): BacktestTrade => ({
      id: "t",
      symbol: "BTC/USDT",
      side: "long",
      entryTime: new Date(0),
      exitTime: new Date(1),
      entryPrice: 100,
      exitPrice: 100,
      pnl: netPnl,
      pnlPct: 0,
      netPnl,
      exitReason: "signal",
      initialRiskPct: 1,
      fillType: "taker",
      entryFeePct: 0,
      exitFeePct: 0,
    });
    const trades = [
      t(-1500),
      t(500),
      t(500),
      t(500),
      t(500),
      t(500),
      t(500),
      t(500),
    ];
    const finalSum = trades.reduce((s, x) => s + x.netPnl, 0) + 1000;
    expect(finalSum).toBeGreaterThan(0);

    const result = attachMonteCarlo(makeResult(trades), 1000, 1000);
    expect(result.monteCarlo).toBeDefined();
    expect(result.monteCarlo!.probabilityOfRuinPct).toBeGreaterThan(0);
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

  it("slices HTF candles to each decision time (no look-ahead bias)", () => {
    // HTF: 80 bars downtrend then 20 bars uptrend. The final full-series view
    // is UP. Without slicing, every decision would use that final UP trend and
    // reject shorts at every bar. With slicing, early decisions (during the
    // down prefix) see the DOWN trend and correctly allow shorts.
    const down = makeCandles(80, 100, "down");
    const up = makeCandles(20, down[down.length - 1].close, "up");
    const htf: CandleLike[] = [...down, ...up];
    const candles = makeCandles(100, 100, "up");

    const result = runBacktest({
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
      htfCandles: htf,
      htfTrendFastPeriod: 10,
      htfTrendSlowPeriod: 20,
    });

    expect(result.totalTrades).toBeGreaterThan(0);
    // The first trade (during the HTF downtrend prefix) must be a short;
    // a look-ahead full-series view would have rejected it.
    expect(result.trades[0].side).toBe("short");
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
    // Widen the timestamp field so the fixture can carry an invalid value;
    // the backtest tolerates the widened type then rejects it at runtime.
    type BadTimestampCandle = Omit<CandleLike, "timestamp"> & {
      timestamp: Date | string;
    };
    const candles = makeCandles(60, 100, "up") as Array<BadTimestampCandle>;
    candles[30] = { ...candles[30], timestamp: "2025-01-01T00:00:00Z" };
    expect(() =>
      runBacktest({ ...baseOpts, candles } as Parameters<
        typeof runBacktest
      >[0]),
    ).toThrow(/invalid candle timestamp/i);
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

describe("sweepComposerConfigs", () => {
  it("ranks candidates by total return and merges thresholds over the base config", () => {
    const candles = makeCandles(300, 100, "up");
    const base = {
      symbol: "BTC/USDT",
      exchange: "test",
      timeframe: "1h",
      candles,
      initialCapital: 10_000,
      positionSizePct: 100,
      stopLossPct: 0,
      takeProfitPct: 0,
      feePct: 0.06,
      minConfidence: 0.35,
      slippageBps: 2,
      maxBarsInTrade: 12,
      htfCandles: [],
    } as const;

    const candidates = [
      composerSweepCandidate("base", {}),
      composerSweepCandidate("tight-band", {
        adxWeakTrend: 30,
        bollingerEntryMinPct: 0.1,
        bollingerEntryMaxPct: 0.9,
      }),
    ];

    const ranked = sweepComposerConfigs(base, candidates);
    expect(ranked).toHaveLength(2);
    // Sorted descending by total return.
    expect(ranked[0].totalReturnPct).toBeGreaterThanOrEqual(
      ranked[1].totalReturnPct,
    );
    // Every result carries the summary fields.
    for (const r of ranked) {
      expect(r.result.totalTrades).toBe(r.totalTrades);
      expect(r.sharpeRatio).toBe(r.result.sharpeRatio);
    }
  });

  it("candidate threshold overrides actually change the merged config", () => {
    const candles = makeCandles(300, 100, "up");
    const base = {
      symbol: "BTC/USDT",
      exchange: "test",
      timeframe: "1h",
      candles,
      initialCapital: 10_000,
      positionSizePct: 100,
      stopLossPct: 0,
      takeProfitPct: 0,
      feePct: 0.06,
      minConfidence: 0.35,
      slippageBps: 2,
      maxBarsInTrade: 12,
      htfCandles: [],
    } as const;

    const candidate = composerSweepCandidate("tight-adx", { adxWeakTrend: 40 });
    expect(candidate.composerConfig.thresholds?.adxWeakTrend).toBe(40);
    const ranked = sweepComposerConfigs(base, [candidate]);
    expect(ranked).toHaveLength(1);
  });
});
