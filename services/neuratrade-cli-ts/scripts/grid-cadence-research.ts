#!/usr/bin/env bun
/**
 * Grid cadence x win-rate research sweep (ephemeral research, not committed).
 *
 * Sweeps grid params (step, grids, pause, chop-gate ADX, target ratio) over
 * full window, recent window, and an OOS train/test split. Reports win rate,
 * profit factor, total return, expectancy per fill, and projected fills/day.
 *
 * Usage:
 *   bun run scripts/grid-cadence-research.ts \
 *     --exchange bitget-futures --symbol BTC/USDT:USDT --timeframe 15m \
 *     [--full-start 2025-07-17 --full-end 2026-08-07] \
 *     [--recent-start 2026-07-01 --recent-end 2026-08-07] \
 *     [--oos-train 2025-07-17 --oos-test 2026-02-01 --oos-end 2026-08-07] \
 *     [--fee 0.06 --slippage-bps 2 --capital 1000] [--out path]
 */
import { Database } from "bun:sqlite";
import { mkdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { runGridBacktest } from "../src/scalping/grid.js";
import type { GridOptions } from "../src/scalping/grid.js";
import type { Candle } from "../src/market-data/types.js";

interface Args {
  exchange: string;
  symbol: string;
  timeframe: string;
  fee: number;
  slippageBps: number;
  capital: number;
  fullStart: string;
  fullEnd: string;
  recentStart: string;
  recentEnd: string;
  oosTrainEnd: string;
  oosTestStart: string;
  oosEnd: string;
  out: string;
  steps: number[];
  grids: number[];
  pauses: number[];
  gates: number[];
  targets: number[];
}

function parseArgs(argv: readonly string[]): Args {
  const get = (name: string, dflt: string): string => {
    const i = argv.indexOf(name);
    return i >= 0 && i + 1 < argv.length ? argv[i + 1] : dflt;
  };
  const numList = (name: string, dflt: string): number[] =>
    get(name, dflt)
      .split(",")
      .map((s) => Number(s))
      .filter((n) => Number.isFinite(n));
  const home =
    process.env.NEURATRADE_HOME ?? join(process.env.HOME!, ".neuratrade");
  return {
    exchange: get("--exchange", "bitget-futures"),
    symbol: get("--symbol", "BTC/USDT:USDT"),
    timeframe: get("--timeframe", "15m"),
    fee: Number(get("--fee", "0.06")),
    slippageBps: Number(get("--slippage-bps", "2")),
    capital: Number(get("--capital", "1000")),
    fullStart: get("--full-start", ""),
    fullEnd: get("--full-end", ""),
    recentStart: get("--recent-start", "2026-07-01"),
    recentEnd: get("--recent-end", "2026-08-07"),
    oosTrainEnd: get("--oos-train", "2026-02-01"),
    oosTestStart: get("--oos-test", "2026-02-01"),
    oosEnd: get("--oos-end", "2026-08-07"),
    out: get("--out", join(home, "tuning", "grid-cadence-research.json")),
    steps: numList("--steps", "0.1,0.15,0.2,0.3,0.5,0.75,1.0"),
    grids: numList("--grids", "1,2,3"),
    pauses: numList("--pauses", "0,6,24,36"),
    gates: numList("--gates", "0,10,15,20"),
    targets: numList("--targets", "1.0,1.5"),
  };
}

const args = parseArgs(process.argv.slice(2));
const home =
  process.env.NEURATRADE_HOME ?? join(process.env.HOME!, ".neuratrade");
const db = new Database(join(home, "data", "neuratrade.db"), { readonly: true });

const candleRows = db
  .query(
    `SELECT o.open_price, o.high_price, o.low_price, o.close_price, o.volume, o.timestamp
     FROM ohlcv_data o
     JOIN exchanges e ON e.id = o.exchange_id
     JOIN trading_pairs tp ON tp.id = o.trading_pair_id
     WHERE e.name = ? AND tp.symbol = ? AND o.timeframe = ?
     ORDER BY o.timestamp ASC`,
  )
  .all(args.exchange, args.symbol, args.timeframe) as Array<{
  open_price: number;
  high_price: number;
  low_price: number;
  close_price: number;
  volume: number;
  timestamp: string;
}>;
db.close();

if (candleRows.length === 0) {
  console.error(`No candles for ${args.exchange}:${args.symbol}:${args.timeframe}`);
  process.exit(1);
}

const candles: Candle[] = candleRows.map((r) => ({
  exchange: args.exchange,
  symbol: args.symbol,
  timeframe: args.timeframe,
  open: r.open_price,
  high: r.high_price,
  low: r.low_price,
  close: r.close_price,
  volume: r.volume,
  timestamp: new Date(
    r.timestamp.endsWith("Z") ? r.timestamp : r.timestamp.replace(" ", "T") + "Z",
  ),
}));

const inRange = (start: string, end: string): Candle[] => {
  const from = start ? new Date(start + "T00:00:00Z").getTime() : -Infinity;
  const to = end ? new Date(end + "T00:00:00Z").getTime() : Infinity;
  return candles.filter((c) => {
    const t = c.timestamp.getTime();
    return t >= from && t <= to;
  });
};

const daysBetween = (a: Candle, b: Candle): number =>
  Math.max((b.timestamp.getTime() - a.timestamp.getTime()) / 86400000, 1e-9);

interface WindowResult {
  name: string;
  candles: Candle[];
  days: number;
}

const windows: WindowResult[] = [];
if (args.fullStart || args.fullEnd) {
  const w = inRange(args.fullStart, args.fullEnd);
  if (w.length) windows.push({ name: "full", candles: w, days: daysBetween(w[0], w[w.length - 1]) });
}
const rec = inRange(args.recentStart, args.recentEnd);
if (rec.length) windows.push({ name: "recent", candles: rec, days: daysBetween(rec[0], rec[rec.length - 1]) });
// OOS split
const oosTrain = inRange(args.oosTrainEnd ? "" : "", args.oosTrainEnd);
const oosTest = inRange(args.oosTestStart, args.oosEnd);
if (oosTrain.length && oosTest.length) {
  windows.push({ name: "oos-train", candles: oosTrain, days: daysBetween(oosTrain[0], oosTrain[oosTrain.length - 1]) });
  windows.push({ name: "oos-test", candles: oosTest, days: daysBetween(oosTest[0], oosTest[oosTest.length - 1]) });
}
if (windows.length === 0) {
  windows.push({ name: "full", candles, days: daysBetween(candles[0], candles[candles.length - 1]) });
}

interface Row {
  step: number;
  grids: number;
  pause: number;
  gate: number;
  target: number;
  [k: string]: unknown;
}

interface Metric {
  trades: number;
  winRate: number;
  profitFactor: number;
  returnPct: number;
  maxDdPct: number;
  expPerFill: number;
  fillsPerDay: number;
}

function runMetrics(cs: Candle[], opts: GridOptions): Metric {
  const r = runGridBacktest(cs, opts);
  const days = daysBetween(cs[0], cs[cs.length - 1]);
  return {
    trades: r.totalTrades,
    winRate: r.winRate,
    profitFactor: r.profitFactor,
    returnPct: r.totalReturnPct,
    maxDdPct: r.maxDrawdownPct,
    expPerFill: r.totalTrades > 0 ? r.totalReturnPct / r.totalTrades : 0,
    fillsPerDay: r.totalTrades / days,
  };
}

const rows: Row[] = [];
let tested = 0;
const total =
  args.steps.length * args.grids.length * args.pauses.length * args.gates.length * args.targets.length;

for (const step of args.steps) {
  for (const grids of args.grids) {
    for (const pause of args.pauses) {
      for (const gate of args.gates) {
        for (const target of args.targets) {
          const row: Row = { step, grids, pause, gate, target };
          for (const w of windows) {
            const opts: GridOptions = {
              gridStepPct: step,
              gridMaxGrids: grids,
              gridPauseAfterLossBars: pause,
              feePct: args.fee,
              slippageBps: args.slippageBps,
              initialCapital: args.capital,
              trendFilterPeriod: 0,
              leverage: 1,
              targetRatio: target,
              chopGateAdxThreshold: gate,
            };
            row[w.name] = runMetrics(w.candles, opts);
          }
          rows.push(row);
          tested += 1;
          if (tested % 200 === 0 || tested === total) {
            console.log(`progress ${tested}/${total}`);
          }
        }
      }
    }
  }
}

console.log(
  `${args.symbol} ${args.timeframe}: ${windows.map((w) => `${w.name}=${w.candles.length}cd/${w.days.toFixed(0)}d`).join(" ")}`,
);
// Print a compact table: for each window, show fills/d, win%, PF, ret%, exp/fill.
const headerWin = windows.map(() => "fills/d win%  PF   ret%   exp/fill").join("  ");
console.log(
  `${"step".padStart(5)} ${"grids".padStart(5)} ${"pause".padStart(5)} ${"gate".padStart(4)} ${"tgt".padStart(4)} ${headerWin}`,
);
for (const r of rows) {
  const cells = windows
    .map((w) => {
      const m = r[w.name] as Metric;
      return `${m.fillsPerDay.toFixed(1).padStart(6)} ${m.winRate.toFixed(0).padStart(4)}% ${m.profitFactor.toFixed(2).padStart(5)} ${m.returnPct.toFixed(1).padStart(6)}% ${m.expPerFill.toFixed(3).padStart(7)}`;
    })
    .join("  ");
  console.log(
    `${String(r.step).padStart(5)} ${String(r.grids).padStart(5)} ${String(r.pause).padStart(5)} ${String(r.gate).padStart(4)} ${String(r.target).padStart(4)} ${cells}`,
  );
}

mkdirSync(dirname(args.out), { recursive: true });
await Bun.write(args.out, JSON.stringify({ args, windows: windows.map((w) => ({ name: w.name, candles: w.candles.length, days: w.days })), rows }, null, 2));
console.log(`\nWrote ${args.out}`);