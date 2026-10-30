/**
 * Declarative StrategySpec — the flexible "rules/flows/conditions" layer.
 *
 * A spec is a validated (Effect-Schema) description of an autonomous
 * strategy:
 *
 *   - an arbitrary set of signal rules pulled from the rule registry,
 *     each with its own weight and optional `when` condition gates,
 *   - regime-level condition gates evaluated per bar from causal
 *     indicators (ADX/ATR/Bollinger/time), enabling flows like
 *     "only run momentum rules when ADX > 25",
 *   - execution and risk limits consumed by the spec runtime.
 *
 * `compileStrategySpec` projects a spec into a `ComposerConfig` so all of
 * the existing machinery (composer aggregation, confluence filters,
 * backtests) runs unchanged, and `buildSpecRegistry` produces a per-spec
 * rule registry whose rules carry the spec's `when` gates.
 */

import * as S from "effect/Schema";
import { Effect, Predicate } from "effect";
import { analyzeRegime, defaultComposerConfig } from "../scalping/composer.js";
import type {
  CandleLike,
  ComposerConfig,
  ComposerThresholds,
  ComposerWeights,
} from "../scalping/types.js";
import { RuleRegistry, defaultRuleRegistry } from "./registry.js";

// ---------------------------------------------------------------------------
// Schema
// ---------------------------------------------------------------------------

const num = (d: number) =>
  S.Number.pipe(S.withDecodingDefault(Effect.succeed(d)));
const bool = (d: boolean) =>
  S.Boolean.pipe(S.withDecodingDefault(Effect.succeed(d)));

/** Fields a condition gate may observe for the latest bar (all causal). */
export const ConditionFieldSchema = S.Literals([
  "adx",
  "plusDI",
  "minusDI",
  "atrPct",
  "trendRegime",
  "volatilityRegime",
  "bollingerPercentB",
  "priceChangePct",
  "hourUtc",
  "weekdayUtc",
]);
export type ConditionField = typeof ConditionFieldSchema.Type;

export const ConditionOpSchema = S.Literals(["<", "<=", ">", ">=", "==", "!="]);
export type ConditionOp = typeof ConditionOpSchema.Type;

/**
 * Signal-level condition gate. Numeric fields compare numerically; regime
 * string fields compare with ==/!=.
 */
export const ConditionGateSchema = S.Struct({
  field: ConditionFieldSchema,
  op: ConditionOpSchema,
  value: S.Union([S.Number, S.String]),
});

export type ConditionGate = typeof ConditionGateSchema.Type;

/** One rule activation within a spec. */
export const RuleActivationSchema = S.Struct({
  /** Rule id from the registry (built-in or custom via rule modules). */
  rule: S.String,
  /** Composer weight (normalized over the spec's rules when compiling). */
  weight: num(0),
  /** Per-rule condition gates; all must pass for the rule to contribute. */
  when: S.Array(ConditionGateSchema).pipe(
    S.withDecodingDefault(Effect.succeed([])),
  ),
});

export type RuleActivation = typeof RuleActivationSchema.Type;

/** Risk limits the spec runtime enforces per spec (paper defaults below). */
export const RiskLimitsSchema = S.Struct({
  maxDrawdownPct: num(15),
  maxDailyLossPct: num(5),
  maxPositionSizePct: num(50),
  minCapital: num(5),
  maxTradesPerDay: num(20),
});

export type SpecRiskLimits = typeof RiskLimitsSchema.Type;

/** Engine execution parameters supported by the spec runtime. */
export const ExecutionSpecSchema = S.Struct({
  positionSizePct: num(25),
  riskPerTradePct: num(0),
  maxBarsInTrade: num(0),
  stopLossPct: num(1.5),
  takeProfitPct: num(2.5),
  useAtrStops: bool(true),
  atrStopMultiplier: num(1.5),
  atrTakeProfitMultiplier: num(2.5),
  atrRiskReward: num(0),
  scaleOutAtR: num(0),
  scaleOutPct: num(0),
  breakevenAtR: num(1),
  trailingStopAtrMultiplier: num(0),
  minAtrPct: num(0),
  leverage: num(1),
  feePct: num(0.06),
  slippageBps: num(2),
  entryOrderType: S.Literals(["market", "limit"]).pipe(
    S.withDecodingDefault(Effect.succeed("market" as const)),
  ),
});

export type SpecExecution = typeof ExecutionSpecSchema.Type;

