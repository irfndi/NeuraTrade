export const VALIDATED_BTC_GRID_CANDIDATE = {
  exchange: "bitget-futures",
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

// ETH cohort candidate (2026-08-13, gate-scored 15m fast space, pooled
// stress-LB via scripts/grid-cohort-sweep.ts): the ETH sweep over the full
// 24-month bitget history (70,873 15m candles) found two passing families at
// step 0.75. This one is chosen for robustness margin: confLB +0.00099 and
// stressLB +0.00194 thicken the adverse-selection + taker-stop margin well
// above zero (the rival grids-3/pause-0/target-3/adx-20 fit returned +15.19%
// but only confLB +0.00019, too thin to survive minor data perturbation).
// windows 53.8%, ret +5.23%, dd 13.40%, OOS 85 (~17.3 fills/mo, the fastest
// clock in the cohort), stressWorst +2.95%. Adds ~17 fills/mo toward the
// cohort's 50-fill gate.
export const VALIDATED_ETH_GRID_CANDIDATE = {
  exchange: "bitget-futures",
  symbol: "ETH/USDT:USDT",
  timeframe: "15m",
  productType: "USDT-FUTURES",
  gridStepPct: 0.75,
  gridMaxGrids: 2,
  gridPauseAfterLossBars: 48,
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
