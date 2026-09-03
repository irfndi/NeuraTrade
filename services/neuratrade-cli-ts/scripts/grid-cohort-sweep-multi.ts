// Multi-symbol cohort sweep for Bybit (and any exchange): run the SAME
// readiness gate (validateGridEvidence) over a focused parameter space for
// every symbol with deep 15m history, and rank passing configs by stress-LB
// margin. Used to find cohort candidates beyond BTC/SOL.
//
// Usage:
//   bun run scripts/grid-cohort-sweep-multi.ts --exchange=bybit-futures [--symbols=BTCUSDT,ETHUSDT,...] [--min-candles=55000]
//
// Output: ~/.neuratrade/tuning/grid-cohort-<exchange>-multi.json (flushed
// every 50 configs so partial progress survives).
import { Database } from "bun:sqlite";
import { mkdirSync, writeFileSync } from "node:fs";
import { join, dirname } from "node:path";
import { validateGridEvidence } from "../src/scalping/grid-validation.js";
import type { GridOptions } from "../src/scalping/grid.js";

const HOME = process.env.NEURATRADE_HOME ?? `${process.env.HOME}/.neuratrade`;
const arg = (flag: string, fallback: string) =>
  process.argv.find((a) => a.startsWith(flag))?.slice(flag.length) ?? fallback;
const exchange = arg("--exchange=", "bybit-futures");
const symbolFilter = arg("--symbols=", "")
  .split(",")
  .filter(Boolean)
  .map((s) => (s.includes("/") ? s : `${s.replace(/USDT$/, "")}/USDT:USDT`));
const minCandles = Number(arg("--min-candles=", "55000"));

const db = new Database(`${HOME}/data/neuratrade.db`, { readonly: true });
const symbols = db
  .query(
    `SELECT tp.symbol, COUNT(*) n
     FROM ohlcv_data o JOIN exchanges e ON e.id=o.exchange_id
     JOIN trading_pairs tp ON tp.id=o.trading_pair_id
     WHERE e.name=? AND o.timeframe='15m'
     GROUP BY tp.symbol HAVING COUNT(*) >= ? ORDER BY n DESC`,
  )
  .all(exchange, minCandles) as Array<{ symbol: string; n: number }>;
db.close();

const targets = symbols
  .filter((s) => symbolFilter.length === 0 || symbolFilter.includes(s.symbol))
  .map((s) => s.symbol);

if (targets.length === 0) {
  console.error(`no ${exchange} symbols with >= ${minCandles} 15m candles`);
  process.exit(1);
}

// Focused space: the validated BTC/SOL/ETH winners live in step 0.5-1.25,
// grids 2-3, pause 0-48, target 2-4, adx 0-28.
const steps = [0.5, 0.75, 1.0, 1.25];
const grids = [2, 3];
const pauses = [0, 24, 48];
const targetsRatio = [2, 3, 4];
const adxGates = [0, 15, 20, 28];
const base: GridOptions = {
  initialCapital: 100,
  gridStepPct: 0.75,
  gridMaxGrids: 2,
  gridPauseAfterLossBars: 0,
  feePct: 0.02,
  slippageBps: 1,
  trendFilterPeriod: 0,
  leverage: 1,
  positionFraction: 0.5,
  onlyWithTrend: false,
};

interface Row {
  symbol: string;
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
    tradesPerMonth: number;
    confidenceLowerBoundPct: number;
    stressWorstReturnPct: number;
    stressLowerBoundPct: number;
  };
  pass: boolean;
}

const db2 = new Database(`${HOME}/data/neuratrade.db`, { readonly: true });
const outPath = join(
  HOME,
  "tuning",
  `grid-cohort-${exchange.replace(/[^A-Za-z0-9]/g, "_")}-multi.json`,
);
mkdirSync(dirname(outPath), { recursive: true });
const all: Row[] = [];
let evaluated = 0;
const t0 = Date.now();

