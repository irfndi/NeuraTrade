/**
 * scalp CLI option surface.
 *
 * Extracted from src/cli/scalp.ts so the flat option plumbing lives apart
 * from command wiring. The min/max/step optimization-range triples are built
 * by the grouped `minMaxStep` builder (0 = disabled semantics encoded once),
 * and the load-bearing user-facing defaults are pinned in
 * SCALP_OPTION_DEFAULTS.
 */
import { Options } from "./kit/kit.ts";

/**
 * Grouped builder for the min/max/step optimization-range triples.
 * A triple default of 0 means "use the strategy's internal default" (the
 * range is disabled) — encoded here once instead of in each description.
 */
function minMaxStep(
  base: string,
  kind: "float" | "integer",
  label: string,
  defaults: {
    readonly min: number;
    readonly max: number;
    readonly step: number;
  } = {
    min: 0,
    max: 0,
    step: 0,
  },
) {
  return {
    min: Options[kind](`${base}-min`).pipe(
      Options.withDefault(defaults.min),
      Options.withDescription(`Minimum ${label} to test`),
    ),
    max: Options[kind](`${base}-max`).pipe(
      Options.withDefault(defaults.max),
      Options.withDescription(`Maximum ${label} to test`),
    ),
    step: Options[kind](`${base}-step`).pipe(
      Options.withDefault(defaults.step),
      Options.withDescription(`Step size for ${label}`),
    ),
  };
}

/**
 * Single defaults manifest for the scalp CLI's load-bearing user-facing
 * options. The builders below read their defaults from here so the flag
 * contract is pinned in one place (and asserted by src/cli/scalp.test.ts).
 */
export const SCALP_OPTION_DEFAULTS = {
  /** Initial capital in quote currency. */
  capital: 10000,
  /** Position size as percent of capital. */
  positionSize: 100,
  /** Risk per trade as percent of capital (0 = disabled, uses --position-size). */
  riskPerTrade: 0,
  /** Futures leverage multiplier. */
  leverage: 3,
  /** Max position size as percent of capital when risk-based sizing is on. */
  maxPositionSize: 100,
  /** Maximum number of grid levels (0 = disabled). */
  gridMaxGrids: 0,
} as const;

export const exchangeOption = Options.text("exchange").pipe(
  Options.withDefault("binance"),
  Options.withDescription("Exchange identifier"),
);

export const symbolOption = Options.text("symbol").pipe(
  Options.withDefault("BTC/USDT"),
  Options.withDescription("Trading pair symbol"),
);

export const timeframeOption = Options.text("timeframe").pipe(
  Options.withDefault("1h"),
  Options.withDescription("Candle timeframe"),
);

export const capitalOption = Options.integer("capital").pipe(
  Options.withDefault(SCALP_OPTION_DEFAULTS.capital),
  Options.withDescription("Initial capital in quote currency"),
);

export const positionSizeOption = Options.integer("position-size").pipe(
  Options.withDefault(SCALP_OPTION_DEFAULTS.positionSize),
  Options.withDescription("Position size as percent of capital"),
);

export const riskPerTradeOption = Options.float("risk-per-trade").pipe(
  Options.withDefault(SCALP_OPTION_DEFAULTS.riskPerTrade),
  Options.withDescription(
    "Risk per trade as percent of capital (overrides --position-size; 0 = disabled)",
  ),
);

export const riskBasedMaxPositionSizeOption = Options.float(
  "max-position-size",
).pipe(
  Options.withDefault(SCALP_OPTION_DEFAULTS.maxPositionSize),
  Options.withDescription(
    "Maximum position size as percent of capital when using --risk-per-trade",
  ),
);

export const stopLossOption = Options.float("stop-loss").pipe(
  Options.withDefault(1.5),
  Options.withDescription("Stop loss percent"),
);

export const takeProfitOption = Options.float("take-profit").pipe(
  Options.withDefault(3.0),
  Options.withDescription("Take profit percent"),
);

export const feeOption = Options.float("fee").pipe(
  Options.withDefault(0.1),
  Options.withDescription("Trading fee percent per side"),
);

export const futuresOption = Options.boolean("futures").pipe(
  Options.withDefault(false),
  Options.withDescription("Trade perpetual futures instead of spot"),
);

export const leverageOption = Options.integer("leverage").pipe(
  Options.withDefault(SCALP_OPTION_DEFAULTS.leverage),
  Options.withDescription("Futures leverage (default 3x)"),
);

export const fundingRateOption = Options.float("funding-rate-pct").pipe(
  Options.withDefault(0.01),
  Options.withDescription(
    "Per-interval funding cost in percent (default 0.01% every 8h)",
  ),
);

export const slippageBpsOption = Options.float("slippage-bps").pipe(
  Options.withDefault(0),
  Options.withDescription("Slippage in basis points applied to fills"),
);

export const trailingStopPctOption = Options.float("trailing-stop-pct").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Trail stop-loss this percentage behind the most favorable price (0 = disabled)",
  ),
);

export const trailingStopAtrMultOption = Options.float(
  "trailing-stop-atr-mult",
).pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Trail stop-loss at this ATR multiplier behind the most favorable price (0 = disabled)",
  ),
);

