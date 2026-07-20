import * as S from "effect/Schema";
import { Effect, FileSystem } from "effect";
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
  enabled: S.Boolean.pipe(S.withDecodingDefault(Effect.succeed(true))),
  weight: S.Number.pipe(S.withDecodingDefault(Effect.succeed(0))),
  params: S.Record(S.String, S.Number).pipe(
    S.withDecodingDefault(Effect.succeed({})),
  ),
});

export type IndicatorConfig = typeof IndicatorConfigSchema.Type;

export const IndicatorsConfigSchema = S.Struct({
  leading: S.Record(S.String, IndicatorConfigSchema).pipe(
    S.withDecodingDefault(
      Effect.succeed({
        rsiPullback: { enabled: false, weight: 0, params: { period: 14 } },
        emaPullback: { enabled: false, weight: 0, params: { period: 9 } },
      }),
    ),
  ),
  current: S.Record(S.String, IndicatorConfigSchema).pipe(
    S.withDecodingDefault(
      Effect.succeed({
        trend: {
          enabled: true,
          weight: 0.18,
          params: { fastPeriod: 9, slowPeriod: 21, style: 0 },
        },
        imbalance: { enabled: true, weight: 0.22, params: {} },
        spread: { enabled: true, weight: 0.18, params: {} },
        liquidity: { enabled: true, weight: 0.09, params: {} },
      }),
    ),
  ),
  lagging: S.Record(S.String, IndicatorConfigSchema).pipe(
    S.withDecodingDefault(
      Effect.succeed({
        rsi: { enabled: true, weight: 0.09, params: { period: 14 } },
        connorsRsi2: { enabled: false, weight: 0, params: {} },
        volatility: { enabled: true, weight: 0.13, params: {} },
        regime: { enabled: true, weight: 0.11, params: {} },
        funding: { enabled: false, weight: 0, params: {} },
      }),
    ),
  ),
});

export type IndicatorsConfig = typeof IndicatorsConfigSchema.Type;

export const SignalRulesConfigSchema = S.Struct({
  minConfluence: S.Number.pipe(S.withDecodingDefault(Effect.succeed(0))),
  minCategoryConfluence: S.Number.pipe(
    S.withDecodingDefault(Effect.succeed(0)),
  ),
  minConfidence: S.Number.pipe(S.withDecodingDefault(Effect.succeed(0.5))),
  regimeMode: S.Literals(["trend", "reversion", "breakout"]).pipe(
    S.withDecodingDefault(Effect.succeed("trend" as const)),
  ),
  breakoutLookback: S.Number.pipe(S.withDecodingDefault(Effect.succeed(20))),
  breakoutVolumeMinRatio: S.Number.pipe(
    S.withDecodingDefault(Effect.succeed(1.2)),
  ),
  breakoutAdxMin: S.Number.pipe(S.withDecodingDefault(Effect.succeed(20))),
  directionalOnly: S.Boolean.pipe(S.withDecodingDefault(Effect.succeed(false))),
  strictAgreement: S.Boolean.pipe(S.withDecodingDefault(Effect.succeed(false))),
  rsiFollowTrend: S.Boolean.pipe(S.withDecodingDefault(Effect.succeed(false))),
  volumeMinRatio: S.Number.pipe(S.withDecodingDefault(Effect.succeed(0))),
  volumeLookback: S.Number.pipe(S.withDecodingDefault(Effect.succeed(20))),
  maxSpreadPct: S.Number.pipe(S.withDecodingDefault(Effect.succeed(0))),
  minLiquidity: S.Number.pipe(S.withDecodingDefault(Effect.succeed(0))),
  adxMin: S.Number.pipe(S.withDecodingDefault(Effect.succeed(0))),
  atrMinPctOfPrice: S.Number.pipe(S.withDecodingDefault(Effect.succeed(0))),
  atrMaxPctOfPrice: S.Number.pipe(S.withDecodingDefault(Effect.succeed(0))),
  minEfficiencyRatio: S.Number.pipe(S.withDecodingDefault(Effect.succeed(0))),
  bollingerEntryMinPct: S.Number.pipe(S.withDecodingDefault(Effect.succeed(0))),
  bollingerEntryMaxPct: S.Number.pipe(S.withDecodingDefault(Effect.succeed(0))),
  useAdaptiveMarketFilters: S.Boolean.pipe(
    S.withDecodingDefault(Effect.succeed(false)),
  ),
  adaptiveLookback: S.Number.pipe(S.withDecodingDefault(Effect.succeed(100))),
  trendFilterFastPeriod: S.Number.pipe(
    S.withDecodingDefault(Effect.succeed(50)),
  ),
  trendFilterSlowPeriod: S.Number.pipe(
    S.withDecodingDefault(Effect.succeed(100)),
  ),
  trendFilterPeriod: S.Number.pipe(S.withDecodingDefault(Effect.succeed(200))),
  entryRsiLongThreshold: S.Number.pipe(
    S.withDecodingDefault(Effect.succeed(10)),
  ),
  entryRsiShortThreshold: S.Number.pipe(
    S.withDecodingDefault(Effect.succeed(90)),
  ),
  exitRsiLongThreshold: S.Number.pipe(
    S.withDecodingDefault(Effect.succeed(60)),
  ),
  exitRsiShortThreshold: S.Number.pipe(
    S.withDecodingDefault(Effect.succeed(40)),
  ),
  entryCandleConfirm: S.Boolean.pipe(
    S.withDecodingDefault(Effect.succeed(false)),
  ),
  momentumConfirmBars: S.Number.pipe(S.withDecodingDefault(Effect.succeed(0))),
  signalPersistence: S.Number.pipe(S.withDecodingDefault(Effect.succeed(0))),
  autoRegimeFilter: S.Boolean.pipe(
    S.withDecodingDefault(Effect.succeed(false)),
  ),
  autoRegimeAdxThreshold: S.Number.pipe(
    S.withDecodingDefault(Effect.succeed(25)),
  ),
  sessionStart: S.String.pipe(S.withDecodingDefault(Effect.succeed(""))),
  sessionEnd: S.String.pipe(S.withDecodingDefault(Effect.succeed(""))),
  trendSignalStyle: S.Literals(["slope", "cross"]).pipe(
    S.withDecodingDefault(Effect.succeed("slope" as const)),
  ),
  fundingBiasThreshold: S.Number.pipe(
    S.withDecodingDefault(Effect.succeed(0.0001)),
  ),
  useFunding: S.Boolean.pipe(S.withDecodingDefault(Effect.succeed(false))),
});

