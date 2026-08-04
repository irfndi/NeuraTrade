import { defaultComposerConfig } from "./composer.js";
import type { ComposerConfig, ComposerWeights } from "./types.js";
import type { ResolvedBacktestArgs } from "./strategy-profile.js";

export type StrategyTemplateName =
  | "meanReversion"
  | "trendFollowing"
  | "breakout"
  | "emaPullback"
  | "momentum"
  | "rangeExpansion"
  | "fundingCarry"
  | "dualEmaCross"
  | "ensemble"
  | "microScalp"
  | "connorsRsi2"
  | "gridScalp";

export interface StrategyTemplate {
  readonly name: StrategyTemplateName;
  readonly description: string;
  readonly composerConfigOverrides: Partial<ComposerConfig>;
  readonly executionOverrides: Partial<ResolvedBacktestArgs>;
}

function normalizeWeights(weights: Partial<ComposerWeights>): ComposerWeights {
  const base = { ...defaultComposerConfig.weights, ...weights };
  const sum = Object.values(base).reduce((a, b) => a + b, 0);
  if (sum <= 0) return base;
  const normalized = { ...base };
  for (const key of Object.keys(normalized) as Array<keyof ComposerWeights>) {
    normalized[key] /= sum;
  }
  return normalized;
}

export function listStrategies(): StrategyTemplate[] {
  return [
    {
      name: "meanReversion",
      description:
        "Fade extremes in a mean-reverting regime using RSI and Bollinger signals.",
      composerConfigOverrides: {
        weights: normalizeWeights({
          rsi: 0.4,
          regime: 0.35,
          trend: 0.1,
          volatility: 0.1,
          emaPullback: 0.05,
        }),
        thresholds: {
          ...defaultComposerConfig.thresholds,
          regimeMode: "reversion",
        },
      },
      executionOverrides: {
        useAtrStops: true,
        atrStopMultiplier: 2.0,
        atrTakeProfitMultiplier: 2.5,
        regimeMode: "reversion",
      },
    },
    {
      name: "trendFollowing",
      description:
        "Follow the prevailing trend with ADX confirmation and a trailing ATR stop.",
      composerConfigOverrides: {
        weights: normalizeWeights({
          trend: 0.5,
          volatility: 0.15,
          regime: 0.15,
          rsi: 0.1,
          emaPullback: 0.1,
        }),
        thresholds: {
          ...defaultComposerConfig.thresholds,
          regimeMode: "trend",
          adxMin: 20,
        },
      },
      executionOverrides: {
        useAtrStops: true,
        atrStopMultiplier: 1.5,
        atrTakeProfitMultiplier: 3.0,
        trailingStopAtrMultiplier: 1.5,
        adxMin: 20,
        regimeMode: "trend",
      },
    },
    {
      name: "breakout",
      description:
        "Enter on volume-confirmed price breakouts above/below recent range.",
      composerConfigOverrides: {
        weights: normalizeWeights({
          regime: 0.4,
          trend: 0.2,
          volatility: 0.2,
          rsi: 0.1,
          emaPullback: 0.1,
        }),
        thresholds: {
          ...defaultComposerConfig.thresholds,
          regimeMode: "breakout",
          breakoutLookback: 20,
          breakoutVolumeMinRatio: 1.2,
          volumeMinRatio: 1.2,
        },
      },
      executionOverrides: {
        useAtrStops: true,
        atrStopMultiplier: 1.5,
        atrTakeProfitMultiplier: 2.0,
        regimeMode: "breakout",
        volumeMinRatio: 1.2,
        breakoutLookback: 20,
        breakoutVolumeMinRatio: 1.2,
      },
    },
    {
      name: "emaPullback",
      description:
        "Buy pullbacks to the fast EMA in an uptrend and sell rallies in a downtrend.",
      composerConfigOverrides: {
        weights: normalizeWeights({
          emaPullback: 0.35,
          trend: 0.25,
          volatility: 0.2,
          regime: 0.2,
        }),
        thresholds: {
          ...defaultComposerConfig.thresholds,
          trendFastPeriod: 9,
          trendSlowPeriod: 21,
        },
      },
      executionOverrides: {
        useAtrStops: true,
        atrStopMultiplier: 1.5,
        atrTakeProfitMultiplier: 2.5,
        entryPullbackEmaPeriod: 21,
        trendFastPeriod: 9,
        trendSlowPeriod: 21,
      },
    },
    {
      name: "momentum",
      description:
        "Trade in the direction of trend strength using RSI trend confirmation.",
      composerConfigOverrides: {
        weights: normalizeWeights({
          trend: 0.45,
          rsi: 0.25,
          volatility: 0.15,
          regime: 0.15,
        }),
        thresholds: {
          ...defaultComposerConfig.thresholds,
          regimeMode: "trend",
          rsiFollowTrend: true,
        },
      },
      executionOverrides: {
        useAtrStops: true,
        atrStopMultiplier: 1.0,
        atrTakeProfitMultiplier: 2.0,
        rsiFollowTrend: true,
        regimeMode: "trend",
      },
    },
    {
      name: "rangeExpansion",
      description:
        "Capture moves when volatility expands and Bollinger bands widen.",
      composerConfigOverrides: {
        weights: normalizeWeights({
          volatility: 0.5,
          trend: 0.2,
          regime: 0.2,
          rsi: 0.1,
        }),
        thresholds: {
          ...defaultComposerConfig.thresholds,
          bollingerEntryMinPct: -0.5,
          bollingerEntryMaxPct: 1.5,
        },
      },
      executionOverrides: {
        useAtrStops: true,
        atrStopMultiplier: 1.5,
        atrTakeProfitMultiplier: 2.0,
      },
    },
    {
      name: "fundingCarry",
      description:
        "Fade extreme perpetual-futures funding rates (contrarian funding bias).",
      composerConfigOverrides: {
        weights: normalizeWeights({
          funding: 0.35,
          trend: 0.3,
          volatility: 0.15,
          regime: 0.15,
          rsi: 0.05,
        }),
        thresholds: {
          ...defaultComposerConfig.thresholds,
          useFunding: true,
          fundingBiasThreshold: 0.0001,
        },
      },
      executionOverrides: {
        useFunding: true,
        fundingBiasThreshold: 0.0001,
        regimeMode: "reversion",
      },
    },
    {
      name: "dualEmaCross",
      description:
        "Classic dual EMA crossover (fast/slow) with a trailing ATR stop and no fixed take-profit.",
      composerConfigOverrides: {
        weights: normalizeWeights({
          trend: 0.7,
          volatility: 0.15,
          regime: 0.1,
          rsi: 0.05,
        }),
        thresholds: {
          ...defaultComposerConfig.thresholds,
          regimeMode: "trend",
          trendSignalStyle: "cross",
          trendFastPeriod: 50,
          trendSlowPeriod: 200,
        },
      },
      executionOverrides: {
        useAtrStops: true,
        atrStopMultiplier: 2.0,
        atrTakeProfitMultiplier: 0,
        atrRiskReward: 0,
        trailingStopAtrMultiplier: 2.0,
        trendSignalStyle: "cross",
        trendFastPeriod: 50,
        trendSlowPeriod: 200,
        regimeMode: "trend",
      },
    },
    {
      name: "ensemble",
      description:
        "Multi-strategy ensemble: trend, mean-reversion, funding and momentum components must concur (2+ agreements) to enter.",
      composerConfigOverrides: {
        weights: normalizeWeights({
          trend: 0.25,
          regime: 0.2,
          rsi: 0.15,
          funding: 0.15,
          volatility: 0.15,
          emaPullback: 0.1,
        }),
        thresholds: {
          ...defaultComposerConfig.thresholds,
          regimeMode: "trend",
          minConfluence: 2,
          minCategoryConfluence: 2,
          useFunding: true,
          fundingBiasThreshold: 0.0001,
          volumeMinRatio: 1.0,
        },
      },
      executionOverrides: {
        useAtrStops: true,
        atrStopMultiplier: 1.5,
        atrTakeProfitMultiplier: 2.5,
        useFunding: true,
        fundingBiasThreshold: 0.0001,
        volumeMinRatio: 1.0,
        regimeMode: "trend",
      },
    },
    {
      name: "microScalp",
      description:
        "Manual-style scalping: RSI(2) extreme entries with a trend filter and fixed TP/SL brackets (0.3% SL / 0.8% TP).",
      composerConfigOverrides: {
        weights: normalizeWeights({
          rsi: 0.45,
          regime: 0.25,
          trend: 0.2,
          volatility: 0.1,
        }),
        thresholds: {
          ...defaultComposerConfig.thresholds,
          rsiPeriod: 2,
          rsiOversoldStrong: 5,
          rsiOverboughtStrong: 95,
          regimeMode: "reversion",
          volumeMinRatio: 1.0,
        },
      },
      executionOverrides: {
        useAtrStops: false,
        stopLoss: 0.3,
        takeProfit: 0.8,
        maxBarsInTrade: 0,
        regimeMode: "reversion",
      },
    },
    {
      name: "connorsRsi2",
      description:
        "Larry Connors RSI(2) mean reversion with a 200-period trend filter and RSI-based exits. Long when price is above trend and RSI(2) < 10; short when price is below trend and RSI(2) > 90.",
      composerConfigOverrides: {
        weights: normalizeWeights({
          connorsRsi2: 0.7,
          trend: 0.2,
          volatility: 0.1,
        }),
        thresholds: {
          ...defaultComposerConfig.thresholds,
          rsiPeriod: 2,
          trendFilterPeriod: 200,
          entryRsiLongThreshold: 10,
          entryRsiShortThreshold: 90,
          exitRsiLongThreshold: 60,
          exitRsiShortThreshold: 40,
          regimeMode: "reversion",
        },
      },
      executionOverrides: {
        useAtrStops: false,
        exitRsiPeriod: 2,
        exitRsiLongLevel: 60,
        exitRsiShortLevel: 40,
        trendFilterPeriod: 200,
        regimeMode: "reversion",
      },
    },
    {
      name: "gridScalp",
      description:
        "Trend-biased grid scalping: enter grids only in the direction of the SMA trend, use a 2% step and a 1:1 target/stop grid. Criterion-realistic costs: 0.1% per side + 5 bps slippage.",
      composerConfigOverrides: {}, // grid does not use composer signals
      executionOverrides: {
        strategyType: "grid",
        gridStepPct: 2.0,
        gridMaxGrids: 1.5,
        gridPauseAfterLossBars: 0,
        fee: 0.2, // 0.1% per side
        slippageBps: 5,
        onlyWithTrend: true,
        targetRatio: 1.0,
      },
    },
  ];
}

