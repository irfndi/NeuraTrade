import { describe, expect, it } from "bun:test";
import { runWalkForward, type WalkForwardResult } from "./walk-forward.js";
import type { BacktestResult } from "./backtest.js";
import type { CandleLike } from "./types.js";
import type { SelectArgs, SelectResult } from "../cli/scalp.js";

function makeCandle(
  timestamp: Date,
  close: number,
  overrides: Partial<CandleLike> = {},
): CandleLike {
  const open = overrides.open ?? close;
  const high = overrides.high ?? close * 1.01;
  const low = overrides.low ?? close * 0.99;
  return {
    open,
    high,
    low,
    close,
    volume: 1000,
    timestamp,
    ...overrides,
  };
}

function makeCandles(count: number, startPrice = 100): CandleLike[] {
  const candles: CandleLike[] = [];
  let price = startPrice;
  const startTime = new Date("2024-01-01T00:00:00Z").getTime();
  for (let i = 0; i < count; i++) {
    price = price * (1 + Math.sin(i / 5) * 0.01);
    candles.push(
      makeCandle(new Date(startTime + i * 4 * 60 * 60 * 1000), price),
    );
  }
  return candles;
}

function makeBacktestResult(
  overrides: Partial<BacktestResult> = {},
): BacktestResult {
  return {
    symbol: "BTC/USDT",
    totalTrades: 10,
    winningTrades: 5,
    losingTrades: 5,
    winRate: 0.5,
    totalReturnPct: 0,
    maxDrawdownPct: 5,
    sharpeRatio: 1,
    trades: [],
    totalFeesPaid: 0,
    totalFundingCost: 0,
    benchmarkReturnPct: 0,
    metrics: {
      profitFactor: 1,
      expectancy: 0,
      averageRMultiple: 0,
      sortinoRatio: 1,
      calmarRatio: 0,
      maxConsecutiveLosses: 0,
      averageTradeDurationHours: 1,
      timeInMarketPct: 10,
    },
    robustnessScore: 0,
    ...overrides,
  };
}

function makeSelectArgs(overrides: Partial<SelectArgs> = {}): SelectArgs {
  return {
    exchange: "binance",
    symbol: "BTC/USDT",
    timeframe: "4h",
    capital: 10000,
    positionSize: 100,
    riskPerTrade: 0,
    maxPositionSize: 100,
    stopLoss: 1.5,
    takeProfit: 3,
    fee: 0.1,
    makerFeePct: 0,
    entryOrderType: "market",
    entryLimitOffsetBps: 0,
    minConfidence: 0.5,
    useAtrStops: true,
    atrStopMultiplier: 1.5,
    atrTakeProfitMultiplier: 2.5,
    atrRiskReward: 0,
    scaleOutAtR: 0,
    scaleOutPct: 50,
    volatilityLookback: 0,
    volatilityLowPct: 20,
    volatilityHighPct: 80,
    volatilityLowFactor: 0.8,
    volatilityHighFactor: 1.2,
    volatilityTargetAnnualPct: 0,
    priceOnly: false,
    noRsi: false,
    noTrend: false,
    holdUntilStop: false,
    regimeMode: "trend",
    breakoutLookback: 20,
    breakoutVolumeMinRatio: 1.2,
    breakoutAdxMin: 20,
    useFunding: false,
    fundingBiasThreshold: 0.0001,
    futures: false,
    fundingRatePct: 0.01,
    slippageBps: 0,
    trailingStopPct: 0,
    trailingStopAtrMultiplier: 0,
    minAtrPct: 0,
    adxMin: 0,
    volumeMinRatio: 0,
    volumeLookback: 20,
    minConfluence: 0,
    entryCandleConfirm: false,
    signalPersistence: 0,
    momentumConfirmBars: 0,
    lossConfidencePenalty: 0,
    lossConfidenceDecay: 0,
    htfTimeframe: "",
    htfTrendFastPeriod: 50,
    htfTrendSlowPeriod: 100,
    htfSignalConfidence: 0,
    entryPullbackEmaPeriod: 0,
    entryPullbackMarginPct: 0.1,
    minEfficiencyRatio: 0,
    efficiencyRatioPeriod: 20,
    rsiLongMax: 0,
    rsiShortMin: 0,
    bollingerLongMaxPctB: -1,
    bollingerShortMinPctB: 2,
    recordEquityCurve: false,
    exportTrades: "",
    oosPct: 0,
    mcIterations: 0,
    leverage: 1,
    breakevenAtR: 0,
    maxBarsInTrade: 0,
    lossCooldownBars: 0,
    sessionStart: "",
    sessionEnd: "",
    autoRegimeFilter: false,
    autoRegimeAdxThreshold: 25,
    trendSignalStyle: "slope",
    trendFastPeriod: 9,
    trendSlowPeriod: 21,
    directionalOnly: false,
    rsiFollowTrend: false,
    strictAgreement: false,
    entryOnClose: false,
    observedPrice: false,
    realistic: false,
    strictRealism: false,
    realisticSlippageBps: 5,
    universe: "",
    top: 0,
    minRobustness: 0,
    minReturnPct: 0,
    maxDrawdownPct: 30,
    minTrades: 0,
    selectLookbackCandles: 0,
    selectBy: "return",
    ...overrides,
  } as SelectArgs;
}

