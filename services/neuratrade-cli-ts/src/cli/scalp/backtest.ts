// Backtest cluster: profile loading, composer-config builder, backtestCommand (candle
// range, HTF/funding loaders, MC attach, export/print helpers).
// Extracted from the scalp.ts god-module; behavior unchanged.
import { Command, Options } from "../kit/kit.ts";
import { Console, Effect, FileSystem, Option } from "effect";
import { dirname, isAbsolute, resolve } from "node:path";
import { Path } from "../../services/path.js";
import { SqliteClient } from "../../services/sqlite.js";
import {
  MarketDataRepository,
  MarketDataRepositoryError,
  MarketDataRepositorySQLiteLive,
  type MarketDataRepositoryService,
} from "../../market-data/repository.js";
import { defaultComposerConfig } from "../../scalping/composer.js";
import { CandleLike, ComposerConfig } from "../../scalping/types.js";
import {
  attachMonteCarlo,
  runBacktest,
  splitCandlesByOos,
  type BacktestResult,
  type BacktestTrade,
} from "../../scalping/backtest.js";
import { GridResult, GridTrade } from "../../scalping/grid.js";
import { computePerformanceMetrics } from "../../scalping/performance-metrics.js";
import { BacktestEngine } from "../../scalping/services.js";
import { FundingRate } from "../../market-data/types.js";
import {
  findSymbolOverride,
  loadStrategyProfile,
  resolveBacktestArgs,
  type ResolvedBacktestArgs,
  type StrategyProfile,
} from "../../scalping/strategy-profile.js";
import {
  buildBacktestArgsFromTemplate,
  buildComposerConfigFromTemplate,
  type StrategyTemplateName,
} from "../../scalping/strategy-library.js";
import {
  exchangeOption,
  symbolOption,
  timeframeOption,
  startOption,
  endOption,
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
} from "../scalp-options/index.js";
import { makeDbLayer } from "./shared.js";

export function loadProfileIfNeeded(
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

export const backtestOptions = {
  exchange: exchangeOption,
  symbol: symbolOption,
  timeframe: timeframeOption,
  start: startOption,
  end: endOption,
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

export function requireBacktestCandleRange(args: ResolvedBacktestArgs) {
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
  result: import("../../scalping/backtest.js").BacktestResult,
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
  result: import("../../scalping/backtest.js").BacktestResult,
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
): import("../../scalping/backtest.js").BacktestResult {
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
