import { Command, Options } from "@effect/cli";
import { BunContext } from "@effect/platform-bun";
import { Console, Effect, Layer, Option } from "effect";
import { Database } from "bun:sqlite";
import * as fs from "node:fs";
import { dirname, isAbsolute, resolve } from "node:path";
import { Path, PathLive } from "../services/path.js";
import {
  MarketDataRepository,
  MarketDataRepositoryError,
  MarketDataRepositorySQLiteLive,
} from "../market-data/repository.js";
import { defaultComposerConfig } from "../scalping/composer.js";
import type { ComposerConfig } from "../scalping/types.js";
import { runBacktest, type BacktestResult } from "../scalping/backtest.js";
import { MarketDataGatewayLive } from "../market-data/gateways/index.js";
import { MarketDataGatewayRepositoryLive } from "../market-data/gateway-repository.js";
import { SimulatedExchangeAdapterLive } from "../exchange/adapters/simulated.js";
import { BinanceLiveExchangeAdapterLive } from "../exchange/adapters/binance-live.js";
import { SimulatedFuturesExchangeAdapterLive } from "../exchange/adapters/simulated-futures.js";
import { BitgetFuturesExchangeAdapterLive } from "../exchange/adapters/bitget-futures.js";
import type { FuturesMarginMode } from "../exchange/futures-adapter.js";
import { RiskGuardLive } from "../risk/guards.js";
import { KillSwitch, KillSwitchSQLiteLive } from "../risk/kill-switch.js";
import { CircuitBreakerSQLiteLive } from "../risk/circuit-breaker.js";
import {
  runPaperTradingIteration,
  type PaperTradingOptions,
} from "../paper-trading/engine.js";
import {
  runFuturesPaperTradingIteration,
  type FuturesPaperTradingOptions,
} from "../paper-trading/futures-engine.js";
import {
  BitgetClientLiveConfig,
  type BitgetProductType,
} from "../services/bitget-client.js";
import { BitgetConfigLive } from "../services/bitget-config.js";
import { RateLimiterLive } from "../services/rate-limiter.js";
import {
  PaperTradingRepository,
  PaperTradingRepositorySQLiteLive,
} from "../paper-trading/repository.js";
import {
  runSoak,
  type SoakOptions,
  type SoakSymbol,
  type IterationResult,
} from "../scalping/soak.js";
import {
  buildStrategyProfileFromArgs,
  loadStrategyProfile,
  resolveBacktestArgs,
  saveStrategyProfile,
  type ResolvedBacktestArgs,
  type StrategyProfile,
  type StrategyProfileParams,
} from "../scalping/strategy-profile.js";

const exchangeOption = Options.text("exchange").pipe(
  Options.withDefault("binance"),
  Options.withDescription("Exchange identifier"),
);

const symbolOption = Options.text("symbol").pipe(
  Options.withDefault("BTC/USDT"),
  Options.withDescription("Trading pair symbol"),
);

const timeframeOption = Options.text("timeframe").pipe(
  Options.withDefault("1h"),
  Options.withDescription("Candle timeframe"),
);

const capitalOption = Options.integer("capital").pipe(
  Options.withDefault(10000),
  Options.withDescription("Initial capital in quote currency"),
);

const positionSizeOption = Options.integer("position-size").pipe(
  Options.withDefault(100),
  Options.withDescription("Position size as percent of capital"),
);

const riskPerTradeOption = Options.float("risk-per-trade").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Risk per trade as percent of capital (overrides --position-size; 0 = disabled)",
  ),
);

const riskBasedMaxPositionSizeOption = Options.float("max-position-size").pipe(
  Options.withDefault(100),
  Options.withDescription(
    "Maximum position size as percent of capital when using --risk-per-trade",
  ),
);

const stopLossOption = Options.float("stop-loss").pipe(
  Options.withDefault(1.5),
  Options.withDescription("Stop loss percent"),
);

const takeProfitOption = Options.float("take-profit").pipe(
  Options.withDefault(3.0),
  Options.withDescription("Take profit percent"),
);

const feeOption = Options.float("fee").pipe(
  Options.withDefault(0.1),
  Options.withDescription("Trading fee percent per side"),
);

const futuresOption = Options.boolean("futures").pipe(
  Options.withDefault(false),
  Options.withDescription("Trade perpetual futures instead of spot"),
);

const leverageOption = Options.integer("leverage").pipe(
  Options.withDefault(3),
  Options.withDescription("Futures leverage (default 3x)"),
);

const fundingRateOption = Options.float("funding-rate-pct").pipe(
  Options.withDefault(0.01),
  Options.withDescription(
    "Per-interval funding cost in percent (default 0.01% every 8h)",
  ),
);

const slippageBpsOption = Options.float("slippage-bps").pipe(
  Options.withDefault(0),
  Options.withDescription("Slippage in basis points applied to fills"),
);

const trailingStopPctOption = Options.float("trailing-stop-pct").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Trail stop-loss this percentage behind the most favorable price (0 = disabled)",
  ),
);