describe("walk-forward window slicing", () => {
  it("produces the expected number of windows for a small synthetic series", () => {
    const candles = makeCandles(300);
    const selectArgs = makeSelectArgs();
    let selectCallCount = 0;

    const selectBestForSymbol = (
      _symbol: string,
      trainCandles: readonly CandleLike[],
      _args: SelectArgs,
      _exchange: string,
    ): SelectResult | null => {
      selectCallCount++;
      expect(trainCandles.length).toBe(180);
      return {
        symbol: "BTC/USDT",
        params: {
          regimeMode: "trend",
          atrStopMultiplier: 1.5,
          atrTakeProfitMultiplier: 2.5,
          minConfidence: 0.5,
          adxMin: 0,
        },
        result: makeBacktestResult({ totalTrades: 20 }),
      };
    };

    const runSelectBacktest = (
      _symbol: string,
      testCandles: readonly CandleLike[],
      _args: SelectArgs,
      _exchange: string,
      _params: SelectResult["params"],
    ): BacktestResult => {
      expect(testCandles.length).toBe(60);
      return makeBacktestResult({ totalReturnPct: 1, totalTrades: 5 });
    };

    const result = runWalkForward({
      symbol: "BTC/USDT",
      exchange: "binance",
      candles,
      trainWindow: 180,
      testWindow: 60,
      initialCapital: 10000,
      args: selectArgs,
      selectBestForSymbol,
      runSelectBacktest,
    });

    expect(selectCallCount).toBe(2);
    expect(result.windows.length).toBe(2);
    expect(result.windows[0].trainStartIndex).toBe(0);
    expect(result.windows[0].trainEndIndex).toBe(180);
    expect(result.windows[0].testStartIndex).toBe(180);
    expect(result.windows[0].testEndIndex).toBe(240);
    expect(result.windows[1].trainStartIndex).toBe(60);
    expect(result.windows[1].trainEndIndex).toBe(240);
    expect(result.windows[1].testStartIndex).toBe(240);
    expect(result.windows[1].testEndIndex).toBe(300);
  });

  it("returns empty results when there are not enough candles", () => {
    const candles = makeCandles(100);
    const selectArgs = makeSelectArgs();

    const result = runWalkForward({
      symbol: "BTC/USDT",
      exchange: "binance",
      candles,
      trainWindow: 180,
      testWindow: 60,
      initialCapital: 10000,
      args: selectArgs,
      selectBestForSymbol: () => null,
      runSelectBacktest: () => makeBacktestResult(),
    });

    expect(result.windows.length).toBe(0);
    expect(result.totalReturnPct).toBe(0);
    expect(result.avgTradesPerWindow).toBe(0);
  });

  it("skips windows where training selection returns no result", () => {
    const candles = makeCandles(300);
    const selectArgs = makeSelectArgs();
    let selectCallCount = 0;

    const selectBestForSymbol = (
      _symbol: string,
      _candles: readonly CandleLike[],
      _args: SelectArgs,
      _exchange: string,
    ): SelectResult | null => {
      selectCallCount++;
      if (selectCallCount === 1) return null;
      return {
        symbol: "BTC/USDT",
        params: {
          regimeMode: "reversion",
          atrStopMultiplier: 2,
          atrTakeProfitMultiplier: 2.5,
          minConfidence: 0.5,
          adxMin: 20,
        },
        result: makeBacktestResult({ totalTrades: 20 }),
      };
    };

    const runSelectBacktest = (
      _symbol: string,
      _candles: readonly CandleLike[],
      _args: SelectArgs,
      _exchange: string,
      _params: SelectResult["params"],
    ): BacktestResult =>
      makeBacktestResult({ totalReturnPct: 2, totalTrades: 3 });

    const result = runWalkForward({
      symbol: "BTC/USDT",
      exchange: "binance",
      candles,
      trainWindow: 180,
      testWindow: 60,
      initialCapital: 10000,
      args: selectArgs,
      selectBestForSymbol,
      runSelectBacktest,
    });

    expect(result.windows.length).toBe(1);
    expect(result.windows[0].params.regimeMode).toBe("reversion");
    expect(result.profitableWindowsPct).toBe(100);
  });

  it("computes aggregate metrics from window results", () => {
    const candles = makeCandles(240);
    const selectArgs = makeSelectArgs();

    const selectBestForSymbol = (): SelectResult => ({
      symbol: "BTC/USDT",
      params: {
        regimeMode: "trend",
        atrStopMultiplier: 1.5,
        atrTakeProfitMultiplier: 2.5,
        minConfidence: 0.5,
        adxMin: 0,
      },
      result: makeBacktestResult({ totalTrades: 20 }),
    });

    const runSelectBacktest = (
      _symbol: string,
      _candles: readonly CandleLike[],
      _args: SelectArgs,
      _exchange: string,
      _params: SelectResult["params"],
    ): BacktestResult =>
      makeBacktestResult({
        totalReturnPct: 5,
        totalTrades: 1,
        trades: [
          {
            id: "t1",
            symbol: "BTC/USDT",
            side: "long",
            entryTime: new Date("2024-01-01T00:00:00Z"),
            exitTime: new Date("2024-01-01T04:00:00Z"),
            entryPrice: 100,
            exitPrice: 105,
            pnl: 500,
            pnlPct: 5,
            netPnl: 500,
            exitReason: "take_profit",
            initialRiskPct: 0.01,
            fillType: "taker",
            entryFeePct: 0.1,
            exitFeePct: 0.1,
          },
        ],
      });

    const result = runWalkForward({
      symbol: "BTC/USDT",
      exchange: "binance",
      candles,
      trainWindow: 180,
      testWindow: 60,
      initialCapital: 10000,
      args: selectArgs,
      selectBestForSymbol,
      runSelectBacktest,
    });

    expect(result.windows.length).toBe(1);
    expect(result.avgWindowReturnPct).toBe(5);
    expect(result.profitableWindowsPct).toBe(100);
    expect(result.avgTradesPerWindow).toBe(1);
    expect(result.totalReturnPct).toBe(5);
  });
});
