/**
 * scalp CLI options — exit
 *
 * Stop/target geometry, ATR and trailing rules, scale-out, time stops and volatility gates.
 */
import { Options } from "../kit/kit.ts";
import { minMaxStep } from "./defaults.js";

export const stopLossOption = Options.float("stop-loss").pipe(
  Options.withDefault(1.5),
  Options.withDescription("Stop loss percent"),
);

export const takeProfitOption = Options.float("take-profit").pipe(
  Options.withDefault(3.0),
  Options.withDescription("Take profit percent"),
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

export const maxHoldBarsOption = Options.integer("max-hold-bars").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Ladder: force-close a filled rung at the current close if it stays open for more than this many bars (0 = disabled, matches the backtest)",
  ),
);

export const stopRatioOption = Options.float("stop-ratio").pipe(
  Options.withDefault(0),
  Options.withDescription(
    "Ladder: stop distance as a multiple of the grid step for the per-rung stop (0 = legacy base-anchored ladder boundary). A tight step-relative stop (1.5-2x) with a multi-step target gives a positive R:R",
  ),
);

export const noAtrOption = Options.boolean("no-atr").pipe(
  Options.withDefault(false),
  Options.withDescription("Disable ATR stops and use fixed percent stops"),
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

export const holdUntilStopOption = Options.boolean("hold-until-stop").pipe(
  Options.withDefault(false),
  Options.withDescription(
    "Ignore opposite-signal exits and only exit on stop/take-profit",
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
