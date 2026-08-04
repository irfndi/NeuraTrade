import { Command, Options } from "./kit/kit.ts";
import { BunServices } from "@effect/platform-bun";
import { Console, Effect, FileSystem, Layer, Option } from "effect";
import { dirname, isAbsolute, resolve } from "node:path";
import { Path, PathLive } from "../services/path.js";
import { ConfigLive } from "../services/config.js";
import {
  SqliteClient,
  SqliteClientLiveRaw,
  SqliteError,
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
import { MarketDataGatewayRepositoryLive } from "../market-data/gateway-repository.js";
import { SimulatedExchangeAdapterLive } from "../exchange/adapters/simulated.js";
import { BinanceLiveExchangeAdapterLive } from "../exchange/adapters/binance-live.js";
import { SimulatedFuturesExchangeAdapterLive } from "../exchange/adapters/simulated-futures.js";
import { BitgetFuturesExchangeAdapterLive } from "../exchange/adapters/bitget-futures.js";
import { BitgetClientLiveConfig } from "../services/bitget-client.js";
import type { FuturesMarginMode } from "../exchange/futures-adapter.js";
import { RiskGuardLive } from "../risk/guards.js";
import { KillSwitch, KillSwitchSQLiteLive } from "../risk/kill-switch.js";
import { CircuitBreakerSQLiteLive } from "../risk/circuit-breaker.js";
import { Decimal, money } from "../utils/money.js";
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
import type { BitgetProductType } from "../services/bitget-client.js";
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
import { VALIDATED_BTC_GRID_CANDIDATE } from "../scalping/grid-candidate.js";
import { applyPreset } from "../scalping/presets.js";
import {
  buildComposerConfigFromTemplate,
  type StrategyTemplateName,
} from "../scalping/strategy-library.js";
import { runWalkForward } from "../scalping/walk-forward.js";
import { makeDemoReadinessCommand } from "./demo-readiness.js";

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
const makerFeeOption = Options.float("maker-fee-pct").pipe(
  Options.withDefault(0),
  Options.withDescription("Maker fee percent per side (for limit entries)"),
);

const entryOrderTypeOption = Options.choice("entry-order-type", [
  "market",
  "limit",
] as const).pipe(
  Options.withDefault("market" as const),
  Options.withDescription("Entry fill type: market or limit"),
);

const entryLimitOffsetBpsOption = Options.float("entry-limit-offset-bps").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Limit entry offset from signal price in basis points",
  ),
);

const rsiPeriodOption = Options.integer("rsi-period").pipe(
  Options.withDefault(14),
  Options.withDescription("RSI lookback period"),
);

const rsiOversoldStrongOption = Options.float("rsi-oversold-strong").pipe(
  Options.withDefault(30),
  Options.withDescription("RSI level considered strongly oversold"),
);

const rsiOverboughtStrongOption = Options.float("rsi-overbought-strong").pipe(
  Options.withDefault(70),
  Options.withDescription("RSI level considered strongly overbought"),
);

const trendFilterPeriodOption = Options.integer("trend-filter-period").pipe(
  Options.withDefault(200),
  Options.withDescription("SMA trend filter period for Connors RSI(2) entries"),
);

const entryRsiLongThresholdOption = Options.float(
  "entry-rsi-long-threshold",
).pipe(
  Options.withDefault(10),
  Options.withDescription("RSI(2) long entry threshold"),
);

const entryRsiShortThresholdOption = Options.float(
  "entry-rsi-short-threshold",
).pipe(
  Options.withDefault(90),
  Options.withDescription("RSI(2) short entry threshold"),
);

const exitRsiPeriodOption = Options.integer("exit-rsi-period").pipe(
  Options.withDefault(0),
  Options.withDescription("RSI exit lookback period (0 disables RSI exits)"),
);

const exitRsiLongLevelOption = Options.float("exit-rsi-long-level").pipe(
  Options.withDefault(0),
  Options.withDescription("Close long when RSI rises above this level"),
);

const exitRsiShortLevelOption = Options.float("exit-rsi-short-level").pipe(
  Options.withDefault(0),
  Options.withDescription("Close short when RSI falls below this level"),
);

const observedPriceOption = Options.boolean("observed-price").pipe(
  Options.withDefault(false),
  Options.withDescription("Use observed price (close-only) for entries"),
);