export const minAtrPctOption = Options.float("min-atr-pct").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Minimum ATR% required to enter a trade, filters low-volatility chop (0 = disabled)",
  ),
);

export const adxMinOption = Options.float("adx-min").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Minimum ADX required by the regime filter (0 = use default adxWeakTrend)",
  ),
);

export const volumeMinRatioOption = Options.float("volume-min-ratio").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Require current volume >= this ratio of its moving average to enter (0 = disabled)",
  ),
);

export const volumeLookbackOption = Options.integer("volume-lookback").pipe(
  Options.withDefault(20),
  Options.withDescription("Lookback period for volume moving average filter"),
);

export const minConfluenceOption = Options.integer("min-confluence").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Minimum number of components that must agree to enter (0 = disabled)",
  ),
);

export const entryCandleConfirmOption = Options.boolean(
  "entry-candle-confirm",
).pipe(
  Options.withDefault(false),
  Options.withDescription(
    "Require the entry candle body to align with the signal direction",
  ),
);

export const signalPersistenceOption = Options.integer(
  "signal-persistence",
).pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Require the same directional signal for N consecutive candles before entering (0 = disabled)",
  ),
);

export const momentumConfirmBarsOption = Options.integer(
  "momentum-confirm-bars",
).pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Require net price movement over the last N candles to align with the signal (0 = disabled)",
  ),
);

export const makerFeeOption = Options.float("maker-fee-pct").pipe(
  Options.withDefault(0),
  Options.withDescription("Maker fee percent per side (for limit entries)"),
);

export const entryOrderTypeOption = Options.choice("entry-order-type", [
  "market",
  "limit",
] as const).pipe(
  Options.withDefault("market" as const),
  Options.withDescription("Entry fill type: market or limit"),
);

export const entryLimitOffsetBpsOption = Options.float(
  "entry-limit-offset-bps",
).pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Limit entry offset from signal price in basis points",
  ),
);

export const rsiPeriodOption = Options.integer("rsi-period").pipe(
  Options.withDefault(14),
  Options.withDescription("RSI lookback period"),
);

export const rsiOversoldStrongOption = Options.float(
  "rsi-oversold-strong",
).pipe(
  Options.withDefault(30),
  Options.withDescription("RSI level considered strongly oversold"),
);

export const rsiOverboughtStrongOption = Options.float(
  "rsi-overbought-strong",
).pipe(
  Options.withDefault(70),
  Options.withDescription("RSI level considered strongly overbought"),
);

export const trendFilterPeriodOption = Options.integer(
  "trend-filter-period",
).pipe(
  Options.withDefault(200),
  Options.withDescription("SMA trend filter period for Connors RSI(2) entries"),
);

export const entryRsiLongThresholdOption = Options.float(
  "entry-rsi-long-threshold",
).pipe(
  Options.withDefault(10),
  Options.withDescription("RSI(2) long entry threshold"),
);

export const entryRsiShortThresholdOption = Options.float(
  "entry-rsi-short-threshold",
).pipe(
  Options.withDefault(90),
  Options.withDescription("RSI(2) short entry threshold"),
);

export const exitRsiPeriodOption = Options.integer("exit-rsi-period").pipe(
  Options.withDefault(0),
  Options.withDescription("RSI exit lookback period (0 disables RSI exits)"),
);

export const exitRsiLongLevelOption = Options.float("exit-rsi-long-level").pipe(
  Options.withDefault(0),
  Options.withDescription("Close long when RSI rises above this level"),
);

export const exitRsiShortLevelOption = Options.float(
  "exit-rsi-short-level",
).pipe(
  Options.withDefault(0),
  Options.withDescription("Close short when RSI falls below this level"),
);

export const observedPriceOption = Options.boolean("observed-price").pipe(
  Options.withDefault(false),
  Options.withDescription("Use observed price (close-only) for entries"),
);

export const realisticOption = Options.boolean("realistic").pipe(
  Options.withDefault(false),
  Options.withDescription("Apply realistic slippage and fee assumptions"),
);

export const strictRealismOption = Options.boolean("strict-realism").pipe(
  Options.withDefault(false),
  Options.withDescription("Use strict realism (close-only + slippage)"),
);

export const realisticSlippageBpsOption = Options.float(
  "realistic-slippage-bps",
).pipe(
  Options.withDefault(5),
  Options.withDescription("Realistic slippage in basis points"),
);

export const trendSignalStyleOption = Options.choice("trend-signal-style", [
  "slope",
  "cross",
] as const).pipe(
  Options.withDefault("slope" as const),
  Options.withDescription("Trend signal style: slope or cross"),
);

export const trendFastPeriodOption = Options.integer("trend-fast-period").pipe(
  Options.withDefault(9),
  Options.withDescription("Fast EMA period for trend component"),
);

export const trendSlowPeriodOption = Options.integer("trend-slow-period").pipe(
  Options.withDefault(21),
  Options.withDescription("Slow EMA period for trend component"),
);