const trailingStopAtrMultOption = Options.float("trailing-stop-atr-mult").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Trail stop-loss at this ATR multiplier behind the most favorable price (0 = disabled)",
  ),
);

const minAtrPctOption = Options.float("min-atr-pct").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Minimum ATR% required to enter a trade, filters low-volatility chop (0 = disabled)",
  ),
);

const adxMinOption = Options.float("adx-min").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Minimum ADX required by the regime filter (0 = use default adxWeakTrend)",
  ),
);

const volumeMinRatioOption = Options.float("volume-min-ratio").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Require current volume >= this ratio of its moving average to enter (0 = disabled)",
  ),
);

const volumeLookbackOption = Options.integer("volume-lookback").pipe(
  Options.withDefault(20),
  Options.withDescription("Lookback period for volume moving average filter"),
);

const minConfluenceOption = Options.integer("min-confluence").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Minimum number of components that must agree to enter (0 = disabled)",
  ),
);

const entryCandleConfirmOption = Options.boolean("entry-candle-confirm").pipe(
  Options.withDefault(false),
  Options.withDescription(
    "Require the entry candle body to align with the signal direction",
  ),
);

const signalPersistenceOption = Options.integer("signal-persistence").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Require the same directional signal for N consecutive candles before entering (0 = disabled)",
  ),
);

const momentumConfirmBarsOption = Options.integer("momentum-confirm-bars").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Require net price movement over the last N candles to align with the signal (0 = disabled)",
  ),
);

const lossConfidencePenaltyOption = Options.float(
  "loss-confidence-penalty",
).pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Boost min-confidence by this amount after a losing trade (0 = disabled)",
  ),
);

const lossConfidenceDecayOption = Options.float("loss-confidence-decay").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Decay the post-loss confidence penalty by this amount each candle (0 = step reset)",
  ),
);

const htfTimeframeOption = Options.text("htf-timeframe").pipe(
  Options.withDefault(""),
  Options.withDescription(
    "Higher-timeframe for trend filter (e.g. 1h). Empty disables the filter.",
  ),
);

const htfTrendFastPeriodOption = Options.integer("htf-trend-fast").pipe(
  Options.withDefault(50),
  Options.withDescription("Higher-timeframe EMA fast period for trend filter"),
);

const htfTrendSlowPeriodOption = Options.integer("htf-trend-slow").pipe(
  Options.withDefault(100),
  Options.withDescription("Higher-timeframe EMA slow period for trend filter"),
);

const entryPullbackEmaPeriodOption = Options.integer(
  "entry-pullback-ema-period",
).pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Only enter when price is within --entry-pullback-margin-pct of this EMA period (0 = disabled)",
  ),
);

const entryPullbackMarginPctOption = Options.float(
  "entry-pullback-margin-pct",
).pipe(
  Options.withDefault(0.1),
  Options.withDescription(
    "Allowed distance from the pullback EMA as a percentage of price",
  ),
);

const minEfficiencyRatioOption = Options.float("min-efficiency-ratio").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Minimum Kaufman Efficiency Ratio (0-1) required to enter; high values filter chop",
  ),
);

const efficiencyRatioPeriodOption = Options.integer(
  "efficiency-ratio-period",
).pipe(
  Options.withDefault(20),
  Options.withDescription("Lookback period for the efficiency-ratio filter"),
);

const rsiLongMaxOption = Options.float("rsi-long-max").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Leading RSI filter: only enter longs when RSI <= this value (0 = disabled)",
  ),
);

const rsiShortMinOption = Options.float("rsi-short-min").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Leading RSI filter: only enter shorts when RSI >= this value (0 = disabled)",
  ),
);

const bollingerLongMaxPctBOption = Options.float(
  "bollinger-long-max-pctb",
).pipe(
  Options.withDefault(-1),
  Options.withDescription(
    "Leading Bollinger %B filter: only enter longs when %B <= this value (-1 = disabled)",
  ),
);

const bollingerShortMinPctBOption = Options.float(
  "bollinger-short-min-pctb",
).pipe(
  Options.withDefault(2),
  Options.withDescription(
    "Leading Bollinger %B filter: only enter shorts when %B >= this value (2 = disabled)",
  ),
);

const profileOption = Options.text("profile").pipe(
  Options.withDefault(""),
  Options.withDescription(
    "Strategy profile name to load from ~/.neuratrade/profiles",
  ),
);

const recordEquityCurveOption = Options.boolean("record-equity-curve").pipe(
  Options.withDefault(false),
  Options.withDescription("Record equity curve at each trade close"),
);

const exportTradesOption = Options.text("export-trades").pipe(
  Options.withDefault(""),
  Options.withDescription(
    "Write trades.csv (+ equity.csv if --record-equity-curve) to this path",
  ),
);

const oosPctOption = Options.integer("oos-pct").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Percentage of recent candles to hold out for out-of-sample validation (0 disables)",
  ),
);

const mcIterationsOption = Options.integer("mc-iterations").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Number of Monte Carlo permutations for drawdown simulation (0 disables)",
  ),
);

