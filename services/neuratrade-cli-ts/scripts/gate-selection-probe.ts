#!/usr/bin/env bun
/**
 * GATE-SELECTION VALIDATION PROBE — pre-registered BEFORE running.
 *
 * The decisive question after the DGT probe: broad-universe grid trading has
 * negative median forward returns, so the funnel's ONLY path to edge is its
 * SELECTION layer (stage gates + fill-frequency floor + walk-forward
 * survivorship). Does that selection actually pick windows where the ladder
 * earns more than the unselected pool?
 *
 * Method: at each ~daily step, for each symbol, evaluate the funnel-style
 * criteria on history ENDING at the step (causal), then backtest the ladder
 * over the NEXT 30 days. Split windows into SELECTED (passed all criteria)
 * vs REJECTED. Compare forward-return distributions.
 *
 * Selection rule (fixed now, mirrors the funnel's stage-4 spirit):
 *   - fill frequency: fraction of bars whose [low,high] range spans a rung
 *     level >= 5% over the lookback (the funnel's floor)
 *   - causal walk-forward on the lookback window: split in half, ladder
 *     return on each half must be > -2% (both halves roughly non-losing)
 *   - ADX gate: causal ADX(14) < 24 at the step (chop, not trend)
 *
 * SURVIVAL / SHIP (pre-registered):
 *   H1 AND H2 both satisfy:
 *     median(selected) - median(rejected) >= +3pp   AND
 *     selected pass-rate is not degenerate (>= 5% and <= 95% of windows)
 * Also reported: selected vs buy&hold, hit rate of positive windows.
 *
 * NULL result = the selection layer adds nothing measurable -> the whole
 * "gates find edge" hypothesis is dead at these thresholds, and the strategy
 * review must move to different strategy CLASSES (carry/structural), not
 * better grid tuning.
 */
import { Database } from "bun:sqlite";
import { resampleCandles } from "../src/scalping/grid-universe.ts";
import { makeCausalSymbolStats } from "../src/scalping/symbol-stats.ts";
import { runLadderGridBacktest } from "../src/scalping/ladder-grid.ts";
import type { Candle } from "../src/market-data/types.ts";

const HOME = process.env.NEURATRADE_HOME ?? `${process.env.HOME}/.neuratrade`;

function arg(name: string, fallback: string): string {
  const hit = process.argv.find((a) => a.startsWith(`--${name}=`));
  return hit?.split("=")[1] ?? fallback;
}
const TOP_N = Number(arg("symbols", "16"));
const MAX_STEPS = Number(arg("steps", "150"));

// ---------------------------------------------------------------------------
// Panel loading (same as prior probes)
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
  console.error(`probe: only ${panel.size} usable symbols`);
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
// Causal gate evaluation + forward backtest
// ---------------------------------------------------------------------------

