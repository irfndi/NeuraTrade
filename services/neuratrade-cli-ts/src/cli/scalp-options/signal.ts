/**
 * scalp CLI options — signal
 *
 * Entry signal, regime, session and strategy-profile flags, plus the ranges that sweep them.
 */
import { Options } from "../kit/kit.ts";
import { minMaxStep } from "./defaults.js";

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

export const configMismatchActionOption = Options.choice(
  "config-mismatch-action",
  ["hold", "force-reseed"],
).pipe(
  Options.withDefault("hold" as const),
  Options.withDescription(
    "Ladder: how to resolve a persisted state whose config no longer matches the current options while rungs are open. hold = freeze until manually reset (default); force-reseed = close open rungs at the current close and re-seed fresh so a whitelist edit self-heals",
  ),
);

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

export const noRsiOption = Options.boolean("no-rsi").pipe(
  Options.withDefault(false),
  Options.withDescription("Disable RSI mean-reversion component in backtest"),
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
  min: confMinOption,
  max: confMaxOption,
  step: confStepOption,
} = minMaxStep("conf", "float", "min-confidence", {
  min: 0.5,
  max: 0.7,
  step: 0.1,
});