export type SignalRulesConfig = typeof SignalRulesConfigSchema.Type;

export const ExecutionConfigSchema = S.Struct({
  stopLossPct: S.Number.pipe(S.withDecodingDefault(Effect.succeed(1.0))),
  takeProfitPct: S.Number.pipe(S.withDecodingDefault(Effect.succeed(1.2))),
  useAtrStops: S.Boolean.pipe(S.withDecodingDefault(Effect.succeed(false))),
  atrStopMultiplier: S.Number.pipe(S.withDecodingDefault(Effect.succeed(1.5))),
  atrTakeProfitMultiplier: S.Number.pipe(
    S.withDecodingDefault(Effect.succeed(2.5)),
  ),
  atrRiskReward: S.Number.pipe(S.withDecodingDefault(Effect.succeed(0))),
  useAdaptiveStops: S.Boolean.pipe(
    S.withDecodingDefault(Effect.succeed(false)),
  ),
  adaptiveStopAtrMultiplier: S.Number.pipe(
    S.withDecodingDefault(Effect.succeed(1.5)),
  ),
  adaptiveRiskReward: S.Number.pipe(S.withDecodingDefault(Effect.succeed(2))),
  scaleOutAtR: S.Number.pipe(S.withDecodingDefault(Effect.succeed(0))),
  scaleOutPct: S.Number.pipe(S.withDecodingDefault(Effect.succeed(50))),
  riskPerTradePct: S.Number.pipe(S.withDecodingDefault(Effect.succeed(0))),
  positionSizePct: S.Number.pipe(S.withDecodingDefault(Effect.succeed(100))),
  maxPositionSizePct: S.Number.pipe(S.withDecodingDefault(Effect.succeed(100))),
  volatilityTargetAnnualPct: S.Number.pipe(
    S.withDecodingDefault(Effect.succeed(0)),
  ),
  leverage: S.Number.pipe(S.withDecodingDefault(Effect.succeed(1))),
  feePct: S.Number.pipe(S.withDecodingDefault(Effect.succeed(0.1))),
  makerFeePct: S.Number.pipe(S.withDecodingDefault(Effect.succeed(0))),
  entryOrderType: S.Literals(["market", "limit"]).pipe(
    S.withDecodingDefault(Effect.succeed("market" as const)),
  ),
  entryLimitOffsetBps: S.Number.pipe(S.withDecodingDefault(Effect.succeed(0))),
  slippageBps: S.Number.pipe(S.withDecodingDefault(Effect.succeed(0))),
  maxBarsInTrade: S.Number.pipe(S.withDecodingDefault(Effect.succeed(0))),
  lossCooldownBars: S.Number.pipe(S.withDecodingDefault(Effect.succeed(0))),
  lossConfidencePenalty: S.Number.pipe(
    S.withDecodingDefault(Effect.succeed(0)),
  ),
  lossConfidenceDecay: S.Number.pipe(S.withDecodingDefault(Effect.succeed(0))),
  breakevenAtR: S.Number.pipe(S.withDecodingDefault(Effect.succeed(0))),
  trailingStopPct: S.Number.pipe(S.withDecodingDefault(Effect.succeed(0))),
  trailingStopAtrMultiplier: S.Number.pipe(
    S.withDecodingDefault(Effect.succeed(0)),
  ),
  holdUntilStop: S.Boolean.pipe(S.withDecodingDefault(Effect.succeed(false))),
  entryOnClose: S.Boolean.pipe(S.withDecodingDefault(Effect.succeed(false))),
  observedPrice: S.Boolean.pipe(S.withDecodingDefault(Effect.succeed(false))),
  strictRealism: S.Boolean.pipe(S.withDecodingDefault(Effect.succeed(false))),
  minAtrPct: S.Number.pipe(S.withDecodingDefault(Effect.succeed(0))),
  exitRsiPeriod: S.Number.pipe(S.withDecodingDefault(Effect.succeed(0))),
  exitRsiLongLevel: S.Number.pipe(S.withDecodingDefault(Effect.succeed(0))),
  exitRsiShortLevel: S.Number.pipe(S.withDecodingDefault(Effect.succeed(0))),
  volatilityLookback: S.Number.pipe(S.withDecodingDefault(Effect.succeed(0))),
  volatilityLowPct: S.Number.pipe(S.withDecodingDefault(Effect.succeed(20))),
  volatilityHighPct: S.Number.pipe(S.withDecodingDefault(Effect.succeed(80))),
  volatilityLowFactor: S.Number.pipe(
    S.withDecodingDefault(Effect.succeed(0.8)),
  ),
  volatilityHighFactor: S.Number.pipe(
    S.withDecodingDefault(Effect.succeed(1.2)),
  ),
});

