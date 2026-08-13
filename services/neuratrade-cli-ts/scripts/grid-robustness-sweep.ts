// Grid robustness sweep: rank BTC configs by stress-LB margin (adverse
// selection + taker stops), not by raw return. The locked candidate was fitted
// for risk-adjusted return (grid-sweep-fit.ts) and lands at a thin stressLB
// (+0.00100). This sweep searches the same structural space plus the locked
// candidate's neighborhood and ranks PASSING configs by stressLowerBoundPct
// so a more robust deployable config can be identified on the full 24-month span.
//
// Usage: bun run scripts/grid-robustness-sweep.ts [--quick]
// Output: ~/.neuratrade/tuning/grid-robustness-sweep.json
import { Database } from "bun:sqlite";
import { mkdirSync, writeFileSync } from "node:fs";
import { join, dirname } from "node:path";
import { validateGridEvidence } from "../src/scalping/grid-validation.js";
import type { GridOptions } from "../src/scalping/grid.js";

const HOME = process.env.NEURATRADE_HOME ?? `${process.env.HOME}/.neuratrade`;
const quick = process.argv.includes("--quick");
const stepArg = process.argv.find((a) => a.startsWith("--steps="));
const stepFilter = stepArg ? stepArg.slice("--steps=".length).split(",").map(Number) : null;

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
  .all() as Array<{
  open: number;
  high: number;
  low: number;
  close: number;
  volume: number;
  ts: string;
}>;
db.close();

const candles = rows.map((r) => ({
  open: r.open,
  high: r.high,
  low: r.low,
  close: r.close,
  volume: r.volume,
  timestamp: new Date(Date.parse(r.ts)),
}));
const now = new Date(candles.at(-1)!.timestamp.getTime() + 15 * 60 * 1000);
console.log(`candles: ${candles.length} | last: ${candles.at(-1)!.timestamp.toISOString()}`);

const base: GridOptions = {
  initialCapital: 100,
  gridStepPct: 0.5,
  gridMaxGrids: 2,
  gridPauseAfterLossBars: 0,
  feePct: 0.02,
  slippageBps: 1,
  trendFilterPeriod: 0,
  leverage: 1,
  positionFraction: 0.5,
  onlyWithTrend: false,
};

const steps = stepFilter ?? (quick ? [0.5, 1.0] : [0.25, 0.5, 0.75, 1.0]);
const grids = quick ? [2, 3] : [1.5, 2.0, 3.0];
const pauses = quick ? [0, 24, 48] : [0, 24, 48];
const targets = quick ? [2, 3] : [1, 2, 3, 4];
const adxGates = quick ? [12, 15, 20, 28] : [0, 12, 15, 20, 28];

interface Row {
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

const results: Row[] = [];
const outPath = join(
  HOME,
  "tuning",
  `grid-robustness-sweep${stepFilter ? `-steps${stepFilter.join("_")}` : ""}.json`,
);
mkdirSync(dirname(outPath), { recursive: true });
let total = 0;
const t0 = Date.now();

for (const step of steps)
  for (const maxGrids of grids)
    for (const pause of pauses)
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
            timeframeMinutes: 15,
            grid,
            executionParityPassed: true,
          });
          if (r.kind === "invalid") continue;
          const gate = {
            profitableWindowPct: r.historical.profitableWindowPct,
            compoundedReturnPct: r.historical.compoundedReturnPct,
            maxDrawdownPct: r.historical.maximumDrawdownPct,
            fixedOosTrades: r.fixedOos.totalTrades,
            confidenceLowerBoundPct: r.confidence.lowerBoundPct,
            stressWorstReturnPct: r.stress.worstReturnPct,
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
          if (total % 100 === 0) {
            // Flush partial results so a timeout never loses completed work.
            writeFileSync(
              outPath,
              JSON.stringify({ candles: candles.length, total, passing: results.filter((x) => x.pass), all: results }, null, 2),
            );
            const min = (Date.now() - t0) / 60000;
            console.log(
              `...${total} after ${min.toFixed(1)}m (${((min / total) * 60).toFixed(1)}s/config) | pass so far: ${results.filter((x) => x.pass).length}`,
            );
          }
        }

const passing = results
  .filter((r) => r.pass)
  .sort((a, b) => b.gates.stressLowerBoundPct - a.gates.stressLowerBoundPct);

writeFileSync(
  outPath,
  JSON.stringify(
    { candles: candles.length, total, passing, all: results },
    null,
    2,
  ),
);

console.log(`\n=== ${total} configs, ${passing.length} PASS (ranked by stressLB) ===`);
for (const p of passing.slice(0, 20)) {
  console.log(
    `PASS step=${p.config.gridStepPct} grids=${p.config.gridMaxGrids} pause=${p.config.gridPauseAfterLossBars} target=${p.config.targetRatio} adx=${p.config.chopGateAdx} | windows=${p.gates.profitableWindowPct.toFixed(1)}% ret=${p.gates.compoundedReturnPct.toFixed(2)}% dd=${p.gates.maxDrawdownPct.toFixed(2)}% oos=${p.gates.fixedOosTrades} confLB=${p.gates.confidenceLowerBoundPct.toFixed(5)} stressRet=${p.gates.stressWorstReturnPct.toFixed(2)}% stressLB=${p.gates.stressLowerBoundPct.toFixed(5)}`,
  );
}
console.log(`\nresults: ${outPath}`);
