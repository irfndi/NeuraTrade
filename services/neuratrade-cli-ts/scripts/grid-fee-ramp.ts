// Fee/slippage ramp for the locked BTC candidate vs the robustness leader.
// Quantifies the maker-assumption safety margin: how far can execution costs
// rise before stressLB / confLB go negative. Runs validateGridEvidence per
// (config, fee, slippage) combo on the full 24-month span.
//
// Usage: bun run scripts/grid-fee-ramp.ts
import { Database } from "bun:sqlite";
import { validateGridEvidence } from "../src/scalping/grid-validation.js";
import type { GridOptions } from "../src/scalping/grid.js";

const HOME = process.env.NEURATRADE_HOME ?? `${process.env.HOME}/.neuratrade`;
const db = new Database(`${HOME}/data/neuratrade.db`, { readonly: true });
const rows = db
  .query(
    `SELECT o.open_price as open, o.high_price as high, o.low_price as low,
            o.close_price as close, o.volume as volume, o.timestamp as ts
     FROM ohlcv_data o JOIN exchanges e ON e.id=o.exchange_id
     JOIN trading_pairs tp ON tp.id=o.trading_pair_id
     WHERE e.name='bitget-futures' AND tp.symbol='BTC/USDT:USDT' AND o.timeframe='15m'
     ORDER BY o.timestamp ASC`,
  )
  .all() as Array<{ open: number; high: number; low: number; close: number; volume: number; ts: string }>;
db.close();
const candles = rows.map((r) => ({
  open: r.open, high: r.high, low: r.low, close: r.close, volume: r.volume,
  timestamp: new Date(Date.parse(r.ts)),
}));
const now = new Date(candles.at(-1)!.timestamp.getTime() + 15 * 60 * 1000);

const CONFIGS: Array<{ name: string; grid: GridOptions }> = [
  {
    name: "LOCKED (0.5/2/p0/t2/a15)",
    grid: { initialCapital: 100, gridStepPct: 0.5, gridMaxGrids: 2, gridPauseAfterLossBars: 0, feePct: 0.02, slippageBps: 1, trendFilterPeriod: 0, leverage: 1, positionFraction: 0.5, onlyWithTrend: false, targetRatio: 2, chopGateAdxThreshold: 15 },
  },
  {
    name: "ROBUST (0.5/3/p48/t2/a15)",
    grid: { initialCapital: 100, gridStepPct: 0.5, gridMaxGrids: 3, gridPauseAfterLossBars: 48, feePct: 0.02, slippageBps: 1, trendFilterPeriod: 0, leverage: 1, positionFraction: 0.5, onlyWithTrend: false, targetRatio: 2, chopGateAdxThreshold: 15 },
  },
];

const FEES = [0.02, 0.03, 0.04, 0.05, 0.06];
const SLIPS = [1, 2];

for (const { name, grid } of CONFIGS) {
  console.log(`\n=== ${name} ===`);
  console.log("fee  slip | ret%    dd%    oos  confLB    stressRet% stressLB  pass");
  for (const fee of FEES) {
    for (const slip of SLIPS) {
      const g: GridOptions = { ...grid, feePct: fee, slippageBps: slip };
      const r = validateGridEvidence(candles, { now, timeframeMinutes: 15, grid: g, executionParityPassed: true });
      if (r.kind === "invalid") {
        console.log(`${fee}  ${slip}  | INVALID: ${r.failures.join("; ")}`);
        continue;
      }
      const pass =
        r.historical.profitableWindowPct > 50 &&
        r.historical.compoundedReturnPct >= 0 &&
        r.historical.maximumDrawdownPct <= 15 &&
        r.fixedOos.totalTrades >= 30 &&
        r.confidence.lowerBoundPct >= 0 &&
        r.stress.worstReturnPct >= 0 &&
        r.stress.pooledLowerBoundPct >= 0;
      console.log(
        `${fee}  ${slip}   | ${r.historical.compoundedReturnPct.toFixed(2).padStart(6)} ${r.historical.maximumDrawdownPct.toFixed(2).padStart(6)} ${String(r.fixedOos.totalTrades).padStart(4)} ${r.confidence.lowerBoundPct.toFixed(5).padStart(9)} ${r.stress.worstReturnPct.toFixed(2).padStart(9)} ${r.stress.pooledLowerBoundPct.toFixed(5).padStart(9)}  ${pass ? "PASS" : "FAIL"}`,
      );
    }
  }
}
