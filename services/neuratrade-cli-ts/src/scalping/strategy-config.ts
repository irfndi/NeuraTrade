import * as S from "effect/Schema";
import { Effect } from "effect";
import * as fs from "fs";
import * as path from "path";
import type {
  ComposerConfig,
  ComposerEnabled,
  ComposerThresholds,
  ComposerWeights,
} from "./types.js";

/**
 * A single indicator switch + weight + parameter map.
 * The weight is used when the indicator is enabled; disabled indicators are
 * ignored entirely. Params are indicator-specific (e.g. period, threshold).
 */
export const IndicatorConfigSchema = S.Struct({
  enabled: S.optional(S.Boolean).pipe(S.withDecodingDefault(() => true)),
  weight: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  params: S.optional(S.Record({ key: S.String, value: S.Number })).pipe(
    S.withDecodingDefault(() => ({})),
  ),
});

export type IndicatorConfig = typeof IndicatorConfigSchema.Type;

export const IndicatorsConfigSchema = S.Struct({
  leading: S.optional(
    S.Record({ key: S.String, value: IndicatorConfigSchema }),
  ).pipe(
    S.withDecodingDefault(() => ({
      rsiPullback: { enabled: false, weight: 0, params: { period: 14 } },
      emaPullback: { enabled: false, weight: 0, params: { period: 9 } },
    })),
  ),
  current: S.optional(
    S.Record({ key: S.String, value: IndicatorConfigSchema }),
  ).pipe(
    S.withDecodingDefault(() => ({
      trend: {
        enabled: true,
        weight: 0.18,
        params: { fastPeriod: 9, slowPeriod: 21, style: 0 },
      },
      imbalance: { enabled: true, weight: 0.22, params: {} },
      spread: { enabled: true, weight: 0.18, params: {} },
      liquidity: { enabled: true, weight: 0.09, params: {} },
    })),
  ),
  lagging: S.optional(
    S.Record({ key: S.String, value: IndicatorConfigSchema }),
  ).pipe(
    S.withDecodingDefault(() => ({
      rsi: { enabled: true, weight: 0.09, params: { period: 14 } },
      connorsRsi2: { enabled: false, weight: 0, params: {} },
      volatility: { enabled: true, weight: 0.13, params: {} },
      regime: { enabled: true, weight: 0.11, params: {} },
      funding: { enabled: false, weight: 0, params: {} },
    })),
  ),
});

export type IndicatorsConfig = typeof IndicatorsConfigSchema.Type;

export const SignalRulesConfigSchema = S.Struct({
  minConfluence: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  minCategoryConfluence: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 0),
  ),
  minConfidence: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0.5)),
  regimeMode: S.optional(S.Literal("trend", "reversion", "breakout")).pipe(
    S.withDecodingDefault(() => "trend" as const),
  ),
  breakoutLookback: S.optional(S.Number).pipe(S.withDecodingDefault(() => 20)),
  breakoutVolumeMinRatio: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 1.2),
  ),
  breakoutAdxMin: S.optional(S.Number).pipe(S.withDecodingDefault(() => 20)),
  directionalOnly: S.optional(S.Boolean).pipe(
    S.withDecodingDefault(() => false),
  ),
  strictAgreement: S.optional(S.Boolean).pipe(
    S.withDecodingDefault(() => false),
  ),
  rsiFollowTrend: S.optional(S.Boolean).pipe(
    S.withDecodingDefault(() => false),
  ),
  volumeMinRatio: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  volumeLookback: S.optional(S.Number).pipe(S.withDecodingDefault(() => 20)),
  maxSpreadPct: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  minLiquidity: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  adxMin: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  atrMinPctOfPrice: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  atrMaxPctOfPrice: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  minEfficiencyRatio: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  bollingerEntryMinPct: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 0),
  ),
  bollingerEntryMaxPct: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 0),
  ),
  useAdaptiveMarketFilters: S.optional(S.Boolean).pipe(
    S.withDecodingDefault(() => false),
  ),
  adaptiveLookback: S.optional(S.Number).pipe(S.withDecodingDefault(() => 100)),
  trendFilterFastPeriod: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 50),
  ),
  trendFilterSlowPeriod: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 100),
  ),
  trendFilterPeriod: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 200),
  ),
  entryRsiLongThreshold: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 10),
  ),
  entryRsiShortThreshold: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 90),
  ),
  exitRsiLongThreshold: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 60),
  ),
  exitRsiShortThreshold: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 40),
  ),
  entryCandleConfirm: S.optional(S.Boolean).pipe(
    S.withDecodingDefault(() => false),
  ),
  momentumConfirmBars: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 0),
  ),
  signalPersistence: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  autoRegimeFilter: S.optional(S.Boolean).pipe(
    S.withDecodingDefault(() => false),
  ),
  autoRegimeAdxThreshold: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 25),
  ),
  sessionStart: S.optional(S.String).pipe(S.withDecodingDefault(() => "")),
  sessionEnd: S.optional(S.String).pipe(S.withDecodingDefault(() => "")),
  trendSignalStyle: S.optional(S.Literal("slope", "cross")).pipe(
    S.withDecodingDefault(() => "slope" as const),
  ),
  fundingBiasThreshold: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 0.0001),
  ),
  useFunding: S.optional(S.Boolean).pipe(S.withDecodingDefault(() => false)),
});