export const directionalOnlyOption = Options.boolean("directional-only").pipe(
  Options.withDefault(false),
  Options.withDescription("Use only directional components as signals"),
);

export const rsiFollowTrendOption = Options.boolean("rsi-follow-trend").pipe(
  Options.withDefault(false),
  Options.withDescription("RSI confirms trend direction instead of fading"),
);

export const strictAgreementOption = Options.boolean("strict-agreement").pipe(
  Options.withDefault(false),
  Options.withDescription("Require all directional components to agree"),
);

export const entryOnCloseOption = Options.boolean("entry-on-close").pipe(
  Options.withDefault(false),
  Options.withDescription("Enter at the close of the signal candle"),
);

export const breakoutLookbackOption = Options.integer("breakout-lookback").pipe(
  Options.withDefault(20),
  Options.withDescription("Breakout regime lookback period"),
);

export const breakoutVolumeMinRatioOption = Options.float(
  "breakout-volume-min-ratio",
).pipe(
  Options.withDefault(1.2),
  Options.withDescription("Breakout minimum volume ratio"),
);

export const breakoutAdxMinOption = Options.float("breakout-adx-min").pipe(
  Options.withDefault(20),
  Options.withDescription("Breakout minimum ADX threshold"),
);

export const fundingBiasThresholdOption = Options.float(
  "funding-bias-threshold",
).pipe(
  Options.withDefault(0.0001),
  Options.withDescription("Funding bias threshold for contrarian signal"),
);

export const useFundingOption = Options.boolean("use-funding").pipe(
  Options.withDefault(false),
  Options.withDescription("Enable funding-rate bias component"),
);

export const strategyTypeOption = Options.choice("strategy-type", [
  "signal",
  "grid",
] as const).pipe(
  Options.withDefault("signal" as const),
  Options.withDescription("Strategy type: signal-based or grid scalping"),
);

export const gridStepPctOption = Options.float("grid-step-pct").pipe(
  Options.withDefault(0),
  Options.withDescription("Grid step percentage (0 disables grid)"),
);

export const gridMaxGridsOption = Options.integer("grid-max-grids").pipe(
  Options.withDefault(SCALP_OPTION_DEFAULTS.gridMaxGrids),
  Options.withDescription("Maximum number of grid levels"),
);

export const gridPauseAfterLossBarsOption = Options.integer(
  "grid-pause-after-loss-bars",
).pipe(
  Options.withDefault(0),
  Options.withDescription("Bars to pause grid after a loss"),
);

export const onlyWithTrendOption = Options.boolean("only-with-trend").pipe(
  Options.withDefault(false),
  Options.withDescription(
    "Grid: only enter long above trend SMA and short below it",
  ),
);

export const targetRatioOption = Options.float("target-ratio").pipe(
  Options.withDefault(1),
  Options.withDescription(
    "Grid: target distance as a multiple of the grid step (default 1.0)",
  ),
);

export const chopGateAdxOption = Options.float("chop-gate-adx").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Grid: skip new entries while causal ADX(14) >= this threshold (0 = disabled)",
  ),
);

export const volatilityTargetAnnualPctOption = Options.float(
  "volatility-target-annual-pct",
).pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Annual volatility target percent for position sizing",
  ),
);

export const noAtrOption = Options.boolean("no-atr").pipe(
  Options.withDefault(false),
  Options.withDescription("Disable ATR stops and use fixed percent stops"),
);

export const scanEntryOrdersOption = Options.boolean("scan-entry-orders").pipe(
  Options.withDefault(false),
  Options.withDescription("Scan market/limit entry order types in optimize"),
);

export const randomSearchOption = Options.integer("random-search").pipe(
  Options.withDefault(0),
  Options.withDescription("Random search sample count (0 = full grid)"),
);

export const walkForwardOption = Options.boolean("walk-forward").pipe(
  Options.withDefault(false),
  Options.withDescription("Run walk-forward optimization"),
);

export const wfTrainDaysOption = Options.integer("wf-train-days").pipe(
  Options.withDefault(180),
  Options.withDescription("Walk-forward training window in days"),
);

export const wfTestDaysOption = Options.integer("wf-test-days").pipe(
  Options.withDefault(60),
  Options.withDescription("Walk-forward testing window in days"),
);

export const wfStepDaysOption = Options.integer("wf-step-days").pipe(
  Options.withDefault(60),
  Options.withDescription("Walk-forward step size in days"),
);

export const minTradesOption = Options.integer("min-trades").pipe(
  Options.withDefault(0),
  Options.withDescription("Minimum trades filter for optimization/selection"),
);

export const minOosTradesOption = Options.integer("min-oos-trades").pipe(
  Options.withDefault(0),
  Options.withDescription("Minimum out-of-sample trades filter"),
);

export const selectByOption = Options.choice("select-by", [
  "return",
  "sharpe",
  "calmar",
] as const).pipe(
  Options.withDefault("return" as const),
  Options.withDescription("Objective for selecting best candidate"),
);

export const {
  min: stopLossMinOption,
  max: stopLossMaxOption,
  step: stopLossStepOption,
} = minMaxStep("stop-loss", "float", "fixed stop loss percent");

