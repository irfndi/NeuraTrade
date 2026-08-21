import { Decimal, money } from "../utils/money.js";
import type {
  LadderPaperPortfolioMember,
  LadderPaperPortfolioSummary,
} from "./types.js";

export interface LadderPortfolioEntry {
  readonly key: string;
  /** Existing scan allocation. Missing/zero weights are treated equally. */
  readonly allocatedWeight?: number;
}

/**
 * Normalize watchlist weights into bounded capital partitions.
 *
 * The result is deliberately Decimal-based: a $50 watchlist with two equal
 * symbols gets two $25 partitions, never two independent $50 ledgers. The
 * allocation is conservative when a legacy watchlist has no weights.
 */
export function allocateLadderPortfolioCapital(
  entries: readonly LadderPortfolioEntry[],
  totalCapital: number | string | Decimal,
): ReadonlyMap<string, Decimal> {
  const capital = money(totalCapital);
  if (entries.length === 0 || capital.lessThanOrEqualTo(0)) return new Map();

  const weights = entries.map((entry) => {
    const weight = entry.allocatedWeight;
    return Number.isFinite(weight) && (weight ?? 0) > 0
      ? money(weight as number)
      : money(1);
  });
  const totalWeight = weights.reduce(
    (sum, weight) => sum.plus(weight),
    money(0),
  );
  const allocations = new Map<string, Decimal>();
  entries.forEach((entry, index) => {
    allocations.set(entry.key, capital.times(weights[index]).div(totalWeight));
  });
  return allocations;
}

/** One member's rebalance outcome: what it should own and how to size toward it. */
export interface LadderRebalancePlan {
  readonly targetAllocation: Decimal;
  /**
   * maxPositionPct for the next iteration: target allocation as a percent of
   * the member's CURRENT capital, clamped to [0, 100]. A member above target
   * sizes down; a member below target can only size up to its own capital.
   */
  readonly positionPct: number;
}

/**
 * Recompute per-symbol allocations from LIVE member equity so account-level
 * compounding actually flows: winners' profits raise everyone's targets,
 * losers' shrinkage lowers theirs. Static startup partitions never move
 * capital between symbols; this does.
 *
 * Returns an empty map (caller keeps static partitions) unless every entry
 * has known live capital and the total is positive — partial coverage would
 * silently mis-scale the members it does know.
 */
export function planLadderPortfolioRebalance(
  entries: readonly LadderPortfolioEntry[],
  liveCapital: ReadonlyMap<string, Decimal | number>,
): ReadonlyMap<string, LadderRebalancePlan> {
  if (entries.length === 0) return new Map();
  const covered = entries.every((entry) => liveCapital.has(entry.key));
  if (!covered) return new Map();

  const weights = entries.map((entry) => {
    const weight = entry.allocatedWeight;
    return Number.isFinite(weight) && (weight ?? 0) > 0
      ? money(weight as number)
      : money(1);
  });
  const totalWeight = weights.reduce(
    (sum, weight) => sum.plus(weight),
    money(0),
  );
  const totalEquity = entries.reduce(
    (sum, entry) => sum.plus(money(liveCapital.get(entry.key) ?? 0)),
    money(0),
  );
  if (totalWeight.lessThanOrEqualTo(0) || totalEquity.lessThanOrEqualTo(0)) {
    return new Map();
  }

  const plans = new Map<string, LadderRebalancePlan>();
  entries.forEach((entry, index) => {
    const targetAllocation = totalEquity.times(weights[index]).div(totalWeight);
    const current = money(liveCapital.get(entry.key) ?? 0);
    const fraction = current.greaterThan(0)
      ? targetAllocation.div(current).toNumber()
      : 1;
    plans.set(entry.key, {
      targetAllocation,
      positionPct: Math.min(100, Math.max(0, fraction * 100)),
    });
  });
  return plans;
}

/** Aggregate member partitions into the account-level realized/equity mark. */
export function summarizeLadderPortfolio(
  portfolioId: string,
  exchange: string,
  timeframe: string,
  initialCapital: Decimal,
  members: readonly LadderPaperPortfolioMember[],
  previousPeakEquity: Decimal = money(0),
): LadderPaperPortfolioSummary {
  const active = members.filter((member) => member.active);
  const capital = active.reduce(
    (sum, member) => sum.plus(member.capital),
    money(0),
  );
  const equity = active.reduce(
    (sum, member) => sum.plus(member.equity),
    money(0),
  );
  const unrealizedPnl = active.reduce(
    (sum, member) => sum.plus(member.unrealizedPnl),
    money(0),
  );
  return {
    portfolioId,
    exchange,
    timeframe,
    initialCapital,
    capital,
    equity,
    peakEquity: Decimal.max(previousPeakEquity, equity),
    unrealizedPnl,
    activeSymbols: active.length,
    updatedAt: new Date(),
  };
}