export type SignalRulesConfig = typeof SignalRulesConfigSchema.Type;

export const ExecutionConfigSchema = S.Struct({
  stopLossPct: S.optional(S.Number).pipe(S.withDecodingDefault(() => 1.0)),
  takeProfitPct: S.optional(S.Number).pipe(S.withDecodingDefault(() => 1.2)),
  useAtrStops: S.optional(S.Boolean).pipe(S.withDecodingDefault(() => false)),
  atrStopMultiplier: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 1.5),
  ),
  atrTakeProfitMultiplier: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 2.5),
  ),
  atrRiskReward: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  useAdaptiveStops: S.optional(S.Boolean).pipe(
    S.withDecodingDefault(() => false),
  ),
  adaptiveStopAtrMultiplier: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 1.5),
  ),
  adaptiveRiskReward: S.optional(S.Number).pipe(S.withDecodingDefault(() => 2)),
  scaleOutAtR: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  scaleOutPct: S.optional(S.Number).pipe(S.withDecodingDefault(() => 50)),
  riskPerTradePct: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  positionSizePct: S.optional(S.Number).pipe(S.withDecodingDefault(() => 100)),
  maxPositionSizePct: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 100),
  ),
  volatilityTargetAnnualPct: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 0),
  ),
  leverage: S.optional(S.Number).pipe(S.withDecodingDefault(() => 1)),
  feePct: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0.1)),
  makerFeePct: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  entryOrderType: S.optional(S.Literal("market", "limit")).pipe(
    S.withDecodingDefault(() => "market" as const),
  ),
  entryLimitOffsetBps: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 0),
  ),
  slippageBps: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  maxBarsInTrade: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  lossCooldownBars: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  lossConfidencePenalty: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 0),
  ),
  lossConfidenceDecay: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 0),
  ),
  breakevenAtR: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  trailingStopPct: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  trailingStopAtrMultiplier: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 0),
  ),
  holdUntilStop: S.optional(S.Boolean).pipe(S.withDecodingDefault(() => false)),
  entryOnClose: S.optional(S.Boolean).pipe(S.withDecodingDefault(() => false)),
  observedPrice: S.optional(S.Boolean).pipe(S.withDecodingDefault(() => false)),
  strictRealism: S.optional(S.Boolean).pipe(S.withDecodingDefault(() => false)),
  minAtrPct: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  exitRsiPeriod: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  exitRsiLongLevel: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  exitRsiShortLevel: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  volatilityLookback: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  volatilityLowPct: S.optional(S.Number).pipe(S.withDecodingDefault(() => 20)),
  volatilityHighPct: S.optional(S.Number).pipe(S.withDecodingDefault(() => 80)),
  volatilityLowFactor: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 0.8),
  ),
  volatilityHighFactor: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 1.2),
  ),
});

export type ExecutionConfig = typeof ExecutionConfigSchema.Type;

export const PortfolioConfigSchema = S.Struct({
  maxOpenPositions: S.optional(S.Number).pipe(S.withDecodingDefault(() => 1)),
  maxPortfolioHeatPct: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 100),
  ),
  correlationFilter: S.optional(S.Boolean).pipe(
    S.withDecodingDefault(() => false),
  ),
  correlationLookback: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 50),
  ),
  correlationThreshold: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 0.8),
  ),
  allowlist: S.optional(S.Array(S.String)).pipe(
    S.withDecodingDefault(() => []),
  ),
  blocklist: S.optional(S.Array(S.String)).pipe(
    S.withDecodingDefault(() => []),
  ),
});

