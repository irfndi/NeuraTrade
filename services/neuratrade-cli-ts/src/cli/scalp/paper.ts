// Paper-trade + soak cluster: live-validation gates, adapter/options builders,
// ladder provenance + rebalance plumbing, paperTradeCommand and soakCommand.
// Extracted from the scalp.ts god-module; behavior unchanged.
//
// NOTE on the ../scalp.js imports: buildBacktestComposerConfig /
// loadProfileIfNeeded / WatchlistEntry still live in the remaining clusters
// of ../scalp.ts, and scalp.ts imports this module's commands in return; the
// cycle is safe because this module's top level evaluates only Command.make
// literals — every ../scalp.js binding is touched lazily inside function
// bodies.
import { Command } from "../kit/kit.ts";
import { BunServices } from "@effect/platform-bun";
import { Console, Effect, Layer, Option } from "effect";
import { resolve } from "node:path";
import { Path, PathLive } from "../../services/path.js";
import { SqliteClient } from "../../services/sqlite.js";
import {
  MarketDataRepository,
  MarketDataRepositoryError,
  MarketDataRepositorySQLiteLive,
} from "../../market-data/repository.js";
import { MarketDataGatewayLive } from "../../market-data/gateways/index.js";
import {
  MarketDataGateway,
  type MarketDataGatewayService,
} from "../../market-data/gateway.js";
import { MarketDataGatewayRepositoryLive } from "../../market-data/gateway-repository.js";
import { SimulatedExchangeAdapterLive } from "../../exchange/adapters/simulated.js";
import { BinanceLiveExchangeAdapterLive } from "../../exchange/adapters/binance-live.js";
import { SimulatedFuturesExchangeAdapterLive } from "../../exchange/adapters/simulated-futures.js";
import { BitgetFuturesExchangeAdapterLive } from "../../exchange/adapters/bitget-futures.js";
import {
  BybitFuturesExchangeAdapterLive,
  BybitClient,
  BybitClientLiveConfig,
  toBybitSymbol,
  type BybitContract,
} from "../../exchange/adapters/bybit-futures.js";
import {
  BitgetClientLiveConfig,
  BitgetClient,
  toBitgetFuturesSymbol,
  type BitgetContract,
} from "../../services/bitget-client.js";
import {
  BitgetConfig,
  BitgetConfigLive,
} from "../../services/bitget-config.js";
import { BybitConfig, BybitConfigLive } from "../../services/bybit-config.js";
import { RateLimiterLive } from "../../services/rate-limiter.js";
import { FuturesMarginMode } from "../../exchange/futures-adapter.js";
import { type FuturesExchangeAdapterService } from "../../exchange/futures-adapter.js";
import { RiskGuard, RiskGuardLive } from "../../risk/guards.js";
import { KillSwitch, KillSwitchSQLiteLive } from "../../risk/kill-switch.js";
import { CircuitBreakerSQLiteLive } from "../../risk/circuit-breaker.js";
import { Decimal, money, toNumber } from "../../utils/money.js";
import {
  runPaperTradingIteration,
  type PaperTradingOptions,
} from "../../paper-trading/engine.js";
import {
  runFuturesPaperTradingIteration,
  type FuturesPaperTradingOptions,
} from "../../paper-trading/futures-engine.js";
import {
  runGridPaperTradingIteration,
  type GridPaperTradingOptions,
  type GridPaperTradingIterationResult,
} from "../../paper-trading/grid-engine.js";
import {
  runLadderPaperTradingIteration,
  setLadderTradeProvenance,
  type LadderPaperIterationResult,
  type LadderPaperTradingOptions,
} from "../../paper-trading/ladder-engine.js";
import {
  allocateLadderPortfolioCapital,
  planLadderPortfolioRebalance,
  type LadderRebalancePlan,
  summarizeLadderPortfolio,
} from "../../paper-trading/ladder-portfolio.js";
import {
  DEFAULT_STRATEGY_MANIFEST,
  fingerprintStrategyManifest,
  type StrategyManifest,
} from "../../scalping/real-money-readiness.js";
import { ContractSizeSpec } from "../../paper-trading/types.js";
import { BitgetProductType } from "../../services/bitget-client.js";
import {
  PaperTradingRepository,
  PaperTradingRepositorySQLiteLive,
  type PaperTradingRepositoryError,
} from "../../paper-trading/repository.js";
import {
  runSoak,
  type SoakOptions,
  type SoakSymbol,
  type IterationResult,
} from "../../scalping/soak.js";
import {
  findSymbolOverride,
  type ResolvedBacktestArgs,
  type StrategyProfile,
  type StrategyProfileParams,
} from "../../scalping/strategy-profile.js";
import {
  READINESS_COHORT_CANDIDATES,
  VALIDATED_BTC_GRID_CANDIDATE,
  candidateForSymbol,
  type ValidatedGridCandidate,
} from "../../scalping/grid-candidate.js";
import {
  resolveBybitBaseUrl,
  describeBybitSignalFeed,
} from "../../market-data/gateways/bybit.js";
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
  slippageBpsOption,
  minAtrPctOption,
  volumeMinRatioOption,
  volumeLookbackOption,
  minConfluenceOption,
  entryCandleConfirmOption,
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
  allowMainnetLiveOption,
  demoLiveMarketEntriesOption,
  takerExitFeePctOption,
  fundingRatePct8hOption,
  maintenanceMarginRatePctOption,
  volatilityTargetAnnualPctOption,
  strategyOption,
  profileOption,
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
} from "../scalp-options/index.js";
import {
  type MutablePartialRiskLimits,
  makeDbLayer,
  parseMarginMode,
  parseProductType,
} from "./shared.js";
// Shared with the rest of ../scalp.ts (see the cycle note above).
import {
  buildBacktestComposerConfig,
  loadProfileIfNeeded,
  type WatchlistEntry,
} from "../scalp.js";

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
  /** Live execution on a NON-sandbox account; needs the confirm token too. */
  readonly allowMainnetLive: boolean;
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
    allowMainnetLive: allowMainnetLiveOption,
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

