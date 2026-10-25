/**
 * Autoresearch keep-guards + claim bars.
 *
 * v1 (CLAIMED 2026-09-06): log-ret > 0, WR ≥ 48, DD ≤ 15, trades ≥ 4, expectancy > 0
 *   → champion medLogRet≈0.062, WR≈55%, DD≈10.2%
 *
 * v2: raise the floor so v1 does not re-claim; paper/demo keep soaking v1 knobs.
 */
export const GOAL_VERSION = "v2" as const;

/** Minimum quality for KEEP (guardsOk). */
export const KEEP_GUARDS = {
  minMedianLogReturn: 0, // must be strictly >
  minWinRatePct: 52,
  maxMedianDrawdownPct: 12,
  minTradesPerSymMonth: 4,
  minExpectancyPct: 0.0002, // must be strictly >; a claim needs 0.001, so a
  // screen/confirm that admits +0.0004 can never claim — it only burns the
  // confirm budget on a candidate already known to fail CLAIM_BARS.
} as const;

/**
 * Claim when ALL true on a confirm-phase KEEP.
 * Strictly harder than v1 champion so search continues overnight.
 */
export const CLAIM_BARS = {
  minMedianLogReturn: 0.08,
  minWinRatePct: 55,
  // Was 10; overnight stuck at ~10.7 DD with 0.073 log-ret. 11 keeps risk
  // tight vs v1(15) while unblocking near-miss confirms.
  maxMedianDrawdownPct: 11,
  minTradesPerSymMonth: 4,
  minExpectancyPct: 0.001,
} as const;

export type GuardInput = {
  readonly medianLogReturn: number;
  readonly winRatePct: number;
  readonly medianDrawdownPct: number;
  readonly tradesPerSymMonth: number;
  readonly expectancyPct: number;
};

export interface GuardCheckResult {
  readonly ok: boolean;
  readonly reason: string;
}

export function checkKeepGuards(input: GuardInput): GuardCheckResult {
  const g = KEEP_GUARDS;
  const guards: string[] = [];
  if (!(input.medianLogReturn > g.minMedianLogReturn)) {
    guards.push("log_return_nonpositive");
  }
  if (!(input.winRatePct >= g.minWinRatePct)) {
    guards.push(`winrate_below_${g.minWinRatePct}`);
  }
  if (!(input.medianDrawdownPct <= g.maxMedianDrawdownPct)) {
    guards.push(`drawdown_above_${g.maxMedianDrawdownPct}`);
  }
  if (!(input.tradesPerSymMonth >= g.minTradesPerSymMonth)) {
    guards.push(`throughput_below_${g.minTradesPerSymMonth}`);
  }
  if (!(input.expectancyPct > g.minExpectancyPct)) {
    guards.push("expectancy_nonpositive");
  }
  return {
    ok: guards.length === 0,
    reason: guards.length === 0 ? "ok" : guards.join(","),
  };
}

export function meetsClaimBars(input: GuardInput): boolean {
  const c = CLAIM_BARS;
  return (
    input.medianLogReturn >= c.minMedianLogReturn &&
    input.winRatePct >= c.minWinRatePct &&
    input.medianDrawdownPct <= c.maxMedianDrawdownPct &&
    input.tradesPerSymMonth >= c.minTradesPerSymMonth &&
    input.expectancyPct >= c.minExpectancyPct
  );
}