export type PortfolioConfig = typeof PortfolioConfigSchema.Type;

export const StrategyConfigSchema = S.Struct({
  name: S.optional(S.String),
  version: S.optional(S.Literal("1")).pipe(S.withDecodingDefault(() => "1")),
  indicators: S.optional(IndicatorsConfigSchema).pipe(
    S.withDecodingDefault(defaultIndicatorsConfig),
  ),
  signalRules: S.optional(SignalRulesConfigSchema).pipe(
    S.withDecodingDefault(defaultSignalRulesConfig),
  ),
  execution: S.optional(ExecutionConfigSchema).pipe(
    S.withDecodingDefault(defaultExecutionConfig),
  ),
  portfolio: S.optional(PortfolioConfigSchema).pipe(
    S.withDecodingDefault(defaultPortfolioConfig),
  ),
});

export type StrategyConfig = typeof StrategyConfigSchema.Type;

export const decodeStrategyConfig = S.decodeUnknown(StrategyConfigSchema);

function defaultIndicatorsConfig(): IndicatorsConfig {
  return {
    leading: {
      rsiPullback: { enabled: false, weight: 0, params: { period: 14 } },
      emaPullback: { enabled: false, weight: 0, params: { period: 9 } },
    },
    current: {
      trend: {
        enabled: true,
        weight: 0.18,
        params: { fastPeriod: 9, slowPeriod: 21, style: 0 },
      },
      imbalance: { enabled: true, weight: 0.22, params: {} },
      spread: { enabled: true, weight: 0.18, params: {} },
      liquidity: { enabled: true, weight: 0.09, params: {} },
    },
    lagging: {
      rsi: { enabled: true, weight: 0.09, params: { period: 14 } },
      connorsRsi2: { enabled: false, weight: 0, params: {} },
      volatility: { enabled: true, weight: 0.13, params: {} },
      regime: { enabled: true, weight: 0.11, params: {} },
      funding: { enabled: false, weight: 0, params: {} },
    },
  };
}

function defaultSignalRulesConfig(): SignalRulesConfig {
  return {
    minConfluence: 0,
    minCategoryConfluence: 0,
    minConfidence: 0.5,
    regimeMode: "trend",
    breakoutLookback: 20,
    breakoutVolumeMinRatio: 1.2,
    breakoutAdxMin: 20,
    directionalOnly: false,
    strictAgreement: false,
    rsiFollowTrend: false,
    volumeMinRatio: 0,
    volumeLookback: 20,
    maxSpreadPct: 0,
    minLiquidity: 0,
    adxMin: 0,
    atrMinPctOfPrice: 0,
    atrMaxPctOfPrice: 0,
    minEfficiencyRatio: 0,
    bollingerEntryMinPct: 0,
    bollingerEntryMaxPct: 0,
    useAdaptiveMarketFilters: false,
    adaptiveLookback: 100,
    trendFilterFastPeriod: 50,
    trendFilterSlowPeriod: 100,
    trendFilterPeriod: 200,
    entryRsiLongThreshold: 10,
    entryRsiShortThreshold: 90,
    exitRsiLongThreshold: 60,
    exitRsiShortThreshold: 40,
    entryCandleConfirm: false,
    momentumConfirmBars: 0,
    signalPersistence: 0,
    autoRegimeFilter: false,
    autoRegimeAdxThreshold: 25,
    sessionStart: "",
    sessionEnd: "",
    trendSignalStyle: "slope",
    fundingBiasThreshold: 0.0001,
    useFunding: false,
  };
}

