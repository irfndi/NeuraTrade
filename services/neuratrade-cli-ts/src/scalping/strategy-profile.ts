import * as S from "effect/Schema";
import { Effect } from "effect";
import * as fs from "fs";
import * as path from "path";

/**
 * Raw strategy parameter block without decoding defaults. Used as the base
 * for both the global defaults schema and the per-symbol override schema.
 */
const StrategyProfileParamsSchemaRaw = S.Struct({
  minConfidence: S.optional(S.Number),
  useAtrStops: S.optional(S.Boolean),
  atrStopMultiplier: S.optional(S.Number),
  atrTakeProfitMultiplier: S.optional(S.Number),
  atrRiskReward: S.optional(S.Number),
  stopLossPct: S.optional(S.Number),
  takeProfitPct: S.optional(S.Number),
  scaleOutAtR: S.optional(S.Number),
  scaleOutPct: S.optional(S.Number),
  volatilityLookback: S.optional(S.Number),
  volatilityLowPct: S.optional(S.Number),
  volatilityHighPct: S.optional(S.Number),
  volatilityLowFactor: S.optional(S.Number),
  volatilityHighFactor: S.optional(S.Number),
  positionSizePct: S.optional(S.Number),
  riskPerTradePct: S.optional(S.Number),
  maxPositionSizePct: S.optional(S.Number),
  minAtrPct: S.optional(S.Number),
  holdUntilStop: S.optional(S.Boolean),
  feePct: S.optional(S.Number),
  volumeMinRatio: S.optional(S.Number),
  volumeLookback: S.optional(S.Number),
  minConfluence: S.optional(S.Number),
  entryCandleConfirm: S.optional(S.Boolean),
  momentumConfirmBars: S.optional(S.Number),
  signalPersistence: S.optional(S.Number),
  lossConfidencePenalty: S.optional(S.Number),
  lossConfidenceDecay: S.optional(S.Number),
  adxMin: S.optional(S.Number),
  htfTimeframe: S.optional(S.String),
  htfTrendFastPeriod: S.optional(S.Number),
  htfTrendSlowPeriod: S.optional(S.Number),
  entryPullbackEmaPeriod: S.optional(S.Number),
  entryPullbackMarginPct: S.optional(S.Number),
  minEfficiencyRatio: S.optional(S.Number),
  efficiencyRatioPeriod: S.optional(S.Number),
  rsiLongMax: S.optional(S.Number),
  rsiShortMin: S.optional(S.Number),
  bollingerLongMaxPctB: S.optional(S.Number),
  bollingerShortMinPctB: S.optional(S.Number),
  exchange: S.optional(S.String),
  defaultSymbol: S.optional(S.String),
  timeframe: S.optional(S.String),
});

function defaultStrategyProfileParams(): StrategyProfileParams {
  return {
    minConfidence: 0,
    useAtrStops: false,
    atrStopMultiplier: 0,
    atrTakeProfitMultiplier: 0,
    atrRiskReward: 0,
    stopLossPct: 0,
    takeProfitPct: 0,
    scaleOutAtR: 0,
    scaleOutPct: 0,
    volatilityLookback: 0,
    volatilityLowPct: 20,
    volatilityHighPct: 80,
    volatilityLowFactor: 0.8,
    volatilityHighFactor: 1.2,
    positionSizePct: 0,
    riskPerTradePct: 0,
    maxPositionSizePct: 100,
    minAtrPct: 0,
    holdUntilStop: false,
    feePct: 0,
    volumeMinRatio: 0,
    volumeLookback: 20,
    minConfluence: 0,
    entryCandleConfirm: false,
    momentumConfirmBars: 0,
    signalPersistence: 0,
    lossConfidencePenalty: 0,
    lossConfidenceDecay: 0,
    adxMin: 0,
    htfTimeframe: undefined,
    htfTrendFastPeriod: 50,
    htfTrendSlowPeriod: 100,
    entryPullbackEmaPeriod: 0,
    entryPullbackMarginPct: 0.1,
    minEfficiencyRatio: 0,
    efficiencyRatioPeriod: 20,
    rsiLongMax: 0,
    rsiShortMin: 0,
    bollingerLongMaxPctB: -1,
    bollingerShortMinPctB: 2,
  };
}

