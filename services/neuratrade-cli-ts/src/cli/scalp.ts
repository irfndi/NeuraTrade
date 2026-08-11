import { Command, Options } from "./kit/kit.ts";
import { BunServices } from "@effect/platform-bun";
import {
  Console,
  Duration,
  Effect,
  FileSystem,
  Layer,
  Option,
  Schedule,
} from "effect";
import { dirname, isAbsolute, resolve } from "node:path";
import { Path, PathLive } from "../services/path.js";
import { ConfigLive } from "../services/config.js";
import {
  SqliteClient,
  SqliteClientLiveRaw,
  type SqliteError,
} from "../services/sqlite.js";
import {
  MarketDataRepository,
  MarketDataRepositoryError,
  MarketDataRepositorySQLite,
  MarketDataRepositorySQLiteLive,
} from "../market-data/repository.js";
import { defaultComposerConfig } from "../scalping/composer.js";
import type { CandleLike, ComposerConfig } from "../scalping/types.js";
import {
  attachMonteCarlo,
  runBacktest,
  splitCandlesByOos,
  type BacktestResult,
  type BacktestTrade,
} from "../scalping/backtest.js";
import type { GridResult, GridTrade } from "../scalping/grid.js";
import { computePerformanceMetrics } from "../scalping/performance-metrics.js";
import {
  BacktestEngine,
  BacktestEngineLive,
  ExitEngineLive,
  SignalComposerLive,
  StrategyLibrary,
  StrategyLibraryLive,
} from "../scalping/services.js";
import { MarketDataGatewayLive } from "../market-data/gateways/index.js";
import { MarketDataGateway } from "../market-data/gateway.js";
import { MarketDataGatewayRepositoryLive } from "../market-data/gateway-repository.js";
import type { FundingRate } from "../market-data/types.js";
import { SimulatedExchangeAdapterLive } from "../exchange/adapters/simulated.js";
import { BinanceLiveExchangeAdapterLive } from "../exchange/adapters/binance-live.js";
import { SimulatedFuturesExchangeAdapterLive } from "../exchange/adapters/simulated-futures.js";
import { BitgetFuturesExchangeAdapterLive } from "../exchange/adapters/bitget-futures.js";
import {
  BybitFuturesExchangeAdapterLive,
  BybitClientLiveConfig,
} from "../exchange/adapters/bybit-futures.js";
import {
  BitgetClientLiveConfig,
  BitgetClient,
  BitgetApiError,
  isBitgetUnsupportedInstrumentError,
  toBitgetFuturesSymbol,
  type BitgetContract,
} from "../services/bitget-client.js";
import { BitgetConfig, BitgetConfigLive } from "../services/bitget-config.js";
import { BybitConfigLive } from "../services/bybit-config.js";
import { RateLimiterLive } from "../services/rate-limiter.js";
import type { FuturesMarginMode } from "../exchange/futures-adapter.js";
import {
  FuturesExchangeAdapter,
  type FuturesExchangeAdapterService,
} from "../exchange/futures-adapter.js";
import type { MarketDataGatewayService } from "../market-data/gateway.js";
import { RiskGuard, RiskGuardLive } from "../risk/guards.js";
import { KillSwitch, KillSwitchSQLiteLive } from "../risk/kill-switch.js";
import {
  CircuitBreaker,
  CircuitBreakerSQLiteLive,
} from "../risk/circuit-breaker.js";
import { Decimal, money, toNumber } from "../utils/money.js";
import {
  runPaperTradingIteration,
  type PaperTradingOptions,
} from "../paper-trading/engine.js";
import {
  runFuturesPaperTradingIteration,
  type FuturesPaperTradingOptions,
} from "../paper-trading/futures-engine.js";
import {
  runGridPaperTradingIteration,
  type GridPaperTradingOptions,
  type GridPaperTradingIterationResult,
} from "../paper-trading/grid-engine.js";
import {
  freshFlowTradeState,
  iterateFlowTrade,
  type FlowTradeError,
  type FlowTradeIterationResult,
  type FlowTradeOptions,
} from "../scalping/flow-trade.js";
import type { ContractSizeSpec } from "../paper-trading/types.js";
import type { BitgetProductType } from "../services/bitget-client.js";
import {
  PaperTradingRepository,
  PaperTradingRepositorySQLite,
  PaperTradingRepositorySQLiteLive,
  type WatchlistEntry as DbWatchlistEntry,
} from "../paper-trading/repository.js";
import {
  runSoak,
  type SoakOptions,
  type SoakSymbol,
  type IterationResult,
} from "../scalping/soak.js";
import {
  buildStrategyProfileFromArgs,
  findSymbolOverride,
  loadStrategyProfile,
  resolveBacktestArgs,
  saveStrategyProfile,
  type ResolvedBacktestArgs,
  type StrategyProfile,
  type StrategyProfileParams,
} from "../scalping/strategy-profile.js";
import {
  evaluateReadiness,
  formatReadinessReport,
} from "../scalping/readiness.js";
import {
  READINESS_COHORT_CANDIDATES,
  VALIDATED_BTC_GRID_CANDIDATE,
  candidateForSymbol,
} from "../scalping/grid-candidate.js";
import {
  runGridUniverseScan,
  runMarketUniverseScan,
  type GridUniverseEntry,
  type GridUniverseOptions,
  DEFAULT_GRID_UNIVERSE_SEARCH_SPACE,
  DEFAULT_PER_SYMBOL_FILL_CAP,
  accountScaledTargetFillsPerDay,
  accountSymbolCap,
  selectUniversePortfolio,
} from "../scalping/grid-universe.js";
import { applyPreset } from "../scalping/presets.js";
import {
  buildBacktestArgsFromTemplate,
  buildComposerConfigFromTemplate,
  type StrategyTemplateName,
} from "../scalping/strategy-library.js";
import { runWalkForward } from "../scalping/walk-forward.js";
import {
  runFlowRecorder,
  resolveFlowSymbols,
  type FlowRecorderRepository,
} from "../scalping/flow-recorder.js";
import {
  runFlowBacktest,
  defaultFlowBacktestOptions,
  type FlowBacktestData,
  type FlowBacktestOptions,
  type FlowBacktestReport,
  type FlowSymbolSeries,
} from "../scalping/flow-backtest.js";
import {
  selectFlowUniverse,
  type FlowInstrument,
  type FlowUniverseEntry,
} from "../scalping/flow-universe.js";
import {
  fetch24hrVolumes,
  fetchInstruments,
} from "../market-data/gateways/bybit.js";
import { MarketDataError } from "../market-data/gateway.js";
import { makeDemoReadinessCommand } from "./demo-readiness.js";
import { makeParityReplayCommand } from "./parity-replay.js";
import {
  exchangeOption,
  symbolOption,
  timeframeOption,
  capitalOption,
  positionSizeOption,
  riskPerTradeOption,
  riskBasedMaxPositionSizeOption,
  stopLossOption,
  takeProfitOption,
  feeOption,
  futuresOption,
  leverageOption,
  fundingRateOption,
  slippageBpsOption,
  trailingStopPctOption,
  trailingStopAtrMultOption,
  minAtrPctOption,
  adxMinOption,
  volumeMinRatioOption,
  volumeLookbackOption,
  minConfluenceOption,
  entryCandleConfirmOption,
  signalPersistenceOption,
  momentumConfirmBarsOption,
  makerFeeOption,
  entryOrderTypeOption,
  entryLimitOffsetBpsOption,
  rsiPeriodOption,
  rsiOversoldStrongOption,
  rsiOverboughtStrongOption,
  trendFilterPeriodOption,
  entryRsiLongThresholdOption,
  entryRsiShortThresholdOption,
  exitRsiPeriodOption,
  exitRsiLongLevelOption,
  exitRsiShortLevelOption,
  observedPriceOption,
  realisticOption,
  strictRealismOption,
  realisticSlippageBpsOption,
  trendSignalStyleOption,
  trendFastPeriodOption,
  trendSlowPeriodOption,
  directionalOnlyOption,
  rsiFollowTrendOption,
  strictAgreementOption,
  entryOnCloseOption,
  breakoutLookbackOption,
  breakoutVolumeMinRatioOption,
  breakoutAdxMinOption,
  fundingBiasThresholdOption,
  useFundingOption,
  strategyTypeOption,
  gridStepPctOption,
  gridMaxGridsOption,
  gridPauseAfterLossBarsOption,
  onlyWithTrendOption,
  targetRatioOption,
  chopGateAdxOption,
  volatilityTargetAnnualPctOption,
  noAtrOption,
  scanEntryOrdersOption,
  randomSearchOption,
  walkForwardOption,
  wfTrainDaysOption,
  wfTestDaysOption,
  wfStepDaysOption,
  minTradesOption,
  minOosTradesOption,
  selectByOption,
  stopLossMinOption,
  stopLossMaxOption,
  stopLossStepOption,
  takeProfitMinOption,
  takeProfitMaxOption,
  takeProfitStepOption,
  breakevenAtRMinOption,
  breakevenAtRMaxOption,
  breakevenAtRStepOption,
  maxBarsInTradeMinOption,
  maxBarsInTradeMaxOption,
  maxBarsInTradeStepOption,
  lossCooldownBarsMinOption,
  lossCooldownBarsMaxOption,
  lossCooldownBarsStepOption,
  adxMinMinOption,
  adxMinMaxOption,
  adxMinStepOption,
  minEfficiencyRatioMinOption,
  minEfficiencyRatioMaxOption,
  minEfficiencyRatioStepOption,
  rsiLongMaxMinOption,
  rsiLongMaxMaxOption,
  rsiLongMaxStepOption,
  rsiShortMinMinOption,
  rsiShortMinMaxOption,
  rsiShortMinStepOption,
  strategyOption,
  lossConfidencePenaltyOption,
  lossConfidenceDecayOption,
  htfTimeframeOption,
  htfTrendFastPeriodOption,
  htfTrendSlowPeriodOption,
  htfSignalConfidenceOption,
  entryPullbackEmaPeriodOption,
  entryPullbackMarginPctOption,
  minEfficiencyRatioOption,
  efficiencyRatioPeriodOption,
  rsiLongMaxOption,
  rsiShortMinOption,
  bollingerLongMaxPctBOption,
  bollingerShortMinPctBOption,
  profileOption,
  recordEquityCurveOption,
  exportTradesOption,
  oosPctOption,
  mcIterationsOption,
  breakevenAtROption,
  maxBarsInTradeOption,
  lossCooldownBarsOption,
  sessionStartOption,
  sessionEndOption,
  autoRegimeFilterOption,
  autoRegimeAdxThresholdOption,
  confidenceOption,
  useAtrStopsOption,
  atrStopMultiplierOption,
  atrTakeProfitMultiplierOption,
  atrRiskRewardOption,
  scaleOutAtROption,
  scaleOutPctOption,
  volatilityLookbackOption,
  volatilityLowPctOption,
  volatilityHighPctOption,
  volatilityLowFactorOption,
  volatilityHighFactorOption,
  priceOnlyOption,
  noRsiOption,
  holdUntilStopOption,
  noTrendOption,
  regimeModeOption,
  atrStopMinOption,
  atrStopMaxOption,
  atrStopStepOption,
  atrTpMinOption,
  atrTpMaxOption,
  atrTpStepOption,
  confMinOption,
  confMaxOption,
  confStepOption,
  minCandlesOption,
  topOption,
  optimizeScanOption,
  minReturnOption,
  minSharpeOption,
  scanMaxDrawdownOption,
  saveWatchlistOption,
  intervalOption,
  iterationsOption,
  replayBarsOption,
  liveOption,
  apiKeyOption,
  apiSecretOption,
  marginModeOption,
  productTypeOption,
  maxDrawdownOption,
  maxDailyLossOption,
  maxPositionSizeOption,
  maxTradesPerDayOption,
  minCapitalOption,
  watchlistOption,
  noWatchlistOption,
  killSwitchOption,
  disengageOption,
  soakWatchlistOption,
  profileNameOption,
  trainWindowOption,
  testWindowOption,
  wfMinTradesOption,
  minTradesPerMonthOption,
  gridUniverseExchangeOption,
  gridUniverseTimeframeOption,
  gridUniverseMinCandlesOption,
  gridUniverseTrainWindowOption,
  gridUniverseTestWindowOption,
  gridUniverseMinProfitableWindowsOption,
  gridUniverseMinAggregateReturnOption,
  gridUniverseFeeOption,
  gridUniverseSlippageOption,
  gridUniverseTrendFilterOption,
  gridUniverseMarketOption,
  gridUniverseOutputOption,
  gridUniverseWatchOption,
  gridUniverseIntervalOption,
  gridUniverseMinFillFrequencyOption,
  gridUniverseTargetFillsPerDayOption,
  gridUniverseAccountCapitalOption,
  gridUniverseTierOption,
  gridUniverseDataSourceOption,
  watchlistListExchangeOption,
  watchlistListTimeframeOption,
  flowSymbolsOption,
  flowStartOption,
  flowEndOption,
  flowTimeframeOption,
  flowThresholdOption,
  flowHoldTimesOption,
  flowFeeOption,
  flowSpreadBpsOption,
  flowConservativeFillRateOption,
  flowMaxBreakevenWinRateOption,
  flowLimitOption,
  flowMinTurnoverOption,
  flowUniverseDataSourceOption,
  flowTradeExchangeOption,
  flowTradeSymbolOption,
  flowHoldMinutesOption,
} from "./scalp-options.js";

function makeLayer(home?: string) {
  return Layer.mergeAll(
    BunServices.layer,
    PathLive(home),
    BacktestEngineLive,
    SignalComposerLive,
    ExitEngineLive,
    StrategyLibraryLive,
  );
}

/**
 * Layer for commands that read the market-data SQLite database. On top of
 * `makeLayer` it provides the runtime config and the scoped `SqliteClient`
 * (opens the DB via Effect, closes it when the command's scope ends). Uses
 * the raw open mode — repositories own their schema via ensureTables.
 */
function makeDbLayer(home?: string) {
  const base = makeLayer(home);
  const config = Layer.provide(ConfigLive(home), base);
  return Layer.mergeAll(
    base,
    config,
    Layer.provide(SqliteClientLiveRaw, Layer.merge(base, config)),
  );
}

function loadProfileIfNeeded(
  homeDir: string,
  profileName: string,
): Effect.Effect<Option.Option<StrategyProfile>, Error> {
  if (!profileName || profileName.trim().length === 0) {
    return Effect.succeed(Option.none());
  }
  return loadStrategyProfile(homeDir, profileName).pipe(
    Effect.map((p) => Option.some(p)),
  );
}

function formatNumber(value: number, digits: number): string {
  if (!Number.isFinite(value)) return String(value);
  return value.toFixed(digits);
}

const backtestOptions = {
  exchange: exchangeOption,
  symbol: symbolOption,
  timeframe: timeframeOption,
  start: Options.text("start").pipe(
    Options.withDefault(""),
    Options.withDescription(
      "Inclusive backtest start date (YYYY-MM-DD). Empty = earliest available candle.",
    ),
  ),
  end: Options.text("end").pipe(
    Options.withDefault(""),
    Options.withDescription(
      "Inclusive backtest end date (YYYY-MM-DD). Empty = latest available candle.",
    ),
  ),
  template: Options.text("template").pipe(
    Options.withDefault(""),
    Options.withDescription(
      "Apply a strategy template's signal logic + execution overrides (e.g. microScalp, connorsRsi2)",
    ),
  ),
  capital: capitalOption,
  positionSize: positionSizeOption,
  riskPerTrade: riskPerTradeOption,
  maxPositionSize: riskBasedMaxPositionSizeOption,
  stopLoss: stopLossOption,
  takeProfit: takeProfitOption,
  fee: feeOption,
  minConfidence: confidenceOption,
  useAtrStops: useAtrStopsOption,
  atrStopMultiplier: atrStopMultiplierOption,
  atrTakeProfitMultiplier: atrTakeProfitMultiplierOption,
  atrRiskReward: atrRiskRewardOption,
  scaleOutAtR: scaleOutAtROption,
  scaleOutPct: scaleOutPctOption,
  volatilityLookback: volatilityLookbackOption,
  volatilityLowPct: volatilityLowPctOption,
  volatilityHighPct: volatilityHighPctOption,
  volatilityLowFactor: volatilityLowFactorOption,
  volatilityHighFactor: volatilityHighFactorOption,
  priceOnly: priceOnlyOption,
  noRsi: noRsiOption,
  holdUntilStop: holdUntilStopOption,
  noTrend: noTrendOption,
  regimeMode: regimeModeOption,
  futures: futuresOption,
  fundingRatePct: fundingRateOption,
  slippageBps: slippageBpsOption,
  trailingStopPct: trailingStopPctOption,
  trailingStopAtrMultiplier: trailingStopAtrMultOption,
  minAtrPct: minAtrPctOption,
  adxMin: adxMinOption,
  volumeMinRatio: volumeMinRatioOption,
  volumeLookback: volumeLookbackOption,
  minConfluence: minConfluenceOption,
  entryCandleConfirm: entryCandleConfirmOption,
  signalPersistence: signalPersistenceOption,
  momentumConfirmBars: momentumConfirmBarsOption,
  lossConfidencePenalty: lossConfidencePenaltyOption,
  lossConfidenceDecay: lossConfidenceDecayOption,
  htfTimeframe: htfTimeframeOption,
  htfTrendFastPeriod: htfTrendFastPeriodOption,
  htfTrendSlowPeriod: htfTrendSlowPeriodOption,
  htfSignalConfidence: htfSignalConfidenceOption,
  entryPullbackEmaPeriod: entryPullbackEmaPeriodOption,
  entryPullbackMarginPct: entryPullbackMarginPctOption,
  minEfficiencyRatio: minEfficiencyRatioOption,
  efficiencyRatioPeriod: efficiencyRatioPeriodOption,
  rsiLongMax: rsiLongMaxOption,
  rsiShortMin: rsiShortMinOption,
  bollingerLongMaxPctB: bollingerLongMaxPctBOption,
  bollingerShortMinPctB: bollingerShortMinPctBOption,
  recordEquityCurve: recordEquityCurveOption,
  exportTrades: exportTradesOption,
  oosPct: oosPctOption,
  mcIterations: mcIterationsOption,
  leverage: leverageOption,
  breakevenAtR: breakevenAtROption,
  maxBarsInTrade: maxBarsInTradeOption,
  lossCooldownBars: lossCooldownBarsOption,
  sessionStart: sessionStartOption,
  sessionEnd: sessionEndOption,
  autoRegimeFilter: autoRegimeFilterOption,
  autoRegimeAdxThreshold: autoRegimeAdxThresholdOption,
  trendSignalStyle: trendSignalStyleOption,
  trendFastPeriod: trendFastPeriodOption,
  trendSlowPeriod: trendSlowPeriodOption,
  directionalOnly: directionalOnlyOption,
  rsiFollowTrend: rsiFollowTrendOption,
  strictAgreement: strictAgreementOption,
  entryOnClose: entryOnCloseOption,
  observedPrice: observedPriceOption,
  realistic: realisticOption,
  strictRealism: strictRealismOption,
  realisticSlippageBps: realisticSlippageBpsOption,
  makerFeePct: makerFeeOption,
  entryOrderType: entryOrderTypeOption,
  entryLimitOffsetBps: entryLimitOffsetBpsOption,
  rsiPeriod: rsiPeriodOption,
  rsiOversoldStrong: rsiOversoldStrongOption,
  rsiOverboughtStrong: rsiOverboughtStrongOption,
  trendFilterPeriod: trendFilterPeriodOption,
  entryRsiLongThreshold: entryRsiLongThresholdOption,
  entryRsiShortThreshold: entryRsiShortThresholdOption,
  exitRsiPeriod: exitRsiPeriodOption,
  exitRsiLongLevel: exitRsiLongLevelOption,
  exitRsiShortLevel: exitRsiShortLevelOption,
  breakoutLookback: breakoutLookbackOption,
  breakoutVolumeMinRatio: breakoutVolumeMinRatioOption,
  breakoutAdxMin: breakoutAdxMinOption,
  fundingBiasThreshold: fundingBiasThresholdOption,
  useFunding: useFundingOption,
  strategyType: strategyTypeOption,
  gridStepPct: gridStepPctOption,
  gridMaxGrids: gridMaxGridsOption,
  gridPauseAfterLossBars: gridPauseAfterLossBarsOption,
  onlyWithTrend: onlyWithTrendOption,
  targetRatio: targetRatioOption,
  chopGateAdx: chopGateAdxOption,
  volatilityTargetAnnualPct: volatilityTargetAnnualPctOption,
  profile: profileOption,
};

export const backtestCommand = Command.make(
  "backtest",
  backtestOptions,
  (args) =>
    Effect.gen(function* () {
      const path = yield* Path;
      const sqlite = yield* SqliteClient;
      const repoLayer = MarketDataRepositorySQLiteLive(sqlite.database);

      const profile = yield* loadProfileIfNeeded(path.homeDir, args.profile);
      if (Option.isSome(profile)) {
        const overrideKeys = Object.keys(profile.value.symbols);
        if (
          overrideKeys.length > 0 &&
          findSymbolOverride(profile.value, args.symbol) === undefined
        ) {
          yield* Effect.logWarning(
            `Profile '${args.profile}' defines symbol overrides (${overrideKeys.join(", ")}) but none match ${args.symbol}; using profile defaults only.`,
          );
        }
      }
      const programArgs = Option.isSome(profile)
        ? resolveBacktestArgs(
            profile.value,
            args.symbol,
            args.exchange,
            args.timeframe,
            args,
          )
        : args;

      const result = yield* backtestProgram(programArgs).pipe(
        Effect.provide(repoLayer),
        Effect.tap((r) => printBacktestResult(r)),
        Effect.catch((err) =>
          Effect.gen(function* () {
            const msg = err instanceof Error ? err.message : err.reason;
            yield* Console.error(`backtest failed: ${msg}`);
            return emptyResult(args.symbol);
          }),
        ),
      );

      return result;
    }).pipe(Effect.provide(makeDbLayer(process.env.NEURATRADE_HOME))),
).pipe(
  Command.withDescription(
    "Backtest deterministic scalping strategy on historical candles",
  ),
);

export interface BacktestArgs extends ResolvedBacktestArgs {}

export function buildBacktestComposerConfig(
  priceOnly: boolean,
  noRsi: boolean,
  noTrend: boolean,
  regimeMode: "trend" | "reversion" | "breakout" = "trend",
  volumeMinRatio = 0,
  volumeLookback = 20,
  minConfluence = 0,
  entryCandleConfirm = false,
  momentumConfirmBars = 0,
  adxMin = 0,
  breakoutLookback = 0,
  fundingBiasThreshold?: number,
  useFunding?: boolean,
): ComposerConfig {
  if (
    !priceOnly &&
    !noRsi &&
    !noTrend &&
    regimeMode === defaultComposerConfig.thresholds.regimeMode &&
    volumeMinRatio <= 0 &&
    minConfluence <= 0 &&
    !entryCandleConfirm &&
    momentumConfirmBars <= 0 &&
    adxMin <= 0 &&
    (breakoutLookback <= 0 ||
      breakoutLookback === defaultComposerConfig.thresholds.breakoutLookback) &&
    fundingBiasThreshold === undefined &&
    useFunding === undefined
  ) {
    return defaultComposerConfig;
  }

  const weights = { ...defaultComposerConfig.weights };
  if (priceOnly) {
    weights.spread = 0;
    weights.imbalance = 0;
    weights.liquidity = 0;
  }
  if (noRsi) {
    weights.rsi = 0;
  }
  if (noTrend) {
    weights.trend = 0;
  }

  const activeSum = Object.values(weights).reduce((a, b) => a + b, 0);
  if (activeSum <= 0) return defaultComposerConfig;

  const normalized: ComposerConfig["weights"] = {
    spread: weights.spread / activeSum,
    imbalance: weights.imbalance / activeSum,
    volatility: weights.volatility / activeSum,
    trend: weights.trend / activeSum,
    liquidity: weights.liquidity / activeSum,
    rsi: weights.rsi / activeSum,
    rsiPullback: weights.rsiPullback / activeSum,
    emaPullback: weights.emaPullback / activeSum,
    regime: weights.regime / activeSum,
    funding: weights.funding / activeSum,
    connorsRsi2: weights.connorsRsi2 / activeSum,
  };

  return {
    weights: normalized,
    thresholds: {
      ...defaultComposerConfig.thresholds,
      regimeMode,
      volumeMinRatio,
      volumeLookback,
      minConfluence,
      entryCandleConfirm,
      momentumConfirmBars,
      adxMin,
      ...(breakoutLookback > 0 ? { breakoutLookback } : {}),
      ...(fundingBiasThreshold !== undefined ? { fundingBiasThreshold } : {}),
      ...(useFunding !== undefined ? { useFunding } : {}),
    },
  };
}

/**
 * Derive the inclusive candle range for a backtest from either the raw CLI
 * `--start`/`--end` options or profile-set `startDate`/`endDate`. Empty values
 * leave the bound open (earliest/latest available candle). An inverted or
 * equal (zero-width) range is rejected.
 */
function resolveBacktestCandleRange(args: ResolvedBacktestArgs): {
  from?: Date;
  to?: Date;
} {
  const start = args.start ?? args.startDate;
  const end = args.end ?? args.endDate;
  const range: { from?: Date; to?: Date } = {};
  if (start && start.trim().length > 0)
    range.from = new Date(`${start}T00:00:00Z`);
  if (end && end.trim().length > 0) range.to = new Date(`${end}T00:00:00Z`);

  if (range.from && range.to && range.from.getTime() >= range.to.getTime()) {
    throw new Error(
      `backtest range is inverted or empty: start (${start}) must be before end (${end})`,
    );
  }
  return range;
}

