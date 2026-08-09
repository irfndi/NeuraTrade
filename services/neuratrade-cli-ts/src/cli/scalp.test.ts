import { describe, expect, it } from "bun:test";
import type { BacktestResult } from "../scalping/backtest.js";
import { applyPreset } from "../scalping/presets.js";
import { VALIDATED_BTC_GRID_CANDIDATE } from "../scalping/grid-candidate.js";
import {
  buildStrategyProfileFromArgs,
  type ResolvedBacktestArgs,
} from "../scalping/strategy-profile.js";
import type { CandleLike, ComposerConfig } from "../scalping/types.js";
import { Command } from "./kit/kit.ts";
import {
  buildCandidate,
  buildPaperTradeComposerConfig,
  buildStrategyProfileFromOptimizeResult,
  combineWalkForwardResults,
  extractExplicitOverrides,
  generateCandidates,
  generateWalkForwardWindows,
  isLiveReady,
  libraryCommand,
  loadSelectWatchlist,
  objectiveValue,
  runSelectBacktest,
  selectBestForSymbol,
  selectWinner,
  validateWatchlist,
  validateLiveExecutionMarket,
  validateLiveExecutionStrategy,
  resolveFuturesMarketExchange,
  gridOverridesFromWatchlistRow,
  validateLiveGridConfiguration,
  validateLiveGridWatchlist,
  validateLiveSoakExecution,
  probeNamesProbedSymbol,
  walkForwardCommand,
  buildValidateBacktestArgs,
  type LiveGridConfiguration,
  type OptimizeArgs,
  type OptimizeCandidateParams,
  type OptimizeResult,
  type SelectArgs,
  type SelectWatchlistEntry,
  type ValidationRow,
} from "./scalp.js";

describe("live execution market guard", () => {
  it("rejects the un-gated spot live path", () => {
    expect(validateLiveExecutionMarket(true, false)).toContain(
      "live spot execution is disabled",
    );
  });

  it("allows the backend-gated futures path", () => {
    expect(validateLiveExecutionMarket(true, true)).toBeUndefined();
  });

  it("rejects the unproven directional signal path for live execution", () => {
    expect(validateLiveExecutionStrategy(true, "signal")).toContain(
      "live directional signal execution is disabled",
    );
    expect(validateLiveExecutionStrategy(true, "grid")).toBeUndefined();
    expect(validateLiveExecutionStrategy(false, "signal")).toBeUndefined();
  });

  it("routes default futures market data to the Bitget futures gateway", () => {
    expect(resolveFuturesMarketExchange("binance", true)).toBe(
      "bitget-futures",
    );
    expect(resolveFuturesMarketExchange("bitget-futures", true)).toBe(
      "bitget-futures",
    );
    expect(resolveFuturesMarketExchange("binance", false)).toBe("binance");
  });

  it("accepts only the validated BTC grid profile for live execution", () => {
    const config = {
      exchange: VALIDATED_BTC_GRID_CANDIDATE.exchange,
      symbol: VALIDATED_BTC_GRID_CANDIDATE.symbol,
      timeframe: VALIDATED_BTC_GRID_CANDIDATE.timeframe,
      productType: VALIDATED_BTC_GRID_CANDIDATE.productType,
      gridStepPct: VALIDATED_BTC_GRID_CANDIDATE.gridStepPct,
      gridMaxGrids: VALIDATED_BTC_GRID_CANDIDATE.gridMaxGrids,
      gridPauseAfterLossBars:
        VALIDATED_BTC_GRID_CANDIDATE.gridPauseAfterLossBars,
      feePct: VALIDATED_BTC_GRID_CANDIDATE.feePct,
      slippageBps: VALIDATED_BTC_GRID_CANDIDATE.slippageBps,
      trendFilterPeriod: VALIDATED_BTC_GRID_CANDIDATE.trendFilterPeriod,
      onlyWithTrend: VALIDATED_BTC_GRID_CANDIDATE.onlyWithTrend,
      targetRatio: VALIDATED_BTC_GRID_CANDIDATE.targetRatio,
      chopGateAdx: VALIDATED_BTC_GRID_CANDIDATE.chopGateAdx,
      leverage: VALIDATED_BTC_GRID_CANDIDATE.leverage,
      maxPositionSizePct: VALIDATED_BTC_GRID_CANDIDATE.maxPositionSizePct,
      maxDrawdownPct: VALIDATED_BTC_GRID_CANDIDATE.maxDrawdownPct,
      maxDailyLossPct: VALIDATED_BTC_GRID_CANDIDATE.maxDailyLossPct,
    };

    expect(validateLiveGridConfiguration(config)).toBeUndefined();
    expect(
      validateLiveGridConfiguration({ ...config, gridStepPct: 0.5 }),
    ).toContain("validated readiness cohort");
    expect(
      validateLiveGridConfiguration({ ...config, maxPositionSizePct: 51 }),
    ).toContain("50%");
    expect(
      validateLiveGridConfiguration({ ...config, maxPositionSizePct: 0 }),
    ).toContain("between 0% and 50%");
  });

  it("rejects any drift from the audited candidate fields", () => {
    const config = {
      exchange: VALIDATED_BTC_GRID_CANDIDATE.exchange,
      symbol: VALIDATED_BTC_GRID_CANDIDATE.symbol,
      timeframe: VALIDATED_BTC_GRID_CANDIDATE.timeframe,
      productType: VALIDATED_BTC_GRID_CANDIDATE.productType,
      gridStepPct: VALIDATED_BTC_GRID_CANDIDATE.gridStepPct,
      gridMaxGrids: VALIDATED_BTC_GRID_CANDIDATE.gridMaxGrids,
      gridPauseAfterLossBars:
        VALIDATED_BTC_GRID_CANDIDATE.gridPauseAfterLossBars,
      feePct: VALIDATED_BTC_GRID_CANDIDATE.feePct,
      slippageBps: VALIDATED_BTC_GRID_CANDIDATE.slippageBps,
      trendFilterPeriod: VALIDATED_BTC_GRID_CANDIDATE.trendFilterPeriod,
      onlyWithTrend: VALIDATED_BTC_GRID_CANDIDATE.onlyWithTrend,
      targetRatio: VALIDATED_BTC_GRID_CANDIDATE.targetRatio,
      chopGateAdx: VALIDATED_BTC_GRID_CANDIDATE.chopGateAdx,
      leverage: VALIDATED_BTC_GRID_CANDIDATE.leverage,
      maxPositionSizePct: VALIDATED_BTC_GRID_CANDIDATE.maxPositionSizePct,
      maxDrawdownPct: VALIDATED_BTC_GRID_CANDIDATE.maxDrawdownPct,
      maxDailyLossPct: VALIDATED_BTC_GRID_CANDIDATE.maxDailyLossPct,
    };

    const drifts: Array<Partial<LiveGridConfiguration>> = [
      { exchange: "binance" },
      { symbol: "ETH/USDT:USDT" },
      { timeframe: "5m" },
      { productType: "SPOT" },
      { gridStepPct: 0.5 },
      { gridMaxGrids: 2 },
      { gridPauseAfterLossBars: 0 },
      { feePct: 0.06 },
      { slippageBps: 2 },
      { trendFilterPeriod: 96 },
      { onlyWithTrend: true },
      { targetRatio: 1 },
      { chopGateAdx: 20 },
    ];

    for (const drift of drifts) {
      expect(validateLiveGridConfiguration({ ...config, ...drift })).toContain(
        "validated readiness cohort",
      );
    }

    // Leverage may exceed the candidate (sizing floor-raise for tiny
    // accounts); the risk engine caps it. Strategy params cannot drift.
    expect(
      validateLiveGridConfiguration({ ...config, leverage: 2 }),
    ).toBeUndefined();

    expect(
      validateLiveGridConfiguration(
        { ...config, symbol: "ETH/USDT:USDT" },
        true,
      ),
    ).toBeUndefined();
  });

  it("enforces the live drawdown and daily-loss risk caps", () => {
    const config = {
      exchange: VALIDATED_BTC_GRID_CANDIDATE.exchange,
      symbol: VALIDATED_BTC_GRID_CANDIDATE.symbol,
      timeframe: VALIDATED_BTC_GRID_CANDIDATE.timeframe,
      productType: VALIDATED_BTC_GRID_CANDIDATE.productType,
      gridStepPct: VALIDATED_BTC_GRID_CANDIDATE.gridStepPct,
      gridMaxGrids: VALIDATED_BTC_GRID_CANDIDATE.gridMaxGrids,
      gridPauseAfterLossBars:
        VALIDATED_BTC_GRID_CANDIDATE.gridPauseAfterLossBars,
      feePct: VALIDATED_BTC_GRID_CANDIDATE.feePct,
      slippageBps: VALIDATED_BTC_GRID_CANDIDATE.slippageBps,
      trendFilterPeriod: VALIDATED_BTC_GRID_CANDIDATE.trendFilterPeriod,
      onlyWithTrend: VALIDATED_BTC_GRID_CANDIDATE.onlyWithTrend,
      targetRatio: VALIDATED_BTC_GRID_CANDIDATE.targetRatio,
      chopGateAdx: VALIDATED_BTC_GRID_CANDIDATE.chopGateAdx,
      leverage: VALIDATED_BTC_GRID_CANDIDATE.leverage,
      maxPositionSizePct: VALIDATED_BTC_GRID_CANDIDATE.maxPositionSizePct,
      maxDrawdownPct: VALIDATED_BTC_GRID_CANDIDATE.maxDrawdownPct,
      maxDailyLossPct: VALIDATED_BTC_GRID_CANDIDATE.maxDailyLossPct,
    };

    expect(
      validateLiveGridConfiguration({ ...config, maxDrawdownPct: 5.1 }),
    ).toContain("max drawdown must be between 0% and 5%");
    expect(
      validateLiveGridConfiguration({ ...config, maxDrawdownPct: -1 }),
    ).toContain("max drawdown must be between 0% and 5%");
    expect(
      validateLiveGridConfiguration({ ...config, maxDailyLossPct: 2.1 }),
    ).toContain("max daily loss must be between 0% and 2%");
    expect(
      validateLiveGridConfiguration({ ...config, maxDailyLossPct: Number.NaN }),
    ).toContain("max daily loss must be between 0% and 2%");
  });

  it("disables the directional multi-symbol live soak surface", () => {
    expect(validateLiveSoakExecution(true)).toContain("live soak is disabled");
    expect(validateLiveSoakExecution(false)).toBeUndefined();
  });

  it("disables live grid watchlists that can substitute unvalidated symbols", () => {
    expect(
      validateLiveGridWatchlist(true, "grid", [{ symbol: "ETH/USDT:USDT" }]),
    ).toContain("live grid watchlists are disabled");
    expect(validateLiveGridWatchlist(true, "grid", [])).toBeUndefined();
    expect(
      validateLiveGridWatchlist(false, "grid", [{ symbol: "ETH/USDT:USDT" }]),
    ).toBeUndefined();
    expect(
      validateLiveGridWatchlist(
        true,
        "grid",
        [{ symbol: "ETH/USDT:USDT" }],
        true,
      ),
    ).toBeUndefined();
  });
});

