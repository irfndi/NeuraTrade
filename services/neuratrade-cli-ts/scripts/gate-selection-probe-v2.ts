#!/usr/bin/env bun
/**
 * GATE-SELECTION VALIDATION PROBE v2 — pre-registered.
 *
 * v1 used an APPROXIMATION of the funnel gates. This version uses the
 * funnel's own code path: computeFillFrequencyPct + validateLadderEvidence
 * + passesLadderGateCriteria on the lookback window ending at each decision
 * bar (exactly what scripts/ladder-whitelist-gatewatch.ts does per symbol),
 * then forward-30d ladder backtests as the label.
 *
 * Pre-registration (unchanged): selection SURVIVES iff in BOTH timeline
 * halves median(selected) - median(rejected) >= +3pp with a non-degenerate
 * pass rate (5%..95%).
 */
import { Database } from "bun:sqlite";
import {
  computeFillFrequencyPct,
  resampleCandles,
} from "../src/scalping/grid-universe.ts";
import { validateLadderEvidence } from "../src/scalping/ladder-validation.ts";
import type { LadderOptions } from "../src/scalping/ladder-grid.ts";
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

console.log(`probe v2: loading top ${TOP_N} symbols...`);
const panel = new Map<string, Candle[]>();
for (const row of symbolRows.slice(0, TOP_N)) {
  const candles = load15m(row.symbol);
  if (candles.length >= 6000) panel.set(row.symbol, candles);
}
if (panel.size < 8) {
  console.error(`probe v2: only ${panel.size} usable symbols`);
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
  if (clipped.length >= 6000) aligned.set(symbol, clipped);
}
const symbols = [...aligned.keys()].sort();
const refLen = Math.min(...symbols.map((s) => aligned.get(s)!.length));
console.log(
  `probe v2: ${symbols.length} symbols, ${refLen} x 15m bars (${new Date(t0).toISOString()} .. ${new Date(t1).toISOString()})`,
);

const TRAIN_BARS = 1440; // 15 days lookback for the evidence validator
const TEST_BARS = 480; // 5 days test window inside the validator
const STEP_BARS = 96;
const FORWARD_BARS = 2880;

// The validated ladder family used across the funnel.
function ladderOpts(): LadderOptions {
  return {
    rungs: 1,
    gridStepPct: 1.0,
    gridMaxGrids: 3,
    gridPauseAfterLossBars: 24,
    feePct: 0.02,
    slippageBps: 2,
    initialCapital: 10_000,
    leverage: 1,
    trendFilterPeriod: 0,
    targetRatio: 2,
    stopRatio: 1.5,
    maxHoldBars: 48,
    conservativeIntrabar: true,
  };
}

/** Funnel's own gate evaluation on history ENDING at endIdx (inclusive). */
function passesFunnelGates(candles: readonly Candle[], endIdx: number): boolean {
  const startIdx = endIdx - TRAIN_BARS - TEST_BARS * 2 + 1;
  if (startIdx < 0) return false;
  const window = candles.slice(startIdx, endIdx + 1);
  if (window.length < TRAIN_BARS + TEST_BARS * 2) return false;

  // Stage-3: fill-frequency floor (funnel uses the same helper).
  const fillPct = computeFillFrequencyPct(window, 1.0, 5);
  if (fillPct < 5) return false;

  // Stage-4 evidence validation with the exact production parameters.
  const n = window.length;
  const minimumWindows = Math.max(
    1,
    Math.floor((n - TRAIN_BARS - TEST_BARS) / TEST_BARS),
  );
  const evidence = validateLadderEvidence(window, {
    now: new Date(),
    timeframeMinutes: 15,
    trainBars: TRAIN_BARS,
    testBars: TEST_BARS,
    minimumWindows,
    ladder: ladderOpts(),
  });
  if (evidence.kind !== "ok") return false;

  // Gate criteria (freshness etc.) — same call gatewatch makes.
  const { passesLadderGateCriteria } =
    require("../src/scalping/grid-universe.ts") as typeof import("../src/scalping/grid-universe.ts");
  return passesLadderGateCriteria(evidence);
}