export function backtestProgram(args: ResolvedBacktestArgs) {
  return Effect.gen(function* () {
    const repo = yield* MarketDataRepository;
    const path = yield* Path;
    const engine = yield* BacktestEngine;

    const candleRange = resolveBacktestCandleRange(args);

    const candles = yield* repo.getCandles({
      exchange: args.exchange,
      symbol: args.symbol,
      timeframe: args.timeframe,
      from: candleRange.from,
      to: candleRange.to,
    });

    const htfCandles =
      args.htfTimeframe && args.htfTimeframe.trim().length > 0
        ? yield* repo.getCandles({
            exchange: args.exchange,
            symbol: args.symbol,
            timeframe: args.htfTimeframe,
            from: candleRange.from,
            to: candleRange.to,
          })
        : [];

    if (candles.length === 0) {
      return yield* Effect.fail(
        new MarketDataRepositoryError(
          `No candles found for ${args.exchange}:${args.symbol}:${args.timeframe}. Run 'market fetch-candles' first.`,
        ),
      );
    }

    // Historical funding rates power the funding-bias component. Missing
    // rows leave the component inert (buildFundingComponent returns null);
    // a fetch failure must not fail the backtest.
    const fundingRates = yield* repo
      .getFundingRates(
        args.exchange,
        args.symbol,
        candles[0]?.timestamp,
        candles[candles.length - 1]?.timestamp,
      )
      .pipe(
        Effect.catch((err) =>
          Effect.gen(function* () {
            yield* Effect.logWarning(
              `failed to load funding rates: ${
                err instanceof Error ? err.message : String(err)
              } — funding component inert`,
            );
            return [] as readonly FundingRate[];
          }),
        ),
      );
    if (fundingRates.length === 0) {
      yield* Effect.logWarning(
        "funding rates absent — funding component inert",
      );
    } else {
      yield* Effect.log(
        `loaded ${fundingRates.length} funding rates — funding component active`,
      );
    }

    let composerConfig = buildBacktestComposerConfig(
      args.priceOnly,
      args.noRsi,
      args.noTrend,
      args.regimeMode,
      args.volumeMinRatio,
      args.volumeLookback,
      args.minConfluence,
      args.entryCandleConfirm,
      args.momentumConfirmBars,
      args.adxMin,
      args.breakoutLookback,
      args.fundingBiasThreshold,
      args.useFunding,
    );

    // --template applies the strategy template's signal weights/thresholds
    // (e.g. microScalp RSI(2)) and its execution overrides on top of the
    // CLI-derived config. Previously the template flags were parsed but
    // never wired — backtests silently ran the default composer.
    if (args.template !== undefined && args.template !== "") {
      const template = args.template as StrategyTemplateName;
      composerConfig = buildComposerConfigFromTemplate(
        template,
        composerConfig,
      );
      args = buildBacktestArgsFromTemplate(template, args);
    }

    const result: BacktestResult =
      args.strategyType === "grid"
        ? yield* Effect.gen(function* () {
            const runOne = (slice: readonly CandleLike[]) =>
              engine.runGridBacktest(slice, {
                gridStepPct: args.gridStepPct,
                gridMaxGrids: args.gridMaxGrids,
                gridPauseAfterLossBars: args.gridPauseAfterLossBars,
                feePct: args.fee,
                slippageBps: args.slippageBps,
                initialCapital: args.capital,
                trendFilterPeriod: args.trendFilterPeriod,
                leverage: args.leverage,
                onlyWithTrend: args.onlyWithTrend,
                targetRatio: args.targetRatio,
                chopGateAdxThreshold: args.chopGateAdx,
              });
            if (args.oosPct > 0) {
              const { is: isCandles, oos: oosCandles } = splitCandlesByOos(
                candles,
                args.oosPct,
              );
              if (isCandles.length >= 20 && oosCandles.length >= 20) {
                const isResult = yield* runOne(isCandles);
                const oosResult = yield* runOne(oosCandles);
                const isBt = gridResultToBacktestResult(
                  args.symbol,
                  isResult,
                  isCandles,
                  args.capital,
                  args.fee,
                );
                const oosBt = gridResultToBacktestResult(
                  args.symbol,
                  oosResult,
                  oosCandles,
                  args.capital,
                  args.fee,
                );
                return attachMonteCarlo(
                  { ...isBt, oosResult: oosBt },
                  args.capital,
                  args.mcIterations,
                );
              }
            }
            const full = yield* runOne(candles);
            return attachMonteCarlo(
              gridResultToBacktestResult(
                args.symbol,
                full,
                candles,
                args.capital,
                args.fee,
              ),
              args.capital,
              args.mcIterations,
            );
          })
        : yield* engine.runBacktest({
            symbol: args.symbol,
            exchange: args.exchange,
            timeframe: args.timeframe,
            candles,
            composerConfig,
            initialCapital: args.capital,
            positionSizePct: args.positionSize,
            riskPerTradePct: args.riskPerTrade,
            maxPositionSizePct: args.maxPositionSize,
            stopLossPct: args.stopLoss,
            takeProfitPct: args.takeProfit,
            feePct: args.fee,
            minConfidence: args.minConfidence,
            useAtrStops: args.useAtrStops,
            atrStopMultiplier: args.atrStopMultiplier,
            atrTakeProfitMultiplier: args.atrTakeProfitMultiplier,
            atrRiskReward: args.atrRiskReward,
            scaleOutAtR: args.scaleOutAtR,
            scaleOutPct: args.scaleOutPct,
            volatilityLookback: args.volatilityLookback,
            volatilityLowPct: args.volatilityLowPct,
            volatilityHighPct: args.volatilityHighPct,
            volatilityLowFactor: args.volatilityLowFactor,
            volatilityHighFactor: args.volatilityHighFactor,
            holdUntilStop: args.holdUntilStop,
            isFutures: args.futures,
            fundingRatePct: args.fundingRatePct,
            fundingRates,
            slippageBps: args.slippageBps,
            makerFeePct: args.makerFeePct,
            entryOrderType: args.entryOrderType,
            entryLimitOffsetBps: args.entryLimitOffsetBps,
            entryOnClose: args.entryOnClose,
            trailingStopPct: args.trailingStopPct,
            trailingStopAtrMultiplier: args.trailingStopAtrMultiplier,
            minAtrPct: args.minAtrPct,
            signalPersistence: args.signalPersistence,
            lossConfidencePenalty: args.lossConfidencePenalty,
            lossConfidenceDecay: args.lossConfidenceDecay,
            htfCandles,
            htfTrendFastPeriod: args.htfTrendFastPeriod,
            htfTrendSlowPeriod: args.htfTrendSlowPeriod,
            entryPullbackEmaPeriod: args.entryPullbackEmaPeriod,
            entryPullbackMarginPct: args.entryPullbackMarginPct,
            minEfficiencyRatio: args.minEfficiencyRatio,
            efficiencyRatioPeriod: args.efficiencyRatioPeriod,
            rsiLongMax: args.rsiLongMax,
            rsiShortMin: args.rsiShortMin,
            bollingerLongMaxPctB: args.bollingerLongMaxPctB,
            bollingerShortMinPctB: args.bollingerShortMinPctB,
            recordEquityCurve:
              args.recordEquityCurve || args.exportTrades.length > 0,
            oosPct: args.oosPct,
            mcIterations: args.mcIterations,
            leverage: args.leverage,
            breakevenAtR: args.breakevenAtR,
            maxBarsInTrade: args.maxBarsInTrade,
            lossCooldownBars: args.lossCooldownBars,
            sessionStart: args.sessionStart,
            sessionEnd: args.sessionEnd,
            autoRegimeFilter: args.autoRegimeFilter,
            autoRegimeAdxThreshold: args.autoRegimeAdxThreshold,
          });

    if (args.exportTrades && args.exportTrades.length > 0) {
      const exportPath = isAbsolute(args.exportTrades)
        ? args.exportTrades
        : resolve(path.homeDir, "data", args.exportTrades);
      yield* exportBacktestResults(result, exportPath);
    }

    return result;
  });
}

function exportBacktestResults(
  result: import("../scalping/backtest.js").BacktestResult,
  exportPath: string,
): Effect.Effect<void, Error, FileSystem.FileSystem> {
  return Effect.gen(function* () {
    const fsys = yield* FileSystem.FileSystem;
    yield* fsys
      .makeDirectory(dirname(exportPath), { recursive: true })
      .pipe(
        Effect.mapError(
          (cause) =>
            new Error(`Failed to create export directory: ${String(cause)}`),
        ),
      );

    const tradesHeader =
      "symbol,side,entryTime,exitTime,entryPrice,exitPrice,pnl,pnlPct,exitReason,initialRiskPct\n";
    const tradesRows = result.trades
      .map(
        (t) =>
          `${t.symbol},${t.side},${t.entryTime.toISOString()},${t.exitTime.toISOString()},${t.entryPrice.toFixed(8)},${t.exitPrice.toFixed(8)},${t.pnl.toFixed(8)},${t.pnlPct.toFixed(8)},${t.exitReason},${t.initialRiskPct.toFixed(8)}`,
      )
      .join("\n");
    yield* Effect.tryPromise({
      try: () =>
        Bun.write(`${exportPath}-trades.csv`, tradesHeader + tradesRows),
      catch: (err) => new Error(`Failed to write trades CSV: ${String(err)}`),
    });

    if (result.equityCurve && result.equityCurve.length > 0) {
      const equityHeader = "tradeIndex,timestamp,capital\n";
      const equityRows = result.equityCurve
        .map(
          (e) =>
            `${e.tradeIndex},${e.timestamp.toISOString()},${e.capital.toFixed(8)}`,
        )
        .join("\n");
      yield* Effect.tryPromise({
        try: () =>
          Bun.write(`${exportPath}-equity.csv`, equityHeader + equityRows),
        catch: (err) => new Error(`Failed to write equity CSV: ${String(err)}`),
      });
    }

    yield* Console.log(`\n💾 Exported trades to ${exportPath}-trades.csv`);
  });
}

function printBacktestResult(
  result: import("../scalping/backtest.js").BacktestResult,
) {
  return Effect.gen(function* () {
    yield* Console.log("\n📊 Backtest Results");
    yield* Console.log("===================");
    yield* Console.log(`Symbol:        ${result.symbol}`);
    yield* Console.log(`Total trades:  ${result.totalTrades}`);
    yield* Console.log(`Win rate:      ${(result.winRate * 100).toFixed(2)}%`);
    yield* Console.log(`Total return:  ${result.totalReturnPct.toFixed(2)}%`);
    yield* Console.log(`Max drawdown:  ${result.maxDrawdownPct.toFixed(2)}%`);
    yield* Console.log(`Sharpe ratio:  ${result.sharpeRatio.toFixed(3)}`);
    yield* Console.log("\n📈 Performance Metrics");
    yield* Console.log("----------------------");
    yield* Console.log(
      `Profit factor:   ${formatNumber(result.metrics.profitFactor, 3)}`,
    );
    yield* Console.log(
      `Expectancy:      ${result.metrics.expectancy.toFixed(3)}%`,
    );
    yield* Console.log(
      `Avg R-multiple:  ${result.metrics.averageRMultiple.toFixed(3)}`,
    );
    yield* Console.log(
      `Sortino ratio:   ${formatNumber(result.metrics.sortinoRatio, 3)}`,
    );
    yield* Console.log(
      `Calmar ratio:    ${formatNumber(result.metrics.calmarRatio, 3)}`,
    );
    yield* Console.log(
      `Max cons. losses: ${result.metrics.maxConsecutiveLosses}`,
    );
    yield* Console.log(
      `Avg trade duration: ${result.metrics.averageTradeDurationHours.toFixed(2)}h`,
    );
    yield* Console.log(
      `Time in market:  ${result.metrics.timeInMarketPct.toFixed(2)}%`,
    );
    if (result.oosResult) {
      const oos = result.oosResult;
      yield* Console.log("\n📤 Out-of-Sample Results");
      yield* Console.log("------------------------");
      yield* Console.log(`Total trades:  ${oos.totalTrades}`);
      yield* Console.log(`Win rate:      ${(oos.winRate * 100).toFixed(2)}%`);
      yield* Console.log(`Total return:  ${oos.totalReturnPct.toFixed(2)}%`);
      yield* Console.log(`Max drawdown:  ${oos.maxDrawdownPct.toFixed(2)}%`);
      yield* Console.log(`Sharpe ratio:  ${oos.sharpeRatio.toFixed(3)}`);
    }

    if (result.monteCarlo) {
      const mc = result.monteCarlo;
      yield* Console.log("\n🎲 Monte Carlo Drawdown");
      yield* Console.log("------------------------");
      yield* Console.log(`Iterations:        ${mc.iterations}`);
      yield* Console.log(
        `Median max DD:     ${mc.medianMaxDrawdownPct.toFixed(2)}%`,
      );
      yield* Console.log(
        `P95 max DD:        ${mc.p95MaxDrawdownPct.toFixed(2)}%`,
      );
      yield* Console.log(
        `P99 max DD:        ${mc.p99MaxDrawdownPct.toFixed(2)}%`,
      );
      yield* Console.log(
        `Worst max DD:      ${mc.worstMaxDrawdownPct.toFixed(2)}%`,
      );
      yield* Console.log(
        `Ruin probability:  ${mc.probabilityOfRuinPct.toFixed(2)}%`,
      );
    }

    if (result.trades.length > 0) {
      yield* Console.log("\nLast 5 trades:");
      for (const trade of result.trades.slice(-5)) {
        yield* Console.log(
          `  ${trade.side} ${trade.entryPrice.toFixed(2)} → ${trade.exitPrice.toFixed(2)} | ` +
            `PnL ${trade.pnlPct.toFixed(2)}% | ${trade.exitReason}`,
        );
      }
    }
  });
}

function gridResultToBacktestResult(
  symbol: string,
  grid: GridResult,
  candles: readonly CandleLike[],
  initialCapital: number,
  feePct: number,
): BacktestResult {
  const trades: BacktestTrade[] = grid.trades.map(
    (t: GridTrade, idx: number) => {
      const entryTime =
        candles[t.entryBar]?.timestamp ?? candles[0]?.timestamp ?? new Date(0);
      const exitTime =
        candles[t.exitBar]?.timestamp ??
        candles[candles.length - 1]?.timestamp ??
        new Date(0);
      return {
        id: `grid-${idx}`,
        symbol,
        side: t.side,
        entryTime,
        exitTime,
        entryPrice: t.entryPrice,
        exitPrice: t.exitPrice,
        pnl: t.pnlQuote,
        pnlPct: t.pnlPct * 100,
        netPnl: t.pnlQuote,
        exitReason: t.isLiquidation
          ? ("liquidation" as const)
          : t.win
            ? ("take_profit" as const)
            : ("stop_loss" as const),
        initialRiskPct: 0,
        fillType: "maker" as const,
        entryFeePct: feePct / 2,
        exitFeePct: feePct / 2,
      };
    },
  );
  const first = candles[0]?.timestamp.getTime() ?? 0;
  const last = candles[candles.length - 1]?.timestamp.getTime() ?? 0;
  const metrics = computePerformanceMetrics({
    trades,
    initialCapital,
    maxDrawdownPct: grid.maxDrawdownPct,
    totalReturnPct: grid.totalReturnPct,
    candleSpanMs: Math.max(0, last - first),
  });
  const winningTrades = trades.filter((t) => t.pnlPct > 0).length;
  return {
    symbol,
    totalTrades: grid.totalTrades,
    winningTrades,
    losingTrades: grid.totalTrades - winningTrades,
    winRate: grid.winRate / 100,
    totalReturnPct: grid.totalReturnPct,
    maxDrawdownPct: grid.maxDrawdownPct,
    sharpeRatio: 0,
    trades,
    totalFeesPaid: 0,
    totalFundingCost: 0,
    benchmarkReturnPct: 0,
    metrics,
    robustnessScore: 0,
  };
}

