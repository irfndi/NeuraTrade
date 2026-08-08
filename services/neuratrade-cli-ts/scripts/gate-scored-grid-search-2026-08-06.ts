#!/usr/bin/env bun
/**
 * Gate-scored grid search re-run (2026-08-06) with the CORRECTED bootstrap.
 *
 * The Aug-3 search (docs/superpowers/specs/2026-08-03-gate-scored-grid-search.md)
 * promoted pause-24/ADX-24 on confidence LB +0.0022 computed with a degenerate
 * bootstrap (all resamples identical -> LB = sample mean). The degeneracy was
 * fixed in 745071e0; this sweep re-derives the frontier with validateGridEvidence
 * (which now uses the fixed bootstrap) and requires EVERY gate:
 *   profitable windows > 50%, compounded >= 0%, DD <= 15%, fixed-OOS >= 30,
 *   confidence LB >= 0, stress return >= 0, stress LB >= 0.
 *
 * Manifest-locked values (fee 0.06, slippage 2, position fraction 0.5,
 * leverage 1, trend filter 0, onlyWithTrend false) are held constant; the
 * strategy dials sweep the same space as Aug-3.
 *
 * Usage: bun run scripts/gate-scored-grid-search-2026-08-06.ts
 * Output: ~/.neuratrade/tuning/gate-scored-search-2026-08-06.json
 */
import { Database } from "bun:sqlite";
import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { validateGridEvidence } from "../src/scalping/grid-validation.js";
import type { GridOptions } from "../src/scalping/grid.js";

// Sweep parameters: --fee and --slippage-bps vary the execution-cost model
// (0.06/2 = taker market-after-trigger; 0.02/1 = maker/limit assumption).
const argValue = (name: string, fallback: string): string => {
  const index = process.argv.indexOf(name);
  return index === -1 ? fallback : (process.argv[index + 1] ?? fallback);
};
const fee = Number(argValue("--fee", "0.06"));
const slippage = Number(argValue("--slippage-bps", "2"));
const fast = process.argv.includes("--fast");
// Timeframe-aware: 5m pause arrays are the 15m wall-clock equivalents x3.
const timeframe = argValue("--timeframe", "15m");
const symbol = argValue("--symbol", "BTC/USDT:USDT");
const barMinutes = timeframe === "5m" ? 5 : 15;
// Chunking: --pauses filters the pause space (comma list); --tag suffixes
// the output file. Needed because per-call heap churn (bootstraps) grows
// ~5x on 5m and a single long run can exhaust JSC's heap under load.
const pauseFilter = argValue("--pauses", "");
const stepFilter = argValue("--steps", "");
const targetFilter = argValue("--targets", "");
const adxFilter = argValue("--adx", "");
const tag = argValue("--tag", "");
const home =
  process.env.NEURATRADE_HOME ?? join(process.env.HOME!, ".neuratrade");
const db = new Database(join(home, "data", "neuratrade.db"), {
  readonly: true,
});
const rows = db
  .query<Record<string, unknown>, string[]>(
    `SELECT o.open_price, o.high_price, o.low_price, o.close_price, o.volume, o.timestamp
   FROM ohlcv_data o JOIN exchanges e ON e.id = o.exchange_id JOIN trading_pairs p ON p.id = o.trading_pair_id
   WHERE e.name = ? AND p.symbol = ? AND o.timeframe = ? ORDER BY o.timestamp ASC`,
  )
  .all("bitget-futures", symbol, timeframe);
const candles = rows.map((r) => ({
  open: r.open_price as number,
  high: r.high_price as number,
  low: r.low_price as number,
  close: r.close_price as number,
  volume: r.volume as number,
  timestamp: new Date(r.timestamp as string),
}));
console.log(
  `candles: ${candles.length} | last: ${candles.at(-1)!.timestamp.toISOString()}`,
);

const now = new Date(
  candles.at(-1)!.timestamp.getTime() + barMinutes * 60 * 1000,
);
const base: GridOptions = {
  initialCapital: 100,
  gridStepPct: 1,
  gridMaxGrids: 1,
  gridPauseAfterLossBars: 24,
  feePct: fee,
  slippageBps: slippage,
  trendFilterPeriod: 0,
  leverage: 1,
  positionFraction: 0.5,
  onlyWithTrend: false,
};

interface SweepResult {
  config: {
    gridStepPct: number;
    gridMaxGrids: number;
    gridPauseAfterLossBars: number;
    targetRatio: number;
    chopGateAdx: number;
  };
  gates: {
    profitableWindowPct: number;
    compoundedReturnPct: number;
    maxDrawdownPct: number;
    fixedOosTrades: number;
    confidenceLowerBoundPct: number;
    stressWorstReturnPct: number;
    stressLowerBoundPct: number;
  };
  pass: boolean;
  failures: string[];
}

const results: SweepResult[] = [];
// --steps/--targets/--adx OVERRIDE the space (comma list), so values
// outside the default arrays (e.g. small steps) can be swept.
const steps = stepFilter
  ? stepFilter.split(",").map(Number)
  : fast
    ? [1, 1.25]
    : [0.75, 1, 1.25];
