/**
 * Rule-level performance attribution.
 *
 * Every entry signal records which rules fired (and their alignment against
 * the final direction). Attribution aggregates trade outcomes per rule so the
 * runtime can (a) expose which rules actually carry expectancy, and
 * (b) propose dynamic weight updates for the next compile — the feedback loop
 * the hardcoded composer never had.
 */

export interface AttributionRecord {
  /** Rule id that fired on the entry bar. */
  readonly ruleId: string;
  /** Own contribution weight share at entry (rule weight / sum of weights). */
  readonly weightShare: number;
  /** "buy" | "sell" | "hold" — component agrees with the taken direction? */
  readonly alignment: "aligned" | "against" | "hold";
}

export interface AttributionTrade {
  /** Realized pnl in percent of allocated capital (may be null while open). */
  readonly pnlPct: number | null;
  readonly attributions: readonly AttributionRecord[];
  readonly closedAtMs: number;
}

export interface RuleAttribution {
  readonly ruleId: string;
  readonly trades: number;
  readonly alignedTrades: number;
  readonly winRatePct: number;
  readonly pnlPctSum: number;
  /** pnl per aligned trade in pct — rough per-rule expectancy. */
  readonly expectancyPct: number;
}

/** Aggregate trades into per-rule expectancies. */
export function aggregateRuleAttribution(
  trades: readonly AttributionTrade[],
): RuleAttribution[] {
  const acc = new Map<
    string,
    { trades: number; wins: number; pnl: number; aligned: number }
  >();
  for (const trade of trades) {
    if (trade.pnlPct === null) continue;
    const contributing = trade.attributions.filter(
      (a) => a.alignment !== "hold",
    );
    for (const attribution of contributing) {
      const entry = acc.get(attribution.ruleId) ?? {
        trades: 0,
        wins: 0,
        pnl: 0,
        aligned: 0,
      };
      entry.trades += 1;
      entry.aligned += attribution.alignment === "aligned" ? 1 : 0;
      const share = Math.max(0.01, attribution.weightShare);
      const sharePnl = trade.pnlPct * share;
      entry.pnl += sharePnl;
      if (sharePnl > 0) entry.wins += 1;
      acc.set(attribution.ruleId, entry);
    }
  }
  return [...acc.entries()]
    .map(([ruleId, entry]) => ({
      ruleId,
      trades: entry.trades,
      alignedTrades: entry.aligned,
      winRatePct: entry.trades > 0 ? (entry.wins / entry.trades) * 100 : 0,
      pnlPctSum: entry.pnl,
      expectancyPct: entry.aligned > 0 ? entry.pnl / entry.aligned : 0,
    }))
    .sort((a, b) => b.expectancyPct - a.expectancyPct);
}

// ---------------------------------------------------------------------------
// Dynamic re-weighting
// ---------------------------------------------------------------------------

export interface ReweightOptions {
  /** Learning rate applied to the sigmoid-shaped expectancy signal. */
  readonly learningRate?: number;
  /** Hard bounds on the multiplicative update factor. */
  readonly minFactor?: number;
  readonly maxFactor?: number;
  /** Minimum aligned trades before a rule is trusted for re-weighting. */
  readonly minTrades?: number;
}

export const defaultReweightOptions: required<ReweightOptions> = {
  learningRate: 0.25,
  minFactor: 0.25,
  maxFactor: 2,
  minTrades: 10,
} as required<ReweightOptions>;

type required<T> = {
  [K in keyof T]-?: T[K];
};

/**
 * Propose a multiplicative re-weight per rule:
 *
 *   factor = clamp(1 + lr * tanh(expectancy / referencePct))
 *
 * where `referencePct` scales expectancy into a bounded signal (defaults to
 * 1.0). High-expectancy rules are scaled up, negative-expectancy rules are
 * scaled down; rules without enough evidence keep factor 1.
 */
export function proposeReweightFactors(
  attributions: readonly RuleAttribution[],
  options: ReweightOptions = {},
) {
  const opts = {
    ...defaultReweightOptions,
    ...options,
  } as required<ReweightOptions>;
  const factors: Record<string, number> = {};
  for (const attribution of attributions) {
    if (attribution.alignedTrades < opts.minTrades) {
      factors[attribution.ruleId] = 1;
      continue;
    }
    const signal = Math.tanh(attribution.expectancyPct / 1.0);
    const factor = 1 + opts.learningRate * signal;
    factors[attribution.ruleId] = Math.min(
      opts.maxFactor,
      Math.max(opts.minFactor, factor),
    );
  }
  return factors;
}

/**
 * Apply re-weight factors to a spec's rule activations, preserving every
 * other field (e.g. `when` condition gates) of each activation untouched.
 */
export function applyReweightFactors<
  T extends { rule: string; weight: number },
>(rules: readonly T[], factors: Record<string, number>): T[] {
  return rules.map((r) => ({
    ...r,
    weight: r.weight * (factors[r.rule] ?? 1),
  }));
}