const LADDER_OPTS = {
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

const LOOKBACK = 1344; // 14 days of 15m bars
const STEP_BARS = 96;
const FORWARD_BARS = 2880; // 30 days
const MIN_FILL_FRACTION = 0.05;
const HALF_FLOOR_PCT = -2;

const statsCache = new Map<string, ReturnType<typeof makeCausalSymbolStats>>();
function statsFor(symbol: string) {
  let s = statsCache.get(symbol);
  if (!s) {
    s = makeCausalSymbolStats(aligned.get(symbol)!, "15m");
    statsCache.set(symbol, s);
  }
  return s;
}

/** Ladder return % over an arbitrary candle slice. */
function ladderReturn(slice: readonly Candle[]): number {
  try {
    return runLadderGridBacktest(slice, LADDER_OPTS).totalReturnPct;
  } catch {
    return Number.NaN;
  }
}

/**
 * Causal funnel-style selection using history ending at `endIdx` (inclusive).
 */
function passesGates(candles: readonly Candle[], endIdx: number): boolean {
  const startIdx = Math.max(0, endIdx - LOOKBACK + 1);
  if (endIdx - startIdx + 1 < LOOKBACK * 0.9) return false;
  const window = candles.slice(startIdx, endIdx + 1);

  // 1) Fill-frequency floor: bars whose range spans a rung level.
  const mid = window[window.length - 1].close;
  const step = mid * (LADDER_OPTS.gridStepPct / 100);
  let fills = 0;
  for (const c of window) {
    const levelsBelow = Math.floor((c.high - c.low) / step);
    if (levelsBelow >= 1) fills++;
  }
  if (fills / window.length < MIN_FILL_FRACTION) return false;

  // 2) Causal two-half walk-forward: both halves roughly non-losing.
  const halfPoint = Math.floor(window.length / 2);
  const firstHalfRet = ladderReturn(window.slice(0, halfPoint));
  const secondHalfRet = ladderReturn(window.slice(halfPoint));
  if (
    !Number.isFinite(firstHalfRet) ||
    !Number.isFinite(secondHalfRet) ||
    firstHalfRet < HALF_FLOOR_PCT ||
    secondHalfRet < HALF_FLOOR_PCT
  ) {
    return false;
  }

  // 3) ADX chop gate at the decision bar.
  const adx = statsFor(symbolsCacheKey(candles))(endIdx).adx14;
  if (!(adx < 24)) return false;

  return true;
}

// symbols are keyed by their candle array identity via a side map.
const keyByArray = new Map<readonly Candle[], string>();
for (const [symbol, candles] of aligned) keyByArray.set(candles, symbol);
function symbolsCacheKey(candles: readonly Candle[]): string {
  return keyByArray.get(candles) ?? "";
}

interface WindowOutcome {
  readonly barIndex: number;
  readonly selectedRet: number[];
  readonly rejectedRet: number[];
  readonly bhSelected: number[];
  readonly bhRejected: number[];
}

const outcomes: WindowOutcome[] = [];
let stepCount = 0;
const firstBar = LOOKBACK;
const lastStartBar = refLen - FORWARD_BARS - 1;

console.log("probe: walking timeline (gate evals + forward backtests)...");
for (
  let bar = firstBar;
  bar <= lastStartBar && stepCount < MAX_STEPS;
  bar += STEP_BARS, stepCount++
) {
  const selectedRet: number[] = [];
  const rejectedRet: number[] = [];
  const bhSelected: number[] = [];
  const bhRejected: number[] = [];
  for (let s = 0; s < symbols.length; s++) {
    const candles = aligned.get(symbols[s])!;
    const startIdx = candles.length - refLen + bar;
    const endIdx = Math.min(candles.length - 1, startIdx + FORWARD_BARS - 1);
    if (endIdx - startIdx < FORWARD_BARS * 0.9) continue;
    const fwdSlice = candles.slice(startIdx, endIdx + 1);
    const bh =
      (fwdSlice[fwdSlice.length - 1].close / fwdSlice[0].open - 1) * 100;
    // Gates use history ENDING just before the forward window starts.
    const decisionIdx = startIdx - 1;
    if (decisionIdx < LOOKBACK) continue;
    const selected = passesGates(candles, decisionIdx);
    const ret = ladderReturn(fwdSlice);
    if (!Number.isFinite(ret)) continue;
    if (selected) {
      selectedRet.push(ret);
      bhSelected.push(bh);
    } else {
      rejectedRet.push(ret);
      bhRejected.push(bh);
    }
  }
  if (selectedRet.length + rejectedRet.length === 0) continue;
  outcomes.push({
    barIndex: bar,
    selectedRet,
    rejectedRet,
    bhSelected,
    bhRejected,
  });
}

console.log(`probe: ${outcomes.length} steps evaluated`);
if (outcomes.length < 30) {
  console.error("probe: too few steps (<30)");
  process.exit(3);
}

// ---------------------------------------------------------------------------
// Evaluation
// ---------------------------------------------------------------------------

function median(xs: number[]): number {
  const sorted = xs.filter(Number.isFinite).sort((a, b) => a - b);
  if (sorted.length === 0) return Number.NaN;
  const mid = Math.floor(sorted.length / 2);
  return sorted.length % 2 === 1
    ? sorted[mid]
    : (sorted[mid - 1] + sorted[mid]) / 2;
}

function pooled(
  halfOutcomes: WindowOutcome[],
  pick: (w: WindowOutcome) => number[],
): number[] {
  return halfOutcomes.flatMap(pick);
}

const halfSplit = Math.floor(outcomes.length / 2);
const halves = [
  { name: "H1", ws: outcomes.slice(0, halfSplit) },
  { name: "H2", ws: outcomes.slice(halfSplit) },
];

console.log(
  "\nhalf | n_sel | n_rej | med(sel)% | med(rej)% | margin | sel>BH% | rej>BH% | passRate",
);
console.log("-".repeat(96));
const margins: Record<string, number> = {};
const passRates: Record<string, number> = {};
for (const h of halves) {
  const sel = pooled(h.ws, (w) => w.selectedRet);
  const rej = pooled(h.ws, (w) => w.rejectedRet);
  const bhSel = pooled(h.ws, (w) => w.bhSelected);
  const bhRej = pooled(h.ws, (w) => w.bhRejected);
  const medSel = median(sel);
  const medRej = median(rej);
  const margin = medSel - medRej;
  const total = sel.length + rej.length;
  const passRate = total > 0 ? sel.length / total : NaN;
  const selBeatsBh =
    sel.length > 0
      ? sel.filter((r, i) => Number.isFinite(bhSel[i]) && r > bhSel[i]).length /
        sel.length
      : NaN;
  const rejBeatsBh =
    rej.length > 0
      ? rej.filter((r, i) => Number.isFinite(bhRej[i]) && r > bhRej[i]).length /
        rej.length
      : NaN;
  margins[h.name] = margin;
  passRates[h.name] = passRate;
  console.log(
    `${h.name} | ${String(sel.length).padStart(5)} | ${String(rej.length).padStart(5)} | ${medSel.toFixed(2).padStart(9)} | ${medRej.toFixed(2).padStart(9)} | ${margin.toFixed(2).padStart(6)} | ${(Number.isNaN(selBeatsBh) ? NaN : selBeatsBh * 100).toFixed(1)}% | ${(Number.isNaN(rejBeatsBh) ? NaN : rejBeatsBh * 100).toFixed(1)}% | ${(passRate * 100).toFixed(1)}%`,
  );
}

console.log("\n=== KILL CRITERIA (pre-registered) ===");
const h1Ok = margins.H1 >= 3 && passRates.H1 >= 0.05 && passRates.H1 <= 0.95;
const h2Ok = margins.H2 >= 3 && passRates.H2 >= 0.05 && passRates.H2 <= 0.95;
console.log(
  `H1 margin ${margins.H1.toFixed(2)}pp (need >= +3): ${h1Ok ? "OK" : "FAIL"} | H2 margin ${margins.H2.toFixed(2)}pp: ${h2Ok ? "OK" : "FAIL"}`,
);
if (h1Ok && h2Ok) {
  console.log(
    "\nVERDICT: SURVIVED — the selection layer picks better-than-random windows. Next: fresh-data walk-forward, then consider relaxing gate strictness toward higher pass rates.",
  );
} else {
  console.log(
    "\nVERDICT: NULL — gate selection does not measurably beat the rejected pool at these thresholds. Grid-family edge is not recoverable by selection on this data; strategy review should move to structural/carry classes.",
  );
}
