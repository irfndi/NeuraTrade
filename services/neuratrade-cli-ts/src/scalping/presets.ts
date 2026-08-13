import type { ResolvedBacktestArgs } from "./strategy-profile.js";

/**
 * Built-in scalper presets for the `scalp preset` command.
 *
 * Every preset defaults to observed-price mode (`observedPrice: true`) and uses
 * ATR-based stops so that stop/take-profit distances adapt to the market. The
 * presets trade off signal quality against trade frequency:
 *
 * - conservative: wider stops, higher confidence, fewer trades, smaller size
 * - balanced: middle ground, default choice for exploration
 * - aggressive: tighter stops, lower confidence, more trades, larger size
 */
export type PresetName = "conservative" | "balanced" | "aggressive";

const PRESET_DESCRIPTIONS = {
  conservative: {
    description:
      "Wide ATR stops, high confidence filters, small risk per trade. Prioritizes survival over frequency.",
    highlights: [
      "realistic costs (5 bps slippage, 0.1% fee)",
      "2.5x ATR stop / 3.5x ATR target",
      "min confidence 0.65, confluence 2",
      "1% risk per trade, 10% max position",
    ],
  },
  balanced: {
    description:
      "Moderate ATR distances and confidence thresholds. Good starting point for most symbols.",
    highlights: [
      "realistic costs (5 bps slippage, 0.1% fee)",
      "2.0x ATR stop / 3.0x ATR target",
      "min confidence 0.55, confluence 1",
      "1.5% risk per trade, 15% max position",
    ],
  },
  aggressive: {
    description:
      "Tight stops, lower confidence, larger size. Higher trade count but more sensitive to noise.",
    highlights: [
      "realistic costs (5 bps slippage, 0.1% fee)",
      "1.5x ATR stop / 2.5x ATR target",
      "min confidence 0.45, confluence 0",
      "2% risk per trade, 20% max position",
    ],
  },
} satisfies Record<PresetName, { description: string; highlights: string[] }>;

/**
 * Return the catalog of available presets with a short description and
 * human-readable highlights for each.
 */
export function listPresets(): {
  name: PresetName;
  description: string;
  highlights: string[];
}[] {
  return (Object.keys(PRESET_DESCRIPTIONS) as PresetName[]).map((name) => ({
    name,
    ...PRESET_DESCRIPTIONS[name],
  }));
}

/**
 * Apply a built-in scalper preset on top of a partial args object.
 *
 * The returned object is a full `ResolvedBacktestArgs`: preset values fill any
 * fields not provided by the caller, and caller-provided values always win.
 *
 * Important: every preset sets `realistic: true` by default (5 bps slippage +
 * 0.1% fee) and does not use close-only observed-price exits. Passing
 * `--realistic=false` or `--observed-price` on the CLI will override this.
 */
export function applyPreset(
  name: PresetName,
  base: Partial<ResolvedBacktestArgs> = {},
): ResolvedBacktestArgs {
  const preset = buildPresetArgs(name);
  return { ...preset, ...base } as ResolvedBacktestArgs;
}

function buildPresetArgs(name: PresetName): ResolvedBacktestArgs {
  const common: Omit<
    ResolvedBacktestArgs,
    | "atrStopMultiplier"
    | "atrTakeProfitMultiplier"
    | "minConfidence"
    | "minConfluence"
    | "riskPerTrade"
    | "maxPositionSize"
    | "entryOrderType"
    | "slippageBps"
    | "autoRegimeFilter"
    | "regimeMode"
    | "maxBarsInTrade"
    | "lossCooldownBars"
    | "breakevenAtR"
    | "observedPrice"
    | "realistic"
  > = {
    exchange: "binance",
    symbol: "BTC/USDT",
    timeframe: "1h",
    capital: 10000,
    positionSize: 100,
    stopLoss: 1.5,
    takeProfit: 3.0,
    fee: 0.1,
    makerFeePct: 0,
    entryLimitOffsetBps: 0,
    useAtrStops: true,
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
    futures: false,
    fundingRatePct: 0.01,
    trailingStopPct: 0,
    trailingStopAtrMultiplier: 0,
    minAtrPct: 0,
    volumeMinRatio: 0,
    volumeLookback: 20,
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
    sessionStart: "",
    sessionEnd: "",
    autoRegimeAdxThreshold: 25,
    trendSignalStyle: "slope",
    trendFastPeriod: 9,
    trendSlowPeriod: 21,
    directionalOnly: false,
    rsiFollowTrend: false,
    strictAgreement: false,
    entryOnClose: false,
    strictRealism: false,
    realisticSlippageBps: 5,
    breakoutLookback: 20,
    breakoutVolumeMinRatio: 1.2,
    breakoutAdxMin: 20,
    useFunding: false,
    fundingBiasThreshold: 0.0001,
    strategyType: "signal",
    gridStepPct: 0,
    gridMaxGrids: 0,
    gridPauseAfterLossBars: 0,
  };

  const specifics = {
    conservative: {
      atrStopMultiplier: 2.5,
      atrTakeProfitMultiplier: 3.5,
      minConfidence: 0.65,
      minConfluence: 2,
      riskPerTrade: 1,
      maxPositionSize: 10,
      entryOrderType: "market",
      slippageBps: 5,
      autoRegimeFilter: true,
      regimeMode: "trend",
      maxBarsInTrade: 48,
      lossCooldownBars: 8,
      breakevenAtR: 1.5,
      observedPrice: false,
      realistic: true,
    },
    balanced: {
      atrStopMultiplier: 2.0,
      atrTakeProfitMultiplier: 3.0,
      minConfidence: 0.55,
      minConfluence: 1,
      riskPerTrade: 1.5,
      maxPositionSize: 15,
      entryOrderType: "market",
      slippageBps: 5,
      autoRegimeFilter: true,
      regimeMode: "trend",
      maxBarsInTrade: 36,
      lossCooldownBars: 5,
      breakevenAtR: 1.0,
      observedPrice: false,
      realistic: true,
    },
    aggressive: {
      atrStopMultiplier: 1.5,
      atrTakeProfitMultiplier: 2.5,
      minConfidence: 0.45,
      minConfluence: 0,
      riskPerTrade: 2,
      maxPositionSize: 20,
      entryOrderType: "market",
      slippageBps: 5,
      autoRegimeFilter: false,
      regimeMode: "reversion",
      maxBarsInTrade: 24,
      lossCooldownBars: 3,
      breakevenAtR: 0.5,
      observedPrice: false,
      realistic: true,
    },
  } satisfies Record<PresetName, Partial<ResolvedBacktestArgs>>;

  return { ...common, ...specifics[name] } as ResolvedBacktestArgs;
}