function defaultExecutionConfig(): ExecutionConfig {
  return {
    stopLossPct: 1.0,
    takeProfitPct: 1.2,
    useAtrStops: false,
    atrStopMultiplier: 1.5,
    atrTakeProfitMultiplier: 2.5,
    atrRiskReward: 0,
    useAdaptiveStops: false,
    adaptiveStopAtrMultiplier: 1.5,
    adaptiveRiskReward: 2,
    scaleOutAtR: 0,
    scaleOutPct: 50,
    riskPerTradePct: 0,
    positionSizePct: 100,
    maxPositionSizePct: 100,
    volatilityTargetAnnualPct: 0,
    leverage: 1,
    feePct: 0.1,
    makerFeePct: 0,
    entryOrderType: "market",
    entryLimitOffsetBps: 0,
    slippageBps: 0,
    maxBarsInTrade: 0,
    lossCooldownBars: 0,
    lossConfidencePenalty: 0,
    lossConfidenceDecay: 0,
    breakevenAtR: 0,
    trailingStopPct: 0,
    trailingStopAtrMultiplier: 0,
    holdUntilStop: false,
    entryOnClose: false,
    observedPrice: false,
    strictRealism: false,
    minAtrPct: 0,
    exitRsiPeriod: 0,
    exitRsiLongLevel: 0,
    exitRsiShortLevel: 0,
    volatilityLookback: 0,
    volatilityLowPct: 20,
    volatilityHighPct: 80,
    volatilityLowFactor: 0.8,
    volatilityHighFactor: 1.2,
  };
}

function defaultPortfolioConfig(): PortfolioConfig {
  return {
    maxOpenPositions: 1,
    maxPortfolioHeatPct: 100,
    correlationFilter: false,
    correlationLookback: 50,
    correlationThreshold: 0.8,
    allowlist: [],
    blocklist: [],
  };
}

export function defaultStrategyConfig(): StrategyConfig {
  return {
    version: "1",
    indicators: defaultIndicatorsConfig(),
    signalRules: defaultSignalRulesConfig(),
    execution: defaultExecutionConfig(),
    portfolio: defaultPortfolioConfig(),
  };
}

function configDir(homeDir: string): string {
  return path.join(homeDir, "strategy-configs");
}

function defaultConfigPath(homeDir: string): string {
  return path.join(homeDir, "strategy-config.json");
}

function namedConfigPath(homeDir: string, name: string): string {
  const safeName = path.basename(name).replace(/[^a-zA-Z0-9_-]/g, "_");
  return path.join(configDir(homeDir), `${safeName}.json`);
}

function configPath(homeDir: string, name?: string): string {
  return name ? namedConfigPath(homeDir, name) : defaultConfigPath(homeDir);
}

/**
 * Load a strategy config from `~/.neuratrade/strategy-config.json`
 * (or `~/.neuratrade/strategy-configs/<name>.json` if a name is given).
 */
export function loadStrategyConfig(
  homeDir: string,
  name?: string,
): Effect.Effect<StrategyConfig, Error> {
  return Effect.gen(function* () {
    const filePath = configPath(homeDir, name);
    const text = yield* Effect.tryPromise({
      try: async () => {
        const file = Bun.file(filePath);
        return await file.text();
      },
      catch: (err) =>
        new Error(
          `Failed to read strategy config ${filePath}: ${err instanceof Error ? err.message : String(err)}`,
        ),
    });
    const json = yield* Effect.try({
      try: () => JSON.parse(text) as unknown,
      catch: (err) =>
        new Error(
          `Invalid JSON in strategy config ${filePath}: ${err instanceof Error ? err.message : String(err)}`,
        ),
    });
    return yield* decodeStrategyConfig(json);
  });
}

/**
 * Save a strategy config to `~/.neuratrade/strategy-config.json`
 * (or `~/.neuratrade/strategy-configs/<name>.json` if a name is given).
 */
export function saveStrategyConfig(
  homeDir: string,
  config: StrategyConfig,
  name?: string,
): Effect.Effect<void, Error> {
  return Effect.gen(function* () {
    const filePath = configPath(homeDir, name);
    const dir = path.dirname(filePath);
    yield* Effect.sync(() => fs.mkdirSync(dir, { recursive: true }));
    const payload = JSON.stringify(config, null, 2);
    yield* Effect.tryPromise({
      try: () => Bun.write(filePath, payload),
      catch: (err) =>
        new Error(
          `Failed to write strategy config ${filePath}: ${err instanceof Error ? err.message : String(err)}`,
        ),
    });
  });
}

const composerIndicatorNames: Array<keyof ComposerWeights> = [
  "spread",
  "imbalance",
  "volatility",
  "trend",
  "liquidity",
  "rsi",
  "connorsRsi2",
  "rsiPullback",
  "emaPullback",
  "regime",
  "funding",
];