for (const symbol of targets) {
  const rows = db2
    .query(
      `SELECT o.open_price as open, o.high_price as high, o.low_price as low,
              o.close_price as close, o.volume as volume, o.timestamp as ts
       FROM ohlcv_data o JOIN exchanges e ON e.id=o.exchange_id
       JOIN trading_pairs tp ON tp.id=o.trading_pair_id
       WHERE e.name=? AND tp.symbol=? AND o.timeframe='15m'
       ORDER BY o.timestamp ASC`,
    )
    .all(exchange, symbol) as Array<{
    open: number;
    high: number;
    low: number;
    close: number;
    volume: number;
    ts: string;
  }>;
  const candles = rows.map((r) => ({
    open: r.open,
    high: r.high,
    low: r.low,
    close: r.close,
    volume: r.volume,
    timestamp: new Date(Date.parse(r.ts)),
  }));
  const now = new Date(candles.at(-1)!.timestamp.getTime() + 15 * 60 * 1000);
  const monthsOos = (0.2 * candles.length * 15) / 43200;

  for (const step of steps)
    for (const maxGrids of grids)
      for (const pause of pauses)
        for (const target of targetsRatio)
          for (const adx of adxGates) {
            evaluated += 1;
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
            const g = {
              profitableWindowPct: r.historical.profitableWindowPct,
              compoundedReturnPct: r.historical.compoundedReturnPct,
              maxDrawdownPct: r.historical.maximumDrawdownPct,
              fixedOosTrades: r.fixedOos.totalTrades,
              tradesPerMonth: r.fixedOos.totalTrades / monthsOos,
              confidenceLowerBoundPct: r.confidence.lowerBoundPct,
              stressWorstReturnPct: r.stress.worstReturnPct,
              stressLowerBoundPct: r.stress.pooledLowerBoundPct,
            };
            const pass =
              g.profitableWindowPct > 50 &&
              g.maxDrawdownPct <= 15 &&
              g.fixedOosTrades >= 30 &&
              g.confidenceLowerBoundPct >= 0 &&
              g.stressWorstReturnPct >= 0 &&
              g.stressLowerBoundPct >= 0;
            all.push({
              symbol,
              config: {
                gridStepPct: step,
                gridMaxGrids: maxGrids,
                gridPauseAfterLossBars: pause,
                targetRatio: target,
                chopGateAdx: adx,
              },
              gates: g,
              pass,
            });
            if (evaluated % 50 === 0) {
              const passing = all
                .filter((x) => x.pass)
                .sort(
                  (a, b) =>
                    b.gates.stressLowerBoundPct - a.gates.stressLowerBoundPct,
                );
              writeFileSync(
                outPath,
                JSON.stringify(
                  {
                    exchange,
                    evaluated,
                    symbols: targets.length,
                    passing,
                    all,
                  },
                  null,
                  2,
                ),
              );
              const min = (Date.now() - t0) / 60000;
              console.log(
                `...${evaluated} (${symbol}) after ${min.toFixed(1)}m, ${((min / evaluated) * 60).toFixed(1)}s/conf | pass ${passing.length}`,
              );
            }
          }
}

const passing = all
  .filter((x) => x.pass)
  .sort((a, b) => b.gates.stressLowerBoundPct - a.gates.stressLowerBoundPct);
writeFileSync(
  outPath,
  JSON.stringify(
    { exchange, evaluated, symbols: targets.length, passing, all },
    null,
    2,
  ),
);
console.log(
  `\n=== ${exchange}: ${targets.length} symbols, ${evaluated} evaluated, ${passing.length} PASS (by stressLB) ===`,
);
for (const p of passing.slice(0, 30)) {
  console.log(
    `PASS ${p.symbol} step=${p.config.gridStepPct} grids=${p.config.gridMaxGrids} pause=${p.config.gridPauseAfterLossBars} target=${p.config.targetRatio} adx=${p.config.chopGateAdx} | win=${p.gates.profitableWindowPct.toFixed(1)}% ret=${p.gates.compoundedReturnPct.toFixed(2)}% dd=${p.gates.maxDrawdownPct.toFixed(2)}% tpm=${p.gates.tradesPerMonth.toFixed(1)} oos=${p.gates.fixedOosTrades} confLB=${p.gates.confidenceLowerBoundPct.toFixed(5)} stressLB=${p.gates.stressLowerBoundPct.toFixed(5)}`,
  );
}
console.log(`\nresults: ${outPath}`);
db2.close();