const outcomes: {
  selectedRet: number[];
  rejectedRet: number[];
}[] = [];
let stepCount = 0;
const firstBar = TRAIN_BARS + TEST_BARS * 2;
const lastStartBar = refLen - FORWARD_BARS - 1;

console.log("probe v2: walking timeline (this is slow — full validators)...");
for (
  let bar = firstBar;
  bar <= lastStartBar && stepCount < MAX_STEPS;
  bar += STEP_BARS, stepCount++
) {
  const selectedRet: number[] = [];
  const rejectedRet: number[] = [];
  for (let s = 0; s < symbols.length; s++) {
    const candles = aligned.get(symbols[s])!;
    const startIdx = candles.length - refLen + bar;
    const endIdx = Math.min(candles.length - 1, startIdx + FORWARD_BARS - 1);
    if (endIdx - startIdx < FORWARD_BARS * 0.9) continue;
    const fwdSlice = candles.slice(startIdx, endIdx + 1);
    const decisionIdx = startIdx - 1;
    if (decisionIdx < firstBar) continue;
    let selected = false;
    try {
      selected = passesFunnelGates(candles, decisionIdx);
    } catch {
      continue;
    }
    const ret = (() => {
      try {
        return runLadderGridBacktest(fwdSlice, ladderOpts()).totalReturnPct;
      } catch {
        return Number.NaN;
      }
    })();
    if (!Number.isFinite(ret)) continue;
    (selected ? selectedRet : rejectedRet).push(ret);
  }
  if (selectedRet.length + rejectedRet.length === 0) continue;
  outcomes.push({ selectedRet, rejectedRet });
}

console.log(`probe v2: ${outcomes.length} steps`);
if (outcomes.length < 25) {
  console.error("probe v2: too few steps (<25)");
  process.exit(3);
}

function median(xs: number[]): number {
  const sorted = xs.filter(Number.isFinite).sort((a, b) => a - b);
  if (sorted.length === 0) return Number.NaN;
  const mid = Math.floor(sorted.length / 2);
  return sorted.length % 2 === 1 ? sorted[mid] : (sorted[mid - 1] + sorted[mid]) / 2;
}

const halfSplit = Math.floor(outcomes.length / 2);
const halves = [
  { name: "H1", ws: outcomes.slice(0, halfSplit) },
  { name: "H2", ws: outcomes.slice(halfSplit) },
];

console.log("\nhalf | n_sel | n_rej | med(sel)% | med(rej)% | margin | passRate");
console.log("-".repeat(72));
const margins: Record<string, number> = {};
const rates: Record<string, number> = {};
for (const h of halves) {
  const sel = h.ws.flatMap((w) => w.selectedRet);
  const rej = h.ws.flatMap((w) => w.rejectedRet);
  const medSel = median(sel);
  const medRej = median(rej);
  const margin = medSel - medRej;
  const rate = sel.length / Math.max(1, sel.length + rej.length);
  margins[h.name] = margin;
  rates[h.name] = rate;
  console.log(
    `${h.name} | ${String(sel.length).padStart(5)} | ${String(rej.length).padStart(5)} | ${medSel.toFixed(2).padStart(9)} | ${medRej.toFixed(2).padStart(9)} | ${margin.toFixed(2).padStart(6)} | ${(rate * 100).toFixed(1)}%`,
  );
}

console.log("\n=== KILL CRITERIA (pre-registered) ===");
const okH1 = margins.H1 >= 3 && rates.H1 >= 0.05 && rates.H1 <= 0.95;
const okH2 = margins.H2 >= 3 && rates.H2 >= 0.05 && rates.H2 <= 0.95;
console.log(
  `H1 ${okH1 ? "OK" : "FAIL"} | H2 ${okH2 ? "OK" : "FAIL"}`,
);
if (okH1 && okH2) {
  console.log("\nVERDICT: SURVIVED — the real funnel gates select better windows.");
} else {
  console.log(
    "\nVERDICT: NULL — even the funnel's own validator does not select better-than-random windows on this data. Selection-layer hypothesis closed.",
  );
}