const realisticOption = Options.boolean("realistic").pipe(
  Options.withDefault(false),
  Options.withDescription("Apply realistic slippage and fee assumptions"),
);

const strictRealismOption = Options.boolean("strict-realism").pipe(
  Options.withDefault(false),
  Options.withDescription("Use strict realism (close-only + slippage)"),
);

const realisticSlippageBpsOption = Options.float("realistic-slippage-bps").pipe(
  Options.withDefault(5),
  Options.withDescription("Realistic slippage in basis points"),
);

const trendSignalStyleOption = Options.choice("trend-signal-style", [
  "slope",
  "cross",
] as const).pipe(
  Options.withDefault("slope" as const),
  Options.withDescription("Trend signal style: slope or cross"),
);

const trendFastPeriodOption = Options.integer("trend-fast-period").pipe(
  Options.withDefault(9),
  Options.withDescription("Fast EMA period for trend component"),
);

const trendSlowPeriodOption = Options.integer("trend-slow-period").pipe(
  Options.withDefault(21),
  Options.withDescription("Slow EMA period for trend component"),
);

const directionalOnlyOption = Options.boolean("directional-only").pipe(
  Options.withDefault(false),
  Options.withDescription("Use only directional components as signals"),
);

const rsiFollowTrendOption = Options.boolean("rsi-follow-trend").pipe(
  Options.withDefault(false),
  Options.withDescription("RSI confirms trend direction instead of fading"),
);

const strictAgreementOption = Options.boolean("strict-agreement").pipe(
  Options.withDefault(false),
  Options.withDescription("Require all directional components to agree"),
);

const entryOnCloseOption = Options.boolean("entry-on-close").pipe(
  Options.withDefault(false),
  Options.withDescription("Enter at the close of the signal candle"),
);

const breakoutLookbackOption = Options.integer("breakout-lookback").pipe(
  Options.withDefault(20),
  Options.withDescription("Breakout regime lookback period"),
);

const breakoutVolumeMinRatioOption = Options.float(
  "breakout-volume-min-ratio",
).pipe(
  Options.withDefault(1.2),
  Options.withDescription("Breakout minimum volume ratio"),
);

const breakoutAdxMinOption = Options.float("breakout-adx-min").pipe(
  Options.withDefault(20),
  Options.withDescription("Breakout minimum ADX threshold"),
);

const fundingBiasThresholdOption = Options.float("funding-bias-threshold").pipe(
  Options.withDefault(0.0001),
  Options.withDescription("Funding bias threshold for contrarian signal"),
);

const useFundingOption = Options.boolean("use-funding").pipe(
  Options.withDefault(false),
  Options.withDescription("Enable funding-rate bias component"),
);

const strategyTypeOption = Options.choice("strategy-type", [
  "signal",
  "grid",
] as const).pipe(
  Options.withDefault("signal" as const),
  Options.withDescription("Strategy type: signal-based or grid scalping"),
);

const gridStepPctOption = Options.float("grid-step-pct").pipe(
  Options.withDefault(0),
  Options.withDescription("Grid step percentage (0 disables grid)"),
);

const gridMaxGridsOption = Options.float("grid-max-grids").pipe(
  Options.withDefault(0),
  Options.withDescription("Maximum number of grid levels"),
);

const gridPauseAfterLossBarsOption = Options.integer(
  "grid-pause-after-loss-bars",
).pipe(
  Options.withDefault(0),
  Options.withDescription("Bars to pause grid after a loss"),
);

const onlyWithTrendOption = Options.boolean("only-with-trend").pipe(
  Options.withDefault(false),
  Options.withDescription(
    "Grid: only enter long above trend SMA and short below it",
  ),
);

const targetRatioOption = Options.float("target-ratio").pipe(
  Options.withDefault(1),
  Options.withDescription(
    "Grid: target distance as a multiple of the grid step (default 1.0)",
  ),
);

const chopGateAdxOption = Options.float("chop-gate-adx").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Grid: skip new entries while causal ADX(14) >= this threshold (0 = disabled)",
  ),
);

const volatilityTargetAnnualPctOption = Options.float(
  "volatility-target-annual-pct",
).pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Annual volatility target percent for position sizing",
  ),
);

const noAtrOption = Options.boolean("no-atr").pipe(
  Options.withDefault(false),
  Options.withDescription("Disable ATR stops and use fixed percent stops"),
);