export const {
  min: takeProfitMinOption,
  max: takeProfitMaxOption,
  step: takeProfitStepOption,
} = minMaxStep("take-profit", "float", "fixed take profit percent");

export const {
  min: breakevenAtRMinOption,
  max: breakevenAtRMaxOption,
  step: breakevenAtRStepOption,
} = minMaxStep("breakeven-at-r", "float", "breakeven-at-R");

export const {
  min: maxBarsInTradeMinOption,
  max: maxBarsInTradeMaxOption,
  step: maxBarsInTradeStepOption,
} = minMaxStep("max-bars-in-trade", "integer", "max-bars-in-trade");

export const {
  min: lossCooldownBarsMinOption,
  max: lossCooldownBarsMaxOption,
  step: lossCooldownBarsStepOption,
} = minMaxStep("loss-cooldown-bars", "integer", "loss-cooldown-bars");

export const {
  min: adxMinMinOption,
  max: adxMinMaxOption,
  step: adxMinStepOption,
} = minMaxStep("adx-min", "float", "ADX filter");

export const {
  min: minEfficiencyRatioMinOption,
  max: minEfficiencyRatioMaxOption,
  step: minEfficiencyRatioStepOption,
} = minMaxStep("min-efficiency-ratio", "float", "efficiency ratio");

export const {
  min: rsiLongMaxMinOption,
  max: rsiLongMaxMaxOption,
  step: rsiLongMaxStepOption,
} = minMaxStep("rsi-long-max", "float", "RSI long max filter");

export const {
  min: rsiShortMinMinOption,
  max: rsiShortMinMaxOption,
  step: rsiShortMinStepOption,
} = minMaxStep("rsi-short-min", "float", "RSI short min filter");

export const strategyOption = Options.choice("strategy", [
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

export const lossConfidencePenaltyOption = Options.float(
  "loss-confidence-penalty",
).pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Boost min-confidence by this amount after a losing trade (0 = disabled)",
  ),
);

export const lossConfidenceDecayOption = Options.float(
  "loss-confidence-decay",
).pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Decay the post-loss confidence penalty by this amount each candle (0 = step reset)",
  ),
);

export const htfTimeframeOption = Options.text("htf-timeframe").pipe(
  Options.withDefault(""),
  Options.withDescription(
    "Higher-timeframe for trend filter (e.g. 1h). Empty disables the filter.",
  ),
);

export const htfTrendFastPeriodOption = Options.integer("htf-trend-fast").pipe(
  Options.withDefault(50),
  Options.withDescription("Higher-timeframe EMA fast period for trend filter"),
);

export const htfTrendSlowPeriodOption = Options.integer("htf-trend-slow").pipe(
  Options.withDefault(100),
  Options.withDescription("Higher-timeframe EMA slow period for trend filter"),
);

export const htfSignalConfidenceOption = Options.float(
  "htf-signal-confidence",
).pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Confidence boost when HTF trend aligns (0 disables)",
  ),
);

export const entryPullbackEmaPeriodOption = Options.integer(
  "entry-pullback-ema-period",
).pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Only enter when price is within --entry-pullback-margin-pct of this EMA period (0 = disabled)",
  ),
);

export const entryPullbackMarginPctOption = Options.float(
  "entry-pullback-margin-pct",
).pipe(
  Options.withDefault(0.1),
  Options.withDescription(
    "Allowed distance from the pullback EMA as a percentage of price",
  ),
);

export const minEfficiencyRatioOption = Options.float(
  "min-efficiency-ratio",
).pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Minimum Kaufman Efficiency Ratio (0-1) required to enter; high values filter chop",
  ),
);

export const efficiencyRatioPeriodOption = Options.integer(
  "efficiency-ratio-period",
).pipe(
  Options.withDefault(20),
  Options.withDescription("Lookback period for the efficiency-ratio filter"),
);

export const rsiLongMaxOption = Options.float("rsi-long-max").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Leading RSI filter: only enter longs when RSI <= this value (0 = disabled)",
  ),
);

export const rsiShortMinOption = Options.float("rsi-short-min").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Leading RSI filter: only enter shorts when RSI >= this value (0 = disabled)",
  ),
);

export const bollingerLongMaxPctBOption = Options.float(
  "bollinger-long-max-pctb",
).pipe(
  Options.withDefault(-1),
  Options.withDescription(
    "Leading Bollinger %B filter: only enter longs when %B <= this value (-1 = disabled)",
  ),
);

export const bollingerShortMinPctBOption = Options.float(
  "bollinger-short-min-pctb",
).pipe(
  Options.withDefault(2),
  Options.withDescription(
    "Leading Bollinger %B filter: only enter shorts when %B >= this value (2 = disabled)",
  ),
);

export const profileOption = Options.text("profile").pipe(
  Options.withDefault(""),
  Options.withDescription(
    "Strategy profile name to load from ~/.neuratrade/profiles",
  ),
);

export const recordEquityCurveOption = Options.boolean(
  "record-equity-curve",
).pipe(
  Options.withDefault(false),
  Options.withDescription("Record equity curve at each trade close"),
);

