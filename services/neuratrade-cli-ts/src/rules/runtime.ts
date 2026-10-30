/**
 * StrategySpec runtime — the parallel multi-strategy supervisor.
 *
 * Runs every enabled spec over the union of its symbols, fetching each
 * symbol's candles ONCE per iteration and evaluating every spec against that
 * shared bar. Per-(spec, symbol) state is fully isolated (one capital + one
 * position per symbol), restart-resumable via per-spec JSON state, and risk
 * is enforced per spec with the graceful protocol the hardcoded engines
 * lack:
 *
 *   - drawdown breach  -> realize the open position (risk-close), then
 *     quarantine the SYMBOL; the rest of the portfolio keeps operating. The
 *     "bar rolled back forever" deadlock observed on the live champion runs
 *     (bd clever-cabin-70n4) cannot occur here;
 *   - daily loss breach -> entries blocked until the next UTC day; stops and
 *     targets stay active;
 *   - min capital / max trades per day -> entries blocked.
 *
 * Fills are ledger-simulated paper (close ± slippage bps, fee per side);
 * every closed trade carries rule attributions, and specs with
 * `autoReweight` fold attribution-derived factors into their next compile.
 * Paper ledger math uses plain numbers like the backtest engines; decimal
 * accounting applies on the live path (live wiring is a later, gated phase).
 */

import { Console, Context, Effect, Layer } from "effect";
import type { PlatformError } from "effect/PlatformError";
// Context.Service + Layer.effect are the v4 idioms used to expose the service.
import * as S from "effect/Schema";
import { FileSystem } from "effect";
import { composeSignal } from "../scalping/composer.js";
import { calculateATR } from "../scalping/indicators.js";
import type {
  CandleLike,
  ComposerConfig,
  OHLCVInput,
  OrderBookMetricsInput,
  ScalpingSignal,
} from "../scalping/types.js";
import {
  MarketDataGateway,
  MarketDataError,
  validateCandlePrices,
  type MarketDataGatewayService,
} from "../market-data/gateway.js";
import {
  defaultRuleRegistry,
  loadRuleModules,
  type RuleRegistry,
} from "./registry.js";
import {
  SpecError,
  StrategySpecSchema,
  buildConditionContext,
  buildSpecRegistry,
  compileStrategySpec,
  setSpecBarContext,
  evaluateConditions,
  type StrategySpec,
} from "./spec.js";
import {
  aggregateRuleAttribution,
  applyReweightFactors,
  proposeReweightFactors,
  type AttributionTrade,
} from "./attribution.js";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export type SpecSide = "LONG" | "SHORT";

export type SpecExitReason = "target" | "stop" | "hold-time" | "risk-close";

export interface SignalAttribution {
  readonly ruleId: string;
  readonly weightShare: number;
  readonly alignment: "aligned" | "against" | "hold";
}

export interface SpecPosition {
  readonly side: SpecSide;
  readonly entryPrice: number;
  readonly qty: number;
  readonly stop: number;
  readonly target: number;
  readonly openedAtMs: number;
  readonly barsHeld: number;
  readonly breakevenDone: boolean;
  readonly scaleOutDone: boolean;
  readonly initialRisk: number;
  readonly entryFee: number;
  readonly attributions: readonly SignalAttribution[];
}

export interface SpecTradeRecord {
  readonly id: string;
  readonly symbol: string;
  readonly side: SpecSide;
  readonly entryPrice: number;
  readonly exitPrice: number;
  readonly qty: number;
  readonly pnlPct: number;
  readonly capitalBefore: number;
  readonly capitalAfter: number;
  readonly exitReason: SpecExitReason;
  readonly openedAtMs: number;
  readonly closedAtMs: number;
  readonly attributions: readonly SignalAttribution[];
}

