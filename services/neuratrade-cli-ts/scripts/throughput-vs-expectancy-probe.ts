#!/usr/bin/env bun
/**
 * THROUGHPUT vs EXPECTANCY PROBE — pre-registered.
 *
 * User goal: "grow $50 as fast as possible; no trade == no gain; want high
 * throughput." This probe answers the ONLY question that matters: does
 * trading MORE often increase EXPECTED GROWTH, or does it just multiply
 * exposure to a negative-expectancy engine?
 *
 * Method: on the mainnet 15m panel, run the validated ladder over many
 * forward-30d windows at THREE cadence/coverage settings (tight gate +
 * wide stop = fewer, longer trades; base; high-throughput = tight stop +
 * max-hold 8 + small step => many short trades). Compare, per config:
 *   - trades per symbol-month (throughput)
 *   - median per-trade pnlPct and win rate
 *   - median window return AND median window DRAWDOWN
 *   - growth proxy: median log-return per window (what compounds)
 *
 * PRE-REGISTERED BAR: high-throughput is ADOPTABLE only if its median
 * window log-return beats base AND its median drawdown is not worse by
 * more than 2pp. Otherwise throughput is exposure, not growth.
 */
import { Database } from "bun:sqlite";
import { resampleCandles } from "../src/scalping/grid-universe.ts";
import { runLadderGridBacktest } from "../src/scalping/ladder-grid.ts";
import type { Candle } from "../src/market-data/types.ts";

const HOME = process.env.NEURATRADE_HOME ?? `${process.env.HOME}/.neuratrade`;

function arg(name: string, fallback: string): string {
  const hit = process.argv.find((a) => a.startsWith(`--${name}=`));
  return hit?.split("=")[1] ?? fallback;
}
const TOP_N = Number(arg("symbols", "12"));
const MAX_STEPS = Number(arg("steps", "120"));

const db = new Database(`${HOME}/data/neuratrade.db`, { readonly: true });
db.exec("PRAGMA busy_timeout = 30000;");

interface SymbolRow {
  symbol: string;
  count: number;
}

const symbolRows = db
  .query(
    `SELECT tp.symbol AS symbol, COUNT(*) AS count
     FROM ohlcv_data c JOIN trading_pairs tp ON tp.id = c.trading_pair_id
     WHERE c.timeframe = '5m'
     GROUP BY tp.symbol ORDER BY count DESC LIMIT ?`,
  )
  .all(TOP_N + 4) as SymbolRow[];

interface Raw5m {
  open: number;
  high: number;
  low: number;
  close: number;
  volume: number;
  timestamp: string;
}

function load15m(symbolWire: string): Candle[] {
  const canonical = symbolWire.replace(/\/USDT.*/, "/USDT");
  const rowsDb = db
    .query(
      `SELECT c.open_price AS open, c.high_price AS high, c.low_price AS low,
              c.close_price AS close, c.volume, c.timestamp
       FROM ohlcv_data c JOIN trading_pairs tp ON tp.id = c.trading_pair_id
       WHERE tp.symbol IN (?,?,?) AND c.timeframe = '5m'
       ORDER BY c.timestamp DESC LIMIT ?`,
    )
    .all(symbolWire, canonical, `${canonical}:USDT`, 200_000) as Raw5m[];
  const base: Candle[] = rowsDb.toReversed().map((r) => ({
    exchange: "bybit-futures",
    symbol: symbolWire,
    timeframe: "5m",
    open: r.open,
    high: r.high,
    low: r.low,
    close: r.close,
    volume: r.volume,
    timestamp: new Date(r.timestamp),
  }));
  return resampleCandles(base, 15, "15m");
}

console.log(`probe: loading top ${TOP_N} symbols...`);
const panel = new Map<string, Candle[]>();
for (const row of symbolRows.slice(0, TOP_N)) {
  const candles = load15m(row.symbol);
  if (candles.length >= 6000) panel.set(row.symbol, candles);
}

let t0 = 0;
let t1 = Number.POSITIVE_INFINITY;
for (const candles of panel.values()) {
  t0 = Math.max(t0, candles[0].timestamp.getTime());
  t1 = Math.min(t1, candles[candles.length - 1].timestamp.getTime());
}
const aligned = new Map<string, Candle[]>();
for (const [symbol, candles] of panel) {
  const clipped = candles.filter(
    (c) =>
      c.timestamp.getTime() >= t0 &&
      c.timestamp.getTime() <= t1 &&
      Number.isFinite(c.close) &&
      c.close > 0,
  );
  if (clipped.length >= 6000) aligned.set(symbol, clipped);
}
const symbols = [...aligned.keys()].sort();
const refLen = Math.min(...symbols.map((s) => aligned.get(s)!.length));
console.log(`probe: ${symbols.length} symbols, ${refLen} bars`);