const breakevenAtROption = Options.float("breakeven-at-r").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Move stop-loss to breakeven once price reaches this R profit (0 disables)",
  ),
);

const maxBarsInTradeOption = Options.integer("max-bars-in-trade").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Maximum bars to hold a position before time-stop exit (0 disables)",
  ),
);

const lossCooldownBarsOption = Options.integer("loss-cooldown-bars").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Bars to skip after a losing trade before allowing new entries (0 disables)",
  ),
);

const sessionStartOption = Options.text("session-start").pipe(
  Options.withDefault(""),
  Options.withDescription("UTC session start in HH:MM (empty disables)"),
);

const sessionEndOption = Options.text("session-end").pipe(
  Options.withDefault(""),
  Options.withDescription("UTC session end in HH:MM (empty disables)"),
);

const autoRegimeFilterOption = Options.boolean("auto-regime-filter").pipe(
  Options.withDefault(false),
  Options.withDescription(
    "Block entries that do not match the detected trend/mean-reversion regime",
  ),
);

const autoRegimeAdxThresholdOption = Options.float(
  "auto-regime-adx-threshold",
).pipe(
  Options.withDefault(25),
  Options.withDescription("ADX threshold for auto-regime detection"),
);

const confidenceOption = Options.float("min-confidence").pipe(
  Options.withDefault(0.5),
  Options.withDescription("Minimum signal confidence to enter a trade"),
);

const useAtrStopsOption = Options.boolean("use-atr-stops").pipe(
  Options.withDefault(false),
  Options.withDescription("Use ATR-based dynamic stop loss and take profit"),
);

const atrStopMultiplierOption = Options.float("atr-stop-multiplier").pipe(
  Options.withDefault(1.5),
  Options.withDescription(
    "ATR multiplier for stop loss when --use-atr-stops is set",
  ),
);

const atrTakeProfitMultiplierOption = Options.float(
  "atr-take-profit-multiplier",
).pipe(
  Options.withDefault(2.5),
  Options.withDescription(
    "ATR multiplier for take profit when --use-atr-stops is set (legacy; overridden by --atr-risk-reward)",
  ),
);

const atrRiskRewardOption = Options.float("atr-risk-reward").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "ATR take-profit distance as a multiple of the stop distance (0 = use --atr-take-profit-multiplier)",
  ),
);

const scaleOutAtROption = Options.float("scale-out-at-r").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Partial scale-out trigger in R multiples (e.g. 1.0 = +1R). 0 disables scale-out.",
  ),
);

const scaleOutPctOption = Options.float("scale-out-pct").pipe(
  Options.withDefault(50),
  Options.withDescription(
    "Percentage of the position to close at the scale-out trigger",
  ),
);

const volatilityLookbackOption = Options.integer("volatility-lookback").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Lookback window length for ATR% volatility calibration. 0 disables.",
  ),
);

const volatilityLowPctOption = Options.float("volatility-low-pct").pipe(
  Options.withDefault(20),
  Options.withDescription(
    "Low percentile threshold for volatility calibration",
  ),
);

const volatilityHighPctOption = Options.float("volatility-high-pct").pipe(
  Options.withDefault(80),
  Options.withDescription(
    "High percentile threshold for volatility calibration",
  ),
);

const volatilityLowFactorOption = Options.float("volatility-low-factor").pipe(
  Options.withDefault(0.8),
  Options.withDescription(
    "Multiplier applied to ATR stop distance in low-volatility regimes",
  ),
);

const volatilityHighFactorOption = Options.float("volatility-high-factor").pipe(
  Options.withDefault(1.2),
  Options.withDescription(
    "Multiplier applied to ATR stop distance in high-volatility regimes",
  ),
);

const priceOnlyOption = Options.boolean("price-only").pipe(
  Options.withDefault(false),
  Options.withDescription(
    "Ignore synthetic order-book components in backtest (trend/volatility/RSI/regime only)",
  ),
);

const noRsiOption = Options.boolean("no-rsi").pipe(
  Options.withDefault(false),
  Options.withDescription("Disable RSI mean-reversion component in backtest"),
);

const holdUntilStopOption = Options.boolean("hold-until-stop").pipe(
  Options.withDefault(false),
  Options.withDescription(
    "Ignore opposite-signal exits and only exit on stop/take-profit",
  ),
);

const noTrendOption = Options.boolean("no-trend").pipe(
  Options.withDefault(false),
  Options.withDescription("Disable trend-following EMA component in backtest"),
);

const regimeModeOption = Options.choice("regime-mode", [
  "trend",
  "reversion",
] as const).pipe(
  Options.withDefault("trend" as const),
  Options.withDescription(
    "Regime filter mode: trend-following or mean-reversion",
  ),
);

function makeLayer(home?: string) {
  return Layer.mergeAll(BunContext.layer, PathLive(home));
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
  profile: profileOption,
};