/**
 * The exact token an operator must export to authorize MAINNET execution.
 * Deliberately not a boolean: an empty/typo'd value must not read as "on".
 */
export const MAINNET_CONFIRM_ENV = "NEURATRADE_MAINNET_CONFIRM";
export const MAINNET_CONFIRM_TOKEN = "I-CONFIRM-MAINNET";

/**
 * Live-execution venue gate.
 *
 * Sandbox/testnet live is allowed as before. MAINNET live (`live` without a
 * demo account) needs TWO independent, deliberate acts:
 *   1. `--allow-mainnet-live` — visible in the ecosystem config, so it shows up
 *      in a code review and in `git diff`;
 *   2. `NEURATRADE_MAINNET_CONFIRM=I-CONFIRM-MAINNET` exported in the launching
 *      shell — an operator action at start time, never stored in the repo's
 *      config file.
 * Missing either one fails closed BEFORE any credential or exchange object is
 * built, so a typo'd env var cannot quietly route real money to api2.
 */
export function validateLiveSandboxMode(
  live: boolean,
  sandbox: boolean,
  allowMainnet = false,
  confirmToken: string | undefined = undefined,
): string | undefined {
  if (!live || sandbox) return undefined;
  if (!allowMainnet) {
    return "live execution is disabled until the demo/testnet account is enabled (BITGET_USE_SANDBOX=true or BYBIT_USE_TESTNET=true); mainnet requires --allow-mainnet-live";
  }
  if (confirmToken !== MAINNET_CONFIRM_TOKEN) {
    return `mainnet live execution requires ${MAINNET_CONFIRM_ENV}=${MAINNET_CONFIRM_TOKEN} exported in the launching environment`;
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

/** Fields the live config is compared on, for a diff-able error message. */
function cohortMismatches(
  config: LiveGridConfiguration,
  candidate: ValidatedGridCandidate,
): readonly string[] {
  const pairs: readonly (readonly [string, unknown, unknown])[] = [
    ["exchange", config.exchange, candidate.exchange],
    ["timeframe", config.timeframe, candidate.timeframe],
    ["productType", config.productType, candidate.productType],
    ["gridStepPct", config.gridStepPct, candidate.gridStepPct],
    ["gridMaxGrids", config.gridMaxGrids, candidate.gridMaxGrids],
    [
      "gridPauseAfterLossBars",
      config.gridPauseAfterLossBars,
      candidate.gridPauseAfterLossBars,
    ],
    ["feePct", config.feePct, candidate.feePct],
    ["slippageBps", config.slippageBps, candidate.slippageBps],
    [
      "trendFilterPeriod",
      config.trendFilterPeriod,
      candidate.trendFilterPeriod,
    ],
    ["onlyWithTrend", config.onlyWithTrend, candidate.onlyWithTrend],
    ["targetRatio", config.targetRatio, candidate.targetRatio],
    ["chopGateAdx", config.chopGateAdx, candidate.chopGateAdx],
  ];
  return pairs
    .filter(([, mine, theirs]) => mine !== theirs)
    .map(
      ([name, mine, theirs]) => `${name} ${String(mine)} != ${String(theirs)}`,
    );
}

export function validateLiveGridConfiguration(
  config: LiveGridConfiguration,
  sandbox = false,
): string | undefined {
  if (!sandbox) {
    const candidate = candidateForSymbol(config.symbol);
    // Two different failures, two different remedies: a symbol outside the
    // registry is a RESEARCH task (it must be validated and added), while a
    // mismatched config is a one-line fix. Saying which one it is avoids an
    // operator editing knobs that can never satisfy the gate.
    if (candidate === undefined) {
      const validated = READINESS_COHORT_CANDIDATES.map((c) => c.symbol).join(
        ", ",
      );
      return `live grid has no validated readiness candidate for ${config.symbol} (validated: ${validated}); validate and register it before live trading`;
    }
    if (!isLiveGridCohortCandidate(config, candidate)) {
      const diff = cohortMismatches(config, candidate);
      return `live grid must use the validated readiness cohort candidate for ${config.symbol}${diff.length > 0 ? `: ${diff.join("; ")}` : ""}`;
    }
    return firstLiveGridRiskError(config, liveGridRiskCaps(candidate));
  }
  return undefined;
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
  allowMainnet = false,
  confirmToken: string | undefined = undefined,
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
    // Mainnet grid watchlists are allowed only under the same two-key gate as
    // the venue check: a code-visible flag plus an operator-supplied token.
    // Without BOTH this stays disabled, exactly as before.
    if (!allowMainnet) {
      return "live grid watchlists are disabled; run the validated BTC candidate directly";
    }
    if (confirmToken !== MAINNET_CONFIRM_TOKEN) {
      return `mainnet live grid watchlists require ${MAINNET_CONFIRM_ENV}=${MAINNET_CONFIRM_TOKEN} exported in the launching environment`;
    }
  }
  return undefined;
}

export function validateLiveSoakExecution(live: boolean): string | undefined {
  if (live) {
    return "live soak is disabled; use scalp paper-trade --strategy-type grid";
  }
  return undefined;
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
    validateLiveSandboxMode(
      args.live,
      isDemoAccount,
      args.allowMainnetLive,
      process.env[MAINNET_CONFIRM_ENV],
    ) ??
    validateLiveExecutionStrategy(args.live, strategyType) ??
    validateLiveGridWatchlist(
      args.live,
      strategyType,
      args.entries,
      isDemoAccount,
      args.allowMainnetLive,
      process.env[MAINNET_CONFIRM_ENV],
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
  | import("../../paper-trading/engine.js").PaperTradingIterationResult
  | import("../../paper-trading/futures-engine.js").FuturesPaperTradingIterationResult
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
      import("../../paper-trading/engine.js").PaperTradingIterationResult,
      never,
      never
    > =>
      runPaperTradingIteration(opts).pipe(
        Effect.provide(spotAdapterLayer),
      ) as Effect.Effect<
        import("../../paper-trading/engine.js").PaperTradingIterationResult,
        never,
        never
      >;

    const runFuturesIteration = (
      opts: FuturesPaperTradingOptions,
    ): Effect.Effect<
      import("../../paper-trading/futures-engine.js").FuturesPaperTradingIterationResult,
      never,
      never
    > =>
      runFuturesPaperTradingIteration(opts).pipe(
        Effect.provide(futuresAdapterLayer),
      ) as Effect.Effect<
        import("../../paper-trading/futures-engine.js").FuturesPaperTradingIterationResult,
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

function printSoakResult(result: import("../../scalping/soak.js").SoakResult) {
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