export const exportTradesOption = Options.text("export-trades").pipe(
  Options.withDefault(""),
  Options.withDescription(
    "Write trades.csv (+ equity.csv if --record-equity-curve) to this path",
  ),
);

export const oosPctOption = Options.integer("oos-pct").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Percentage of recent candles to hold out for out-of-sample validation (0 disables)",
  ),
);

export const mcIterationsOption = Options.integer("mc-iterations").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Number of Monte Carlo permutations for drawdown simulation (0 disables)",
  ),
);

export const breakevenAtROption = Options.float("breakeven-at-r").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Move stop-loss to breakeven once price reaches this R profit (0 disables)",
  ),
);

export const maxBarsInTradeOption = Options.integer("max-bars-in-trade").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Maximum bars to hold a position before time-stop exit (0 disables)",
  ),
);

export const lossCooldownBarsOption = Options.integer(
  "loss-cooldown-bars",
).pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Bars to skip after a losing trade before allowing new entries (0 disables)",
  ),
);

export const sessionStartOption = Options.text("session-start").pipe(
  Options.withDefault(""),
  Options.withDescription("UTC session start in HH:MM (empty disables)"),
);

export const sessionEndOption = Options.text("session-end").pipe(
  Options.withDefault(""),
  Options.withDescription("UTC session end in HH:MM (empty disables)"),
);

export const autoRegimeFilterOption = Options.boolean(
  "auto-regime-filter",
).pipe(
  Options.withDefault(false),
  Options.withDescription(
    "Block entries that do not match the detected trend/mean-reversion regime",
  ),
);

export const autoRegimeAdxThresholdOption = Options.float(
  "auto-regime-adx-threshold",
).pipe(
  Options.withDefault(25),
  Options.withDescription("ADX threshold for auto-regime detection"),
);

export const confidenceOption = Options.float("min-confidence").pipe(
  Options.withDefault(0.5),
  Options.withDescription("Minimum signal confidence to enter a trade"),
);

export const useAtrStopsOption = Options.boolean("use-atr-stops").pipe(
  Options.withDefault(false),
  Options.withDescription("Use ATR-based dynamic stop loss and take profit"),
);

export const atrStopMultiplierOption = Options.float(
  "atr-stop-multiplier",
).pipe(
  Options.withDefault(1.5),
  Options.withDescription(
    "ATR multiplier for stop loss when --use-atr-stops is set",
  ),
);

export const atrTakeProfitMultiplierOption = Options.float(
  "atr-take-profit-multiplier",
).pipe(
  Options.withDefault(2.5),
  Options.withDescription(
    "ATR multiplier for take profit when --use-atr-stops is set (legacy; overridden by --atr-risk-reward)",
  ),
);

export const atrRiskRewardOption = Options.float("atr-risk-reward").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "ATR take-profit distance as a multiple of the stop distance (0 = use --atr-take-profit-multiplier)",
  ),
);

export const scaleOutAtROption = Options.float("scale-out-at-r").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Partial scale-out trigger in R multiples (e.g. 1.0 = +1R). 0 disables scale-out.",
  ),
);

export const scaleOutPctOption = Options.float("scale-out-pct").pipe(
  Options.withDefault(50),
  Options.withDescription(
    "Percentage of the position to close at the scale-out trigger",
  ),
);

export const volatilityLookbackOption = Options.integer(
  "volatility-lookback",
).pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Lookback window length for ATR% volatility calibration. 0 disables.",
  ),
);

export const volatilityLowPctOption = Options.float("volatility-low-pct").pipe(
  Options.withDefault(20),
  Options.withDescription(
    "Low percentile threshold for volatility calibration",
  ),
);

export const volatilityHighPctOption = Options.float(
  "volatility-high-pct",
).pipe(
  Options.withDefault(80),
  Options.withDescription(
    "High percentile threshold for volatility calibration",
  ),
);

export const volatilityLowFactorOption = Options.float(
  "volatility-low-factor",
).pipe(
  Options.withDefault(0.8),
  Options.withDescription(
    "Multiplier applied to ATR stop distance in low-volatility regimes",
  ),
);

export const volatilityHighFactorOption = Options.float(
  "volatility-high-factor",
).pipe(
  Options.withDefault(1.2),
  Options.withDescription(
    "Multiplier applied to ATR stop distance in high-volatility regimes",
  ),
);

export const priceOnlyOption = Options.boolean("price-only").pipe(
  Options.withDefault(false),
  Options.withDescription(
    "Ignore synthetic order-book components in backtest (trend/volatility/RSI/regime only)",
  ),
);

export const noRsiOption = Options.boolean("no-rsi").pipe(
  Options.withDefault(false),
  Options.withDescription("Disable RSI mean-reversion component in backtest"),
);

export const holdUntilStopOption = Options.boolean("hold-until-stop").pipe(
  Options.withDefault(false),
  Options.withDescription(
    "Ignore opposite-signal exits and only exit on stop/take-profit",
  ),
);

export const noTrendOption = Options.boolean("no-trend").pipe(
  Options.withDefault(false),
  Options.withDescription("Disable trend-following EMA component in backtest"),
);

