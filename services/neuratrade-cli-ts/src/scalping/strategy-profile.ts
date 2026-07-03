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
  rsiPeriod: S.optional(S.Number),
  rsiOversoldStrong: S.optional(S.Number),
  rsiOverboughtStrong: S.optional(S.Number),
  stopLossPct: S.optional(S.Number),
  takeProfitPct: S.optional(S.Number),
  scaleOutAtR: S.optional(S.Number),
  scaleOutPct: S.optional(S.Number),
  volatilityLookback: S.optional(S.Number),
  volatilityLowPct: S.optional(S.Number),
  volatilityHighPct: S.optional(S.Number),
  volatilityLowFactor: S.optional(S.Number),
  volatilityHighFactor: S.optional(S.Number),
  volatilityTargetAnnualPct: S.optional(S.Number),
  positionSizePct: S.optional(S.Number),
  riskPerTradePct: S.optional(S.Number),
  maxPositionSizePct: S.optional(S.Number),
  minAtrPct: S.optional(S.Number),
  holdUntilStop: S.optional(S.Boolean),
  feePct: S.optional(S.Number),
  makerFeePct: S.optional(S.Number),
  entryOrderType: S.optional(S.Literal("market", "limit")),
  entryLimitOffsetBps: S.optional(S.Number),
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
  htfSignalConfidence: S.optional(S.Number),
  entryPullbackEmaPeriod: S.optional(S.Number),
  entryPullbackMarginPct: S.optional(S.Number),
  minEfficiencyRatio: S.optional(S.Number),
  efficiencyRatioPeriod: S.optional(S.Number),
  rsiLongMax: S.optional(S.Number),
  rsiShortMin: S.optional(S.Number),
  bollingerLongMaxPctB: S.optional(S.Number),
  bollingerShortMinPctB: S.optional(S.Number),
  trendFilterPeriod: S.optional(S.Number),
  entryRsiLongThreshold: S.optional(S.Number),
  entryRsiShortThreshold: S.optional(S.Number),
  exitRsiPeriod: S.optional(S.Number),
  exitRsiLongLevel: S.optional(S.Number),
  exitRsiShortLevel: S.optional(S.Number),
  recordEquityCurve: S.optional(S.Boolean),
  exportTrades: S.optional(S.String),
  oosPct: S.optional(S.Number),
  mcIterations: S.optional(S.Number),
  leverage: S.optional(S.Number),
  breakevenAtR: S.optional(S.Number),
  maxBarsInTrade: S.optional(S.Number),
  lossCooldownBars: S.optional(S.Number),
  sessionStart: S.optional(S.String),
  sessionEnd: S.optional(S.String),
  autoRegimeFilter: S.optional(S.Boolean),
  autoRegimeAdxThreshold: S.optional(S.Number),
  trendSignalStyle: S.optional(S.Literal("slope", "cross")),
  trendFastPeriod: S.optional(S.Number),
  trendSlowPeriod: S.optional(S.Number),
  directionalOnly: S.optional(S.Boolean),
  rsiFollowTrend: S.optional(S.Boolean),
  strictAgreement: S.optional(S.Boolean),
  entryOnClose: S.optional(S.Boolean),
  observedPrice: S.optional(S.Boolean),
  realistic: S.optional(S.Boolean),
  strictRealism: S.optional(S.Boolean),
  exchange: S.optional(S.String),
  defaultSymbol: S.optional(S.String),
  timeframe: S.optional(S.String),
  regimeMode: S.optional(S.Literal("trend", "reversion", "breakout")),
  breakoutLookback: S.optional(S.Number),
  breakoutVolumeMinRatio: S.optional(S.Number),
  breakoutAdxMin: S.optional(S.Number),
  fundingBiasThreshold: S.optional(S.Number),
  useFunding: S.optional(S.Boolean),
  strategyType: S.optional(S.Literal("signal", "grid")),
  gridStepPct: S.optional(S.Number),
  gridMaxGrids: S.optional(S.Number),
  gridPauseAfterLossBars: S.optional(S.Number),
  onlyWithTrend: S.optional(S.Boolean),
  targetRatio: S.optional(S.Number),
});

