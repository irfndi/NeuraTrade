/**
 * Rule registry: named lookup + registration of signal rules.
 *
 * The built-in registry wraps the composer's existing component builders in
 * canonical composer order, so the composer's output is unchanged
 * (behavior-preserving dispatch). User modules can register additional rules
 * via `loadRuleModules` (modules export `RuleDefinition[]` or a `RuleDefinition`
 * default) — anything registered flows through the same composer aggregation,
 * weight normalization and confluence filters as built-ins.
 */

import {
  buildConnorsRsi2Component,
  buildEmaPullbackComponent,
  buildFundingComponent,
  buildImbalanceComponent,
  buildLiquidityComponent,
  buildRegimeComponent,
  buildRsiComponent,
  buildRsiPullbackComponent,
  passesLiquidityFilter,
  passesVolatilityFilter,
  buildSpreadComponent,
  buildTrendComponent,
  buildVolatilityComponent,
} from "../scalping/composer.js";
import type { ComposerThresholds, SignalComponent } from "../scalping/types.js";
import type { RuleDefinition } from "./rule.js";

export type {
  RuleDefinition,
  RuleEvaluationInput,
  RuleCategory,
} from "./rule.js";

/** Adapt a built-in component filter to the registry's directional gate. */
function passesThresholdFilter(
  pass: (component: SignalComponent, thresholds: ComposerThresholds) => boolean,
): NonNullable<RuleDefinition["directionalFilter"]> {
  return (component, thresholds) => pass(component, thresholds);
}

/** Build a registry preloaded with the composer's built-in rules. */
export function defaultRuleRegistry(): RuleRegistry {
  const registry = new RuleRegistry();
  registry.registerAll([
    {
      id: "spread",
      category: "market",
      description: "Top-of-book spread component.",
      evaluate: (input) =>
        buildSpreadComponent(
          input.obMetrics,
          input.config.weights.spread,
          input.thresholds,
        ),
    },
    {
      id: "imbalance",
      category: "market",
      description: "Order-book imbalance component.",
      evaluate: (input) =>
        buildImbalanceComponent(
          input.obMetrics,
          input.config.weights.imbalance,
          input.thresholds,
        ),
    },
    {
      id: "volatility",
      category: "market",
      description: "Candle volatility component (directional-gated).",
      directionalFilter: passesThresholdFilter(passesVolatilityFilter),
      evaluate: (input) =>
        buildVolatilityComponent(
          input.candles,
          input.config.weights.volatility,
          input.thresholds,
        ),
    },
    {
      id: "trend",
      category: "market",
      description: "EMA fast/slow trend component.",
      evaluate: (input) =>
        buildTrendComponent(
          input.candles,
          input.config.weights.trend,
          input.thresholds,
        ),
    },
    {
      id: "liquidity",
      category: "market",
      description: "Order-book liquidity component (directional-gated).",
      directionalFilter: passesThresholdFilter(passesLiquidityFilter),
      evaluate: (input) =>
        buildLiquidityComponent(
          input.obMetrics,
          input.config.weights.liquidity,
          input.thresholds,
        ),
    },
    {
      id: "rsi",
      category: "oscillator",
      description: "RSI oscillator component.",
      evaluate: (input) =>
        buildRsiComponent(
          input.candles,
          input.config.weights.rsi,
          input.thresholds,
        ),
    },
    {
      id: "connorsRsi2",
      category: "oscillator",
      description: "Connors RSI(2) mean-reversion component.",
      evaluate: (input) =>
        buildConnorsRsi2Component(
          input.candles,
          input.config.weights.connorsRsi2,
          input.thresholds,
        ),
    },
    {
      id: "rsiPullback",
      category: "oscillator",
      description: "RSI pullback component.",
      evaluate: (input) =>
        buildRsiPullbackComponent(
          input.candles,
          input.config.weights.rsiPullback ?? 0,
          input.thresholds,
        ),
    },
    {
      id: "emaPullback",
      category: "oscillator",
      description: "EMA pullback component.",
      evaluate: (input) =>
        buildEmaPullbackComponent(
          input.candles,
          input.config.weights.emaPullback ?? 0,
          input.thresholds,
        ),
    },
    {
      id: "regime",
      category: "context",
      description: "ADX/DI + ATR/Volatility regime component.",
      evaluate: (input) =>
        buildRegimeComponent(
          input.candles,
          input.obMetrics,
          input.config.weights.regime,
          input.thresholds,
        ),
    },
    {
      id: "funding",
      category: "context",
      description: "Funding-rate bias component (needs useFunding).",
      gate: (thresholds) => thresholds.useFunding === true,
      evaluate: (input) =>
        buildFundingComponent(
          input.candles,
          input.obMetrics,
          input.config.weights.funding,
          input.thresholds,
          input.ohlcv.fundingRates,
        ),
    },
  ]);
  return registry;
}

export class RuleRegistry {
  private readonly rules = new Map<string, RuleDefinition>();

  /** Register a rule; throws on duplicate ids to keep dispatch deterministic. */
  register(rule: RuleDefinition): void {
    if (this.rules.has(rule.id)) {
      throw new Error(`rule "${rule.id}" is already registered`);
    }
    this.rules.set(rule.id, rule);
  }

  registerAll(rules: readonly RuleDefinition[]): void {
    for (const rule of rules) this.register(rule);
  }

  get(id: string): RuleDefinition | undefined {
    return this.rules.get(id);
  }

  has(id: string): boolean {
    return this.rules.has(id);
  }

  /** Rules in registration order (composer dispatch relies on this order). */
  list(): readonly RuleDefinition[] {
    return [...this.rules.values()];
  }

  ids(): readonly string[] {
    return [...this.rules.keys()];
  }
}

/**
 * A custom rule module's exports: either `rules` (array), `default`, or any
 * named export, each a RuleDefinition or an array of them.
 *
 * shortcut: values are typed, not schema-decoded, because a module is local
 * operator-supplied code loaded via an explicit `--rule-module` flag, not
 * remote input; a malformed export fails loudly at register/compose time.
 */
type RuleModuleExports = Readonly<
  Record<string, RuleDefinition | RuleDefinition[] | undefined>
>;

/** Collect every RuleDefinition a module namespace contributes. */
function rulesFromModule(
  module: RuleModuleExports,
  spec: string,
): readonly RuleDefinition[] {
  const rules: RuleDefinition[] = [];
  const collect = (
    candidate: RuleDefinition | RuleDefinition[] | undefined,
  ) => {
    if (candidate === undefined) return;
    if (Array.isArray(candidate)) {
      for (const rule of candidate) rules.push(rule);
      return;
    }
    rules.push(candidate);
  };
  collect(module.rules);
  collect(module.default);
  for (const [key, value] of Object.entries(module)) {
    if (key === "rules" || key === "default") continue;
    collect(value);
  }
  if (rules.length === 0) {
    throw new Error(`rule module "${spec}" exported no RuleDefinitions`);
  }
  return rules;
}

/**
 * Dynamically load custom rule modules. Each path is imported; modules may
 * export `rules` (array), `default` (RuleDefinition, RuleDefinition[] or
 * { rules }), or named `RuleDefinition` exports.
 */
export async function loadRuleModules(
  registry: RuleRegistry,
  paths: readonly string[],
): Promise<void> {
  for (const spec of paths) {
    const module: RuleModuleExports = await import(spec);
    for (const rule of rulesFromModule(module, spec)) registry.register(rule);
  }
}
