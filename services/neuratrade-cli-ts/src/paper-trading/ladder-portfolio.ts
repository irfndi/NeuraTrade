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