/**
 * Strategy parameter block with per-field decoding defaults. These names use
 * the same suffixes as the backtest engine (`stopLossPct`, `positionSizePct`,
 * etc.).
 */
const StrategyProfileParamsSchema = S.Struct({
  minConfidence: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  useAtrStops: S.optional(S.Boolean).pipe(S.withDecodingDefault(() => false)),
  atrStopMultiplier: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  atrTakeProfitMultiplier: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 0),
  ),
  atrRiskReward: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  stopLossPct: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  takeProfitPct: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  scaleOutAtR: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  scaleOutPct: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  volatilityLookback: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  volatilityLowPct: S.optional(S.Number).pipe(S.withDecodingDefault(() => 20)),
  volatilityHighPct: S.optional(S.Number).pipe(S.withDecodingDefault(() => 80)),
  volatilityLowFactor: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 0.8),
  ),
  volatilityHighFactor: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 1.2),
  ),
  positionSizePct: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  riskPerTradePct: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  maxPositionSizePct: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 100),
  ),
  minAtrPct: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  holdUntilStop: S.optional(S.Boolean).pipe(S.withDecodingDefault(() => false)),
  feePct: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  volumeMinRatio: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  volumeLookback: S.optional(S.Number).pipe(S.withDecodingDefault(() => 20)),
  minConfluence: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  entryCandleConfirm: S.optional(S.Boolean).pipe(
    S.withDecodingDefault(() => false),
  ),
  momentumConfirmBars: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 0),
  ),
  signalPersistence: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  lossConfidencePenalty: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 0),
  ),
  lossConfidenceDecay: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 0),
  ),
  adxMin: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  htfTimeframe: S.optional(S.String),
  htfTrendFastPeriod: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 50),
  ),
  htfTrendSlowPeriod: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 100),
  ),
  entryPullbackEmaPeriod: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 0),
  ),
  entryPullbackMarginPct: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 0.1),
  ),
  minEfficiencyRatio: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  efficiencyRatioPeriod: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 20),
  ),
  rsiLongMax: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  rsiShortMin: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  bollingerLongMaxPctB: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => -1),
  ),
  bollingerShortMinPctB: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 2),
  ),
  exchange: S.optional(S.String),
  defaultSymbol: S.optional(S.String),
  timeframe: S.optional(S.String),
});

export type StrategyProfileParams = typeof StrategyProfileParamsSchema.Type;

const SymbolOverrideSchema = S.partial(StrategyProfileParamsSchemaRaw);

const StrategyProfileSchema = S.Struct({
  name: S.optional(S.String),
  defaults: S.optional(StrategyProfileParamsSchema).pipe(
    S.withDecodingDefault(defaultStrategyProfileParams),
  ),
  symbols: S.optional(
    S.Record({ key: S.String, value: SymbolOverrideSchema }),
  ).pipe(S.withDecodingDefault(() => ({}))),
});

export type StrategyProfile = typeof StrategyProfileSchema.Type;

export const decodeStrategyProfile = S.decodeUnknown(StrategyProfileSchema);

/**
 * Resolved CLI args shape used by the backtest/optimize/scan commands.
 * Field names match the command-line option names (`--stop-loss`, etc.).
 */
export interface ResolvedBacktestArgs {
  readonly exchange: string;
  readonly symbol: string;
  readonly timeframe: string;
  readonly capital: number;
  readonly positionSize: number;
  readonly riskPerTrade: number;
  readonly maxPositionSize: number;
  readonly stopLoss: number;
  readonly takeProfit: number;
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
  readonly priceOnly: boolean;
  readonly noRsi: boolean;
  readonly holdUntilStop: boolean;
  readonly noTrend: boolean;
  readonly regimeMode: "trend" | "reversion";
  readonly futures: boolean;
  readonly fundingRatePct: number;
  readonly slippageBps: number;
  readonly trailingStopPct: number;
  readonly trailingStopAtrMultiplier: number;
  readonly minAtrPct: number;
  readonly volumeMinRatio: number;
  readonly volumeLookback: number;
  readonly minConfluence: number;
  readonly entryCandleConfirm: boolean;
  readonly signalPersistence: number;
  readonly momentumConfirmBars: number;
  readonly lossConfidencePenalty: number;
  readonly lossConfidenceDecay: number;
  readonly adxMin: number;
  readonly htfTimeframe?: string;
  readonly htfTrendFastPeriod: number;
  readonly htfTrendSlowPeriod: number;
  readonly entryPullbackEmaPeriod: number;
  readonly entryPullbackMarginPct: number;
  readonly minEfficiencyRatio: number;
  readonly efficiencyRatioPeriod: number;
  readonly rsiLongMax: number;
  readonly rsiShortMin: number;
  readonly bollingerLongMaxPctB: number;
  readonly bollingerShortMinPctB: number;
}

