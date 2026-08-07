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