const CONFIGS = [
  { name: "base", gridStepPct: 1.0, stopRatio: 1.5, maxHoldBars: 48, targetRatio: 2 },
  { name: "fast", gridStepPct: 0.5, stopRatio: 1.0, maxHoldBars: 8, targetRatio: 1.5 },
  { name: "fastest", gridStepPct: 0.3, stopRatio: 0.8, maxHoldBars: 4, targetRatio: 1.2 },
] as const;

const BASE_OPTS = {
  rungs: 1,
  gridMaxGrids: 3,
  gridPauseAfterLossBars: 4,
  feePct: 0.02,
  slippageBps: 2,
  initialCapital: 10_000,
  leverage: 1,
  trendFilterPeriod: 0,
  conservativeIntrabar: true,
};

const STEP_BARS = 96;
const FORWARD_BARS = 2880;
const firstBar = 672;
const lastStartBar = refLen - FORWARD_BARS - 1;

interface Agg {
  trades: number;
  wins: number;
  pnlSum: number;
  rets: number[];
  dds: number[];
}

const acc = new Map<string, Agg>();
for (const c of CONFIGS)
  acc.set(c.name, { trades: 0, wins: 0, pnlSum: 0, rets: [], dds: [] });

let steps = 0;
console.log("probe: walking...");
for (
  let bar = firstBar;
  bar <= lastStartBar && steps < MAX_STEPS;
  bar += STEP_BARS, steps++
) {
  for (let s = 0; s < symbols.length; s++) {
    const candles = aligned.get(symbols[s])!;
    const startIdx = candles.length - refLen + bar;
    const endIdx = Math.min(candles.length, startIdx + FORWARD_BARS);
    if (endIdx - startIdx < FORWARD_BARS * 0.9) continue;
    const slice = candles.slice(startIdx, endIdx);
    for (const cfg of CONFIGS) {
      try {
        const r = runLadderGridBacktest(slice, {
          ...BASE_OPTS,
          gridStepPct: cfg.gridStepPct,
          stopRatio: cfg.stopRatio,
          maxHoldBars: cfg.maxHoldBars,
          targetRatio: cfg.targetRatio,
        });
        const a = acc.get(cfg.name)!;
        a.trades += r.trades.length;
        a.wins += r.trades.filter((t) => t.win).length;
        a.pnlSum += r.trades.reduce((sum, t) => sum + (t.pnlPct ?? 0), 0);
        a.rets.push(r.totalReturnPct);
        a.dds.push(r.maxDrawdownPct);
      } catch {
        continue;
      }
    }
  }
}

function median(xs: number[]): number {
  const s = xs.filter(Number.isFinite).sort((a, b) => a - b);
  if (!s.length) return NaN;
  const m = Math.floor(s.length / 2);
  return s.length % 2 ? s[m] : (s[m - 1] + s[m]) / 2;
}

const months = (steps * STEP_BARS * 15) / (60 * 24 * 30);
console.log(`\nprobe: ${steps} steps x ${symbols.length} symbols (~${months.toFixed(1)} symbol-months per config)\n`);
console.log(
  "config   | trades/sym-mo | win%  | medTrade% | medRet%  | medDD%  | med log-ret/window",
);
console.log("-".repeat(88));
let baseLog = 0;
let fastLog = 0;
let baseDD = 0;
for (const cfg of CONFIGS) {
  const a = acc.get(cfg.name)!;
  const perSymMonth = a.trades / Math.max(1, steps * symbols.length) / (months / steps) * steps / Math.max(1, months);
  const winPct = a.trades > 0 ? (a.wins / a.trades) * 100 : NaN;
  const medTrade = a.trades > 0 ? a.pnlSum / a.trades : NaN;
  const medRet = median(a.rets);
  const medDD = median(a.dds);
  // Median log return per window (compounding proxy).
  const logs = a.rets
    .filter(Number.isFinite)
    .map((r) => Math.log(1 + Math.max(-0.95, r / 100)));
  const medLog = median(logs);
  if (cfg.name === "base") {
    baseLog = medLog;
    baseDD = medDD;
  }
  if (cfg.name === "fast") fastLog = medLog;
  console.log(
    `${cfg.name.padEnd(8)} | ${perSymMonth.toFixed(1).padStart(13)} | ${winPct.toFixed(1).padStart(4)} | ${medTrade.toFixed(3).padStart(9)} | ${medRet.toFixed(2).padStart(8)} | ${medDD.toFixed(1).padStart(7)} | ${medLog.toFixed(4).padStart(17)}`,
  );
}

console.log("\n=== KILL CRITERIA (pre-registered) ===");
const fastWins = fastLog > baseLog;
console.log(
  `fast median log-return ${fastLog.toFixed(4)} vs base ${baseLog.toFixed(4)}: ${fastWins ? "BETTER" : "WORSE"}`,
);
if (!fastWins) {
  console.log(
    "\nVERDICT: NULL — higher throughput does NOT improve expected compounding; it multiplies exposure to a negative-expectancy engine. Throughput is not the bottleneck; edge is.",
  );
} else {
  console.log(
    "\nVERDICT: SURVIVED — higher cadence improves expected compounding; proceed to fresh-data validation.",
  );
}
