#!/usr/bin/env bun
/**
 * Maker-fill realism stress for the chop-gated BTC 15m grid winner.
 *
 * The grid's entire edge comes from MAKER/LIMIT fills modeled optimistically:
 * "touched = 100% filled at 1bp slippage, round-trip fee = 2x the per-side fee".
 * This script measures how the out-of-sample edge degrades as those assumptions
 * are made realistic:
 *   - slippage sweep (1 -> 10 bps, hits entry AND exit so 2x per trade)
 *
 * If the OOS edge evaporates under conservative slippage, the "winner" is a
 * fill-assumption artifact rather than a tradable edge, and the readiness
 * verdict must reflect that (bd cleaver-cabin readiness Phase 3 caveat).
 *
 * Usage: bun run scripts/grid-fill-stress.ts [--symbol BTC/USDT:USDT] [--timeframe 15m]
 */
import { Database } from "bun:sqlite";
import { join } from "node:path";
import { runGridBacktest, type GridResult } from "../src/scalping/grid.js";
import { splitCandlesByOos } from "../src/scalping/backtest.js";
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
const db = new Database(join(home, "data", "neuratrade.db"), { readonly: true });
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

const winner = {
  feePct: 0.02,
  slippageBps: 1,
  trendFilterPeriod: 0,
  leverage: 1,
  onlyWithTrend: false,
  targetRatio: 1,
  chopGateAdxThreshold: 30,
  initialCapital: 10_000,
  gridStepPct: 1,
  gridMaxGrids: 1.5,
  gridPauseAfterLossBars: 12,
};

const { is, oos } = splitCandlesByOos(candles, 20);

function expectancyPct(r: GridResult): number {
  if (r.trades.length === 0) return 0;
  return (r.trades.reduce((s, t) => s + t.pnlPct, 0) / r.trades.length) * 100;
}

function line(label: string, r: GridResult): string {
  return (
    `${label.padEnd(6)} ret ${r.totalReturnPct.toFixed(2).padStart(7)}% ` +
    `tr ${String(r.totalTrades).padStart(4)} win ${r.winRate.toFixed(1).padStart(5)}% ` +
    `PF ${r.profitFactor.toFixed(2).padStart(5)} exp ${expectancyPct(r).toFixed(3).padStart(7)}%/tr ` +
    `DD ${r.maxDrawdownPct.toFixed(1).padStart(5)}%`
  );
}

console.log(
  `Grid fill stress ${exchange}:${symbol}:${timeframe} — ${candles.length} candles ` +
    `(IS ${is.length} bars / OOS ${oos.length} bars, last 20%)\n`,
);

console.log("=== Slippage sweep (winner config, round-trip fee fixed at 0.04%) ===");
console.log(
  `${"slip".padEnd(6)} ${"--- IN-SAMPLE ---".padEnd(58)} ${"--- OUT-OF-SAMPLE ---"}`,
);
for (const slip of [1, 2, 3, 4, 5, 7, 10]) {
  const isR = runGridBacktest(is, { ...winner, slippageBps: slip });
  const oosR = runGridBacktest(oos, { ...winner, slippageBps: slip });
  console.log(`${String(slip).padEnd(6)} ${line("IS", isR)}   ${line("OOS", oosR)}`);
}

// Walk-forward Pass B: fixed winner config on rolling 45d windows across all
// regimes — robustness check the single 80/20 OOS slice cannot provide.
const TRAIN = 120 * 96;
const TEST = 45 * 96;
console.log(
  "\n=== Walk-forward Pass B (FIXED winner config) across all 45d windows ===",
);
console.log(
  `${"slip".padEnd(6)} ${"profit".padStart(8)} ${"meanRet%".padStart(9)} ${"worstDD%".padStart(9)} ${"trades".padStart(7)}`,
);
for (const slip of [1, 2, 3, 5, 8, 10]) {
  let profitable = 0;
  let windows = 0;
  let totalRet = 0;
  let maxDd = 0;
  let trades = 0;
  for (let start = TRAIN; start + TEST <= candles.length; start += TEST) {
    const slice = candles.slice(start, start + TEST);
    const r = runGridBacktest(slice, { ...winner, slippageBps: slip });
    windows += 1;
    totalRet += r.totalReturnPct;
    maxDd = Math.max(maxDd, r.maxDrawdownPct);
    trades += r.totalTrades;
    if (r.totalReturnPct > 0) profitable += 1;
  }
  console.log(
    `${String(slip).padEnd(6)} ${(profitable + "/" + windows).padStart(8)} ` +
      `${(totalRet / Math.max(1, windows)).toFixed(2).padStart(9)} ` +
      `${maxDd.toFixed(2).padStart(9)} ${String(trades).padStart(7)}`,
  );
}
