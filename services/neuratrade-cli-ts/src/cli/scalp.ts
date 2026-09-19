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
  type MarketDataRepositoryService,
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
  type BacktestEngineImpl,
} from "../scalping/services.js";
import { MarketDataGatewayLive } from "../market-data/gateways/index.js";
import {
  MarketDataGateway,
  type MarketDataGatewayService,
} from "../market-data/gateway.js";
import { MarketDataGatewayRepositoryLive } from "../market-data/gateway-repository.js";
import type { FundingRate } from "../market-data/types.js";
import { SimulatedExchangeAdapterLive } from "../exchange/adapters/simulated.js";
import { BinanceLiveExchangeAdapterLive } from "../exchange/adapters/binance-live.js";
import { SimulatedFuturesExchangeAdapterLive } from "../exchange/adapters/simulated-futures.js";
import { BitgetFuturesExchangeAdapterLive } from "../exchange/adapters/bitget-futures.js";
import {
  BybitFuturesExchangeAdapterLive,
  BybitClient,
  BybitClientLiveConfig,
  toBybitSymbol,
  type BybitContract,
} from "../exchange/adapters/bybit-futures.js";
import {
  BitgetClientLiveConfig,
  BitgetClient,
  BitgetApiError,
  isBitgetUnsupportedInstrumentError,
  toBitgetFuturesSymbol,
  type BitgetClientError,
  type BitgetContract,
} from "../services/bitget-client.js";
import { BitgetConfig, BitgetConfigLive } from "../services/bitget-config.js";
import { BybitConfig, BybitConfigLive } from "../services/bybit-config.js";
import { RateLimiterLive } from "../services/rate-limiter.js";
import type { FuturesMarginMode } from "../exchange/futures-adapter.js";
import {
  FuturesExchangeAdapter,
  type FuturesExchangeAdapterService,
} from "../exchange/futures-adapter.js";
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
  runLadderPaperTradingIteration,
  setLadderTradeProvenance,
  type LadderPaperIterationResult,
  type LadderPaperTradingOptions,
} from "../paper-trading/ladder-engine.js";
import {
  allocateLadderPortfolioCapital,
  planLadderPortfolioRebalance,
  type LadderRebalancePlan,
  summarizeLadderPortfolio,
} from "../paper-trading/ladder-portfolio.js";
import { bybitSnapshotCommand } from "./bybit-snapshot.js";
import {
  DEFAULT_STRATEGY_MANIFEST,
  fingerprintStrategyManifest,
  type StrategyManifest,
} from "../scalping/real-money-readiness.js";
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
  type PaperTradingRepositoryError,
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
  type ValidatedGridCandidate,
} from "../scalping/grid-candidate.js";
import {
  runGridUniverseScan,
  runMarketUniverseScan,
  type GridUniverseEntry,
  type GridUniverseOptions,
  DEFAULT_GRID_UNIVERSE_SEARCH_SPACE,
  LADDER_GATE_TAIL,
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
  fetchTickers,
  fetchInstruments,
  resolveBybitBaseUrl,
  resolveBybitSignalFeed,
  describeBybitSignalFeed,
} from "../market-data/gateways/bybit.js";
import { makeDemoReadinessCommand } from "./demo-readiness.js";
import { makeParityReplayCommand } from "./parity-replay.js";
import { makeTimesFmForecastCommand } from "./timesfm.js";
import { tradeCommand } from "./trade.js";
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
  maxHoldBarsOption,
  configMismatchActionOption,
  signalFeedOption,
  maxPositionDrawdownPctOption,
  stopRatioOption,
  liveEntryCrossBpsOption,
  demoLiveMarketEntriesOption,
  takerExitFeePctOption,
  fundingRatePct8hOption,
  maintenanceMarginRatePctOption,
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
  shadowOption,
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
  gridUniverseEngineOption,
  gridUniverseRungsOption,
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
            const msg =
              err instanceof Error
                ? err.message
                : String((err as { readonly reason?: unknown }).reason ?? err);
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

/**
 * True when every strategy-tuning flag matches the default composer — the
 * caller can reuse `defaultComposerConfig` verbatim instead of rebuilding it.
 */
function isDefaultBacktestComposerFlags(
  priceOnly: boolean,
  noRsi: boolean,
  noTrend: boolean,
  regimeMode: "trend" | "reversion" | "breakout",
  volumeMinRatio: number,
  minConfluence: number,
  entryCandleConfirm: boolean,
  momentumConfirmBars: number,
  adxMin: number,
  breakoutLookback: number,
  fundingBiasThreshold: number | undefined,
  useFunding: boolean | undefined,
): boolean {
  return (
    isDefaultSignalSelection(priceOnly, noRsi, noTrend) &&
    isDefaultRegimeThresholds(regimeMode, volumeMinRatio, minConfluence) &&
    isDefaultConfirmationGates(
      entryCandleConfirm,
      momentumConfirmBars,
      adxMin,
      breakoutLookback,
    ) &&
    isDefaultFundingGates(fundingBiasThreshold, useFunding)
  );
}

function isDefaultSignalSelection(
  priceOnly: boolean,
  noRsi: boolean,
  noTrend: boolean,
): boolean {
  return !priceOnly && !noRsi && !noTrend;
}

function isDefaultRegimeThresholds(
  regimeMode: "trend" | "reversion" | "breakout",
  volumeMinRatio: number,
  minConfluence: number,
): boolean {
  return (
    regimeMode === defaultComposerConfig.thresholds.regimeMode &&
    volumeMinRatio <= 0 &&
    minConfluence <= 0
  );
}

function isDefaultConfirmationGates(
  entryCandleConfirm: boolean,
  momentumConfirmBars: number,
  adxMin: number,
  breakoutLookback: number,
): boolean {
  return (
    !entryCandleConfirm &&
    momentumConfirmBars <= 0 &&
    adxMin <= 0 &&
    (breakoutLookback <= 0 ||
      breakoutLookback === defaultComposerConfig.thresholds.breakoutLookback)
  );
}

function isDefaultFundingGates(
  fundingBiasThreshold: number | undefined,
  useFunding: boolean | undefined,
): boolean {
  return fundingBiasThreshold === undefined && useFunding === undefined;
}

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
    isDefaultBacktestComposerFlags(
      priceOnly,
      noRsi,
      noTrend,
      regimeMode,
      volumeMinRatio,
      minConfluence,
      entryCandleConfirm,
      momentumConfirmBars,
      adxMin,
      breakoutLookback,
      fundingBiasThreshold,
      useFunding,
    )
  ) {
    return defaultComposerConfig;
  }

  const normalized = normalizeComposerWeights(
    disabledSignalWeights(priceOnly, noRsi, noTrend),
  );
  if (normalized === null) return defaultComposerConfig;
  return {
    weights: normalized,
    thresholds: overrideComposerThresholds(
      regimeMode,
      volumeMinRatio,
      volumeLookback,
      minConfluence,
      entryCandleConfirm,
      momentumConfirmBars,
      adxMin,
      breakoutLookback,
      fundingBiasThreshold,
      useFunding,
    ),
  } satisfies ComposerConfig;
}

function disabledSignalWeights(
  priceOnly: boolean,
  noRsi: boolean,
  noTrend: boolean,
): ComposerConfig["weights"] {
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
  return weights;
}

function normalizeComposerWeights(
  weights: ComposerConfig["weights"],
): ComposerConfig["weights"] | null {
  const activeSum = Object.values(weights).reduce((a, b) => a + b, 0);
  if (activeSum <= 0) return null;
  return {
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
}

function overrideComposerThresholds(
  regimeMode: "trend" | "reversion" | "breakout",
  volumeMinRatio: number,
  volumeLookback: number,
  minConfluence: number,
  entryCandleConfirm: boolean,
  momentumConfirmBars: number,
  adxMin: number,
  breakoutLookback: number,
  fundingBiasThreshold: number | undefined,
  useFunding: boolean | undefined,
): ComposerConfig["thresholds"] {
  const thresholds = {
    ...defaultComposerConfig.thresholds,
    regimeMode,
    volumeMinRatio,
    volumeLookback,
    minConfluence,
    entryCandleConfirm,
    momentumConfirmBars,
    adxMin,
  };
  if (breakoutLookback > 0) thresholds.breakoutLookback = breakoutLookback;
  if (fundingBiasThreshold !== undefined)
    thresholds.fundingBiasThreshold = fundingBiasThreshold;
  if (useFunding !== undefined) thresholds.useFunding = useFunding;
  return thresholds;
}

/**
 * Derive the inclusive candle range for a backtest from either the raw CLI
 * `--start`/`--end` options or profile-set `startDate`/`endDate`. Empty values
 * leave the bound open (earliest/latest available candle). An inverted or
 * equal (zero-width) range is rejected.
 */
function resolveBacktestCandleRange(
  args: ResolvedBacktestArgs,
):
  | { ok: true; range: { from?: Date; to?: Date } }
  | { ok: false; error: string } {
  const start = args.start ?? args.startDate;
  const end = args.end ?? args.endDate;
  const startDate =
    start && start.trim().length > 0
      ? new Date(`${start}T00:00:00Z`)
      : undefined;
  const endDate =
    end && end.trim().length > 0 ? new Date(`${end}T00:00:00Z`) : undefined;
  const range = { from: startDate, to: endDate };

  if (range.from && range.to && range.from.getTime() >= range.to.getTime()) {
    return {
      ok: false,
      error: `backtest range is inverted or empty: start (${start}) must be before end (${end})`,
    };
  }
  return { ok: true, range };
}

function requireBacktestCandleRange(args: ResolvedBacktestArgs) {
  const candleRange = resolveBacktestCandleRange(args);
  if (!candleRange.ok) {
    return Effect.fail(new Error(candleRange.error));
  }
  return Effect.succeed(candleRange.range);
}

function loadHtfCandles(
  repo: MarketDataRepositoryService,
  args: ResolvedBacktestArgs,
  from: Date | undefined,
  to: Date | undefined,
) {
  const htf = args.htfTimeframe;
  if (htf === undefined || htf.trim().length === 0) {
    return Effect.succeed([] as readonly CandleLike[]);
  }
  return repo.getCandles({
    exchange: args.exchange,
    symbol: args.symbol,
    timeframe: htf,
    from,
    to,
  });
}

function loadFundingRatesOrWarn(
  repo: MarketDataRepositoryService,
  args: ResolvedBacktestArgs,
  candles: readonly CandleLike[],
) {
  // Historical funding rates power the funding-bias component. Missing
  // rows leave the component inert (buildFundingComponent returns null);
  // a fetch failure must not fail the backtest.
  return repo
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
      Effect.tap((fundingRates) =>
        fundingRates.length === 0
          ? Effect.logWarning("funding rates absent — funding component inert")
          : Effect.log(
              `loaded ${fundingRates.length} funding rates — funding component active`,
            ),
      ),
    );
}

interface TemplatedBacktestConfig {
  readonly args: ResolvedBacktestArgs;
  readonly composerConfig: ComposerConfig;
}

function applyBacktestTemplate(
  args: ResolvedBacktestArgs,
  composerConfig: ComposerConfig,
): TemplatedBacktestConfig {
  // --template applies the strategy template's signal weights/thresholds
  // (e.g. microScalp RSI(2)) and its execution overrides on top of the
  // CLI-derived config. Previously the template flags were parsed but
  // never wired — backtests silently ran the default composer.
  if (args.template === undefined || args.template === "") {
    return { args, composerConfig };
  }
  const template = args.template as StrategyTemplateName;
  return {
    args: buildBacktestArgsFromTemplate(template, args),
    composerConfig: buildComposerConfigFromTemplate(template, composerConfig),
  };
}