export const StrategySpecSchema = S.Struct({
  id: S.String,
  description: S.String.pipe(S.withDecodingDefault(Effect.succeed(""))),
  enabled: bool(true),
  /** Explicit symbol universe (e.g. "BTC/USDT:USDT"). */
  symbols: S.Array(S.String).pipe(S.withDecodingDefault(Effect.succeed([]))),
  /** Entry-signal rules (registry ids + weights + gates). */
  rules: S.Array(RuleActivationSchema),
  /** Spec-level gate: evaluated per bar; fails => no evaluation that bar. */
  regimeGate: S.Array(ConditionGateSchema).pipe(
    S.withDecodingDefault(Effect.succeed([])),
  ),
  /** Overall entry-confidence floor in (0,1]. */
  minConfidence: num(0.55),
  /** Strategy-level composer threshold overrides (validated keys). */
  thresholds: S.Record(S.String, S.Union([S.Number, S.Boolean, S.String])).pipe(
    S.withDecodingDefault(Effect.succeed({})),
  ),
  execution: ExecutionSpecSchema,
  risk: RiskLimitsSchema,
  /** When true, attribution-derived weight updates are applied to the next compile. */
  autoReweight: bool(false),
});

export type StrategySpec = typeof StrategySpecSchema.Type;

// ---------------------------------------------------------------------------
// Condition evaluation
// ---------------------------------------------------------------------------

export class SpecError extends Error {
  readonly _tag = "SpecError";
}

/** Snapshot of the causal bar context a condition gates on. */
export interface ConditionContext {
  readonly adx: number | null;
  readonly plusDI: number | null;
  readonly minusDI: number | null;
  readonly atrPct: number;
  readonly trendRegime: string;
  readonly volatilityRegime: string;
  readonly bollingerPercentB: number | null;
  readonly priceChangePct: number;
  readonly hourUtc: number;
  readonly weekdayUtc: number;
}

/** Regime fields shared by every condition context, from one causal read. */
function regimeFields(
  candles: readonly CandleLike[],
  thresholds: Pick<
    ComposerThresholds,
    "adxWeakTrend" | "volatilityModeratePct" | "volatilityHighPct"
  >,
) {
  const regime = analyzeRegime(candles, {
    adxWeakTrend: thresholds.adxWeakTrend,
    adxStrongTrend: defaultComposerConfig.thresholds.adxStrongTrend,
    atrMaxPctOfPrice: 0,
    bollingerEntryMinPct: 0,
    bollingerEntryMaxPct: 0,
    volatilityLowPct: defaultComposerConfig.thresholds.volatilityLowPct,
    volatilityModeratePct: thresholds.volatilityModeratePct,
    volatilityHighPct: thresholds.volatilityHighPct,
  });
  return {
    adx: regime?.adx ?? null,
    plusDI: regime?.plusDI ?? null,
    minusDI: regime?.minusDI ?? null,
    atrPct: regime?.atrPct ?? 0,
    trendRegime: regime?.trendRegime ?? "ranging",
    volatilityRegime: regime?.volatilityRegime ?? "low",
    bollingerPercentB: regime?.bollinger?.percentB ?? null,
  };
}

/** Build the condition context causally from the trailing candle window. */
export function buildConditionContext(
  candles: readonly CandleLike[],
  thresholds: Pick<
    ComposerThresholds,
    "adxWeakTrend" | "volatilityModeratePct" | "volatilityHighPct"
  >,
  now = new Date(),
): ConditionContext {
  const last = candles[candles.length - 1];
  const prev = candles[candles.length - 2] as CandleLike | undefined;
  const lastTs = last?.timestamp instanceof Date ? last.timestamp : now;
  const priceChangePct =
    last && prev && prev.close > 0
      ? ((last.close - prev.close) / prev.close) * 100
      : 0;
  return {
    ...regimeFields(candles, thresholds),
    priceChangePct,
    hourUtc: lastTs.getUTCHours(),
    weekdayUtc: lastTs.getUTCDay(),
  };
}

function compareNumeric(
  actual: number,
  op: ConditionOp,
  value: number,
): boolean {
  switch (op) {
    case "<":
      return actual < value;
    case "<=":
      return actual <= value;
    case ">":
      return actual > value;
    case ">=":
      return actual >= value;
    case "==":
      return actual === value;
    case "!=":
      return actual !== value;
  }
}