describe("watchlist grid overrides (soak reproduction)", () => {
  const baseArgs = {
    targetRatio: 1,
    chopGateAdx: 0,
    maxPositionSizePct: Option.some(50),
  };

  it("reproduces the row's validated targetRatio and chopGateAdx", () => {
    const overrides = gridOverridesFromWatchlistRow(
      {
        gridStepPct: 0.5,
        gridMaxGrids: 2,
        gridPauseAfterLossBars: 6,
        targetRatio: 1.5,
        chopGateAdx: 25,
        allocatedWeight: 0.5,
      },
      baseArgs,
    );
    expect(overrides.targetRatio).toBe(1.5);
    expect(overrides.chopGateAdxThreshold).toBe(25);
  });

  it("falls back to CLI defaults when the row has no validated config", () => {
    const overrides = gridOverridesFromWatchlistRow(
      { gridStepPct: 0.5, gridMaxGrids: 2, gridPauseAfterLossBars: 6 },
      baseArgs,
    );
    expect(overrides.targetRatio).toBe(1);
    expect(overrides.chopGateAdxThreshold).toBe(0);
  });

  it("sizes maxPositionPct from allocatedWeight and the CLI base fraction", () => {
    // basePositionFraction = 50/100 = 0.5; weight 0.4 -> fraction 0.2 -> 20%
    const overrides = gridOverridesFromWatchlistRow(
      {
        gridStepPct: 0.5,
        gridMaxGrids: 2,
        gridPauseAfterLossBars: 6,
        allocatedWeight: 0.4,
      },
      baseArgs,
    );
    expect(overrides.maxPositionPct).toBeCloseTo(20, 6);
  });

  it("clamps allocatedWeight to [0.01, 1] with legacy 0 as full allocation", () => {
    const zero = gridOverridesFromWatchlistRow(
      { gridStepPct: 0.5, gridMaxGrids: 2, gridPauseAfterLossBars: 6, allocatedWeight: 0 },
      baseArgs,
    );
    // Legacy rows (pre-allocated_weight) load 0 = UNSET -> full allocation:
    // without this, positions collapsed to 0.01 x base and were rejected.
    expect(zero.maxPositionPct).toBe(50); // 1 * 0.5 * 100

    const tiny = gridOverridesFromWatchlistRow(
      { gridStepPct: 0.5, gridMaxGrids: 2, gridPauseAfterLossBars: 6, allocatedWeight: 0.01 },
      baseArgs,
    );
    expect(tiny.maxPositionPct).toBeCloseTo(0.5, 6); // 0.01 * 0.5 * 100

    const over = gridOverridesFromWatchlistRow(
      { gridStepPct: 0.5, gridMaxGrids: 2, gridPauseAfterLossBars: 6, allocatedWeight: 3 },
      baseArgs,
    );
    expect(over.maxPositionPct).toBe(50); // 1 * 0.5 * 100

    const missing = gridOverridesFromWatchlistRow(
      { gridStepPct: 0.5, gridMaxGrids: 2, gridPauseAfterLossBars: 6 },
      baseArgs,
    );
    expect(missing.maxPositionPct).toBe(50); // 1 * 0.5 * 100
  });

  it("keeps the CLI base position when maxPositionSizePct is unset", () => {
    const overrides = gridOverridesFromWatchlistRow(
      { gridStepPct: 0.5, gridMaxGrids: 2, gridPauseAfterLossBars: 6, allocatedWeight: 0.4 },
      { targetRatio: 1, chopGateAdx: 0, maxPositionSizePct: Option.none() },
    );
    expect(overrides.maxPositionPct).toBeCloseTo(40, 6); // 0.4 * 1.0 * 100
  });
});
import { Effect, Layer, Option } from "effect";
import { BunServices } from "@effect/platform-bun";
import { PathLive } from "../services/path.js";
import { Database } from "bun:sqlite";
import * as fs from "node:fs";
import * as path from "node:path";
import { MarketDataRepositorySQLite } from "../market-data/repository.js";
import {
  MarketDataRepository,
  MarketDataRepositorySQLiteLive,
} from "../market-data/repository.js";
import { BacktestEngine } from "../scalping/services.js";
import { backtestProgram, buildBacktestComposerConfig } from "./scalp.js";
import type { Candle } from "../market-data/types.js";