function emptyResult(
  symbol: string,
): import("../scalping/backtest.js").BacktestResult {
  return {
    symbol,
    totalTrades: 0,
    winningTrades: 0,
    losingTrades: 0,
    winRate: 0,
    totalReturnPct: 0,
    maxDrawdownPct: 0,
    sharpeRatio: 0,
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
}

export interface OptimizeCandidateParams {
  readonly useAtrStops: boolean;
  readonly stopMult: number;
  readonly tpMult: number;
  readonly stopLossPct: number;
  readonly takeProfitPct: number;
  readonly minConfidence: number;
  readonly breakevenAtR: number;
  readonly maxBarsInTrade: number;
  readonly lossCooldownBars: number;
  readonly adxMin: number;
  readonly minEfficiencyRatio: number;
  readonly rsiLongMax: number;
  readonly rsiShortMin: number;
  readonly entryOrderType: "market" | "limit";
  readonly entryLimitOffsetBps: number;
}

export interface OptimizeResult {
  readonly params: OptimizeCandidateParams;
  readonly isResult: BacktestResult;
  readonly oosResult?: BacktestResult;
}

export interface OptimizeArgs extends ResolvedBacktestArgs {
  readonly atrStopMin: number;
  readonly atrStopMax: number;
  readonly atrStopStep: number;
  readonly atrTpMin: number;
  readonly atrTpMax: number;
  readonly atrTpStep: number;
  readonly confMin: number;
  readonly confMax: number;
  readonly confStep: number;
  readonly stopLossMin: number;
  readonly stopLossMax: number;
  readonly stopLossStep: number;
  readonly takeProfitMin: number;
  readonly takeProfitMax: number;
  readonly takeProfitStep: number;
  readonly breakevenAtRMin: number;
  readonly breakevenAtRMax: number;
  readonly breakevenAtRStep: number;
  readonly maxBarsInTradeMin: number;
  readonly maxBarsInTradeMax: number;
  readonly maxBarsInTradeStep: number;
  readonly lossCooldownBarsMin: number;
  readonly lossCooldownBarsMax: number;
  readonly lossCooldownBarsStep: number;
  readonly adxMinMin: number;
  readonly adxMinMax: number;
  readonly adxMinStep: number;
  readonly minEfficiencyRatioMin: number;
  readonly minEfficiencyRatioMax: number;
  readonly minEfficiencyRatioStep: number;
  readonly rsiLongMaxMin: number;
  readonly rsiLongMaxMax: number;
  readonly rsiLongMaxStep: number;
  readonly rsiShortMinMin: number;
  readonly rsiShortMinMax: number;
  readonly rsiShortMinStep: number;
  readonly scanEntryOrders: boolean;
  readonly randomSearch: number;
  readonly noAtr: boolean;
  readonly walkForward: boolean;
  readonly wfTrainDays: number;
  readonly wfTestDays: number;
  readonly wfStepDays: number;
  readonly minTrades: number;
  readonly minOosTrades: number;
  readonly selectBy: "return" | "sharpe" | "calmar";
}

function mergeOptimizeArgs(
  args: OptimizeArgs,
  profile: StrategyProfile,
): OptimizeArgs {
  const overrides = findSymbolOverride(profile, args.symbol) ?? {};
  const get = <K extends keyof StrategyProfileParams>(
    key: K,
  ): StrategyProfileParams[K] =>
    (overrides[key] !== undefined
      ? overrides[key]
      : profile.defaults[key]) as StrategyProfileParams[K];

  const base: Partial<OptimizeArgs> = {
    atrRiskReward: get("atrRiskReward"),
    scaleOutAtR: get("scaleOutAtR"),
    scaleOutPct: get("scaleOutPct"),
    volatilityLookback: get("volatilityLookback"),
    volatilityLowPct: get("volatilityLowPct"),
    volatilityHighPct: get("volatilityHighPct"),
    volatilityLowFactor: get("volatilityLowFactor"),
    volatilityHighFactor: get("volatilityHighFactor"),
    volumeMinRatio: get("volumeMinRatio"),
    volumeLookback: get("volumeLookback"),
    minConfluence: get("minConfluence"),
    entryCandleConfirm: get("entryCandleConfirm"),
    momentumConfirmBars: get("momentumConfirmBars"),
  };

  return { ...base, ...args };
}

export const optimizeCommand = Command.make(
  "optimize",
  {
    exchange: exchangeOption,
    symbol: symbolOption,
    timeframe: timeframeOption,
    capital: capitalOption,
    positionSize: positionSizeOption,
    riskPerTrade: riskPerTradeOption,
    maxPositionSize: riskBasedMaxPositionSizeOption,
    fee: feeOption,
    priceOnly: priceOnlyOption,
    noRsi: noRsiOption,
    noTrend: noTrendOption,
    holdUntilStop: holdUntilStopOption,
    regimeMode: regimeModeOption,
    atrRiskReward: atrRiskRewardOption,
    scaleOutAtR: scaleOutAtROption,
    scaleOutPct: scaleOutPctOption,
    volatilityLookback: volatilityLookbackOption,
    volatilityLowPct: volatilityLowPctOption,
    volatilityHighPct: volatilityHighPctOption,
    volatilityLowFactor: volatilityLowFactorOption,
    volatilityHighFactor: volatilityHighFactorOption,
    atrStopMin: atrStopMinOption,
    atrStopMax: atrStopMaxOption,
    atrStopStep: atrStopStepOption,
    atrTpMin: atrTpMinOption,
    atrTpMax: atrTpMaxOption,
    atrTpStep: atrTpStepOption,
    confMin: confMinOption,
    confMax: confMaxOption,
    confStep: confStepOption,
    volumeMinRatio: volumeMinRatioOption,
    volumeLookback: volumeLookbackOption,
    minConfluence: minConfluenceOption,
    entryCandleConfirm: entryCandleConfirmOption,
    momentumConfirmBars: momentumConfirmBarsOption,
    noAtr: noAtrOption,
    scanEntryOrders: scanEntryOrdersOption,
    randomSearch: randomSearchOption,
    walkForward: walkForwardOption,
    wfTrainDays: wfTrainDaysOption,
    wfTestDays: wfTestDaysOption,
    wfStepDays: wfStepDaysOption,
    minTrades: minTradesOption,
    minOosTrades: minOosTradesOption,
    selectBy: selectByOption,
    stopLossMin: stopLossMinOption,
    stopLossMax: stopLossMaxOption,
    stopLossStep: stopLossStepOption,
    takeProfitMin: takeProfitMinOption,
    takeProfitMax: takeProfitMaxOption,
    takeProfitStep: takeProfitStepOption,
    breakevenAtRMin: breakevenAtRMinOption,
    breakevenAtRMax: breakevenAtRMaxOption,
    breakevenAtRStep: breakevenAtRStepOption,
    maxBarsInTradeMin: maxBarsInTradeMinOption,
    maxBarsInTradeMax: maxBarsInTradeMaxOption,
    maxBarsInTradeStep: maxBarsInTradeStepOption,
    lossCooldownBarsMin: lossCooldownBarsMinOption,
    lossCooldownBarsMax: lossCooldownBarsMaxOption,
    lossCooldownBarsStep: lossCooldownBarsStepOption,
    adxMinMin: adxMinMinOption,
    adxMinMax: adxMinMaxOption,
    adxMinStep: adxMinStepOption,
    minEfficiencyRatioMin: minEfficiencyRatioMinOption,
    minEfficiencyRatioMax: minEfficiencyRatioMaxOption,
    minEfficiencyRatioStep: minEfficiencyRatioStepOption,
    rsiLongMaxMin: rsiLongMaxMinOption,
    rsiLongMaxMax: rsiLongMaxMaxOption,
    rsiLongMaxStep: rsiLongMaxStepOption,
    rsiShortMinMin: rsiShortMinMinOption,
    rsiShortMinMax: rsiShortMinMaxOption,
    rsiShortMinStep: rsiShortMinStepOption,
    makerFeePct: makerFeeOption,
    entryOrderType: entryOrderTypeOption,
    entryLimitOffsetBps: entryLimitOffsetBpsOption,
    rsiPeriod: rsiPeriodOption,
    rsiOversoldStrong: rsiOversoldStrongOption,
    rsiOverboughtStrong: rsiOverboughtStrongOption,
    trendFilterPeriod: trendFilterPeriodOption,
    entryRsiLongThreshold: entryRsiLongThresholdOption,
    entryRsiShortThreshold: entryRsiShortThresholdOption,
    exitRsiPeriod: exitRsiPeriodOption,
    exitRsiLongLevel: exitRsiLongLevelOption,
    exitRsiShortLevel: exitRsiShortLevelOption,
    observedPrice: observedPriceOption,
    realistic: realisticOption,
    strictRealism: strictRealismOption,
    realisticSlippageBps: realisticSlippageBpsOption,
    breakoutLookback: breakoutLookbackOption,
    breakoutVolumeMinRatio: breakoutVolumeMinRatioOption,
    breakoutAdxMin: breakoutAdxMinOption,
    fundingBiasThreshold: fundingBiasThresholdOption,
    useFunding: useFundingOption,
    strategyType: strategyTypeOption,
    gridStepPct: gridStepPctOption,
    gridMaxGrids: gridMaxGridsOption,
    gridPauseAfterLossBars: gridPauseAfterLossBarsOption,
    onlyWithTrend: onlyWithTrendOption,
    targetRatio: targetRatioOption,
    chopGateAdx: chopGateAdxOption,
    volatilityTargetAnnualPct: volatilityTargetAnnualPctOption,
    profile: profileOption,
  },
  (args) =>
    Effect.gen(function* () {
      const path = yield* Path;
      const sqlite = yield* SqliteClient;
      const repoLayer = MarketDataRepositorySQLiteLive(sqlite.database);

      const profile = yield* loadProfileIfNeeded(path.homeDir, args.profile);
      const programArgs = Option.isSome(profile)
        ? mergeOptimizeArgs(args as unknown as OptimizeArgs, profile.value)
        : (args as unknown as OptimizeArgs);

      const result = yield* optimizeProgram(programArgs).pipe(
        Effect.provide(repoLayer),
        Effect.tap((r) => printOptimizeResult(r, args.symbol, args.timeframe)),
        Effect.catch((err) =>
          Effect.gen(function* () {
            yield* Console.error(`optimize failed: ${err.reason}`);
            return [];
          }),
        ),
      );

      return result;
    }).pipe(Effect.provide(makeDbLayer(process.env.NEURATRADE_HOME))),
).pipe(
  Command.withDescription(
    "Grid-search ATR/confidence parameters over historical candles",
  ),
);

function optimizeProgram(args: OptimizeArgs) {
  return Effect.gen(function* () {
    const repo = yield* MarketDataRepository;
    const engine = yield* BacktestEngine;

    const candles = yield* repo.getCandles({
      exchange: args.exchange,
      symbol: args.symbol,
      timeframe: args.timeframe,
    });

    if (candles.length === 0) {
      return yield* Effect.fail(
        new MarketDataRepositoryError(
          `No candles found for ${args.exchange}:${args.symbol}:${args.timeframe}. Run 'market fetch-candles' first.`,
        ),
      );
    }

    const composerConfig = buildBacktestComposerConfig(
      args.priceOnly,
      args.noRsi,
      args.noTrend,
      args.regimeMode,
      args.volumeMinRatio,
      args.volumeLookback,
      args.minConfluence,
      args.entryCandleConfirm,
      args.momentumConfirmBars,
    );
    const results: Array<{
      readonly stopMult: number;
      readonly tpMult: number;
      readonly minConfidence: number;
      readonly totalReturnPct: number;
      readonly sharpeRatio: number;
      readonly totalTrades: number;
      readonly winRate: number;
      readonly maxDrawdownPct: number;
    }> = [];

    for (
      let stopMult = args.atrStopMin;
      stopMult <= args.atrStopMax + 1e-9;
      stopMult += args.atrStopStep
    ) {
      for (
        let tpMult = args.atrTpMin;
        tpMult <= args.atrTpMax + 1e-9;
        tpMult += args.atrTpStep
      ) {
        for (
          let conf = args.confMin;
          conf <= args.confMax + 1e-9;
          conf += args.confStep
        ) {
          const result = yield* engine.runBacktest({
            symbol: args.symbol,
            exchange: args.exchange,
            timeframe: args.timeframe,
            candles,
            composerConfig,
            initialCapital: args.capital,
            positionSizePct: args.positionSize,
            riskPerTradePct: args.riskPerTrade,
            maxPositionSizePct: args.maxPositionSize,
            stopLossPct: 1.5,
            takeProfitPct: 3.0,
            feePct: args.fee,
            minConfidence: Number(conf.toFixed(4)),
            useAtrStops: true,
            atrStopMultiplier: Number(stopMult.toFixed(4)),
            atrTakeProfitMultiplier: Number(tpMult.toFixed(4)),
            atrRiskReward: args.atrRiskReward,
            scaleOutAtR: args.scaleOutAtR,
            scaleOutPct: args.scaleOutPct,
            volatilityLookback: args.volatilityLookback,
            volatilityLowPct: args.volatilityLowPct,
            volatilityHighPct: args.volatilityHighPct,
            volatilityLowFactor: args.volatilityLowFactor,
            volatilityHighFactor: args.volatilityHighFactor,
            holdUntilStop: args.holdUntilStop,
          });
          results.push({
            stopMult: Number(stopMult.toFixed(4)),
            tpMult: Number(tpMult.toFixed(4)),
            minConfidence: Number(conf.toFixed(4)),
            totalReturnPct: result.totalReturnPct,
            sharpeRatio: result.sharpeRatio,
            totalTrades: result.totalTrades,
            winRate: result.winRate,
            maxDrawdownPct: result.maxDrawdownPct,
          });
        }
      }
    }

    return results;
  });
}

function printOptimizeResult(
  results: ReadonlyArray<{
    readonly stopMult: number;
    readonly tpMult: number;
    readonly minConfidence: number;
    readonly totalReturnPct: number;
    readonly sharpeRatio: number;
    readonly totalTrades: number;
    readonly winRate: number;
    readonly maxDrawdownPct: number;
  }>,
  symbol: string,
  timeframe: string,
) {
  return Effect.gen(function* () {
    if (results.length === 0) {
      yield* Console.log("No optimization results.");
      return;
    }

    const byReturn = [...results]
      .sort((a, b) => b.totalReturnPct - a.totalReturnPct)
      .slice(0, 5);
    const bySharpe = [...results]
      .sort((a, b) => b.sharpeRatio - a.sharpeRatio)
      .slice(0, 5);

    yield* Console.log(
      `\n🔬 Optimization results for ${symbol} ${timeframe} (${results.length} configs tested)`,
    );
    yield* Console.log("\nTop 5 by total return:");
    for (const r of byReturn) {
      yield* Console.log(
        `  stop=${r.stopMult.toFixed(2)} tp=${r.tpMult.toFixed(2)} conf=${r.minConfidence.toFixed(2)} | ` +
          `return=${r.totalReturnPct.toFixed(2)}% sharpe=${r.sharpeRatio.toFixed(3)} trades=${r.totalTrades} win=${(r.winRate * 100).toFixed(1)}% dd=${r.maxDrawdownPct.toFixed(2)}%`,
      );
    }

    yield* Console.log("\nTop 5 by Sharpe ratio:");
    for (const r of bySharpe) {
      yield* Console.log(
        `  stop=${r.stopMult.toFixed(2)} tp=${r.tpMult.toFixed(2)} conf=${r.minConfidence.toFixed(2)} | ` +
          `return=${r.totalReturnPct.toFixed(2)}% sharpe=${r.sharpeRatio.toFixed(3)} trades=${r.totalTrades} win=${(r.winRate * 100).toFixed(1)}% dd=${r.maxDrawdownPct.toFixed(2)}%`,
      );
    }
  });
}

export interface ScanArgs extends Omit<ResolvedBacktestArgs, "symbol"> {
  readonly symbol?: string;
  readonly minCandles: number;
  readonly top: number;
  readonly optimize: boolean;
  readonly minReturnPct: Option.Option<number>;
  readonly minSharpe: Option.Option<number>;
  readonly maxDrawdownPct: Option.Option<number>;
  readonly saveWatchlist: Option.Option<string>;
  readonly watchlistPath?: string;
  readonly selectBy: "return" | "sharpe" | "calmar";
  readonly minTrades: number;
  readonly minOosTrades: number;
}

function mergeScanArgs(args: ScanArgs, profile: StrategyProfile): ScanArgs {
  const defaults = profile.defaults;
  const get = <K extends keyof StrategyProfileParams>(
    key: K,
  ): StrategyProfileParams[K] => defaults[key];

  const base: Partial<ScanArgs> = {
    minConfidence: get("minConfidence"),
    useAtrStops: get("useAtrStops"),
    atrStopMultiplier: get("atrStopMultiplier"),
    atrTakeProfitMultiplier: get("atrTakeProfitMultiplier"),
    atrRiskReward: get("atrRiskReward"),
    stopLoss: get("stopLossPct"),
    takeProfit: get("takeProfitPct"),
    scaleOutAtR: get("scaleOutAtR"),
    scaleOutPct: get("scaleOutPct"),
    volatilityLookback: get("volatilityLookback"),
    volatilityLowPct: get("volatilityLowPct"),
    volatilityHighPct: get("volatilityHighPct"),
    volatilityLowFactor: get("volatilityLowFactor"),
    volatilityHighFactor: get("volatilityHighFactor"),
    minAtrPct: get("minAtrPct"),
    holdUntilStop: get("holdUntilStop"),
    fee: get("feePct"),
    volumeMinRatio: get("volumeMinRatio"),
    volumeLookback: get("volumeLookback"),
    minConfluence: get("minConfluence"),
    entryCandleConfirm: get("entryCandleConfirm"),
    momentumConfirmBars: get("momentumConfirmBars"),
  };

  return { ...base, ...args };
}

export const scanCommand = Command.make(
  "scan",
  {
    exchange: exchangeOption,
    timeframe: timeframeOption,
    capital: capitalOption,
    positionSize: positionSizeOption,
    riskPerTrade: riskPerTradeOption,
    maxPositionSize: riskBasedMaxPositionSizeOption,
    fee: feeOption,
    minConfidence: confidenceOption,
    useAtrStops: useAtrStopsOption,
    atrStopMultiplier: atrStopMultiplierOption,
    atrTakeProfitMultiplier: atrTakeProfitMultiplierOption,
    atrRiskReward: atrRiskRewardOption,
    scaleOutAtR: scaleOutAtROption,
    scaleOutPct: scaleOutPctOption,
    volatilityLookback: volatilityLookbackOption,
    volatilityLowPct: volatilityLowPctOption,
    volatilityHighPct: volatilityHighPctOption,
    volatilityLowFactor: volatilityLowFactorOption,
    volatilityHighFactor: volatilityHighFactorOption,
    stopLoss: stopLossOption,
    takeProfit: takeProfitOption,
    priceOnly: priceOnlyOption,
    noRsi: noRsiOption,
    noTrend: noTrendOption,
    holdUntilStop: holdUntilStopOption,
    regimeMode: regimeModeOption,
    minAtrPct: minAtrPctOption,
    minCandles: minCandlesOption,
    top: topOption,
    optimize: optimizeScanOption,
    minReturnPct: minReturnOption,
    minSharpe: minSharpeOption,
    maxDrawdownPct: scanMaxDrawdownOption,
    saveWatchlist: saveWatchlistOption,
    futures: futuresOption,
    fundingRatePct: fundingRateOption,
    slippageBps: slippageBpsOption,
    volumeMinRatio: volumeMinRatioOption,
    volumeLookback: volumeLookbackOption,
    minConfluence: minConfluenceOption,
    entryCandleConfirm: entryCandleConfirmOption,
    momentumConfirmBars: momentumConfirmBarsOption,
    noAtr: noAtrOption,
    scanEntryOrders: scanEntryOrdersOption,
    randomSearch: randomSearchOption,
    walkForward: walkForwardOption,
    wfTrainDays: wfTrainDaysOption,
    wfTestDays: wfTestDaysOption,
    wfStepDays: wfStepDaysOption,
    minTrades: minTradesOption,
    minOosTrades: minOosTradesOption,
    selectBy: selectByOption,
    stopLossMin: stopLossMinOption,
    stopLossMax: stopLossMaxOption,
    stopLossStep: stopLossStepOption,
    takeProfitMin: takeProfitMinOption,
    takeProfitMax: takeProfitMaxOption,
    takeProfitStep: takeProfitStepOption,
    breakevenAtRMin: breakevenAtRMinOption,
    breakevenAtRMax: breakevenAtRMaxOption,
    breakevenAtRStep: breakevenAtRStepOption,
    maxBarsInTradeMin: maxBarsInTradeMinOption,
    maxBarsInTradeMax: maxBarsInTradeMaxOption,
    maxBarsInTradeStep: maxBarsInTradeStepOption,
    lossCooldownBarsMin: lossCooldownBarsMinOption,
    lossCooldownBarsMax: lossCooldownBarsMaxOption,
    lossCooldownBarsStep: lossCooldownBarsStepOption,
    adxMinMin: adxMinMinOption,
    adxMinMax: adxMinMaxOption,
    adxMinStep: adxMinStepOption,
    minEfficiencyRatioMin: minEfficiencyRatioMinOption,
    minEfficiencyRatioMax: minEfficiencyRatioMaxOption,
    minEfficiencyRatioStep: minEfficiencyRatioStepOption,
    rsiLongMaxMin: rsiLongMaxMinOption,
    rsiLongMaxMax: rsiLongMaxMaxOption,
    rsiLongMaxStep: rsiLongMaxStepOption,
    rsiShortMinMin: rsiShortMinMinOption,
    rsiShortMinMax: rsiShortMinMaxOption,
    rsiShortMinStep: rsiShortMinStepOption,
    makerFeePct: makerFeeOption,
    entryOrderType: entryOrderTypeOption,
    entryLimitOffsetBps: entryLimitOffsetBpsOption,
    rsiPeriod: rsiPeriodOption,
    rsiOversoldStrong: rsiOversoldStrongOption,
    rsiOverboughtStrong: rsiOverboughtStrongOption,
    trendFilterPeriod: trendFilterPeriodOption,
    entryRsiLongThreshold: entryRsiLongThresholdOption,
    entryRsiShortThreshold: entryRsiShortThresholdOption,
    exitRsiPeriod: exitRsiPeriodOption,
    exitRsiLongLevel: exitRsiLongLevelOption,
    exitRsiShortLevel: exitRsiShortLevelOption,
    observedPrice: observedPriceOption,
    realistic: realisticOption,
    strictRealism: strictRealismOption,
    realisticSlippageBps: realisticSlippageBpsOption,
    breakoutLookback: breakoutLookbackOption,
    breakoutVolumeMinRatio: breakoutVolumeMinRatioOption,
    breakoutAdxMin: breakoutAdxMinOption,
    fundingBiasThreshold: fundingBiasThresholdOption,
    useFunding: useFundingOption,
    strategyType: strategyTypeOption,
    gridStepPct: gridStepPctOption,
    gridMaxGrids: gridMaxGridsOption,
    gridPauseAfterLossBars: gridPauseAfterLossBarsOption,
    onlyWithTrend: onlyWithTrendOption,
    targetRatio: targetRatioOption,
    chopGateAdx: chopGateAdxOption,
    volatilityTargetAnnualPct: volatilityTargetAnnualPctOption,
    profile: profileOption,
  },
  (args) =>
    Effect.gen(function* () {
      const path = yield* Path;
      const sqlite = yield* SqliteClient;
      const repoLayer = MarketDataRepositorySQLiteLive(sqlite.database);

      const profile = yield* loadProfileIfNeeded(path.homeDir, args.profile);
      const mergedArgs = Option.isSome(profile)
        ? mergeScanArgs(args as unknown as ScanArgs, profile.value)
        : (args as unknown as ScanArgs);

      const watchlistPath = Option.match(mergedArgs.saveWatchlist, {
        onNone: () => undefined as string | undefined,
        onSome: (file) => resolve(path.homeDir, "data", file),
      });

      const result = yield* scanProgram({ ...mergedArgs, watchlistPath }).pipe(
        Effect.provide(repoLayer),
        Effect.tap((r) => printScanResult(r)),
        Effect.catch((err) =>
          Effect.gen(function* () {
            yield* Console.error(`scan failed: ${err.reason}`);
            return [];
          }),
        ),
      );

      return result;
    }).pipe(Effect.provide(makeDbLayer(process.env.NEURATRADE_HOME))),
).pipe(
  Command.withDescription(
    "Backtest deterministic scalping across all stored symbols",
  ),
);

export function scanProgram(args: ScanArgs) {
  return Effect.gen(function* () {
    const repo = yield* MarketDataRepository;
    const exchanges = args.exchange
      .split(",")
      .map((e) => e.trim())
      .filter((e) => e.length > 0);

    if (exchanges.length === 0) {
      return yield* Effect.fail(
        new MarketDataRepositoryError("No exchanges provided to scan."),
      );
    }

    const composerConfig = buildBacktestComposerConfig(
      args.priceOnly,
      args.noRsi,
      args.noTrend,
      args.regimeMode,
      args.volumeMinRatio,
      args.volumeLookback,
      args.minConfluence,
      args.entryCandleConfirm,
      args.momentumConfirmBars,
    );

    const results: Array<ScanResult> = [];

    for (const exchange of exchanges) {
      const exchangeResults = yield* scanSingleExchange(
        repo,
        exchange,
        args,
        composerConfig,
      );
      results.push(...exchangeResults);
    }

    if (args.watchlistPath && results.length > 0) {
      const payload = JSON.stringify(
        results.map((r) => ({
          symbol: r.symbol,
          exchange: r.exchange,
          returnPct: r.totalReturnPct,
          sharpe: r.sharpeRatio,
          bestParams: r.bestParams,
        })),
        null,
        2,
      );
      yield* Effect.tryPromise({
        try: () => Bun.write(args.watchlistPath!, payload),
        catch: (err) =>
          new MarketDataRepositoryError(
            `Failed to write watchlist: ${err instanceof Error ? err.message : String(err)}`,
            err,
          ),
      });
      yield* Console.log(`Watchlist saved to ${args.watchlistPath}`);
    }

    return results;
  });
}

function scanSingleExchange(
  repo: import("../market-data/repository.js").MarketDataRepositoryService,
  exchange: string,
  args: ScanArgs,
  composerConfig: ComposerConfig,
) {
  return Effect.gen(function* () {
    const symbols = yield* repo.listSymbols(
      exchange,
      args.timeframe,
      args.minCandles,
    );
    if (symbols.length === 0) {
      yield* Console.warn(
        `No symbols found for ${exchange}:${args.timeframe} with >= ${args.minCandles} candles.`,
      );
      return [];
    }

    const selected = args.top > 0 ? symbols.slice(0, args.top) : symbols;
    const results: Array<ScanResult> = [];

    for (const symbol of selected) {
      const candles = yield* repo.getCandles({
        exchange,
        symbol,
        timeframe: args.timeframe,
      });

      if (candles.length < 50) continue;

      const result = args.optimize
        ? yield* optimizeForSymbol(
            symbol,
            candles,
            args,
            exchange,
            composerConfig,
          )
        : yield* runBacktestWithParams(
            symbol,
            candles,
            args,
            exchange,
            composerConfig,
            {
              atrStopMultiplier: args.atrStopMultiplier,
              atrTakeProfitMultiplier: args.atrTakeProfitMultiplier,
              minConfidence: args.minConfidence,
            },
          );

      if (
        Option.isSome(args.minReturnPct) &&
        result.totalReturnPct < args.minReturnPct.value
      ) {
        continue;
      }

      if (
        Option.isSome(args.minSharpe) &&
        result.sharpeRatio < args.minSharpe.value
      ) {
        continue;
      }

      if (
        Option.isSome(args.maxDrawdownPct) &&
        result.maxDrawdownPct > args.maxDrawdownPct.value
      ) {
        continue;
      }

      results.push({
        symbol,
        exchange,
        totalTrades: result.totalTrades,
        winRate: result.winRate,
        totalReturnPct: result.totalReturnPct,
        maxDrawdownPct: result.maxDrawdownPct,
        sharpeRatio: result.sharpeRatio,
        bestParams: result.bestParams,
      });
    }

    return results;
  });
}

export interface ScanResult {
  readonly symbol: string;
  readonly exchange: string;
  readonly totalTrades: number;
  readonly winRate: number;
  readonly totalReturnPct: number;
  readonly maxDrawdownPct: number;
  readonly sharpeRatio: number;
  readonly bestParams?: {
    readonly atrStopMultiplier: number;
    readonly atrTakeProfitMultiplier: number;
    readonly minConfidence: number;
  };
}

function runBacktestWithParams(
  symbol: string,
  candles: readonly import("../scalping/types.js").CandleLike[],
  args: ScanArgs,
  exchange: string,
  composerConfig: ComposerConfig,
  params: {
    readonly atrStopMultiplier: number;
    readonly atrTakeProfitMultiplier: number;
    readonly minConfidence: number;
  },
): Effect.Effect<
  BacktestResult & { readonly bestParams?: undefined },
  never,
  BacktestEngine
> {
  return Effect.gen(function* () {
    const engine = yield* BacktestEngine;
    return yield* engine.runBacktest({
      symbol,
      exchange,
      timeframe: args.timeframe,
      candles,
      composerConfig,
      initialCapital: args.capital,
      positionSizePct: args.positionSize,
      riskPerTradePct: args.riskPerTrade,
      maxPositionSizePct: args.maxPositionSize,
      stopLossPct: args.stopLoss,
      takeProfitPct: args.takeProfit,
      feePct: args.fee,
      minConfidence: params.minConfidence,
      useAtrStops: args.useAtrStops,
      atrStopMultiplier: params.atrStopMultiplier,
      atrTakeProfitMultiplier: params.atrTakeProfitMultiplier,
      atrRiskReward: args.atrRiskReward,
      scaleOutAtR: args.scaleOutAtR,
      scaleOutPct: args.scaleOutPct,
      volatilityLookback: args.volatilityLookback,
      volatilityLowPct: args.volatilityLowPct,
      volatilityHighPct: args.volatilityHighPct,
      volatilityLowFactor: args.volatilityLowFactor,
      volatilityHighFactor: args.volatilityHighFactor,
      holdUntilStop: args.holdUntilStop,
      minAtrPct: args.minAtrPct,
      isFutures: args.futures,
      fundingRatePct: args.fundingRatePct,
      slippageBps: args.slippageBps,
    });
  });
}

const SCAN_STOP_MULTS = [1.5, 2.0, 2.5];
const SCAN_TP_MULTS = [2.0, 3.0, 4.0];
const SCAN_CONFIDENCES = [0.4, 0.5, 0.6];

function optimizeForSymbol(
  symbol: string,
  candles: readonly import("../scalping/types.js").CandleLike[],
  args: ScanArgs,
  exchange: string,
  composerConfig: ComposerConfig,
): Effect.Effect<
  BacktestResult & {
    readonly bestParams: {
      readonly atrStopMultiplier: number;
      readonly atrTakeProfitMultiplier: number;
      readonly minConfidence: number;
    };
  },
  never,
  BacktestEngine
> {
  return Effect.gen(function* () {
    let best: BacktestResult | null = null;
    let bestParams = {
      atrStopMultiplier: args.atrStopMultiplier,
      atrTakeProfitMultiplier: args.atrTakeProfitMultiplier,
      minConfidence: args.minConfidence,
    };

    for (const stopMult of SCAN_STOP_MULTS) {
      for (const tpMult of SCAN_TP_MULTS) {
        for (const conf of SCAN_CONFIDENCES) {
          const result = yield* runBacktestWithParams(
            symbol,
            candles,
            args,
            exchange,
            composerConfig,
            {
              atrStopMultiplier: stopMult,
              atrTakeProfitMultiplier: tpMult,
              minConfidence: conf,
            },
          );
          if (!best || result.totalReturnPct > best.totalReturnPct) {
            best = result;
            bestParams = {
              atrStopMultiplier: stopMult,
              atrTakeProfitMultiplier: tpMult,
              minConfidence: conf,
            };
          }
        }
      }
    }

    return { ...(best ?? emptyScanResult(symbol)), bestParams };
  });
}

function emptyScanResult(symbol: string): BacktestResult {
  return {
    symbol,
    totalTrades: 0,
    winningTrades: 0,
    losingTrades: 0,
    winRate: 0,
    totalReturnPct: 0,
    maxDrawdownPct: 0,
    sharpeRatio: 0,
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
}

function printScanResult(results: ReadonlyArray<ScanResult>) {
  return Effect.gen(function* () {
    if (results.length === 0) {
      yield* Console.log("No scan results.");
      return;
    }

    const multiExchange = new Set(results.map((r) => r.exchange)).size > 1;

    yield* Console.log("\n🔎 Multi-ticker backtest scan");
    yield* Console.log(
      multiExchange
        ? "Exchange   Symbol        Trades  Win%    Return   Drawdown  Sharpe"
        : "Symbol        Trades  Win%    Return   Drawdown  Sharpe",
    );
    yield* Console.log(
      "--------------------------------------------------------------------",
    );

    for (const r of results) {
      const row = multiExchange
        ? `${r.exchange.padEnd(10)} ${r.symbol.padEnd(13)} ${String(r.totalTrades).padStart(6)}  ` +
          `${(r.winRate * 100).toFixed(1).padStart(5)}%  ` +
          `${r.totalReturnPct.toFixed(2).padStart(6)}%  ` +
          `${r.maxDrawdownPct.toFixed(2).padStart(7)}%   ` +
          `${r.sharpeRatio.toFixed(3)}`
        : `${r.symbol.padEnd(13)} ${String(r.totalTrades).padStart(6)}  ` +
          `${(r.winRate * 100).toFixed(1).padStart(5)}%  ` +
          `${r.totalReturnPct.toFixed(2).padStart(6)}%  ` +
          `${r.maxDrawdownPct.toFixed(2).padStart(7)}%   ` +
          `${r.sharpeRatio.toFixed(3)}`;
      yield* Console.log(row);
    }

    const profitable = results.filter((r) => r.totalReturnPct > 0);
    const avgReturn =
      results.reduce((sum, r) => sum + r.totalReturnPct, 0) / results.length;
    const avgSharpe =
      results.reduce((sum, r) => sum + r.sharpeRatio, 0) / results.length;

    if (results.some((r) => r.bestParams)) {
      yield* Console.log("\nBest params per symbol");
      for (const r of results) {
        if (r.bestParams) {
          const prefix = multiExchange ? `${r.exchange}:${r.symbol}` : r.symbol;
          yield* Console.log(
            `  ${prefix.padEnd(25)} stop=${r.bestParams.atrStopMultiplier.toFixed(1)} ` +
              `tp=${r.bestParams.atrTakeProfitMultiplier.toFixed(1)} ` +
              `conf=${r.bestParams.minConfidence.toFixed(1)}`,
          );
        }
      }
    }

    const highSharpe = results.filter((r) => r.sharpeRatio > 0.5);
    const lowDrawdown = results.filter((r) => r.maxDrawdownPct < 15);
    const liveReady = results.filter(
      (r) =>
        r.totalReturnPct > 0 && r.sharpeRatio > 0.5 && r.maxDrawdownPct < 15,
    );
    const best = results.reduce((max, r) =>
      r.totalReturnPct > max.totalReturnPct ? r : max,
    );
    const worst = results.reduce((min, r) =>
      r.totalReturnPct < min.totalReturnPct ? r : min,
    );

    yield* Console.log("\nSummary");
    yield* Console.log(`  Symbols tested: ${results.length}`);
    yield* Console.log(
      `  Profitable:     ${profitable.length} (${((profitable.length / results.length) * 100).toFixed(1)}%)`,
    );
    yield* Console.log(
      `  Sharpe > 0.5:   ${highSharpe.length} (${((highSharpe.length / results.length) * 100).toFixed(1)}%)`,
    );
    yield* Console.log(
      `  Drawdown < 15%: ${lowDrawdown.length} (${((lowDrawdown.length / results.length) * 100).toFixed(1)}%)`,
    );
    yield* Console.log(
      `  Live-ready:     ${liveReady.length} (${((liveReady.length / results.length) * 100).toFixed(1)}%)`,
    );
    yield* Console.log(`  Avg return:     ${avgReturn.toFixed(2)}%`);
    yield* Console.log(`  Avg Sharpe:     ${avgSharpe.toFixed(3)}`);
    yield* Console.log(
      `  Best:           ${multiExchange ? `${best.exchange}:` : ""}${best.symbol} ${best.totalReturnPct.toFixed(2)}% (Sharpe ${best.sharpeRatio.toFixed(3)})`,
    );
    yield* Console.log(
      `  Worst:          ${multiExchange ? `${worst.exchange}:` : ""}${worst.symbol} ${worst.totalReturnPct.toFixed(2)}% (Sharpe ${worst.sharpeRatio.toFixed(3)})`,
    );

    if (multiExchange) {
      const byExchange = new Map<string, ScanResult[]>();
      for (const r of results) {
        const list = byExchange.get(r.exchange) ?? [];
        list.push(r);
        byExchange.set(r.exchange, list);
      }

      yield* Console.log("\nPer-exchange averages");
      for (const [exchange, list] of byExchange) {
        const avg =
          list.reduce((sum, r) => sum + r.totalReturnPct, 0) / list.length;
        const sharpe =
          list.reduce((sum, r) => sum + r.sharpeRatio, 0) / list.length;
        yield* Console.log(
          `  ${exchange.padEnd(10)} n=${String(list.length).padStart(3)} avgReturn=${avg.toFixed(2)}% avgSharpe=${sharpe.toFixed(3)}`,
        );
      }

      const bySymbol = new Map<string, ScanResult[]>();
      for (const r of results) {
        const list = bySymbol.get(r.symbol) ?? [];
        list.push(r);
        bySymbol.set(r.symbol, list);
      }
      const consistent = [...bySymbol.entries()]
        .filter(([, list]) => list.every((r) => r.totalReturnPct > 0))
        .sort((a, b) => b[1].length - a[1].length);

      if (consistent.length > 0) {
        yield* Console.log("\nCross-exchange consistent symbols");
        for (const [symbol, list] of consistent.slice(0, 10)) {
          const avg =
            list.reduce((sum, r) => sum + r.totalReturnPct, 0) / list.length;
          yield* Console.log(
            `  ${symbol.padEnd(13)} profitable on ${list.length} exchange(s) avgReturn=${avg.toFixed(2)}%`,
          );
        }
      }
    }
  });
}

export interface WatchlistEntry {
  readonly symbol: string;
  readonly exchange?: string;
  readonly returnPct: number;
  /**
   * Present on file-based watchlists. DB-backed rows have no persisted Sharpe
   * (the watchlist schema has no sharpe column) and nothing downstream ranks on
   * this field, so it may be absent.
   */
  readonly sharpe?: number;
  readonly bestParams?: {
    readonly atrStopMultiplier: number;
    readonly atrTakeProfitMultiplier: number;
    readonly minConfidence: number;
  };
  readonly gridParams?: {
    readonly gridStepPct: number;
    readonly gridMaxGrids: number;
    readonly gridPauseAfterLossBars: number;
    /**
     * Validated config reproduced from a DB watchlist row (gate-scored by the
     * universe scan). Absent on file-based watchlists, where the CLI defaults
     * apply.
     */
    readonly targetRatio?: number;
    readonly chopGateAdx?: number;
    /**
     * Portfolio-selected allocation weight for this symbol. Comes from the
     * scan's portfolio selection (equal weights today). Absent on file-based
     * watchlists, where the full base position applies.
     */
    readonly allocatedWeight?: number;
  };
}

/**
 * Per-row grid overrides reproduced from a DB watchlist entry: the row's
 * VALIDATED targetRatio/chopGateAdx replace the CLI defaults so the soak
 * trades the exact grid the universe scan gate-scored. Position sizing scales
 * the CLI base position fraction by the row's portfolio allocation weight:
 * positionFraction = clamp(allocatedWeight, 0.01, 1) * basePositionFraction,
 * where basePositionFraction = maxPositionSizePct/100 (--max-position-size-pct,
 * 50 in the demo soak). allocatedWeight comes from the scan's portfolio
 * selection (equal weights today). Rows missing the fields (file-based
 * watchlists) fall back to the CLI values unchanged.
 */
export function gridOverridesFromWatchlistRow(
  gridParams: WatchlistEntry["gridParams"],
  args: {
    readonly targetRatio?: number;
    readonly chopGateAdx?: number;
    readonly maxPositionSizePct: Option.Option<number>;
  },
): {
  readonly targetRatio: number;
  readonly chopGateAdxThreshold: number;
  readonly maxPositionPct: number;
} {
  const basePositionFraction =
    Option.getOrElse(args.maxPositionSizePct, () => 100) / 100;
  // Legacy watchlist rows (written before the allocated_weight column
  // existed) load 0 — treat 0 as UNSET -> full allocation. Without this the
  // universe pool's positions collapsed to $0.25 (0.01 x base) and every
  // order was guard-rejected (regression 2026-08-09: ADA starved at 0.5%).
  const rawWeight = gridParams?.allocatedWeight ?? 1;
  const allocatedWeight = Math.min(
    1,
    Math.max(0.01, rawWeight === 0 ? 1 : rawWeight),
  );
  return {
    targetRatio: gridParams?.targetRatio ?? args.targetRatio ?? 1,
    chopGateAdxThreshold: gridParams?.chopGateAdx ?? args.chopGateAdx ?? 0,
    maxPositionPct: allocatedWeight * basePositionFraction * 100,
  };
}

export interface PaperTradeArgs extends ResolvedBacktestArgs {
  readonly interval: number;
  readonly iterations: number;
  readonly replayBars: number;
  readonly live: boolean;
  readonly apiKey: string;
  readonly apiSecret: string;
  readonly marginMode: string;
  readonly productType: string;
  readonly maxDrawdownPct: Option.Option<number>;
  readonly maxDailyLossPct: Option.Option<number>;
  readonly maxPositionSizePct: Option.Option<number>;
  readonly maxTradesPerDay: Option.Option<number>;
  readonly minCapital: Option.Option<number>;
  readonly watchlist: Option.Option<string>;
  readonly noWatchlist: boolean;
  readonly killSwitch: boolean;
  readonly disengage: boolean;
  readonly entries?: readonly WatchlistEntry[];
}

function mergePaperTradeArgs(
  args: PaperTradeArgs,
  profile: StrategyProfile,
): PaperTradeArgs {
  const overrides = findSymbolOverride(profile, args.symbol) ?? {};
  const get = <K extends keyof StrategyProfileParams>(
    key: K,
  ): StrategyProfileParams[K] =>
    (overrides[key] !== undefined
      ? overrides[key]
      : profile.defaults[key]) as StrategyProfileParams[K];

  const base: Partial<PaperTradeArgs> = {
    minConfidence: get("minConfidence"),
    useAtrStops: get("useAtrStops"),
    atrStopMultiplier: get("atrStopMultiplier"),
    atrTakeProfitMultiplier: get("atrTakeProfitMultiplier"),
    atrRiskReward: get("atrRiskReward"),
    stopLoss: get("stopLossPct"),
    takeProfit: get("takeProfitPct"),
    scaleOutAtR: get("scaleOutAtR"),
    scaleOutPct: get("scaleOutPct"),
    volatilityLookback: get("volatilityLookback"),
    volatilityLowPct: get("volatilityLowPct"),
    volatilityHighPct: get("volatilityHighPct"),
    volatilityLowFactor: get("volatilityLowFactor"),
    volatilityHighFactor: get("volatilityHighFactor"),
    positionSize: get("positionSizePct"),
    riskPerTrade: get("riskPerTradePct"),
    fee: get("feePct"),
    minAtrPct: get("minAtrPct"),
    holdUntilStop: get("holdUntilStop"),
    volumeMinRatio: get("volumeMinRatio"),
    volumeLookback: get("volumeLookback"),
    minConfluence: get("minConfluence"),
    entryCandleConfirm: get("entryCandleConfirm"),
    momentumConfirmBars: get("momentumConfirmBars"),
  };

  return { ...base, ...args };
}

export interface SoakArgs extends Omit<ResolvedBacktestArgs, "symbol"> {
  readonly symbol?: string;
  readonly watchlist: string;
  readonly interval: number;
  readonly iterations: number;
  readonly live: boolean;
  readonly apiKey: string;
  readonly apiSecret: string;
  readonly marginMode: string;
  readonly productType: string;
  readonly maxDrawdownPct: Option.Option<number>;
  readonly maxDailyLossPct: Option.Option<number>;
  readonly maxPositionSizePct: Option.Option<number>;
  readonly maxTradesPerDay: Option.Option<number>;
  readonly minCapital: Option.Option<number>;
  readonly profile: string;
}

function mergeSoakArgs(args: SoakArgs, profile: StrategyProfile): SoakArgs {
  const defaults = profile.defaults;
  const get = <K extends keyof StrategyProfileParams>(
    key: K,
  ): StrategyProfileParams[K] => defaults[key];

  const base: Partial<SoakArgs> = {
    minConfidence: get("minConfidence"),
    useAtrStops: get("useAtrStops"),
    atrStopMultiplier: get("atrStopMultiplier"),
    atrTakeProfitMultiplier: get("atrTakeProfitMultiplier"),
    atrRiskReward: get("atrRiskReward"),
    stopLoss: get("stopLossPct"),
    takeProfit: get("takeProfitPct"),
    scaleOutAtR: get("scaleOutAtR"),
    scaleOutPct: get("scaleOutPct"),
    volatilityLookback: get("volatilityLookback"),
    volatilityLowPct: get("volatilityLowPct"),
    volatilityHighPct: get("volatilityHighPct"),
    volatilityLowFactor: get("volatilityLowFactor"),
    volatilityHighFactor: get("volatilityHighFactor"),
    positionSize: get("positionSizePct"),
    riskPerTrade: get("riskPerTradePct"),
    maxPositionSize: get("maxPositionSizePct"),
    minAtrPct: get("minAtrPct"),
    holdUntilStop: get("holdUntilStop"),
    fee: get("feePct"),
    volumeMinRatio: get("volumeMinRatio"),
    volumeLookback: get("volumeLookback"),
    minConfluence: get("minConfluence"),
    entryCandleConfirm: get("entryCandleConfirm"),
    momentumConfirmBars: get("momentumConfirmBars"),
  };

  return { ...base, ...args };
}

type MutablePartialRiskLimits = {
  -readonly [
    K in keyof import("../risk/guards.js").RiskLimits
  ]?: import("../risk/guards.js").RiskLimits[K];
};

function loadWatchlist(
  path: string,
): Effect.Effect<readonly WatchlistEntry[], MarketDataRepositoryError> {
  return Effect.tryPromise({
    try: async () => {
      const file = Bun.file(path);
      const text = await file.text();
      return JSON.parse(text) as readonly WatchlistEntry[];
    },
    catch: (err) =>
      new MarketDataRepositoryError(
        `Failed to load watchlist from ${path}: ${err instanceof Error ? err.message : String(err)}`,
        err,
      ),
  });
}

function buildRiskOverrides(args: PaperTradeArgs): MutablePartialRiskLimits {
  const overrides: MutablePartialRiskLimits = {};
  if (Option.isSome(args.maxDrawdownPct))
    overrides.maxDrawdownPct = args.maxDrawdownPct.value;
  if (Option.isSome(args.maxDailyLossPct))
    overrides.maxDailyLossPct = args.maxDailyLossPct.value;
  if (Option.isSome(args.maxPositionSizePct))
    overrides.maxPositionSizePct = args.maxPositionSizePct.value;
  if (Option.isSome(args.maxTradesPerDay))
    overrides.maxTradesPerDay = args.maxTradesPerDay.value;
  if (Option.isSome(args.minCapital))
    overrides.minCapital = args.minCapital.value;
  return overrides;
}

export const paperTradeCommand = Command.make(
  "paper-trade",
  {
    exchange: exchangeOption,
    symbol: symbolOption,
    timeframe: timeframeOption,
    capital: capitalOption,
    positionSize: positionSizeOption,
    riskPerTrade: riskPerTradeOption,
    maxPositionSize: riskBasedMaxPositionSizeOption,
    fee: feeOption,
    minConfidence: confidenceOption,
    useAtrStops: useAtrStopsOption,
    atrStopMultiplier: atrStopMultiplierOption,
    atrTakeProfitMultiplier: atrTakeProfitMultiplierOption,
    atrRiskReward: atrRiskRewardOption,
    scaleOutAtR: scaleOutAtROption,
    scaleOutPct: scaleOutPctOption,
    volatilityLookback: volatilityLookbackOption,
    volatilityLowPct: volatilityLowPctOption,
    volatilityHighPct: volatilityHighPctOption,
    volatilityLowFactor: volatilityLowFactorOption,
    volatilityHighFactor: volatilityHighFactorOption,
    stopLoss: stopLossOption,
    takeProfit: takeProfitOption,
    priceOnly: priceOnlyOption,
    noRsi: noRsiOption,
    noTrend: noTrendOption,
    holdUntilStop: holdUntilStopOption,
    regimeMode: regimeModeOption,
    minAtrPct: minAtrPctOption,
    volumeMinRatio: volumeMinRatioOption,
    volumeLookback: volumeLookbackOption,
    minConfluence: minConfluenceOption,
    entryCandleConfirm: entryCandleConfirmOption,
    momentumConfirmBars: momentumConfirmBarsOption,
    interval: intervalOption,
    iterations: iterationsOption,
    replayBars: replayBarsOption,
    live: liveOption,
    apiKey: apiKeyOption,
    apiSecret: apiSecretOption,
    futures: futuresOption,
    leverage: leverageOption,
    marginMode: marginModeOption,
    productType: productTypeOption,
    maxDrawdownPct: maxDrawdownOption,
    maxDailyLossPct: maxDailyLossOption,
    maxPositionSizePct: maxPositionSizeOption,
    maxTradesPerDay: maxTradesPerDayOption,
    minCapital: minCapitalOption,
    watchlist: watchlistOption,
    noWatchlist: noWatchlistOption,
    killSwitch: killSwitchOption,
    disengage: disengageOption,
    strategy: strategyOption,
    makerFeePct: makerFeeOption,
    entryOrderType: entryOrderTypeOption,
    entryLimitOffsetBps: entryLimitOffsetBpsOption,
    rsiPeriod: rsiPeriodOption,
    rsiOversoldStrong: rsiOversoldStrongOption,
    rsiOverboughtStrong: rsiOverboughtStrongOption,
    trendFilterPeriod: trendFilterPeriodOption,
    entryRsiLongThreshold: entryRsiLongThresholdOption,
    entryRsiShortThreshold: entryRsiShortThresholdOption,
    exitRsiPeriod: exitRsiPeriodOption,
    exitRsiLongLevel: exitRsiLongLevelOption,
    exitRsiShortLevel: exitRsiShortLevelOption,
    observedPrice: observedPriceOption,
    realistic: realisticOption,
    strictRealism: strictRealismOption,
    realisticSlippageBps: realisticSlippageBpsOption,
    slippageBps: slippageBpsOption,
    autoRegimeFilter: autoRegimeFilterOption,
    autoRegimeAdxThreshold: autoRegimeAdxThresholdOption,
    trendSignalStyle: trendSignalStyleOption,
    trendFastPeriod: trendFastPeriodOption,
    trendSlowPeriod: trendSlowPeriodOption,
    directionalOnly: directionalOnlyOption,
    rsiFollowTrend: rsiFollowTrendOption,
    strictAgreement: strictAgreementOption,
    entryOnClose: entryOnCloseOption,
    breakoutLookback: breakoutLookbackOption,
    breakoutVolumeMinRatio: breakoutVolumeMinRatioOption,
    breakoutAdxMin: breakoutAdxMinOption,
    fundingBiasThreshold: fundingBiasThresholdOption,
    useFunding: useFundingOption,
    strategyType: strategyTypeOption,
    gridStepPct: gridStepPctOption,
    gridMaxGrids: gridMaxGridsOption,
    gridPauseAfterLossBars: gridPauseAfterLossBarsOption,
    onlyWithTrend: onlyWithTrendOption,
    targetRatio: targetRatioOption,
    chopGateAdx: chopGateAdxOption,
    volatilityTargetAnnualPct: volatilityTargetAnnualPctOption,
    profile: profileOption,
  },
  (args) =>
    Effect.gen(function* () {
      const path = yield* Path;
      const sqlite = yield* SqliteClient;
      const db = sqlite.database;

      const paperRepoLayer = PaperTradingRepositorySQLiteLive(db);

      const profile = yield* loadProfileIfNeeded(path.homeDir, args.profile);
      const mergedArgs = Option.isSome(profile)
        ? mergePaperTradeArgs(args as unknown as PaperTradeArgs, profile.value)
        : (args as unknown as PaperTradeArgs);

      const watchlist = yield* Option.match(mergedArgs.watchlist, {
        onNone: () =>
          mergedArgs.noWatchlist
            ? Effect.succeed([] as readonly WatchlistEntry[])
            : Effect.gen(function* () {
                const paperRepo = yield* PaperTradingRepository;
                yield* paperRepo.ensureTables();
                const dbExchange = resolveFuturesMarketExchange(
                  mergedArgs.exchange,
                  mergedArgs.futures,
                );
                const dbEntries = yield* paperRepo.listWatchlist(
                  dbExchange,
                  mergedArgs.timeframe,
                );
                if (dbEntries.length === 0) {
                  yield* Console.warn(
                    `⚠️ DB watchlist is empty for ${dbExchange}:${mergedArgs.timeframe} — paper-trade will run with zero symbols; run grid-universe-scan with a matching --exchange first`,
                  );
                }
                return dbEntries.map((e): WatchlistEntry => ({
                  symbol: e.symbol,
                  exchange: e.exchange,
                  returnPct: e.returnPct,
                  gridParams: {
                    gridStepPct: e.gridStepPct,
                    gridMaxGrids: e.gridMaxGrids,
                    gridPauseAfterLossBars: e.gridPauseAfterLossBars,
                    // Reproduce the row's VALIDATED config (gate-scored by the
                    // universe scan) so the soak trades the same grid the
                    // backtest validated, not the CLI defaults.
                    targetRatio: e.targetRatio,
                    chopGateAdx: e.chopGateAdx,
                    allocatedWeight: e.allocatedWeight,
                  },
                }));
              }).pipe(Effect.provide(paperRepoLayer)),
        onSome: (file) => loadWatchlist(resolve(path.homeDir, "data", file)),
      });

      const repoLayer = MarketDataRepositorySQLiteLive(db);
      const riskGuardLayer = RiskGuardLive(
        mergedArgs.live,
        buildRiskOverrides(mergedArgs),
      );
      const killSwitchLayer = KillSwitchSQLiteLive(db);
      const circuitBreakerMaxLoss = Option.getOrElse(
        mergedArgs.maxDailyLossPct,
        () => 2,
      );
      const circuitBreakerLayer = CircuitBreakerSQLiteLive(
        db,
        circuitBreakerMaxLoss,
      );
      const marketDataLayer = mergedArgs.live
        ? MarketDataGatewayLive
        : Layer.provide(MarketDataGatewayRepositoryLive, repoLayer);
      const layers = Layer.mergeAll(
        BunServices.layer,
        PathLive(process.env.NEURATRADE_HOME),
        marketDataLayer,
        repoLayer,
        paperRepoLayer,
        riskGuardLayer,
        killSwitchLayer,
        circuitBreakerLayer,
      );

      if (mergedArgs.killSwitch) {
        yield* Effect.provide(
          KillSwitch.pipe(
            Effect.flatMap((ks) => ks.engage("CLI --kill-switch")),
          ),
          killSwitchLayer,
        );
      }
      if (mergedArgs.disengage) {
        yield* Effect.provide(
          KillSwitch.pipe(Effect.flatMap((ks) => ks.disengage())),
          killSwitchLayer,
        );
      }

      const result = yield* paperTradeProgram({
        ...mergedArgs,
        entries: watchlist,
      }).pipe(
        Effect.provide(layers),
        Effect.tapError((err) =>
          Console.error(
            `paper-trade failed: ${"reason" in err ? err.reason : String(err)}`,
          ),
        ),
      );

      return result;
    }).pipe(Effect.provide(makeDbLayer(process.env.NEURATRADE_HOME))),
).pipe(
  Command.withDescription("Run deterministic scalping paper-trading loop"),
);

function parseMarginMode(value: string): FuturesMarginMode {
  if (value === "isolated" || value === "crossed") {
    return value;
  }
  throw new Error(
    `invalid margin-mode: ${value} (expected "crossed" or "isolated")`,
  );
}

export function resolveFuturesMarketExchange(
  exchange: string,
  futures: boolean,
): string {
  if (!futures) return exchange;
  if (exchange === "binance") return "bitget-futures";
  if (exchange === "bybit") return "bybit-futures";
  return exchange;
}

export function validateLiveExecutionMarket(
  live: boolean,
  futures: boolean,
): string | undefined {
  if (live && !futures) {
    return "live spot execution is disabled; use --futures for the backend risk-gated path";
  }
  return undefined;
}

export function validateLiveSandboxMode(
  live: boolean,
  sandbox: boolean,
): string | undefined {
  if (live && !sandbox) {
    return "live execution is disabled until BITGET_USE_SANDBOX=true is configured for the demo gate";
  }
  return undefined;
}

export function validateLiveExecutionStrategy(
  live: boolean,
  strategyType: "signal" | "grid",
): string | undefined {
  if (live && strategyType === "signal") {
    return "live directional signal execution is disabled; use --strategy-type grid";
  }
  return undefined;
}

export interface LiveGridConfiguration {
  readonly exchange: string;
  readonly symbol: string;
  readonly timeframe: string;
  readonly productType: string;
  readonly gridStepPct: number;
  readonly gridMaxGrids: number;
  readonly gridPauseAfterLossBars: number;
  readonly feePct: number;
  readonly slippageBps: number;
  readonly trendFilterPeriod: number;
  readonly onlyWithTrend: boolean;
  readonly targetRatio: number;
  readonly chopGateAdx: number;
  readonly leverage: number;
  readonly maxPositionSizePct: number;
  readonly maxDrawdownPct: number;
  readonly maxDailyLossPct: number;
}

export function validateLiveGridConfiguration(
  config: LiveGridConfiguration,
  sandbox = false,
): string | undefined {
  const candidate = candidateForSymbol(config.symbol);
  const validatedCandidate =
    candidate !== undefined &&
    config.exchange === candidate.exchange &&
    config.timeframe === candidate.timeframe &&
    config.productType === candidate.productType &&
    config.gridStepPct === candidate.gridStepPct &&
    config.gridMaxGrids === candidate.gridMaxGrids &&
    config.gridPauseAfterLossBars === candidate.gridPauseAfterLossBars &&
    config.feePct === candidate.feePct &&
    config.slippageBps === candidate.slippageBps &&
    config.trendFilterPeriod === candidate.trendFilterPeriod &&
    config.onlyWithTrend === candidate.onlyWithTrend &&
    config.targetRatio === candidate.targetRatio &&
    config.chopGateAdx === candidate.chopGateAdx &&
    // Leverage may EXCEED the validated candidate value: it scales
    // PnL/variance, not the strategy, and tiny accounts need the sizing's
    // floor-raise (e.g. $10/BTC at 2x = 32% margin vs 65% at 1x). The risk
    // engine's maxLeverage cap still bounds it.
    config.leverage >= candidate.leverage;
  if (!validatedCandidate && !sandbox) {
    return "live grid must use a validated readiness cohort candidate";
  }
  const riskCap =
    candidate?.maxPositionSizePct ??
    VALIDATED_BTC_GRID_CANDIDATE.maxPositionSizePct;
  const ddCap =
    candidate?.maxDrawdownPct ?? VALIDATED_BTC_GRID_CANDIDATE.maxDrawdownPct;
  const dailyCap =
    candidate?.maxDailyLossPct ?? VALIDATED_BTC_GRID_CANDIDATE.maxDailyLossPct;
  if (
    !Number.isFinite(config.maxPositionSizePct) ||
    config.maxPositionSizePct <= 0 ||
    config.maxPositionSizePct > riskCap
  ) {
    return "live grid max position size must be between 0% and 50%";
  }
  if (
    !Number.isFinite(config.maxDrawdownPct) ||
    config.maxDrawdownPct <= 0 ||
    config.maxDrawdownPct > ddCap
  ) {
    return "live grid max drawdown must be between 0% and 5%";
  }
  if (
    !Number.isFinite(config.maxDailyLossPct) ||
    config.maxDailyLossPct <= 0 ||
    config.maxDailyLossPct > dailyCap
  ) {
    return "live grid max daily loss must be between 0% and 2%";
  }
  return undefined;
}

export function validateLiveGridWatchlist(
  live: boolean,
  strategyType: "signal" | "grid",
  entries: readonly Pick<WatchlistEntry, "symbol">[] | undefined,
  sandbox = false,
): string | undefined {
  if (
    live &&
    strategyType === "grid" &&
    entries !== undefined &&
    entries.length > 0
  ) {
    if (sandbox) {
      return undefined;
    }
    return "live grid watchlists are disabled; run the validated BTC candidate directly";
  }
  return undefined;
}

export function validateLiveSoakExecution(live: boolean): string | undefined {
  if (live) {
    return "live soak is disabled; use scalp paper-trade --strategy-type grid";
  }
  return undefined;
}

function parseProductType(value: string): BitgetProductType {
  if (
    value === "USDT-FUTURES" ||
    value === "COIN-FUTURES" ||
    value === "USDC-FUTURES"
  ) {
    return value;
  }
  throw new Error(
    `invalid product-type: ${value} (expected USDT-FUTURES, COIN-FUTURES, or USDC-FUTURES)`,
  );
}

/**
 * Fetch the Bitget futures contract table for a product type. Self-contained
 * layer wiring (client + config + rate limiter) so callers inside command
 * programs do not need BitgetClient in their context. Only used on the live
 * path; the simulated path has no exchange contract table.
 */
function fetchBitgetContracts(
  productType: BitgetProductType,
): Effect.Effect<ReadonlyArray<BitgetContract>, Error> {
  const bitgetClientLayer = BitgetClientLiveConfig.pipe(
    Layer.provide(RateLimiterLive()),
    Layer.provide(BitgetConfigLive),
  );
  return Effect.gen(function* () {
    const client = yield* BitgetClient;
    return yield* client.getContracts(productType);
  }).pipe(
    Effect.provide(bitgetClientLayer),
    Effect.mapError((err: unknown) => {
      const detail =
        typeof err === "object" && err !== null && "body" in err
          ? String((err as { body?: unknown }).body ?? "")
          : String(err);
      return new Error(
        `failed to fetch Bitget contracts: ${detail.length > 0 ? detail : String(err)}`,
      );
    }),
  );
}

/**
 * Resolve a symbol's contract size constraints from the fetched contract
 * table: minTradeNum -> minQty, quantityPrecision -> qtyStep (10^-precision),
 * minTradeUSDT as-is. Undefined when the contract is not found — the engine
 * then falls back to legacy sizing and the adapter-level guard still
 * fail-closes on qty/step violations.
 */
function bitgetContractSpecs(
  contracts: ReadonlyArray<BitgetContract>,
  symbol: string,
  productType: BitgetProductType,
): ContractSizeSpec | undefined {
  const { symbol: bsymbol } = toBitgetFuturesSymbol(symbol, productType);
  const contract = contracts.find(
    (c) => c.symbol.toUpperCase() === bsymbol.toUpperCase(),
  );
  if (contract === undefined) return undefined;
  const precision = Number(contract.quantityPrecision);
  return {
    minQty: Number(contract.minTradeNum),
    qtyStep: Number.isFinite(precision) && precision > 0 ? 10 ** -precision : 0,
    minTradeUSDT: Number(contract.minTradeUSDT),
  };
}

function paperTradeProgram(args: PaperTradeArgs) {
  return Effect.gen(function* () {
    const strategyType = args.strategyType ?? "signal";
    // Sandbox is a property of the resolved Bitget client configuration, not
    // free-floating environment state: BitgetClientLiveConfig derives isDemo
    // from the exact same BitgetConfig.useSandbox value, so validation
    // relaxation and demo routing can never disagree.
    const useSandbox = yield* BitgetConfig.pipe(
      Effect.map((config) => config.useSandbox),
      Effect.provide(BitgetConfigLive),
      Effect.orDie,
    );
    const liveMarketError = validateLiveExecutionMarket(
      args.live,
      args.futures,
    );
    if (liveMarketError !== undefined) {
      return yield* Effect.fail(new Error(liveMarketError));
    }
    const liveSandboxError = validateLiveSandboxMode(args.live, useSandbox);
    if (liveSandboxError !== undefined) {
      return yield* Effect.fail(new Error(liveSandboxError));
    }
    const liveStrategyError = validateLiveExecutionStrategy(
      args.live,
      strategyType,
    );
    if (liveStrategyError !== undefined) {
      return yield* Effect.fail(new Error(liveStrategyError));
    }
    const liveGridWatchlistError = validateLiveGridWatchlist(
      args.live,
      strategyType,
      args.entries,
      useSandbox,
    );
    if (liveGridWatchlistError !== undefined) {
      return yield* Effect.fail(new Error(liveGridWatchlistError));
    }
    const marginMode = parseMarginMode(args.marginMode);
    const productType = parseProductType(args.productType);
    if (args.live && strategyType === "grid") {
      const liveGridError = validateLiveGridConfiguration(
        {
          exchange: resolveFuturesMarketExchange(args.exchange, true),
          symbol: args.symbol,
          timeframe: args.timeframe,
          productType,
          gridStepPct: args.gridStepPct,
          gridMaxGrids: args.gridMaxGrids,
          gridPauseAfterLossBars: args.gridPauseAfterLossBars,
          feePct: args.fee,
          slippageBps: args.slippageBps,
          trendFilterPeriod: args.trendFilterPeriod,
          onlyWithTrend: args.onlyWithTrend ?? false,
          targetRatio: args.targetRatio ?? 0,
          chopGateAdx: args.chopGateAdx ?? 0,
          leverage: args.leverage,
          maxPositionSizePct: Option.getOrElse(
            args.maxPositionSizePct,
            () => 100,
          ),
          maxDrawdownPct: Option.getOrElse(args.maxDrawdownPct, () => 100),
          maxDailyLossPct: Option.getOrElse(args.maxDailyLossPct, () => 100),
        },
        // args.live && strategyType === "grid" are guaranteed by the
        // enclosing if; only multi-symbol sandbox runs get relaxed guards.
        (args.entries?.length ?? 0) > 0 && useSandbox,
      );
      if (liveGridError !== undefined) {
        return yield* Effect.fail(new Error(liveGridError));
      }
    }
    const repo = yield* MarketDataRepository;
    yield* repo.ensureTables();

    const paperRepo = yield* PaperTradingRepository;
    yield* paperRepo.ensureTables();

    if (args.replayBars > 0 && args.strategyType === "grid") {
      // Replay is an explicit diagnostic: always start the pass fresh so a
      // stale replay pointer from an earlier session cannot lock the walk
      // out with "no new replay candle" forever.
      yield* paperRepo.resetGridState(
        args.exchange,
        args.symbol,
        args.timeframe,
      );
    }

    const portfolio = yield* paperRepo.getPortfolio();
    const startCapital = portfolio.capital.lessThanOrEqualTo(0)
      ? money(args.capital)
      : portfolio.capital;
    yield* paperRepo.setPortfolio(
      startCapital,
      Decimal.max(portfolio.peakCapital, startCapital),
    );

    const composerConfig = buildBacktestComposerConfig(
      args.priceOnly,
      args.noRsi,
      args.noTrend,
      args.regimeMode,
      args.volumeMinRatio,
      args.volumeLookback,
      args.minConfluence,
      args.entryCandleConfirm,
      args.momentumConfirmBars,
    );

    const entries =
      args.entries && args.entries.length > 0 ? args.entries : undefined;

    const makeSpotOptions = (
      symbol: string,
      exchange: string,
      overrides?: Partial<PaperTradingOptions>,
    ): PaperTradingOptions => ({
      exchange,
      symbol,
      timeframe: args.timeframe,
      composerConfig,
      positionSizePct: args.positionSize,
      riskPerTradePct: overrides?.riskPerTradePct ?? args.riskPerTrade,
      maxPositionSizePct:
        overrides?.maxPositionSizePct ??
        Option.getOrElse(args.maxPositionSizePct, () => 100),
      feePct: args.fee,
      minConfidence: overrides?.minConfidence ?? args.minConfidence,
      useAtrStops: overrides?.useAtrStops ?? args.useAtrStops,
      atrStopMultiplier: overrides?.atrStopMultiplier ?? args.atrStopMultiplier,
      atrTakeProfitMultiplier:
        overrides?.atrTakeProfitMultiplier ?? args.atrTakeProfitMultiplier,
      atrRiskReward: overrides?.atrRiskReward ?? args.atrRiskReward,
      scaleOutAtR: overrides?.scaleOutAtR ?? args.scaleOutAtR,
      scaleOutPct: overrides?.scaleOutPct ?? args.scaleOutPct,
      volatilityLookback:
        overrides?.volatilityLookback ?? args.volatilityLookback,
      volatilityLowPct: overrides?.volatilityLowPct ?? args.volatilityLowPct,
      volatilityHighPct: overrides?.volatilityHighPct ?? args.volatilityHighPct,
      volatilityLowFactor:
        overrides?.volatilityLowFactor ?? args.volatilityLowFactor,
      volatilityHighFactor:
        overrides?.volatilityHighFactor ?? args.volatilityHighFactor,
      stopLossPct: overrides?.stopLossPct ?? args.stopLoss,
      takeProfitPct: overrides?.takeProfitPct ?? args.takeProfit,
      holdUntilStop: overrides?.holdUntilStop ?? args.holdUntilStop,
      minAtrPct: overrides?.minAtrPct ?? args.minAtrPct,
      initialCapital: args.capital,
      isLive: args.live,
      volatilityTargetAnnualPct: args.volatilityTargetAnnualPct,
    });

    // Live futures orders must respect the exchange's contract size step and
    // minimums (a 5 USDT BTC order is 0.000077 BTC, below the 0.0001 step);
    // fetch the contract table once per command run and resolve specs per
    // symbol into the engine options. Simulated runs have no exchange
    // contract table — specs come only from tests/options.
    const contracts =
      args.live && (args.futures || strategyType === "grid")
        ? yield* fetchBitgetContracts(productType)
        : undefined;
    const contractSpecsFor = (symbol: string): ContractSizeSpec | undefined =>
      contracts === undefined
        ? undefined
        : bitgetContractSpecs(contracts, symbol, productType);

    // Futures data and execution both live on Bitget in this port; default the
    // market-data exchange to bitget-futures unless the operator overrides it.
    const makeFuturesOptions = (
      symbol: string,
      exchangeOverride: string,
      overrides?: Partial<FuturesPaperTradingOptions>,
    ): FuturesPaperTradingOptions => {
      const contractSpecs = contractSpecsFor(symbol);
      return {
        exchange: resolveFuturesMarketExchange(exchangeOverride, true),
        symbol,
        timeframe: args.timeframe,
        composerConfig,
        positionSizePct: args.positionSize,
        riskPerTradePct: overrides?.riskPerTradePct ?? args.riskPerTrade,
        maxPositionSizePct:
          overrides?.maxPositionSizePct ??
          Option.getOrElse(args.maxPositionSizePct, () => 100),
        feePct: args.fee,
        minConfidence: overrides?.minConfidence ?? args.minConfidence,
        useAtrStops: overrides?.useAtrStops ?? args.useAtrStops,
        atrStopMultiplier:
          overrides?.atrStopMultiplier ?? args.atrStopMultiplier,
        atrTakeProfitMultiplier:
          overrides?.atrTakeProfitMultiplier ?? args.atrTakeProfitMultiplier,
        atrRiskReward: overrides?.atrRiskReward ?? args.atrRiskReward,
        scaleOutAtR: overrides?.scaleOutAtR ?? args.scaleOutAtR,
        scaleOutPct: overrides?.scaleOutPct ?? args.scaleOutPct,
        volatilityLookback:
          overrides?.volatilityLookback ?? args.volatilityLookback,
        volatilityLowPct: overrides?.volatilityLowPct ?? args.volatilityLowPct,
        volatilityHighPct:
          overrides?.volatilityHighPct ?? args.volatilityHighPct,
        volatilityLowFactor:
          overrides?.volatilityLowFactor ?? args.volatilityLowFactor,
        volatilityHighFactor:
          overrides?.volatilityHighFactor ?? args.volatilityHighFactor,
        stopLossPct: overrides?.stopLossPct ?? args.stopLoss,
        takeProfitPct: overrides?.takeProfitPct ?? args.takeProfit,
        holdUntilStop: overrides?.holdUntilStop ?? args.holdUntilStop,
        minAtrPct: overrides?.minAtrPct ?? args.minAtrPct,
        initialCapital: args.capital,
        isLive: args.live,
        leverage: args.leverage,
        marginMode,
        productType,
        volatilityTargetAnnualPct: args.volatilityTargetAnnualPct,
        // Account-scaled sizing bounds (RefactorSizing 2026-08-09): cap
        // per-trade risk by the daily loss limit and raise leverage for the
        // notional floor on tiny accounts.
        maxDailyLossPct: Option.getOrElse(args.maxDailyLossPct, () => 2),
        maxConcurrentTrades: 1,
        notionalFloor: 5,
        ...(contractSpecs !== undefined ? { contractSpecs } : {}),
      };
    };

    const makeGridOptions = (
      symbol: string,
      exchange: string,
      gridParams?: WatchlistEntry["gridParams"],
    ): GridPaperTradingOptions => {
      const contractSpecs = contractSpecsFor(symbol);
      const rowOverrides = gridOverridesFromWatchlistRow(gridParams, args);
      return {
        exchange: resolveFuturesMarketExchange(exchange, true),
        symbol,
        timeframe: args.timeframe,
        gridStepPct: gridParams?.gridStepPct ?? args.gridStepPct,
        gridMaxGrids: gridParams?.gridMaxGrids ?? args.gridMaxGrids,
        gridPauseAfterLossBars:
          gridParams?.gridPauseAfterLossBars ?? args.gridPauseAfterLossBars,
        feePct: args.fee,
        slippageBps: args.slippageBps,
        trendFilterPeriod: args.onlyWithTrend ? args.trendFilterPeriod : 0,
        initialCapital: args.capital,
        // Per-row: validated targetRatio/chopGateAdx from the watchlist row,
        // position sized by the row's allocatedWeight (see helper).
        maxPositionPct: rowOverrides.maxPositionPct,
        maxDrawdownPct: Option.getOrElse(args.maxDrawdownPct, () => 100),
        leverage: args.leverage,
        onlyWithTrend: args.onlyWithTrend,
        targetRatio: rowOverrides.targetRatio,
        chopGateAdxThreshold: rowOverrides.chopGateAdxThreshold,
        replayBars: args.replayBars > 0 ? args.replayBars : undefined,
        isLive: args.live,
        executionEnvironment:
          args.live && !useSandbox ? "bitget-live" : "bitget-demo",
        productType,
        marginMode,
        ...(contractSpecs !== undefined ? { contractSpecs } : {}),
      };
    };

    const spotAdapterLayer = args.live
      ? BinanceLiveExchangeAdapterLive({
          apiKey: args.apiKey || process.env.BINANCE_API_KEY || "",
          apiSecret: args.apiSecret || process.env.BINANCE_API_SECRET || "",
        })
      : SimulatedExchangeAdapterLive();
    const futuresAdapterLayer = (
      args.live
        ? resolveFuturesMarketExchange(args.exchange, true) === "bybit-futures"
          ? BybitFuturesExchangeAdapterLive.pipe(
              Layer.provide(BybitClientLiveConfig),
              Layer.provide(BybitConfigLive),
            )
          : BitgetFuturesExchangeAdapterLive.pipe(
              Layer.provide(BitgetClientLiveConfig),
            )
        : SimulatedFuturesExchangeAdapterLive()
    ) as Layer.Layer<
      FuturesExchangeAdapterService,
      never,
      MarketDataGatewayService
    >;

    const runSpotIteration = (
      opts: PaperTradingOptions,
    ): Effect.Effect<
      import("../paper-trading/engine.js").PaperTradingIterationResult,
      never,
      never
    > =>
      runPaperTradingIteration(opts).pipe(
        Effect.provide(spotAdapterLayer),
      ) as Effect.Effect<
        import("../paper-trading/engine.js").PaperTradingIterationResult,
        never,
        never
      >;

    const runFuturesIteration = (
      opts: FuturesPaperTradingOptions,
    ): Effect.Effect<
      import("../paper-trading/futures-engine.js").FuturesPaperTradingIterationResult,
      never,
      never
    > =>
      runFuturesPaperTradingIteration(opts).pipe(
        Effect.provide(futuresAdapterLayer),
      ) as Effect.Effect<
        import("../paper-trading/futures-engine.js").FuturesPaperTradingIterationResult,
        never,
        never
      >;

    const runGridIteration = (
      opts: GridPaperTradingOptions,
    ): Effect.Effect<GridPaperTradingIterationResult, never, never> =>
      runGridPaperTradingIteration(opts).pipe(
        Effect.provide(futuresAdapterLayer),
        Effect.catch((err) =>
          Effect.gen(function* () {
            const tag =
              "tag" in err && typeof err.tag === "string" ? err.tag : "";
            // Safety-critical errors must propagate so the loop stops and the
            // process exits for the operator; only transient network/IO errors
            // are safe to skip and retry on the next cadence.
            if (
              tag === "RiskError" ||
              tag === "KillSwitchError" ||
              tag === "CircuitBreakerError"
            ) {
              return yield* Effect.fail(err);
            }
            const state = yield* paperRepo
              .getGridState(opts.exchange, opts.symbol, opts.timeframe)
              .pipe(Effect.orElseSucceed(() => null));
            const reason =
              "reason" in err && typeof err.reason === "string"
                ? err.reason
                : err instanceof Error
                  ? err.message
                  : String(err);
            yield* Console.error(
              `grid iteration skipped (network/IO error): ${reason}`,
            );
            return {
              action: "hold" as const,
              side: state?.side ?? null,
              capital: state ? toNumber(state.capital) : 0,
              peakCapital: state ? toNumber(state.peakCapital) : 0,
              note: `skip: ${reason}`,
            };
          }),
        ),
      ) as Effect.Effect<GridPaperTradingIterationResult, never, never>;

    let remaining = args.iterations;
    // iterations=0 means run forever.
    while (args.iterations === 0 || remaining !== 0) {
      if (entries) {
        // Cohort symbols are owned by their candidate soaks: the universe
        // soak must never trade them (wrong fingerprint -> kill switch
        // storms; regression 2026-08-08: repeated phantom SOL positions).
        const cohortSymbols = new Set<string>(
          READINESS_COHORT_CANDIDATES.map((candidate) => candidate.symbol),
        );
        for (const entry of entries) {
          if (cohortSymbols.has(entry.symbol)) continue;
          if (remaining === 0 && args.iterations !== 0) break;
          const entryExchange = entry.exchange ?? args.exchange;
          const result =
            args.strategyType === "grid"
              ? yield* runGridIteration(
                  makeGridOptions(
                    entry.symbol,
                    entryExchange,
                    entry.gridParams,
                  ),
                )
              : args.futures
                ? yield* runFuturesIteration(
                    makeFuturesOptions(entry.symbol, entryExchange, {
                      minConfidence: entry.bestParams?.minConfidence,
                      atrStopMultiplier: entry.bestParams?.atrStopMultiplier,
                      atrTakeProfitMultiplier:
                        entry.bestParams?.atrTakeProfitMultiplier,
                    }),
                  )
                : yield* runSpotIteration(
                    makeSpotOptions(entry.symbol, entryExchange, {
                      minConfidence: entry.bestParams?.minConfidence,
                      atrStopMultiplier: entry.bestParams?.atrStopMultiplier,
                      atrTakeProfitMultiplier:
                        entry.bestParams?.atrTakeProfitMultiplier,
                    }),
                  );
          yield* Console.log(
            `[${new Date().toISOString()}] ${entryExchange}:${entry.symbol} ${result.action.toUpperCase()} | capital=${result.capital.toFixed(2)} | ${result.note}`,
          );

          if (remaining > 0) {
            remaining -= 1;
          }

          // Sleep between iterations: always in infinite mode (0), otherwise
          // after every iteration except the final one.
          if (args.iterations === 0 || remaining !== 0) {
            yield* Effect.sleep(`${args.interval} seconds`);
          }
        }
      } else {
        const result =
          args.strategyType === "grid"
            ? yield* runGridIteration(
                makeGridOptions(args.symbol, args.exchange),
              )
            : args.futures
              ? yield* runFuturesIteration(
                  makeFuturesOptions(args.symbol, args.exchange),
                )
              : yield* runSpotIteration(
                  makeSpotOptions(args.symbol, args.exchange),
                );
        yield* Console.log(
          `[${new Date().toISOString()}] ${result.action.toUpperCase()} | capital=${result.capital.toFixed(2)} | ${result.note}`,
        );

        if (remaining > 0) {
          remaining -= 1;
        }

        // Sleep between iterations: always in infinite mode (0), otherwise
        // after every iteration except the final one.
        if (args.iterations === 0 || remaining !== 0) {
          yield* Effect.sleep(`${args.interval} seconds`);
        }
      }
    }

    const closedTrades = yield* paperRepo.listRecentTrades(5);
    if (closedTrades.length > 0) {
      yield* Console.log("\nRecent closed trades:");
      for (const t of closedTrades) {
        yield* Console.log(
          `  ${t.side} ${t.entryPrice.toFixed(2)} → ${t.exitPrice.toFixed(2)} | PnL ${t.pnlPct.toFixed(2)}% | ${t.exitReason}`,
        );
      }
    }
  });
}

interface SoakWatchlistFileEntry {
  readonly symbol: string;
  readonly exchange?: string;
  readonly productType?: "USDT-FUTURES" | "USDC-FUTURES" | "COIN-FUTURES";
  readonly leverage?: number;
  readonly marginMode?: string;
  readonly bestParams?: {
    readonly minConfidence?: number;
    readonly atrStopMultiplier?: number;
    readonly atrTakeProfitMultiplier?: number;
  };
}

function loadSoakWatchlist(
  path: string,
): Effect.Effect<readonly SoakWatchlistFileEntry[], MarketDataRepositoryError> {
  return Effect.tryPromise({
    try: async () => {
      const file = Bun.file(path);
      const text = await file.text();
      return JSON.parse(text) as readonly SoakWatchlistFileEntry[];
    },
    catch: (err) =>
      new MarketDataRepositoryError(
        `Failed to load soak watchlist from ${path}: ${err instanceof Error ? err.message : String(err)}`,
        err,
      ),
  });
}

function printSoakResult(result: import("../scalping/soak.js").SoakResult) {
  return Effect.gen(function* () {
    yield* Console.log("\n Multi-ticker soak results");
    yield* Console.log(
      "Symbol        Trades  Return   Drawdown  Win%    Sharpe",
    );
    yield* Console.log(
      "-------------------------------------------------------",
    );

    for (const r of result.perSymbolResults) {
      yield* Console.log(
        `${r.symbol.padEnd(13)} ${String(r.trades).padStart(6)}  ` +
          `${r.totalReturnPct.toFixed(2).padStart(6)}%  ` +
          `${r.maxDrawdownPct.toFixed(2).padStart(7)}%   ` +
          `${(r.winRate * 100).toFixed(1).padStart(5)}%  ` +
          `${r.sharpeRatio.toFixed(3)}`,
      );
    }

    yield* Console.log(
      "-------------------------------------------------------",
    );

    const agg = result.aggregate;
    const totalSymbols = result.perSymbolResults.length;
    yield* Console.log("\nSummary");
    yield* Console.log(`  Symbols:      ${totalSymbols}`);
    yield* Console.log(
      `  Profitable:   ${agg.profitableCount} (${totalSymbols > 0 ? ((agg.profitableCount / totalSymbols) * 100).toFixed(1) : "0.0"}%)`,
    );
    yield* Console.log(`  Avg return:   ${agg.avgReturnPct.toFixed(2)}%`);
    yield* Console.log(`  Max drawdown: ${agg.maxDrawdownPct.toFixed(2)}%`);
    yield* Console.log(`  Avg Sharpe:   ${agg.avgSharpeRatio.toFixed(3)}`);
  });
}

export const soakCommand = Command.make(
  "soak",
  {
    watchlist: soakWatchlistOption,
    exchange: exchangeOption,
    timeframe: timeframeOption,
    capital: capitalOption,
    positionSize: positionSizeOption,
    riskPerTrade: riskPerTradeOption,
    maxPositionSize: riskBasedMaxPositionSizeOption,
    fee: feeOption,
    minConfidence: confidenceOption,
    useAtrStops: useAtrStopsOption,
    atrStopMultiplier: atrStopMultiplierOption,
    atrTakeProfitMultiplier: atrTakeProfitMultiplierOption,
    atrRiskReward: atrRiskRewardOption,
    scaleOutAtR: scaleOutAtROption,
    scaleOutPct: scaleOutPctOption,
    volatilityLookback: volatilityLookbackOption,
    volatilityLowPct: volatilityLowPctOption,
    volatilityHighPct: volatilityHighPctOption,
    volatilityLowFactor: volatilityLowFactorOption,
    volatilityHighFactor: volatilityHighFactorOption,
    stopLoss: stopLossOption,
    takeProfit: takeProfitOption,
    priceOnly: priceOnlyOption,
    noRsi: noRsiOption,
    noTrend: noTrendOption,
    holdUntilStop: holdUntilStopOption,
    regimeMode: regimeModeOption,
    minAtrPct: minAtrPctOption,
    volumeMinRatio: volumeMinRatioOption,
    volumeLookback: volumeLookbackOption,
    minConfluence: minConfluenceOption,
    entryCandleConfirm: entryCandleConfirmOption,
    momentumConfirmBars: momentumConfirmBarsOption,
    interval: intervalOption,
    iterations: iterationsOption,
    replayBars: replayBarsOption,
    live: liveOption,
    apiKey: apiKeyOption,
    apiSecret: apiSecretOption,
    futures: futuresOption,
    leverage: leverageOption,
    marginMode: marginModeOption,
    productType: productTypeOption,
    maxDrawdownPct: maxDrawdownOption,
    maxDailyLossPct: maxDailyLossOption,
    maxPositionSizePct: maxPositionSizeOption,
    maxTradesPerDay: maxTradesPerDayOption,
    minCapital: minCapitalOption,
    makerFeePct: makerFeeOption,
    entryOrderType: entryOrderTypeOption,
    entryLimitOffsetBps: entryLimitOffsetBpsOption,
    rsiPeriod: rsiPeriodOption,
    rsiOversoldStrong: rsiOversoldStrongOption,
    rsiOverboughtStrong: rsiOverboughtStrongOption,
    trendFilterPeriod: trendFilterPeriodOption,
    entryRsiLongThreshold: entryRsiLongThresholdOption,
    entryRsiShortThreshold: entryRsiShortThresholdOption,
    exitRsiPeriod: exitRsiPeriodOption,
    exitRsiLongLevel: exitRsiLongLevelOption,
    exitRsiShortLevel: exitRsiShortLevelOption,
    observedPrice: observedPriceOption,
    realistic: realisticOption,
    strictRealism: strictRealismOption,
    realisticSlippageBps: realisticSlippageBpsOption,
    autoRegimeFilter: autoRegimeFilterOption,
    autoRegimeAdxThreshold: autoRegimeAdxThresholdOption,
    trendSignalStyle: trendSignalStyleOption,
    trendFastPeriod: trendFastPeriodOption,
    trendSlowPeriod: trendSlowPeriodOption,
    directionalOnly: directionalOnlyOption,
    rsiFollowTrend: rsiFollowTrendOption,
    strictAgreement: strictAgreementOption,
    entryOnClose: entryOnCloseOption,
    breakoutLookback: breakoutLookbackOption,
    breakoutVolumeMinRatio: breakoutVolumeMinRatioOption,
    breakoutAdxMin: breakoutAdxMinOption,
    fundingBiasThreshold: fundingBiasThresholdOption,
    useFunding: useFundingOption,
    strategyType: strategyTypeOption,
    gridStepPct: gridStepPctOption,
    gridMaxGrids: gridMaxGridsOption,
    gridPauseAfterLossBars: gridPauseAfterLossBarsOption,
    onlyWithTrend: onlyWithTrendOption,
    targetRatio: targetRatioOption,
    chopGateAdx: chopGateAdxOption,
    volatilityTargetAnnualPct: volatilityTargetAnnualPctOption,
    profile: profileOption,
  },
  (args) =>
    Effect.gen(function* () {
      const path = yield* Path;
      const sqlite = yield* SqliteClient;
      const db = sqlite.database;

      const profile = yield* loadProfileIfNeeded(path.homeDir, args.profile);
      const mergedArgs = Option.isSome(profile)
        ? mergeSoakArgs(args as unknown as SoakArgs, profile.value)
        : (args as unknown as SoakArgs);

      const liveMarketError = validateLiveExecutionMarket(
        mergedArgs.live,
        mergedArgs.futures,
      );
      if (liveMarketError !== undefined) {
        return yield* Effect.fail(new Error(liveMarketError));
      }
      const liveSoakError = validateLiveSoakExecution(mergedArgs.live);
      if (liveSoakError !== undefined) {
        return yield* Effect.fail(new Error(liveSoakError));
      }

      const watchlistPath = resolve(path.homeDir, "data", mergedArgs.watchlist);
      const watchlistEntries = yield* loadSoakWatchlist(watchlistPath);

      const repoLayer = MarketDataRepositorySQLiteLive(db);
      const paperRepoLayer = PaperTradingRepositorySQLiteLive(db);
      const soakRiskOverrides: MutablePartialRiskLimits = {};
      if (Option.isSome(mergedArgs.maxDrawdownPct))
        soakRiskOverrides.maxDrawdownPct = mergedArgs.maxDrawdownPct.value;
      if (Option.isSome(mergedArgs.maxDailyLossPct))
        soakRiskOverrides.maxDailyLossPct = mergedArgs.maxDailyLossPct.value;
      if (Option.isSome(mergedArgs.maxPositionSizePct))
        soakRiskOverrides.maxPositionSizePct =
          mergedArgs.maxPositionSizePct.value;
      if (Option.isSome(mergedArgs.maxTradesPerDay))
        soakRiskOverrides.maxTradesPerDay = mergedArgs.maxTradesPerDay.value;
      if (Option.isSome(mergedArgs.minCapital))
        soakRiskOverrides.minCapital = mergedArgs.minCapital.value;
      const riskGuardLayer = RiskGuardLive(mergedArgs.live, soakRiskOverrides);
      const killSwitchLayer = KillSwitchSQLiteLive(db);
      const circuitBreakerMaxLoss = Option.getOrElse(
        mergedArgs.maxDailyLossPct,
        () => 2,
      );
      const circuitBreakerLayer = CircuitBreakerSQLiteLive(
        db,
        circuitBreakerMaxLoss,
      );
      const marketDataLayer = mergedArgs.live
        ? MarketDataGatewayLive
        : Layer.provide(MarketDataGatewayRepositoryLive, repoLayer);
      const layers = Layer.mergeAll(
        BunServices.layer,
        PathLive(process.env.NEURATRADE_HOME),
        marketDataLayer,
        repoLayer,
        paperRepoLayer,
        riskGuardLayer,
        killSwitchLayer,
        circuitBreakerLayer,
      );

      const spotAdapterLayer = mergedArgs.live
        ? BinanceLiveExchangeAdapterLive({
            apiKey: mergedArgs.apiKey || process.env.BINANCE_API_KEY || "",
            apiSecret:
              mergedArgs.apiSecret || process.env.BINANCE_API_SECRET || "",
          })
        : SimulatedExchangeAdapterLive();
      const futuresAdapterLayer = (
        mergedArgs.live
          ? resolveFuturesMarketExchange(mergedArgs.exchange, true) ===
            "bybit-futures"
            ? BybitFuturesExchangeAdapterLive.pipe(
                Layer.provide(BybitClientLiveConfig),
                Layer.provide(BybitConfigLive),
              )
            : BitgetFuturesExchangeAdapterLive.pipe(
                Layer.provide(BitgetClientLiveConfig),
              )
          : SimulatedFuturesExchangeAdapterLive()
      ) as Layer.Layer<
        FuturesExchangeAdapterService,
        never,
        MarketDataGatewayService
      >;

      const composerConfig = buildBacktestComposerConfig(
        mergedArgs.priceOnly,
        mergedArgs.noRsi,
        mergedArgs.noTrend,
        mergedArgs.regimeMode,
        mergedArgs.volumeMinRatio,
        mergedArgs.volumeLookback,
        mergedArgs.minConfluence,
        mergedArgs.entryCandleConfirm,
        mergedArgs.momentumConfirmBars,
      );

      const marginModeParsed = parseMarginMode(mergedArgs.marginMode);
      const productTypeParsed = parseProductType(mergedArgs.productType);

      const soakWatchlist: SoakSymbol[] = watchlistEntries.map((e) => ({
        symbol: e.symbol,
        exchange: e.exchange ?? mergedArgs.exchange,
        productType:
          e.productType ?? (mergedArgs.futures ? productTypeParsed : undefined),
        leverage: e.leverage ?? mergedArgs.leverage,
        marginMode: (e.marginMode ??
          mergedArgs.marginMode) as SoakSymbol["marginMode"],
        bestParams: e.bestParams,
      }));

      // Live futures orders must respect the exchange's contract size step
      // and minimums; fetch the contract table once per soak run and resolve
      // specs per symbol into the engine options.
      const contracts =
        mergedArgs.live &&
        (mergedArgs.futures ||
          watchlistEntries.some((e) => e.productType !== undefined))
          ? yield* fetchBitgetContracts(productTypeParsed)
          : undefined;

      const runner = (
        symbol: string,
        exchange: string,
        bestParams?: SoakSymbol["bestParams"],
      ): Effect.Effect<IterationResult, unknown, never> => {
        const entry = soakWatchlist.find((e) => e.symbol === symbol);
        const useFutures =
          entry?.productType !== undefined || mergedArgs.futures;
        const futuresExchange =
          useFutures && exchange === "binance" ? "bitget-futures" : exchange;

        if (useFutures) {
          const opts: FuturesPaperTradingOptions = {
            exchange: futuresExchange,
            symbol,
            timeframe: mergedArgs.timeframe,
            composerConfig,
            positionSizePct: mergedArgs.positionSize,
            riskPerTradePct: mergedArgs.riskPerTrade,
            maxPositionSizePct: mergedArgs.maxPositionSize,
            feePct: mergedArgs.fee,
            minConfidence:
              bestParams?.minConfidence ?? mergedArgs.minConfidence,
            useAtrStops:
              bestParams?.atrStopMultiplier !== undefined
                ? true
                : mergedArgs.useAtrStops,
            atrStopMultiplier:
              bestParams?.atrStopMultiplier ?? mergedArgs.atrStopMultiplier,
            atrTakeProfitMultiplier:
              bestParams?.atrTakeProfitMultiplier ??
              mergedArgs.atrTakeProfitMultiplier,
            atrRiskReward: mergedArgs.atrRiskReward,
            scaleOutAtR: mergedArgs.scaleOutAtR,
            scaleOutPct: mergedArgs.scaleOutPct,
            volatilityLookback: mergedArgs.volatilityLookback,
            volatilityLowPct: mergedArgs.volatilityLowPct,
            volatilityHighPct: mergedArgs.volatilityHighPct,
            volatilityLowFactor: mergedArgs.volatilityLowFactor,
            volatilityHighFactor: mergedArgs.volatilityHighFactor,
            stopLossPct: mergedArgs.stopLoss,
            takeProfitPct: mergedArgs.takeProfit,
            holdUntilStop: mergedArgs.holdUntilStop,
            minAtrPct: mergedArgs.minAtrPct,
            initialCapital: mergedArgs.capital,
            isLive: mergedArgs.live,
            leverage: entry?.leverage ?? mergedArgs.leverage,
            marginMode: entry?.marginMode ?? marginModeParsed,
            productType: entry?.productType ?? productTypeParsed,
            volatilityTargetAnnualPct: mergedArgs.volatilityTargetAnnualPct,
            ...(contracts !== undefined
              ? {
                  contractSpecs: bitgetContractSpecs(
                    contracts,
                    symbol,
                    entry?.productType ?? productTypeParsed,
                  ),
                }
              : {}),
          };
          return runFuturesPaperTradingIteration(opts).pipe(
            Effect.provide(futuresAdapterLayer),
            Effect.provide(layers),
            Effect.map((r): IterationResult => ({
              action: r.action,
              capital: r.capital,
              note: r.note,
            })),
          ) as Effect.Effect<IterationResult, unknown, never>;
        }

        const opts: PaperTradingOptions = {
          exchange,
          symbol,
          timeframe: mergedArgs.timeframe,
          composerConfig,
          positionSizePct: mergedArgs.positionSize,
          riskPerTradePct: mergedArgs.riskPerTrade,
          maxPositionSizePct: mergedArgs.maxPositionSize,
          feePct: mergedArgs.fee,
          minConfidence: bestParams?.minConfidence ?? mergedArgs.minConfidence,
          useAtrStops:
            bestParams?.atrStopMultiplier !== undefined
              ? true
              : mergedArgs.useAtrStops,
          atrStopMultiplier:
            bestParams?.atrStopMultiplier ?? mergedArgs.atrStopMultiplier,
          atrTakeProfitMultiplier:
            bestParams?.atrTakeProfitMultiplier ??
            mergedArgs.atrTakeProfitMultiplier,
          atrRiskReward: mergedArgs.atrRiskReward,
          scaleOutAtR: mergedArgs.scaleOutAtR,
          scaleOutPct: mergedArgs.scaleOutPct,
          volatilityLookback: mergedArgs.volatilityLookback,
          volatilityLowPct: mergedArgs.volatilityLowPct,
          volatilityHighPct: mergedArgs.volatilityHighPct,
          volatilityLowFactor: mergedArgs.volatilityLowFactor,
          volatilityHighFactor: mergedArgs.volatilityHighFactor,
          stopLossPct: mergedArgs.stopLoss,
          takeProfitPct: mergedArgs.takeProfit,
          holdUntilStop: mergedArgs.holdUntilStop,
          minAtrPct: mergedArgs.minAtrPct,
          initialCapital: mergedArgs.capital,
          isLive: mergedArgs.live,
          volatilityTargetAnnualPct: mergedArgs.volatilityTargetAnnualPct,
        };
        return runPaperTradingIteration(opts).pipe(
          Effect.provide(spotAdapterLayer),
          Effect.provide(layers),
          Effect.map((r): IterationResult => ({
            action: r.action,
            capital: r.capital,
            note: r.note,
          })),
        ) as Effect.Effect<IterationResult, unknown, never>;
      };

      const soakOptions: SoakOptions = {
        watchlist: soakWatchlist,
        iterationsPerSymbol: mergedArgs.iterations,
        intervalSeconds: mergedArgs.interval,
        isLive: mergedArgs.live,
        initialCapital: mergedArgs.capital,
        positionSizePct: mergedArgs.positionSize,
        feePct: mergedArgs.fee,
        minConfidence: mergedArgs.minConfidence,
        useAtrStops: mergedArgs.useAtrStops,
        atrStopMultiplier: mergedArgs.atrStopMultiplier,
        atrTakeProfitMultiplier: mergedArgs.atrTakeProfitMultiplier,
        atrRiskReward: mergedArgs.atrRiskReward,
        scaleOutAtR: mergedArgs.scaleOutAtR,
        scaleOutPct: mergedArgs.scaleOutPct,
        volatilityLookback: mergedArgs.volatilityLookback,
        volatilityLowPct: mergedArgs.volatilityLowPct,
        volatilityHighPct: mergedArgs.volatilityHighPct,
        volatilityLowFactor: mergedArgs.volatilityLowFactor,
        volatilityHighFactor: mergedArgs.volatilityHighFactor,
        holdUntilStop: mergedArgs.holdUntilStop,
        regimeMode: mergedArgs.regimeMode,
        composerConfig,
        leverage: mergedArgs.leverage,
        marginMode: marginModeParsed,
        productType: productTypeParsed,
      };

      const result = yield* runSoak(soakOptions, runner).pipe(
        Effect.catch((err) =>
          Effect.gen(function* () {
            yield* Console.error(
              `soak failed: ${err instanceof Error ? err.message : String(err)}`,
            );
            return {
              perSymbolResults: [],
              aggregate: {
                avgReturnPct: 0,
                profitableCount: 0,
                maxDrawdownPct: 0,
                avgSharpeRatio: 0,
                totalTrades: 0,
              },
            };
          }),
        ),
      );

      yield* printSoakResult(result);
      return result;
    }).pipe(Effect.provide(makeDbLayer(process.env.NEURATRADE_HOME))),
).pipe(Command.withDescription("Run multi-ticker paper-trading soak harness"));

const profileSaveCommand = Command.make(
  "save",
  { name: profileNameOption, ...backtestOptions },
  (args) =>
    Effect.gen(function* () {
      const path = yield* Path;
      const { name, profile: _profile, ...rest } = args;
      const profile = buildStrategyProfileFromArgs(
        name,
        rest as ResolvedBacktestArgs,
      );
      yield* saveStrategyProfile(path.homeDir, name, profile);
      yield* Console.log(
        `Profile saved to ${resolve(path.homeDir, "profiles", `${name}.json`)}`,
      );
    }).pipe(Effect.provide(makeLayer(process.env.NEURATRADE_HOME))),
).pipe(
  Command.withDescription(
    "Save current backtest options as a strategy profile",
  ),
);

const profileCommand = Command.make("profile", {}, () =>
  Console.log(
    "Profile commands. Use 'profile save --name <name> [backtest options]'.",
  ),
).pipe(
  Command.withDescription("Strategy profile management"),
  Command.withSubcommands([profileSaveCommand]),
);

// ---------------------------------------------------------------------------
// Select / validate / library / walk-forward helpers
// ---------------------------------------------------------------------------

export interface SelectArgs extends ResolvedBacktestArgs {
  readonly universe: string;
  readonly top: number;
  readonly minRobustness: number;
  readonly minReturnPct: number;
  readonly maxDrawdownPct: number;
  readonly minTrades: number;
  readonly selectLookbackCandles: number;
  readonly selectBy: "return" | "sharpe" | "calmar";
}

export interface SelectResult {
  readonly symbol: string;
  readonly params: {
    readonly regimeMode: "trend" | "reversion" | "breakout";
    readonly atrStopMultiplier: number;
    readonly atrTakeProfitMultiplier: number;
    readonly minConfidence: number;
    readonly adxMin: number;
  };
  readonly result: BacktestResult;
}

export interface SelectWatchlistEntry {
  readonly symbol: string;
  readonly timeframe: string;
  readonly profile: SelectResult["params"];
}

export interface ValidationRow {
  readonly symbol: string;
  readonly regimeMode: "trend" | "reversion" | "breakout";
  readonly isReturnPct: number;
  readonly oosReturnPct: number;
  readonly oosMaxDrawdownPct: number;
  readonly mcP95DrawdownPct: number;
  readonly mcRuinPct: number;
  readonly robustnessScore: number;
  readonly isTrades: number;
  readonly oosTrades: number;
  readonly liveReady: boolean;
  readonly entry: SelectWatchlistEntry;
}

export function buildCandidate(
  useAtrStops: boolean,
  vector: readonly number[],
): OptimizeCandidateParams {
  const stopMult = useAtrStops ? vector[0] : 0;
  const tpMult = useAtrStops ? vector[1] : 0;
  const stopLossPct = useAtrStops ? 0 : vector[0];
  const takeProfitPct = useAtrStops ? 0 : vector[1];
  const minConfidence = vector[2] ?? 0.5;
  const breakevenAtR = vector[3] ?? 0;
  const maxBarsInTrade = vector[4] ?? 0;
  const lossCooldownBars = vector[5] ?? 0;
  const adxMin = vector[6] ?? 0;
  const minEfficiencyRatio = vector[7] ?? 0;
  const rsiLongMax = vector[8] ?? 0;
  const rsiShortMin = vector[9] ?? 0;
  const hasEntryOrder = vector.length >= 12;
  const entryOrderTypeIndex = hasEntryOrder ? vector[10] : 0;
  const entryLimitOffsetBps = hasEntryOrder ? vector[11] : 0;
  const entryOrderType = entryOrderTypeIndex < 0.5 ? "market" : "limit";
  return {
    useAtrStops,
    stopMult,
    tpMult,
    stopLossPct,
    takeProfitPct,
    minConfidence,
    breakevenAtR,
    maxBarsInTrade,
    lossCooldownBars,
    adxMin,
    minEfficiencyRatio,
    rsiLongMax,
    rsiShortMin,
    entryOrderType,
    entryLimitOffsetBps,
  };
}

function randomInRange(min: number, max: number): number {
  if (min >= max) return min;
  return Number((min + Math.random() * (max - min)).toFixed(6));
}

function cartesianProduct<T>(arrays: T[][]): T[][] {
  return arrays.reduce<T[][]>(
    (acc, arr) => acc.flatMap((a) => arr.map((b) => [...a, b])),
    [[]],
  );
}

function range(min: number, max: number, step: number): number[] {
  const result: number[] = [];
  if (step <= 0 || max < min) {
    if (min === max) return [min];
    return [min];
  }
  for (let v = min; v <= max + 1e-9; v += step) {
    result.push(Number(v.toFixed(6)));
  }
  return result;
}

export function generateCandidates(
  args: OptimizeArgs,
): OptimizeCandidateParams[] {
  const useAtrStops = !args.noAtr;
  const stopRange = useAtrStops
    ? range(args.atrStopMin, args.atrStopMax, args.atrStopStep)
    : range(args.stopLossMin, args.stopLossMax, args.stopLossStep);
  const tpRange = useAtrStops
    ? range(args.atrTpMin, args.atrTpMax, args.atrTpStep)
    : range(args.takeProfitMin, args.takeProfitMax, args.takeProfitStep);
  const confRange = range(args.confMin, args.confMax, args.confStep);
  const beRange = range(
    args.breakevenAtRMin,
    args.breakevenAtRMax,
    args.breakevenAtRStep,
  );
  const barsRange = range(
    args.maxBarsInTradeMin,
    args.maxBarsInTradeMax,
    args.maxBarsInTradeStep,
  );
  const cooldownRange = range(
    args.lossCooldownBarsMin,
    args.lossCooldownBarsMax,
    args.lossCooldownBarsStep,
  );
  const adxRange = range(args.adxMinMin, args.adxMinMax, args.adxMinStep);
  const erRange = range(
    args.minEfficiencyRatioMin,
    args.minEfficiencyRatioMax,
    args.minEfficiencyRatioStep,
  );
  const rsiLongRange = range(
    args.rsiLongMaxMin,
    args.rsiLongMaxMax,
    args.rsiLongMaxStep,
  );
  const rsiShortRange = range(
    args.rsiShortMinMin,
    args.rsiShortMinMax,
    args.rsiShortMinStep,
  );

  const dimensions = [
    stopRange,
    tpRange,
    confRange,
    beRange,
    barsRange,
    cooldownRange,
    adxRange,
    erRange,
    rsiLongRange,
    rsiShortRange,
  ];

  if (args.scanEntryOrders) {
    dimensions.push([0, 1]); // market, limit
    dimensions.push([0, 5, 10]); // offset bps
  }

  let vectors = cartesianProduct(dimensions);

  if (args.randomSearch > 0) {
    if (vectors.length > args.randomSearch) {
      const shuffled = [...vectors].sort(() => Math.random() - 0.5);
      vectors = shuffled.slice(0, args.randomSearch);
    } else {
      // Sample additional random candidates within the search bounds.
      while (vectors.length < args.randomSearch) {
        const randomVector = [
          randomInRange(
            useAtrStops ? args.atrStopMin : args.stopLossMin,
            useAtrStops ? args.atrStopMax : args.stopLossMax,
          ),
          randomInRange(
            useAtrStops ? args.atrTpMin : args.takeProfitMin,
            useAtrStops ? args.atrTpMax : args.takeProfitMax,
          ),
          randomInRange(args.confMin, args.confMax),
          randomInRange(args.breakevenAtRMin, args.breakevenAtRMax),
          Math.floor(
            randomInRange(args.maxBarsInTradeMin, args.maxBarsInTradeMax),
          ),
          Math.floor(
            randomInRange(args.lossCooldownBarsMin, args.lossCooldownBarsMax),
          ),
          randomInRange(args.adxMinMin, args.adxMinMax),
          randomInRange(args.minEfficiencyRatioMin, args.minEfficiencyRatioMax),
          randomInRange(args.rsiLongMaxMin, args.rsiLongMaxMax),
          randomInRange(args.rsiShortMinMin, args.rsiShortMinMax),
        ];
        if (args.scanEntryOrders) {
          randomVector.push(Math.random() < 0.5 ? 0 : 1);
          randomVector.push([0, 5, 10][Math.floor(Math.random() * 3)]);
        }
        vectors.push(randomVector);
      }
    }
  }

  return vectors.map((v) => buildCandidate(useAtrStops, v));
}

export function objectiveValue(
  result: BacktestResult,
  selectBy: "return" | "sharpe" | "calmar",
): number {
  if (selectBy === "sharpe") return result.sharpeRatio;
  if (selectBy === "calmar") return result.metrics.calmarRatio;
  return result.totalReturnPct;
}

export function selectWinner(
  results: readonly OptimizeResult[],
  selectBy: "return" | "sharpe" | "calmar",
  minTrades: number,
  minOosTrades?: number,
): OptimizeResult | null {
  const oosThreshold = minOosTrades ?? minTrades;
  const passing = results.filter((r) => {
    if (r.oosResult) {
      return r.oosResult.totalTrades >= oosThreshold;
    }
    return r.isResult.totalTrades >= minTrades;
  });
  if (passing.length === 0) return null;
  const sorted = [...passing].sort(
    (a, b) =>
      objectiveValue(b.oosResult ?? b.isResult, selectBy) -
      objectiveValue(a.oosResult ?? a.isResult, selectBy),
  );
  return sorted[0];
}

export function buildStrategyProfileFromOptimizeResult(
  name: string,
  args: OptimizeArgs,
  winner: OptimizeResult,
): StrategyProfile {
  const p = winner.params;
  const override: Partial<StrategyProfileParams> = {
    minConfidence: p.minConfidence,
    adxMin: p.adxMin,
    breakevenAtR: p.breakevenAtR,
    maxBarsInTrade: p.maxBarsInTrade,
    lossCooldownBars: p.lossCooldownBars,
    minEfficiencyRatio: p.minEfficiencyRatio,
    rsiLongMax: p.rsiLongMax,
    rsiShortMin: p.rsiShortMin,
    entryOrderType: p.entryOrderType,
    entryLimitOffsetBps: p.entryLimitOffsetBps,
    ...(p.useAtrStops
      ? {
          atrStopMultiplier: p.stopMult,
          atrTakeProfitMultiplier: p.tpMult,
          stopLossPct: 0,
          takeProfitPct: 0,
        }
      : {
          stopLossPct: p.stopLossPct,
          takeProfitPct: p.takeProfitPct,
          atrStopMultiplier: 0,
          atrTakeProfitMultiplier: 0,
        }),
  };
  const defaults: StrategyProfileParams = {
    ...buildStrategyProfileFromArgs(name, args).defaults,
    ...override,
    useAtrStops: p.useAtrStops,
    exchange: args.exchange,
    defaultSymbol: args.symbol,
    timeframe: args.timeframe,
  };
  return {
    name,
    defaults,
    symbols: {
      [args.symbol]: override,
    },
  };
}

export interface WalkForwardWindow {
  readonly trainCandles: CandleLike[];
  readonly testCandles: CandleLike[];
}

export function generateWalkForwardWindows(
  candles: readonly CandleLike[],
  trainDays: number,
  testDays: number,
  stepDays: number,
): WalkForwardWindow[] {
  if (candles.length < 2) return [];
  const intervalMs =
    candles[1].timestamp.getTime() - candles[0].timestamp.getTime();
  const msPerDay = 24 * 60 * 60 * 1000;
  const candlesPerDay = Math.max(1, Math.round(msPerDay / intervalMs));
  const trainSize = Math.max(1, trainDays * candlesPerDay);
  const testSize = Math.max(1, testDays * candlesPerDay);
  const stepSize = Math.max(1, stepDays * candlesPerDay);

  const windows: WalkForwardWindow[] = [];
  for (
    let start = 0;
    start + trainSize + testSize <= candles.length;
    start += stepSize
  ) {
    windows.push({
      trainCandles: candles.slice(start, start + trainSize) as CandleLike[],
      testCandles: candles.slice(
        start + trainSize,
        start + trainSize + testSize,
      ) as CandleLike[],
    });
  }
  return windows;
}

export function combineWalkForwardResults(
  results: readonly BacktestResult[],
  initialCapital: number,
  symbol: string,
): BacktestResult {
  const combinedTrades: BacktestTrade[] = [];
  let capital = initialCapital;
  let peak = capital;
  let totalFeesPaid = 0;
  let totalFundingCost = 0;

  for (const r of results) {
    const windowStartCapital = capital;
    for (const t of r.trades) {
      const scale = windowStartCapital / initialCapital;
      const scaledNetPnl = t.netPnl * scale;
      capital += scaledNetPnl;
      if (capital > peak) peak = capital;
      combinedTrades.push({ ...t, pnl: t.pnl * scale, netPnl: scaledNetPnl });
      totalFeesPaid += (t.pnl - t.netPnl) * scale;
    }
    totalFeesPaid += r.totalFeesPaid * (windowStartCapital / initialCapital);
    totalFundingCost +=
      r.totalFundingCost * (windowStartCapital / initialCapital);
  }

  const totalReturnPct = ((capital - initialCapital) / initialCapital) * 100;
  const maxDrawdownPct = 0; // Simplified
  const winningTrades = combinedTrades.filter((t) => t.netPnl > 0).length;
  const losingTrades = combinedTrades.filter((t) => t.netPnl < 0).length;

  return {
    symbol,
    totalTrades: combinedTrades.length,
    winningTrades,
    losingTrades,
    winRate:
      combinedTrades.length > 0 ? winningTrades / combinedTrades.length : 0,
    totalReturnPct,
    maxDrawdownPct,
    sharpeRatio: 0,
    trades: combinedTrades,
    totalFeesPaid,
    totalFundingCost,
    benchmarkReturnPct: 0,
    robustnessScore: 0,
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
  };
}

function selectBacktestComposerConfig(
  args: SelectArgs,
  params: SelectResult["params"],
): ComposerConfig {
  return buildBacktestComposerConfig(
    args.priceOnly,
    args.noRsi,
    args.noTrend,
    params.regimeMode,
    args.volumeMinRatio,
    args.volumeLookback,
    args.minConfluence,
    args.entryCandleConfirm,
    args.momentumConfirmBars,
    args.adxMin,
  );
}

export function runSelectBacktest(
  symbol: string,
  candles: readonly CandleLike[],
  args: SelectArgs,
  exchange: string,
  params: SelectResult["params"],
): BacktestResult {
  const composerConfig = selectBacktestComposerConfig(args, params);
  const slippageBps = args.realistic
    ? args.realisticSlippageBps
    : args.slippageBps;
  return runBacktest({
    symbol,
    exchange,
    timeframe: args.timeframe,
    candles,
    composerConfig,
    initialCapital: args.capital,
    positionSizePct: args.positionSize,
    riskPerTradePct: args.riskPerTrade,
    maxPositionSizePct: args.maxPositionSize,
    stopLossPct: args.stopLoss,
    takeProfitPct: args.takeProfit,
    feePct: args.fee,
    makerFeePct: args.makerFeePct,
    entryOrderType: args.entryOrderType,
    entryLimitOffsetBps: args.entryLimitOffsetBps,
    minConfidence: params.minConfidence,
    useAtrStops: true,
    atrStopMultiplier: params.atrStopMultiplier,
    atrTakeProfitMultiplier: params.atrTakeProfitMultiplier,
    atrRiskReward: args.atrRiskReward,
    scaleOutAtR: args.scaleOutAtR,
    scaleOutPct: args.scaleOutPct,
    volatilityLookback: args.volatilityLookback,
    volatilityLowPct: args.volatilityLowPct,
    volatilityHighPct: args.volatilityHighPct,
    volatilityLowFactor: args.volatilityLowFactor,
    volatilityHighFactor: args.volatilityHighFactor,
    volatilityTargetAnnualPct: args.volatilityTargetAnnualPct,
    holdUntilStop: args.holdUntilStop,
    isFutures: args.futures,
    fundingRatePct: args.fundingRatePct,
    slippageBps,
    trailingStopPct: args.trailingStopPct,
    trailingStopAtrMultiplier: args.trailingStopAtrMultiplier,
    minAtrPct: args.minAtrPct,
    signalPersistence: args.signalPersistence,
    lossConfidencePenalty: args.lossConfidencePenalty,
    lossConfidenceDecay: args.lossConfidenceDecay,
    htfCandles: [],
    htfTrendFastPeriod: args.htfTrendFastPeriod,
    htfTrendSlowPeriod: args.htfTrendSlowPeriod,
    entryPullbackEmaPeriod: args.entryPullbackEmaPeriod,
    entryPullbackMarginPct: args.entryPullbackMarginPct,
    minEfficiencyRatio: args.minEfficiencyRatio,
    efficiencyRatioPeriod: args.efficiencyRatioPeriod,
    rsiLongMax: args.rsiLongMax,
    rsiShortMin: args.rsiShortMin,
    bollingerLongMaxPctB: args.bollingerLongMaxPctB,
    bollingerShortMinPctB: args.bollingerShortMinPctB,
    recordEquityCurve: false,
    oosPct: 0,
    mcIterations: 0,
    leverage: args.leverage,
    breakevenAtR: args.breakevenAtR,
    maxBarsInTrade: args.maxBarsInTrade,
    lossCooldownBars: args.lossCooldownBars,
    sessionStart: args.sessionStart,
    sessionEnd: args.sessionEnd,
    autoRegimeFilter: args.autoRegimeFilter,
    autoRegimeAdxThreshold: args.autoRegimeAdxThreshold,
  });
}

export function selectBestForSymbol(
  symbol: string,
  candles: readonly CandleLike[],
  args: SelectArgs,
  exchange: string,
): SelectResult | null {
  const regimeModes: Array<"trend" | "reversion" | "breakout"> = [
    "trend",
    "reversion",
  ];
  const stopMults = [1.5, 2.0];
  const tpMults = [2.0, 2.5];
  const confs = [0.4, 0.5];
  const adxMins = [0, 20];

  let best: SelectResult | null = null;
  let bestObjective = -Infinity;

  for (const regimeMode of regimeModes) {
    for (const atrStopMultiplier of stopMults) {
      for (const atrTakeProfitMultiplier of tpMults) {
        for (const minConfidence of confs) {
          for (const adxMin of adxMins) {
            const params: SelectResult["params"] = {
              regimeMode,
              atrStopMultiplier,
              atrTakeProfitMultiplier,
              minConfidence,
              adxMin,
            };
            const result = runSelectBacktest(
              symbol,
              candles,
              args,
              exchange,
              params,
            );
            if (result.totalTrades < args.minTrades) continue;
            if (result.totalReturnPct < args.minReturnPct) continue;
            if (result.maxDrawdownPct > args.maxDrawdownPct) continue;
            const obj = objectiveValue(result, args.selectBy);
            if (obj > bestObjective) {
              bestObjective = obj;
              best = { symbol, params, result };
            }
          }
        }
      }
    }
  }

  return best;
}

function cliDefaultArgs(): ResolvedBacktestArgs {
  return {
    exchange: "binance",
    symbol: "BTC/USDT",
    timeframe: "1h",
    capital: 10000,
    positionSize: 100,
    riskPerTrade: 0,
    maxPositionSize: 100,
    stopLoss: 1.5,
    takeProfit: 3.0,
    fee: 0.1,
    makerFeePct: 0,
    entryOrderType: "market",
    entryLimitOffsetBps: 0,
    minConfidence: 0.5,
    useAtrStops: false,
    atrStopMultiplier: 1.5,
    atrTakeProfitMultiplier: 2.5,
    atrRiskReward: 0,
    rsiPeriod: 14,
    rsiOversoldStrong: 30,
    rsiOverboughtStrong: 70,
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
    holdUntilStop: false,
    noTrend: false,
    regimeMode: "trend",
    breakoutLookback: 20,
    breakoutVolumeMinRatio: 1.2,
    breakoutAdxMin: 20,
    fundingBiasThreshold: 0.0001,
    useFunding: false,
    futures: false,
    fundingRatePct: 0.01,
    slippageBps: 0,
    trailingStopPct: 0,
    trailingStopAtrMultiplier: 0,
    minAtrPct: 0,
    volumeMinRatio: 0,
    volumeLookback: 20,
    minConfluence: 0,
    entryCandleConfirm: false,
    signalPersistence: 0,
    momentumConfirmBars: 0,
    lossConfidencePenalty: 0,
    lossConfidenceDecay: 0,
    adxMin: 0,
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
    trendFilterPeriod: 200,
    entryRsiLongThreshold: 10,
    entryRsiShortThreshold: 90,
    exitRsiPeriod: 0,
    exitRsiLongLevel: 0,
    exitRsiShortLevel: 0,
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
    strategyType: "signal",
    gridStepPct: 0,
    gridMaxGrids: 0,
    gridPauseAfterLossBars: 0,
    onlyWithTrend: false,
    targetRatio: 1,
  };
}

export function extractExplicitOverrides(
  args: Partial<ResolvedBacktestArgs>,
): Partial<ResolvedBacktestArgs> {
  const defaults = cliDefaultArgs();
  const overrides: Partial<ResolvedBacktestArgs> = {};
  for (const key of Object.keys(args) as Array<keyof ResolvedBacktestArgs>) {
    const value = args[key];
    const defaultValue = defaults[key];
    if (value !== defaultValue) {
      (overrides as Record<string, unknown>)[key] = value;
    }
  }
  // Behavioral flags are always explicit so profiles preserve user intent.
  if (args.observedPrice !== undefined)
    (overrides as Record<string, unknown>).observedPrice = args.observedPrice;
  if (args.realistic !== undefined)
    (overrides as Record<string, unknown>).realistic = args.realistic;
  if (args.strictRealism !== undefined)
    (overrides as Record<string, unknown>).strictRealism = args.strictRealism;
  return overrides;
}

export function loadSelectWatchlist(
  path: string,
): Effect.Effect<readonly SelectWatchlistEntry[], MarketDataRepositoryError> {
  return Effect.tryPromise({
    try: async () => {
      const file = Bun.file(path);
      const text = await file.text();
      return JSON.parse(text) as readonly SelectWatchlistEntry[];
    },
    catch: (err) =>
      new MarketDataRepositoryError(
        `Failed to load watchlist from ${path}: ${err instanceof Error ? err.message : String(err)}`,
        err,
      ),
  });
}

export function buildValidateBacktestArgs(
  entry: SelectWatchlistEntry,
  exchange: string,
): ResolvedBacktestArgs {
  const base = applyPreset("balanced");
  return {
    ...base,
    exchange,
    symbol: entry.symbol,
    timeframe: entry.timeframe,
    useAtrStops: true,
    atrStopMultiplier: entry.profile.atrStopMultiplier,
    atrTakeProfitMultiplier: entry.profile.atrTakeProfitMultiplier,
    minConfidence: entry.profile.minConfidence,
    regimeMode: entry.profile.regimeMode,
    adxMin: entry.profile.adxMin,
    oosPct: 20,
    mcIterations: 200,
    realistic: true,
  };
}

export function isLiveReady(row: ValidationRow): boolean {
  return (
    row.oosReturnPct > 0 &&
    row.oosMaxDrawdownPct <= 15 &&
    row.mcP95DrawdownPct <= 20 &&
    row.mcRuinPct <= 5 &&
    row.isTrades >= 10 &&
    row.oosTrades >= 10
  );
}

export function validateWatchlist(args: {
  watchlist: string;
  exchange: string;
}): Effect.Effect<
  readonly ValidationRow[],
  MarketDataRepositoryError | SqliteError
> {
  return Effect.gen(function* () {
    const path = yield* Path;
    const watchlistPath = resolve(
      path.homeDir,
      "watchlists",
      `${args.watchlist}.json`,
    );
    const entries = yield* loadSelectWatchlist(watchlistPath);

    const sqlite = yield* SqliteClient;
    const engine = yield* BacktestEngine;
    const repoLayer = MarketDataRepositorySQLiteLive(sqlite.database);

    const rows = yield* Effect.gen(function* () {
      const repo = yield* MarketDataRepository;
      const result: ValidationRow[] = [];
      for (const entry of entries) {
        const backtestArgs = buildValidateBacktestArgs(entry, args.exchange);
        const candles = yield* repo.getCandles({
          exchange: args.exchange,
          symbol: entry.symbol,
          timeframe: entry.timeframe,
        });
        if (candles.length === 0) continue;
        const splitIndex = Math.floor(
          candles.length * (1 - backtestArgs.oosPct / 100),
        );
        const isCandles = candles.slice(0, splitIndex);
        const oosCandles = candles.slice(splitIndex);
        const isResult = yield* engine.runBacktest({
          ...backtestArgs,
          candles: isCandles,
          composerConfig: buildBacktestComposerConfig(
            backtestArgs.priceOnly,
            backtestArgs.noRsi,
            backtestArgs.noTrend,
            backtestArgs.regimeMode,
            backtestArgs.volumeMinRatio,
            backtestArgs.volumeLookback,
            backtestArgs.minConfluence,
            backtestArgs.entryCandleConfirm,
            backtestArgs.momentumConfirmBars,
            backtestArgs.adxMin,
          ),
          initialCapital: backtestArgs.capital,
          positionSizePct: backtestArgs.positionSize,
          riskPerTradePct: backtestArgs.riskPerTrade,
          maxPositionSizePct: backtestArgs.maxPositionSize,
          stopLossPct: backtestArgs.stopLoss,
          takeProfitPct: backtestArgs.takeProfit,
          feePct: backtestArgs.fee,
          recordEquityCurve: false,
          htfCandles: [],
        });
        const oosResult = yield* engine.runBacktest({
          ...backtestArgs,
          candles: oosCandles,
          composerConfig: buildBacktestComposerConfig(
            backtestArgs.priceOnly,
            backtestArgs.noRsi,
            backtestArgs.noTrend,
            backtestArgs.regimeMode,
            backtestArgs.volumeMinRatio,
            backtestArgs.volumeLookback,
            backtestArgs.minConfluence,
            backtestArgs.entryCandleConfirm,
            backtestArgs.momentumConfirmBars,
            backtestArgs.adxMin,
          ),
          initialCapital: backtestArgs.capital,
          positionSizePct: backtestArgs.positionSize,
          riskPerTradePct: backtestArgs.riskPerTrade,
          maxPositionSizePct: backtestArgs.maxPositionSize,
          stopLossPct: backtestArgs.stopLoss,
          takeProfitPct: backtestArgs.takeProfit,
          feePct: backtestArgs.fee,
          recordEquityCurve: false,
          htfCandles: [],
        });
        const mcP95DrawdownPct = oosResult.monteCarlo?.p95MaxDrawdownPct ?? 0;
        const mcRuinPct = oosResult.monteCarlo?.probabilityOfRuinPct ?? 0;
        let row: ValidationRow = {
          symbol: entry.symbol,
          regimeMode: entry.profile.regimeMode,
          isReturnPct: isResult.totalReturnPct,
          oosReturnPct: oosResult.totalReturnPct,
          oosMaxDrawdownPct: oosResult.maxDrawdownPct,
          mcP95DrawdownPct,
          mcRuinPct,
          robustnessScore: oosResult.robustnessScore,
          isTrades: isResult.totalTrades,
          oosTrades: oosResult.totalTrades,
          liveReady: false,
          entry,
        };
        row = { ...row, liveReady: isLiveReady(row) };
        result.push(row);
      }
      return result;
    }).pipe(Effect.provide(repoLayer));

    return rows;
  }).pipe(Effect.provide(makeDbLayer(process.env.NEURATRADE_HOME)));
}

export function buildPaperTradeComposerConfig(args: {
  strategy: StrategyTemplateName;
  priceOnly: boolean;
  noRsi: boolean;
  noTrend: boolean;
  regimeMode: "trend" | "reversion" | "breakout";
  volumeMinRatio: number;
  volumeLookback: number;
  minConfluence: number;
  entryCandleConfirm: boolean;
  momentumConfirmBars: number;
  breakoutLookback: number;
  breakoutVolumeMinRatio: number;
  breakoutAdxMin: number;
  useFunding: boolean;
  fundingBiasThreshold: number;
  rsiPeriod: number;
  rsiOversoldStrong: number;
  rsiOverboughtStrong: number;
  trendFilterPeriod: number;
  entryRsiLongThreshold: number;
  entryRsiShortThreshold: number;
  exitRsiLongLevel: number;
  exitRsiShortLevel: number;
}): ComposerConfig {
  const base = buildComposerConfigFromTemplate(args.strategy);
  const custom = buildBacktestComposerConfig(
    args.priceOnly,
    args.noRsi,
    args.noTrend,
    args.regimeMode,
    args.volumeMinRatio,
    args.volumeLookback,
    args.minConfluence,
    args.entryCandleConfirm,
    args.momentumConfirmBars,
    0,
  );
  return {
    weights: { ...custom.weights, ...base.weights },
    thresholds: {
      ...custom.thresholds,
      ...base.thresholds,
      rsiPeriod: args.rsiPeriod,
      rsiOversoldStrong: args.rsiOversoldStrong,
      rsiOverboughtStrong: args.rsiOverboughtStrong,
      trendFilterPeriod: args.trendFilterPeriod,
      entryRsiLongThreshold: args.entryRsiLongThreshold,
      entryRsiShortThreshold: args.entryRsiShortThreshold,
      exitRsiLongThreshold: args.exitRsiLongLevel,
      exitRsiShortThreshold: args.exitRsiShortLevel,
      breakoutLookback: args.breakoutLookback,
      breakoutVolumeMinRatio: args.breakoutVolumeMinRatio,
      breakoutAdxMin: args.breakoutAdxMin,
      useFunding: args.useFunding,
      fundingBiasThreshold: args.fundingBiasThreshold,
    },
  };
}

const libraryListCommand = Command.make("list", {}, () =>
  Effect.gen(function* () {
    const library = yield* StrategyLibrary;
    const strategies = yield* library.listStrategies();
    for (const s of strategies) {
      yield* Console.log(`${s.name}: ${s.description}`);
    }
    return strategies;
  }).pipe(Effect.provide(makeLayer(process.env.NEURATRADE_HOME))),
).pipe(Command.withDescription("List available strategy templates"));

const libraryStrategyCommand = Command.make(
  "strategy",
  {
    strategy: strategyOption,
    ...backtestOptions,
  },
  (args) =>
    Effect.gen(function* () {
      const template = args.strategy as StrategyTemplateName;
      const baseArgs = args as unknown as ResolvedBacktestArgs;
      const library = yield* StrategyLibrary;
      const merged = yield* library.buildBacktestArgsFromTemplate(
        template,
        baseArgs,
      );
      const config = yield* library.buildComposerConfigFromTemplate(template);
      yield* Console.log(`Strategy: ${template}`);
      yield* Console.log(JSON.stringify(merged, null, 2));
      return config;
    }).pipe(Effect.provide(makeLayer(process.env.NEURATRADE_HOME))),
).pipe(Command.withDescription("Show strategy template details"));

export const libraryCommand = Command.make(
  "library",
  {
    list: Options.boolean("list").pipe(Options.withDefault(false)),
    strategy: Options.optional(strategyOption),
  },
  (args) =>
    Effect.gen(function* () {
      const library = yield* StrategyLibrary;
      if (args.list || Option.isNone(args.strategy)) {
        const strategies = yield* library.listStrategies();
        for (const s of strategies) {
          yield* Console.log(`${s.name}: ${s.description}`);
        }
        return strategies;
      }
      const strategy = Option.getOrElse(
        args.strategy,
        () => "meanReversion" as StrategyTemplateName,
      );
      const baseArgs = cliDefaultArgs();
      const merged = yield* library.buildBacktestArgsFromTemplate(
        strategy,
        baseArgs,
      );
      const config = yield* library.buildComposerConfigFromTemplate(strategy);
      yield* Console.log(`Strategy: ${strategy}`);
      yield* Console.log(JSON.stringify(merged, null, 2));
      return config;
    }).pipe(Effect.provide(makeLayer(process.env.NEURATRADE_HOME))),
).pipe(
  Command.withDescription("Strategy template library"),
  Command.withSubcommands([libraryListCommand, libraryStrategyCommand]),
);

export const walkForwardCommand = Command.make(
  "walk-forward",
  {
    ...backtestOptions,
    trainWindow: trainWindowOption,
    testWindow: testWindowOption,
    minTrades: wfMinTradesOption,
  },
  (args) =>
    Effect.gen(function* () {
      const sqlite = yield* SqliteClient;
      const repoLayer = MarketDataRepositorySQLiteLive(sqlite.database);

      const resolvedArgs = args as unknown as Omit<
        ResolvedBacktestArgs,
        "minTrades"
      >;
      const selectArgs: SelectArgs = {
        ...resolvedArgs,
        universe: "",
        top: 0,
        minRobustness: 0,
        minReturnPct: -100,
        maxDrawdownPct: 100,
        minTrades: args.minTrades,
        selectLookbackCandles: 0,
        selectBy: "return",
      };

      const result = yield* Effect.gen(function* () {
        const repo = yield* MarketDataRepository;
        const candles = yield* repo.getCandles({
          exchange: args.exchange,
          symbol: args.symbol,
          timeframe: args.timeframe,
        });
        if (candles.length === 0) {
          return yield* Effect.fail(
            new MarketDataRepositoryError(
              `No candles found for ${args.exchange}:${args.symbol}:${args.timeframe}`,
            ),
          );
        }
        return runWalkForward({
          symbol: args.symbol,
          exchange: args.exchange,
          candles,
          trainWindow: args.trainWindow,
          testWindow: args.testWindow,
          initialCapital: args.capital,
          args: selectArgs,
          selectBestForSymbol,
          runSelectBacktest,
        });
      }).pipe(Effect.provide(repoLayer));

      return result;
    }).pipe(Effect.provide(makeDbLayer(process.env.NEURATRADE_HOME))),
).pipe(Command.withDescription("Run walk-forward optimization"));

export const readinessCommand = Command.make(
  "readiness",
  { ...backtestOptions, minTradesPerMonth: minTradesPerMonthOption },
  (args) =>
    Effect.gen(function* () {
      const path = yield* Path;
      const sqlite = yield* SqliteClient;
      const repoLayer = MarketDataRepositorySQLiteLive(sqlite.database);
      const repo = new MarketDataRepositorySQLite(sqlite.database);

      const profile = yield* loadProfileIfNeeded(path.homeDir, args.profile);
      const programArgs = Option.isSome(profile)
        ? resolveBacktestArgs(
            profile.value,
            args.symbol,
            args.exchange,
            args.timeframe,
            args,
          )
        : args;

      // Readiness is a gate, not a backtest: OOS + Monte Carlo are mandatory.
      const gatedArgs = {
        ...programArgs,
        oosPct: programArgs.oosPct > 0 ? programArgs.oosPct : 20,
        mcIterations:
          programArgs.mcIterations > 0 ? programArgs.mcIterations : 200,
      };

      const result = yield* backtestProgram(gatedArgs).pipe(
        Effect.provide(repoLayer),
      );

      const candles = yield* repo.getCandles({
        exchange: args.exchange,
        symbol: args.symbol,
        timeframe: args.timeframe,
      });
      if (candles.length < 2) {
        return yield* Effect.fail(
          new Error(
            `Not enough candles for ${args.exchange}:${args.symbol}:${args.timeframe} to evaluate readiness.`,
          ),
        );
      }
      const first = candles[0].timestamp.getTime();
      const last = candles[candles.length - 1].timestamp.getTime();
      const fullMonths = Math.max(
        (last - first) / (30.44 * 24 * 60 * 60 * 1000),
        1e-9,
      );
      const inSampleMonths =
        gatedArgs.oosPct > 0
          ? fullMonths * (1 - gatedArgs.oosPct / 100)
          : fullMonths;

      const minTradesPerMonth = Option.isSome(args.minTradesPerMonth)
        ? args.minTradesPerMonth.value
        : args.timeframe === "5m"
          ? 20
          : 10;

      const report = evaluateReadiness({
        result,
        timeframe: args.timeframe,
        inSampleMonths,
        thresholds: { minTradesPerMonth },
      });
      yield* Console.log(formatReadinessReport(report));
      if (!report.ready) {
        return yield* Effect.fail(new Error("readiness gates failed"));
      }
      return report;
    }).pipe(Effect.provide(makeDbLayer(process.env.NEURATRADE_HOME))),
).pipe(
  Command.withDescription(
    "Evaluate scalping readiness gates (G1-G4) for a config; exits non-zero when any gate fails",
  ),
);

/**
 * True when a 40034 body names the probed symbol itself, e.g.
 * `Parameter BLESSUSDT does not exist`. Bitget reports the instrument as
 * the parameter value rather than a parameter *name*, so
 * `isBitgetUnsupportedInstrumentError` (which only accepts literal
 * symbol/contract/instrument parameter names) misses it. Still fails closed:
 * a message naming any other parameter (marginCoin, clientType, ...) or an
 * unrelated token is not treated as an unsupported instrument.
 */
export function probeNamesProbedSymbol(
  error: BitgetApiError,
  probedSymbol: string,
  probedProductType: BitgetProductType = "USDT-FUTURES",
): boolean {
  if (error.code !== "40034") return false;
  // Bitget uses both "Parameter X does not exist" and "Parameter X not exist".
  const token = /\bparameter\s+(\S+)\s+(?:does not|not)\s+exist\b/i.exec(
    error.body,
  )?.[1];
  if (token === undefined) return false;
  const bitgetSymbol = toBitgetFuturesSymbol(
    probedSymbol,
    probedProductType,
  ).symbol;
  return token.toUpperCase() === bitgetSymbol.toUpperCase();
}

/**
 * `scalp grid-universe scan` — per-symbol grid walk-forward universe scanner.
 *
 * Walks every stored symbol with enough candles, finds the best grid
 * parameters in-sample, and reports which symbols survive a profitability and
 * robustness gate. With --output it writes a whitelist JSON consumable by
 * `scalp paper-trade --strategy-type grid --watchlist`.
 */
export const gridUniverseScanCommand = Command.make(
  "grid-universe-scan",
  {
    exchange: gridUniverseExchangeOption,
    timeframe: gridUniverseTimeframeOption,
    minCandles: gridUniverseMinCandlesOption,
    trainWindow: gridUniverseTrainWindowOption,
    testWindow: gridUniverseTestWindowOption,
    minProfitableWindowsPct: gridUniverseMinProfitableWindowsOption,
    minAggregateReturnPct: gridUniverseMinAggregateReturnOption,
    fee: gridUniverseFeeOption,
    slippageBps: gridUniverseSlippageOption,
    trendFilterPeriod: gridUniverseTrendFilterOption,
    output: gridUniverseOutputOption,
    watch: gridUniverseWatchOption,
    interval: gridUniverseIntervalOption,
    minFillFrequencyPct: gridUniverseMinFillFrequencyOption,
    targetFillsPerDay: gridUniverseTargetFillsPerDayOption,
    accountCapital: gridUniverseAccountCapitalOption,
    tier: gridUniverseTierOption,
    market: gridUniverseMarketOption,
    dataSource: gridUniverseDataSourceOption,
  },
  (args) =>
    Effect.gen(function* () {
      if (args.tier !== "readiness" && args.tier !== "fast") {
        return yield* Effect.fail(
          new Error(
            `invalid --tier '${args.tier}': expected 'readiness' or 'fast'`,
          ),
        );
      }
      if (args.dataSource !== "gateway" && args.dataSource !== "db-mainnet") {
        return yield* Effect.fail(
          new Error(
            `invalid --data-source '${args.dataSource}': expected 'gateway' or 'db-mainnet'`,
          ),
        );
      }
      if (args.dataSource === "db-mainnet" && !args.market) {
        // The DB-sourced (non-market) scan reads candles at the scan
        // timeframe — for bybit-futures those 15m rows are TESTNET-native.
        // db-mainnet must go through the market scan so candles come from
        // the resampled 5m mainnet cache instead.
        return yield* Effect.fail(
          new Error(
            `--data-source db-mainnet requires --market (db-mainnet candles come from the 5m mainnet DB cache via the market scan)`,
          ),
        );
      }
      const path = yield* Path;
      const sqlite = yield* SqliteClient;
      const repoLayer = MarketDataRepositorySQLiteLive(sqlite.database);
      const paperRepoLayer = PaperTradingRepositorySQLiteLive(sqlite.database);

      const outputPath = Option.isSome(args.output)
        ? resolve(path.homeDir, "data", args.output.value)
        : undefined;

      // One resolved futures-market key for delete-write-read consistency:
      // scan with --exchange binance then paper-trade --futures must both
      // resolve to bitget-futures, or the watchlist lookups disagree.
      const marketExchange = resolveFuturesMarketExchange(args.exchange, true);
      if (args.minFillFrequencyPct <= 0) {
        yield* Console.warn(
          `⚠️ --min-fill-frequency-pct is 0 — the fill gate is DISABLED; survivors whose grid step is too wide to fill live will not be rejected`,
        );
      }
      const options: GridUniverseOptions = {
        exchange: args.exchange,
        timeframe: args.timeframe,
        initialCapital: 10000,
        minCandles: args.minCandles,
        trainWindow: args.trainWindow,
        testWindow: args.testWindow,
        minProfitableWindowsPct: args.minProfitableWindowsPct,
        minAggregateReturnPct: args.minAggregateReturnPct,
        minFillFrequencyPct: args.minFillFrequencyPct,
        feePct: args.fee,
        slippageBps: args.slippageBps,
        trendFilterPeriod: args.trendFilterPeriod,
        searchSpace: DEFAULT_GRID_UNIVERSE_SEARCH_SPACE,
        tier: args.tier,
        // db-mainnet evaluates on mainnet-fidelity candles; its fills are
        // modeled conservatively by default (a wick touch is not a fill).
        dataSource: args.dataSource,
        fillModel: args.dataSource === "db-mainnet" ? "conservative" : "wick",
      };

      const targetFillsPerDay = Option.isSome(args.targetFillsPerDay)
        ? args.targetFillsPerDay.value
        : accountScaledTargetFillsPerDay(args.accountCapital);

      const persistSurvivors = (result: {
        readonly entries: readonly GridUniverseEntry[];
        readonly survivors: readonly GridUniverseEntry[];
        readonly gateDropped?: number;
      }) =>
        Effect.gen(function* () {
          const paperRepo = yield* PaperTradingRepository;
          yield* paperRepo.ensureTables();
          // Readiness cohort symbols are owned by their candidate soaks;
          // the universe soak must never trade them (its fills/positions
          // carry a different manifest and trip the account kill switch).
          const cohortSymbols = new Set<string>(
            READINESS_COHORT_CANDIDATES.map((candidate) => candidate.symbol),
          );
          const survivors = result.survivors.filter(
            (entry) => !cohortSymbols.has(entry.symbol),
          );

          // Frequency-targeted selection (runs AFTER the tradeability probe
          // upstream, so only tradeable symbols are considered): rank by
          // edge/trade and take the top-K whose capped fills/day reach the
          // target, bounded by how many positions the account capital fits
          // (accountSymbolCap: max(1, floor(A × 0.5 / 10)) symbols; tiny
          // accounts get concentrated mode). Only the selected entries
          // reach the watchlist.
          const symbolCap = accountSymbolCap(args.accountCapital);
          if (symbolCap === 1) {
            yield* Console.log(
              `⚠️ Tiny account ($${args.accountCapital}): concentrated mode — portfolio capped at 1 symbol`,
            );
          }
          const selected = selectUniversePortfolio(
            survivors,
            targetFillsPerDay,
            DEFAULT_PER_SYMBOL_FILL_CAP,
            args.accountCapital,
          );
          // Stage-4 funnel summary: walk-forward survivors → gate-eligible →
          // selected. Entries keep walk-forward failures AND gate-dropped
          // survivors (flagged), so eligibility = passed && !gatedDropped.
          const eligibleCount = result.entries.filter(
            (e) => e.passed && !e.gatedDropped,
          ).length;
          const gateDroppedCount = result.gateDropped ?? 0;
          yield* Console.log(
            `🎯 Gate-scored funnel: ${eligibleCount + gateDroppedCount} walk-forward survivors → ${eligibleCount} gate-eligible (${gateDroppedCount} dropped by stage-4 gates) → ${selected.length} selected`,
          );
          const projectedFills = selected.reduce(
            (sum, e) =>
              sum + Math.min(e.fillsPerDay ?? 0, DEFAULT_PER_SYMBOL_FILL_CAP),
            0,
          );
          yield* Console.log(
            `🎯 Portfolio selection: ${selected.length}/${survivors.length} survivors selected, ~${Math.round(projectedFills)} fills/day projected (target ${targetFillsPerDay})`,
          );

          const entries: DbWatchlistEntry[] = selected.map((e) => ({
            // Persist under the resolved futures-market key so scan-write
            // and paper-trade read always agree, even when the scan was run
            // with a raw exchange name that resolves differently (e.g.
            // --exchange binance + futures => bitget-futures).
            exchange: marketExchange,
            symbol: e.symbol,
            timeframe: args.timeframe,
            returnPct: e.walkForward.aggregateReturnPct,
            profitableWindowsPct: e.walkForward.profitableWindowsPct,
            aggregateReturnPct: e.walkForward.aggregateReturnPct,
            gridStepPct: e.bestParams.gridStepPct,
            gridMaxGrids: e.bestParams.gridMaxGrids,
            gridPauseAfterLossBars: e.bestParams.gridPauseAfterLossBars,
            // Stage-4 gate-scored validation fills these; walk-forward-only
            // (DB-sourced) scans default to target 1 / no chop gate.
            targetRatio: e.validatedTargetRatio ?? 1,
            chopGateAdx: e.validatedChopGateAdx ?? 0,
            oosTrades: e.oosTrades ?? 0,
            fillsPerDay: e.fillsPerDay ?? 0,
            edgePerTradePct: e.edgePerTradePct ?? 0,
            volatility: e.volatility ?? 0,
            // ponytail: equal weight per selected symbol — simple, spreads
            // risk; upgrade to edge-proportional (edgePerTradePct / sum)
            // once the soak measures per-symbol edge live.
            allocatedWeight: selected.length > 0 ? 1 / selected.length : 0,
            updatedAt: new Date(),
          }));
          if (entries.length === 0) {
            yield* Console.log(
              `💾 No survivors selected this cycle — keeping existing watchlist unchanged`,
            );
          } else {
            yield* paperRepo.replaceWatchlist(
              marketExchange,
              args.timeframe,
              entries,
            );
            yield* Console.log(
              `💾 Watchlist replaced: ${entries.length} survivors now in DB (${marketExchange}:${args.timeframe}); prior symbols in this scope were removed`,
            );
          }

          // Only ever write the whitelist file after a successful scan with
          // survivors: a failed/empty scan must not truncate the file the
          // demo soak consumes.
          if (outputPath && survivors.length > 0) {
            const watchlistJson = survivors.map((e) => ({
              symbol: e.symbol,
              exchange: args.exchange,
              returnPct: e.walkForward.aggregateReturnPct,
              gridParams: {
                gridStepPct: e.bestParams.gridStepPct,
                gridMaxGrids: e.bestParams.gridMaxGrids,
                gridPauseAfterLossBars: e.bestParams.gridPauseAfterLossBars,
              },
            }));
            const fsys = yield* FileSystem.FileSystem;
            yield* fsys.makeDirectory(dirname(outputPath), {
              recursive: true,
            });
            yield* Effect.tryPromise({
              try: () =>
                Bun.write(outputPath, JSON.stringify(watchlistJson, null, 2)),
              catch: (err) =>
                new MarketDataRepositoryError(
                  `Failed to write grid whitelist: ${err instanceof Error ? err.message : String(err)}`,
                ),
            });
            yield* Console.log(`Whitelist written to ${outputPath}`);
          }
        });

      const runScan = () =>
        Effect.gen(function* () {
          const gateway = yield* MarketDataGateway;
          // db-mainnet: the universe already came from the mainnet 5m cache —
          // fetching the testnet contract list to filter survivors would
          // re-introduce testnet ground truth. Empty set = no filter.
          const futuresSymbols =
            args.dataSource === "db-mainnet"
              ? []
              : yield* gateway.fetchSymbols(args.exchange).pipe(
                  Effect.catch((err) =>
                    Effect.gen(function* () {
                      const reason =
                        err instanceof Error ? err.message : String(err);
                      yield* Console.warn(
                        `⚠️ futures symbol fetch failed (${reason}) — skipping the futures filter this cycle`,
                      );
                      return [] as readonly string[];
                    }),
                  ),
                );
          const futuresSet = new Set(futuresSymbols);
          const canonicalSymbol = (symbol: string) =>
            symbol.includes(":")
              ? symbol.slice(0, symbol.lastIndexOf(":"))
              : symbol;
          const isFuturesSymbol = (symbol: string) =>
            futuresSet.has(symbol) || futuresSet.has(canonicalSymbol(symbol));

          // Scan errors are NOT swallowed here: they propagate so a failed
          // scan never persists (DB watchlist kept, whitelist file untouched)
          // and the one-shot command exits non-zero.
          const rawResult = args.market
            ? yield* runMarketUniverseScan(options).pipe(
                Effect.provide(repoLayer),
              )
            : yield* runGridUniverseScan(options).pipe(
                Effect.provide(repoLayer),
              );

          let survivors =
            rawResult.survivors.length > 0 && futuresSet.size > 0
              ? rawResult.survivors.filter((e) => isFuturesSymbol(e.symbol))
              : rawResult.survivors;

          if (survivors.length > 0) {
            // Tradeability probe: Bitget's demo is a SUBSET of the live
            // contract list, so survivors must be probed against the demo
            // account. Bybit testnet has no subset — the scan's universe
            // already came from the testnet instrument list, so the probe
            // is redundant (skipped, not silently dropped).
            if (args.exchange.toLowerCase() !== "bitget-futures") {
              yield* Console.log(
                `🎯 Probe skipped: ${args.exchange} universe is sourced from its demo/tradeable instrument list`,
              );
            } else {
              const client = yield* BitgetClient;
              const tradeable: GridUniverseEntry[] = [];
              for (const entry of survivors) {
                const probe = yield* client
                  .getLeverage({
                    symbol: entry.symbol,
                    productType: "USDT-FUTURES",
                  })
                  .pipe(Effect.result);
                if (probe._tag === "Success") {
                  tradeable.push(entry);
                } else if (
                  probe._tag === "Failure" &&
                  probe.failure instanceof BitgetApiError &&
                  (isBitgetUnsupportedInstrumentError(probe.failure) ||
                    probeNamesProbedSymbol(
                      probe.failure,
                      entry.symbol,
                      "USDT-FUTURES",
                    ))
                ) {
                  // Only a probe error that proves the instrument is unsupported
                  // (40034 with a missing-symbol/contract message) drops the
                  // survivor; auth, rate-limit, transport, and other parameter
                  // defects must NOT drop survivors. Log the exact API evidence
                  // so a drop is auditable (verified 2026-08-09: the demo
                  // returns 40034 "Parameter CYSUSDT does not exist" for
                  // contracts its subset does not list).
                  const dropEvidence =
                    probe.failure instanceof BitgetApiError
                      ? `Bitget ${probe.failure.code ?? "?"}: ${probe.failure.body.slice(0, 140)}`
                      : "demo subset does not list this contract";
                  yield* Console.log(
                    `🎯 Dropped ${entry.symbol}: not tradeable on ${args.exchange} demo (${dropEvidence})`,
                  );
                } else {
                  const reason =
                    probe._tag === "Failure"
                      ? probe.failure instanceof BitgetApiError
                        ? `BitgetApiError code=${probe.failure.code ?? "-"}: ${probe.failure.body.slice(0, 140)}`
                        : probe.failure instanceof Error
                          ? probe.failure.message
                          : String(probe.failure)
                      : "unknown";
                  yield* Console.log(
                    `⚠️ Keep ${entry.symbol}: probe failed transiently (${reason})`,
                  );
                  tradeable.push(entry);
                }
              }
              if (tradeable.length < survivors.length) {
                yield* Console.log(
                  `🎯 Filtered ${survivors.length - tradeable.length} survivors not tradeable in demo`,
                );
              }
              survivors = tradeable;
            }
          }
          const result = {
            entries: rawResult.entries,
            survivors,
            gateDropped: rawResult.gateDropped ?? 0,
          };

          yield* Console.log(
            `\n🎯 Grid universe scan: ${result.entries.length} symbols, ${result.survivors.length} survivors (tier=${args.tier})`,
          );
          yield* Console.log(
            "Symbol        Candles  Step%  Grids  Pause  ProfitWin%  Aggregate%",
          );
          yield* Console.log(
            "------------------------------------------------------------------",
          );
          for (const e of result.entries) {
            const mark = e.gatedDropped ? " ✘" : e.passed ? " ✔" : "";
            yield* Console.log(
              `${e.symbol.padEnd(13)} ${String(e.candles).padStart(7)}  ` +
                `${e.bestParams.gridStepPct.toFixed(2).padStart(5)}  ` +
                `${String(e.bestParams.gridMaxGrids).padStart(5)}  ` +
                `${String(e.bestParams.gridPauseAfterLossBars).padStart(5)}  ` +
                `${e.walkForward.profitableWindowsPct.toFixed(0).padStart(6)}%  ` +
                `${e.walkForward.aggregateReturnPct.toFixed(2).padStart(9)}%${mark}`,
            );
          }

          yield* persistSurvivors(result).pipe(Effect.provide(paperRepoLayer));
          return result.survivors;
        });

      const runScanWithLayers = () =>
        runScan().pipe(Effect.provide(MarketDataGatewayLive));

      if (args.watch) {
        // The watch loop must survive transient scan failures: log the cycle
        // error and continue; the DB watchlist and whitelist file are left
        // untouched on failure.
        const watchCycle = runScanWithLayers().pipe(
          Effect.catch((err) =>
            Effect.gen(function* () {
              yield* Console.error(
                `grid-universe scan cycle failed: ${
                  err instanceof Error ? err.message : String(err)
                }`,
              );
              return [] as readonly GridUniverseEntry[];
            }),
          ),
        );
        yield* Console.log(
          `👁 Watching universe ${args.exchange}:${args.timeframe} (tier=${args.tier}), re-scan every ${args.interval}s...`,
        );
        // Repeat until interrupted: Effect.repeat + Schedule.spaced runs the
        // scan, spaces iterations by the interval, and cancels cleanly on
        // SIGTERM/SIGINT (BunRuntime.runMain interrupts the fiber at the
        // schedule boundary).
        yield* Effect.repeat(watchCycle, {
          schedule: Schedule.spaced(`${args.interval} seconds`),
        });
      }

      // One-shot mode: failures propagate (non-zero exit, nothing persisted).
      return yield* runScanWithLayers();
    }).pipe(Effect.provide(makeDbLayer(process.env.NEURATRADE_HOME))),
).pipe(
  Command.withDescription(
    "Per-symbol grid walk-forward scan; finds profitable grid candidates across the stored universe",
  ),
);

export const watchlistListCommand = Command.make(
  "list",
  {
    exchange: watchlistListExchangeOption,
    timeframe: watchlistListTimeframeOption,
  },
  (args) =>
    Effect.gen(function* () {
      const sqlite = yield* SqliteClient;
      const paperRepoLayer = PaperTradingRepositorySQLiteLive(sqlite.database);
      const entries = yield* PaperTradingRepository.pipe(
        Effect.flatMap((repo) =>
          Effect.gen(function* () {
            yield* repo.ensureTables();
            return yield* repo.listWatchlist(args.exchange, args.timeframe);
          }),
        ),
        Effect.provide(paperRepoLayer),
      );
      yield* Console.log(
        `\n📋 Watchlist ${args.exchange}:${args.timeframe} (${entries.length} symbols)`,
      );
      yield* Console.log(
        "Symbol        Return%   ProfitWin%  Step%  Grids  Pause  Updated",
      );
      yield* Console.log(
        "------------------------------------------------------------------",
      );
      for (const e of entries) {
        yield* Console.log(
          `${e.symbol.padEnd(13)} ${e.aggregateReturnPct.toFixed(2).padStart(8)}%  ` +
            `${e.profitableWindowsPct.toFixed(0).padStart(8)}%  ` +
            `${e.gridStepPct.toFixed(2).padStart(5)}  ` +
            `${String(e.gridMaxGrids).padStart(5)}  ` +
            `${String(e.gridPauseAfterLossBars).padStart(5)}  ` +
            `  ${e.updatedAt.toISOString().slice(0, 16)}`,
        );
      }
      return entries;
    }).pipe(Effect.provide(makeDbLayer(process.env.NEURATRADE_HOME))),
).pipe(
  Command.withDescription(
    "List the DB-backed watchlist for an exchange/timeframe",
  ),
);

export const watchlistCommand = Command.make("watchlist", {}, () =>
  Console.log(
    "Watchlist commands. Use 'watchlist list --exchange <ex> --timeframe <tf>'.",
  ),
).pipe(
  Command.withDescription("DB-backed watchlist management"),
  Command.withSubcommands([watchlistListCommand]),
);
export const demoReadinessCommand = makeDemoReadinessCommand(
  makeDbLayer(process.env.NEURATRADE_HOME),
);

export const parityReplayCommand = makeParityReplayCommand(
  process.env.NEURATRADE_HOME,
);

// ---------------------------------------------------------------------------
// Flow Ignition (flow-v1): backtest + universe
// ---------------------------------------------------------------------------

/** Wire symbol → canonical candle form: "BTCUSDT" → "BTC/USDT". */
function wireToCanonicalSymbol(symbol: string): string {
  return symbol.endsWith("USDT") && !symbol.includes("/")
    ? `${symbol.slice(0, -4)}/${symbol.slice(-4)}`
    : symbol;
}

function signedPct(value: number): string {
  return `${value >= 0 ? "+" : ""}${value.toFixed(3)}%`;
}

function formatFlowBacktestReport(report: FlowBacktestReport): string {
  const lines: string[] = [];
  lines.push("Flow-v1 backtest report");
  lines.push(
    `  windows (train ${report.options.trainDays}d / test ${report.options.testDays}d / steps ${report.windows.length}):`,
  );
  for (const w of report.windows) {
    lines.push(
      `    #${w.index} test ${new Date(w.testStart).toISOString().slice(0, 16)}..${new Date(w.testEnd).toISOString().slice(0, 16)}: ${w.signals} signals (${w.purged} purged at boundary)`,
    );
  }
  lines.push(
    "  hold-time | trades | win %  | avg edge/trade | max DD  | expectancy",
  );
  for (const h of report.byHoldTime) {
    lines.push(
      `  ${String(h.holdTimeHours).padStart(4)}h    | ${String(h.totalTrades).padStart(6)} | ${(h.winRate * 100).toFixed(1).padStart(5)}% | ${signedPct(h.avgEdgePerTradePct).padStart(15)} | ${h.maxDrawdownPct.toFixed(2).padStart(6)}% | ${signedPct(h.expectancyPct).padStart(9)} | BE ${(h.breakevenWinRate * 100).toFixed(1)}% ${h.passesHonestyGates ? "PASS" : "REJECT"}`,
    );
  }
  const p = report.portfolio;
  lines.push(
    `  portfolio (hold ${p.holdTimeHours}h): ${p.totalTrades} trades, ${(p.winRate * 100).toFixed(1)}% win, ${signedPct(p.avgEdgePerTradePct)} avg edge/trade, ${p.maxDrawdownPct.toFixed(2)}% max DD, expectancy ${signedPct(p.expectancyPct)}`,
  );
  lines.push("  per-symbol:");
  for (const s of p.bySymbol) {
    lines.push(
      `    ${s.symbol.padEnd(12)} ${String(s.trades).padStart(4)} trades  ${(s.winRate * 100).toFixed(1).padStart(5)}% win  ${signedPct(s.avgEdgePct)} avg edge`,
    );
  }
  return lines.join("\n");
}

function formatFlowUniverse(entries: readonly FlowUniverseEntry[]): string {
  const lines: string[] = [];
  lines.push("rank  symbol       turnover24h(USDT)  spreadBps  ageDays");
  for (const e of entries) {
    lines.push(
      `${String(e.rank).padStart(4)}  ${e.symbol.padEnd(12)} ${Math.round(e.turnover24h).toLocaleString("en-US").padStart(17)} ${String(e.spreadBps).padStart(9)} ${e.ageDays.toFixed(0).padStart(7)}`,
    );
  }
  return lines.join("\n");
}

export const flowBacktestCommand = Command.make(
  "flow-backtest",
  {
    symbols: flowSymbolsOption,
    start: flowStartOption,
    end: flowEndOption,
    timeframe: flowTimeframeOption,
    threshold: flowThresholdOption,
    holdTimes: flowHoldTimesOption,
    fee: flowFeeOption,
    spreadBps: flowSpreadBpsOption,
    conservativeFillRate: flowConservativeFillRateOption,
    maxBreakevenWinRate: flowMaxBreakevenWinRateOption,
    zMode: Options.text("z-mode").pipe(
      Options.withDefault("per-symbol"),
      Options.withDescription(
        "z-score normalization: per-symbol (rolling) or cross-sectional (across the universe at each boundary)",
      ),
    ),
    stopMult: Options.float("stop-mult").pipe(
      Options.withDefault(defaultFlowBacktestOptions.stopMultiplier ?? 0),
      Options.withDescription(
        "ATR stop multiplier; 0 disables the ATR stop (pure time/OFI exits)",
      ),
    ),
  },
  (args) =>
    Effect.gen(function* () {
      const sqlite = yield* SqliteClient;
      const symbols = args.symbols
        .split(",")
        .map((s) => s.trim())
        .filter((s) => s.length > 0);
      const holdTimes = args.holdTimes
        .split(",")
        .map((s) => Number.parseFloat(s.trim()))
        .filter((n) => Number.isFinite(n) && n > 0);
      if (args.zMode !== "per-symbol" && args.zMode !== "cross-sectional") {
        return yield* Effect.fail(
          new MarketDataRepositoryError(
            `Invalid --z-mode '${args.zMode}': expected per-symbol or cross-sectional`,
          ),
        );
      }
      if (holdTimes.length === 0) {
        return yield* Effect.fail(
          new MarketDataRepositoryError(
            `Invalid --hold-times '${args.holdTimes}': expected comma-separated hours > 0`,
          ),
        );
      }
      const end = args.end.length > 0 ? new Date(args.end) : new Date();
      const start =
        args.start.length > 0
          ? new Date(args.start)
          : new Date(end.getTime() - 180 * 86_400_000);

      const options: FlowBacktestOptions = {
        fees: {
          taker: args.fee / 100,
          maker: defaultFlowBacktestOptions.fees.maker,
        },
        spreadBps: args.spreadBps,
        thresholds: {
          ...defaultFlowBacktestOptions.thresholds,
          entry: args.threshold,
        },
        holdTimes,
        trainDays: defaultFlowBacktestOptions.trainDays,
        testDays: defaultFlowBacktestOptions.testDays,
        walkForwardSteps: defaultFlowBacktestOptions.walkForwardSteps,
        zMode: args.zMode,
        stopMultiplier: args.stopMult > 0 ? args.stopMult : null,
        conservativeFillRate: Math.max(
          0,
          Math.min(1, args.conservativeFillRate),
        ),
        maxBreakevenWinRate: args.maxBreakevenWinRate,
      };

      const series: FlowSymbolSeries[] = [];
      let totalCandles = 0;
      // Flow tables are created by the flow data layer at runtime; guard so
      // a not-yet-fetched universe degrades to empty series, not a crash.
      const hasOiTable =
        (yield* sqlite.queryOne<{ name: string }>(
          "SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'open_interest_history'",
        )) !== null;
      const hasFundingTable =
        (yield* sqlite.queryOne<{ name: string }>(
          "SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'funding_rates'",
        )) !== null;
      for (const symbol of symbols) {
        const canonical = wireToCanonicalSymbol(symbol);
        // OI/funding rows are stored under multiple canonical forms
        // (e.g. "BTC/USDT" and "BTC/USDT:USDT") depending on which recorder
        // wrote them; the raw wire symbol ("BTCUSDT") never matches. Query
        // all variants so the flow honesty gates see real OI/funding data.
        const symbolVariants = [
          symbol,
          canonical,
          canonical.endsWith(":USDT") ? canonical : `${canonical}:USDT`,
        ];
        const variantPlaceholders = symbolVariants
          .map(() => "?")
          .join(",");
        const candleRows = yield* sqlite.queryAll<{
          open: number;
          high: number;
          low: number;
          close: number;
          volume: number;
          timestamp: string;
        }>(
          `SELECT c.open_price AS open, c.high_price AS high, c.low_price AS low,
                  c.close_price AS close, c.volume, c.timestamp
           FROM ohlcv_data c
           JOIN trading_pairs tp ON tp.id = c.trading_pair_id
           WHERE tp.symbol IN (${variantPlaceholders}) AND c.timeframe = ?
             AND c.timestamp >= ? AND c.timestamp <= ?
           ORDER BY c.timestamp ASC`,
          [
            ...symbolVariants,
            args.timeframe,
            start.toISOString(),
            end.toISOString(),
          ],
        );
        const oiRows = hasOiTable
          ? yield* sqlite.queryAll<{
              ts: number;
              oi: number;
              oiValue: number | null;
            }>(
              `SELECT ts, oi, oi_value AS oiValue FROM open_interest_history
               WHERE exchange IN ('bybit','bybit-futures') AND symbol IN (${variantPlaceholders}) AND ts BETWEEN ? AND ?
               ORDER BY ts ASC`,
              [...symbolVariants, start.getTime(), end.getTime()],
            )
          : [];
        const fundingRows = hasFundingTable
          ? yield* sqlite.queryAll<{
              fundingRate: number;
              timestamp: string;
            }>(
              `SELECT funding_rate AS fundingRate, timestamp FROM funding_rates
               WHERE exchange IN ('bybit','bybit-futures') AND symbol IN (${variantPlaceholders}) AND timestamp >= ? AND timestamp <= ?
               ORDER BY timestamp ASC`,
              [...symbolVariants, start.toISOString(), end.toISOString()],
            )
          : [];
        totalCandles += candleRows.length;
        series.push({
          symbol,
          exchange: "bybit",
          timeframe: args.timeframe,
          candles: candleRows.map((r) => ({
            open: r.open,
            high: r.high,
            low: r.low,
            close: r.close,
            volume: r.volume,
            timestamp: new Date(r.timestamp),
          })),
          oi: oiRows.map((r) => ({
            ts: r.ts,
            oi: r.oi,
            oiValue: r.oiValue ?? undefined,
          })),
          funding: fundingRows.map((r) => ({
            ts: new Date(r.timestamp).getTime(),
            fundingRate: r.fundingRate,
          })),
        });
      }
      if (totalCandles === 0) {
        return yield* Effect.fail(
          new MarketDataRepositoryError(
            `No candles found for ${symbols.join(",")} at ${args.timeframe} between ${start.toISOString()} and ${end.toISOString()} (exchange=bybit). Run the flow data fetch first.`,
          ),
        );
      }
      const data: FlowBacktestData = { series, options };
      return runFlowBacktest(data);
    }).pipe(
      Effect.tap((report) => Console.log(formatFlowBacktestReport(report))),
      Effect.provide(makeDbLayer(process.env.NEURATRADE_HOME)),
    ),
).pipe(
  Command.withDescription(
    "Run the flow-v1 walk-forward backtest on DB candles/OI/funding",
  ),
);

export const flowUniverseCommand = Command.make(
  "flow-universe",
  {
    limit: flowLimitOption,
    minTurnover: flowMinTurnoverOption,
    dataSource: flowUniverseDataSourceOption,
  },
  (args) =>
    Effect.gen(function* () {
      const sqlite = yield* SqliteClient;
      let volumes: Readonly<Record<string, number>>;
      let instruments: readonly FlowInstrument[];
      if (args.dataSource === "db-mainnet") {
        const rows = yield* sqlite.queryAll<{
          symbol: string;
          turnover24h: number;
          firstTs: string;
        }>(
          `WITH first_seen AS (
             SELECT trading_pair_id, exchange_id, timeframe, MIN(timestamp) AS firstTs
             FROM ohlcv_data
             GROUP BY trading_pair_id, exchange_id, timeframe
           )
           SELECT REPLACE(REPLACE(tp.symbol, '/USDT:USDT', 'USDT'), '/USDT', 'USDT') AS symbol,
                  SUM(c.close_price * c.volume) AS turnover24h,
                  fs.firstTs AS firstTs
           FROM ohlcv_data c
           JOIN trading_pairs tp ON tp.id = c.trading_pair_id
           JOIN exchanges e ON e.id = c.exchange_id
           JOIN first_seen fs ON fs.trading_pair_id = c.trading_pair_id
             AND fs.exchange_id = c.exchange_id
             AND fs.timeframe = c.timeframe
           WHERE e.name IN ('bybit','bybit-futures')
             AND c.timestamp >= datetime('now', '-1 day')
           GROUP BY tp.symbol, fs.firstTs`,
        );
        volumes = Object.fromEntries(
          rows.map((r) => [r.symbol, r.turnover24h]),
        );
        instruments = rows.map((r) => ({
          symbol: r.symbol,
          status: "Trading",
          listedTime: new Date(r.firstTs).getTime(),
        }));
      } else {
        // Mainnet Bybit public market data (no auth needed).
        const baseUrl = "https://api.bybit.com";
        volumes = yield* fetch24hrVolumes(baseUrl);
        instruments = yield* fetchInstruments(baseUrl);
      }
      const ranked = selectFlowUniverse(volumes, instruments, undefined, {
        topN: args.limit,
      });
      return args.minTurnover > 0
        ? ranked.filter((e) => e.turnover24h >= args.minTurnover)
        : ranked;
    }).pipe(
      Effect.tap((entries) => Console.log(formatFlowUniverse(entries))),
      Effect.catch((err) =>
        Effect.gen(function* () {
          const msg =
            err instanceof Error
              ? err.message
              : ((err as { reason?: string }).reason ?? String(err));
          yield* Console.error(`flow-universe failed: ${msg}`);
          return [] as readonly FlowUniverseEntry[];
        }),
      ),
      Effect.provide(makeDbLayer(process.env.NEURATRADE_HOME)),
    ),
).pipe(
  Command.withDescription(
    "Rank the liquid USDT-perp universe by 24h turnover (mainnet Bybit)",
  ),
);

export const flowRecordCommand = Command.make(
  "flow-record",
  {
    symbols: Options.text("symbols").pipe(
      Options.withDefault(""),
      Options.withDescription(
        "Comma-separated Bybit linear symbols (default: flow universe top-40 or fallback set)",
      ),
    ),
    duration: Options.integer("duration").pipe(
      Options.withDefault(0),
      Options.withDescription(
        "Minutes to record before exiting (0 = until Ctrl-C)",
      ),
    ),
  },
  (args) =>
    Effect.gen(function* () {
      const sqlite = yield* SqliteClient;
      const paperRepo = new PaperTradingRepositorySQLite(sqlite.database);
      const flowRepo: FlowRecorderRepository = paperRepo;

      const symbols = yield* Effect.promise(() =>
        resolveFlowSymbols(
          args.symbols.length > 0
            ? args.symbols
                .split(",")
                .map((s) => s.trim())
                .filter((s) => s.length > 0)
            : undefined,
        ),
      );

      yield* Console.log(
        `Recording live flow (trades/liquidations) for ${symbols.length} symbols ` +
          "from Bybit mainnet public WS; Ctrl-C to stop.",
      );

      const record = runFlowRecorder(flowRepo, {
        symbols,
        onFlush: (rows) => {
          for (const row of rows) {
            console.log(
              `[flow] ${new Date(row.ts).toISOString()} ${row.symbol} ` +
                `buy=${row.buyVol} sell=${row.sellVol} trades=${row.trades}`,
            );
          }
        },
        onAggregate: (rows, prices) => {
          for (const row of rows) {
            const last = prices.get(row.symbol);
            console.log(
              `[flow] ${new Date(row.ts).toISOString()} ${row.symbol} ` +
                `buy=${row.buyVol} sell=${row.sellVol} trades=${row.trades}` +
                (last !== undefined ? ` last=${last}` : ""),
            );
          }
        },
        onWarn: (message) => console.warn(`[flow] ${message}`),
      });

      if (args.duration > 0) {
        // Whichever finishes first wins; the loser is interrupted, which runs
        // the recorder's close finalizer (flush + clean WS shutdown).
        yield* Effect.race(
          record,
          Effect.sleep(Duration.minutes(args.duration)),
        );
      } else {
        yield* record;
      }
    }).pipe(Effect.provide(makeDbLayer(process.env.NEURATRADE_HOME))),
).pipe(
  Command.withDescription(
    "Record live Bybit order-flow (trades -> 1m OFI, liquidations) into the DB",
  ),
);

export interface FlowTradeArgs {
  readonly exchange: string;
  readonly symbol: string;
  readonly timeframe: "5m" | "1m";
  readonly capital: number;
  readonly maxPositionSizePct: Option.Option<number>;
  readonly leverage: number;
  readonly interval: number;
  readonly iterations: number;
  readonly threshold: number;
  readonly holdMinutes: number;
  readonly minCapital: Option.Option<number>;
  readonly maxDrawdownPct: Option.Option<number>;
  readonly maxDailyLossPct: Option.Option<number>;
  readonly marginMode: string;
  readonly productType: string;
  readonly live: boolean;
  readonly killSwitch: boolean;
  readonly disengage: boolean;
}

/**
 * Flow Ignition live trade engine — testnet execution validation of the
 * flow-v1 signal. Signals are computed from MAINNET data in the local DB (the
 * flow recorder / fetch), orders go through the exchange adapter (bybit
 * testnet creds with --live). Mirrors paper-trade's risk wiring.
 */
export const flowTradeCommand = Command.make(
  "flow-trade",
  {
    exchange: flowTradeExchangeOption,
    symbol: flowTradeSymbolOption,
    timeframe: flowTimeframeOption,
    capital: capitalOption,
    maxPositionSizePct: maxPositionSizeOption,
    leverage: leverageOption,
    interval: intervalOption,
    iterations: iterationsOption,
    threshold: flowThresholdOption,
    holdMinutes: flowHoldMinutesOption,
    minCapital: minCapitalOption,
    maxDrawdownPct: maxDrawdownOption,
    maxDailyLossPct: maxDailyLossOption,
    marginMode: marginModeOption,
    productType: productTypeOption,
    live: liveOption,
    killSwitch: killSwitchOption,
    disengage: disengageOption,
  },
  (args) =>
    Effect.gen(function* () {
      const path = yield* Path;
      const sqlite = yield* SqliteClient;
      const db = sqlite.database;

      const repoLayer = MarketDataRepositorySQLiteLive(db);
      const paperRepoLayer = PaperTradingRepositorySQLiteLive(db);

      const riskOverrides: MutablePartialRiskLimits = {};
      if (Option.isSome(args.maxDrawdownPct))
        riskOverrides.maxDrawdownPct = args.maxDrawdownPct.value;
      if (Option.isSome(args.maxDailyLossPct))
        riskOverrides.maxDailyLossPct = args.maxDailyLossPct.value;
      if (Option.isSome(args.minCapital))
        riskOverrides.minCapital = args.minCapital.value;
      const riskGuardLayer = RiskGuardLive(args.live, riskOverrides);
      const killSwitchLayer = KillSwitchSQLiteLive(db);
      const circuitBreakerMaxLoss = Option.getOrElse(
        args.maxDailyLossPct,
        () => 2,
      );
      const circuitBreakerLayer = CircuitBreakerSQLiteLive(
        db,
        circuitBreakerMaxLoss,
      );
      // Signals ALWAYS come from the mainnet data in the DB (the proposal's
      // split: mainnet research, testnet execution); the live gateway is only
      // used by the bybit adapter for reference ticks, which the engine
      // avoids by passing its own reference price.
      const marketDataLayer = Layer.provide(
        MarketDataGatewayRepositoryLive,
        repoLayer,
      );
      const futuresAdapterLayer = (
        args.live
          ? BybitFuturesExchangeAdapterLive.pipe(
              Layer.provide(BybitClientLiveConfig),
              Layer.provide(BybitConfigLive),
            )
          : SimulatedFuturesExchangeAdapterLive()
      ) as Layer.Layer<
        FuturesExchangeAdapterService,
        never,
        MarketDataGatewayService
      >;
      const layers = Layer.mergeAll(
        BunServices.layer,
        PathLive(process.env.NEURATRADE_HOME),
        marketDataLayer,
        repoLayer,
        paperRepoLayer,
        riskGuardLayer,
        killSwitchLayer,
        circuitBreakerLayer,
      );

      if (args.killSwitch) {
        yield* Effect.provide(
          KillSwitch.pipe(
            Effect.flatMap((ks) => ks.engage("CLI --kill-switch")),
          ),
          killSwitchLayer,
        );
      }
      if (args.disengage) {
        yield* Effect.provide(
          KillSwitch.pipe(Effect.flatMap((ks) => ks.disengage())),
          killSwitchLayer,
        );
      }

      return yield* flowTradeProgram(args).pipe(
        Effect.provide(futuresAdapterLayer),
        Effect.provide(layers),
        Effect.tapError((err) =>
          Console.error(
            `flow-trade failed: ${"reason" in err ? err.reason : String(err)}`,
          ),
        ),
      );
    }).pipe(Effect.provide(makeDbLayer(process.env.NEURATRADE_HOME))),
).pipe(
  Command.withDescription(
    "Run the flow-v1 live trade engine (testnet execution, mainnet DB signals)",
  ),
);

function flowTradeProgram(args: FlowTradeArgs) {
  return Effect.gen(function* () {
    const repo = yield* PaperTradingRepository;
    const gateway = yield* MarketDataGateway;
    const adapter = yield* FuturesExchangeAdapter;
    const riskGuard = yield* RiskGuard;
    const killSwitch = yield* KillSwitch;
    const circuitBreaker = yield* CircuitBreaker;
    yield* repo.ensureTables();

    const productType = parseProductType(args.productType);
    const marginMode = parseMarginMode(args.marginMode);
    const opts: FlowTradeOptions = {
      exchange: args.exchange,
      symbol: args.symbol,
      timeframe: args.timeframe,
      capital: args.capital,
      maxPositionSizePct: Option.getOrElse(args.maxPositionSizePct, () => 10),
      leverage: args.leverage,
      productType,
      marginMode,
      threshold: args.threshold,
      holdMinutes: args.holdMinutes,
      isLive: args.live,
    };

    const runIteration = (): Effect.Effect<
      FlowTradeIterationResult,
      FlowTradeError,
      never
    > =>
      iterateFlowTrade(
        repo,
        gateway,
        adapter,
        riskGuard,
        killSwitch,
        circuitBreaker,
        opts,
      ).pipe(
        Effect.catch((err) =>
          Effect.gen(function* () {
            const tag =
              "tag" in err && typeof err.tag === "string" ? err.tag : "";
            // Safety-critical errors must propagate so the loop stops and the
            // process exits for the operator; only transient network/IO
            // errors are safe to skip and retry on the next cadence.
            if (
              tag === "RiskError" ||
              tag === "KillSwitchError" ||
              tag === "CircuitBreakerError"
            ) {
              return yield* Effect.fail(err);
            }
            const current = yield* repo
              .getFlowTradeState(args.exchange, args.symbol)
              .pipe(Effect.orElseSucceed(() => null));
            const reason =
              "reason" in err && typeof err.reason === "string"
                ? err.reason
                : err instanceof Error
                  ? err.message
                  : String(err);
            yield* Console.error(
              `flow-trade iteration skipped (network/IO error): ${reason}`,
            );
            return {
              action: "hold" as const,
              side: current?.side ?? null,
              state: current ?? freshFlowTradeState(opts, Date.now()),
              note: `skip: ${reason}`,
            };
          }),
        ),
      );

    let remaining = args.iterations;
    let last: FlowTradeIterationResult | null = null;
    // iterations=0 means run forever.
    while (args.iterations === 0 || remaining !== 0) {
      const result = yield* runIteration();
      last = result;
      yield* Console.log(`[flow-trade] ${result.note}`);

      if (remaining > 0) {
        remaining -= 1;
      }

      // Sleep between iterations: always in infinite mode (0), otherwise only
      // when more iterations remain.
      if (args.iterations === 0 || remaining !== 0) {
        yield* Effect.sleep(`${args.interval} seconds`);
      }
    }
    return last;
  });
}

export const scalpCommand = Command.make("scalp", {}, () =>
  Console.log(
    "Scalping commands. Use 'scalp backtest|optimize|scan|paper-trade|soak|profile|readiness|demo-readiness|parity-replay|flow-backtest|flow-universe|flow-record|flow-trade --help' for details.",
  ),
).pipe(
  Command.withDescription("Deterministic scalping operations"),
  Command.withSubcommands([
    backtestCommand,
    optimizeCommand,
    scanCommand,
    paperTradeCommand,
    soakCommand,
    profileCommand,
    libraryCommand,
    walkForwardCommand,
    readinessCommand,
    demoReadinessCommand,
    parityReplayCommand,
    gridUniverseScanCommand,
    watchlistCommand,
    flowBacktestCommand,
    flowUniverseCommand,
    flowRecordCommand,
    flowTradeCommand,
  ]),
);