export interface SpecSymbolState {
  readonly symbol: string;
  capital: number;
  peakCapital: number;
  quarantined: boolean;
  dailyLossUsd: number;
  dailyDayKey: string;
  tradesToday: number;
  position: SpecPosition | null;
  trades: SpecTradeRecord[];
  reweighFactors: Record<string, number>;
}

export interface SpecSummary {
  readonly specId: string;
  readonly capital: number;
  readonly equity: number;
  readonly openSymbols: number;
  readonly quarantinedSymbols: number;
  readonly evaluated: number;
}

export interface StrategySupervisorOptions {
  readonly specs: readonly StrategySpec[];
  readonly exchange: string;
  readonly timeframe: string;
  readonly intervalMs: number;
  /** Max iterations; 0 = run until interrupted. */
  readonly iterations: number;
  /** Candle lookback fetched per symbol per evaluation (default 210). */
  readonly lookback?: number;
  /** Directory for per-spec state JSON files. */
  readonly stateDir: string;
  /** Print one summary line per spec per iteration (default true). */
  readonly verbose?: boolean;
  /** Parallel symbol evaluations per spec (default 4). */
  readonly maxConcurrency?: number;
  /** Default starting capital for symbols without persisted state (200). */
  readonly initialCapital?: number;
  /** Extra rule modules loaded before specs compile. */
  readonly ruleModules?: readonly string[];
}

export interface SpecRuntimeResult {
  readonly summaries: readonly SpecSummary[];
  readonly iterations: number;
  /** Per-rule expectancy across all specs (final snapshot). */
  readonly attribution: ReturnType<typeof aggregateRuleAttribution>;
}

export class StrategySupervisorError {
  readonly _tag = "StrategySupervisorError";
  constructor(readonly message: string) {}
}

export interface StrategySupervisorService {
  readonly run: (
    options: StrategySupervisorOptions,
  ) => Effect.Effect<
    SpecRuntimeResult,
    MarketDataError | SpecError | PlatformError,
    MarketDataGatewayService | FileSystem.FileSystem
  >;
}

export const StrategySupervisor =
  Context.Service<StrategySupervisorService>("StrategySupervisor");

// ---------------------------------------------------------------------------
// State (de)serialization
// ---------------------------------------------------------------------------

const AttributionSchema = S.Struct({
  ruleId: S.String,
  weightShare: S.Number,
  alignment: S.Literals(["aligned", "against", "hold"]),
});

const PositionSchema = S.Struct({
  side: S.Literals(["LONG", "SHORT"]),
  entryPrice: S.Number,
  qty: S.Number,
  stop: S.Number,
  target: S.Number,
  openedAtMs: S.Number,
  barsHeld: S.Number,
  breakevenDone: S.Boolean,
  scaleOutDone: S.Boolean,
  initialRisk: S.Number,
  entryFee: S.Number,
  attributions: S.Array(AttributionSchema),
});

const TradeSchema = S.Struct({
  id: S.String,
  symbol: S.String,
  side: S.Literals(["LONG", "SHORT"]),
  entryPrice: S.Number,
  exitPrice: S.Number,
  qty: S.Number,
  pnlPct: S.Number,
  capitalBefore: S.Number,
  capitalAfter: S.Number,
  exitReason: S.Literals(["target", "stop", "hold-time", "risk-close"]),
  openedAtMs: S.Number,
  closedAtMs: S.Number,
  attributions: S.Array(AttributionSchema),
});

const SymbolStateSchema = S.Struct({
  symbol: S.String,
  capital: S.Number,
  peakCapital: S.Number,
  quarantined: S.Boolean,
  dailyLossUsd: S.Number,
  dailyDayKey: S.String,
  tradesToday: S.Number,
  position: S.Union([PositionSchema, S.Null]),
  trades: S.Array(TradeSchema),
  reweighFactors: S.Record(S.String, S.Number),
});

const SpecStateFileSchema = S.Struct({
  fingerprint: S.Struct({
    exchange: S.String,
    timeframe: S.String,
  }),
  symbols: S.Array(SymbolStateSchema),
});