function profileDir(homeDir: string): string {
  return path.join(homeDir, "profiles");
}

function profilePath(homeDir: string, profileName: string): string {
  const safeName = path.basename(profileName).replace(/[^a-zA-Z0-9_-]/g, "_");
  return path.join(profileDir(homeDir), `${safeName}.json`);
}

/**
 * Load a strategy profile from `~/.neuratrade/profiles/<profileName>.json`.
 */
export function loadStrategyProfile(
  homeDir: string,
  profileName: string,
): Effect.Effect<StrategyProfile, Error> {
  return Effect.gen(function* () {
    const filePath = profilePath(homeDir, profileName);
    const text = yield* Effect.tryPromise({
      try: async () => {
        const file = Bun.file(filePath);
        return await file.text();
      },
      catch: (err) =>
        new Error(
          `Failed to read profile ${profileName}: ${err instanceof Error ? err.message : String(err)}`,
        ),
    });
    const json = yield* Effect.try({
      try: () => JSON.parse(text) as unknown,
      catch: (err) =>
        new Error(
          `Invalid JSON in profile ${profileName}: ${err instanceof Error ? err.message : String(err)}`,
        ),
    });
    return yield* decodeStrategyProfile(json);
  });
}

/**
 * Save a strategy profile to `~/.neuratrade/profiles/<profileName>.json`.
 */
export function saveStrategyProfile(
  homeDir: string,
  profileName: string,
  profile: StrategyProfile,
): Effect.Effect<void, Error> {
  return Effect.gen(function* () {
    const dir = profileDir(homeDir);
    yield* Effect.sync(() => fs.mkdirSync(dir, { recursive: true }));
    const filePath = profilePath(homeDir, profileName);
    const payload = JSON.stringify(profile, null, 2);
    yield* Effect.tryPromise({
      try: () => Bun.write(filePath, payload),
      catch: (err) =>
        new Error(
          `Failed to write profile ${profileName}: ${err instanceof Error ? err.message : String(err)}`,
        ),
    });
  });
}

/**
 * Merge profile defaults, per-symbol overrides, and CLI overrides.
 * CLI overrides always win.
 */
