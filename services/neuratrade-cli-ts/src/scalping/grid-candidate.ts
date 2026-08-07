export const VALIDATED_BTC_GRID_CANDIDATE = {
  exchange: "bitget-futures",
  symbol: "BTC/USDT:USDT",
  timeframe: "15m",
  productType: "USDT-FUTURES",
  gridStepPct: 1,
  gridMaxGrids: 1.5,
  // Re-locked 2026-08-07: pause 24 strictly dominates the prior pause-36 lock
  // under the pooled stress-LB protocol (same 5-seed sequence: confLB
  // +0.00367 vs +0.00227, stressLB +0.00422 vs +0.00235, ret +10.10% vs
  // +8.50%, dd 10.76% vs 12.06%; identical ~7-month cohort clock).
  gridPauseAfterLossBars: 24,
  feePct: 0.02,
  slippageBps: 1,
  trendFilterPeriod: 0,
  onlyWithTrend: false,
  targetRatio: 3,
  chopGateAdx: 28,
  leverage: 1,
  maxPositionSizePct: 50,
  maxDrawdownPct: 5,
  maxDailyLossPct: 2,
} as const;

// SOL cohort candidate (2026-08-07, gate-scored 15m fast space, pooled
// stress-LB): best-margin SOL config — windows 53.8%, ret +6.86%, dd 13.39%,
// OOS 39 (~8 fills/mo), confLB +0.00133, stressLB +0.00325. Adds ~8 fills/mo
// to the cohort clock (BTC+SOL ≈ 15/mo → 50 fills ≈ 3.3 months).
export const VALIDATED_SOL_GRID_CANDIDATE = {
  exchange: "bitget-futures",
  symbol: "SOL/USDT:USDT",
  timeframe: "15m",
  productType: "USDT-FUTURES",
  gridStepPct: 1.25,
  gridMaxGrids: 2,
  gridPauseAfterLossBars: 36,
  feePct: 0.02,
  slippageBps: 1,
  trendFilterPeriod: 0,
  onlyWithTrend: false,
  targetRatio: 4,
  chopGateAdx: 26,
  leverage: 1,
  maxPositionSizePct: 50,
  maxDrawdownPct: 5,
  maxDailyLossPct: 2,
} as const;

export type ValidatedGridCandidate =
  | typeof VALIDATED_BTC_GRID_CANDIDATE
  | typeof VALIDATED_SOL_GRID_CANDIDATE;

/** Readiness cohort (v2): each symbol runs its own validated candidate. */
export const READINESS_COHORT_CANDIDATES = [
  VALIDATED_BTC_GRID_CANDIDATE,
  VALIDATED_SOL_GRID_CANDIDATE,
] as const satisfies readonly ValidatedGridCandidate[];

export function candidateForSymbol(
  symbol: string,
): ValidatedGridCandidate | undefined {
  return READINESS_COHORT_CANDIDATES.find(
    (candidate) => candidate.symbol === symbol,
  );
}
