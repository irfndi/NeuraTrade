#!/usr/bin/env bun
/**
 * Grid walk-forward anti-overfit validation for the chop-gated BTC 15m winner.
 *
 * Pass A (param stability): per rolling window, re-optimize within the winner's
 *   neighborhood on the train slice, evaluate on the unseen test slice.
 * Pass B (fixed-config stability): run the exact winner config on every test
 *   slice without re-optimization.
 *
 * Usage: bun run scripts/grid-walkforward.ts [--symbol BTC/USDT:USDT] [--timeframe 15m]
 */
import { Database } from "bun:sqlite";
import { join } from "node:path";
import { runGridBacktest, runGridWalkForward } from "../src/scalping/grid.js";
import type { Candle } from "../src/market-data/types.js";

function arg(name: string, dflt: string): string {
  const i = process.argv.indexOf(name);
  return i >= 0 && i + 1 < process.argv.length ? process.argv[i + 1] : dflt;
}
const exchange = arg("--exchange", "bitget-futures");
const symbol = arg("--symbol", "BTC/USDT:USDT");
const timeframe = arg("--timeframe", "15m");

const home =
  process.env.NEURATRADE_HOME ?? join(process.env.HOME!, ".neuratrade");
const db = new Database(join(home, "data", "neuratrade.db"), {
  readonly: true,
});
const rows = db
  .query(
    `SELECT o.open_price, o.high_price, o.low_price, o.close_price, o.volume, o.timestamp
     FROM ohlcv_data o JOIN exchanges e ON e.id=o.exchange_id JOIN trading_pairs tp ON tp.id=o.trading_pair_id
     WHERE e.name = ? AND tp.symbol = ? AND o.timeframe = ? ORDER BY o.timestamp ASC`,
  )
  .all(exchange, symbol, timeframe) as Array<{
  open_price: number;
  high_price: number;
  low_price: number;
  close_price: number;
  volume: number;
  timestamp: string;
}>;
db.close();
if (rows.length === 0) {
  console.error(`No candles for ${exchange}:${symbol}:${timeframe}`);
  process.exit(1);
}
const candles: Candle[] = rows.map((r) => ({
  exchange,
  symbol,
  timeframe,
  open: r.open_price,
  high: r.high_price,
  low: r.low_price,
  close: r.close_price,
  volume: r.volume,
  timestamp: new Date(
    r.timestamp.endsWith("Z")
      ? r.timestamp
      : r.timestamp.replace(" ", "T") + "Z",
  ),
}));

// 120d train / 45d test in 15m bars.
const TRAIN = 120 * 96;
const TEST = 45 * 96;

const baseOptions = {
  feePct: 0.02,
  slippageBps: 1,
  trendFilterPeriod: 0,
  leverage: 1,
  onlyWithTrend: false,
  targetRatio: 1,
  chopGateAdxThreshold: 30,
};

console.log(
  `Walk-forward ${exchange}:${symbol}:${timeframe} — ${candles.length} candles, train ${TRAIN} bars / test ${TEST} bars`,
);

// ---------- Pass A: re-optimize in the winner's neighborhood ----------
const wf = runGridWalkForward(candles, {
  trainWindow: TRAIN,
  testWindow: TEST,
  initialCapital: 10000,
  searchSpace: {
    gridStepPct: [0.75, 1, 1.25],
    gridMaxGrids: [1.5, 2],
    gridPauseAfterLossBars: [6, 12],
  },
  baseOptions,
});

console.log("\n=== Pass A: re-optimized per window (neighborhood) ===");
console.log(
  `${"win".padStart(4)} ${"step".padStart(5)} ${"grids".padStart(6)} ${"pause".padStart(6)} ${"testRet%".padStart(9)} ${"testDD%".padStart(8)} ${"trades".padStart(7)}`,
);
for (let i = 0; i < wf.windows.length; i++) {
  const w = wf.windows[i];
  console.log(
    `${String(i + 1).padStart(4)} ${w.params.gridStepPct.toFixed(2).padStart(5)} ${w.params.gridMaxGrids.toFixed(1).padStart(6)} ${String(w.params.gridPauseAfterLossBars).padStart(6)} ${w.testReturnPct.toFixed(2).padStart(9)} ${w.testMaxDrawdownPct.toFixed(2).padStart(8)} ${String(w.testTrades).padStart(7)}`,
  );
}
console.log(
  `AGGREGATE: return ${wf.aggregateReturnPct.toFixed(2)}% | profitable windows ${wf.profitableWindowsPct.toFixed(0)}% | maxDD ${wf.maxDrawdownPct.toFixed(2)}% | trades ${wf.totalTrades}`,
);

// ---------- Pass B: fixed winner config on each test slice ----------
const winner = {
  ...baseOptions,
  initialCapital: 10000,
  gridStepPct: 1,
  gridMaxGrids: 1.5,
  gridPauseAfterLossBars: 12,
};
console.log(
  "\n=== Pass B: fixed winner config (step 1, grids 1.5, pause 12, gate 30) ===",
);
let profitable = 0;
let windows = 0;
let totalRet = 0;
let maxDd = 0;
let totalTradesB = 0;
for (let start = TRAIN; start + TEST <= candles.length; start += TEST) {
  const slice = candles.slice(start, start + TEST);
  const r = runGridBacktest(slice, winner);
  windows += 1;
  totalRet += r.totalReturnPct;
  maxDd = Math.max(maxDd, r.maxDrawdownPct);
  totalTradesB += r.totalTrades;
  if (r.totalReturnPct > 0) profitable += 1;
  console.log(
    `window ${windows}: ${slice[0].timestamp.toISOString().slice(0, 10)} → ret ${r.totalReturnPct.toFixed(2)}% dd ${r.maxDrawdownPct.toFixed(2)}% trades ${r.totalTrades} win ${r.winRate.toFixed(1)}% PF ${r.profitFactor.toFixed(2)}`,
  );
}
console.log(
  `FIXED: ${profitable}/${windows} profitable windows | mean window ret ${(totalRet / Math.max(1, windows)).toFixed(2)}% | worst DD ${maxDd.toFixed(2)}% | trades ${totalTradesB}`,
);
