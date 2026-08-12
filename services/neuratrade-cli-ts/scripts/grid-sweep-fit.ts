// Grid parameter-fitting sweep for VALIDATED_BTC_GRID_CANDIDATE.
// Usage: bun run scripts/grid-sweep-fit.ts <familyId>
// Reads ALL bitget-futures BTC/USDT:USDT 15m candles from the DB (2024-08..2026-08),
// sweeps the structural grid knobs for one (leverage, positionFraction) family,
// and prints the top configs ranked by fitness =
//   totalReturnPct - 1.0 * maxDrawdownPct + 0.02 * totalTrades
// subject to hard guards: maxDD <= 20%, trades >= 400, return > 0, PF >= 1.05.
// Output is JSON lines to stdout so a subagent/parent can consume it.
import { Database } from "bun:sqlite";
import { runGridBacktest } from "../src/scalping/grid.js";
import type { CandleLike } from "../src/scalping/types.js";

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
const candles: CandleLike[] = rows.map((r) => ({
  open: r.open, high: r.high, low: r.low, close: r.close, volume: r.volume,
  timestamp: new Date(Date.parse(r.ts)),
}));

// Adjustable hard-guard ceiling for max drawdown (override via env).
const DD_CAP = Number(process.env.DD_CAP ?? 20);
const MIN_TRADES = Number(process.env.MIN_TRADES ?? 400);
const MIN_RETURN = Number(process.env.MIN_RETURN ?? 0);
const MIN_PF = Number(process.env.MIN_PF ?? 1.05);

// Families: [leverage, positionFraction]
const FAMILIES: [number, number][] = [
  [1, 1.0],
  [1, 0.5],
  [2, 0.5],
  [2, 1.0],
  [3, 0.25],
  [3, 0.5],
];
const familyId = Number.parseInt(process.argv[2] ?? "0", 10);
const [leverage, positionFraction] =
  FAMILIES[familyId] ?? FAMILIES[0];

// Structural grid to sweep for this family.
const gridStepPct = [0.25, 0.5, 0.75, 1.0];
const gridMaxGrids = [1.5, 2.0, 3.0, 4.0];
const gridPauseAfterLossBars = [0, 24, 48];
const targetRatio = [1, 2, 3, 4];
const chopGateAdx = [0, 12, 20, 28];

const results: Array<{
  gridStepPct: number; gridMaxGrids: number; gridPauseAfterLossBars: number;
  targetRatio: number; chopGateAdx: number; leverage: number; positionFraction: number;
  totalReturnPct: number; maxDrawdownPct: number; totalTrades: number; profitFactor: number;
  score: number;
}> = [];

let evaluated = 0;
for (const gs of gridStepPct)
  for (const gm of gridMaxGrids)
    for (const gp of gridPauseAfterLossBars)
      for (const tr of targetRatio)
        for (const cg of chopGateAdx) {
          const r = runGridBacktest(candles, {
            gridStepPct: gs,
            gridMaxGrids: gm,
            gridPauseAfterLossBars: gp,
            feePct: 0.02,
            slippageBps: 1,
            initialCapital: 10_000,
            trendFilterPeriod: 0,
            leverage,
            onlyWithTrend: false,
            targetRatio: tr,
            chopGateAdxThreshold: cg,
            positionFraction,
          });
          evaluated++;
          if (
            r.maxDrawdownPct > DD_CAP ||
            r.totalTrades < MIN_TRADES ||
            r.totalReturnPct <= MIN_RETURN ||
            r.profitFactor < MIN_PF
          ) continue;
          results.push({
            gridStepPct: gs, gridMaxGrids: gm, gridPauseAfterLossBars: gp,
            targetRatio: tr, chopGateAdx: cg, leverage, positionFraction,
            totalReturnPct: r.totalReturnPct, maxDrawdownPct: r.maxDrawdownPct,
            totalTrades: r.totalTrades, profitFactor: r.profitFactor,
            score: r.totalReturnPct - 1.0 * r.maxDrawdownPct + 0.02 * r.totalTrades,
          });
        }

results.sort((a, b) => b.score - a.score);

// meta line to stderr
console.error(
  `family=${familyId} lev=${leverage} posFrac=${positionFraction} evaluated=${evaluated} passing=${results.length}`,
);
// results to stdout as JSON array
process.stdout.write(JSON.stringify(results.slice(0, 12), null, 2) + "\n");