export const backtestCommand = Command.make(
  "backtest",
  backtestOptions,
  (args) =>
    Effect.gen(function* () {
      const path = yield* Path;
      const sqlitePath = resolve(path.homeDir, "data", "neuratrade.db");
      const db = new Database(sqlitePath);
      db.exec("PRAGMA foreign_keys = ON;");

      const repoLayer = MarketDataRepositorySQLiteLive(db);

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

      const result = yield* backtestProgram(programArgs).pipe(
        Effect.provide(repoLayer),
        Effect.tap((r) => printBacktestResult(r)),
        Effect.catchAll((err) =>
          Effect.gen(function* () {
            const msg = err instanceof Error ? err.message : err.reason;
            yield* Console.error(`backtest failed: ${msg}`);
            return emptyResult(args.symbol);
          }),
        ),
        Effect.ensuring(Effect.sync(() => db.close())),
      );

      return result;
    }).pipe(Effect.provide(makeLayer(process.env.NEURATRADE_HOME))),
).pipe(
  Command.withDescription(
    "Backtest deterministic scalping strategy on historical candles",
  ),
);

interface BacktestArgs extends ResolvedBacktestArgs {}

function buildBacktestComposerConfig(
  priceOnly: boolean,
  noRsi: boolean,
  noTrend: boolean,
  regimeMode: "trend" | "reversion" = "trend",
  volumeMinRatio = 0,
  volumeLookback = 20,
  minConfluence = 0,
  entryCandleConfirm = false,
  momentumConfirmBars = 0,
  adxMin = 0,
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
    adxMin <= 0
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
    regime: weights.regime / activeSum,
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
    },
  };
}

