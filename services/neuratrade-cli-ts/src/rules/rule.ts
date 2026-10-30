/**
 * Rule primitives for the flexible signal engine.
 *
 * A rule is an isolated, named evaluator that maps a bar context (candles,
 * order-book metrics, thresholds) to a single `SignalComponent`. The composer
 * is an aggregator over registered rules; custom rule modules register on the
 * same interface so strategies can compose arbitrary rule sets without
 * touching the composer.
 */

import type {
  CandleLike,
  ComposerConfig,
  ComposerThresholds,
  OHLCVInput,
  OrderBookMetricsInput,
  SignalComponent,
} from "../scalping/types.js";

/** Group of the composer pipeline a rule belongs to. */
export type RuleCategory = "market" | "oscillator" | "context";

/** Everything a rule evaluator may need for one bar. */
export interface RuleEvaluationInput {
  readonly candles: readonly CandleLike[];
  readonly obMetrics: OrderBookMetricsInput;
  /** Full OHLCV envelope (carries funding rates when the feed provides them). */
  readonly ohlcv: OHLCVInput;
  /** Effective composer config (enabled weights already normalized). */
  readonly config: ComposerConfig;
  readonly thresholds: ComposerThresholds;
}

/**
 * A registered signal rule. `id` doubles as the weight key in
 * `ComposerWeights` and the enable switch in `ComposerEnabled`, so a rule
 * participates in the composer's weight normalization and enable/disable
 * machinery exactly like the built-in components.
 */
export interface RuleDefinition {
  /** Unique id, e.g. "rsi". Custom rules use namespaced ids ("custom:*"). */
  readonly id: string;
  readonly description?: string;
  /** Composer group the rule runs in; evaluation order follows registration. */
  readonly category: RuleCategory;
  /**
   * Extra activation gate beyond the enabled flag (e.g. `funding` also
   * requires `thresholds.useFunding`). False excludes the rule that bar.
   */
  readonly gate?: (
    thresholds: ComposerThresholds,
    config: ComposerConfig,
  ) => boolean;
  /**
   * When `thresholds.directionalOnly` is set, a produced component that fails
   * this filter aborts evaluation (composer returns no signal). Mirrors the
   * built-in volatility/liquidity filters.
   */
  readonly directionalFilter?: (
    component: SignalComponent,
    thresholds: ComposerThresholds,
  ) => boolean;
  /** Produce the signal component for this bar, or null to abstain. */
  readonly evaluate: (input: RuleEvaluationInput) => SignalComponent | null;
}