function isComposerIndicatorName(name: string): name is keyof ComposerWeights {
  return (composerIndicatorNames as string[]).includes(name);
}

/**
 * Build the composer's `ComposerConfig` from a unified `StrategyConfig`.
 * Unknown indicator names are ignored, so the indicator palette can grow
 * without breaking this mapping.
 */
export function strategyConfigToComposerConfig(
  config: StrategyConfig,
): ComposerConfig {
  const weights: Record<string, number> = {};
  const enabled: Record<string, boolean> = {};

  for (const name of composerIndicatorNames) {
    weights[name] = 0;
    enabled[name] = false;
  }

  const categories: Array<keyof IndicatorsConfig> = [
    "leading",
    "current",
    "lagging",
  ];
  for (const category of categories) {
    const group = config.indicators[category] ?? {};
    for (const [name, indicator] of Object.entries(group)) {
      if (isComposerIndicatorName(name)) {
        weights[name] = indicator.enabled ? indicator.weight : 0;
        enabled[name] = indicator.enabled;
      }
    }
  }

  const s = config.signalRules;
  const thresholds: ComposerThresholds = {
    spreadTightPct: 0.0005,
    spreadModeratePct: 0.001,
    spreadWidePct: 0.002,
    imbalanceWeak: 0.05,
    imbalanceStrong: 0.2,
    volatilityLowPct: 0.005,
    volatilityModeratePct: 0.02,
    volatilityHighPct: 0.05,
    trendWeakPct: 0.001,
    trendStrongPct: 0.005,
    liquidityMedium: 40,
    liquidityStrong: 70,
    rsiOversoldStrong: 30,
    rsiOversoldMedium: 40,
    rsiOverboughtMedium: 60,
    rsiOverboughtStrong: 70,
    adxStrongTrend: 30,
    adxWeakTrend: 20,
    atrMaxPctOfPrice: s.atrMaxPctOfPrice,
    bollingerEntryMaxPct: s.bollingerEntryMaxPct,
    bollingerEntryMinPct: s.bollingerEntryMinPct,
    minConfidenceSpread: 0.1,
    regimeMode: s.regimeMode,
    breakoutLookback: s.breakoutLookback,
    breakoutVolumeMinRatio: s.breakoutVolumeMinRatio,
    breakoutAdxMin: s.breakoutAdxMin,
    trendFilterFastPeriod: s.trendFilterFastPeriod,
    trendFilterSlowPeriod: s.trendFilterSlowPeriod,
    trendFilterPeriod: s.trendFilterPeriod,
    entryRsiLongThreshold: s.entryRsiLongThreshold,
    entryRsiShortThreshold: s.entryRsiShortThreshold,
    exitRsiLongThreshold: s.exitRsiLongThreshold,
    exitRsiShortThreshold: s.exitRsiShortThreshold,
    volumeMinRatio: s.volumeMinRatio,
    volumeLookback: s.volumeLookback,
    minConfluence: s.minConfluence,
    minCategoryConfluence: s.minCategoryConfluence,
    entryCandleConfirm: s.entryCandleConfirm,
    momentumConfirmBars: s.momentumConfirmBars,
    directionalOnly: s.directionalOnly,
    rsiFollowTrend: s.rsiFollowTrend,
    strictAgreement: s.strictAgreement,
    maxSpreadPct: s.maxSpreadPct,
    minLiquidity: s.minLiquidity,
    sessionStart: s.sessionStart,
    sessionEnd: s.sessionEnd,
    atrMinPctOfPrice: s.atrMinPctOfPrice,
    minEfficiencyRatio: s.minEfficiencyRatio,
    useAdaptiveMarketFilters: s.useAdaptiveMarketFilters,
    adaptiveLookback: s.adaptiveLookback,
    trendSignalStyle: s.trendSignalStyle,
    trendFastPeriod: config.indicators.current?.trend?.params.fastPeriod ?? 9,
    trendSlowPeriod: config.indicators.current?.trend?.params.slowPeriod ?? 21,
    adxMin: s.adxMin,
    fundingBiasThreshold: s.fundingBiasThreshold,
    useFunding: s.useFunding,
  };

  return {
    weights: weights as unknown as ComposerWeights,
    thresholds,
    enabled: enabled as ComposerEnabled,
  };
}