const scanEntryOrdersOption = Options.boolean("scan-entry-orders").pipe(
  Options.withDefault(false),
  Options.withDescription("Scan market/limit entry order types in optimize"),
);

const randomSearchOption = Options.integer("random-search").pipe(
  Options.withDefault(0),
  Options.withDescription("Random search sample count (0 = full grid)"),
);

const walkForwardOption = Options.boolean("walk-forward").pipe(
  Options.withDefault(false),
  Options.withDescription("Run walk-forward optimization"),
);

const wfTrainDaysOption = Options.integer("wf-train-days").pipe(
  Options.withDefault(180),
  Options.withDescription("Walk-forward training window in days"),
);

const wfTestDaysOption = Options.integer("wf-test-days").pipe(
  Options.withDefault(60),
  Options.withDescription("Walk-forward testing window in days"),
);

const wfStepDaysOption = Options.integer("wf-step-days").pipe(
  Options.withDefault(60),
  Options.withDescription("Walk-forward step size in days"),
);

const minTradesOption = Options.integer("min-trades").pipe(
  Options.withDefault(0),
  Options.withDescription("Minimum trades filter for optimization/selection"),
);

const minOosTradesOption = Options.integer("min-oos-trades").pipe(
  Options.withDefault(0),
  Options.withDescription("Minimum out-of-sample trades filter"),
);

const selectByOption = Options.choice("select-by", [
  "return",
  "sharpe",
  "calmar",
] as const).pipe(
  Options.withDefault("return" as const),
  Options.withDescription("Objective for selecting best candidate"),
);

const stopLossMinOption = Options.float("stop-loss-min").pipe(
  Options.withDefault(0),
  Options.withDescription("Minimum fixed stop loss percent to test"),
);

const stopLossMaxOption = Options.float("stop-loss-max").pipe(
  Options.withDefault(0),
  Options.withDescription("Maximum fixed stop loss percent to test"),
);

const stopLossStepOption = Options.float("stop-loss-step").pipe(
  Options.withDefault(0),
  Options.withDescription("Step size for fixed stop loss percent"),
);

const takeProfitMinOption = Options.float("take-profit-min").pipe(
  Options.withDefault(0),
  Options.withDescription("Minimum fixed take profit percent to test"),
);

const takeProfitMaxOption = Options.float("take-profit-max").pipe(
  Options.withDefault(0),
  Options.withDescription("Maximum fixed take profit percent to test"),
);

const takeProfitStepOption = Options.float("take-profit-step").pipe(
  Options.withDefault(0),
  Options.withDescription("Step size for fixed take profit percent"),
);

const breakevenAtRMinOption = Options.float("breakeven-at-r-min").pipe(
  Options.withDefault(0),
  Options.withDescription("Minimum breakeven-at-R to test"),
);

const breakevenAtRMaxOption = Options.float("breakeven-at-r-max").pipe(
  Options.withDefault(0),
  Options.withDescription("Maximum breakeven-at-R to test"),
);

const breakevenAtRStepOption = Options.float("breakeven-at-r-step").pipe(
  Options.withDefault(0),
  Options.withDescription("Step size for breakeven-at-R"),
);

const maxBarsInTradeMinOption = Options.integer("max-bars-in-trade-min").pipe(
  Options.withDefault(0),
  Options.withDescription("Minimum max-bars-in-trade to test"),
);

const maxBarsInTradeMaxOption = Options.integer("max-bars-in-trade-max").pipe(
  Options.withDefault(0),
  Options.withDescription("Maximum max-bars-in-trade to test"),
);

const maxBarsInTradeStepOption = Options.integer("max-bars-in-trade-step").pipe(
  Options.withDefault(0),
  Options.withDescription("Step size for max-bars-in-trade"),
);

const lossCooldownBarsMinOption = Options.integer(
  "loss-cooldown-bars-min",
).pipe(
  Options.withDefault(0),
  Options.withDescription("Minimum loss-cooldown-bars to test"),
);

const lossCooldownBarsMaxOption = Options.integer(
  "loss-cooldown-bars-max",
).pipe(
  Options.withDefault(0),
  Options.withDescription("Maximum loss-cooldown-bars to test"),
);