function makeOptimizeArgs(overrides: Partial<OptimizeArgs> = {}): OptimizeArgs {
  return {
    exchange: "binance",
    symbol: "BTC/USDT",
    timeframe: "1h",
    capital: 10000,
    positionSize: 10,
    riskPerTrade: 1,
    maxPositionSize: 50,
    fee: 0.001,
    makerFeePct: 0,
    entryOrderType: "market",
    entryLimitOffsetBps: 0,
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
    atrRiskReward: 0,
    scaleOutAtR: 0,
    scaleOutPct: 0,
    volatilityLookback: 20,
    volatilityLowPct: 20,
    volatilityHighPct: 80,
    volatilityLowFactor: 1,
    volatilityHighFactor: 1,
    volatilityTargetAnnualPct: 0,
    atrStopMin: 1,
    atrStopMax: 1,
    atrStopStep: 0,
    atrTpMin: 2,
    atrTpMax: 2,
    atrTpStep: 0,
    confMin: 0.6,
    confMax: 0.6,
    confStep: 0,
    volumeMinRatio: 0,
    volumeLookback: 20,
    minConfluence: 0,
    entryCandleConfirm: false,
    momentumConfirmBars: 0,
    oosPct: 0,
    mcIterations: 0,
    walkForward: false,
    wfTrainDays: 180,
    wfTestDays: 60,
    wfStepDays: 60,
    minTrades: 0,
    minOosTrades: 0,
    selectBy: "return",
    noAtr: false,
    randomSearch: 0,
    breakevenAtRMin: 1,
    breakevenAtRMax: 1,
    breakevenAtRStep: 0,
    maxBarsInTradeMin: 24,
    maxBarsInTradeMax: 24,
    maxBarsInTradeStep: 0,
    lossCooldownBarsMin: 0,
    lossCooldownBarsMax: 0,
    lossCooldownBarsStep: 0,
    adxMinMin: 0,
    adxMinMax: 0,
    adxMinStep: 0,
    minEfficiencyRatioMin: 0,
    minEfficiencyRatioMax: 0,
    minEfficiencyRatioStep: 0,
    rsiLongMaxMin: 70,
    rsiLongMaxMax: 70,
    rsiLongMaxStep: 0,
    rsiShortMinMin: 30,
    rsiShortMinMax: 30,
    rsiShortMinStep: 0,
    stopLossMin: 1,
    stopLossMax: 1,
    stopLossStep: 0,
    takeProfitMin: 2,
    takeProfitMax: 2,
    takeProfitStep: 0,
    htfTimeframe: "",
    htfSignalConfidence: 0,
    scanEntryOrders: false,
    observedPrice: false,
    strictRealism: false,
    realistic: false,
    slippageBps: 0,
    realisticSlippageBps: 5,
    rsiPeriod: 14,
    rsiOversoldStrong: 30,
    rsiOverboughtStrong: 70,
    stopLoss: 1.5,
    takeProfit: 3.0,
    minConfidence: 0.5,
    useAtrStops: false,
    atrStopMultiplier: 1.5,
    atrTakeProfitMultiplier: 2.5,
    futures: false,
    fundingRatePct: 0.01,
    trailingStopPct: 0,
    trailingStopAtrMultiplier: 0,
    minAtrPct: 0,
    adxMin: 0,
    signalPersistence: 0,
    lossConfidencePenalty: 0,
    lossConfidenceDecay: 0,
    htfTrendFastPeriod: 50,
    htfTrendSlowPeriod: 100,
    entryPullbackEmaPeriod: 0,
    entryPullbackMarginPct: 0.1,
    minEfficiencyRatio: 0,
    efficiencyRatioPeriod: 20,
    rsiLongMax: 0,
    rsiShortMin: 0,
    bollingerLongMaxPctB: -1,
    bollingerShortMinPctB: 2,
    trendFilterPeriod: 200,
    entryRsiLongThreshold: 10,
    entryRsiShortThreshold: 90,
    exitRsiPeriod: 0,
    exitRsiLongLevel: 0,
    exitRsiShortLevel: 0,
    recordEquityCurve: false,
    exportTrades: "",
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
    strategyType: "signal",
    gridStepPct: 0,
    gridMaxGrids: 0,
    gridPauseAfterLossBars: 0,
    ...overrides,
  };
}