type DecodedSymbolState = S.Schema.Type<typeof SymbolStateSchema>;

// ---------------------------------------------------------------------------
// Engine internals
// ---------------------------------------------------------------------------

const DEFAULT_INITIAL_CAPITAL = 200;
const DEFAULT_LOOKBACK = 210;

function nowMs(): number {
  return Date.now();
}

function dayKey(ms: number): string {
  return new Date(ms).toISOString().slice(0, 10);
}

function clamp01(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

function safeIdPart(value: string): string {
  return value.replace(/[^A-Za-z0-9._-]/g, "_");
}

function freshSymbolState(
  symbol: string,
  initialCapital: number,
  ms: number,
): SpecSymbolState {
  return {
    symbol,
    capital: initialCapital,
    peakCapital: initialCapital,
    quarantined: false,
    dailyLossUsd: 0,
    dailyDayKey: dayKey(ms),
    tradesToday: 0,
    position: null,
    trades: [],
    reweighFactors: {},
  };
}

interface SpecRuntime {
  spec: StrategySpec;
  config: ComposerConfig;
  registry: RuleRegistry;
  readonly states: Map<string, SpecSymbolState>;
  readonly attributionTrades: AttributionTrade[];
}

/** Build the runtime for one spec: compile config, gate registry, states. */
export function makeSpecRuntime(
  spec: StrategySpec,
  moduleRegistry: RuleRegistry,
): SpecRuntime {
  return {
    spec,
    config: compileStrategySpec(spec),
    registry: buildSpecRegistry(spec, moduleRegistry),
    states: new Map(),
    attributionTrades: [],
  };
}

/** Apply attribution factors onto a spec's rule weights (pure copy). */
export function reweightedSpec(
  spec: StrategySpec,
  factors: Record<string, number>,
): StrategySpec {
  if (!spec.autoReweight) return spec;
  const reweighted = applyReweightFactors(spec.rules, factors);
  return { ...spec, rules: reweighted };
}

interface RuntimeEvalContext {
  readonly exchange: string;
  readonly lookback: number;
  readonly initialCapital: number;
}

/**
 * One bar per symbol: update state via exits, then evaluate entries.
 * Returns state mutations on the shared runtime (imperative by design —
 * each symbol's state is exclusively owned by this evaluation).
 */
function evaluateSymbol(
  runtime: SpecRuntime,
  symbol: string,
  candles: readonly CandleLike[],
  ctx: RuntimeEvalContext,
): void {
  const spec = runtime.spec;
  const exec = spec.execution;
  const state =
    runtime.states.get(symbol) ??
    freshSymbolState(symbol, ctx.initialCapital, nowMs());
  runtime.states.set(symbol, state);

  // Daily counters reset on UTC day rollover.
  const today = dayKey(nowMs());
  if (state.dailyDayKey !== today) {
    state.dailyLossUsd = 0;
    state.tradesToday = 0;
    state.dailyDayKey = today;
  }

  // Mark peak capital for drawdown math.
  if (state.capital > state.peakCapital) state.peakCapital = state.capital;

  if (!passesRiskProtocol(runtime, symbol, candles, state)) return;

  // --- exit management (stops/targets stay active even when blocked) --------
  manageOpenPositionOnBar(runtime, symbol, candles, state);

  if (state.position !== null || state.quarantined) return;
  if (!passesEntryLimits(spec, state)) return;

  const signal = evaluateEntrySignal(runtime, candles);
  if (signal === null) return;

  const atr = calculateATR(candles, 14);
  const lastClose = candles[candles.length - 1].close;
  if (lastClose <= 0) return;
  if (exec.minAtrPct > 0) {
    if (atr === null || atr <= 0) return;
    if ((atr / lastClose) * 100 < exec.minAtrPct) return;
  }

  openPosition(runtime, candles, state, signal, exec, atr);
}

/**
 * Drawdown/daily-loss/limits gate. Returns false when entries must be skipped;
 * mutates quarantine state and realizes the open position on a drawdown breach.
 */
function passesRiskProtocol(
  runtime: SpecRuntime,
  symbol: string,
  candles: readonly CandleLike[],
  state: SpecSymbolState,
): boolean {
  const maxDrawdownPct = runtime.spec.risk.maxDrawdownPct;
  const drawdownPct =
    state.peakCapital > 0
      ? ((state.peakCapital - state.capital) / state.peakCapital) * 100
      : 0;
  if (drawdownPct > maxDrawdownPct && !state.quarantined) {
    if (state.position === null) {
      state.quarantined = true;
    } else {
      // Realize the position first; only quarantine when still breached.
      closePosition(
        runtime,
        symbol,
        state,
        candles[candles.length - 1].close,
        0,
        "risk-close",
      );
      if (
        ((state.peakCapital - state.capital) / state.peakCapital) * 100 >
        maxDrawdownPct
      ) {
        state.quarantined = true;
      }
    }
  }
  return !state.quarantined;
}

/** Per-day trade cap, minimum capital and daily-loss gate. */
function passesEntryLimits(
  spec: StrategySpec,
  state: SpecSymbolState,
): boolean {
  const dailyLossPct =
    state.capital > 0 ? (state.dailyLossUsd / state.capital) * 100 : 0;
  return (
    state.tradesToday < spec.risk.maxTradesPerDay &&
    state.capital >= spec.risk.minCapital &&
    dailyLossPct <= spec.risk.maxDailyLossPct
  );
}

/** Compose one entry signal, honoring the spec's regime gate and confidence floor. */
function evaluateEntrySignal(
  runtime: SpecRuntime,
  candles: readonly CandleLike[],
): ScalpingSignal | null {
  const conditionContext = buildConditionContext(
    candles,
    runtime.config.thresholds,
  );
  if (!evaluateConditions(runtime.spec.regimeGate, conditionContext)) {
    return null;
  }
  setSpecBarContext(conditionContext);
  let signal: ScalpingSignal | null;
  try {
    signal = composeSignal(
      ohlcvFrom(candles),
      syntheticOrderBook(candles),
      runtime.config,
      runtime.registry,
    );
  } finally {
    setSpecBarContext(null);
  }
  if (signal === null || signal.direction === "hold") return null;
  if (signal.confidence < runtime.spec.minConfidence) return null;
  return signal;
}

function manageOpenPositionOnBar(
  runtime: SpecRuntime,
  symbol: string,
  candles: readonly CandleLike[],
  state: SpecSymbolState,
): void {
  const position = state.position;
  if (position === null) return;
  const bar = candles[candles.length - 1];
  const updated: SpecPosition = {
    ...position,
    barsHeld: position.barsHeld + 1,
  };

  const isLong = position.side === "LONG";

  // Out-of-bar sequencing: check stop first (pessimistic), then target.
  const stopHit = isLong ? bar.low <= position.stop : bar.high >= position.stop;
  const targetHit = isLong
    ? bar.high >= position.target
    : bar.low <= position.target;

  if (stopHit) {
    closePosition(runtime, symbol, state, position.stop, 0, "stop");
    return;
  }
  if (targetHit) {
    closePosition(runtime, symbol, state, position.target, 0, "target");
    return;
  }

  // Hold-time discipline.
  if (
    runtime.spec.execution.maxBarsInTrade > 0 &&
    updated.barsHeld >= runtime.spec.execution.maxBarsInTrade
  ) {
    closePosition(runtime, symbol, state, bar.close, 0, "hold-time");
    return;
  }

  // Breakeven at R.
  const favorable = isLong
    ? bar.close - position.entryPrice
    : position.entryPrice - bar.close;
  const rMultiple =
    position.initialRisk > 0 ? favorable / position.initialRisk : 0;
  const breakevenAtR = runtime.spec.execution.breakevenAtR;
  const breakevenDone =
    updated.breakevenDone || (breakevenAtR > 0 && rMultiple >= breakevenAtR);

  // Trailing stop after breakeven (trailingStopAtrMultiplier over ATR).
  let stop = updated.stop;
  if (breakevenDone && !updated.breakevenDone) {
    stop = position.entryPrice;
  }
  const trailMult = runtime.spec.execution.trailingStopAtrMultiplier;
  if (breakevenDone && trailMult > 0) {
    const atr = calculateATR(candles, 14);
    if (atr !== null && atr > 0) {
      stop = isLong
        ? Math.max(stop, bar.close - trailMult * atr)
        : Math.min(stop, bar.close + trailMult * atr);
    }
  }

  state.position = {
    ...updated,
    breakevenDone,
    stop,
  };
}

// --- order construction ------------------------------------------------------

function ohlcvFrom(candles: readonly CandleLike[]): OHLCVInput {
  return {
    exchange: "synthetic",
    symbol: "spec",
    timeframe: "spec",
    candles: [...candles],
    fundingRates: undefined,
  };
}

function syntheticOrderBook(
  candles: readonly CandleLike[],
): OrderBookMetricsInput {
  const last = candles[candles.length - 1];
  return {
    exchange: "synthetic",
    symbol: "spec",
    spread: 0,
    spreadPercent: 0,
    bidDepth: 0,
    askDepth: 0,
    imbalance: 0,
    midPrice: last.close,
    timestamp: new Date(nowMs()),
  };
}

function positionAttributions(
  signal: ScalpingSignal,
): readonly SignalAttribution[] {
  const directional = signal.components.filter(
    (component) => component.signal !== "hold" && component.weight > 0,
  );
  const weightSum = directional.reduce(
    (sum, component) => sum + component.weight,
    0,
  );
  return directional.map((component) => ({
    ruleId: component.name,
    weightShare:
      weightSum > 0
        ? component.weight / weightSum
        : 1 / Math.max(1, directional.length),
    alignment: component.signal === signal.direction ? "aligned" : "against",
  }));
}

function openPosition(
  runtime: SpecRuntime,
  candles: readonly CandleLike[],
  state: SpecSymbolState,
  signal: ScalpingSignal,
  exec: StrategySpec["execution"],
  atr: number | null,
): void {
  const isLong = signal.direction === "buy";
  const bar = candles[candles.length - 1];
  const slip = exec.slippageBps / 10_000;
  const entryPrice = isLong ? bar.close * (1 + slip) : bar.close * (1 - slip);

  let stopDistance: number;
  if (exec.useAtrStops && atr !== null && atr > 0) {
    stopDistance = exec.atrStopMultiplier * atr;
  } else {
    stopDistance = (exec.stopLossPct / 100) * entryPrice;
  }
  const stop = isLong ? entryPrice - stopDistance : entryPrice + stopDistance;

  let takeDistance: number;
  if (exec.atrRiskReward > 0) {
    takeDistance = exec.atrRiskReward * stopDistance;
  } else if (exec.useAtrStops && atr !== null && atr > 0) {
    takeDistance = exec.atrTakeProfitMultiplier * atr;
  } else {
    takeDistance = (exec.takeProfitPct / 100) * entryPrice;
  }
  const target = isLong ? entryPrice + takeDistance : entryPrice - takeDistance;

  // Sizing: notional = capital * positionSizePct%, capped by risk per trade
  // and the position-size cap; below-minimum capital blocks the entry.
  let notional = (state.capital * exec.positionSizePct) / 100;
  if (exec.riskPerTradePct > 0 && stopDistance > 0) {
    const maxNotionalByRisk =
      ((state.capital * exec.riskPerTradePct) / 100 / stopDistance) *
      entryPrice;
    notional = Math.min(notional, maxNotionalByRisk);
  }
  const maxNotional = (state.capital * maxPositionSizePct(runtime)) / 100;
  notional = Math.min(notional, maxNotional);
  const qty = entryPrice > 0 ? notional / entryPrice : 0;
  if (notional < 1 || qty <= 0) return;

  const entryFee = (notional * exec.feePct) / 100;
  state.capital -= entryFee;
  state.position = {
    side: isLong ? "LONG" : "SHORT",
    entryPrice,
    qty,
    stop,
    target,
    openedAtMs: bar.timestamp.getTime(),
    barsHeld: 0,
    breakevenDone: false,
    scaleOutDone: exec.scaleOutPct <= 0,
    initialRisk: stopDistance,
    entryFee,
    attributions: positionAttributions(signal),
  };
  state.tradesToday += 1;
}

function maxPositionSizePct(runtime: SpecRuntime): number {
  return runtime.spec.risk.maxPositionSizePct;
}

function closePosition(
  runtime: SpecRuntime,
  symbol: string,
  state: SpecSymbolState,
  rawExitPrice: number,
  extraSlipFraction: number,
  reason: SpecExitReason,
): void {
  const position = state.position;
  if (position === null) return;
  const exec = runtime.spec.execution;
  const isLong = position.side === "LONG";
  const slip = exec.slippageBps / 10_000 + extraSlipFraction;
  const exitPrice = isLong
    ? rawExitPrice * (1 - slip)
    : rawExitPrice * (1 + slip);

  const deltaPct = isLong
    ? ((exitPrice - position.entryPrice) / position.entryPrice) * 100
    : ((position.entryPrice - exitPrice) / position.entryPrice) * 100;
  const entryNotional = position.qty * position.entryPrice;
  const pnlUsd = entryNotional * (deltaPct / 100);
  const exitFee = (position.qty * exitPrice * exec.feePct) / 100;

  const capitalBefore = state.capital;
  state.capital = state.capital + pnlUsd - exitFee;
  if (pnlUsd < 0) state.dailyLossUsd += Math.abs(pnlUsd);
  state.position = null;

  const trade: SpecTradeRecord = {
    id: `${runtime.spec.id}:${symbol}:${position.openedAtMs}`,
    symbol,
    side: position.side,
    entryPrice: position.entryPrice,
    exitPrice,
    qty: position.qty,
    pnlPct: deltaPct,
    capitalBefore,
    capitalAfter: state.capital,
    exitReason: reason,
    openedAtMs: position.openedAtMs,
    closedAtMs: nowMs(),
    attributions: position.attributions,
  };
  state.trades.push(trade);
  if (state.trades.length > 500)
    state.trades.splice(0, state.trades.length - 500);
  runtime.attributionTrades.push({
    pnlPct: deltaPct,
    attributions: position.attributions,
    closedAtMs: trade.closedAtMs,
  });
}

// ---------------------------------------------------------------------------
// Supervision (service implementation)
// ---------------------------------------------------------------------------

function persistRuntime(
  fs: FileSystem.FileSystem,
  runtime: SpecRuntime,
  options: {
    readonly exchange: string;
    readonly timeframe: string;
    readonly stateDir: string;
  },
): Effect.Effect<void, PlatformError> {
  const symbols: DecodedSymbolState[] = [...runtime.states.values()];
  const payload = {
    fingerprint: {
      exchange: options.exchange,
      timeframe: options.timeframe,
    },
    symbols,
  };
  const path = `${options.stateDir}/${safeIdPart(runtime.spec.id)}.json`;
  return fs
    .makeDirectory(options.stateDir, { recursive: true })
    .pipe(
      Effect.ignore,
      Effect.andThen(
        fs.writeFileString(path, JSON.stringify(payload, null, 2)),
      ),
    );
}

/** Load the built-in registry, then any custom rule modules on top. */
function loadRegistry(
  paths: readonly string[] | undefined,
): Effect.Effect<RuleRegistry, SpecError> {
  return Effect.gen(function* () {
    const registry = defaultRuleRegistry();
    if (paths === undefined || paths.length === 0) return registry;
    yield* Effect.tryPromise({
      try: () => loadRuleModules(registry, paths),
      catch: (error) =>
        new SpecError(`rule module load failed: ${String(error)}`),
    });
    return registry;
  });
}

/** Restore per-spec state whose fingerprint matches this run's exchange/timeframe. */
function restoreState(
  fs: FileSystem.FileSystem,
  runtimes: readonly SpecRuntime[],
  options: StrategySupervisorOptions,
): Effect.Effect<void, PlatformError> {
  return Effect.gen(function* () {
    for (const runtime of runtimes) {
      const statePath = `${options.stateDir}/${safeIdPart(runtime.spec.id)}.json`;
      const exists = yield* fs.exists(statePath);
      if (!exists) continue;
      const raw = yield* fs.readFileString(statePath);
      let decoded: typeof SpecStateFileSchema.Type;
      try {
        decoded = S.decodeUnknownSync(SpecStateFileSchema)(JSON.parse(raw));
      } catch {
        continue; // corrupt/unreadable state => start fresh for this spec
      }
      if (
        decoded.fingerprint.exchange !== options.exchange ||
        decoded.fingerprint.timeframe !== options.timeframe
      ) {
        continue;
      }
      for (const symbolState of decoded.symbols as SpecSymbolState[]) {
        runtime.states.set(symbolState.symbol, symbolState);
      }
    }
  });
}

/** Fetch every symbol's candles once for this iteration (shared across specs). */
function fetchBars(
  gateway: MarketDataGatewayService,
  symbols: ReadonlySet<string>,
  options: StrategySupervisorOptions,
  lookback: number,
): Effect.Effect<
  Map<string, readonly CandleLike[]>,
  MarketDataError | SpecError
> {
  return Effect.gen(function* () {
    const bars = new Map<string, readonly CandleLike[]>();
    for (const symbol of symbols) {
      const fetched = yield* gateway.fetchOHLCV(
        options.exchange,
        symbol,
        options.timeframe,
        lookback,
      );
      yield* validateCandlePrices(options.exchange, fetched);
      if (fetched.length > 1) bars.set(symbol, fetched);
    }
    return bars;
  });
}

/** One summary line per spec per iteration. */
function logSummaries(summaries: readonly SpecSummary[]): Effect.Effect<void> {
  return Effect.forEach(summaries, (summary) =>
    Console.log(
      `[spec:${summary.specId}] capital=${summary.capital.toFixed(2)} equity=${summary.equity.toFixed(2)} open=${summary.openSymbols} quarantined=${summary.quarantinedSymbols} evaluated=${summary.evaluated}`,
    ),
  );
}

/** Fold attribution-derived factors into each auto-reweighting spec's next compile. */
function applyAutoReweight(
  runtimes: readonly SpecRuntime[],
  moduleRegistry: RuleRegistry,
): void {
  for (const runtime of runtimes) {
    if (runtime.spec.autoReweight !== true) continue;
    const factors = proposeReweightFactors(
      aggregateRuleAttribution(runtime.attributionTrades),
    );
    runtime.spec = reweightedSpec(runtime.spec, factors);
    runtime.config = compileStrategySpec(runtime.spec);
    runtime.registry = buildSpecRegistry(runtime.spec, moduleRegistry);
  }
}

export const StrategySupervisorLive = Layer.effect(
  StrategySupervisor,
  Effect.sync(() => {
    const service: StrategySupervisorService = {
      run: (options) =>
        Effect.gen(function* () {
          const gateway = yield* MarketDataGateway;
          const fs = yield* FileSystem.FileSystem;
          const moduleRegistry = yield* loadRegistry(options.ruleModules);

          const enabledSpecs = options.specs.filter(
            (spec) => spec.enabled !== false,
          );
          if (enabledSpecs.length === 0) {
            return {
              summaries: [],
              iterations: 0,
              attribution: [],
            } satisfies SpecRuntimeResult;
          }

          // Compile eagerly: an unknown rule id throws before any iteration.
          const runtimes = enabledSpecs.map((spec) =>
            makeSpecRuntime(spec, moduleRegistry),
          );
          yield* restoreState(fs, runtimes, options);

          const symbols = new Set<string>();
          for (const spec of enabledSpecs) {
            for (const symbol of spec.symbols) symbols.add(symbol);
          }
          if (symbols.size === 0) {
            throw new SpecError("no enabled spec defines any symbols");
          }

          const lookback = options.lookback ?? DEFAULT_LOOKBACK;
          const evalCtx: RuntimeEvalContext = {
            exchange: options.exchange,
            lookback,
            initialCapital: options.initialCapital ?? DEFAULT_INITIAL_CAPITAL,
          };

          let iterations = 0;
          let summaries: readonly SpecSummary[] = [];
          let running = true;

          while (running) {
            const startedAt = nowMs();
            const bars = yield* fetchBars(gateway, symbols, options, lookback);

            for (const runtime of runtimes) {
              for (const symbol of runtime.spec.symbols) {
                const candles = bars.get(symbol);
                if (candles === undefined) continue;
                evaluateSymbol(runtime, symbol, candles, evalCtx);
              }
            }

            iterations += 1;
            summaries = runtimes.map((runtime) => summarize(runtime));
            if (options.verbose !== false) yield* logSummaries(summaries);

            // Persist after every iteration (tiny JSON; restart-resumable).
            for (const runtime of runtimes) {
              yield* persistRuntime(fs, runtime, options).pipe(Effect.ignore);
            }
            applyAutoReweight(runtimes, moduleRegistry);

            if (options.iterations > 0 && iterations >= options.iterations) {
              running = false;
              break;
            }
            const elapsed = nowMs() - startedAt;
            if (elapsed < options.intervalMs) {
              yield* Effect.sleep(options.intervalMs - elapsed);
            }
          }

          const attribution = aggregateRuleAttribution(
            runtimes.flatMap((runtime) => runtime.attributionTrades),
          );
          return {
            summaries,
            iterations,
            attribution,
          } satisfies SpecRuntimeResult;
        }),
    };
    return service;
  }),
);

function summarize(runtime: SpecRuntime): SpecSummary {
  let capital = 0;
  let equity = 0;
  let openSymbols = 0;
  let quarantinedSymbols = 0;
  for (const state of runtime.states.values()) {
    capital += state.capital;
    equity += state.capital; // position MtM is realized on the next bar close
    if (state.position !== null) openSymbols += 1;
    if (state.quarantined) quarantinedSymbols += 1;
  }
  return {
    specId: runtime.spec.id,
    capital,
    equity,
    openSymbols,
    quarantinedSymbols,
    evaluated: runtime.states.size,
  };
}

/** Convenience loader: decode specs from parsed JSON values (Effect Schema). */
export function decodeSpecs(
  values: readonly unknown[],
): readonly StrategySpec[] {
  return values.map((value) => S.decodeUnknownSync(StrategySpecSchema)(value));
}

/**
 * Symbol-level drawdown check used by the supervisor's risk protocol; also
 * exported for spec backtests that want to emulate the runtime's protocol.
 */
export function drawdownPctOf(state: {
  readonly peakCapital: number;
  readonly capital: number;
}): number {
  return state.peakCapital > 0
    ? clamp01(
        ((state.peakCapital - state.capital) / state.peakCapital) * 100,
        0,
        Number.MAX_SAFE_INTEGER,
      )
    : 0;
}
