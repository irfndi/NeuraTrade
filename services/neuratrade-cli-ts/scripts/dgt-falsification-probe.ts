#!/usr/bin/env bun
/**
 * DGT FALSIFICATION PROBE — pre-registered BEFORE running.
 *
 * Tests the testable claim from arXiv 2506.11921 (Dynamic Grid Trading):
 * grids that NEVER stop out (wide/no boundary, no time exit, hold through
 * break-downs) outperform tight-stopped grids. NOTE what we are NOT testing:
 * their "hold the bag" mechanic is only survivable on SPOT at 1x; our live
 * engines are leveraged perps, so a real adoption would still carry a hard
 * liquidation-bounded stop far outside the trading range. At 1x here the
 * comparison is apples-to-apples with the paper's framing.
 *
 * Configs (fixed now):
 *   A validated : stopRatio 1.5, maxHoldBars 48, gridMaxGrids 3   (funnel family)
 *   B dgt_wide  : stopRatio 0,   maxHoldBars 0,  gridMaxGrids 50  (boundary ~51 steps -> never)
 *   C dgt_pure  : stopRatio 0,   maxHoldBars 0,  gridMaxGrids 200 (never)
 *   D loose     : stopRatio 3,   maxHoldBars 96, gridMaxGrids 10  (middle ground)
 *
 * Labels: forward 30d return % per (symbol, step) window at fee 0.02 /
 * slippage 2bps, leverage 1. Plus buy-and-hold per window as benchmark.
 *
 * SURVIVAL (per config): median forward return > 0 in BOTH timeline halves.
 * SHIP (pre-registered): dgt_wide must ALSO
 *   - beat `validated`'s median by >= 1pp in BOTH halves,
 *   - beat buy-and-hold's median in BOTH halves,
 *   - have NO single window worse than -20% (tail check).
 * Anything else: idea closed, no engine change.
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
const TOP_N = Number(arg("symbols", "16"));
const MAX_STEPS = Number(arg("steps", "180"));

// ---------------------------------------------------------------------------
// Panel loading (same data path as the spectral probe)
// ---------------------------------------------------------------------------

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
  if (candles.length >= 3000) panel.set(row.symbol, candles);
}
if (panel.size < 8) {
  console.error(`probe: only ${panel.size} usable symbols, need >= 8`);
  process.exit(2);
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
  if (clipped.length >= 3000) aligned.set(symbol, clipped);
}
const symbols = [...aligned.keys()].sort();
const refLen = Math.min(...symbols.map((s) => aligned.get(s)!.length));
console.log(
  `probe: ${symbols.length} symbols, ${refLen} x 15m bars (${new Date(t0).toISOString()} .. ${new Date(t1).toISOString()})`,
);

// ---------------------------------------------------------------------------
// Configs under test
// ---------------------------------------------------------------------------

const CONFIGS = [
  { name: "validated", stopRatio: 1.5, maxHoldBars: 48, gridMaxGrids: 3 },
  { name: "dgt_wide", stopRatio: 0, maxHoldBars: 0, gridMaxGrids: 50 },
  { name: "dgt_pure", stopRatio: 0, maxHoldBars: 0, gridMaxGrids: 200 },
  { name: "loose", stopRatio: 3, maxHoldBars: 96, gridMaxGrids: 10 },
] as const;

const BASE_OPTS = {
  rungs: 1,
  gridStepPct: 1.0,
  gridPauseAfterLossBars: 24,
  feePct: 0.02,
  slippageBps: 2,
  initialCapital: 10_000,
  leverage: 1,
  trendFilterPeriod: 0,
  targetRatio: 2,
  conservativeIntrabar: true,
};

const STEP_BARS = 96; // ~daily
const FORWARD_BARS = 2880; // 30 days
const firstBar = 672; // skip warmup
const lastStartBar = refLen - FORWARD_BARS - 1;

interface WindowResult {
  readonly barIndex: number;
  readonly returnsPct: Readonly<Record<string, number>>;
  readonly buyHoldPct: number;
}

const windows: WindowResult[] = [];
let stepCount = 0;

console.log("probe: walking timeline...");
for (
  let bar = firstBar;
  bar <= lastStartBar && stepCount < MAX_STEPS;
  bar += STEP_BARS, stepCount++
) {
  const perConfigSum = new Map<string, number>();
  const perConfigCount = new Map<string, number>();
  const bhSum = { v: 0 };
  let bhCount = 0;
  for (let s = 0; s < symbols.length; s++) {
    const candles = aligned.get(symbols[s])!;
    const startIdx = candles.length - refLen + bar;
    const endIdx = Math.min(candles.length, startIdx + FORWARD_BARS);
    if (endIdx - startIdx < FORWARD_BARS * 0.9) continue;
    const slice = candles.slice(startIdx, endIdx);
    // Buy-and-hold benchmark for this window.
    bhSum.v += (slice[slice.length - 1].close / slice[0].open - 1) * 100;
    bhCount++;
    for (const cfg of CONFIGS) {
      try {
        const r = runLadderGridBacktest(slice, {
          ...BASE_OPTS,
          stopRatio: cfg.stopRatio,
          maxHoldBars: cfg.maxHoldBars,
          gridMaxGrids: cfg.gridMaxGrids,
        });
        perConfigSum.set(cfg.name, (perConfigSum.get(cfg.name) ?? 0) + r.totalReturnPct);
        perConfigCount.set(cfg.name, (perConfigCount.get(cfg.name) ?? 0) + 1);
      } catch {
        continue;
      }
    }
  }
  if (bhCount < Math.ceil(symbols.length * 0.7)) continue;
  const returnsPct: Record<string, number> = {};
  for (const cfg of CONFIGS) {
    const n = perConfigCount.get(cfg.name) ?? 0;
    returnsPct[cfg.name] =
      n > 0 ? (perConfigSum.get(cfg.name) ?? 0) / n : Number.NaN;
  }
  windows.push({
    barIndex: bar,
    returnsPct,
    buyHoldPct: bhSum.v / bhCount,
  });
}

console.log(`probe: ${windows.length} usable windows`);
if (windows.length < 40) {
  console.error("probe: too few windows (<40) for any conclusion");
  process.exit(3);
}

// ---------------------------------------------------------------------------
// Evaluation against pre-registration
// ---------------------------------------------------------------------------

function median(xs: number[]): number {
  const sorted = xs.filter(Number.isFinite).sort((a, b) => a - b);
  if (sorted.length === 0) return Number.NaN;
  const mid = Math.floor(sorted.length / 2);
  return sorted.length % 2 === 1
    ? sorted[mid]
    : (sorted[mid - 1] + sorted[mid]) / 2;
}

function minFinite(xs: number[]): number {
  const f = xs.filter(Number.isFinite);
  return f.length > 0 ? Math.min(...f) : Number.NaN;
}

const half = Math.floor(windows.length / 2);
const halves = [
  { name: "H1", ws: windows.slice(0, half) },
  { name: "H2", ws: windows.slice(half) },
];

console.log(
  "\nconfig        | " +
    halves.map((h) => `${h.name} med%  ${h.name} min%`).join(" | ") +
    " | survives",
);
console.log("-".repeat(88));
const medians = new Map<string, { h1: number; h2: number }>();
for (const cfg of CONFIGS) {
  const row: string[] = [];
  const meds: number[] = [];
  let survives = true;
  for (const h of halves) {
    const vals = h.ws.map((w) => w.returnsPct[cfg.name]);
    const med = median(vals);
    meds.push(med);
    const mn = minFinite(vals);
    if (!(med > 0)) survives = false;
    row.push(`${med.toFixed(2).padStart(7)}  ${mn.toFixed(1).padStart(7)}`);
  }
  medians.set(cfg.name, { h1: meds[0], h2: meds[1] });
  console.log(`${cfg.name.padEnd(13)} | ${row.join(" | ")} | ${survives ? "YES" : "-"}`);
}
{
  const row: string[] = [];
  for (const h of halves) {
    const vals = h.ws.map((w) => w.buyHoldPct);
    row.push(
      `${median(vals).toFixed(2).padStart(7)}  ${minFinite(vals).toFixed(1).padStart(7)}`,
    );
  }
  console.log(`${"(buy&hold)".padEnd(13)} | ${row.join(" | ")} |  (benchmark)`);
}

console.log("\n=== KILL CRITERIA (pre-registered) ===");
const val = medians.get("validated")!;
const wide = medians.get("dgt_wide")!;
const bhMed = {
  h1: median(halves[0].ws.map((w) => w.buyHoldPct)),
  h2: median(halves[1].ws.map((w) => w.buyHoldPct)),
};
const worstWideWindow = minFinite(windows.map((w) => w.returnsPct.dgt_wide));
const beatsValidatedBoth =
  wide.h1 - val.h1 >= 1 && wide.h2 - val.h2 >= 1;
const beatsBhBoth = wide.h1 > bhMed.h1 && wide.h2 > bhMed.h2;
const tailOk = worstWideWindow > -20;
console.log(
  `dgt_wide vs validated median margin: H1 ${(wide.h1 - val.h1).toFixed(2)}pp, H2 ${(wide.h2 - val.h2).toFixed(2)}pp (need >= +1pp both)`,
);
console.log(
  `dgt_wide vs buy&hold median: H1 ${wide.h1.toFixed(2)} vs ${bhMed.h1.toFixed(2)}, H2 ${wide.h2.toFixed(2)} vs ${bhMed.h2.toFixed(2)}`,
);
console.log(`worst single dgt_wide window: ${worstWideWindow.toFixed(1)}% (need > -20%)`);
if (beatsValidatedBoth && beatsBhBoth && tailOk) {
  console.log(
    "\nVERDICT: SURVIVED — proceed to fresh-data walk-forward before ANY engine change.",
  );
} else {
  console.log(
    "\nVERDICT: NULL — never-stop grids do not clear the pre-registered bar. Idea closed; validated tight-stop family stays.",
  );
}