function makeResult(overrides: Partial<BacktestResult> = {}): BacktestResult {
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

function makeOptimizeResult(
  params: OptimizeCandidateParams,
  isResult: Partial<BacktestResult> = {},
  oosResult?: Partial<BacktestResult>,
): OptimizeResult {
  return {
    params,
    isResult: makeResult(isResult),
    oosResult: oosResult ? makeResult(oosResult) : undefined,
  };
}

describe("optimizer candidate generation", () => {
  it("buildCandidate maps dimensions to parameters", () => {
    const c = buildCandidate(true, [1.5, 3, 0.7, 1, 12, 6, 20, 0.3, 65, 35]);
    expect(c).toEqual({
      useAtrStops: true,
      stopMult: 1.5,
      tpMult: 3,
      stopLossPct: 0,
      takeProfitPct: 0,
      minConfidence: 0.7,
      breakevenAtR: 1,
      maxBarsInTrade: 12,
      lossCooldownBars: 6,
      adxMin: 20,
      minEfficiencyRatio: 0.3,
      rsiLongMax: 65,
      rsiShortMin: 35,
      entryOrderType: "market",
      entryLimitOffsetBps: 0,
    });
  });

  it("buildCandidate maps fixed-pct stops when ATR is disabled", () => {
    const c = buildCandidate(false, [1.5, 3, 0.7, 1, 12, 6, 0, 0, 70, 30]);
    expect(c.stopLossPct).toBe(1.5);
    expect(c.takeProfitPct).toBe(3);
    expect(c.stopMult).toBe(0);
    expect(c.tpMult).toBe(0);
  });

  it("generateCandidates produces the full Cartesian grid by default", () => {
    const args = makeOptimizeArgs({
      atrStopMin: 1,
      atrStopMax: 2,
      atrStopStep: 1,
      confMin: 0.5,
      confMax: 0.6,
      confStep: 0.1,
    });
    const candidates = generateCandidates(args);
    expect(candidates.length).toBe(4);
    expect(new Set(candidates.map((c) => c.stopMult)).size).toBe(2);
    expect(new Set(candidates.map((c) => c.minConfidence)).size).toBe(2);
  });

  it("generateCandidates respects --no-atr", () => {
    const args = makeOptimizeArgs({
      noAtr: true,
      stopLossMin: 1,
      stopLossMax: 2,
      stopLossStep: 1,
      takeProfitMin: 2,
      takeProfitMax: 3,
      takeProfitStep: 1,
    });
    const candidates = generateCandidates(args);
    expect(candidates.every((c) => !c.useAtrStops)).toBe(true);
    expect(candidates[0].stopLossPct).toBe(1);
  });

  it("generateCandidates supports random search", () => {
    const args = makeOptimizeArgs({ randomSearch: 5 });
    const candidates = generateCandidates(args);
    expect(candidates.length).toBe(5);
  });

  it("generateCandidates expands order types and limit offsets when scanning", () => {
    const args = makeOptimizeArgs({
      scanEntryOrders: true,
      atrStopMin: 1,
      atrStopMax: 1,
      atrStopStep: 0,
      confMin: 0.6,
      confMax: 0.6,
      confStep: 0,
    });
    const candidates = generateCandidates(args);
    const orderTypes = new Set(candidates.map((c) => c.entryOrderType));
    const offsets = new Set(candidates.map((c) => c.entryLimitOffsetBps));
    expect(orderTypes.has("market")).toBe(true);
    expect(orderTypes.has("limit")).toBe(true);
    expect(offsets.has(0)).toBe(true);
    expect(offsets.has(5)).toBe(true);
    expect(offsets.has(10)).toBe(true);
  });
});

describe("optimizer selection", () => {
  it("objectiveValue returns total return by default", () => {
    const r = makeResult({ totalReturnPct: 12.3 });
    expect(objectiveValue(r, "return")).toBe(12.3);
  });

  it("objectiveValue returns Sharpe when requested", () => {
    const r = makeResult({ sharpeRatio: 1.7 });
    expect(objectiveValue(r, "sharpe")).toBe(1.7);
  });

  it("objectiveValue returns Calmar when requested", () => {
    const r = makeResult({
      metrics: { ...makeResult().metrics, calmarRatio: 2.5 },
    });
    expect(objectiveValue(r, "calmar")).toBe(2.5);
  });

  it("selectWinner picks the best OOS result when it meets minTrades", () => {
    const p = buildCandidate(true, [1, 2, 0.6, 1, 24, 0, 0, 0, 70, 30]);
    const results: OptimizeResult[] = [
      makeOptimizeResult(
        p,
        { totalReturnPct: 10 },
        { totalReturnPct: 5, totalTrades: 8 },
      ),
      makeOptimizeResult(
        p,
        { totalReturnPct: 5 },
        { totalReturnPct: 15, totalTrades: 8 },
      ),
    ];
    const winner = selectWinner(results, "return", 5);
    expect(winner).toBe(results[1]);
  });

  it("selectWinner returns null when OOS does not meet minOosTrades", () => {
    const p = buildCandidate(true, [1, 2, 0.6, 1, 24, 0, 0, 0, 70, 30]);
    const results: OptimizeResult[] = [
      makeOptimizeResult(
        p,
        { totalReturnPct: 20, totalTrades: 10 },
        { totalReturnPct: 100, totalTrades: 1 },
      ),
      makeOptimizeResult(
        p,
        { totalReturnPct: 5, totalTrades: 10 },
        { totalReturnPct: 6, totalTrades: 1 },
      ),
    ];
    const winner = selectWinner(results, "return", 5, 5);
    expect(winner).toBeNull();
  });

  it("selectWinner returns null when no result passes the trade floor", () => {
    const p = buildCandidate(true, [1, 2, 0.6, 1, 24, 0, 0, 0, 70, 30]);
    const results: OptimizeResult[] = [
      makeOptimizeResult(p, { totalReturnPct: 5, totalTrades: 1 }),
      makeOptimizeResult(p, { totalReturnPct: 10, totalTrades: 1 }),
    ];
    const winner = selectWinner(results, "return", 5);
    expect(winner).toBeNull();
  });

  it("selectWinner uses IS floor when OOS is disabled", () => {
    const p = buildCandidate(true, [1, 2, 0.6, 1, 24, 0, 0, 0, 70, 30]);
    const results: OptimizeResult[] = [
      makeOptimizeResult(p, { totalReturnPct: 5, totalTrades: 10 }),
      makeOptimizeResult(p, { totalReturnPct: 10, totalTrades: 10 }),
    ];
    const winner = selectWinner(results, "return", 5);
    expect(winner).toBe(results[1]);
  });
});

describe("buildStrategyProfileFromOptimizeResult", () => {
  it("writes winning ATR params to the per-symbol override", () => {
    const args = makeOptimizeArgs();
    const p = buildCandidate(true, [2, 4, 0.7, 1, 24, 6, 25, 0.3, 65, 35]);
    const winner = makeOptimizeResult(p, { totalReturnPct: 10 });
    const profile = buildStrategyProfileFromOptimizeResult(
      "test-profile",
      args,
      winner,
    );

    expect(profile.name).toBe("test-profile");
    expect(profile.defaults.exchange).toBe(args.exchange);
    expect(profile.defaults.useAtrStops).toBe(true);

    const override = profile.symbols[args.symbol];
    expect(override).toBeDefined();
    expect(override.minConfidence).toBe(0.7);
    expect(override.atrStopMultiplier).toBe(2);
    expect(override.atrTakeProfitMultiplier).toBe(4);
    expect(override.adxMin).toBe(25);
    expect(override.breakevenAtR).toBe(1);
    expect(override.maxBarsInTrade).toBe(24);
    expect(override.lossCooldownBars).toBe(6);
    expect(override.entryOrderType).toBe("market");
    expect(override.entryLimitOffsetBps).toBe(0);
  });

  it("writes winning fixed-pct params to the per-symbol override", () => {
    const args = makeOptimizeArgs({ noAtr: true });
    const p = buildCandidate(false, [1.5, 3, 0.6, 1, 24, 0, 0, 0, 70, 30]);
    const winner = makeOptimizeResult(p, { totalReturnPct: 10 });
    const profile = buildStrategyProfileFromOptimizeResult(
      "test-profile",
      args,
      winner,
    );

    const override = profile.symbols[args.symbol];
    expect(override.stopLossPct).toBe(1.5);
    expect(override.takeProfitPct).toBe(3);
    expect(override.atrStopMultiplier).toBe(0);
    expect(override.atrTakeProfitMultiplier).toBe(0);
  });

  it("persists winning entry order type and limit offset", () => {
    const args = makeOptimizeArgs({ entryOrderType: "limit" });
    const p: OptimizeCandidateParams = {
      ...buildCandidate(true, [1, 2, 0.6, 1, 24, 0, 0, 0, 70, 30]),
      entryOrderType: "limit",
      entryLimitOffsetBps: 5,
    };
    const winner = makeOptimizeResult(p, { totalReturnPct: 10 });
    const profile = buildStrategyProfileFromOptimizeResult(
      "test-profile",
      args,
      winner,
    );

    expect(profile.defaults.entryOrderType).toBe("limit");
    expect(profile.defaults.entryLimitOffsetBps).toBe(5);
    expect(profile.symbols[args.symbol].entryOrderType).toBe("limit");
    expect(profile.symbols[args.symbol].entryLimitOffsetBps).toBe(5);
  });
});

describe("walk-forward optimization helpers", () => {
  function makeCandles(count: number, startMs: number) {
    const candles = [];
    for (let i = 0; i < count; i++) {
      const t = startMs + i * 60 * 60 * 1000;
      candles.push({
        open: 100,
        high: 101,
        low: 99,
        close: 100,
        volume: 1,
        timestamp: new Date(t),
      });
    }
    return candles;
  }

  it("generateWalkForwardWindows creates sequential train/test slices", () => {
    const candles = makeCandles(24 * 400, 0); // 400 days hourly
    const windows = generateWalkForwardWindows(candles, 180, 60, 60);
    expect(windows.length).toBeGreaterThan(1);
    const first = windows[0];
    expect(first.trainCandles.length).toBeGreaterThanOrEqual(24 * 180 - 24); // hourly
    expect(first.testCandles.length).toBeGreaterThanOrEqual(24 * 60 - 24);
    for (const w of windows) {
      const trainEnd =
        w.trainCandles[w.trainCandles.length - 1].timestamp.getTime();
      const testStart = w.testCandles[0].timestamp.getTime();
      expect(testStart).toBeGreaterThanOrEqual(trainEnd);
    }
    for (let i = 1; i < windows.length; i++) {
      expect(
        windows[i].trainCandles[0].timestamp.getTime(),
      ).toBeGreaterThanOrEqual(
        windows[i - 1].trainCandles[0].timestamp.getTime(),
      );
    }
  });

  it("combineWalkForwardResults compounds window returns", () => {
    const r1 = makeResult({
      totalReturnPct: 10,
      totalTrades: 1,
      trades: [
        {
          id: "t1",
          symbol: "BTC/USDT",
          side: "long",
          entryTime: new Date(0),
          exitTime: new Date(1),
          entryPrice: 100,
          exitPrice: 110,
          pnl: 100,
          pnlPct: 1,
          netPnl: 100,
          exitReason: "take_profit",
          initialRiskPct: 0.01,
          fillType: "taker",
          entryFeePct: 0.1,
          exitFeePct: 0.1,
        },
      ],
    });
    const r2 = makeResult({
      totalReturnPct: 10,
      totalTrades: 1,
      trades: [
        {
          id: "t2",
          symbol: "BTC/USDT",
          side: "long",
          entryTime: new Date(2),
          exitTime: new Date(3),
          entryPrice: 100,
          exitPrice: 110,
          pnl: 100,
          pnlPct: 1,
          netPnl: 100,
          exitReason: "take_profit",
          initialRiskPct: 0.01,
          fillType: "taker",
          entryFeePct: 0.1,
          exitFeePct: 0.1,
        },
      ],
    });
    const combined = combineWalkForwardResults([r1, r2], 10000, "BTC/USDT");
    expect(combined.totalTrades).toBe(2);
    expect(combined.totalReturnPct).toBeGreaterThan(0);
    expect(combined.trades[0].netPnl).toBe(100);
    expect(combined.trades[1].netPnl).toBeGreaterThan(100);
  });
});

describe("preset command helpers", () => {
  it("extractExplicitOverrides keeps only values that differ from defaults", () => {
    const base = applyPreset("balanced");
    const args: import("../scalping/strategy-profile.js").ResolvedBacktestArgs =
      {
        ...base,
        symbol: "ETH/USDT",
        timeframe: "4h",
        fee: 0.05,
      };

    const overrides = extractExplicitOverrides(args);

    // Defaults equal to the built-in command defaults should be omitted.
    expect(overrides.exchange).toBeUndefined();
    expect(overrides.capital).toBeUndefined();

    // Explicit overrides should be kept.
    expect(overrides.symbol).toBe("ETH/USDT");
    expect(overrides.timeframe).toBe("4h");
    expect(overrides.fee).toBe(0.05);
  });

  it("extractExplicitOverrides treats observed-price=false as an explicit override", () => {
    const base = applyPreset("balanced");
    const args: import("../scalping/strategy-profile.js").ResolvedBacktestArgs =
      {
        ...base,
        observedPrice: false,
      };

    const overrides = extractExplicitOverrides(args);
    expect(overrides.observedPrice).toBe(false);
  });
});

describe("realistic mode", () => {
  it("presets default to realistic=true and observedPrice=false", () => {
    for (const name of ["conservative", "balanced", "aggressive"] as const) {
      const preset = applyPreset(name);
      expect(preset.realistic).toBe(true);
      expect(preset.observedPrice).toBe(false);
    }
  });

  it("extractExplicitOverrides includes --realistic=true", () => {
    const base = applyPreset("balanced");
    const args: ResolvedBacktestArgs = { ...base, realistic: false };
    const overrides = extractExplicitOverrides(args);
    expect(overrides.realistic).toBe(false);
  });

  it("buildStrategyProfileFromArgs persists realistic flag", () => {
    const base = applyPreset("balanced");
    const profile = buildStrategyProfileFromArgs("realistic-test", {
      ...base,
      realistic: true,
    });
    expect(profile.defaults.realistic).toBe(true);
  });
});

function makeUptrendCandles(count: number): CandleLike[] {
  const candles: CandleLike[] = [];
  let close = 100;
  for (let i = 0; i < count; i++) {
    const open = close;
    close *= 1.01;
    const high = close * 1.005;
    const low = open * 0.995;
    candles.push({
      open,
      high,
      low,
      close,
      volume: 1,
      timestamp: new Date(i * 3_600_000),
    });
  }
  return candles;
}

function makeSelectArgs(overrides: Partial<SelectArgs> = {}): SelectArgs {
  const base = applyPreset("balanced");
  return {
    ...base,
    universe: "",
    top: 0,
    minRobustness: 0,
    minReturnPct: 0,
    maxDrawdownPct: 100,
    minTrades: 0,
    selectLookbackCandles: 0,
    ...overrides,
  } as SelectArgs;
}

describe("select command helpers", () => {
  it("runSelectBacktest applies realistic slippage when realistic=true", () => {
    const candles = makeUptrendCandles(100);
    const args = makeSelectArgs({
      realistic: true,
      slippageBps: 0,
      fee: 0.1,
    });
    const withRealism = runSelectBacktest(
      "BTC/USDT",
      candles,
      args,
      "binance",
      {
        regimeMode: "trend",
        atrStopMultiplier: 2,
        atrTakeProfitMultiplier: 3,
        minConfidence: 0.5,
        adxMin: 0,
      },
    );

    const withoutRealism = runSelectBacktest(
      "BTC/USDT",
      candles,
      { ...args, realistic: false },
      "binance",
      {
        regimeMode: "trend",
        atrStopMultiplier: 2,
        atrTakeProfitMultiplier: 3,
        minConfidence: 0.5,
        adxMin: 0,
      },
    );

    expect(withRealism.totalTrades).toBeGreaterThan(0);
    expect(withoutRealism.totalTrades).toBeGreaterThan(0);
    expect(withRealism.totalReturnPct).not.toBe(withoutRealism.totalReturnPct);
  });

  it("selectBestForSymbol returns the best passing grid config", () => {
    const candles = makeUptrendCandles(100);
    const args = makeSelectArgs({
      realistic: false,
      slippageBps: 0,
      fee: 0,
      minTrades: 1,
      minReturnPct: -100,
    });
    const best = selectBestForSymbol("BTC/USDT", candles, args, "binance");

    expect(best).not.toBeNull();
    expect(best!.symbol).toBe("BTC/USDT");
    expect(best!.result.totalTrades).toBeGreaterThanOrEqual(1);
    expect(["trend", "reversion"]).toContain(best!.params.regimeMode);
    expect([1.5, 2.0]).toContain(best!.params.atrStopMultiplier);
    expect([2.0, 2.5]).toContain(best!.params.atrTakeProfitMultiplier);
    expect([0.4, 0.5]).toContain(best!.params.minConfidence);
    expect([0, 20]).toContain(best!.params.adxMin);
  });
});

describe("validate command", () => {
  function makeWatchlistEntry(
    overrides: Partial<SelectWatchlistEntry> = {},
  ): SelectWatchlistEntry {
    const base = {
      symbol: "BTC/USDT",
      timeframe: "1h",
      profile: {
        regimeMode: "trend" as const,
        atrStopMultiplier: 2,
        atrTakeProfitMultiplier: 3,
        minConfidence: 0.5,
        adxMin: 0,
      },
    };
    return {
      ...base,
      ...overrides,
      profile: overrides.profile
        ? { ...base.profile, ...overrides.profile }
        : base.profile,
    };
  }

  it("fails gracefully when watchlist does not exist", async () => {
    const result = await Effect.runPromise(
      validateWatchlist({ watchlist: "missing", exchange: "binance" }).pipe(
        Effect.catch((err) => Effect.succeed(err)),
        Effect.provide(PathLive(process.env.NEURATRADE_HOME)),
      ),
    );

    const reason = "reason" in result ? String(result.reason) : String(result);
    expect(reason).toContain("Failed to load watchlist");
  });

  it("loads a saved watchlist fixture", async () => {
    const homeDir = process.env.NEURATRADE_HOME!;
    const watchlistDir = path.join(homeDir, "watchlists");
    fs.mkdirSync(watchlistDir, { recursive: true });
    const watchlistPath = path.join(watchlistDir, "test.json");
    const entries = [makeWatchlistEntry()];
    fs.writeFileSync(watchlistPath, JSON.stringify(entries, null, 2));

    const loaded = await Effect.runPromise(
      loadSelectWatchlist(watchlistPath).pipe(
        Effect.catch((err) => Effect.fail(err)),
      ),
    );

    expect(loaded).toHaveLength(1);
    expect(loaded[0].symbol).toBe("BTC/USDT");
    expect(loaded[0].profile.atrStopMultiplier).toBe(2);
  });

  it("builds validation backtest args from watchlist entry", () => {
    const entry = makeWatchlistEntry({
      symbol: "ETH/USDT",
      timeframe: "4h",
      profile: {
        regimeMode: "reversion",
        atrStopMultiplier: 1.5,
        atrTakeProfitMultiplier: 3,
        minConfidence: 0.5,
        adxMin: 0,
      },
    });
    const args = buildValidateBacktestArgs(entry, "binance");

    expect(args.symbol).toBe("ETH/USDT");
    expect(args.timeframe).toBe("4h");
    expect(args.exchange).toBe("binance");
    expect(args.useAtrStops).toBe(true);
    expect(args.atrStopMultiplier).toBe(1.5);
    expect(args.atrTakeProfitMultiplier).toBe(3);
    expect(args.minConfidence).toBe(0.5);
    expect(args.regimeMode).toBe("reversion");
    expect(args.adxMin).toBe(0);
    expect(args.oosPct).toBe(20);
    expect(args.mcIterations).toBe(200);
    expect(args.realistic).toBe(true);
  });

  it("marks rows live-ready only when all gates pass", () => {
    const base: ValidationRow = {
      symbol: "BTC/USDT",
      regimeMode: "trend",
      isReturnPct: 10,
      oosReturnPct: 5,
      oosMaxDrawdownPct: 10,
      mcP95DrawdownPct: 15,
      mcRuinPct: 2,
      robustnessScore: 20,
      isTrades: 15,
      oosTrades: 12,
      liveReady: false,
      entry: makeWatchlistEntry(),
    };

    expect(isLiveReady(base)).toBe(true);
    expect(isLiveReady({ ...base, oosReturnPct: -1 })).toBe(false);
    expect(isLiveReady({ ...base, oosMaxDrawdownPct: 16 })).toBe(false);
    expect(isLiveReady({ ...base, mcP95DrawdownPct: 21 })).toBe(false);
    expect(isLiveReady({ ...base, mcRuinPct: 6 })).toBe(false);
    expect(isLiveReady({ ...base, isTrades: 9 })).toBe(false);
    expect(isLiveReady({ ...base, oosTrades: 9 })).toBe(false);
  });

  it("validates a saved watchlist against stored candles", async () => {
    const homeDir = process.env.NEURATRADE_HOME!;
    const dataDir = path.join(homeDir, "data");
    fs.mkdirSync(dataDir, { recursive: true });
    const db = new Database(path.join(dataDir, "neuratrade.db"));
    db.exec("PRAGMA foreign_keys = ON;");

    const repo = new MarketDataRepositorySQLite(db);
    await Effect.runPromise(repo.ensureTables());

    const candles = makeUptrendCandles(300).map((c, i) => ({
      ...c,
      exchange: "binance",
      symbol: "BTC/USDT",
      timeframe: "1h",
      volume: 1,
      timestamp: new Date(Date.UTC(2026, 0, 1, i)),
    }));
    await Effect.runPromise(repo.saveCandles(candles));
    db.close();

    const watchlistDir = path.join(homeDir, "watchlists");
    fs.mkdirSync(watchlistDir, { recursive: true });
    const entries: SelectWatchlistEntry[] = [
      {
        symbol: "BTC/USDT",
        timeframe: "1h",
        profile: {
          regimeMode: "trend",
          atrStopMultiplier: 2,
          atrTakeProfitMultiplier: 3,
          minConfidence: 0.4,
          adxMin: 0,
        },
      },
    ];
    fs.writeFileSync(
      path.join(watchlistDir, "candidates.json"),
      JSON.stringify(entries, null, 2),
    );

    const rows = await Effect.runPromise(
      validateWatchlist({ watchlist: "candidates", exchange: "binance" }).pipe(
        Effect.provide(PathLive(homeDir)),
      ),
    );

    expect(rows).toHaveLength(1);
    expect(rows[0].symbol).toBe("BTC/USDT");
    expect(rows[0].mcP95DrawdownPct).toBeGreaterThanOrEqual(0);
    expect(rows[0].mcRuinPct).toBeGreaterThanOrEqual(0);
    expect(rows[0].isTrades).toBeGreaterThanOrEqual(0);
    expect(rows[0].oosTrades).toBeGreaterThanOrEqual(0);
  });
});
describe("library command", () => {
  it("prints the strategy catalog for 'scalp library --list'", async () => {
    const run = Command.run(libraryCommand, { name: "test", version: "0.0.0" });
    const result = await Effect.runPromise(
      run(["bun", "test", "--list"]).pipe(
        Effect.provide(
          Layer.mergeAll(
            BunServices.layer,
            PathLive(process.env.NEURATRADE_HOME),
          ),
        ),
      ),
    );
    expect(Array.isArray(result)).toBe(true);
  });

  it("'scalp library gridScalp --help' lists grid options", async () => {
    const proc = Bun.spawn([
      "bun",
      "run",
      "index.ts",
      "scalp",
      "library",
      "gridScalp",
      "--help",
    ]);
    const output = await new Response(proc.stdout).text();
    expect(output).toContain("--grid-step-pct");
    expect(output).toContain("--grid-max-grids");
    expect(output).toContain("--grid-pause-after-loss-bars");
    expect(output).toContain("--realistic");
  }, 15_000);
});

describe("walk-forward command", () => {
  it("runs walk-forward optimization on stored candles", async () => {
    const homeDir = process.env.NEURATRADE_HOME!;
    const dataDir = path.join(homeDir, "data");
    fs.mkdirSync(dataDir, { recursive: true });
    const db = new Database(path.join(dataDir, "neuratrade.db"));
    db.exec("PRAGMA foreign_keys = ON;");

    const repo = new MarketDataRepositorySQLite(db);
    await Effect.runPromise(repo.ensureTables());

    const candles = makeUptrendCandles(300).map((c, i) => ({
      ...c,
      exchange: "binance",
      symbol: "BTC/USDT",
      timeframe: "4h",
      volume: 1,
      timestamp: new Date(Date.UTC(2026, 0, 1, i * 4)),
    }));
    await Effect.runPromise(repo.saveCandles(candles));
    db.close();

    const run = Command.run(walkForwardCommand, {
      name: "test",
      version: "0.0.0",
    });
    const result = await Effect.runPromise(
      run([
        "bun",
        "test",
        "--symbol",
        "BTC/USDT",
        "--timeframe",
        "4h",
        "--exchange",
        "binance",
        "--realistic",
        "--train-window",
        "120",
        "--test-window",
        "60",
        "--min-trades",
        "1",
      ]).pipe(
        Effect.provide(
          Layer.mergeAll(
            BunServices.layer,
            PathLive(process.env.NEURATRADE_HOME),
          ),
        ),
      ),
    );

    expect(result).toBeDefined();
    expect(
      (result as unknown as { windows: unknown[] }).windows.length,
    ).toBeGreaterThan(0);
  });
});

describe("paper-trade command", () => {
  it("buildPaperTradeComposerConfig returns a config matching the dualEmaCross template", () => {
    const config = buildPaperTradeComposerConfig({
      strategy: "dualEmaCross",
      priceOnly: false,
      noRsi: false,
      noTrend: false,
      regimeMode: "trend",
      volumeMinRatio: 0,
      volumeLookback: 20,
      minConfluence: 0,
      entryCandleConfirm: false,
      momentumConfirmBars: 0,
      breakoutLookback: 20,
      breakoutVolumeMinRatio: 1.2,
      breakoutAdxMin: 20,
      useFunding: false,
      fundingBiasThreshold: 0.0001,
      rsiPeriod: 14,
      rsiOversoldStrong: 30,
      rsiOverboughtStrong: 70,
      trendFilterPeriod: 200,
      entryRsiLongThreshold: 10,
      entryRsiShortThreshold: 90,
      exitRsiLongLevel: 60,
      exitRsiShortLevel: 40,
    });

    expect(config.thresholds.regimeMode).toBe("trend");
    expect(config.thresholds.trendSignalStyle).toBe("cross");
    expect(config.thresholds.trendFastPeriod).toBe(50);
    expect(config.thresholds.trendSlowPeriod).toBe(200);
    expect(config.weights.trend).toBeGreaterThan(config.weights.volatility);
    expect(config.weights.trend).toBeGreaterThan(config.weights.regime);
    expect(config.weights.trend).toBeGreaterThan(config.weights.rsi);
  });

  it("paper-trade --strategy dualEmaCross --help lists the strategy option", async () => {
    const proc = Bun.spawn([
      "bun",
      "run",
      "index.ts",
      "scalp",
      "paper-trade",
      "--help",
      "--strategy",
      "dualEmaCross",
    ]);
    const output = await new Response(proc.stdout).text();
    expect(output).toContain("--strategy");
    expect(output).toContain("dualEmaCross");
    expect(output).toContain("--realistic");
  }, 15_000);
});

describe("backtestProgram fill-model option forwarding", () => {
  it("forwards makerFeePct/entryOrderType/entryLimitOffsetBps/entryOnClose to runBacktest", async () => {
    const db = new Database(":memory:");
    const candles: Candle[] = Array.from({ length: 150 }, (_, i) => {
      const close = 100 + i * 0.5;
      return {
        exchange: "binance",
        symbol: "BTC/USDT",
        timeframe: "1h",
        open: close - 0.2,
        high: close + 0.5,
        low: close - 0.5,
        close,
        volume: 10,
        timestamp: new Date(Date.now() + i * 3600_000),
      };
    });
    await Effect.runPromise(
      Effect.gen(function* () {
        const repo = yield* MarketDataRepository;
        yield* repo.ensureTables();
        yield* repo.saveCandles(candles);
      }).pipe(Effect.provide(MarketDataRepositorySQLiteLive(db))),
    );

    let captured: {
      makerFeePct: number | undefined;
      entryOrderType: string | undefined;
      entryLimitOffsetBps: number | undefined;
      entryOnClose: boolean | undefined;
    } | null = null;
    const fakeEngine = Layer.succeed(BacktestEngine, {
      runBacktest: (options) => {
        captured = {
          makerFeePct: options.makerFeePct,
          entryOrderType: options.entryOrderType,
          entryLimitOffsetBps: options.entryLimitOffsetBps,
          entryOnClose: options.entryOnClose,
        };
        return Effect.succeed(makeResult());
      },
      runGridBacktest: () =>
        Effect.succeed({
          totalReturnPct: 0,
          maxDrawdownPct: 0,
          winRate: 0,
          totalTrades: 0,
          profitFactor: 0,
          trades: [],
        }),
    });

    const args = makeOptimizeArgs({
      makerFeePct: 0.04,
      entryOrderType: "limit",
      entryLimitOffsetBps: 25,
      entryOnClose: true,
    });

    await Effect.runPromise(
      backtestProgram(
        args as unknown as Parameters<typeof backtestProgram>[0],
      ).pipe(
        Effect.provide(
          Layer.mergeAll(
            MarketDataRepositorySQLiteLive(db),
            fakeEngine,
            BunServices.layer,
            PathLive("/tmp"),
          ),
        ),
      ),
    );

    expect(captured!).toEqual({
      makerFeePct: 0.04,
      entryOrderType: "limit",
      entryLimitOffsetBps: 25,
      entryOnClose: true,
    });
    db.close();
  });
});

describe("backtestProgram funding-rate wiring", () => {
  const makeCandles = (): Candle[] =>
    Array.from({ length: 150 }, (_, i) => {
      const close = 100 + i * 0.5;
      return {
        exchange: "binance",
        symbol: "BTC/USDT",
        timeframe: "1h",
        open: close - 0.2,
        high: close + 0.5,
        low: close - 0.5,
        close,
        volume: 10,
        timestamp: new Date(Date.now() + i * 3600_000),
      };
    });

  function makeEngine(captured: {
    fundingRates?: unknown;
    composerConfig?: unknown;
  }): Layer.Layer<BacktestEngine> {
    return Layer.succeed(BacktestEngine, {
      runBacktest: (options) => {
        captured.fundingRates = options.fundingRates;
        captured.composerConfig = options.composerConfig;
        return Effect.succeed(makeResult());
      },
      runGridBacktest: () =>
        Effect.succeed({
          totalReturnPct: 0,
          maxDrawdownPct: 0,
          winRate: 0,
          totalTrades: 0,
          profitFactor: 0,
          trades: [],
        }),
    });
  }

  it("forwards funding rates fetched from the repo to runBacktest", async () => {
    const db = new Database(":memory:");
    const candles = makeCandles();
    const fundingRates = candles
      .filter((_, i) => i % 10 === 0)
      .map((c) => ({
        exchange: "binance",
        symbol: "BTC/USDT",
        fundingRate: 0.0003,
        timestamp: c.timestamp,
      }));
    await Effect.runPromise(
      Effect.gen(function* () {
        const repo = yield* MarketDataRepository;
        yield* repo.ensureTables();
        yield* repo.saveCandles(candles);
        yield* repo.saveFundingRates(
          "binance",
          "BTC/USDT",
          fundingRates,
        );
      }).pipe(Effect.provide(MarketDataRepositorySQLiteLive(db))),
    );

    const captured: { fundingRates: unknown } = { fundingRates: undefined };
    await Effect.runPromise(
      backtestProgram(
        makeOptimizeArgs() as unknown as Parameters<typeof backtestProgram>[0],
      ).pipe(
        Effect.provide(
          Layer.mergeAll(
            MarketDataRepositorySQLiteLive(db),
            makeEngine(captured),
            BunServices.layer,
            PathLive("/tmp"),
          ),
        ),
      ),
    );

    expect(captured.fundingRates).toEqual(fundingRates);
    db.close();
  });

  it("passes an empty funding array when no funding rows exist", async () => {
    const db = new Database(":memory:");
    await Effect.runPromise(
      Effect.gen(function* () {
        const repo = yield* MarketDataRepository;
        yield* repo.ensureTables();
        yield* repo.saveCandles(makeCandles());
      }).pipe(Effect.provide(MarketDataRepositorySQLiteLive(db))),
    );

    const captured: { fundingRates: unknown } = { fundingRates: undefined };
    await Effect.runPromise(
      backtestProgram(
        makeOptimizeArgs() as unknown as Parameters<typeof backtestProgram>[0],
      ).pipe(
        Effect.provide(
          Layer.mergeAll(
            MarketDataRepositorySQLiteLive(db),
            makeEngine(captured),
            BunServices.layer,
            PathLive("/tmp"),
          ),
        ),
      ),
    );

    expect(captured.fundingRates).toEqual([]);
    db.close();
  });

  it("wires --use-funding and --funding-bias-threshold into the composer config", async () => {
    const db = new Database(":memory:");
    await Effect.runPromise(
      Effect.gen(function* () {
        const repo = yield* MarketDataRepository;
        yield* repo.ensureTables();
        yield* repo.saveCandles(makeCandles());
      }).pipe(Effect.provide(MarketDataRepositorySQLiteLive(db))),
    );

    const captured: { fundingRates?: unknown; composerConfig?: unknown } = {};
    await Effect.runPromise(
      backtestProgram(
        makeOptimizeArgs({
          useFunding: true,
          fundingBiasThreshold: 0.00005,
        }) as unknown as Parameters<typeof backtestProgram>[0],
      ).pipe(
        Effect.provide(
          Layer.mergeAll(
            MarketDataRepositorySQLiteLive(db),
            makeEngine(captured),
            BunServices.layer,
            PathLive("/tmp"),
          ),
        ),
      ),
    );

    const config = captured.composerConfig as ComposerConfig;
    expect(config.thresholds.useFunding).toBe(true);
    expect(config.thresholds.fundingBiasThreshold).toBe(0.00005);
    db.close();
  });
});

describe("buildBacktestComposerConfig funding wiring", () => {
  it("threads non-default funding threshold and useFunding into thresholds", () => {
    const config = buildBacktestComposerConfig(
      false,
      false,
      false,
      "trend",
      0,
      20,
      0,
      false,
      0,
      0,
      20,
      0.00005,
      true,
    );
    expect(config.thresholds.fundingBiasThreshold).toBe(0.00005);
    expect(config.thresholds.useFunding).toBe(true);
  });

  it("keeps the default funding threshold and useFunding when not overridden", () => {
    const config = buildBacktestComposerConfig(false, false, false);
    expect(config.thresholds.fundingBiasThreshold).toBe(0.0001);
    expect(config.thresholds.useFunding).toBe(false);
  });
});

describe("buildBacktestComposerConfig breakout lookback", () => {
  it("threads a non-default breakoutLookback into thresholds", () => {
    const config = buildBacktestComposerConfig(
      false,
      false,
      false,
      "trend",
      0,
      20,
      0,
      false,
      0,
      0,
      55,
    );
    expect(config.thresholds.breakoutLookback).toBe(55);
  });

  it("keeps the default breakoutLookback when not overridden", () => {
    const config = buildBacktestComposerConfig(false, false, false);
    expect(config.thresholds.breakoutLookback).toBe(20);
  });
});

describe("probeNamesProbedSymbol", () => {
  const apiErr = (code: string, body: string) =>
    new (class extends Error {
      readonly status = 400;
      readonly body = body;
      readonly code = code;
      readonly endpoint = "/api/v2/mix/account/account";
    })() as never;

  it("drops a 40034 that names the probed symbol as the parameter value", () => {
    expect(
      probeNamesProbedSymbol(
        apiErr(
          "40034",
          '{"code":"40034","msg":"Parameter BLESSUSDT does not exist"}',
        ),
        "BLESS/USDT",
      ),
    ).toBe(true);
    expect(
      probeNamesProbedSymbol(
        apiErr(
          "40034",
          '{"code":"40034","msg":"Parameter SOLUSDT does not exist"}',
        ),
        "SOL/USDT:USDT",
      ),
    ).toBe(true);
    expect(
      probeNamesProbedSymbol(
        apiErr(
          "40034",
          '{"code":"40034","msg":"Parameter BLESSUSDT not exist"}',
        ),
        "BLESS/USDT",
      ),
    ).toBe(true);
  });

  it("fails closed on named config parameters and unrelated tokens", () => {
    expect(
      probeNamesProbedSymbol(
        apiErr(
          "40034",
          '{"code":"40034","msg":"Parameter marginCoin does not exist"}',
        ),
        "BLESS/USDT",
      ),
    ).toBe(false);
    expect(
      probeNamesProbedSymbol(
        apiErr(
          "40034",
          '{"code":"40034","msg":"Parameter clientType does not exist"}',
        ),
        "BLESS/USDT",
      ),
    ).toBe(false);
    expect(
      probeNamesProbedSymbol(
        apiErr(
          "40034",
          '{"code":"40034","msg":"Parameter FOOUSDT does not exist"}',
        ),
        "BLESS/USDT",
      ),
    ).toBe(false);
  });

  it("ignores non-40034 codes", () => {
    expect(
      probeNamesProbedSymbol(
        apiErr(
          "40774",
          '{"code":"40774","msg":"Parameter BLESSUSDT does not exist"}',
        ),
        "BLESS/USDT",
      ),
    ).toBe(false);
  });
});