function defaultStrategyProfileParams(): StrategyProfileParams {
  return {
    minConfidence: 0,
    useAtrStops: false,
    atrStopMultiplier: 0,
    atrTakeProfitMultiplier: 0,
    atrRiskReward: 0,
    rsiPeriod: 14,
    rsiOversoldStrong: 30,
    rsiOverboughtStrong: 70,
    stopLossPct: 0,
    takeProfitPct: 0,
    scaleOutAtR: 0,
    scaleOutPct: 0,
    volatilityLookback: 0,
    volatilityLowPct: 20,
    volatilityHighPct: 80,
    volatilityLowFactor: 0.8,
    volatilityHighFactor: 1.2,
    volatilityTargetAnnualPct: 0,
    positionSizePct: 0,
    riskPerTradePct: 0,
    maxPositionSizePct: 100,
    minAtrPct: 0,
    holdUntilStop: false,
    feePct: 0,
    makerFeePct: 0,
    entryOrderType: "market" as const,
    entryLimitOffsetBps: 0,
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
    trendSignalStyle: "slope" as const,
    trendFastPeriod: 9,
    trendSlowPeriod: 21,
    directionalOnly: false,
    rsiFollowTrend: false,
    strictAgreement: false,
    entryOnClose: false,
    observedPrice: false,
    realistic: false,
    strictRealism: false,
    exchange: "",
    defaultSymbol: "",
    timeframe: "",
    regimeMode: "trend" as const,
    breakoutLookback: 20,
    breakoutVolumeMinRatio: 1.2,
    breakoutAdxMin: 20,
    fundingBiasThreshold: 0.0001,
    useFunding: false,
    strategyType: "signal" as const,
    gridStepPct: 0,
    gridMaxGrids: 0,
    gridPauseAfterLossBars: 0,
    onlyWithTrend: false,
    targetRatio: 1,
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
  rsiPeriod: S.optional(S.Number).pipe(S.withDecodingDefault(() => 14)),
  rsiOversoldStrong: S.optional(S.Number).pipe(S.withDecodingDefault(() => 30)),
  rsiOverboughtStrong: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 70),
  ),
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
  volatilityTargetAnnualPct: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 0),
  ),
  positionSizePct: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  riskPerTradePct: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  maxPositionSizePct: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 100),
  ),
  minAtrPct: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  holdUntilStop: S.optional(S.Boolean).pipe(S.withDecodingDefault(() => false)),
  feePct: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  makerFeePct: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  entryOrderType: S.optional(S.Literal("market", "limit")).pipe(
    S.withDecodingDefault(() => "market" as const),
  ),
  entryLimitOffsetBps: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 0),
  ),
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
  htfSignalConfidence: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 0),
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
  trendFilterPeriod: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 200),
  ),
  entryRsiLongThreshold: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 10),
  ),
  entryRsiShortThreshold: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 90),
  ),
  exitRsiPeriod: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  exitRsiLongLevel: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  exitRsiShortLevel: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  recordEquityCurve: S.optional(S.Boolean).pipe(
    S.withDecodingDefault(() => false),
  ),
  exportTrades: S.optional(S.String).pipe(S.withDecodingDefault(() => "")),
  oosPct: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  mcIterations: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  leverage: S.optional(S.Number).pipe(S.withDecodingDefault(() => 1)),
  breakevenAtR: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  maxBarsInTrade: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  lossCooldownBars: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  sessionStart: S.optional(S.String).pipe(S.withDecodingDefault(() => "")),
  sessionEnd: S.optional(S.String).pipe(S.withDecodingDefault(() => "")),
  autoRegimeFilter: S.optional(S.Boolean).pipe(
    S.withDecodingDefault(() => false),
  ),
  autoRegimeAdxThreshold: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 25),
  ),
  trendSignalStyle: S.optional(S.Literal("slope", "cross")).pipe(
    S.withDecodingDefault(() => "slope" as const),
  ),
  trendFastPeriod: S.optional(S.Number).pipe(S.withDecodingDefault(() => 9)),
  trendSlowPeriod: S.optional(S.Number).pipe(S.withDecodingDefault(() => 21)),
  directionalOnly: S.optional(S.Boolean).pipe(
    S.withDecodingDefault(() => false),
  ),
  rsiFollowTrend: S.optional(S.Boolean).pipe(
    S.withDecodingDefault(() => false),
  ),
  strictAgreement: S.optional(S.Boolean).pipe(
    S.withDecodingDefault(() => false),
  ),
  entryOnClose: S.optional(S.Boolean).pipe(S.withDecodingDefault(() => false)),
  observedPrice: S.optional(S.Boolean).pipe(S.withDecodingDefault(() => false)),
  realistic: S.optional(S.Boolean).pipe(S.withDecodingDefault(() => false)),
  strictRealism: S.optional(S.Boolean).pipe(S.withDecodingDefault(() => false)),
  exchange: S.optional(S.String).pipe(S.withDecodingDefault(() => "")),
  defaultSymbol: S.optional(S.String).pipe(S.withDecodingDefault(() => "")),
  timeframe: S.optional(S.String).pipe(S.withDecodingDefault(() => "")),
  regimeMode: S.optional(S.Literal("trend", "reversion", "breakout")).pipe(
    S.withDecodingDefault(() => "trend" as const),
  ),
  breakoutLookback: S.optional(S.Number).pipe(S.withDecodingDefault(() => 20)),
  breakoutVolumeMinRatio: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 1.2),
  ),
  breakoutAdxMin: S.optional(S.Number).pipe(S.withDecodingDefault(() => 20)),
  fundingBiasThreshold: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 0.0001),
  ),
  useFunding: S.optional(S.Boolean).pipe(S.withDecodingDefault(() => false)),
  strategyType: S.optional(S.Literal("signal", "grid")).pipe(
    S.withDecodingDefault(() => "signal" as const),
  ),
  gridStepPct: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  gridMaxGrids: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0)),
  gridPauseAfterLossBars: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 0),
  ),
  onlyWithTrend: S.optional(S.Boolean).pipe(
    S.withDecodingDefault(() => false),
  ),
  targetRatio: S.optional(S.Number).pipe(S.withDecodingDefault(() => 1)),
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
  readonly makerFeePct: number;
  readonly entryOrderType: "market" | "limit";
  readonly entryLimitOffsetBps: number;
  readonly minConfidence: number;
  readonly useAtrStops: boolean;
  readonly atrStopMultiplier: number;
  readonly atrTakeProfitMultiplier: number;
  readonly atrRiskReward: number;
  readonly rsiPeriod: number;
  readonly rsiOversoldStrong: number;
  readonly rsiOverboughtStrong: number;
  readonly scaleOutAtR: number;
  readonly scaleOutPct: number;
  readonly volatilityLookback: number;
  readonly volatilityLowPct: number;
  readonly volatilityHighPct: number;
  readonly volatilityLowFactor: number;
  readonly volatilityHighFactor: number;
  readonly volatilityTargetAnnualPct: number;
  readonly priceOnly: boolean;
  readonly noRsi: boolean;
  readonly holdUntilStop: boolean;
  readonly noTrend: boolean;
  readonly regimeMode: "trend" | "reversion" | "breakout";
  readonly breakoutLookback: number;
  readonly breakoutVolumeMinRatio: number;
  readonly breakoutAdxMin: number;
  readonly fundingBiasThreshold: number;
  readonly useFunding: boolean;
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
  readonly htfSignalConfidence: number;
  readonly entryPullbackEmaPeriod: number;
  readonly entryPullbackMarginPct: number;
  readonly minEfficiencyRatio: number;
  readonly efficiencyRatioPeriod: number;
  readonly rsiLongMax: number;
  readonly rsiShortMin: number;
  readonly bollingerLongMaxPctB: number;
  readonly bollingerShortMinPctB: number;
  readonly trendFilterPeriod: number;
  readonly entryRsiLongThreshold: number;
  readonly entryRsiShortThreshold: number;
  readonly exitRsiPeriod: number;
  readonly exitRsiLongLevel: number;
  readonly exitRsiShortLevel: number;
  readonly recordEquityCurve: boolean;
  readonly exportTrades: string;
  readonly oosPct: number;
  readonly mcIterations: number;
  readonly leverage: number;
  readonly breakevenAtR: number;
  readonly maxBarsInTrade: number;
  readonly lossCooldownBars: number;
  readonly sessionStart: string;
  readonly sessionEnd: string;
  readonly autoRegimeFilter: boolean;
  readonly autoRegimeAdxThreshold: number;
  readonly trendSignalStyle: "slope" | "cross";
  readonly trendFastPeriod: number;
  readonly trendSlowPeriod: number;
  readonly directionalOnly: boolean;
  readonly rsiFollowTrend: boolean;
  readonly strictAgreement: boolean;
  readonly entryOnClose: boolean;
  readonly observedPrice: boolean;
  readonly realistic: boolean;
  readonly strictRealism: boolean;
  readonly realisticSlippageBps: number;
  readonly strategyType?: "signal" | "grid";
  readonly gridStepPct: number;
  readonly gridMaxGrids: number;
  readonly gridPauseAfterLossBars: number;
  readonly onlyWithTrend?: boolean;
  readonly targetRatio?: number;
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
    rsiPeriod: get("rsiPeriod"),
    rsiOversoldStrong: get("rsiOversoldStrong"),
    rsiOverboughtStrong: get("rsiOverboughtStrong"),
    stopLoss: get("stopLossPct"),
    takeProfit: get("takeProfitPct"),
    scaleOutAtR: get("scaleOutAtR"),
    scaleOutPct: get("scaleOutPct"),
    volatilityLookback: get("volatilityLookback"),
    volatilityLowPct: get("volatilityLowPct"),
    volatilityHighPct: get("volatilityHighPct"),
    volatilityLowFactor: get("volatilityLowFactor"),
    volatilityHighFactor: get("volatilityHighFactor"),
    volatilityTargetAnnualPct: get("volatilityTargetAnnualPct"),
    positionSize: get("positionSizePct"),
    riskPerTrade: get("riskPerTradePct"),
    maxPositionSize: get("maxPositionSizePct"),
    minAtrPct: get("minAtrPct"),
    holdUntilStop: get("holdUntilStop"),
    fee: get("feePct"),
    makerFeePct: get("makerFeePct"),
    entryOrderType: get("entryOrderType"),
    entryLimitOffsetBps: get("entryLimitOffsetBps"),
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
    htfSignalConfidence: get("htfSignalConfidence"),
    entryPullbackEmaPeriod: get("entryPullbackEmaPeriod"),
    entryPullbackMarginPct: get("entryPullbackMarginPct"),
    minEfficiencyRatio: get("minEfficiencyRatio"),
    efficiencyRatioPeriod: get("efficiencyRatioPeriod"),
    rsiLongMax: get("rsiLongMax"),
    rsiShortMin: get("rsiShortMin"),
    bollingerLongMaxPctB: get("bollingerLongMaxPctB"),
    bollingerShortMinPctB: get("bollingerShortMinPctB"),
    trendFilterPeriod: get("trendFilterPeriod"),
    entryRsiLongThreshold: get("entryRsiLongThreshold"),
    entryRsiShortThreshold: get("entryRsiShortThreshold"),
    exitRsiPeriod: get("exitRsiPeriod"),
    exitRsiLongLevel: get("exitRsiLongLevel"),
    exitRsiShortLevel: get("exitRsiShortLevel"),
    recordEquityCurve: get("recordEquityCurve"),
    exportTrades: get("exportTrades"),
    oosPct: get("oosPct"),
    mcIterations: get("mcIterations"),
    leverage: get("leverage"),
    breakevenAtR: get("breakevenAtR"),
    maxBarsInTrade: get("maxBarsInTrade"),
    lossCooldownBars: get("lossCooldownBars"),
    sessionStart: get("sessionStart"),
    sessionEnd: get("sessionEnd"),
    autoRegimeFilter: get("autoRegimeFilter"),
    autoRegimeAdxThreshold: get("autoRegimeAdxThreshold"),
    trendSignalStyle: get("trendSignalStyle"),
    trendFastPeriod: get("trendFastPeriod"),
    trendSlowPeriod: get("trendSlowPeriod"),
    directionalOnly: get("directionalOnly"),
    rsiFollowTrend: get("rsiFollowTrend"),
    strictAgreement: get("strictAgreement"),
    entryOnClose: get("entryOnClose"),
    observedPrice: get("observedPrice"),
    realistic: get("realistic"),
    strictRealism: get("strictRealism"),
    realisticSlippageBps: 5,
    breakoutLookback: get("breakoutLookback"),
    breakoutVolumeMinRatio: get("breakoutVolumeMinRatio"),
    breakoutAdxMin: get("breakoutAdxMin"),
    fundingBiasThreshold: get("fundingBiasThreshold"),
    useFunding: get("useFunding"),
    strategyType: get("strategyType") ?? "signal",
    gridStepPct: get("gridStepPct") ?? 0,
    gridMaxGrids: get("gridMaxGrids") ?? 0,
    gridPauseAfterLossBars: get("gridPauseAfterLossBars") ?? 0,
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
    makerFeePct: 0,
    entryOrderType: "market" as const,
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
    breakoutLookback: 20,
    breakoutVolumeMinRatio: 1.2,
    breakoutAdxMin: 20,
    fundingBiasThreshold: 0.0001,
    useFunding: false,
    strategyType: "signal",
    gridStepPct: 0,
    gridMaxGrids: 0,
    gridPauseAfterLossBars: 0,
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
      rsiPeriod: args.rsiPeriod,
      rsiOversoldStrong: args.rsiOversoldStrong,
      rsiOverboughtStrong: args.rsiOverboughtStrong,
      stopLossPct: args.stopLoss,
      takeProfitPct: args.takeProfit,
      scaleOutAtR: args.scaleOutAtR,
      scaleOutPct: args.scaleOutPct,
      volatilityLookback: args.volatilityLookback,
      volatilityLowPct: args.volatilityLowPct,
      volatilityHighPct: args.volatilityHighPct,
      volatilityLowFactor: args.volatilityLowFactor,
      volatilityHighFactor: args.volatilityHighFactor,
      volatilityTargetAnnualPct: args.volatilityTargetAnnualPct,
      positionSizePct: args.positionSize,
      riskPerTradePct: args.riskPerTrade,
      maxPositionSizePct: args.maxPositionSize,
      minAtrPct: args.minAtrPct,
      holdUntilStop: args.holdUntilStop,
      feePct: args.fee,
      makerFeePct: args.makerFeePct,
      entryOrderType: args.entryOrderType,
      entryLimitOffsetBps: args.entryLimitOffsetBps,
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
      htfSignalConfidence: args.htfSignalConfidence,
      entryPullbackEmaPeriod: args.entryPullbackEmaPeriod,
      entryPullbackMarginPct: args.entryPullbackMarginPct,
      minEfficiencyRatio: args.minEfficiencyRatio,
      efficiencyRatioPeriod: args.efficiencyRatioPeriod,
      rsiLongMax: args.rsiLongMax,
      rsiShortMin: args.rsiShortMin,
      bollingerLongMaxPctB: args.bollingerLongMaxPctB,
      bollingerShortMinPctB: args.bollingerShortMinPctB,
      trendFilterPeriod: args.trendFilterPeriod,
      entryRsiLongThreshold: args.entryRsiLongThreshold,
      entryRsiShortThreshold: args.entryRsiShortThreshold,
      exitRsiPeriod: args.exitRsiPeriod,
      exitRsiLongLevel: args.exitRsiLongLevel,
      exitRsiShortLevel: args.exitRsiShortLevel,
      recordEquityCurve: args.recordEquityCurve,
      exportTrades: args.exportTrades,
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
      trendSignalStyle: args.trendSignalStyle,
      trendFastPeriod: args.trendFastPeriod,
      trendSlowPeriod: args.trendSlowPeriod,
      directionalOnly: args.directionalOnly,
      rsiFollowTrend: args.rsiFollowTrend,
      strictAgreement: args.strictAgreement,
      entryOnClose: args.entryOnClose,
      observedPrice: args.observedPrice,
      realistic: args.realistic,
      strictRealism: args.strictRealism,
      exchange: args.exchange,
      defaultSymbol: args.symbol,
      timeframe: args.timeframe,
      regimeMode: args.regimeMode,
      breakoutLookback: args.breakoutLookback,
      breakoutVolumeMinRatio: args.breakoutVolumeMinRatio,
      breakoutAdxMin: args.breakoutAdxMin,
      fundingBiasThreshold: args.fundingBiasThreshold,
      useFunding: args.useFunding,
      strategyType: args.strategyType ?? "signal",
      gridStepPct: args.gridStepPct,
      gridMaxGrids: args.gridMaxGrids,
      gridPauseAfterLossBars: args.gridPauseAfterLossBars,
      onlyWithTrend: args.onlyWithTrend ?? false,
      targetRatio: args.targetRatio ?? 1,
    },
    symbols: {},
  };
}