function backtestProgram(args: ResolvedBacktestArgs) {
  return Effect.gen(function* () {
    const repo = yield* MarketDataRepository;
    const path = yield* Path;

    const candles = yield* repo.getCandles({
      exchange: args.exchange,
      symbol: args.symbol,
      timeframe: args.timeframe,
    });

    const htfCandles =
      args.htfTimeframe && args.htfTimeframe.trim().length > 0
        ? yield* repo.getCandles({
            exchange: args.exchange,
            symbol: args.symbol,
            timeframe: args.htfTimeframe,
          })
        : [];

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
      args.adxMin,
    );

    const result = runBacktest({
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
      slippageBps: args.slippageBps,
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
      recordEquityCurve: args.recordEquityCurve || args.exportTrades.length > 0,
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
): Effect.Effect<void, Error> {
  return Effect.gen(function* () {
    fs.mkdirSync(dirname(exportPath), { recursive: true });

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
  };
}

const atrStopMinOption = Options.float("atr-stop-min").pipe(
  Options.withDefault(1.0),
  Options.withDescription("Minimum ATR stop multiplier to test"),
);

const atrStopMaxOption = Options.float("atr-stop-max").pipe(
  Options.withDefault(3.0),
  Options.withDescription("Maximum ATR stop multiplier to test"),
);

const atrStopStepOption = Options.float("atr-stop-step").pipe(
  Options.withDefault(0.5),
  Options.withDescription("Step size for ATR stop multiplier"),
);

const atrTpMinOption = Options.float("atr-tp-min").pipe(
  Options.withDefault(2.0),
  Options.withDescription("Minimum ATR take-profit multiplier to test"),
);

const atrTpMaxOption = Options.float("atr-tp-max").pipe(
  Options.withDefault(5.0),
  Options.withDescription("Maximum ATR take-profit multiplier to test"),
);

const atrTpStepOption = Options.float("atr-tp-step").pipe(
  Options.withDefault(0.5),
  Options.withDescription("Step size for ATR take-profit multiplier"),
);

const confMinOption = Options.float("conf-min").pipe(
  Options.withDefault(0.5),
  Options.withDescription("Minimum min-confidence to test"),
);

const confMaxOption = Options.float("conf-max").pipe(
  Options.withDefault(0.7),
  Options.withDescription("Maximum min-confidence to test"),
);

const confStepOption = Options.float("conf-step").pipe(
  Options.withDefault(0.1),
  Options.withDescription("Step size for min-confidence"),
);

interface OptimizeArgs {
  readonly exchange: string;
  readonly symbol: string;
  readonly timeframe: string;
  readonly capital: number;
  readonly positionSize: number;
  readonly riskPerTrade: number;
  readonly maxPositionSize: number;
  readonly fee: number;
  readonly priceOnly: boolean;
  readonly noRsi: boolean;
  readonly noTrend: boolean;
  readonly holdUntilStop: boolean;
  readonly regimeMode: "trend" | "reversion";
  readonly atrRiskReward: number;
  readonly scaleOutAtR: number;
  readonly scaleOutPct: number;
  readonly volatilityLookback: number;
  readonly volatilityLowPct: number;
  readonly volatilityHighPct: number;
  readonly volatilityLowFactor: number;
  readonly volatilityHighFactor: number;
  readonly atrStopMin: number;
  readonly atrStopMax: number;
  readonly atrStopStep: number;
  readonly atrTpMin: number;
  readonly atrTpMax: number;
  readonly atrTpStep: number;
  readonly confMin: number;
  readonly confMax: number;
  readonly confStep: number;
  readonly volumeMinRatio: number;
  readonly volumeLookback: number;
  readonly minConfluence: number;
  readonly entryCandleConfirm: boolean;
  readonly momentumConfirmBars: number;
}

function mergeOptimizeArgs(
  args: OptimizeArgs,
  profile: StrategyProfile,
): OptimizeArgs {
  const overrides = profile.symbols[args.symbol] ?? {};
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
    profile: profileOption,
  },
  (args) =>
    Effect.gen(function* () {
      const path = yield* Path;
      const sqlitePath = resolve(path.homeDir, "data", "neuratrade.db");
      const db = new Database(sqlitePath);
      db.exec("PRAGMA foreign_keys = ON;");

      const repoLayer = MarketDataRepositorySQLiteLive(db);

      const profile = yield* loadProfileIfNeeded(path.homeDir, args.profile);
      const programArgs = Option.isSome(profile)
        ? mergeOptimizeArgs(args, profile.value)
        : args;

      const result = yield* optimizeProgram(programArgs).pipe(
        Effect.provide(repoLayer),
        Effect.tap((r) => printOptimizeResult(r, args.symbol, args.timeframe)),
        Effect.catchAll((err) =>
          Effect.gen(function* () {
            yield* Console.error(`optimize failed: ${err.reason}`);
            return [];
          }),
        ),
        Effect.ensuring(Effect.sync(() => db.close())),
      );

      return result;
    }).pipe(Effect.provide(makeLayer(process.env.NEURATRADE_HOME))),
).pipe(
  Command.withDescription(
    "Grid-search ATR/confidence parameters over historical candles",
  ),
);

function optimizeProgram(args: OptimizeArgs) {
  return Effect.gen(function* () {
    const repo = yield* MarketDataRepository;

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
          const result = runBacktest({
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

const minCandlesOption = Options.integer("min-candles").pipe(
  Options.withDefault(500),
  Options.withDescription(
    "Minimum candles required for a symbol to be included in scan",
  ),
);

const topOption = Options.integer("top").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Limit scan to top N symbols by candle count (0 = all)",
  ),
);

const optimizeScanOption = Options.boolean("optimize").pipe(
  Options.withDefault(false),
  Options.withDescription(
    "Run a coarse per-symbol parameter grid search and report best params",
  ),
);

const minReturnOption = Options.float("min-return-pct").pipe(
  Options.optional,
  Options.withDescription(
    "Skip symbols with total return below this threshold",
  ),
);

const minSharpeOption = Options.float("min-sharpe").pipe(
  Options.optional,
  Options.withDescription(
    "Skip symbols with Sharpe ratio below this threshold",
  ),
);

const scanMaxDrawdownOption = Options.float("max-drawdown-pct").pipe(
  Options.optional,
  Options.withDescription(
    "Skip symbols with max drawdown above this threshold",
  ),
);

const saveWatchlistOption = Options.text("save-watchlist").pipe(
  Options.optional,
  Options.withDescription(
    "Write passing symbols to a JSON watchlist file in NEURATRADE_HOME/data",
  ),
);

interface ScanArgs {
  readonly exchange: string;
  readonly timeframe: string;
  readonly capital: number;
  readonly positionSize: number;
  readonly riskPerTrade: number;
  readonly maxPositionSize: number;
  readonly fee: number;
  readonly minConfidence: number;
  readonly useAtrStops: boolean;
  readonly atrStopMultiplier: number;
  readonly atrTakeProfitMultiplier: number;
  readonly atrRiskReward: number;
  readonly scaleOutAtR: number;
  readonly scaleOutPct: number;
  readonly volatilityLookback: number;
  readonly volatilityLowPct: number;
  readonly volatilityHighPct: number;
  readonly volatilityLowFactor: number;
  readonly volatilityHighFactor: number;
  readonly stopLoss: number;
  readonly takeProfit: number;
  readonly priceOnly: boolean;
  readonly noRsi: boolean;
  readonly noTrend: boolean;
  readonly holdUntilStop: boolean;
  readonly regimeMode: "trend" | "reversion";
  readonly minAtrPct: number;
  readonly minCandles: number;
  readonly top: number;
  readonly optimize: boolean;
  readonly minReturnPct: Option.Option<number>;
  readonly minSharpe: Option.Option<number>;
  readonly maxDrawdownPct: Option.Option<number>;
  readonly saveWatchlist: Option.Option<string>;
  readonly watchlistPath?: string;
  readonly futures: boolean;
  readonly fundingRatePct: number;
  readonly slippageBps: number;
  readonly volumeMinRatio: number;
  readonly volumeLookback: number;
  readonly minConfluence: number;
  readonly entryCandleConfirm: boolean;
  readonly momentumConfirmBars: number;
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
    profile: profileOption,
  },
  (args) =>
    Effect.gen(function* () {
      const path = yield* Path;
      const sqlitePath = resolve(path.homeDir, "data", "neuratrade.db");
      const db = new Database(sqlitePath);
      db.exec("PRAGMA foreign_keys = ON;");

      const repoLayer = MarketDataRepositorySQLiteLive(db);

      const profile = yield* loadProfileIfNeeded(path.homeDir, args.profile);
      const mergedArgs = Option.isSome(profile)
        ? mergeScanArgs(args, profile.value)
        : args;

      const watchlistPath = Option.match(mergedArgs.saveWatchlist, {
        onNone: () => undefined as string | undefined,
        onSome: (file) => resolve(path.homeDir, "data", file),
      });

      const result = yield* scanProgram({ ...mergedArgs, watchlistPath }).pipe(
        Effect.provide(repoLayer),
        Effect.tap((r) => printScanResult(r)),
        Effect.catchAll((err) =>
          Effect.gen(function* () {
            yield* Console.error(`scan failed: ${err.reason}`);
            return [];
          }),
        ),
        Effect.ensuring(Effect.sync(() => db.close())),
      );

      return result;
    }).pipe(Effect.provide(makeLayer(process.env.NEURATRADE_HOME))),
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
        ? optimizeForSymbol(symbol, candles, args, exchange, composerConfig)
        : runBacktestWithParams(
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

interface ScanResult {
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
): BacktestResult & { readonly bestParams?: undefined } {
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
): BacktestResult & {
  readonly bestParams: {
    readonly atrStopMultiplier: number;
    readonly atrTakeProfitMultiplier: number;
    readonly minConfidence: number;
  };
} {
  let best: BacktestResult | null = null;
  let bestParams = {
    atrStopMultiplier: args.atrStopMultiplier,
    atrTakeProfitMultiplier: args.atrTakeProfitMultiplier,
    minConfidence: args.minConfidence,
  };

  for (const stopMult of SCAN_STOP_MULTS) {
    for (const tpMult of SCAN_TP_MULTS) {
      for (const conf of SCAN_CONFIDENCES) {
        const result = runBacktestWithParams(
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

const intervalOption = Options.integer("interval").pipe(
  Options.withDefault(60),
  Options.withDescription("Seconds between paper-trading iterations"),
);

const iterationsOption = Options.integer("iterations").pipe(
  Options.withDefault(1),
  Options.withDescription("Number of iterations to run (0 = infinite)"),
);

const liveOption = Options.boolean("live").pipe(
  Options.withDefault(false),
  Options.withDescription(
    "Use live exchange adapter (Binance spot or Bitget futures)",
  ),
);

const apiKeyOption = Options.text("api-key").pipe(
  Options.withDefault(""),
  Options.withDescription("Binance API key (or set BINANCE_API_KEY env)"),
);

const apiSecretOption = Options.text("api-secret").pipe(
  Options.withDefault(""),
  Options.withDescription("Binance API secret (or set BINANCE_API_SECRET env)"),
);

const marginModeOption = Options.text("margin-mode").pipe(
  Options.withDefault("crossed"),
  Options.withDescription("Futures margin mode: crossed or isolated"),
);

const productTypeOption = Options.text("product-type").pipe(
  Options.withDefault("USDT-FUTURES"),
  Options.withDescription(
    "Futures product type: USDT-FUTURES, COIN-FUTURES or USDC-FUTURES",
  ),
);

const maxDrawdownOption = Options.float("max-drawdown-pct").pipe(
  Options.optional,
  Options.withDescription(
    "Max drawdown % before blocking new trades (live default 5%)",
  ),
);

const maxDailyLossOption = Options.float("max-daily-loss-pct").pipe(
  Options.optional,
  Options.withDescription(
    "Max daily loss % before blocking new trades (live default 2%)",
  ),
);

const maxPositionSizeOption = Options.float("max-position-size-pct").pipe(
  Options.optional,
  Options.withDescription(
    "Max position size % of capital per trade (live default 10%)",
  ),
);

const maxTradesPerDayOption = Options.integer("max-trades-per-day").pipe(
  Options.optional,
  Options.withDescription("Max trades per day (live default 10)"),
);

const minCapitalOption = Options.integer("min-capital").pipe(
  Options.optional,
  Options.withDescription(
    "Minimum capital required to trade (live default 100)",
  ),
);

const watchlistOption = Options.text("watchlist").pipe(
  Options.optional,
  Options.withDescription(
    "Path to a JSON watchlist in NEURATRADE_HOME/data (uses per-symbol best params)",
  ),
);

const killSwitchOption = Options.boolean("kill-switch").pipe(
  Options.withDefault(false),
  Options.withDescription(
    "Engage kill switch before starting (blocks all new trades)",
  ),
);

const disengageOption = Options.boolean("disengage").pipe(
  Options.withDefault(false),
  Options.withDescription("Disengage kill switch before starting"),
);

interface WatchlistEntry {
  readonly symbol: string;
  readonly exchange?: string;
  readonly returnPct: number;
  readonly sharpe: number;
  readonly bestParams?: {
    readonly atrStopMultiplier: number;
    readonly atrTakeProfitMultiplier: number;
    readonly minConfidence: number;
  };
}

interface PaperTradeArgs {
  readonly exchange: string;
  readonly symbol: string;
  readonly timeframe: string;
  readonly capital: number;
  readonly positionSize: number;
  readonly riskPerTrade: number;
  readonly fee: number;
  readonly minConfidence: number;
  readonly useAtrStops: boolean;
  readonly atrStopMultiplier: number;
  readonly atrTakeProfitMultiplier: number;
  readonly atrRiskReward: number;
  readonly scaleOutAtR: number;
  readonly scaleOutPct: number;
  readonly volatilityLookback: number;
  readonly volatilityLowPct: number;
  readonly volatilityHighPct: number;
  readonly volatilityLowFactor: number;
  readonly volatilityHighFactor: number;
  readonly stopLoss: number;
  readonly takeProfit: number;
  readonly priceOnly: boolean;
  readonly noRsi: boolean;
  readonly noTrend: boolean;
  readonly holdUntilStop: boolean;
  readonly regimeMode: "trend" | "reversion";
  readonly minAtrPct: number;
  readonly volumeMinRatio: number;
  readonly volumeLookback: number;
  readonly minConfluence: number;
  readonly entryCandleConfirm: boolean;
  readonly momentumConfirmBars: number;
  readonly interval: number;
  readonly iterations: number;
  readonly live: boolean;
  readonly apiKey: string;
  readonly apiSecret: string;
  readonly futures: boolean;
  readonly leverage: number;
  readonly marginMode: string;
  readonly productType: string;
  readonly maxDrawdownPct: Option.Option<number>;
  readonly maxDailyLossPct: Option.Option<number>;
  readonly maxPositionSizePct: Option.Option<number>;
  readonly maxTradesPerDay: Option.Option<number>;
  readonly minCapital: Option.Option<number>;
  readonly watchlist: Option.Option<string>;
  readonly killSwitch: boolean;
  readonly disengage: boolean;
  readonly entries?: readonly WatchlistEntry[];
}

function mergePaperTradeArgs(
  args: PaperTradeArgs,
  profile: StrategyProfile,
): PaperTradeArgs {
  const overrides = profile.symbols[args.symbol] ?? {};
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

interface SoakArgs {
  readonly exchange: string;
  readonly timeframe: string;
  readonly capital: number;
  readonly positionSize: number;
  readonly riskPerTrade: number;
  readonly maxPositionSize: number;
  readonly fee: number;
  readonly minConfidence: number;
  readonly useAtrStops: boolean;
  readonly atrStopMultiplier: number;
  readonly atrTakeProfitMultiplier: number;
  readonly atrRiskReward: number;
  readonly scaleOutAtR: number;
  readonly scaleOutPct: number;
  readonly volatilityLookback: number;
  readonly volatilityLowPct: number;
  readonly volatilityHighPct: number;
  readonly volatilityLowFactor: number;
  readonly volatilityHighFactor: number;
  readonly stopLoss: number;
  readonly takeProfit: number;
  readonly priceOnly: boolean;
  readonly noRsi: boolean;
  readonly noTrend: boolean;
  readonly holdUntilStop: boolean;
  readonly regimeMode: "trend" | "reversion";
  readonly minAtrPct: number;
  readonly volumeMinRatio: number;
  readonly volumeLookback: number;
  readonly minConfluence: number;
  readonly entryCandleConfirm: boolean;
  readonly momentumConfirmBars: number;
  readonly watchlist: string;
  readonly interval: number;
  readonly iterations: number;
  readonly live: boolean;
  readonly apiKey: string;
  readonly apiSecret: string;
  readonly futures: boolean;
  readonly leverage: number;
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
  -readonly [K in keyof import("../risk/guards.js").RiskLimits]?: import("../risk/guards.js").RiskLimits[K];
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
    killSwitch: killSwitchOption,
    disengage: disengageOption,
    profile: profileOption,
  },
  (args) =>
    Effect.gen(function* () {
      const path = yield* Path;
      const sqlitePath = resolve(path.homeDir, "data", "neuratrade.db");
      const db = new Database(sqlitePath);
      db.exec("PRAGMA foreign_keys = ON;");

      const profile = yield* loadProfileIfNeeded(path.homeDir, args.profile);
      const mergedArgs = Option.isSome(profile)
        ? mergePaperTradeArgs(args, profile.value)
        : args;

      const watchlist = yield* Option.match(mergedArgs.watchlist, {
        onNone: () => Effect.succeed<readonly WatchlistEntry[]>([]),
        onSome: (file) => loadWatchlist(resolve(path.homeDir, "data", file)),
      });

      const repoLayer = MarketDataRepositorySQLiteLive(db);
      const paperRepoLayer = PaperTradingRepositorySQLiteLive(db);
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
        BunContext.layer,
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
        Effect.catchAll((err) =>
          Effect.gen(function* () {
            yield* Console.error(
              `paper-trade failed: ${"reason" in err ? err.reason : String(err)}`,
            );
            return undefined;
          }),
        ),
        Effect.ensuring(Effect.sync(() => db.close())),
      );

      return result;
    }).pipe(Effect.provide(makeLayer(process.env.NEURATRADE_HOME))),
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

function paperTradeProgram(args: PaperTradeArgs) {
  return Effect.gen(function* () {
    const repo = yield* MarketDataRepository;
    yield* repo.ensureTables();

    const paperRepo = yield* PaperTradingRepository;
    yield* paperRepo.ensureTables();

    const portfolio = yield* paperRepo.getPortfolio();
    const startCapital =
      portfolio.capital <= 0 ? args.capital : portfolio.capital;
    yield* paperRepo.setPortfolio(
      startCapital,
      Math.max(portfolio.peakCapital, startCapital),
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
    });

    const marginMode = parseMarginMode(args.marginMode);
    const productType = parseProductType(args.productType);
    // Futures data and execution both live on Bitget in this port; default the
    // market-data exchange to bitget-futures unless the operator overrides it.
    const defaultFuturesExchange =
      args.futures && args.exchange === "binance"
        ? "bitget-futures"
        : args.exchange;
    const makeFuturesOptions = (
      symbol: string,
      exchangeOverride: string,
      overrides?: Partial<FuturesPaperTradingOptions>,
    ): FuturesPaperTradingOptions => ({
      exchange: exchangeOverride ?? defaultFuturesExchange,
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
      leverage: args.leverage,
      marginMode,
      productType,
    });

    const spotAdapterLayer = args.live
      ? BinanceLiveExchangeAdapterLive({
          apiKey: args.apiKey || process.env.BINANCE_API_KEY || "",
          apiSecret: args.apiSecret || process.env.BINANCE_API_SECRET || "",
        })
      : SimulatedExchangeAdapterLive();
    const futuresAdapterLayer = args.live
      ? Layer.provide(
          BitgetFuturesExchangeAdapterLive,
          Layer.provide(
            BitgetClientLiveConfig,
            Layer.merge(BitgetConfigLive, RateLimiterLive()),
          ),
        )
      : SimulatedFuturesExchangeAdapterLive();

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

    let remaining = args.iterations;
    while (remaining !== 0) {
      if (entries) {
        for (const entry of entries) {
          if (remaining === 0) break;
          const entryExchange = entry.exchange ?? args.exchange;
          const result = args.futures
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

          if (remaining !== 0) {
            yield* Effect.sleep(`${args.interval} seconds`);
          }
        }
      } else {
        const result = args.futures
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

        if (remaining !== 0) {
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

const soakWatchlistOption = Options.text("watchlist").pipe(
  Options.withDescription("Path to a JSON watchlist in NEURATRADE_HOME/data"),
);

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
    profile: profileOption,
  },
  (args) =>
    Effect.gen(function* () {
      const path = yield* Path;
      const sqlitePath = resolve(path.homeDir, "data", "neuratrade.db");
      const db = new Database(sqlitePath);
      db.exec("PRAGMA foreign_keys = ON;");

      const profile = yield* loadProfileIfNeeded(path.homeDir, args.profile);
      const mergedArgs = Option.isSome(profile)
        ? mergeSoakArgs(args, profile.value)
        : args;

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
        BunContext.layer,
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
      const futuresAdapterLayer = mergedArgs.live
        ? Layer.provide(
            BitgetFuturesExchangeAdapterLive,
            Layer.provide(
              BitgetClientLiveConfig,
              Layer.merge(BitgetConfigLive, RateLimiterLive()),
            ),
          )
        : SimulatedFuturesExchangeAdapterLive();

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
          };
          return runFuturesPaperTradingIteration(opts).pipe(
            Effect.provide(futuresAdapterLayer),
            Effect.provide(layers),
            Effect.map(
              (r): IterationResult => ({
                action: r.action,
                capital: r.capital,
                note: r.note,
              }),
            ),
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
        };
        return runPaperTradingIteration(opts).pipe(
          Effect.provide(spotAdapterLayer),
          Effect.provide(layers),
          Effect.map(
            (r): IterationResult => ({
              action: r.action,
              capital: r.capital,
              note: r.note,
            }),
          ),
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
        Effect.catchAll((err) =>
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
        Effect.ensuring(Effect.sync(() => db.close())),
      );

      yield* printSoakResult(result);
      return result;
    }).pipe(Effect.provide(makeLayer(process.env.NEURATRADE_HOME))),
).pipe(Command.withDescription("Run multi-ticker paper-trading soak harness"));

const profileNameOption = Options.text("name").pipe(
  Options.withDescription(
    "Profile name (used as filename in ~/.neuratrade/profiles)",
  ),
);

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

export const scalpCommand = Command.make("scalp", {}, () =>
  Console.log(
    "Scalping commands. Use 'scalp backtest|optimize|scan|paper-trade|soak|profile --help' for details.",
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
  ]),
);