/** Evaluate a single gate against the condition context. */
export function evaluateCondition(
  gate: ConditionGate,
  ctx: ConditionContext,
): boolean {
  let actual: number | string;
  switch (gate.field) {
    case "adx":
      actual = ctx.adx ?? -1;
      break;
    case "plusDI":
      actual = ctx.plusDI ?? -1;
      break;
    case "minusDI":
      actual = ctx.minusDI ?? -1;
      break;
    case "atrPct":
      actual = ctx.atrPct;
      break;
    case "trendRegime":
      actual = ctx.trendRegime;
      break;
    case "volatilityRegime":
      actual = ctx.volatilityRegime;
      break;
    case "bollingerPercentB":
      actual = ctx.bollingerPercentB ?? -1;
      break;
    case "priceChangePct":
      actual = ctx.priceChangePct;
      break;
    case "hourUtc":
      actual = ctx.hourUtc;
      break;
    case "weekdayUtc":
      actual = ctx.weekdayUtc;
      break;
  }
  if (Predicate.isString(gate.value)) {
    return gate.op === "==" ? actual === gate.value : actual !== gate.value;
  }
  const compareActual = Predicate.isNumber(actual) ? actual : -1;
  const numeric = Number.isFinite(compareActual) ? compareActual : -1;
  return compareNumeric(numeric, gate.op, gate.value);
}

export function evaluateConditions(
  gates: readonly ConditionGate[],
  ctx: ConditionContext,
): boolean {
  return gates.every((gate) => evaluateCondition(gate, ctx));
}

// ---------------------------------------------------------------------------
// Compilation
// ---------------------------------------------------------------------------

/**
 * Project a spec into the composer config the existing pipeline consumes.
 * Only the spec's rules are enabled; their spec weights (when > 0) replace
 * the defaults, normalized by `applyEnabledWeights` inside the composer.
 */
export function compileStrategySpec(
  spec: StrategySpec,
  base: ComposerConfig = defaultComposerConfig,
): ComposerConfig {
  const enabled: Partial<Record<string, boolean>> = {};
  // Custom rule ids are open-ended, so the map is wider than ComposerWeights;
  // the intersection keeps the known keys typed while allowing extras.
  const weights: ComposerWeights & Record<string, number> = {
    ...base.weights,
  };
  let specWeightSum = 0;
  for (const activation of spec.rules) {
    weights[activation.rule] = activation.weight;
    specWeightSum += activation.weight;
  }
  if (specWeightSum > 0) {
    // Rules present in the spec but with weight 0 still emit components;
    // give them a pulse so they remain visible while contributing nothing.
    for (const activation of spec.rules) {
      if (activation.weight <= 0) weights[activation.rule] = 0;
    }
  }
  // Everything not in the spec is off — including rules with no spec weight.
  for (const key of Object.keys(base.weights)) {
    if (!spec.rules.some((r) => r.rule === key)) {
      enabled[key] = false;
      weights[key] = 0;
    }
  }

  const thresholds: ComposerThresholds = {
    ...base.thresholds,
    ...(spec.thresholds as Partial<ComposerThresholds>),
  };
  for (const key of Object.keys(spec.thresholds)) {
    if (!(key in base.thresholds)) {
      throw new SpecError(`unknown threshold "${key}" in spec ${spec.id}`);
    }
  }

  return {
    ...base,
    weights,
    enabled,
    thresholds,
  };
}

/**
 * Per-spec registry: same rules, each gated by its `when` conditions on top
 * of any built-in gate. The rule-level gates read the per-bar context set via
 * `setSpecBarContext` by the runtime before each evaluation.
 */
export function buildSpecRegistry(
  spec: StrategySpec,
  modules = defaultRuleRegistry(),
): RuleRegistry {
  const registry = new RuleRegistry();
  const byId = new Map(modules.list().map((rule) => [rule.id, rule]));
  for (const activation of spec.rules) {
    const rule = byId.get(activation.rule);
    if (!rule) {
      throw new SpecError(
        `unknown rule "${activation.rule}" in spec ${spec.id} — register it via loadRuleModules first`,
      );
    }
    const baseGate = rule.gate;
    registry.register({
      ...rule,
      gate: (thresholds, config) => {
        if (baseGate && !baseGate(thresholds, config)) return false;
        return passesSpecGate(spec, rule.id);
      },
    });
  }
  return registry;
}

// Module-level per-bar context handoff for gated rules. The runtime sets the
// context each bar before composing; spec `when` gates read it. This avoids
// plumbing a context parameter through the composer's stable API.
let barContext: ConditionContext | null = null;

/** Set the condition context used by spec gates for the current bar. */
export function setSpecBarContext(ctx: ConditionContext | null): void {
  barContext = ctx;
}

/** Compile-time switch used by tests and the runtime to gate via context. */
export function passesSpecGate(spec: StrategySpec, ruleId: string): boolean {
  const activation = spec.rules.find((rule) => rule.rule === ruleId);
  if (!activation) return false;
  if (barContext === null) return true;
  return evaluateConditions(activation.when, barContext);
}
