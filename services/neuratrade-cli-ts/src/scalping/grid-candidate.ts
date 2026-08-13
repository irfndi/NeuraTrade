export const VALIDATED_BTC_GRID_CANDIDATE = {
  exchange: "bybit-futures",
  symbol: "BTC/USDT:USDT",
  timeframe: "15m",
  productType: "USDT-FUTURES",
  gridStepPct: 0.5,
  gridMaxGrids: 3,
  // Re-fitted 2026-08-11 over the FULL 24-month history (70,873 15m candles,
  // 2024-08..2026-08) after the prior lock (step 1 / grids 1.5 / pause 24 /
  // tr 3 / cg 28) degraded to +1.84% ret / 39.5% dd on the full span. Parallel
  // family sweep (leverage x positionFraction) fit: step 0.5, pause 0,
  // targetRatio 2, chopGateAdx 15, leverage 1, 210 trades, 10/12 disjoint 60d
  // windows profitable (max single-window dd 5.3%).
  // Robustness re-fit 2026-08-13: a stress-LB-ranked sweep over the full
  // 24-month span (scripts/grid-robustness-sweep.ts) found grids 3 / pause 48
  // in the same step 0.5 / targetRatio 2 / chopGateAdx 15 family thickens the
  // adverse-selection + taker-stop stress margin from stressLB +0.00100 to
  // +0.00166 and confLB +0.00082 to +0.00165, at compounded +18.39% ret /
  // 2.79% dd (13 windows, 69.2% profitable, 38 fixed-OOS trades). Cost is
  // slightly fewer OOS trades (38 vs 41) and lower window win% (69.2% vs
  // 76.9%), both still above the gate. A fee-ramp (scripts/grid-fee-ramp.ts)
  // confirms the config stays profitable at full taker 0.06%/slippage 2
  // (stressLB +0.00095). Higher leverage (2-3x) only scaled dd (>29%) without
  // improving risk-adjusted return; leverage 1 is optimal.
  gridPauseAfterLossBars: 48,
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

// SOL cohort candidate (re-fitted 2026-08-13 on clean Bybit mainnet 15m data
// via scripts/verify-cohort-on-bybit.ts): the bitget-fitted pause-36/adx-26
// config did NOT transfer cross-venue (confLB -0.00049 on Bybit, high
// variance/overfit). Bybit-passing alternate: step 1.25 / grids 2 / pause 0 /
// target 4 / adx 28 — confLB +0.00041, stressLB +0.00231. Bybit is the live
// engine (clever-cabin-fyv), so the candidate is locked to Bybit-validated
// args. Adds ~8 fills/mo to the cohort clock.
export const VALIDATED_SOL_GRID_CANDIDATE = {
  exchange: "bybit-futures",
  symbol: "SOL/USDT:USDT",
  timeframe: "15m",
  productType: "USDT-FUTURES",
  gridStepPct: 1.25,
  gridMaxGrids: 2,
  gridPauseAfterLossBars: 0,
  feePct: 0.02,
  slippageBps: 1,
  trendFilterPeriod: 0,
  onlyWithTrend: false,
  targetRatio: 4,
  chopGateAdx: 28,
  leverage: 1,
  maxPositionSizePct: 50,
  maxDrawdownPct: 5,
  maxDailyLossPct: 2,
} as const;

// ETH cohort candidate (re-fitted 2026-08-13 on clean Bybit mainnet 15m data
// via scripts/verify-cohort-on-bybit.ts): the bitget-fitted pause-48/target-4/
// adx-28 config was bitget-overfit (win 46.2%, ret -6.57%, dd 18.87% on
// Bybit). Bybit-passing alternate: step 0.75 / grids 3 / pause 24 / target 3 /
// adx 20 — confLB +0.00036, stressLB +0.00102. Bybit is the live engine
// (clever-cabin-fyv), so the candidate is locked to Bybit-validated args.
// Adds ~17 fills/mo toward the cohort's 50-fill gate (fastest clock).
export const VALIDATED_ETH_GRID_CANDIDATE = {
  exchange: "bybit-futures",
  symbol: "ETH/USDT:USDT",
  timeframe: "15m",
  productType: "USDT-FUTURES",
  gridStepPct: 0.75,
  gridMaxGrids: 3,
  gridPauseAfterLossBars: 24,
  feePct: 0.02,
  slippageBps: 1,
  trendFilterPeriod: 0,
  onlyWithTrend: false,
  targetRatio: 3,
  chopGateAdx: 20,
  leverage: 1,
  maxPositionSizePct: 50,
  maxDrawdownPct: 5,
  maxDailyLossPct: 2,
} as const;

export type ValidatedGridCandidate =
  | typeof VALIDATED_BTC_GRID_CANDIDATE
  | typeof VALIDATED_SOL_GRID_CANDIDATE
  | typeof VALIDATED_ETH_GRID_CANDIDATE;

/** Readiness cohort (v2): each symbol runs its own validated candidate. */
export const READINESS_COHORT_CANDIDATES = [
  VALIDATED_BTC_GRID_CANDIDATE,
  VALIDATED_SOL_GRID_CANDIDATE,
  VALIDATED_ETH_GRID_CANDIDATE,
] as const satisfies readonly ValidatedGridCandidate[];

export function candidateForSymbol(
  symbol: string,
): ValidatedGridCandidate | undefined {
  return READINESS_COHORT_CANDIDATES.find(
    (candidate) => candidate.symbol === symbol,
  );
}