export const regimeModeOption = Options.choice("regime-mode", [
  "trend",
  "reversion",
  "breakout",
] as const).pipe(
  Options.withDefault("trend" as const),
  Options.withDescription(
    "Regime filter mode: trend-following or mean-reversion",
  ),
);

export const {
  min: atrStopMinOption,
  max: atrStopMaxOption,
  step: atrStopStepOption,
} = minMaxStep("atr-stop", "float", "ATR stop multiplier", {
  min: 1,
  max: 3,
  step: 0.5,
});

export const {
  min: atrTpMinOption,
  max: atrTpMaxOption,
  step: atrTpStepOption,
} = minMaxStep("atr-tp", "float", "ATR take-profit multiplier", {
  min: 2,
  max: 5,
  step: 0.5,
});

export const {
  min: confMinOption,
  max: confMaxOption,
  step: confStepOption,
} = minMaxStep("conf", "float", "min-confidence", {
  min: 0.5,
  max: 0.7,
  step: 0.1,
});

export const minCandlesOption = Options.integer("min-candles").pipe(
  Options.withDefault(500),
  Options.withDescription(
    "Minimum candles required for a symbol to be included in scan",
  ),
);

export const topOption = Options.integer("top").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Limit scan to top N symbols by candle count (0 = all)",
  ),
);

export const optimizeScanOption = Options.boolean("optimize").pipe(
  Options.withDefault(false),
  Options.withDescription(
    "Run a coarse per-symbol parameter grid search and report best params",
  ),
);

export const minReturnOption = Options.float("min-return-pct").pipe(
  Options.optional,
  Options.withDescription(
    "Skip symbols with total return below this threshold",
  ),
);

export const minSharpeOption = Options.float("min-sharpe").pipe(
  Options.optional,
  Options.withDescription(
    "Skip symbols with Sharpe ratio below this threshold",
  ),
);

export const scanMaxDrawdownOption = Options.float("max-drawdown-pct").pipe(
  Options.optional,
  Options.withDescription(
    "Skip symbols with max drawdown above this threshold",
  ),
);

export const saveWatchlistOption = Options.text("save-watchlist").pipe(
  Options.optional,
  Options.withDescription(
    "Write passing symbols to a JSON watchlist file in NEURATRADE_HOME/data",
  ),
);

export const intervalOption = Options.integer("interval").pipe(
  Options.withDefault(60),
  Options.withDescription("Seconds between paper-trading iterations"),
);

export const iterationsOption = Options.integer("iterations").pipe(
  Options.withDefault(1),
  Options.withDescription("Number of iterations to run (0 = infinite)"),
);

export const replayBarsOption = Options.integer("replay-bars").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Grid: replay the last N stored candles one per iteration (0 = live shadow)",
  ),
);

export const liveOption = Options.boolean("live").pipe(
  Options.withDefault(false),
  Options.withDescription(
    "Use live exchange adapter (Binance spot or Bitget futures)",
  ),
);

export const apiKeyOption = Options.text("api-key").pipe(
  Options.withDefault(""),
  Options.withDescription("Binance API key (or set BINANCE_API_KEY env)"),
);

export const apiSecretOption = Options.text("api-secret").pipe(
  Options.withDefault(""),
  Options.withDescription("Binance API secret (or set BINANCE_API_SECRET env)"),
);

export const marginModeOption = Options.text("margin-mode").pipe(
  Options.withDefault("crossed"),
  Options.withDescription("Futures margin mode: crossed or isolated"),
);

export const productTypeOption = Options.text("product-type").pipe(
  Options.withDefault("USDT-FUTURES"),
  Options.withDescription(
    "Futures product type: USDT-FUTURES, COIN-FUTURES or USDC-FUTURES",
  ),
);

export const maxDrawdownOption = Options.float("max-drawdown-pct").pipe(
  Options.optional,
  Options.withDescription(
    "Max drawdown % before blocking new trades (live default 5%)",
  ),
);

export const maxDailyLossOption = Options.float("max-daily-loss-pct").pipe(
  Options.optional,
  Options.withDescription(
    "Max daily loss % before blocking new trades (live default 2%)",
  ),
);

export const maxPositionSizeOption = Options.float(
  "max-position-size-pct",
).pipe(
  Options.optional,
  Options.withDescription(
    "Max position size % of capital per trade (live default 10%)",
  ),
);

export const maxTradesPerDayOption = Options.integer("max-trades-per-day").pipe(
  Options.optional,
  Options.withDescription("Max trades per day (live default 10)"),
);

export const minCapitalOption = Options.integer("min-capital").pipe(
  Options.optional,
  Options.withDescription(
    "Minimum capital required to trade (live default 100)",
  ),
);

export const watchlistOption = Options.text("watchlist").pipe(
  Options.optional,
  Options.withDescription(
    "Path to a JSON watchlist in NEURATRADE_HOME/data (uses per-symbol best params)",
  ),
);

export const noWatchlistOption = Options.boolean("no-watchlist").pipe(
  Options.withDescription(
    "Run paper-trade with the --symbol only, ignoring the DB watchlist fallback (validated single-candidate soaks)",
  ),
);