export type ExecutionConfig = typeof ExecutionConfigSchema.Type;

export const PortfolioConfigSchema = S.Struct({
  maxOpenPositions: S.Number.pipe(S.withDecodingDefault(Effect.succeed(1))),
  maxPortfolioHeatPct: S.Number.pipe(
    S.withDecodingDefault(Effect.succeed(100)),
  ),
  correlationFilter: S.Boolean.pipe(
    S.withDecodingDefault(Effect.succeed(false)),
  ),
  correlationLookback: S.Number.pipe(S.withDecodingDefault(Effect.succeed(50))),
  correlationThreshold: S.Number.pipe(
    S.withDecodingDefault(Effect.succeed(0.8)),
  ),
  allowlist: S.Array(S.String).pipe(S.withDecodingDefault(Effect.succeed([]))),
  blocklist: S.Array(S.String).pipe(S.withDecodingDefault(Effect.succeed([]))),
});

export type PortfolioConfig = typeof PortfolioConfigSchema.Type;

export const StrategyConfigSchema = S.Struct({
  name: S.optional(S.String),
  version: S.Literal("1").pipe(S.withDecodingDefault(Effect.succeed("1"))),
  indicators: IndicatorsConfigSchema.pipe(
    S.withDecodingDefault(Effect.sync(defaultIndicatorsConfig)),
  ),
  signalRules: SignalRulesConfigSchema.pipe(
    S.withDecodingDefault(Effect.sync(defaultSignalRulesConfig)),
  ),
  execution: ExecutionConfigSchema.pipe(
    S.withDecodingDefault(Effect.sync(defaultExecutionConfig)),
  ),
  portfolio: PortfolioConfigSchema.pipe(
    S.withDecodingDefault(Effect.sync(defaultPortfolioConfig)),
  ),
});

export type StrategyConfig = typeof StrategyConfigSchema.Type;

export const decodeStrategyConfig = S.decodeUnknownEffect(StrategyConfigSchema);

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
): Effect.Effect<void, Error, FileSystem.FileSystem> {
  return Effect.gen(function* () {
    const filePath = configPath(homeDir, name);
    const dir = path.dirname(filePath);
    const fsys = yield* FileSystem.FileSystem;
    yield* fsys
      .makeDirectory(dir, { recursive: true })
      .pipe(
        Effect.mapError(
          (cause) =>
            new Error(
              `Failed to create strategy config directory ${dir}: ${String(cause)}`,
            ),
        ),
      );
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