export function resolveBacktestArgs(
  profile: StrategyProfile,
  symbol: string,
  exchange: string,
  timeframe: string,
  cliArgs: ResolvedBacktestArgs,
): ResolvedBacktestArgs {
  const defaults = profile.defaults;
  const symbolOverride = profile.symbols[symbol] ?? {};

  const get = <K extends keyof StrategyProfileParams>(
    key: K,
  ): StrategyProfileParams[K] => {
    if (symbolOverride[key] !== undefined)
      return symbolOverride[key] as StrategyProfileParams[K];
    return defaults[key];
  };

  const base: Partial<ResolvedBacktestArgs> = {
    exchange: defaults.exchange ?? exchange,
    symbol: defaults.defaultSymbol ?? symbol,
    timeframe: get("timeframe") ?? timeframe,
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
    signalPersistence: get("signalPersistence"),
    lossConfidencePenalty: get("lossConfidencePenalty"),
    lossConfidenceDecay: get("lossConfidenceDecay"),
    adxMin: get("adxMin"),
    htfTimeframe: get("htfTimeframe"),
    htfTrendFastPeriod: get("htfTrendFastPeriod"),
    htfTrendSlowPeriod: get("htfTrendSlowPeriod"),
    entryPullbackEmaPeriod: get("entryPullbackEmaPeriod"),
    entryPullbackMarginPct: get("entryPullbackMarginPct"),
    minEfficiencyRatio: get("minEfficiencyRatio"),
    efficiencyRatioPeriod: get("efficiencyRatioPeriod"),
    rsiLongMax: get("rsiLongMax"),
    rsiShortMin: get("rsiShortMin"),
    bollingerLongMaxPctB: get("bollingerLongMaxPctB"),
    bollingerShortMinPctB: get("bollingerShortMinPctB"),
  };

  // CLI defaults should not override profile values. Only use CLI values that
  // differ from the built-in command defaults (i.e., were explicitly set).
  const cliDefaults: Partial<ResolvedBacktestArgs> = {
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
    minConfidence: 0.5,
    useAtrStops: false,
    atrStopMultiplier: 1.5,
    atrTakeProfitMultiplier: 2.5,
    atrRiskReward: 0,
    scaleOutAtR: 0,
    scaleOutPct: 50,
    volatilityLookback: 0,
    volatilityLowPct: 20,
    volatilityHighPct: 80,
    volatilityLowFactor: 0.8,
    volatilityHighFactor: 1.2,
    priceOnly: false,
    noRsi: false,
    holdUntilStop: false,
    noTrend: false,
    regimeMode: "trend",
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
    htfTimeframe: undefined,
    htfTrendFastPeriod: 50,
    htfTrendSlowPeriod: 100,
    entryPullbackEmaPeriod: 0,
    entryPullbackMarginPct: 0.1,
    minEfficiencyRatio: 0,
    efficiencyRatioPeriod: 20,
    rsiLongMax: 0,
    rsiShortMin: 0,
    bollingerLongMaxPctB: -1,
    bollingerShortMinPctB: 2,
  };

  const merged = { ...base } as ResolvedBacktestArgs;
  for (const key of Object.keys(cliArgs) as Array<keyof ResolvedBacktestArgs>) {
    const cliValue = cliArgs[key];
    const defaultValue = cliDefaults[key];
    const hasBaseValue = Object.prototype.hasOwnProperty.call(merged, key);
    // If the profile did not provide a value, always fill from CLI (even when
    // it equals the built-in default). If the profile did provide a value, only
    // override when the user explicitly changed the CLI value.
    if (!hasBaseValue || cliValue !== defaultValue) {
      (merged as unknown as Record<string, unknown>)[key] = cliValue;
    }
  }

  return merged;
}

/**
 * Build a strategy profile from CLI args (backtest option shape).
 */
export function buildStrategyProfileFromArgs(
  name: string,
  args: ResolvedBacktestArgs,
): StrategyProfile {
  return {
    name,
    defaults: {
      minConfidence: args.minConfidence,
      useAtrStops: args.useAtrStops,
      atrStopMultiplier: args.atrStopMultiplier,
      atrTakeProfitMultiplier: args.atrTakeProfitMultiplier,
      atrRiskReward: args.atrRiskReward,
      stopLossPct: args.stopLoss,
      takeProfitPct: args.takeProfit,
      scaleOutAtR: args.scaleOutAtR,
      scaleOutPct: args.scaleOutPct,
      volatilityLookback: args.volatilityLookback,
      volatilityLowPct: args.volatilityLowPct,
      volatilityHighPct: args.volatilityHighPct,
      volatilityLowFactor: args.volatilityLowFactor,
      volatilityHighFactor: args.volatilityHighFactor,
      positionSizePct: args.positionSize,
      riskPerTradePct: args.riskPerTrade,
      maxPositionSizePct: args.maxPositionSize,
      minAtrPct: args.minAtrPct,
      holdUntilStop: args.holdUntilStop,
      feePct: args.fee,
      volumeMinRatio: args.volumeMinRatio,
      volumeLookback: args.volumeLookback,
      minConfluence: args.minConfluence,
      entryCandleConfirm: args.entryCandleConfirm,
      momentumConfirmBars: args.momentumConfirmBars,
      signalPersistence: args.signalPersistence,
      lossConfidencePenalty: args.lossConfidencePenalty,
      lossConfidenceDecay: args.lossConfidenceDecay,
      adxMin: args.adxMin,
      htfTimeframe: args.htfTimeframe,
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
    },
    symbols: {},
  };
}