export const killSwitchOption = Options.boolean("kill-switch").pipe(
  Options.withDefault(false),
  Options.withDescription(
    "Engage kill switch before starting (blocks all new trades)",
  ),
);

export const disengageOption = Options.boolean("disengage").pipe(
  Options.withDefault(false),
  Options.withDescription("Disengage kill switch before starting"),
);

export const soakWatchlistOption = Options.text("watchlist").pipe(
  Options.withDescription("Path to a JSON watchlist in NEURATRADE_HOME/data"),
);

export const profileNameOption = Options.text("name").pipe(
  Options.withDescription(
    "Profile name (used as filename in ~/.neuratrade/profiles)",
  ),
);

export const trainWindowOption = Options.integer("train-window").pipe(
  Options.withDefault(180),
  Options.withDescription("Walk-forward training window in days"),
);

export const testWindowOption = Options.integer("test-window").pipe(
  Options.withDefault(60),
  Options.withDescription("Walk-forward testing window in days"),
);

export const wfMinTradesOption = Options.integer("min-trades").pipe(
  Options.withDefault(0),
  Options.withDescription("Minimum trades per window"),
);

export const minTradesPerMonthOption = Options.integer(
  "min-trades-per-month",
).pipe(
  Options.optional,
  Options.withDescription(
    "G1 override: minimum in-sample trades per month (default 20 for 5m, 10 otherwise)",
  ),
);

export const gridUniverseExchangeOption = Options.text("exchange").pipe(
  Options.withDefault("bitget-futures"),
  Options.withDescription("Exchange to scan for grid candidates"),
);

export const gridUniverseTimeframeOption = Options.text("timeframe").pipe(
  Options.withDefault("15m"),
  Options.withDescription("Candle timeframe for the grid universe scan"),
);

export const gridUniverseMinCandlesOption = Options.integer("min-candles").pipe(
  Options.withDefault(500),
  Options.withDescription(
    "Minimum candles a symbol must have to be included in the scan",
  ),
);

export const gridUniverseTrainWindowOption = Options.integer(
  "train-window",
).pipe(
  Options.withDefault(180),
  Options.withDescription("Walk-forward training window in candles"),
);

export const gridUniverseTestWindowOption = Options.integer("test-window").pipe(
  Options.withDefault(60),
  Options.withDescription("Walk-forward test window in candles"),
);

export const gridUniverseMinProfitableWindowsOption = Options.float(
  "min-profitable-windows-pct",
).pipe(
  Options.withDefault(60),
  Options.withDescription(
    "Minimum % of walk-forward windows a symbol must be profitable in",
  ),
);

export const gridUniverseMinAggregateReturnOption = Options.float(
  "min-aggregate-return-pct",
).pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Minimum aggregate walk-forward return % a symbol must exceed",
  ),
);

export const gridUniverseFeeOption = Options.float("fee").pipe(
  Options.withDefault(0.06),
  Options.withDescription("Per-side fee % applied in the grid backtests"),
);

export const gridUniverseSlippageOption = Options.float("slippage-bps").pipe(
  Options.withDefault(2),
  Options.withDescription("Slippage in basis points applied to grid fills"),
);

export const gridUniverseTrendFilterOption = Options.integer(
  "trend-filter-period",
).pipe(
  Options.withDefault(0),
  Options.withDescription("SMA trend filter period for the grid backtests"),
);

export const gridUniverseMarketOption = Options.boolean("market").pipe(
  Options.withDefault(false),
  Options.withDescription(
    "Discover symbols from the exchange contract list (24h-volume filtered) instead of the stored candle set",
  ),
);

export const gridUniverseOutputOption = Options.text("output").pipe(
  Options.optional,
  Options.withDescription(
    "Write surviving candidates to a JSON whitelist in NEURATRADE_HOME/data",
  ),
);

export const gridUniverseWatchOption = Options.boolean("watch").pipe(
  Options.withDefault(false),
  Options.withDescription(
    "Continuously re-scan the universe and upsert survivors into the DB watchlist",
  ),
);

export const gridUniverseIntervalOption = Options.integer("interval").pipe(
  Options.withDefault(3600),
  Options.withDescription(
    "Seconds between scans in --watch continuous mode (default 3600)",
  ),
);

export const gridUniverseMinFillFrequencyOption = Options.float(
  "min-fill-frequency-pct",
).pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Reject survivors whose grid step touches < this % of candles (0 = disabled)",
  ),
);

export const gridUniverseTargetFillsPerDayOption = Options.float(
  "target-fills-per-day",
).pipe(
  Options.optional,
  Options.withDescription(
    "Portfolio fills/day target for survivor selection (default: account-scaled 5-50)",
  ),
);

export const gridUniverseAccountCapitalOption = Options.float(
  "account-capital",
).pipe(
  Options.withDefault(1000),
  Options.withDescription(
    "Account capital USDT scaling the fills/day target when --target-fills-per-day is unset (default 1000)",
  ),
);