const lossCooldownBarsStepOption = Options.integer(
  "loss-cooldown-bars-step",
).pipe(
  Options.withDefault(0),
  Options.withDescription("Step size for loss-cooldown-bars"),
);

const adxMinMinOption = Options.float("adx-min-min").pipe(
  Options.withDefault(0),
  Options.withDescription("Minimum ADX filter to test"),
);

const adxMinMaxOption = Options.float("adx-min-max").pipe(
  Options.withDefault(0),
  Options.withDescription("Maximum ADX filter to test"),
);

const adxMinStepOption = Options.float("adx-min-step").pipe(
  Options.withDefault(0),
  Options.withDescription("Step size for ADX filter"),
);

const minEfficiencyRatioMinOption = Options.float(
  "min-efficiency-ratio-min",
).pipe(
  Options.withDefault(0),
  Options.withDescription("Minimum efficiency ratio to test"),
);

const minEfficiencyRatioMaxOption = Options.float(
  "min-efficiency-ratio-max",
).pipe(
  Options.withDefault(0),
  Options.withDescription("Maximum efficiency ratio to test"),
);

const minEfficiencyRatioStepOption = Options.float(
  "min-efficiency-ratio-step",
).pipe(
  Options.withDefault(0),
  Options.withDescription("Step size for efficiency ratio"),
);

const rsiLongMaxMinOption = Options.float("rsi-long-max-min").pipe(
  Options.withDefault(0),
  Options.withDescription("Minimum RSI long max filter to test"),
);

const rsiLongMaxMaxOption = Options.float("rsi-long-max-max").pipe(
  Options.withDefault(0),
  Options.withDescription("Maximum RSI long max filter to test"),
);

const rsiLongMaxStepOption = Options.float("rsi-long-max-step").pipe(
  Options.withDefault(0),
  Options.withDescription("Step size for RSI long max filter"),
);

const rsiShortMinMinOption = Options.float("rsi-short-min-min").pipe(
  Options.withDefault(0),
  Options.withDescription("Minimum RSI short min filter to test"),
);

const rsiShortMinMaxOption = Options.float("rsi-short-min-max").pipe(
  Options.withDefault(0),
  Options.withDescription("Maximum RSI short min filter to test"),
);

const rsiShortMinStepOption = Options.float("rsi-short-min-step").pipe(
  Options.withDefault(0),
  Options.withDescription("Step size for RSI short min filter"),
);

const strategyOption = Options.choice("strategy", [
  "meanReversion",
  "trendFollowing",
  "breakout",
  "emaPullback",
  "momentum",
  "rangeExpansion",
  "fundingCarry",
  "dualEmaCross",
  "ensemble",
  "microScalp",
  "connorsRsi2",
  "gridScalp",
] as const).pipe(
  Options.withDefault("meanReversion" as const),
  Options.withDescription("Strategy template for paper-trade"),
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

const htfSignalConfidenceOption = Options.float("htf-signal-confidence").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Confidence boost when HTF trend aligns (0 disables)",
  ),
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
  "breakout",
] as const).pipe(
  Options.withDefault("trend" as const),
  Options.withDescription(
    "Regime filter mode: trend-following or mean-reversion",
  ),
);

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
    },
  };
}

