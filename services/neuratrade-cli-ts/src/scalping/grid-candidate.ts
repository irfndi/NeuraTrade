export const VALIDATED_BTC_GRID_CANDIDATE = {
  exchange: "bitget-futures",
  symbol: "BTC/USDT:USDT",
  timeframe: "15m",
  productType: "USDT-FUTURES",
  gridStepPct: 0.5,
  gridMaxGrids: 2,
  // Re-fitted 2026-08-11 over the FULL 24-month history (70,873 15m candles,
  // 2024-08..2026-08) after the prior lock (step 1 / grids 1.5 / pause 24 /
  // tr 3 / cg 28) degraded to +1.84% ret / 39.5% dd on the full span. Parallel
  // family sweep (leverage x positionFraction) fit: step 0.5, grids 2,
  // pause 0, targetRatio 2, chopGateAdx 15, leverage 1, 210 trades, 10/12
  // disjoint 60d windows profitable (max single-window dd 5.3%). NOTE: the
  // sweep's "+49.1% ret / 7.3% dd / pf 1.50" headline was measured at FULL
  // position (positionFraction 1.0); this candidate is LOCKED at
  // maxPositionSizePct 50 (positionFraction 0.5), so that headline does not
  // apply to the deployed config. At the locked 0.5 position the same config
  // backtests to +22.40% single-run total return (210 trades), and the
  // readiness gate's compounded 13-window figure is +16.39% ret / 2.80% dd /
  // confLB +0.00082 / stressLB +0.00100. Higher leverage (2-3x) only scaled
  // dd (>29%) without improving risk-adjusted return; leverage 1 is optimal.
  gridPauseAfterLossBars: 0,
  feePct: 0.02,
  slippageBps: 1,
  trendFilterPeriod: 0,
  onlyWithTrend: false,
  targetRatio: 2,
  chopGateAdx: 15,
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