export function backtestProgram(args: ResolvedBacktestArgs) {
  return Effect.gen(function* () {
    const repo = yield* MarketDataRepository;
    const path = yield* Path;
    const engine = yield* BacktestEngine;

    const { from, to } = yield* requireBacktestCandleRange(args);

    const candles = yield* repo.getCandles({
      exchange: args.exchange,
      symbol: args.symbol,
      timeframe: args.timeframe,
      from,
      to,
    });

    const htfCandles = yield* loadHtfCandles(repo, args, from, to);

    if (candles.length === 0) {
      return yield* Effect.fail(
        new MarketDataRepositoryError(
          `No candles found for ${args.exchange}:${args.symbol}:${args.timeframe}. Run 'market fetch-candles' first.`,
        ),
      );
    }

    const fundingRates = yield* loadFundingRatesOrWarn(repo, args, candles);

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

    const templated = applyBacktestTemplate(args, composerConfig);
    args = templated.args;
    composerConfig = templated.composerConfig;

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

function candleTimestamp(
  candles: readonly CandleLike[],
  bar: number,
  fallbackBar: number,
): Date {
  return (
    candles[bar]?.timestamp ?? candles[fallbackBar]?.timestamp ?? new Date(0)
  );
}

function gridTradeToBacktestTrade(
  t: GridTrade,
  idx: number,
  symbol: string,
  candles: readonly CandleLike[],
  feePct: number,
): BacktestTrade {
  const entryTime = candleTimestamp(candles, t.entryBar, 0);
  const exitTime = candleTimestamp(candles, t.exitBar, candles.length - 1);
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
}

function gridResultToBacktestResult(
  symbol: string,
  grid: GridResult,
  candles: readonly CandleLike[],
  initialCapital: number,
  feePct: number,
): BacktestResult {
  const trades: BacktestTrade[] = grid.trades.map((t: GridTrade, idx: number) =>
    gridTradeToBacktestTrade(t, idx, symbol, candles, feePct),
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

function resolveOptimizeArgs(
  args: Partial<OptimizeArgs>,
  profile: Option.Option<StrategyProfile>,
): OptimizeArgs {
  if (Option.isNone(profile)) return args as OptimizeArgs;
  const overrides = findSymbolOverride(profile.value, args.symbol ?? "") ?? {};
  const resolved = profile.value;
  const get = <K extends keyof StrategyProfileParams>(
    key: K,
  ): StrategyProfileParams[K] =>
    (overrides[key] !== undefined
      ? overrides[key]
      : resolved.defaults[key]) as StrategyProfileParams[K];

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

  return { ...base, ...args } as OptimizeArgs;
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
    futures: futuresOption,
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
    maxHoldBars: maxHoldBarsOption,
    volatilityTargetAnnualPct: volatilityTargetAnnualPctOption,
    profile: profileOption,
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
  },
  (args) =>
    Effect.gen(function* () {
      const path = yield* Path;
      const sqlite = yield* SqliteClient;
      const repoLayer = MarketDataRepositorySQLiteLive(sqlite.database);

      const profile = yield* loadProfileIfNeeded(path.homeDir, args.profile);
      const programArgs = resolveOptimizeArgs(args, profile);

      const result = yield* optimizeProgram(programArgs).pipe(
        Effect.provide(repoLayer),
        Effect.tap((r) =>
          printOptimizeResult(r, args.symbol, args.timeframe, args.capital),
        ),
        Effect.catch((err) =>
          Effect.gen(function* () {
            const msg =
              err instanceof Error
                ? err.message
                : String((err as { readonly reason?: unknown }).reason ?? err);
            yield* Console.error(`optimize failed: ${msg}`);
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

    const { from, to } = yield* requireBacktestCandleRange(args);

    const candles = yield* repo.getCandles({
      exchange: args.exchange,
      symbol: args.symbol,
      timeframe: args.timeframe,
      from,
      to,
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
    const candidates = generateCandidates(args);
    const results: OptimizeResult[] = [];

    if (!args.walkForward) {
      for (const params of candidates) {
        const isResult = yield* runOptimizeCandidate(
          engine,
          args,
          candles,
          composerConfig,
          params,
        );
        results.push({ params, isResult });
      }
      return results;
    }

    const windows = generateWalkForwardWindows(
      candles,
      args.wfTrainDays,
      args.wfTestDays,
      args.wfStepDays,
    );
    for (const window of windows) {
      const result = yield* evaluateWalkForwardWindow(
        engine,
        args,
        window,
        composerConfig,
        candidates,
      );
      if (result !== null) results.push(result);
    }

    return results;
  });
}

function evaluateWalkForwardWindow(
  engine: BacktestEngineImpl,
  args: OptimizeArgs,
  window: WalkForwardWindow,
  composerConfig: ComposerConfig,
  candidates: readonly OptimizeCandidateParams[],
) {
  return Effect.gen(function* () {
    let selected:
      | {
          readonly params: OptimizeCandidateParams;
          readonly isResult: BacktestResult;
        }
      | undefined;

    for (const params of candidates) {
      const isResult = yield* runOptimizeCandidate(
        engine,
        args,
        window.trainCandles,
        composerConfig,
        params,
      );
      if (isResult.totalTrades < args.minTrades) continue;
      if (
        selected === undefined ||
        objectiveValue(isResult, args.selectBy) >
          objectiveValue(selected.isResult, args.selectBy)
      ) {
        selected = { params, isResult };
      }
    }

    if (selected === undefined) return null;
    const oosResult = yield* runOptimizeCandidate(
      engine,
      args,
      window.testCandles,
      composerConfig,
      selected.params,
    );
    if (oosResult.totalTrades < args.minOosTrades) return null;
    return { ...selected, oosResult };
  });
}

function runOptimizeCandidate(
  engine: BacktestEngineImpl,
  args: OptimizeArgs,
  candles: readonly CandleLike[],
  composerConfig: ComposerConfig,
  params: OptimizeCandidateParams,
) {
  const slippageBps = args.realistic
    ? args.realisticSlippageBps
    : args.slippageBps;
  return engine.runBacktest({
    symbol: args.symbol,
    exchange: args.exchange,
    timeframe: args.timeframe,
    candles,
    composerConfig,
    initialCapital: args.capital,
    positionSizePct: args.positionSize,
    riskPerTradePct: args.riskPerTrade,
    maxPositionSizePct: args.maxPositionSize,
    stopLossPct: params.stopLossPct,
    takeProfitPct: params.takeProfitPct,
    feePct: args.fee,
    makerFeePct: args.makerFeePct,
    entryOrderType: params.entryOrderType,
    entryLimitOffsetBps: params.entryLimitOffsetBps,
    minConfidence: params.minConfidence,
    useAtrStops: params.useAtrStops,
    atrStopMultiplier: params.stopMult,
    atrTakeProfitMultiplier: params.tpMult,
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
    minEfficiencyRatio: params.minEfficiencyRatio,
    efficiencyRatioPeriod: args.efficiencyRatioPeriod,
    rsiLongMax: params.rsiLongMax,
    rsiShortMin: params.rsiShortMin,
    recordEquityCurve: false,
    htfCandles: [],
    breakevenAtR: params.breakevenAtR,
    maxBarsInTrade: params.maxBarsInTrade,
    lossCooldownBars: params.lossCooldownBars,
    autoRegimeFilter: args.autoRegimeFilter,
    autoRegimeAdxThreshold: args.autoRegimeAdxThreshold,
    entryOnClose: args.entryOnClose,
    useObservedPrice: args.observedPrice,
  });
}

function topOptimizeBy(
  results: ReadonlyArray<OptimizeResult>,
  metric: "totalReturnPct" | "sharpeRatio",
): OptimizeResult[] {
  return [...results]
    .sort(
      (a, b) =>
        (b.oosResult ?? b.isResult)[metric] -
        (a.oosResult ?? a.isResult)[metric],
    )
    .slice(0, 5);
}

export function formatOptimizeCandidateLine(r: OptimizeResult): string {
  const result = r.oosResult ?? r.isResult;
  return (
    `  stop=${r.params.stopMult.toFixed(2)} tp=${r.params.tpMult.toFixed(2)} conf=${r.params.minConfidence.toFixed(2)} | ` +
    `return=${result.totalReturnPct.toFixed(2)}% sharpe=${result.sharpeRatio.toFixed(3)} trades=${result.totalTrades} win=${(result.winRate * 100).toFixed(1)}% dd=${result.maxDrawdownPct.toFixed(2)}%`
  );
}

function logTopOptimizeCandidates(
  title: string,
  rows: ReadonlyArray<OptimizeResult>,
) {
  return Effect.gen(function* () {
    yield* Console.log(title);
    for (const r of rows) {
      yield* Console.log(formatOptimizeCandidateLine(r));
    }
  });
}

function logOosAggregate(
  oosResults: ReadonlyArray<BacktestResult>,
  initialCapital: number,
) {
  return Effect.gen(function* () {
    if (oosResults.length === 0 || initialCapital <= 0) return;
    let capital = initialCapital;
    let peak = capital;
    let maxDrawdownPct = 0;
    for (const result of oosResults) {
      const windowStartCapital = capital;
      const scale = windowStartCapital / initialCapital;
      for (const trade of result.trades) {
        capital += trade.netPnl * scale;
        peak = Math.max(peak, capital);
        maxDrawdownPct = Math.max(
          maxDrawdownPct,
          peak > 0 ? ((peak - capital) / peak) * 100 : 0,
        );
      }
    }
    const profitableWindows = oosResults.filter(
      (result) => result.totalReturnPct > 0,
    ).length;
    yield* Console.log(
      `Walk-forward OOS aggregate: windows=${oosResults.length} ` +
        `profitable=${((profitableWindows / oosResults.length) * 100).toFixed(1)}% ` +
        `compoundReturn=${(((capital - initialCapital) / initialCapital) * 100).toFixed(2)}% ` +
        `maxDD=${maxDrawdownPct.toFixed(2)}% ` +
        `trades=${oosResults.reduce((sum, result) => sum + result.totalTrades, 0)}`,
    );
  });
}

function printOptimizeResult(
  results: ReadonlyArray<OptimizeResult>,
  symbol: string,
  timeframe: string,
  initialCapital: number,
) {
  return Effect.gen(function* () {
    if (results.length === 0) {
      yield* Console.log("No optimization results.");
      return;
    }

    const byReturn = topOptimizeBy(results, "totalReturnPct");
    const bySharpe = topOptimizeBy(results, "sharpeRatio");

    yield* Console.log(
      `\n🔬 Optimization results for ${symbol} ${timeframe} (${results.length} configs tested)`,
    );
    const oosResults = results.flatMap((result) =>
      result.oosResult === undefined ? [] : [result.oosResult],
    );
    yield* logOosAggregate(oosResults, initialCapital);
    yield* logTopOptimizeCandidates("\nTop 5 by total return:", byReturn);
    yield* logTopOptimizeCandidates("\nTop 5 by Sharpe ratio:", bySharpe);
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

function resolveScanArgs(
  args: Partial<ScanArgs>,
  profile: Option.Option<StrategyProfile>,
): ScanArgs {
  if (Option.isNone(profile)) return args as ScanArgs;
  const defaults = profile.value.defaults;
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

  return { ...base, ...args } as ScanArgs;
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
    maxHoldBars: maxHoldBarsOption,
    volatilityTargetAnnualPct: volatilityTargetAnnualPctOption,
    profile: profileOption,
  },
  (args) =>
    Effect.gen(function* () {
      const path = yield* Path;
      const sqlite = yield* SqliteClient;
      const repoLayer = MarketDataRepositorySQLiteLive(sqlite.database);

      const profile = yield* loadProfileIfNeeded(path.homeDir, args.profile);
      const mergedArgs = resolveScanArgs(args, profile);

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
      const result = yield* evaluateScanSymbol(
        repo,
        exchange,
        symbol,
        args,
        composerConfig,
      );
      if (result !== null) results.push(result);
    }

    return results;
  });
}

function passesScanGates(
  result: Pick<
    BacktestResult,
    "totalReturnPct" | "sharpeRatio" | "maxDrawdownPct"
  >,
  args: ScanArgs,
): boolean {
  if (
    Option.isSome(args.minReturnPct) &&
    result.totalReturnPct < args.minReturnPct.value
  ) {
    return false;
  }
  if (
    Option.isSome(args.minSharpe) &&
    result.sharpeRatio < args.minSharpe.value
  ) {
    return false;
  }
  if (
    Option.isSome(args.maxDrawdownPct) &&
    result.maxDrawdownPct > args.maxDrawdownPct.value
  ) {
    return false;
  }
  return true;
}

function evaluateScanSymbol(
  repo: import("../market-data/repository.js").MarketDataRepositoryService,
  exchange: string,
  symbol: string,
  args: ScanArgs,
  composerConfig: ComposerConfig,
) {
  return Effect.gen(function* () {
    const candles = yield* repo.getCandles({
      exchange,
      symbol,
      timeframe: args.timeframe,
    });

    if (candles.length < 50) return null;

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

    if (!passesScanGates(result, args)) return null;

    return {
      symbol,
      exchange,
      totalTrades: result.totalTrades,
      winRate: result.winRate,
      totalReturnPct: result.totalReturnPct,
      maxDrawdownPct: result.maxDrawdownPct,
      sharpeRatio: result.sharpeRatio,
      bestParams: result.bestParams,
    } satisfies ScanResult;
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

const SCAN_TABLE_HEADER_SINGLE =
  "Symbol        Trades  Win%    Return   Drawdown  Sharpe";
const SCAN_TABLE_HEADER_MULTI =
  "Exchange   Symbol        Trades  Win%    Return   Drawdown  Sharpe";
const SCAN_TABLE_SEPARATOR =
  "--------------------------------------------------------------------";

/** Single scan-result table row; identical layout with and without exchange. */
export function formatScanRow(r: ScanResult, multiExchange: boolean): string {
  const stats =
    `${String(r.totalTrades).padStart(6)}  ` +
    `${(r.winRate * 100).toFixed(1).padStart(5)}%  ` +
    `${r.totalReturnPct.toFixed(2).padStart(6)}%  ` +
    `${r.maxDrawdownPct.toFixed(2).padStart(7)}%   ` +
    `${r.sharpeRatio.toFixed(3)}`;
  if (!multiExchange) return `${r.symbol.padEnd(13)} ${stats}`;
  return `${r.exchange.padEnd(10)} ${r.symbol.padEnd(13)} ${stats}`;
}

/** Best-params detail line, or null when this symbol has no tuned params. */
export function formatBestParamsLine(
  r: ScanResult,
  multiExchange: boolean,
): string | null {
  if (!r.bestParams) return null;
  const prefix = multiExchange ? `${r.exchange}:${r.symbol}` : r.symbol;
  return (
    `  ${prefix.padEnd(25)} stop=${r.bestParams.atrStopMultiplier.toFixed(1)} ` +
    `tp=${r.bestParams.atrTakeProfitMultiplier.toFixed(1)} ` +
    `conf=${r.bestParams.minConfidence.toFixed(1)}`
  );
}

export interface ScanSummary {
  readonly profitable: number;
  readonly avgReturn: number;
  readonly avgSharpe: number;
  readonly highSharpe: number;
  readonly lowDrawdown: number;
  readonly liveReady: number;
  readonly best: ScanResult;
  readonly worst: ScanResult;
}

/** Aggregate scan stats. Callers must pass a non-empty list. */
export function summarizeScanResults(
  results: ReadonlyArray<ScanResult>,
): ScanSummary {
  return {
    profitable: results.filter((r) => r.totalReturnPct > 0).length,
    avgReturn:
      results.reduce((sum, r) => sum + r.totalReturnPct, 0) / results.length,
    avgSharpe:
      results.reduce((sum, r) => sum + r.sharpeRatio, 0) / results.length,
    highSharpe: results.filter((r) => r.sharpeRatio > 0.5).length,
    lowDrawdown: results.filter((r) => r.maxDrawdownPct < 15).length,
    liveReady: results.filter(isLiveReadyScanResult).length,
    best: results.reduce((max, r) =>
      r.totalReturnPct > max.totalReturnPct ? r : max,
    ),
    worst: results.reduce((min, r) =>
      r.totalReturnPct < min.totalReturnPct ? r : min,
    ),
  };
}

function isLiveReadyScanResult(r: ScanResult): boolean {
  return r.totalReturnPct > 0 && r.sharpeRatio > 0.5 && r.maxDrawdownPct < 15;
}

function scanShare(count: number, total: number): string {
  return ((count / total) * 100).toFixed(1);
}

function logScanTable(
  results: ReadonlyArray<ScanResult>,
  multiExchange: boolean,
) {
  return Effect.gen(function* () {
    yield* Console.log("\n🔎 Multi-ticker backtest scan");
    yield* Console.log(
      multiExchange ? SCAN_TABLE_HEADER_MULTI : SCAN_TABLE_HEADER_SINGLE,
    );
    yield* Console.log(SCAN_TABLE_SEPARATOR);
    for (const r of results) {
      yield* Console.log(formatScanRow(r, multiExchange));
    }
  });
}

function logBestParamsSection(
  results: ReadonlyArray<ScanResult>,
  multiExchange: boolean,
) {
  return Effect.gen(function* () {
    const lines = results.flatMap((r) => {
      const line = formatBestParamsLine(r, multiExchange);
      return line === null ? [] : [line];
    });
    if (lines.length === 0) return;
    yield* Console.log("\nBest params per symbol");
    for (const line of lines) {
      yield* Console.log(line);
    }
  });
}

function logScanSummary(
  results: ReadonlyArray<ScanResult>,
  multiExchange: boolean,
) {
  const summary = summarizeScanResults(results);
  const bestPrefix = multiExchange ? `${summary.best.exchange}:` : "";
  const worstPrefix = multiExchange ? `${summary.worst.exchange}:` : "";
  return Effect.gen(function* () {
    yield* Console.log("\nSummary");
    yield* Console.log(`  Symbols tested: ${results.length}`);
    yield* Console.log(
      `  Profitable:     ${summary.profitable} (${scanShare(summary.profitable, results.length)}%)`,
    );
    yield* Console.log(
      `  Sharpe > 0.5:   ${summary.highSharpe} (${scanShare(summary.highSharpe, results.length)}%)`,
    );
    yield* Console.log(
      `  Drawdown < 15%: ${summary.lowDrawdown} (${scanShare(summary.lowDrawdown, results.length)}%)`,
    );
    yield* Console.log(
      `  Live-ready:     ${summary.liveReady} (${scanShare(summary.liveReady, results.length)}%)`,
    );
    yield* Console.log(`  Avg return:     ${summary.avgReturn.toFixed(2)}%`);
    yield* Console.log(`  Avg Sharpe:     ${summary.avgSharpe.toFixed(3)}`);
    yield* Console.log(
      `  Best:           ${bestPrefix}${summary.best.symbol} ${summary.best.totalReturnPct.toFixed(2)}% (Sharpe ${summary.best.sharpeRatio.toFixed(3)})`,
    );
    yield* Console.log(
      `  Worst:          ${worstPrefix}${summary.worst.symbol} ${summary.worst.totalReturnPct.toFixed(2)}% (Sharpe ${summary.worst.sharpeRatio.toFixed(3)})`,
    );
  });
}

interface GroupedScanResults {
  readonly byExchange: Map<string, ScanResult[]>;
  readonly bySymbol: Map<string, ScanResult[]>;
}

function groupScanResults(
  results: ReadonlyArray<ScanResult>,
): GroupedScanResults {
  const byExchange = new Map<string, ScanResult[]>();
  const bySymbol = new Map<string, ScanResult[]>();
  for (const r of results) {
    const exchangeList = byExchange.get(r.exchange) ?? [];
    exchangeList.push(r);
    byExchange.set(r.exchange, exchangeList);
    const symbolList = bySymbol.get(r.symbol) ?? [];
    symbolList.push(r);
    bySymbol.set(r.symbol, symbolList);
  }
  return { byExchange, bySymbol };
}

function logPerExchangeBreakdown(results: ReadonlyArray<ScanResult>) {
  return Effect.gen(function* () {
    const { byExchange, bySymbol } = groupScanResults(results);
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
  });
}

function printScanResult(results: ReadonlyArray<ScanResult>) {
  return Effect.gen(function* () {
    if (results.length === 0) {
      yield* Console.log("No scan results.");
      return;
    }

    const multiExchange = new Set(results.map((r) => r.exchange)).size > 1;

    yield* logScanTable(results, multiExchange);

    yield* logBestParamsSection(results, multiExchange);
    yield* logScanSummary(results, multiExchange);

    if (multiExchange) {
      yield* logPerExchangeBreakdown(results);
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
    /**
     * Ladder rung count. Present only on ladder-scan survivor whitelist rows;
     * its presence routes the row to the incremental ladder paper engine
     * (single-position grid rows omit it).
     */
    readonly rungs?: number;
  };
}

/**
 * A watchlist row is a ladder survivor (routed to the incremental ladder paper
 * engine) when its gridParams carry a gate-scored rung count. Single-position
 * grid rows omit rungs, so they keep routing to the grid engine.
 */
export function isLadderSurvivorRow(
  gridParams: WatchlistEntry["gridParams"] | undefined,
  strategyType: string | undefined,
): boolean {
  return strategyType === "grid" && (gridParams?.rungs ?? 0) > 0;
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
) {
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
  /** Ladder: force-close a rung held longer than this many bars (0 = off). */
  readonly maxHoldBars: number;
  /** Ladder: how to resolve a config mismatch with open rungs. */
  readonly configMismatchAction: "hold" | "force-reseed";
  /**
   * Bybit signal-data feed (klines driving entries), independent of the
   * execution venue. testnet (default) matches the demo execution venue;
   * mainnet gives research parity with mainnet price behavior. A demo proves
   * testnet EXECUTION, never a mainnet edge.
   */
  readonly signalFeed: "testnet" | "mainnet";
  /** Ladder: force-close an open rung whose unrealized loss exceeds this % (0 = off). */
  readonly maxPositionDrawdownPct: number;
  /** Ladder: stop distance as a multiple of the grid step (0 = legacy boundary). */
  readonly stopRatio: number;
  /** Ladder: live-entry limit cross in bps (default 0 = opt-in; soak sets 5). */
  readonly liveEntryCrossBps: number;
  /** Ladder: market-type demo entries (default false = limit; testnet-only). */
  readonly demoLiveMarketEntries: boolean;
  /** Per-side taker fee percent for non-target (market) exits. */
  readonly takerExitFeePct: number;
  /** Funding cost percent of notional per 8h held on open positions. */
  readonly fundingRatePct8h: number;
  /** Maintenance margin rate percent of notional for the liquidation model. */
  readonly maintenanceMarginRatePct: number;
  readonly live: boolean;
  readonly shadow?: boolean;
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

interface PaperTradeRuntime {
  readonly strategyType: "signal" | "grid";
  readonly useSandbox: boolean;
  readonly useTestnet: boolean;
  readonly resolvedExchange: string;
  readonly isDemoAccount: boolean;
  readonly marginMode: FuturesMarginMode;
  readonly productType: BitgetProductType;
}

function resolvePaperTradeArgs(
  args: Partial<PaperTradeArgs>,
  profile: Option.Option<StrategyProfile>,
): PaperTradeArgs {
  if (Option.isNone(profile)) return args as PaperTradeArgs;
  const overrides = findSymbolOverride(profile.value, args.symbol ?? "") ?? {};
  const resolved = profile.value;
  const get = <K extends keyof StrategyProfileParams>(
    key: K,
  ): StrategyProfileParams[K] =>
    (overrides[key] !== undefined
      ? overrides[key]
      : resolved.defaults[key]) as StrategyProfileParams[K];

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

  return { ...base, ...args } as PaperTradeArgs;
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

function resolveSoakArgs(
  args: Partial<SoakArgs>,
  profile: Option.Option<StrategyProfile>,
): SoakArgs {
  if (Option.isNone(profile)) return args as SoakArgs;
  const defaults = profile.value.defaults;
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

  return { ...base, ...args } as SoakArgs;
}

type MutablePartialRiskLimits = {
  -readonly [
    K in keyof import("../risk/guards.js").RiskLimits
  ]?: import("../risk/guards.js").RiskLimits[K];
};

type MutableFuturesPaperTradingOptions = {
  -readonly [
    K in keyof FuturesPaperTradingOptions
  ]?: FuturesPaperTradingOptions[K];
};

type MutableGridPaperTradingOptions = {
  -readonly [K in keyof GridPaperTradingOptions]?: GridPaperTradingOptions[K];
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

interface RiskOverrideOptions {
  readonly maxDrawdownPct: Option.Option<number>;
  readonly maxDailyLossPct: Option.Option<number>;
  readonly maxPositionSizePct: Option.Option<number>;
  readonly maxTradesPerDay: Option.Option<number>;
  readonly minCapital: Option.Option<number>;
}

function buildRiskOverrides(
  args: RiskOverrideOptions,
): MutablePartialRiskLimits {
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
    shadow: shadowOption,
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
    maxHoldBars: maxHoldBarsOption,
    configMismatchAction: configMismatchActionOption,
    signalFeed: signalFeedOption,
    maxPositionDrawdownPct: maxPositionDrawdownPctOption,
    stopRatio: stopRatioOption,
    liveEntryCrossBps: liveEntryCrossBpsOption,
    demoLiveMarketEntries: demoLiveMarketEntriesOption,
    takerExitFeePct: takerExitFeePctOption,
    fundingRatePct8h: fundingRatePct8hOption,
    maintenanceMarginRatePct: maintenanceMarginRatePctOption,
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
      const mergedArgs = resolvePaperTradeArgs(args, profile);
      // Single flag driving the Bybit kline feed (clever-cabin-1e1): the
      // gateway resolves its base URL from BYBIT_SIGNAL_FEED at call time.
      process.env.BYBIT_SIGNAL_FEED = mergedArgs.signalFeed ?? "testnet";

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
        onSome: (file) =>
          loadWatchlist(
            file.startsWith("/") ? file : resolve(path.homeDir, "data", file),
          ),
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
      const marketDataLayer =
        mergedArgs.live || mergedArgs.shadow
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
    return "live execution is disabled until the demo/testnet account is enabled (BITGET_USE_SANDBOX=true or BYBIT_USE_TESTNET=true)";
  }
  return undefined;
}

/**
 * Resolve the execution environment recorded on grid fills. Bybit is the live
 * engine (clever-cabin-fyv) and its demo mode is BYBIT_USE_TESTNET=true
 * (api-testnet.bybit.com); Bitget's demo mode is BITGET_USE_SANDBOX=true.
 */
export function executionEnvironmentFor(
  exchange: string,
  live: boolean,
  demoAccount: boolean,
): "bitget-demo" | "bitget-live" | "bybit-demo" | "bybit-live" {
  const isBybit = exchange === "bybit-futures";
  if (isBybit) {
    return live && !demoAccount ? "bybit-live" : "bybit-demo";
  }
  return live && !demoAccount ? "bitget-live" : "bitget-demo";
}

/**
 * Readiness manifest for the LADDER portfolio soak. The fingerprint covers
 * the config fields that change trading behavior; capital is excluded (a
 * rebalance/compounding move must not orphan the cohort's evidence).
 */
export function strategyManifestForLadder(
  args: {
    readonly timeframe: string;
    readonly fee: number;
    readonly slippageBps: number;
    readonly takerExitFeePct: number;
    readonly fundingRatePct8h: number;
    readonly maxDrawdownPct: Option.Option<number>;
    readonly maxDailyLossPct: Option.Option<number>;
    readonly onlyWithTrend?: boolean;
    readonly trendFilterPeriod?: number;
  },
  symbol: string,
  executionEnvironment:
    | "bitget-demo"
    | "bitget-live"
    | "bybit-demo"
    | "bybit-live",
): StrategyManifest {
  return {
    ...DEFAULT_STRATEGY_MANIFEST,
    exchange: executionEnvironment,
    symbol,
    timeframe: args.timeframe,
    // Per-symbol ladder params vary per whitelist row, so the portfolio
    // manifest pins the shared cost/risk surface; per-row geometry stays in
    // the row itself. Placeholder numerics ("0") mark the row-scoped fields
    // — the fingerprinter parses every field as a decimal, so non-numeric
    // markers are invalid here.
    gridStepPct: "0",
    gridMaxGrids: "0",
    gridPauseAfterLossBars: "0",
    positionFraction: "1",
    feePct: args.fee.toString(),
    slippageBps: args.slippageBps.toString(),
    trendFilterPeriod: String(args.trendFilterPeriod ?? 0),
    adxGate: "0",
    targetRatio: "0",
    onlyWithTrend: (args.onlyWithTrend ?? false).toString(),
    leverage: "1",
    productType: "USDT-FUTURES",
    marginMode: "isolated",
    maxDrawdownPct: Option.getOrElse(args.maxDrawdownPct, () => 100).toString(),
    maxDailyLossPct: Option.getOrElse(args.maxDailyLossPct, () => 2).toString(),
    validationProfile: "ladder-portfolio-soak",
    orderType: "limit-at-grid-level",
    triggerTiming: "level-touch",
    engineVersion: "ladder-engine/v2",
  };
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

export function validateShadowMode(
  shadow: boolean,
  live: boolean,
): string | undefined {
  return shadow && live
    ? "--shadow cannot be combined with --live; shadow mode never places exchange orders"
    : undefined;
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

/**
 * True when the live grid config exactly reproduces a validated readiness
 * cohort candidate (leverage may exceed the candidate value: it scales
 * PnL/variance, not the strategy, and tiny accounts need the sizing's
 * floor-raise — the risk engine's maxLeverage cap still bounds it).
 */
function isLiveGridCohortCandidate(
  config: LiveGridConfiguration,
  candidate: ReturnType<typeof candidateForSymbol>,
): boolean {
  return (
    candidate !== undefined &&
    matchesCohortMarket(config, candidate) &&
    matchesCohortGeometry(config, candidate) &&
    matchesCohortCosts(config, candidate) &&
    matchesCohortSignalGates(config, candidate) &&
    config.leverage >= candidate.leverage
  );
}

function matchesCohortMarket(
  config: LiveGridConfiguration,
  candidate: ValidatedGridCandidate,
): boolean {
  return (
    config.exchange === candidate.exchange &&
    config.timeframe === candidate.timeframe &&
    config.productType === candidate.productType
  );
}

function matchesCohortGeometry(
  config: LiveGridConfiguration,
  candidate: ValidatedGridCandidate,
): boolean {
  return (
    config.gridStepPct === candidate.gridStepPct &&
    config.gridMaxGrids === candidate.gridMaxGrids &&
    config.gridPauseAfterLossBars === candidate.gridPauseAfterLossBars
  );
}

function matchesCohortCosts(
  config: LiveGridConfiguration,
  candidate: ValidatedGridCandidate,
): boolean {
  return (
    config.feePct === candidate.feePct &&
    config.slippageBps === candidate.slippageBps
  );
}

function matchesCohortSignalGates(
  config: LiveGridConfiguration,
  candidate: ValidatedGridCandidate,
): boolean {
  return (
    config.trendFilterPeriod === candidate.trendFilterPeriod &&
    config.onlyWithTrend === candidate.onlyWithTrend &&
    config.targetRatio === candidate.targetRatio &&
    config.chopGateAdx === candidate.chopGateAdx
  );
}

/** Message for a live-grid risk limit outside (0%, cap]; undefined when valid. */
function liveGridRiskPctError(
  value: number,
  cap: number,
  message: string,
): string | undefined {
  return !Number.isFinite(value) || value <= 0 || value > cap
    ? message
    : undefined;
}

export function validateLiveGridConfiguration(
  config: LiveGridConfiguration,
  sandbox = false,
): string | undefined {
  const candidate = candidateForSymbol(config.symbol);
  if (!isLiveGridCohortCandidate(config, candidate) && !sandbox) {
    return "live grid must use a validated readiness cohort candidate";
  }
  if (sandbox) return undefined;
  return firstLiveGridRiskError(config, liveGridRiskCaps(candidate));
}

interface LiveGridRiskCaps {
  readonly riskCap: number;
  readonly ddCap: number;
  readonly dailyCap: number;
}

function liveGridRiskCaps(
  candidate: ReturnType<typeof candidateForSymbol>,
): LiveGridRiskCaps {
  return {
    riskCap:
      candidate?.maxPositionSizePct ??
      VALIDATED_BTC_GRID_CANDIDATE.maxPositionSizePct,
    ddCap:
      candidate?.maxDrawdownPct ?? VALIDATED_BTC_GRID_CANDIDATE.maxDrawdownPct,
    dailyCap:
      candidate?.maxDailyLossPct ??
      VALIDATED_BTC_GRID_CANDIDATE.maxDailyLossPct,
  };
}

function firstLiveGridRiskError(
  config: LiveGridConfiguration,
  caps: LiveGridRiskCaps,
): string | undefined {
  return (
    liveGridRiskPctError(
      config.maxPositionSizePct,
      caps.riskCap,
      "live grid max position size must be between 0% and 50%",
    ) ??
    liveGridRiskPctError(
      config.maxDrawdownPct,
      caps.ddCap,
      "live grid max drawdown must be between 0% and 5%",
    ) ??
    liveGridRiskPctError(
      config.maxDailyLossPct,
      caps.dailyCap,
      "live grid max daily loss must be between 0% and 2%",
    )
  );
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
    Effect.mapError((err) => {
      const detail = "body" in err ? err.body : String(err);
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

/**
 * Fetch the Bybit linear contract table for the symbols a live command will
 * touch. The adapter still performs its own authoritative lookup before every
 * order; this startup snapshot only lets the paper engine calculate an
 * orderable quantity before it calls that adapter.
 */
function fetchBybitContracts(
  symbols: readonly string[],
): Effect.Effect<ReadonlyArray<BybitContract>, Error> {
  const bybitClientLayer = BybitClientLiveConfig.pipe(
    Layer.provide(BybitConfigLive),
  );
  const uniqueSymbols = [...new Set(symbols.map(toBybitSymbol))];
  return Effect.gen(function* () {
    const client = yield* BybitClient;
    return yield* Effect.forEach(
      uniqueSymbols,
      (symbol) => client.getContract(symbol),
      { concurrency: 4 },
    );
  }).pipe(
    Effect.provide(bybitClientLayer),
    Effect.mapError((err) => {
      const detail = "body" in err ? err.body : String(err);
      return new Error(
        `failed to fetch Bybit contracts: ${detail.length > 0 ? detail : String(err)}`,
      );
    }),
  );
}

/** Convert Bybit's instrument fields to the shared order-sizing contract. */
export function bybitContractSpecs(
  contract: BybitContract | undefined,
): ContractSizeSpec | undefined {
  if (contract === undefined) return undefined;
  const minQty = Number(contract.minOrderQty);
  const qtyStep = Number(contract.qtyStep);
  const minTradeUSDT = Number(contract.minOrderAmt);
  if (
    !Number.isFinite(minQty) ||
    minQty <= 0 ||
    !Number.isFinite(qtyStep) ||
    qtyStep < 0 ||
    !Number.isFinite(minTradeUSDT) ||
    minTradeUSDT < 0
  ) {
    return undefined;
  }
  return { minQty, qtyStep, minTradeUSDT };
}

/**
 * Per-symbol signal overrides a watchlist entry may layer onto the CLI args
 * (from entry.bestParams). Only these three fields are ever passed; every
 * other option flows straight from the resolved args.
 */
type EngineSignalOverrides = {
  readonly minConfidence?: number;
  readonly atrStopMultiplier?: number;
  readonly atrTakeProfitMultiplier?: number;
};

interface LadderGridSettings {
  readonly rungs: number;
  readonly gridStepPct: number;
  readonly gridMaxGrids: number;
  readonly gridPauseAfterLossBars: number;
  readonly targetRatio: number;
  readonly chopGateAdxThreshold: number;
}

export interface FrozenGridCliKnobs {
  readonly gridStepPct: number;
  readonly gridMaxGrids: number;
  readonly gridPauseAfterLossBars: number;
  readonly targetRatio?: number;
  readonly chopGateAdx?: number;
}

export interface FrozenGridMismatch {
  readonly field:
    | "gridStepPct"
    | "gridMaxGrids"
    | "gridPauseAfterLossBars"
    | "targetRatio"
    | "chopGateAdx";
  readonly watchlist: number;
  readonly cli: number;
}

/**
 * A CLI knob counts as "frozen intent" (explicitly pinned, e.g. from
 * champion-soak.json) only when it differs from the unset/disabled default.
 * The grid CLI defaults are 0 = disabled (step/pause/maxGrids/chopGate) and
 * 1.0 (targetRatio); a default-valued CLI defers to the row's validated
 * config so legacy DB-watchlist soaks keep working untouched.
 */
function isFrozenCliSet(
  field: FrozenGridMismatch["field"],
  value: number | undefined,
): value is number {
  if (value === undefined || !Number.isFinite(value)) return false;
  if (field === "targetRatio") return value !== 1;
  return value > 0;
}

/**
 * Compare one watchlist row's gridParams against the frozen CLI knobs and
 * report every field where BOTH sides are pinned but disagree (clever-cabin-cdv).
 * The champion soak pins step 1.3 / maxGrids 2 / pause 2 / target 1.95 while
 * the checked-in whitelist still carries 1.29 / 3 / 4 / 1.9 — without this
 * check the soak silently validates the wrong baseline.
 */
export function detectFrozenGridMismatch(
  gridParams: WatchlistEntry["gridParams"] | undefined,
  args: FrozenGridCliKnobs,
): readonly FrozenGridMismatch[] {
  if (gridParams === undefined) return [];
  const mismatches: FrozenGridMismatch[] = [];
  const check = (
    field: FrozenGridMismatch["field"],
    watchlist: number | undefined,
    cli: number | undefined,
  ): void => {
    if (watchlist === undefined || !Number.isFinite(watchlist)) return;
    if (!isFrozenCliSet(field, cli)) return;
    if (watchlist !== (cli as number)) {
      mismatches.push({ field, watchlist, cli: cli as number });
    }
  };
  check("gridStepPct", gridParams.gridStepPct, args.gridStepPct);
  check("gridMaxGrids", gridParams.gridMaxGrids, args.gridMaxGrids);
  check(
    "gridPauseAfterLossBars",
    gridParams.gridPauseAfterLossBars,
    args.gridPauseAfterLossBars,
  );
  check("targetRatio", gridParams.targetRatio, args.targetRatio);
  check("chopGateAdx", gridParams.chopGateAdx, args.chopGateAdx);
  return mismatches;
}

export function formatFrozenGridMismatch(
  mismatches: readonly FrozenGridMismatch[],
): string {
  return mismatches
    .map((m) => `${m.field}: watchlist=${m.watchlist} cli(frozen)=${m.cli}`)
    .join("; ");
}

/**
 * Grid geometry for one ladder member on the SINGLE FROZEN CONFIG PATH:
 *
 * - No divergence (or no frozen CLI intent): legacy row-wins resolution —
 *   gate-scored values from the watchlist row when present, CLI defaults
 *   otherwise (direct symbol invocation without a whitelist row).
 * - Divergence + `--config-mismatch-action force-reseed` (the champion soak
 *   setting): the frozen CLI knobs (derived from champion-soak.json) WIN, so
 *   the soak validates the advertised baseline even against a stale
 *   whitelist.
 * - Divergence + `hold` (default): HARD-FAIL (throw) — never silently trade
 *   a config that matches neither the row's validated state nor the frozen
 *   CLI. Re-run with force-reseed to trade the frozen config, or regenerate
 *   the whitelist from the frozen knobs.
 */
/**
 * Pick one knob on the frozen path. Once a divergence is approved (mismatches
 * non-empty implies force-reseed — hold throws above), an explicitly pinned
 * CLI knob wins; otherwise the row's validated value (or the CLI
 * default/fallback) governs, preserving the legacy DB-watchlist behavior.
 */
function pickFrozenKnob(
  field: FrozenGridMismatch["field"],
  cli: number,
  row: number | undefined,
  fallback: number,
  frozen: boolean,
): number {
  if (frozen && isFrozenCliSet(field, cli)) return cli;
  return row ?? fallback;
}

export function resolveLadderGridSettings(
  gridParams: WatchlistEntry["gridParams"] | undefined,
  args: FrozenGridCliKnobs,
  configMismatchAction: "hold" | "force-reseed" = "hold",
): LadderGridSettings {
  const mismatches = detectFrozenGridMismatch(gridParams, args);
  assertNoFrozenMismatch(mismatches, configMismatchAction);
  const frozen = mismatches.length > 0;
  return {
    rungs: gridParams?.rungs ?? 1,
    ...resolveLadderKnobs(gridParams, args, frozen),
  } satisfies LadderGridSettings;
}

function assertNoFrozenMismatch(
  mismatches: readonly FrozenGridMismatch[],
  configMismatchAction: "hold" | "force-reseed",
): void {
  if (mismatches.length > 0 && configMismatchAction === "hold") {
    throw new Error(
      `frozen champion config mismatch [${formatFrozenGridMismatch(mismatches)}] — watchlist gridParams diverge from the frozen CLI knobs (champion-soak.json); refusing to trade the stale row (fail-closed). Re-run with --config-mismatch-action force-reseed to trade the frozen CLI config, or regenerate the whitelist from the frozen knobs`,
    );
  }
}

function resolveLadderKnobs(
  gridParams: WatchlistEntry["gridParams"] | undefined,
  args: FrozenGridCliKnobs,
  frozen: boolean,
): Omit<LadderGridSettings, "rungs"> {
  return {
    gridStepPct: pickFrozenKnob(
      "gridStepPct",
      args.gridStepPct,
      gridParams?.gridStepPct,
      args.gridStepPct,
      frozen,
    ),
    gridMaxGrids: pickFrozenKnob(
      "gridMaxGrids",
      args.gridMaxGrids,
      gridParams?.gridMaxGrids,
      args.gridMaxGrids,
      frozen,
    ),
    gridPauseAfterLossBars: pickFrozenKnob(
      "gridPauseAfterLossBars",
      args.gridPauseAfterLossBars,
      gridParams?.gridPauseAfterLossBars,
      args.gridPauseAfterLossBars,
      frozen,
    ),
    targetRatio: pickFrozenKnob(
      "targetRatio",
      args.targetRatio ?? 1,
      gridParams?.targetRatio,
      1,
      frozen,
    ),
    chopGateAdxThreshold: pickFrozenKnob(
      "chopGateAdx",
      args.chopGateAdx ?? 0,
      gridParams?.chopGateAdx,
      0,
      frozen,
    ),
  };
}

export interface FrozenGridGeometry {
  readonly gridStepPct: number;
  readonly gridMaxGrids: number;
  readonly gridPauseAfterLossBars: number;
}

/**
 * Grid-engine twin of resolveLadderGridSettings for the single-position grid
 * path (watchlist rows WITHOUT rungs): same frozen-wins / hold-fails-closed
 * contract, geometry fields only. Target/chop overlays keep flowing through
 * gridOverridesFromWatchlistRow; the startup gate (assertFrozenWatchlistConfig)
 * covers them for both engines.
 */
export function resolveFrozenGridGeometry(
  gridParams: WatchlistEntry["gridParams"] | undefined,
  args: FrozenGridCliKnobs,
  configMismatchAction: "hold" | "force-reseed" = "hold",
): FrozenGridGeometry {
  const settings = resolveLadderGridSettings(
    gridParams,
    args,
    configMismatchAction,
  );
  return {
    gridStepPct: settings.gridStepPct,
    gridMaxGrids: settings.gridMaxGrids,
    gridPauseAfterLossBars: settings.gridPauseAfterLossBars,
  };
}

/**
 * max-position-size-pct is an account-level cap. Once capital is partitioned,
 * expand the per-partition cap enough to preserve that account-level budget
 * without ever exceeding 100% of a partition. The rebalance cap (when
 * present) then keeps deployed size tracking the member's target share of
 * live portfolio equity.
 */
function ladderPartitionPositionPct(
  rawWeight: Decimal,
  basePositionPct: number,
  positionPctCap: number | undefined,
): number {
  return Math.min(
    rawWeight.greaterThan(0) && rawWeight.lessThan(1)
      ? Math.min(100, basePositionPct / rawWeight.toNumber())
      : basePositionPct,
    positionPctCap ?? 100,
  );
}

/**
 * Run the paper-trade live/sandbox/strategy guard rails in order and return
 * the first violation message, or undefined when the invocation may proceed.
 */
function firstPaperTradeValidationError(
  args: PaperTradeArgs,
  strategyType: "signal" | "grid",
  isDemoAccount: boolean,
): string | undefined {
  return (
    validateLiveExecutionMarket(args.live, args.futures) ??
    validateShadowMode(args.shadow ?? false, args.live) ??
    validateLiveSandboxMode(args.live, isDemoAccount) ??
    validateLiveExecutionStrategy(args.live, strategyType) ??
    validateLiveGridWatchlist(
      args.live,
      strategyType,
      args.entries,
      isDemoAccount,
    )
  );
}

/**
 * One-line startup provenance separating the SIGNAL-data source from the
 * EXECUTION venue (clever-cabin-1e1). A Bybit demo soak reads klines from the
 * signal feed but routes fills to the testnet matching engine: it proves
 * testnet EXECUTION (order routing, fills, risk guards), never a mainnet
 * edge. Keep signal-feed=testnet to certify a forward paper-vs-demo
 * comparison on the SAME feed; signal-feed=mainnet gives research parity
 * with mainnet price behavior while still executing on testnet.
 */
export function describeSignalExecutionSplit(
  args: {
    readonly live: boolean;
    readonly shadow?: boolean;
    readonly signalFeed?: "testnet" | "mainnet";
  },
  runtime: {
    readonly resolvedExchange: string;
    readonly isDemoAccount: boolean;
  },
): string {
  const signalFeed = args.signalFeed ?? "testnet";
  const feedTag = describeBybitSignalFeed(signalFeed);
  const feedUrl = resolveBybitBaseUrl(signalFeed);
  const executionEnv = executionEnvironmentFor(
    runtime.resolvedExchange,
    args.live,
    runtime.isDemoAccount,
  );
  const marketData =
    args.live || args.shadow === true ? "live-venue" : "stored-repository";
  const split =
    signalFeed === "mainnet" && executionEnv === "bybit-demo"
      ? "signals=mainnet fills=testnet"
      : `signals+fills=${signalFeed === "mainnet" ? "mainnet" : "testnet"}`;
  return (
    `signalFeed=${feedTag} (${feedUrl}) | executionEnv=${executionEnv} | ` +
    `marketData=${marketData} | ${split} | demoProves=testnet-execution-only ` +
    `(routing, fills, risk guards) — never a mainnet edge`
  );
}

/**
 * Fail-closed frozen-config gate for watchlist soaks (clever-cabin-cdv).
 * Every row carrying gridParams must agree with the frozen CLI knobs
 * (champion-soak.json) on all explicitly pinned fields:
 * - `--config-mismatch-action hold` (default): HARD-FAIL the process before
 *   any trade — a stale whitelist must never silently replace the frozen
 *   baseline.
 * - `force-reseed` (the champion soak setting): warn per diverged row and
 *   continue; the per-row resolvers then trade the frozen CLI knobs
 *   (frozen wins).
 */
function assertFrozenWatchlistConfig(
  args: PaperTradeArgs,
): Effect.Effect<void, Error> {
  return Effect.gen(function* () {
    const strategyType = args.strategyType ?? "signal";
    if (strategyType !== "grid") return;
    const rows = (args.entries ?? []).filter(
      (entry) => entry.gridParams !== undefined,
    );
    for (const row of rows) {
      const mismatches = detectFrozenGridMismatch(row.gridParams, args);
      if (mismatches.length === 0) continue;
      const detail = `${row.exchange ?? args.exchange}:${row.symbol} [${formatFrozenGridMismatch(mismatches)}]`;
      if (args.configMismatchAction === "hold") {
        return yield* Effect.fail(
          new Error(
            `frozen champion config mismatch for ${detail} — watchlist gridParams diverge from the frozen CLI knobs (champion-soak.json); refusing to soak the stale row (fail-closed). Re-run with --config-mismatch-action force-reseed to trade the frozen CLI config, or regenerate the whitelist from the frozen knobs`,
          ),
        );
      }
      yield* Console.warn(
        `frozen CLI knobs win for ${detail} — trading the champion-soak config (--config-mismatch-action force-reseed)`,
      );
    }
  });
}

/**
 * Assemble the live-grid readiness validation input from the resolved args.
 */
function buildLiveGridConfigCandidate(
  args: PaperTradeArgs,
  resolvedExchange: string,
  productType: BitgetProductType,
): LiveGridConfiguration {
  return {
    exchange: resolvedExchange,
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
    maxPositionSizePct: Option.getOrElse(args.maxPositionSizePct, () => 100),
    maxDrawdownPct: Option.getOrElse(args.maxDrawdownPct, () => 100),
    maxDailyLossPct: Option.getOrElse(args.maxDailyLossPct, () => 100),
  };
}

/** Spot paper-trading adapter layer: live Binance or the in-memory simulator. */
function paperSpotAdapterLayer(args: {
  readonly live: boolean;
  readonly apiKey: string;
  readonly apiSecret: string;
}) {
  return args.live
    ? BinanceLiveExchangeAdapterLive({
        apiKey: args.apiKey || process.env.BINANCE_API_KEY || "",
        apiSecret: args.apiSecret || process.env.BINANCE_API_SECRET || "",
      })
    : SimulatedExchangeAdapterLive();
}

/**
 * Futures execution adapter layer for the paper-trading programs: live runs
 * route to the resolved venue's live adapter, simulated runs to the
 * in-memory simulator.
 */
function paperFuturesAdapterLayer(args: {
  readonly live: boolean;
  readonly exchange: string;
}): Layer.Layer<
  FuturesExchangeAdapterService,
  never,
  MarketDataGatewayService
> {
  return (
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
}

/**
 * Ladder portfolio rebalance cadence in ms from
 * NEURATRADE_LADDER_REBALANCE_HOURS; 0 disables (default 24h).
 */
function ladderRebalanceIntervalMs(): number {
  return (
    (Number(process.env.NEURATRADE_LADDER_REBALANCE_HOURS ?? "24") || 0) *
    3_600_000
  );
}

/**
 * Stamp every ladder trade this process records with readiness provenance
 * (fingerprint + cohort), mirroring the grid engine's per-state fields.
 * Without it ladder fills are untagged legacy rows the real-money readiness
 * gate can never count.
 */
function stampLadderTradeProvenance(
  ladderPortfolioEntries: readonly WatchlistEntry[],
  args: PaperTradeArgs,
  resolvedExchange: string,
  useTestnet: boolean,
  useSandbox: boolean,
): void {
  if (ladderPortfolioEntries.length === 0) return;
  const ladderEnv = executionEnvironmentFor(
    resolvedExchange,
    args.live,
    useTestnet || useSandbox,
  );
  const manifest = strategyManifestForLadder(args, "portfolio", ladderEnv);
  const fingerprint = fingerprintStrategyManifest(manifest);
  setLadderTradeProvenance({
    fingerprint,
    cohortId: `ladder-${fingerprint.slice(0, 16)}`,
    lockedAt: new Date(),
    executionEnvironment: ladderEnv,
  });
}

type PaperIterationLogResult =
  | import("../paper-trading/engine.js").PaperTradingIterationResult
  | import("../paper-trading/futures-engine.js").FuturesPaperTradingIterationResult
  | GridPaperTradingIterationResult
  | LadderPaperIterationResult;

function formatPaperIterationLog(
  scope: string,
  result: PaperIterationLogResult,
): string {
  const rungs =
    "openRungs" in result && "closedThisIteration" in result
      ? ` | open=${result.openRungs} | closed=${result.closedThisIteration}${"equity" in result && "unrealizedPnl" in result ? ` | equity=${Number(result.equity).toFixed(2)} | uPnL=${Number(result.unrealizedPnl).toFixed(4)}` : ""}`
      : "";
  return `[${new Date().toISOString()}] ${scope}${result.action.toUpperCase()} | capital=${result.capital.toFixed(2)}${rungs} | ${result.note}`;
}

interface LadderPortfolioRow {
  readonly entry: WatchlistEntry;
  readonly key: string;
}

interface LadderPortfolioPrep {
  readonly entries: readonly WatchlistEntry[];
  readonly rows: readonly LadderPortfolioRow[];
  readonly keys: readonly string[];
  readonly id: string | undefined;
  readonly allocations: ReadonlyMap<string, Decimal>;
}

function prepareLadderPortfolio(
  entries: readonly WatchlistEntry[] | undefined,
  args: PaperTradeArgs,
  resolvedExchange: string,
): LadderPortfolioPrep {
  const ladderPortfolioEntries =
    entries?.filter((entry) =>
      isLadderSurvivorRow(entry.gridParams, args.strategyType),
    ) ?? [];
  const rows = ladderPortfolioEntries
    .map((entry) => ({
      entry,
      key: `${resolveFuturesMarketExchange(entry.exchange ?? args.exchange, true)}:${entry.symbol}:${args.timeframe}`,
    }))
    .sort((left, right) => left.key.localeCompare(right.key));
  const keys = rows.map((row) => row.key);
  const id =
    ladderPortfolioEntries.length > 0
      ? `ladder:${resolvedExchange}:${args.timeframe}:${args.capital}:${keys.join(",")}`
      : undefined;
  const allocations = allocateLadderPortfolioCapital(
    rows.map((row) => ({
      key: row.key,
      allocatedWeight: row.entry.gridParams?.allocatedWeight,
    })),
    args.capital,
  );
  return { entries: ladderPortfolioEntries, rows, keys, id, allocations };
}

function loadPaperTradeContracts(
  args: PaperTradeArgs,
  resolvedExchange: string,
  strategyType: string,
  productType: BitgetProductType,
  contractSymbols: readonly string[],
) {
  return Effect.gen(function* () {
    const bitgetContracts =
      args.live &&
      resolvedExchange !== "bybit-futures" &&
      (args.futures || strategyType === "grid")
        ? yield* fetchBitgetContracts(productType)
        : undefined;
    const bybitContracts =
      args.live &&
      resolvedExchange === "bybit-futures" &&
      (args.futures || strategyType === "grid")
        ? yield* fetchBybitContracts(contractSymbols)
        : undefined;
    const bybitContractMap = new Map(
      (bybitContracts ?? []).map((contract) => [
        toBybitSymbol(contract.symbol),
        contract,
      ]),
    );
    return { bitgetContracts, bybitContracts, bybitContractMap };
  });
}

function paperTradeProgram(args: PaperTradeArgs) {
  return Effect.gen(function* () {
    const resolveRuntime = (): Effect.Effect<PaperTradeRuntime, Error, never> =>
      Effect.gen(function* () {
        // Sandbox/testnet is read from the resolved client configuration so
        // validation and demo routing cannot disagree.
        const useSandbox = yield* BitgetConfig.pipe(
          Effect.map((config) => config.useSandbox),
          Effect.provide(BitgetConfigLive),
          Effect.orDie,
        );
        const useTestnet = yield* BybitConfig.pipe(
          Effect.map((config) => config.useTestnet),
          Effect.provide(BybitConfigLive),
          Effect.orDie,
        );
        const strategyType = args.strategyType ?? "signal";
        const resolvedExchange = resolveFuturesMarketExchange(
          args.exchange,
          true,
        );
        const isDemoAccount =
          resolvedExchange === "bybit-futures" ? useTestnet : useSandbox;
        const validationError = firstPaperTradeValidationError(
          args,
          strategyType,
          isDemoAccount,
        );
        if (validationError !== undefined) {
          return yield* Effect.fail(new Error(validationError));
        }
        const marginMode = parseMarginMode(args.marginMode);
        const productType = parseProductType(args.productType);
        if (args.live && strategyType === "grid") {
          const liveGridError = validateLiveGridConfiguration(
            buildLiveGridConfigCandidate(args, resolvedExchange, productType),
            // Single-symbol live grid must always reproduce a validated
            // readiness cohort candidate — even on demo/testnet accounts.
            // Only multi-symbol demo/testnet runs (watchlist soaks) relax the
            // cohort gate; mainnet stays blocked by validateLiveSandboxMode.
            (args.entries?.length ?? 0) > 0 && isDemoAccount,
          );
          if (liveGridError !== undefined) {
            return yield* Effect.fail(new Error(liveGridError));
          }
        }
        return {
          strategyType,
          useSandbox,
          useTestnet,
          resolvedExchange,
          isDemoAccount,
          marginMode,
          productType,
        };
      });

    const runtime = yield* resolveRuntime();
    const {
      strategyType,
      useSandbox,
      useTestnet,
      resolvedExchange,
      marginMode,
      productType,
    } = runtime;

    // Explicit signal-vs-execution provenance (clever-cabin-1e1): what the
    // demo proves is logged on every start, next to the execution env.
    yield* Console.log(describeSignalExecutionSplit(args, runtime));
    // Fail-closed frozen-config gate (clever-cabin-cdv): a stale whitelist
    // must never silently replace the champion-soak baseline.
    yield* assertFrozenWatchlistConfig(args);

    const repo = yield* MarketDataRepository;
    const paperRepo = yield* PaperTradingRepository;
    const initializePaperAccount = (): Effect.Effect<
      void,
      MarketDataRepositoryError | PaperTradingRepositoryError,
      never
    > =>
      Effect.gen(function* () {
        yield* repo.ensureTables();
        yield* paperRepo.ensureTables();
        if (args.replayBars > 0 && args.strategyType === "grid") {
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
      });

    yield* initializePaperAccount();

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

    // Ladder survivors were whitelisted from MAINNET db data, but the live
    // ladder soak reads the trading venue's klines (bybit testnet). Symbols
    // not listed on the venue burn every pass on "no candles" — filter them
    // out once at startup.
    const filterUnlistedLadderEntries = (
      list: readonly WatchlistEntry[],
    ): Effect.Effect<
      readonly WatchlistEntry[],
      never,
      MarketDataGatewayService
    > =>
      Effect.gen(function* () {
        const gateway = yield* MarketDataGateway;
        const venueList = yield* gateway
          .fetchSymbols(resolvedExchange)
          .pipe(Effect.orElseSucceed(() => [] as readonly string[]));
        if (venueList.length === 0) return list;
        const listed = new Set(
          venueList.map((s) => s.replace(/:.*$/, "").toLowerCase()),
        );
        const before = list.length;
        const filtered = list.filter(
          (e) =>
            !isLadderSurvivorRow(e.gridParams, args.strategyType) ||
            listed.has(e.symbol.replace(/:.*$/, "").toLowerCase()),
        );
        if (filtered.length !== before) {
          yield* Console.warn(
            `⚠️ Ladder whitelist filtered: ${before - filtered.length} symbol(s) not listed on ${resolvedExchange} — removed (${filtered.map((e) => e.symbol).join(", ")})`,
          );
        }
        return filtered;
      });
    const resolveEntries = (
      candidateEntries?: readonly WatchlistEntry[],
    ): Effect.Effect<
      readonly WatchlistEntry[] | undefined,
      never,
      MarketDataGatewayService
    > =>
      Effect.gen(function* () {
        const entries =
          candidateEntries && candidateEntries.length > 0
            ? candidateEntries
            : undefined;
        if (
          entries === undefined ||
          !entries.some((entry) =>
            isLadderSurvivorRow(entry.gridParams, args.strategyType),
          )
        ) {
          return entries;
        }
        return yield* filterUnlistedLadderEntries(entries);
      });
    const entries = yield* resolveEntries(args.entries);
    const activeEntries = entries;

    // A ladder watchlist is one paper account, not one $capital account per
    // symbol. Give every current survivor a stable cash partition and include
    // the manifest in the portfolio id so a watchlist change starts a new,
    // auditable cohort instead of silently summing old and new symbols.
    const ladderPortfolio = prepareLadderPortfolio(
      entries,
      args,
      resolvedExchange,
    );
    const ladderPortfolioRows = ladderPortfolio.rows;
    const ladderPortfolioId = ladderPortfolio.id;
    const ladderAllocationFor = (
      symbol: string,
      exchange: string,
    ): Decimal | undefined =>
      ladderPortfolio.allocations.get(
        `${resolveFuturesMarketExchange(exchange, true)}:${symbol}:${args.timeframe}`,
      );

    // Stamp every ladder trade this process records with readiness
    // provenance (fingerprint + cohort), mirroring the grid engine's
    // per-state fields. Without it ladder fills are untagged legacy rows the
    // real-money readiness gate can never count.
    stampLadderTradeProvenance(
      ladderPortfolio.entries,
      args,
      resolvedExchange,
      useTestnet,
      useSandbox,
    );

    // Account-level compounding: periodically re-derive each member's target
    // allocation from LIVE equity (winners fund losers' targets) and express
    // it as a maxPositionPct cap on sizing. initialCapital stays fixed so
    // persisted state never mismatches (configMatchesLadderState ignores
    // maxPositionPct). 0 disables; default every 24h.
    const ladderRebalanceMs = ladderRebalanceIntervalMs();
    const ladderLiveCapital = new Map<string, number>();
    let ladderRebalancePlan: ReadonlyMap<string, LadderRebalancePlan> =
      new Map();
    let ladderRebalanceAt = 0;
    const ladderRefreshRebalancePlan = (): string[] => {
      if (
        ladderRebalanceMs <= 0 ||
        Date.now() - ladderRebalanceAt < ladderRebalanceMs
      ) {
        return [];
      }
      // Partial coverage = some members have not reported live capital yet
      // (fresh start, skipped symbol, or a member that errored all cycle).
      // Re-deriving targets from a subset would silently mis-scale everyone,
      // so the plan no-ops — but LOUDLY, so the operator can fix the cause.
      const missing = ladderPortfolioRows
        .map((row) => row.key)
        .filter((key) => !ladderLiveCapital.has(key));
      if (missing.length > 0) {
        return [
          `[${new Date().toISOString()}] REBALANCE SKIPPED: ${missing.length}/${ladderPortfolioRows.length} member(s) missing live capital (${missing.join(", ")})`,
        ];
      }
      const plan = planLadderPortfolioRebalance(
        ladderPortfolioRows.map((row) => ({
          key: row.key,
          allocatedWeight: row.entry.gridParams?.allocatedWeight,
        })),
        ladderLiveCapital,
      );
      if (plan.size === 0) return [];
      ladderRebalancePlan = plan;
      ladderRebalanceAt = Date.now();
      return [...plan].map(
        ([key, entry]) =>
          `[${new Date().toISOString()}] REBALANCE ${key} target=${entry.targetAllocation.toFixed(2)} positionPct=${entry.positionPct.toFixed(1)}`,
      );
    };
    const ladderRebalanceCapFor = (key: string): number | undefined =>
      ladderRebalancePlan.get(key)?.positionPct;

    const makeSpotOptions = (
      symbol: string,
      exchange: string,
      overrides?: EngineSignalOverrides,
    ): PaperTradingOptions => ({
      exchange,
      symbol,
      timeframe: args.timeframe,
      composerConfig,
      positionSizePct: args.positionSize,
      riskPerTradePct: args.riskPerTrade,
      maxPositionSizePct: Option.getOrElse(args.maxPositionSizePct, () => 100),
      feePct: args.fee,
      minConfidence: overrides?.minConfidence ?? args.minConfidence,
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
      stopLossPct: args.stopLoss,
      takeProfitPct: args.takeProfit,
      holdUntilStop: args.holdUntilStop,
      minAtrPct: args.minAtrPct,
      initialCapital: args.capital,
      isLive: args.live,
      volatilityTargetAnnualPct: args.volatilityTargetAnnualPct,
    });

    // Live futures orders must respect the venue's contract size step and
    // minimums (a 5 USDT BTC order is 0.000077 BTC, below Bybit's 0.001
    // minimum). Fetch the relevant table once per command run and resolve
    // specs per symbol into the engine options. Simulated runs have no
    // exchange contract table — specs come only from tests/options.
    const contractSymbols = [
      args.symbol,
      ...(activeEntries ?? []).map((entry) => entry.symbol),
    ];
    const { bitgetContracts, bybitContracts, bybitContractMap } =
      yield* loadPaperTradeContracts(
        args,
        resolvedExchange,
        strategyType,
        productType,
        contractSymbols,
      );
    const contractSpecsFor = (symbol: string): ContractSizeSpec | undefined =>
      bybitContracts !== undefined
        ? bybitContractSpecs(bybitContractMap.get(toBybitSymbol(symbol)))
        : bitgetContracts === undefined
          ? undefined
          : bitgetContractSpecs(bitgetContracts, symbol, productType);

    // Futures data and execution both live on Bitget in this port; default the
    // market-data exchange to bitget-futures unless the operator overrides it.
    const makeFuturesOptions = (
      symbol: string,
      exchangeOverride: string,
      overrides?: EngineSignalOverrides,
    ): FuturesPaperTradingOptions => {
      const contractSpecs = contractSpecsFor(symbol);
      const options: MutableFuturesPaperTradingOptions = {
        exchange: resolveFuturesMarketExchange(exchangeOverride, true),
        symbol,
        timeframe: args.timeframe,
        composerConfig,
        positionSizePct: args.positionSize,
        riskPerTradePct: args.riskPerTrade,
        maxPositionSizePct: Option.getOrElse(
          args.maxPositionSizePct,
          () => 100,
        ),
        feePct: args.fee,
        minConfidence: overrides?.minConfidence ?? args.minConfidence,
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
        stopLossPct: args.stopLoss,
        takeProfitPct: args.takeProfit,
        holdUntilStop: args.holdUntilStop,
        minAtrPct: args.minAtrPct,
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
      };
      if (contractSpecs !== undefined) options.contractSpecs = contractSpecs;
      return options as FuturesPaperTradingOptions;
    };

    const makeGridOptions = (
      symbol: string,
      exchange: string,
      gridParams?: WatchlistEntry["gridParams"],
    ): GridPaperTradingOptions => {
      const contractSpecs = contractSpecsFor(symbol);
      const rowOverrides = gridOverridesFromWatchlistRow(gridParams, args);
      // Single frozen config path (clever-cabin-cdv): frozen CLI knobs win
      // under force-reseed; hold fail-closes (throws) on divergence.
      const frozenGeometry = resolveFrozenGridGeometry(
        gridParams,
        args,
        args.configMismatchAction,
      );
      const resolvedGridExchange = resolveFuturesMarketExchange(exchange, true);
      const options: MutableGridPaperTradingOptions = {
        exchange: resolvedGridExchange,
        symbol,
        timeframe: args.timeframe,
        gridStepPct: frozenGeometry.gridStepPct,
        gridMaxGrids: frozenGeometry.gridMaxGrids,
        gridPauseAfterLossBars: frozenGeometry.gridPauseAfterLossBars,
        feePct: args.fee,
        slippageBps: args.slippageBps,
        trendFilterPeriod: args.onlyWithTrend ? args.trendFilterPeriod : 0,
        initialCapital: args.capital,
        // Per-row: validated targetRatio/chopGateAdx from the watchlist row,
        // position sized by the row's allocatedWeight (see helper).
        maxPositionPct: rowOverrides.maxPositionPct,
        maxDrawdownPct: Option.getOrElse(args.maxDrawdownPct, () => 100),
        leverage: args.leverage,
        takerExitFeePct: args.takerExitFeePct,
        fundingRatePct8h: args.fundingRatePct8h,
        maintenanceMarginRate: args.maintenanceMarginRatePct / 100,
        onlyWithTrend: args.onlyWithTrend,
        // Frozen overlays (clever-cabin-cdv): under force-reseed an
        // explicitly pinned CLI knob wins over the row so the soak trades the
        // frozen baseline; otherwise the row's validated config governs.
        targetRatio:
          args.configMismatchAction === "force-reseed" &&
          isFrozenCliSet("targetRatio", args.targetRatio)
            ? (args.targetRatio as number)
            : rowOverrides.targetRatio,
        chopGateAdxThreshold:
          args.configMismatchAction === "force-reseed" &&
          isFrozenCliSet("chopGateAdx", args.chopGateAdx)
            ? (args.chopGateAdx as number)
            : rowOverrides.chopGateAdxThreshold,
        replayBars: args.replayBars > 0 ? args.replayBars : undefined,
        isLive: args.live,
        executionEnvironment: executionEnvironmentFor(
          resolvedGridExchange,
          args.live,
          resolvedGridExchange === "bybit-futures" ? useTestnet : useSandbox,
        ),
        productType,
        marginMode,
      };
      if (contractSpecs !== undefined) options.contractSpecs = contractSpecs;
      return options as GridPaperTradingOptions;
    };

    const makeLadderOptions = (
      symbol: string,
      exchange: string,
      gridParams?: WatchlistEntry["gridParams"],
      allocatedCapital?: Decimal,
      positionPctCap?: number,
    ): LadderPaperTradingOptions => {
      const resolvedLadderExchange = resolveFuturesMarketExchange(
        exchange,
        true,
      );
      const capitalPartition = allocatedCapital ?? money(args.capital);
      const rawWeight = capitalPartition.div(money(args.capital));
      const basePositionPct = Option.getOrElse(
        args.maxPositionSizePct,
        () => 100,
      );
      const {
        rungs,
        gridStepPct,
        gridMaxGrids,
        gridPauseAfterLossBars,
        targetRatio,
        chopGateAdxThreshold,
      } = resolveLadderGridSettings(
        gridParams,
        args,
        args.configMismatchAction,
      );
      const options: LadderPaperTradingOptions = {
        exchange: resolvedLadderExchange,
        symbol,
        timeframe: args.timeframe,
        rungs,
        gridStepPct,
        gridMaxGrids,
        gridPauseAfterLossBars,
        feePct: args.fee,
        slippageBps: args.slippageBps,
        liveEntryCrossBps: args.liveEntryCrossBps,
        demoLiveMarketEntries: args.demoLiveMarketEntries,
        trendFilterPeriod: args.onlyWithTrend ? args.trendFilterPeriod : 0,
        initialCapital: capitalPartition.toNumber(),
        leverage: args.leverage,
        onlyWithTrend: args.onlyWithTrend,
        targetRatio,
        chopGateAdxThreshold,
        maxHoldBars: args.maxHoldBars ?? 0,
        configMismatchAction: args.configMismatchAction,
        maxPositionDrawdownPct: args.maxPositionDrawdownPct,
        stopRatio: args.stopRatio,
        // Account-level realized drawdown kill (flatten + no new seeds).
        maxDrawdownPct: Option.getOrElse(args.maxDrawdownPct, () => 100),
        takerExitFeePct: args.takerExitFeePct,
        fundingRatePct8h: args.fundingRatePct8h,
        maintenanceMarginRate: args.maintenanceMarginRatePct / 100,
        replayBars: args.replayBars > 0 ? args.replayBars : undefined,
        forwardOnly: args.live || args.shadow === true,
        isLive: args.live,
        productType,
        marginMode,
        maxPositionPct: ladderPartitionPositionPct(
          rawWeight,
          basePositionPct,
          positionPctCap,
        ),
        // Keep gross market exposure bounded separately from the collateral
        // cap enforced by RiskGuard. The latter is leverage-aware; this cap is
        // not, so a future leverage change cannot silently expand notional.
        maxNotionalPct: 100,
        // Fully dynamic leverage: the engine sizes leverage from the account
        // size + per-position budget (accountScaledLeverageCap), ignoring any
        // static --leverage. No fixed leverage in the config.
        fullyDynamicLeverage: true,
        // The account-scaled cap is the real ceiling, but it is clamped to
        // NEURATRADE_MAX_LADDER_LEVERAGE (default 10x): the validated
        // candidates all run 1-2x, and the old hardcoded 150 let a large
        // account silently deploy leverage no candidate was ever validated
        // at. Raise the env deliberately after re-validating at higher lev.
        maxLeverage: Math.max(
          1,
          Number(process.env.NEURATRADE_MAX_LADDER_LEVERAGE ?? "10") || 10,
        ),
      };
      // The ladder can trade Bybit or Bitget. Only attach a spec when it came
      // from the same venue; passing Bitget rules to a Bybit ladder caused
      // false min-orderable rejections in the old path.
      const contractSpecs = contractSpecsFor(symbol);
      return resolvedLadderExchange === "bybit-futures" &&
        contractSpecs !== undefined
        ? { ...options, contractSpecs }
        : options;
    };

    const spotAdapterLayer = paperSpotAdapterLayer(args);
    const futuresAdapterLayer = paperFuturesAdapterLayer(args);

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
            const tag = err._tag;
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
            const reason = err.reason;
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

    const runLadderIteration = (
      opts: LadderPaperTradingOptions,
    ): Effect.Effect<LadderPaperIterationResult, never, never> =>
      runLadderPaperTradingIteration(opts).pipe(
        Effect.provide(futuresAdapterLayer),
        Effect.catch((err) =>
          Effect.gen(function* () {
            const state = yield* paperRepo
              .getLadderState(opts.exchange, opts.symbol, opts.timeframe)
              .pipe(Effect.orElseSucceed(() => null));
            yield* Console.error(
              `ladder iteration skipped (network/IO error): ${err.reason}`,
            );
            return {
              action: "hold" as const,
              capital: state ? toNumber(state.capital) : 0,
              peakCapital: state ? toNumber(state.peakCapital) : 0,
              openRungs: state
                ? state.longRungs.filter((r) => r.filled).length +
                  state.shortRungs.filter((r) => r.filled).length
                : 0,
              closedThisIteration: 0,
              note: `skip: ${err.reason}`,
            };
          }),
        ),
      ) as Effect.Effect<LadderPaperIterationResult, never, never>;

    let remaining = args.iterations;

    const runWatchlistSymbol = (input: {
      readonly entry: WatchlistEntry;
      readonly entryExchange: string;
      readonly entryKey: string;
      readonly isLadderSurvivor: boolean;
      readonly allocatedCapital?: Decimal;
    }): Effect.Effect<PaperIterationLogResult, never, never> =>
      Effect.gen(function* () {
        if (input.isLadderSurvivor) {
          return yield* runLadderIteration(
            makeLadderOptions(
              input.entry.symbol,
              input.entryExchange,
              input.entry.gridParams,
              input.allocatedCapital,
              ladderRebalanceCapFor(input.entryKey),
            ),
          );
        }
        if (args.strategyType === "grid") {
          return yield* runGridIteration(
            makeGridOptions(
              input.entry.symbol,
              input.entryExchange,
              input.entry.gridParams,
            ),
          );
        }
        if (args.futures) {
          return yield* runFuturesIteration(
            makeFuturesOptions(input.entry.symbol, input.entryExchange, {
              minConfidence: input.entry.bestParams?.minConfidence,
              atrStopMultiplier: input.entry.bestParams?.atrStopMultiplier,
              atrTakeProfitMultiplier:
                input.entry.bestParams?.atrTakeProfitMultiplier,
            }),
          );
        }
        return yield* runSpotIteration(
          makeSpotOptions(input.entry.symbol, input.entryExchange, {
            minConfidence: input.entry.bestParams?.minConfidence,
            atrStopMultiplier: input.entry.bestParams?.atrStopMultiplier,
            atrTakeProfitMultiplier:
              input.entry.bestParams?.atrTakeProfitMultiplier,
          }),
        );
      });

    const saveLadderMember = (input: {
      readonly entry: WatchlistEntry;
      readonly entryExchange: string;
      readonly entryKey: string;
      readonly isLadderSurvivor: boolean;
      readonly allocatedCapital?: Decimal;
      readonly result: PaperIterationLogResult;
    }): Effect.Effect<void, PaperTradingRepositoryError, never> =>
      Effect.gen(function* () {
        if (
          !input.isLadderSurvivor ||
          ladderPortfolioId === undefined ||
          input.allocatedCapital === undefined
        ) {
          return;
        }
        if (paperRepo.saveLadderPortfolioMember === undefined) return;
        const ladderResult = input.result as LadderPaperIterationResult;
        ladderLiveCapital.set(input.entryKey, ladderResult.capital);
        // Call through the repo object — extracting the method unbound loses `this.db`.
        yield* paperRepo.saveLadderPortfolioMember({
          portfolioId: ladderPortfolioId,
          exchange: resolveFuturesMarketExchange(input.entryExchange, true),
          symbol: input.entry.symbol,
          timeframe: args.timeframe,
          allocatedCapital: input.allocatedCapital,
          capital: money(ladderResult.capital),
          equity: money(ladderResult.equity ?? ladderResult.capital),
          unrealizedPnl: money(ladderResult.unrealizedPnl ?? 0),
          active: true,
          updatedAt: new Date(),
        });
      });

    const runWatchlistEntry = (
      entry: WatchlistEntry,
      cohortSymbols: ReadonlySet<string>,
    ): Effect.Effect<void, PaperTradingRepositoryError, never> =>
      Effect.gen(function* () {
        if (cohortSymbols.has(entry.symbol)) return;
        if (remaining === 0 && args.iterations !== 0) return;
        const entryExchange = entry.exchange ?? args.exchange;
        const isLadderSurvivor = isLadderSurvivorRow(
          entry.gridParams,
          args.strategyType,
        );
        const entryKey = `${resolveFuturesMarketExchange(entryExchange, true)}:${entry.symbol}:${args.timeframe}`;
        const allocatedCapital = isLadderSurvivor
          ? ladderAllocationFor(entry.symbol, entryExchange)
          : undefined;
        const iteration = {
          entry,
          entryExchange,
          entryKey,
          isLadderSurvivor,
          allocatedCapital,
        };
        const result = yield* runWatchlistSymbol(iteration);
        yield* Console.log(
          formatPaperIterationLog(`${entryExchange}:${entry.symbol} `, result),
        );
        yield* saveLadderMember({ ...iteration, result });
        if (remaining > 0 && args.iterations !== 0) remaining -= 1;
      });

    const saveLadderPortfolioSummary = (
      evaluated: number,
    ): Effect.Effect<void, PaperTradingRepositoryError, never> =>
      Effect.gen(function* () {
        if (
          ladderPortfolioId === undefined ||
          paperRepo.listLadderPortfolioMembers === undefined ||
          paperRepo.saveLadderPortfolioSummary === undefined
        ) {
          return;
        }
        const members =
          yield* paperRepo.listLadderPortfolioMembers(ladderPortfolioId);
        const previous =
          paperRepo.getLadderPortfolioSummary !== undefined
            ? yield* paperRepo.getLadderPortfolioSummary(ladderPortfolioId)
            : null;
        const summary = summarizeLadderPortfolio(
          ladderPortfolioId,
          resolvedExchange,
          args.timeframe,
          money(args.capital),
          members,
          previous?.peakEquity,
        );
        yield* paperRepo.saveLadderPortfolioSummary(summary);
        yield* Console.log(
          `[${new Date().toISOString()}] PORTFOLIO ${ladderPortfolioId} | capital=${summary.capital.toFixed(2)} | equity=${summary.equity.toFixed(2)} | uPnL=${summary.unrealizedPnl.toFixed(4)} | symbols=${summary.activeSymbols} | evaluated=${evaluated}`,
        );
      });

    const runWatchlistSweep = (): Effect.Effect<
      void,
      PaperTradingRepositoryError,
      never
    > =>
      Effect.gen(function* () {
        if (!activeEntries) return;
        // Candidate soaks own readiness-cohort symbols in the shared default
        // home. Explicit --watchlist soaks (champion paper/demo) own their
        // full symbol set — do not skip BTC/ETH/SOL there.
        const cohortSymbols = Option.isSome(args.watchlist)
          ? new Set<string>()
          : new Set<string>(
              READINESS_COHORT_CANDIDATES.map((candidate) => candidate.symbol),
            );
        for (const line of ladderRefreshRebalancePlan()) {
          yield* Console.log(line);
        }
        const results = yield* Effect.forEach(
          activeEntries,
          (entry) => runWatchlistEntry(entry, cohortSymbols),
          { concurrency: 16 },
        );
        yield* saveLadderPortfolioSummary(results.length);
        if (args.iterations === 0 || remaining !== 0) {
          yield* Effect.sleep(`${args.interval} seconds`);
        }
      });

    const runSingleSymbolIteration = (): Effect.Effect<void, never, never> =>
      Effect.gen(function* () {
        const result = yield* args.strategyType === "grid"
          ? runGridIteration(makeGridOptions(args.symbol, args.exchange))
          : args.futures
            ? runFuturesIteration(
                makeFuturesOptions(args.symbol, args.exchange),
              )
            : runSpotIteration(makeSpotOptions(args.symbol, args.exchange));
        yield* Console.log(formatPaperIterationLog("", result));
        if (remaining > 0) remaining -= 1;
        if (args.iterations === 0 || remaining !== 0) {
          yield* Effect.sleep(`${args.interval} seconds`);
        }
      });

    const printRecentClosedTrades = (): Effect.Effect<
      void,
      PaperTradingRepositoryError,
      never
    > =>
      Effect.gen(function* () {
        const closedTrades = yield* paperRepo.listRecentTrades(5);
        if (closedTrades.length === 0) return;
        yield* Console.log("\nRecent closed trades:");
        for (const trade of closedTrades) {
          yield* Console.log(
            `  ${trade.side} ${trade.entryPrice.toFixed(2)} → ${trade.exitPrice.toFixed(2)} | PnL ${trade.pnlPct.toFixed(2)}% | ${trade.exitReason}`,
          );
        }
      });

    // iterations=0 means run forever.
    while (args.iterations === 0 || remaining !== 0) {
      if (activeEntries) {
        yield* runWatchlistSweep();
      } else {
        yield* runSingleSymbolIteration();
      }
    }

    yield* printRecentClosedTrades();
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

function resolveSoakSpotAdapter(mergedArgs: SoakArgs) {
  if (!mergedArgs.live) return SimulatedExchangeAdapterLive();
  return BinanceLiveExchangeAdapterLive({
    apiKey: mergedArgs.apiKey || process.env.BINANCE_API_KEY || "",
    apiSecret: mergedArgs.apiSecret || process.env.BINANCE_API_SECRET || "",
  });
}

function resolveSoakFuturesAdapter(mergedArgs: SoakArgs) {
  if (!mergedArgs.live) return SimulatedFuturesExchangeAdapterLive();
  if (
    resolveFuturesMarketExchange(mergedArgs.exchange, true) === "bybit-futures"
  ) {
    return BybitFuturesExchangeAdapterLive.pipe(
      Layer.provide(BybitClientLiveConfig),
      Layer.provide(BybitConfigLive),
    );
  }
  return BitgetFuturesExchangeAdapterLive.pipe(
    Layer.provide(BitgetClientLiveConfig),
  );
}

function resolveSoakSignalOverrides(
  bestParams: SoakSymbol["bestParams"],
  mergedArgs: SoakArgs,
) {
  return {
    minConfidence: bestParams?.minConfidence ?? mergedArgs.minConfidence,
    useAtrStops:
      bestParams?.atrStopMultiplier !== undefined
        ? true
        : mergedArgs.useAtrStops,
    atrStopMultiplier:
      bestParams?.atrStopMultiplier ?? mergedArgs.atrStopMultiplier,
    atrTakeProfitMultiplier:
      bestParams?.atrTakeProfitMultiplier ?? mergedArgs.atrTakeProfitMultiplier,
  };
}

function resolveSoakExecutionOverrides(
  entry: SoakSymbol | undefined,
  mergedArgs: SoakArgs,
  marginModeParsed: FuturesMarginMode,
  productTypeParsed: BitgetProductType,
) {
  return {
    leverage: entry?.leverage ?? mergedArgs.leverage,
    marginMode: entry?.marginMode ?? marginModeParsed,
    productType: entry?.productType ?? productTypeParsed,
  };
}

function loadSoakContracts(
  mergedArgs: SoakArgs,
  watchlistEntries: readonly SoakWatchlistFileEntry[],
  productTypeParsed: BitgetProductType,
) {
  // Live futures orders must respect the exchange's contract size step
  // and minimums; fetch the contract table once per soak run and resolve
  // specs per symbol into the engine options.
  if (!mergedArgs.live) return Effect.succeed(undefined);
  if (
    !mergedArgs.futures &&
    !watchlistEntries.some((e) => e.productType !== undefined)
  ) {
    return Effect.succeed(undefined);
  }
  return fetchBitgetContracts(productTypeParsed);
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
    maxHoldBars: maxHoldBarsOption,
    volatilityTargetAnnualPct: volatilityTargetAnnualPctOption,
    profile: profileOption,
  },
  (args) =>
    Effect.gen(function* () {
      const path = yield* Path;
      const sqlite = yield* SqliteClient;
      const db = sqlite.database;

      const profile = yield* loadProfileIfNeeded(path.homeDir, args.profile);
      const mergedArgs = resolveSoakArgs(args, profile);

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
      const soakRiskOverrides = buildRiskOverrides(mergedArgs);
      // The ladder uses dynamic leverage up to the account-scaled cap (150x
      // configured); the guard must not block those higher-leverage fills. The
      // real cap is still applied by ladderRungQty (small account -> low cap).
      const riskGuardLayer = RiskGuardLive(mergedArgs.live, {
        ...soakRiskOverrides,
        maxLeverage: 150,
      });
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

      const spotAdapterLayer = resolveSoakSpotAdapter(mergedArgs);
      const futuresAdapterLayer = resolveSoakFuturesAdapter(
        mergedArgs,
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

      const contracts = yield* loadSoakContracts(
        mergedArgs,
        watchlistEntries,
        productTypeParsed,
      );

      const runSoakFuturesIteration = (
        symbol: string,
        exchange: string,
        entry: SoakSymbol | undefined,
        bestParams?: SoakSymbol["bestParams"],
      ): Effect.Effect<IterationResult, unknown, never> => {
        const opts: MutableFuturesPaperTradingOptions = {
          exchange,
          symbol,
          timeframe: mergedArgs.timeframe,
          composerConfig,
          positionSizePct: mergedArgs.positionSize,
          riskPerTradePct: mergedArgs.riskPerTrade,
          maxPositionSizePct: mergedArgs.maxPositionSize,
          feePct: mergedArgs.fee,
          ...resolveSoakSignalOverrides(bestParams, mergedArgs),
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
          ...resolveSoakExecutionOverrides(
            entry,
            mergedArgs,
            marginModeParsed,
            productTypeParsed,
          ),
          volatilityTargetAnnualPct: mergedArgs.volatilityTargetAnnualPct,
        };
        if (contracts !== undefined) {
          opts.contractSpecs = bitgetContractSpecs(
            contracts,
            symbol,
            entry?.productType ?? productTypeParsed,
          );
        }
        return runFuturesPaperTradingIteration(
          opts as FuturesPaperTradingOptions,
        ).pipe(
          Effect.provide(futuresAdapterLayer),
          Effect.provide(layers),
          Effect.map((r): IterationResult => ({
            action: r.action,
            capital: r.capital,
            note: r.note,
          })),
        ) as Effect.Effect<IterationResult, unknown, never>;
      };

      const runSoakSpotIteration = (
        symbol: string,
        exchange: string,
        bestParams?: SoakSymbol["bestParams"],
      ): Effect.Effect<IterationResult, unknown, never> => {
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

      const runner = (
        symbol: string,
        exchange: string,
        bestParams?: SoakSymbol["bestParams"],
      ): Effect.Effect<IterationResult, unknown, never> => {
        const entry = soakWatchlist.find((item) => item.symbol === symbol);
        const useFutures =
          entry?.productType !== undefined || mergedArgs.futures;
        const futuresExchange =
          useFutures && exchange === "binance" ? "bitget-futures" : exchange;
        return useFutures
          ? runSoakFuturesIteration(symbol, futuresExchange, entry, bestParams)
          : runSoakSpotIteration(symbol, exchange, bestParams);
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

/**
 * Candidate vector layout: [0]=stop, [1]=take-profit, [2]=minConfidence,
 * [3]=breakevenAtR, [4]=maxBarsInTrade, [5]=lossCooldownBars, [6]=adxMin,
 * [7]=minEfficiencyRatio, [8]=rsiLongMax, [9]=rsiShortMin,
 * [10]=entryOrderTypeIndex, [11]=entryLimitOffsetBps (only when length>=12).
 */
export function buildCandidate(
  useAtrStops: boolean,
  vector: readonly number[],
): OptimizeCandidateParams {
  return {
    useAtrStops,
    ...stopTakeProfitFromVector(useAtrStops, vector),
    ...coreThresholdsFromVector(vector),
    ...rsiBoundsFromVector(vector),
    ...entryOrderFromVector(vector),
  };
}

function stopTakeProfitFromVector(
  useAtrStops: boolean,
  vector: readonly number[],
): Pick<
  OptimizeCandidateParams,
  "stopMult" | "tpMult" | "stopLossPct" | "takeProfitPct"
> {
  return {
    stopMult: useAtrStops ? vector[0] : 0,
    tpMult: useAtrStops ? vector[1] : 0,
    stopLossPct: useAtrStops ? 0 : vector[0],
    takeProfitPct: useAtrStops ? 0 : vector[1],
  };
}

function coreThresholdsFromVector(
  vector: readonly number[],
): Pick<
  OptimizeCandidateParams,
  | "minConfidence"
  | "breakevenAtR"
  | "maxBarsInTrade"
  | "lossCooldownBars"
  | "adxMin"
  | "minEfficiencyRatio"
> {
  return {
    minConfidence: vector[2] ?? 0.5,
    breakevenAtR: vector[3] ?? 0,
    maxBarsInTrade: vector[4] ?? 0,
    lossCooldownBars: vector[5] ?? 0,
    adxMin: vector[6] ?? 0,
    minEfficiencyRatio: vector[7] ?? 0,
  };
}

function rsiBoundsFromVector(
  vector: readonly number[],
): Pick<OptimizeCandidateParams, "rsiLongMax" | "rsiShortMin"> {
  return {
    rsiLongMax: vector[8] ?? 0,
    rsiShortMin: vector[9] ?? 0,
  };
}

function entryOrderFromVector(
  vector: readonly number[],
): Pick<OptimizeCandidateParams, "entryOrderType" | "entryLimitOffsetBps"> {
  const hasEntryOrder = vector.length >= 12;
  const entryOrderTypeIndex = hasEntryOrder ? vector[10] : 0;
  return {
    entryOrderType: entryOrderTypeIndex < 0.5 ? "market" : "limit",
    entryLimitOffsetBps: hasEntryOrder ? vector[11] : 0,
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
  const vectors = topUpRandomVectors(
    args,
    useAtrStops,
    gridSearchVectors(args, useAtrStops),
  );
  return vectors.map((v) => buildCandidate(useAtrStops, v));
}

function gridSearchVectors(
  args: OptimizeArgs,
  useAtrStops: boolean,
): number[][] {
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

  return cartesianProduct(dimensions);
}

function topUpRandomVectors(
  args: OptimizeArgs,
  useAtrStops: boolean,
  vectors: number[][],
): number[][] {
  if (args.randomSearch <= 0) return vectors;
  let pool = vectors;
  if (pool.length > args.randomSearch) {
    const shuffled = [...pool].sort(() => Math.random() - 0.5);
    pool = shuffled.slice(0, args.randomSearch);
  } else {
    // Sample additional random candidates within the search bounds.
    while (pool.length < args.randomSearch) {
      pool.push(randomVectorWithinBounds(args, useAtrStops));
    }
  }
  return pool;
}

function randomVectorWithinBounds(
  args: OptimizeArgs,
  useAtrStops: boolean,
): number[] {
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
    Math.floor(randomInRange(args.maxBarsInTradeMin, args.maxBarsInTradeMax)),
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
  return randomVector;
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
  const overrides: Record<
    string,
    ResolvedBacktestArgs[keyof ResolvedBacktestArgs]
  > = {};
  for (const key of Object.keys(args) as Array<keyof ResolvedBacktestArgs>) {
    const value = args[key];
    const defaultValue = defaults[key];
    if (value !== defaultValue) {
      overrides[key] = value;
    }
  }
  // Behavioral flags are always explicit so profiles preserve user intent.
  if (args.observedPrice !== undefined)
    overrides.observedPrice = args.observedPrice;
  if (args.realistic !== undefined) overrides.realistic = args.realistic;
  if (args.strictRealism !== undefined)
    overrides.strictRealism = args.strictRealism;
  return overrides as Partial<ResolvedBacktestArgs>;
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
      const template = args.strategy;
      const baseArgs = args as ResolvedBacktestArgs;
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

      const resolvedArgs = args as Omit<ResolvedBacktestArgs, "minTrades">;
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
type ProbeVerdict =
  | { readonly kind: "untradeable"; readonly evidence: string }
  | { readonly kind: "transient"; readonly reason: string };

/** Classify a failed leverage probe: unsupported instrument vs transient. */
export function classifyProbeOutcome(
  failure: BitgetClientError,
  symbol: string,
): ProbeVerdict {
  if (
    failure instanceof BitgetApiError &&
    (isBitgetUnsupportedInstrumentError(failure) ||
      probeNamesProbedSymbol(failure, symbol, "USDT-FUTURES"))
  ) {
    return {
      kind: "untradeable",
      evidence: `Bitget ${failure.code ?? "?"}: ${failure.body.slice(0, 140)}`,
    };
  }
  const reason =
    failure instanceof BitgetApiError
      ? `BitgetApiError code=${failure.code ?? "-"}: ${failure.body.slice(0, 140)}`
      : failure instanceof Error
        ? failure.message
        : String(failure);
  return { kind: "transient", reason };
}

export function formatUniverseScanEntry(e: GridUniverseEntry): string {
  const mark = e.gatedDropped ? " ✘" : e.passed ? " ✔" : "";
  const reason = e.rejectionReason ? ` [${e.rejectionReason}]` : "";
  const gateReasons = e.gateFailureReasons?.length
    ? ` gate=${e.gateFailureReasons.join("|")}`
    : "";
  return (
    `${e.symbol.padEnd(13)} ${String(e.candles).padStart(7)}  ` +
    `${e.bestParams.gridStepPct.toFixed(2).padStart(5)}  ` +
    `${String(e.bestParams.gridMaxGrids).padStart(5)}  ` +
    `${String(e.bestParams.gridPauseAfterLossBars).padStart(5)}  ` +
    `${e.walkForward.profitableWindowsPct.toFixed(0).padStart(6)}%  ` +
    `${e.walkForward.aggregateReturnPct.toFixed(2).padStart(9)}%${mark}${reason}${gateReasons}`
  );
}

/** Parse --rungs into rung counts; absent means the default 1-3 rung sweep. */
export function parseScanRungs(
  rungs: Option.Option<string>,
): readonly number[] {
  if (Option.isNone(rungs)) return [1, 2, 3];
  return rungs.value
    .split(",")
    .map((token) => Number(token.trim()))
    .filter((n) => Number.isInteger(n) && n >= 1);
}

function logScanNotices(args: {
  readonly engine: string;
  readonly tier: string;
  readonly minFillFrequencyPct: number;
}) {
  return Effect.gen(function* () {
    if (args.engine === "ladder" && args.tier === "readiness") {
      // Ladder readiness is gated by the ladder evidence validator inside
      // ladderGateScoredEligibility (data quality, historical windows,
      // fixed-OOS, block-bootstrap confidence, pooled adverse-stress LB);
      // a survivor that clears it is admissible to the readiness board.
      yield* Console.log(
        `🧪 Ladder readiness tier: gate-scoring through the ladder evidence validator (this is slower than fast tier — every combo runs the 5-seed adverse-stress protocol)`,
      );
    }
    if (args.minFillFrequencyPct <= 0) {
      yield* Console.warn(
        `⚠️ --min-fill-frequency-pct is 0 — the fill gate is DISABLED; survivors whose grid step is too wide to fill live will not be rejected`,
      );
    }
  });
}

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
    engine: gridUniverseEngineOption,
    rungs: gridUniverseRungsOption,
    ladderStopRatio: stopRatioOption,
    ladderMaxHoldBars: maxHoldBarsOption,
  },
  (args) =>
    Effect.gen(function* () {
      const scanMode = ():
        | {
            readonly ok: true;
            readonly tier: "readiness" | "fast";
            readonly engine: "grid" | "ladder";
            readonly dataSource: "gateway" | "db-mainnet";
          }
        | { readonly ok: false; readonly error: string } => {
        if (args.tier !== "readiness" && args.tier !== "fast") {
          return {
            ok: false,
            error: `invalid --tier '${args.tier}': expected 'readiness' or 'fast'`,
          };
        }
        if (args.dataSource !== "gateway" && args.dataSource !== "db-mainnet") {
          return {
            ok: false,
            error: `invalid --data-source '${args.dataSource}': expected 'gateway' or 'db-mainnet'`,
          };
        }
        if (args.dataSource === "db-mainnet" && !args.market) {
          // The DB-sourced (non-market) scan reads candles at the scan
          // timeframe — for bybit-futures those 15m rows are TESTNET-native.
          // db-mainnet must go through the market scan so candles come from
          // the resampled 5m mainnet cache instead.
          return {
            ok: false,
            error: `--data-source db-mainnet requires --market (db-mainnet candles come from the 5m mainnet DB cache via the market scan)`,
          };
        }
        if (args.engine !== "grid" && args.engine !== "ladder") {
          return {
            ok: false,
            error: `invalid --engine '${args.engine}': expected 'grid' or 'ladder'`,
          };
        }
        return {
          ok: true,
          tier: args.tier,
          engine: args.engine,
          dataSource: args.dataSource,
        };
      };
      const mode = scanMode();
      if (!mode.ok) {
        return yield* Effect.fail(new Error(mode.error));
      }
      if (args.engine === "ladder" && args.minCandles < LADDER_GATE_TAIL) {
        return yield* Effect.fail(
          new Error(
            `ladder scans require at least ${LADDER_GATE_TAIL} candles (~21 days at 15m); received ${args.minCandles}. Increase --min-candles before creating a paper whitelist`,
          ),
        );
      }
      const rungs = parseScanRungs(args.rungs);
      if (rungs.length === 0) {
        return yield* Effect.fail(
          new Error(
            `invalid --rungs '${args.rungs}': expected comma-separated integers >= 1`,
          ),
        );
      }
      yield* logScanNotices(args);
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
        searchSpace: { ...DEFAULT_GRID_UNIVERSE_SEARCH_SPACE, rungs },
        tier: mode.tier,
        engine: mode.engine,
        // db-mainnet evaluates on mainnet-fidelity candles; its fills are
        // modeled conservatively by default (a wick touch is not a fill).
        dataSource: mode.dataSource,
        fillModel: mode.dataSource === "db-mainnet" ? "conservative" : "wick",
        ladderStopRatio: args.ladderStopRatio,
        ladderMaxHoldBars: args.ladderMaxHoldBars,
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

          if (args.engine === "ladder") {
            // Ladder survivors are NOT written to the DB watchlist (that feed
            // gates the single-position readiness cohort, and the readiness
            // tier fails closed for ladder). The demo soak trades these rows
            // directly from the ladder whitelist file, which carries the rungs,
            // via the incremental ladder paper engine (--watchlist
            // grid-whitelist-ladder.json).
            yield* Console.log(
              `💾 Ladder scan: ${selected.length} survivors reported to the whitelist file (NOT the DB watchlist — readiness tier fails closed for ladder); demo soak can trade them via --watchlist grid-whitelist-ladder.json`,
            );
          } else {
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
          }

          // Only ever write the whitelist file after a successful scan with
          // selected rows: a failed/empty scan must not truncate the file the
          // demo soak consumes, and unselected eligible rows must not bypass
          // the account-sized portfolio cap.
          if (outputPath && selected.length > 0) {
            const watchlistJson = selected.map((e) => ({
              symbol: e.symbol,
              exchange: args.exchange,
              returnPct: e.walkForward.aggregateReturnPct,
              gridParams: {
                gridStepPct: e.bestParams.gridStepPct,
                gridMaxGrids: e.bestParams.gridMaxGrids,
                gridPauseAfterLossBars: e.bestParams.gridPauseAfterLossBars,
                rungs: e.bestParams.rungs,
                // Carry the stage-4 gate-validated dials so the ladder soak
                // trades the geometry that cleared walk-forward + time-split
                // evidence — NOT the CLI default targetRatio=1 (which caps
                // wins at one grid step and inverts the ladder's R:R into
                // an ~82%-win-rate-to-break-even bleed on tight steps).
                targetRatio: e.validatedTargetRatio ?? 1,
                chopGateAdx: e.validatedChopGateAdx ?? 0,
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

      const filterTradeableSurvivors = (
        survivors: readonly GridUniverseEntry[],
      ) =>
        Effect.gen(function* () {
          if (survivors.length === 0) return [...survivors];
          if (args.exchange.toLowerCase() !== "bitget-futures") {
            yield* Console.log(
              `🎯 Probe skipped: ${args.exchange} universe is sourced from its demo/tradeable instrument list`,
            );
            return [...survivors];
          }

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
              continue;
            }
            const verdict = classifyProbeOutcome(probe.failure, entry.symbol);
            if (verdict.kind === "untradeable") {
              yield* Console.log(
                `🎯 Dropped ${entry.symbol}: not tradeable on ${args.exchange} demo (${verdict.evidence})`,
              );
            } else {
              yield* Console.log(
                `⚠️ Keep ${entry.symbol}: probe failed transiently (${verdict.reason})`,
              );
              tradeable.push(entry);
            }
          }
          if (tradeable.length < survivors.length) {
            yield* Console.log(
              `🎯 Filtered ${survivors.length - tradeable.length} survivors not tradeable in demo`,
            );
          }
          return tradeable;
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

          const symbolFilteredSurvivors =
            rawResult.survivors.length > 0 && futuresSet.size > 0
              ? rawResult.survivors.filter((e) => isFuturesSymbol(e.symbol))
              : rawResult.survivors;
          const survivors = yield* filterTradeableSurvivors(
            symbolFilteredSurvivors,
          );
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
            yield* Console.log(formatUniverseScanEntry(e));
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

interface FlowBacktestInputArgs {
  readonly symbols: string;
  readonly holdTimes: string;
  readonly zMode: string;
  readonly start: string;
  readonly end: string;
}

interface FlowBacktestInput {
  readonly symbols: readonly string[];
  readonly holdTimes: readonly number[];
  readonly start: Date;
  readonly end: Date;
  readonly zMode: FlowBacktestOptions["zMode"];
}

function resolveFlowBacktestInput(
  args: FlowBacktestInputArgs,
): Effect.Effect<FlowBacktestInput, MarketDataRepositoryError> {
  const symbols = args.symbols
    .split(",")
    .map((s) => s.trim())
    .filter((s) => s.length > 0);
  const holdTimes = args.holdTimes
    .split(",")
    .map((s) => Number.parseFloat(s.trim()))
    .filter((n) => Number.isFinite(n) && n > 0);
  if (args.zMode !== "per-symbol" && args.zMode !== "cross-sectional") {
    return Effect.fail(
      new MarketDataRepositoryError(
        `Invalid --z-mode '${args.zMode}': expected per-symbol or cross-sectional`,
      ),
    );
  }
  if (holdTimes.length === 0) {
    return Effect.fail(
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
  // Validated above: only the two literals reach here.
  const zMode: FlowBacktestOptions["zMode"] =
    args.zMode === "cross-sectional" ? "cross-sectional" : "per-symbol";
  return Effect.succeed({ symbols, holdTimes, start, end, zMode });
}

interface FlowBacktestOptionsArgs {
  readonly fee: number;
  readonly spreadBps: number;
  readonly threshold: number;
  readonly stopMult: number;
  readonly conservativeFillRate: number;
  readonly maxBreakevenWinRate: number;
}

function buildFlowBacktestOptions(
  args: FlowBacktestOptionsArgs,
  holdTimes: readonly number[],
  zMode: FlowBacktestOptions["zMode"],
): FlowBacktestOptions {
  return {
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
    zMode,
    stopMultiplier: args.stopMult > 0 ? args.stopMult : null,
    conservativeFillRate: Math.max(0, Math.min(1, args.conservativeFillRate)),
    maxBreakevenWinRate: args.maxBreakevenWinRate,
  };
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
      const { symbols, holdTimes, start, end, zMode } =
        yield* resolveFlowBacktestInput(args);

      const options = buildFlowBacktestOptions(args, holdTimes, zMode);

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
        const variantPlaceholders = symbolVariants.map(() => "?").join(",");
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
        const [tickers, rawInstruments] = yield* Effect.all([
          fetchTickers(baseUrl),
          fetchInstruments(baseUrl),
        ]);
        const tickerBySymbol = new Map(
          tickers.map((ticker) => [ticker.symbol, ticker]),
        );
        volumes = Object.fromEntries(
          tickers.map((ticker) => [ticker.symbol, ticker.turnover24h]),
        );
        instruments = rawInstruments.map((instrument) => {
          const ticker = tickerBySymbol.get(instrument.symbol);
          return ticker === undefined
            ? instrument
            : {
                ...instrument,
                bid1Price: ticker.bid1Price,
                ask1Price: ticker.ask1Price,
              };
        });
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
        "Comma-separated Bybit USDT-perp symbols (default: flow universe top-100 or fallback set)",
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
            const tag = err._tag;
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
            const reason = err.reason;
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

const timesFmForecastCommand = makeTimesFmForecastCommand(
  makeDbLayer(process.env.NEURATRADE_HOME),
);

export const scalpCommand = Command.make("scalp", {}, () =>
  Console.log(
    "Scalping commands. Use 'scalp backtest|optimize|scan|paper-trade|soak|profile|readiness|demo-readiness|bybit-snapshot|timesfm-forecast|parity-replay|flow-backtest|flow-universe|flow-record|flow-trade --help' for details.",
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
    bybitSnapshotCommand,
    timesFmForecastCommand,
    parityReplayCommand,
    gridUniverseScanCommand,
    watchlistCommand,
    flowBacktestCommand,
    flowUniverseCommand,
    flowRecordCommand,
    flowTradeCommand,
    tradeCommand,
  ]),
);