export function backtestProgram(args: ResolvedBacktestArgs) {
  return Effect.gen(function* () {
    const repo = yield* MarketDataRepository;
    const path = yield* Path;
    const engine = yield* BacktestEngine;

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

const intervalOption = Options.integer("interval").pipe(
  Options.withDefault(60),
  Options.withDescription("Seconds between paper-trading iterations"),
);

const iterationsOption = Options.integer("iterations").pipe(
  Options.withDefault(1),
  Options.withDescription("Number of iterations to run (0 = infinite)"),
);

const replayBarsOption = Options.integer("replay-bars").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Grid: replay the last N stored candles one per iteration (0 = live shadow)",
  ),
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

export interface WatchlistEntry {
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

      const profile = yield* loadProfileIfNeeded(path.homeDir, args.profile);
      const mergedArgs = Option.isSome(profile)
        ? mergePaperTradeArgs(args as unknown as PaperTradeArgs, profile.value)
        : (args as unknown as PaperTradeArgs);

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
  return futures && exchange === "binance" ? "bitget-futures" : exchange;
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
  sandboxValue: string | undefined,
): string | undefined {
  if (live && sandboxValue !== "true" && sandboxValue !== "1") {
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
): string | undefined {
  const validatedCandidate =
    config.exchange === VALIDATED_BTC_GRID_CANDIDATE.exchange &&
    config.symbol === VALIDATED_BTC_GRID_CANDIDATE.symbol &&
    config.timeframe === VALIDATED_BTC_GRID_CANDIDATE.timeframe &&
    config.productType === VALIDATED_BTC_GRID_CANDIDATE.productType &&
    config.gridStepPct === VALIDATED_BTC_GRID_CANDIDATE.gridStepPct &&
    config.gridMaxGrids === VALIDATED_BTC_GRID_CANDIDATE.gridMaxGrids &&
    config.gridPauseAfterLossBars ===
      VALIDATED_BTC_GRID_CANDIDATE.gridPauseAfterLossBars &&
    config.feePct === VALIDATED_BTC_GRID_CANDIDATE.feePct &&
    config.slippageBps === VALIDATED_BTC_GRID_CANDIDATE.slippageBps &&
    config.trendFilterPeriod ===
      VALIDATED_BTC_GRID_CANDIDATE.trendFilterPeriod &&
    config.onlyWithTrend === VALIDATED_BTC_GRID_CANDIDATE.onlyWithTrend &&
    config.targetRatio === VALIDATED_BTC_GRID_CANDIDATE.targetRatio &&
    config.chopGateAdx === VALIDATED_BTC_GRID_CANDIDATE.chopGateAdx &&
    config.leverage === VALIDATED_BTC_GRID_CANDIDATE.leverage;
  if (!validatedCandidate) {
    return "live grid must use the validated BTC 15m grid candidate";
  }
  if (
    !Number.isFinite(config.maxPositionSizePct) ||
    config.maxPositionSizePct <= 0 ||
    config.maxPositionSizePct > VALIDATED_BTC_GRID_CANDIDATE.maxPositionSizePct
  ) {
    return "live grid max position size must be between 0% and 50%";
  }
  if (
    !Number.isFinite(config.maxDrawdownPct) ||
    config.maxDrawdownPct <= 0 ||
    config.maxDrawdownPct > VALIDATED_BTC_GRID_CANDIDATE.maxDrawdownPct
  ) {
    return "live grid max drawdown must be between 0% and 5%";
  }
  if (
    !Number.isFinite(config.maxDailyLossPct) ||
    config.maxDailyLossPct <= 0 ||
    config.maxDailyLossPct > VALIDATED_BTC_GRID_CANDIDATE.maxDailyLossPct
  ) {
    return "live grid max daily loss must be between 0% and 2%";
  }
  return undefined;
}

export function validateLiveGridWatchlist(
  live: boolean,
  strategyType: "signal" | "grid",
  entries: readonly Pick<WatchlistEntry, "symbol">[] | undefined,
): string | undefined {
  if (
    live &&
    strategyType === "grid" &&
    entries !== undefined &&
    entries.length > 0
  ) {
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

function paperTradeProgram(args: PaperTradeArgs) {
  return Effect.gen(function* () {
    const strategyType = args.strategyType ?? "signal";
    const liveMarketError = validateLiveExecutionMarket(
      args.live,
      args.futures,
    );
    if (liveMarketError !== undefined) {
      return yield* Effect.fail(new Error(liveMarketError));
    }
    const liveSandboxError = validateLiveSandboxMode(
      args.live,
      process.env.BITGET_USE_SANDBOX,
    );
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
    );
    if (liveGridWatchlistError !== undefined) {
      return yield* Effect.fail(new Error(liveGridWatchlistError));
    }
    const marginMode = parseMarginMode(args.marginMode);
    const productType = parseProductType(args.productType);
    if (args.live && strategyType === "grid") {
      const liveGridError = validateLiveGridConfiguration({
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
      });
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

    // Futures data and execution both live on Bitget in this port; default the
    // market-data exchange to bitget-futures unless the operator overrides it.
    const makeFuturesOptions = (
      symbol: string,
      exchangeOverride: string,
      overrides?: Partial<FuturesPaperTradingOptions>,
    ): FuturesPaperTradingOptions => ({
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
      volatilityTargetAnnualPct: args.volatilityTargetAnnualPct,
    });

    const makeGridOptions = (
      symbol: string,
      exchange: string,
    ): GridPaperTradingOptions => ({
      exchange: resolveFuturesMarketExchange(exchange, true),
      symbol,
      timeframe: args.timeframe,
      gridStepPct: args.gridStepPct,
      gridMaxGrids: args.gridMaxGrids,
      gridPauseAfterLossBars: args.gridPauseAfterLossBars,
      feePct: args.fee,
      slippageBps: args.slippageBps,
      trendFilterPeriod: args.trendFilterPeriod,
      initialCapital: args.capital,
      maxPositionPct: Option.getOrElse(args.maxPositionSizePct, () => 100),
      maxDrawdownPct: Option.getOrElse(args.maxDrawdownPct, () => 100),
      leverage: args.leverage,
      onlyWithTrend: args.onlyWithTrend,
      targetRatio: args.targetRatio,
      chopGateAdxThreshold: args.chopGateAdx,
      replayBars: args.replayBars > 0 ? args.replayBars : undefined,
      isLive: args.live,
      executionEnvironment:
        args.live &&
        process.env.BITGET_USE_SANDBOX !== "true" &&
        process.env.BITGET_USE_SANDBOX !== "1"
          ? "bitget-live"
          : "bitget-demo",
      productType,
      marginMode,
    });

    const spotAdapterLayer = args.live
      ? BinanceLiveExchangeAdapterLive({
          apiKey: args.apiKey || process.env.BINANCE_API_KEY || "",
          apiSecret: args.apiSecret || process.env.BINANCE_API_SECRET || "",
        })
      : SimulatedExchangeAdapterLive();
    const futuresAdapterLayer = (args.live
      ? BitgetFuturesExchangeAdapterLive.pipe(
          Layer.provide(BitgetClientLiveConfig),
        )
      : SimulatedFuturesExchangeAdapterLive()) as Layer.Layer<
      import("../exchange/futures-adapter.js").FuturesExchangeAdapterService,
      never,
      import("../market-data/gateway.js").MarketDataGatewayService
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
              capital: state ? Number(state.capital) : 0,
              peakCapital: state ? Number(state.peakCapital) : 0,
              note: `skip: ${reason}`,
            };
          }),
        ),
      ) as Effect.Effect<GridPaperTradingIterationResult, never, never>;

    let remaining = args.iterations;
    // iterations=0 means run forever.
    while (args.iterations === 0 || remaining !== 0) {
      if (entries) {
        for (const entry of entries) {
          if (remaining === 0 && args.iterations !== 0) break;
          const entryExchange = entry.exchange ?? args.exchange;
          const result =
            args.strategyType === "grid"
              ? yield* runGridIteration(
                  makeGridOptions(entry.symbol, entryExchange),
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
      const futuresAdapterLayer = (mergedArgs.live
        ? BitgetFuturesExchangeAdapterLive.pipe(
            Layer.provide(BitgetClientLiveConfig),
          )
        : SimulatedFuturesExchangeAdapterLive()) as Layer.Layer<
        import("../exchange/futures-adapter.js").FuturesExchangeAdapterService,
        never,
        import("../market-data/gateway.js").MarketDataGatewayService
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
          volatilityTargetAnnualPct: mergedArgs.volatilityTargetAnnualPct,
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

const trainWindowOption = Options.integer("train-window").pipe(
  Options.withDefault(180),
  Options.withDescription("Walk-forward training window in days"),
);

const testWindowOption = Options.integer("test-window").pipe(
  Options.withDefault(60),
  Options.withDescription("Walk-forward testing window in days"),
);

const wfMinTradesOption = Options.integer("min-trades").pipe(
  Options.withDefault(0),
  Options.withDescription("Minimum trades per window"),
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

const minTradesPerMonthOption = Options.integer("min-trades-per-month").pipe(
  Options.optional,
  Options.withDescription(
    "G1 override: minimum in-sample trades per month (default 20 for 5m, 10 otherwise)",
  ),
);

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

export const demoReadinessCommand = makeDemoReadinessCommand(
  makeDbLayer(process.env.NEURATRADE_HOME),
);

export const scalpCommand = Command.make("scalp", {}, () =>
  Console.log(
    "Scalping commands. Use 'scalp backtest|optimize|scan|paper-trade|soak|profile|readiness|demo-readiness --help' for details.",
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
  ]),
);