function findTemplate(name: StrategyTemplateName): StrategyTemplate {
  const template = listStrategies().find((s) => s.name === name);
  if (!template) {
    throw new Error(`Unknown strategy template: ${name}`);
  }
  return template;
}

export function buildBacktestArgsFromTemplate(
  templateName: StrategyTemplateName,
  baseArgs: ResolvedBacktestArgs,
): ResolvedBacktestArgs {
  const template = findTemplate(templateName);
  const overrides = template.executionOverrides;
  const merged = { ...baseArgs } as Record<string, unknown>;

  for (const key of Object.keys(overrides) as Array<
    keyof ResolvedBacktestArgs
  >) {
    const value = overrides[key];
    if (value === undefined) continue;
    if (typeof value === "number" && value === 0) continue;
    if (typeof value === "string" && value === "") continue;
    merged[key] = value;
  }

  return merged as unknown as ResolvedBacktestArgs;
}

export function buildComposerConfigFromTemplate(
  templateName: StrategyTemplateName,
  baseConfig: ComposerConfig = defaultComposerConfig,
): ComposerConfig {
  const template = findTemplate(templateName);
  const overrides = template.composerConfigOverrides;

  const mergedWeights: ComposerWeights = {
    ...baseConfig.weights,
    ...overrides.weights,
  };
  const mergedThresholds = {
    ...baseConfig.thresholds,
    ...overrides.thresholds,
  };
  const mergedEnabled = { ...baseConfig.enabled, ...overrides.enabled };

  const sum = Object.values(mergedWeights).reduce((a, b) => a + b, 0);
  let weights = mergedWeights;
  if (sum > 0 && Math.abs(sum - 1.0) > 0.0001) {
    const normalized = { ...mergedWeights };
    for (const key of Object.keys(normalized) as Array<keyof ComposerWeights>) {
      normalized[key] /= sum;
    }
    weights = normalized;
  }

  return {
    weights,
    thresholds: mergedThresholds,
    enabled: mergedEnabled,
  };
}