export const gridUniverseTierOption = Options.text("tier").pipe(
  Options.withDefault("readiness"),
  Options.withDescription(
    "Eligibility tier: 'readiness' (full stage-4 gate board) or 'fast' (light: strict time-split + walk-forward profitability + fills/day floor)",
  ),
);

export const gridUniverseDataSourceOption = Options.text("data-source").pipe(
  Options.withDefault("gateway"),
  Options.withDescription(
    "Candle source for the market scan: 'gateway' (default, live exchange candles — testnet-wired for bybit-futures) or 'db-mainnet' (5m mainnet candles from the DB resampled to the scan timeframe; no gateway fetches; fills modeled conservatively). Requires --market",
  ),
);

export const gridUniverseEngineOption = Options.text("engine").pipe(
  Options.withDefault("grid"),
  Options.withDescription(
    "Grid engine to evaluate: 'grid' (single-position, default) or 'ladder' (multi-rung, one TP per rung)",
  ),
);

export const gridUniverseRungsOption = Options.text("rungs").pipe(
  Options.optional,
  Options.withDescription(
    "Comma-separated rungs sweep for --engine ladder (default '1,2,3')",
  ),
);

export const watchlistListExchangeOption = Options.text("exchange").pipe(
  Options.withDefault("bitget-futures"),
  Options.withDescription("Exchange to list watchlist for"),
);

export const watchlistListTimeframeOption = Options.text("timeframe").pipe(
  Options.withDefault("15m"),
  Options.withDescription("Timeframe to list watchlist for"),
);

// ---------------------------------------------------------------------------
// Flow Ignition (flow-v1) options
// ---------------------------------------------------------------------------

export const flowSymbolsOption = Options.text("symbols").pipe(
  Options.withDefault("BTCUSDT"),
  Options.withDescription(
    "Comma-separated Bybit wire symbols to backtest, e.g. BTCUSDT,ETHUSDT",
  ),
);

export const flowStartOption = Options.text("start").pipe(
  Options.withDefault(""),
  Options.withDescription(
    "Inclusive start date (YYYY-MM-DD). Default: 180 days ago",
  ),
);

export const flowEndOption = Options.text("end").pipe(
  Options.withDefault(""),
  Options.withDescription("Inclusive end date (YYYY-MM-DD). Default: now"),
);

export const flowTimeframeOption = Options.choice("timeframe", [
  "5m",
  "1m",
] as const).pipe(
  Options.withDefault("5m"),
  Options.withDescription("Candle timeframe for the flow backtest"),
);

export const flowThresholdOption = Options.float("threshold").pipe(
  Options.withDefault(1.0),
  Options.withDescription("Entry score threshold in z-units (default 1.0)"),
);

export const flowHoldTimesOption = Options.text("hold-times").pipe(
  Options.withDefault("0.5,1,2,4,8"),
  Options.withDescription(
    "Comma-separated hold-time grid in hours; every entry is reported",
  ),
);

export const flowTradeExchangeOption = Options.text("exchange").pipe(
  Options.withDefault("bybit-futures"),
  Options.withDescription(
    "Exchange key for the flow live engine (data reads + adapter orders)",
  ),
);

export const flowTradeSymbolOption = Options.text("symbol").pipe(
  Options.withDefault("BTCUSDT"),
  Options.withDescription(
    "Bybit wire symbol to trade, e.g. BTCUSDT (default: the flow universe's #1 always-include base)",
  ),
);

export const flowHoldMinutesOption = Options.integer("hold-minutes").pipe(
  Options.withDefault(60),
  Options.withDescription(
    "Time exit for flow positions, in minutes (default 60)",
  ),
);

export const flowFeeOption = Options.float("fee").pipe(
  Options.withDefault(0.055),
  Options.withDescription("Taker fee percent per side (default 0.055)"),
);

export const flowSpreadBpsOption = Options.float("spread-bps").pipe(
  Options.withDefault(2),
  Options.withDescription(
    "Spread in basis points charged on BOTH entry and exit legs",
  ),
);

export const flowConservativeFillRateOption = Options.float(
  "conservative-fill-rate",
).pipe(
  Options.withDefault(0.75),
  Options.withDescription(
    "Deterministic fraction of flow signals filled after queue/partial-fill discount (default 0.75)",
  ),
);

export const flowMaxBreakevenWinRateOption = Options.float(
  "max-breakeven-win-rate",
).pipe(
  Options.withDefault(0.4),
  Options.withDescription(
    "Reject selected flow configs requiring a higher breakeven win rate (default 0.40)",
  ),
);

export const flowLimitOption = Options.integer("limit").pipe(
  Options.withDefault(40),
  Options.withDescription("Maximum ranked rows to print"),
);

export const flowMinTurnoverOption = Options.float("min-turnover").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Minimum 24h quote turnover (USDT) for a symbol to appear (0 = off)",
  ),
);

export const flowUniverseDataSourceOption = Options.choice("data-source", [
  "live-mainnet",
  "db-mainnet",
] as const).pipe(
  Options.withDefault("live-mainnet" as const),
  Options.withDescription(
    "Universe source: live Bybit mainnet tickers or cached mainnet SQLite candles",
  ),
);