const grids = fast ? [1, 1.5, 2] : [1, 1.5, 2];
const pauses =
  timeframe === "5m"
    ? fast
      ? [54, 72, 90, 108, 126]
      : [18, 36, 54, 72, 90, 108, 126, 144]
    : fast
      ? [18, 24, 30, 36, 42]
      : [6, 12, 18, 24, 30, 36, 42, 48];
const targets = targetFilter
  ? targetFilter.split(",").map(Number)
  : fast
    ? [2, 3, 4]
    : [1, 2, 3, 4];
const adxGates = adxFilter
  ? adxFilter.split(",").map(Number)
  : fast
    ? [24, 26, 28, 30]
    : [20, 22, 24, 26, 28, 30];
let total = 0;
const failures: string[] = [];
const t0 = Date.now();

for (const step of steps)
  for (const maxGrids of grids)
    for (const pause of pauses.filter(
      (p) => !pauseFilter || pauseFilter.split(",").includes(String(p)),
    ))
      for (const target of targets)
        for (const adx of adxGates) {
          total += 1;
          const grid: GridOptions = {
            ...base,
            gridStepPct: step,
            gridMaxGrids: maxGrids,
            gridPauseAfterLossBars: pause,
            targetRatio: target,
            chopGateAdxThreshold: adx,
          };
          const r = validateGridEvidence(candles, {
            now,
            timeframeMinutes: barMinutes,
            grid,
            executionParityPassed: true,
          });
          if (r.kind === "invalid") {
            failures.push(r.failures.join("; "));
            continue;
          }
          const gate = {
            profitableWindowPct: r.historical.profitableWindowPct,
            compoundedReturnPct: r.historical.compoundedReturnPct,
            maxDrawdownPct: r.historical.maximumDrawdownPct,
            fixedOosTrades: r.fixedOos.totalTrades,
            confidenceLowerBoundPct: r.confidence.lowerBoundPct,
            stressWorstReturnPct: r.stress.worstReturnPct,
            // Amendment 2026-08-07 (B): stress LB = pooled 5-seed bootstrap.
            stressLowerBoundPct: r.stress.pooledLowerBoundPct,
          };
          const fails: string[] = [];
          if (gate.profitableWindowPct <= 50) fails.push("windows<=50%");
          if (gate.compoundedReturnPct < 0) fails.push("compounded<0");
          if (gate.maxDrawdownPct > 15) fails.push("dd>15%");
          if (gate.fixedOosTrades < 30) fails.push("oos<30");
          if (gate.confidenceLowerBoundPct < 0) fails.push("confLB<0");
          if (gate.stressWorstReturnPct < 0) fails.push("stressRet<0");
          if (gate.stressLowerBoundPct < 0) fails.push("stressLB<0");
          results.push({
            config: {
              gridStepPct: step,
              gridMaxGrids: maxGrids,
              gridPauseAfterLossBars: pause,
              targetRatio: target,
              chopGateAdx: adx,
            },
            gates: gate,
            pass: fails.length === 0,
            failures: fails,
          });
          if (total % 50 === 0) {
            const elapsedMin = (Date.now() - t0) / 60000;
            console.log(
              `...${total}/${steps.length * grids.length * pauses.length * targets.length * adxGates.length} after ${elapsedMin.toFixed(1)}m (${((elapsedMin / total) * 60).toFixed(1)}s/config, ETA ${((elapsedMin / total) * (1728 - total)).toFixed(0)}m) | pass so far: ${results.filter((x) => x.pass).length}`,
            );
          }
        }

const passing = results
  .filter((r) => r.pass)
  .sort(
    (a, b) => b.gates.confidenceLowerBoundPct - a.gates.confidenceLowerBoundPct,
  );
const output = {
  run: "2026-08-06",
  fee,
  slippage,
  fast,
  candles: candles.length,
  total,
  failures,
  passing,
  all: results,
};
const outPath = join(
  home,
  "tuning",
  `gate-scored-search-2026-08-06-fee${fee}-slip${slippage}-${timeframe}-${symbol.replace("/", "-").replace(":", "")}${tag ? `-${tag}` : ""}.json`,
);
mkdirSync(dirname(outPath), { recursive: true });
writeFileSync(outPath, JSON.stringify(output, null, 2));
console.log(
  `\n=== fee ${fee} slip ${slippage}: ${total} configs, ${passing.length} PASS ===`,
);
for (const p of passing.slice(0, 15)) {
  console.log(
    `PASS step=${p.config.gridStepPct} grids=${p.config.gridMaxGrids} pause=${p.config.gridPauseAfterLossBars} target=${p.config.targetRatio} adx=${p.config.chopGateAdx} | windows=${p.gates.profitableWindowPct.toFixed(1)}% ret=${p.gates.compoundedReturnPct.toFixed(2)}% dd=${p.gates.maxDrawdownPct.toFixed(2)}% oos=${p.gates.fixedOosTrades} confLB=${p.gates.confidenceLowerBoundPct.toFixed(5)} stressRet=${p.gates.stressWorstReturnPct.toFixed(2)}% stressLB=${p.gates.stressLowerBoundPct.toFixed(5)}`,
  );
}
console.log(`\nresults: ${outPath}`);
