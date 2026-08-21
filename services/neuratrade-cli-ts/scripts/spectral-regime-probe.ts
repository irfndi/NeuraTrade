#!/usr/bin/env bun
/**
 * SPECTRAL REGIME PROBE — falsification attempt, NOT a feature.
 *
 * Question: do cross-asset correlation-spectral features (absorption ratio,
 * effective rank, mean |corr|, spectral gap) predict ladder-gate passage and
 * forward ladder expectancy on 15m crypto data BETTER than the per-symbol
 * ADX chop gate we use today?
 *
 * Method (pre-registered BEFORE looking at results — do not tune after):
 *   - Panel: top-N symbols by 5m candle count in the mainnet cache,
 *     resampled to 15m, aligned on a common time grid.
 *   - Daily-equivalent steps: every 96 bars (~24h), estimate rolling
 *     correlation matrices over 7d (672 bars) and 14d (1344 bars) windows.
 *   - Features per step: AR1 = λ1/Σλ, effective rank = exp(entropy),
 *     meanAbsCorr, spectral gap = λ1/λ2. Per-symbol ADX(14) at the same bar.
 *   - Labels:
 *       A) gate-pass: does the symbol's whitelist row pass stage-4 gates
 *          when evaluated with history ENDING at this step? (proxy for
 *          "would the funnel admit it now")
 *       B) forward PnL: runLadderGridBacktest total return over the NEXT
 *          30 days of that symbol's candles (the thing that matters).
 *   - Predictive test: split each series into FIRST half / SECOND half.
 *     Report Spearman rank corr between each feature and label B computed
 *     ON THE FIRST HALF ONLY, then verify sign/strength holds on the
 *     second half. A feature "survives" only if first-half |ρ| >= 0.15 AND
 *     second-half ρ has the same sign with |ρ| >= 0.10.
 *
 * KILL CRITERIA (decided now): drop the idea unless at least ONE spectral
 * feature survives on BOTH halves AND beats ADX's survival margin by
 * >= 5pp of |ρ| on the first half. Null result = ADX stays, idea closed.
 *
 * Usage:
 *   NEURATRADE_HOME=~/.neuratrade ~/.bun/bin/bun scripts/spectral-regime-probe.ts \
 *     [--symbols 20] [--steps 240]
 */
import { Database } from "bun:sqlite";
import { makeCausalSymbolStats } from "../src/scalping/symbol-stats.ts";
import { resampleCandles } from "../src/scalping/grid-universe.ts";
import { runLadderGridBacktest } from "../src/scalping/ladder-grid.ts";
import type { Candle } from "../src/market-data/types.ts";

const HOME = process.env.NEURATRADE_HOME ?? `${process.env.HOME}/.neuratrade`;

function arg(name: string, fallback: string): string {
  const hit = process.argv.find((a) => a.startsWith(`--${name}=`));
  return hit?.split("=")[1] ?? fallback;
}
const TOP_N = Number(arg("symbols", "16"));
const MAX_STEPS = Number(arg("steps", "200"));

// ---------------------------------------------------------------------------
// Data loading
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

function load5m(symbolWire: string): Candle[] {
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
  const chronological = rowsDb.toReversed();
  const base: Candle[] = chronological.map((r) => ({
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
  return target15m(base);
}

function target15m(base: Candle[]): Candle[] {
  if (base.length === 0) return base;
  return resampleCandles(base, 15, "15m");
}

console.log(`probe: loading top ${TOP_N} symbols by 5m candle count...`);
const panel = new Map<string, Candle[]>();
for (const row of symbolRows.slice(0, TOP_N)) {
  const candles = load5m(row.symbol);
  if (candles.length >= 3000) panel.set(row.symbol, candles);
}
if (panel.size < 6) {
  console.error(
    `probe: only ${panel.size} symbols have enough history — need >= 6 for a meaningful spectrum`,
  );
  process.exit(2);
}

// Align all symbols to their COMMON time range so the correlation matrix is
// meaningful at every step.
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
      // Drop any leading partial-resample artifacts.
      Number.isFinite(c.close) &&
      c.close > 0,
  );
  if (clipped.length >= 3000) aligned.set(symbol, clipped);
}
const symbols = [...aligned.keys()].sort();
const N = symbols.length;
const refLen = Math.min(...symbols.map((s) => aligned.get(s)!.length));
console.log(
  `probe: ${N} symbols aligned, ${refLen} x 15m bars common (${new Date(t0).toISOString()} .. ${new Date(t1).toISOString()})`,
);

// Simple returns matrix: rets[barIndex][symbolIndex]
const rets: number[][] = [];
const closes: number[][] = [];
for (let b = 0; b < refLen; b++) {
  const rowR: number[] = [];
  const rowC: number[] = [];
  let ok = true;
  for (let s = 0; s < N; s++) {
    const candles = aligned.get(symbols[s])!;
    // Symbols may differ slightly in length; index from the END so all
    // series end at t1.
    const c = candles[candles.length - refLen + b];
    if (c === undefined) {
      ok = false;
      break;
    }
    rowC.push(c.close);
    rowR.push(NaN); // filled below
  }
  if (!ok) break;
  for (let s = 0; s < N; s++) {
    const prev = b === 0 ? NaN : closes[b - 1][s];
    rowR[s] = Number.isFinite(prev) && prev > 0 ? rowC[s] / prev - 1 : 0;
  }
  rets.push(rowR);
  closes.push(rowC);
}

// ---------------------------------------------------------------------------
// Linear algebra: symmetric eigendecomposition (Jacobi)
// ---------------------------------------------------------------------------

function eigenvaluesSymmetric(matrix: number[][]): number[] {
  const n = matrix.length;
  const a = matrix.map((row) => [...row]);
  for (let sweep = 0; sweep < 50; sweep++) {
    let off = 0;
    for (let p = 0; p < n; p++)
      for (let q = p + 1; q < n; q++) off += a[p][q] * a[p][q];
    if (off < 1e-14) break;
    for (let p = 0; p < n; p++) {
      for (let q = p + 1; q < n; q++) {
        if (Math.abs(a[p][q]) < 1e-12) continue;
        const theta = (a[q][q] - a[p][p]) / (2 * a[p][q]);
        const t =
          Math.sign(theta || 1) / (Math.abs(theta) + Math.sqrt(theta * theta + 1));
        const c = 1 / Math.sqrt(t * t + 1);
        const s = t * c;
        for (let k = 0; k < n; k++) {
          const akp = a[k][p];
          const akq = a[k][q];
          a[k][p] = c * akp - s * akq;
          a[k][q] = s * akp + c * akq;
        }
        for (let k = 0; k < n; k++) {
          const apk = a[p][k];
          const aqk = a[q][k];
          a[p][k] = c * apk - s * aqk;
          a[q][k] = s * apk + c * aqk;
        }
      }
    }
  }
  return Array.from({ length: n }, (_, i) => a[i][i]).sort((x, y) => y - x);
}

function corrMatrixFrom(startBar: number, windowBars: number): number[][] {
  // Pearson correlations of returns over [startBar, startBar+windowBars).
  const means = new Array<number>(N).fill(0);
  let count = 0;
  for (let b = startBar; b < startBar + windowBars && b < rets.length; b++) {
    for (let s = 0; s < N; s++) means[s] += rets[b][s];
    count++;
  }
  if (count < windowBars * 0.9) throw new Error("insufficient window");
  for (let s = 0; s < N; s++) means[s] /= count;
  const cov = Array.from({ length: N }, () => new Array<number>(N).fill(0));
  const vars = new Array<number>(N).fill(0);
  for (let b = startBar; b < startBar + windowBars && b < rets.length; b++) {
    for (let i = 0; i < N; i++) {
      const di = rets[b][i] - means[i];
      vars[i] += di * di;
      for (let j = i + 1; j < N; j++) {
        const dj = rets[b][j] - means[j];
        cov[i][j] += di * dj;
      }
    }
  }
  const C = Array.from({ length: N }, () => new Array<number>(N).fill(1));
  for (let i = 0; i < N; i++) {
    for (let j = i + 1; j < N; j++) {
      const denom = Math.sqrt(vars[i] * vars[j]);
      const r = denom > 0 ? cov[i][j] / denom : 0;
      C[i][j] = r;
      C[j][i] = r;
    }
  }
  return C;
}

interface SpectralFeatures {
  ar1: number;
  effRank: number;
  meanAbsCorr: number;
  gap: number;
}

function spectralFeatures(C: number[][]): SpectralFeatures {
  const eigs = eigenvaluesSymmetric(C);
  const total = eigs.reduce((sum, v) => sum + Math.max(0, v), 0);
  const ar1 = total > 0 ? eigs[0] / total : 1;
  let entropy = 0;
  for (const v of eigs) {
    const share = total > 0 ? Math.max(0, v) / total : 0;
    if (share > 0) entropy -= share * Math.log(share);
  }
  const effRank = Math.exp(entropy);
  let sum = 0;
  let pairs = 0;
  for (let i = 0; i < N; i++)
    for (let j = i + 1; j < N; j++) {
      sum += Math.abs(C[i][j]);
      pairs++;
    }
  const meanAbsCorr = pairs > 0 ? sum / pairs : 0;
  const gap = eigs[1] > 1e-12 ? eigs[0] / eigs[1] : 999;
  return { ar1, effRank, meanAbsCorr, gap };
}

// ---------------------------------------------------------------------------
// Walk the timeline
// ---------------------------------------------------------------------------

const WINDOW_7D = 672; // 15m bars
const STEP_BARS = 96; // ~daily
const FORWARD_BARS = 2880; // 30 days forward backtest

interface StepObservation {
  readonly barIndex: number;
  readonly adxMedian: number;
  readonly spectral: SpectralFeatures;
  /** Forward 30d ladder return %, averaged across symbols (label B). */
  readonly forwardReturnPct: number;
  /** Fraction of symbols whose ladder was net-profitable forward. */
  readonly forwardWinFraction: number;
}

const observations: StepObservation[] = [];
const statsCache = new Map<string, ReturnType<typeof makeCausalSymbolStats>>();
function statsFor(symbol: string) {
  let s = statsCache.get(symbol);
  if (!s) {
    s = makeCausalSymbolStats(aligned.get(symbol)!, "15m");
    statsCache.set(symbol, s);
  }
  return s;
}

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

console.log("probe: walking timeline (this runs many small backtests)...");
const firstBar = WINDOW_7D;
const lastStartBar = refLen - FORWARD_BARS - 1;
let stepCount = 0;
for (
  let bar = firstBar;
  bar <= lastStartBar && stepCount < MAX_STEPS;
  bar += STEP_BARS, stepCount++
) {
  let C: number[][];
  try {
    C = corrMatrixFrom(bar - WINDOW_7D, WINDOW_7D);
  } catch {
    continue;
  }
  const spectral = spectralFeatures(C);

  // Per-symbol forward ladder backtests + median ADX at this step.
  let retSum = 0;
  let wins = 0;
  let counted = 0;
  const adxs: number[] = [];
  for (let s = 0; s < N; s++) {
    const symbol = symbols[s];
    const candles = aligned.get(symbol)!;
    const startIdx = candles.length - refLen + bar;
    const endIdx = Math.min(candles.length, startIdx + FORWARD_BARS);
    if (endIdx - startIdx < FORWARD_BARS * 0.9) continue;
    try {
      const result = runLadderGridBacktest(
        candles.slice(startIdx, endIdx),
        LADDER_OPTS,
      );
      retSum += result.totalReturnPct;
      counted++;
      if (result.totalReturnPct > 0) wins++;
    } catch {
      continue;
    }
    const st = statsFor(symbol)(startIdx);
    if (Number.isFinite(st.adx14)) adxs.push(st.adx14);
  }
  if (counted < Math.ceil(N * 0.7)) continue;
  adxs.sort((a, b) => a - b);
  const adxMedian =
    adxs.length % 2 === 1
      ? adxs[(adxs.length - 1) / 2]
      : (adxs[adxs.length / 2 - 1] + adxs[adxs.length / 2]) / 2;
  observations.push({
    barIndex: bar,
    adxMedian: adxMedian ?? NaN,
    spectral,
    forwardReturnPct: retSum / counted,
    forwardWinFraction: wins / counted,
  });
}

console.log(`probe: ${observations.length} usable steps`);
if (observations.length < 40) {
  console.error(
    "probe: too few steps for any conclusion (<40) — extend history or reduce --steps granularity",
  );
  process.exit(3);
}

// ---------------------------------------------------------------------------
// Predictive evaluation: Spearman rho, first half -> second half
// ---------------------------------------------------------------------------

function spearman(xs: number[], ys: number[]): number {
  const n = xs.length;
  const rank = (arr: number[]): number[] => {
    const idx = arr.map((v, i) => [v, i] as const).sort((a, b) => a[0] - b[0]);
    const ranks = new Array<number>(n).fill(0);
    let i = 0;
    while (i < n) {
      let j = i;
      while (j + 1 < n && idx[j + 1][0] === idx[i][0]) j++;
      const r = (i + j) / 2 + 1;
      for (let k = i; k <= j; k++) ranks[idx[k][1]] = r;
      i = j + 1;
    }
    return ranks;
  };
  const rx = rank(xs);
  const ry = rank(ys);
  let num = 0;
  let dx = 0;
  let dy = 0;
  for (let k = 0; k < n; k++) {
    const a = rx[k] - ry[k];
    num += a * a;
    dx += rx[k] * rx[k];
    dy += ry[k] * ry[k];
  }
  if (dx === 0 || dy === 0) return 0;
  // Pearson on ranks.
  const mx = rx.reduce((s, v) => s + v, 0) / n;
  const my = ry.reduce((s, v) => s + v, 0) / n;
  let cov = 0;
  let vx = 0;
  let vy = 0;
  for (let k = 0; k < n; k++) {
    cov += (rx[k] - mx) * (ry[k] - my);
    vx += (rx[k] - mx) ** 2;
    vy += (ry[k] - my) ** 2;
  }
  void num;
  void dx;
  void dy;
  return vx > 0 && vy > 0 ? cov / Math.sqrt(vx * vy) : 0;
}

const features: { name: string; get: (o: StepObservation) => number }[] = [
  { name: "adx_median", get: (o) => o.adxMedian },
  { name: "ar1", get: (o) => o.spectral.ar1 },
  { name: "eff_rank", get: (o) => o.spectral.effRank },
  { name: "mean_abs_corr", get: (o) => o.spectral.meanAbsCorr },
  { name: "spectral_gap", get: (o) => o.spectral.gap },
];

const labels: { name: string; get: (o: StepObservation) => number }[] = [
  { name: "fwd_return", get: (o) => o.forwardReturnPct },
  { name: "fwd_win_fraction", get: (o) => o.forwardWinFraction },
];

const half = Math.floor(observations.length / 2);
const firstHalf = observations.slice(0, half);
const secondHalf = observations.slice(half);

console.log(
  `\nfeature            | label             | rho_H1  | rho_H2  | verdict`,
);
console.log("-".repeat(72));
const survival = new Map<string, { h1: number; h2: number }>();
for (const f of features) {
  for (const l of labels) {
    const rhoH1 = spearman(firstHalf.map(f.get), firstHalf.map(l.get));
    const rhoH2 = spearman(secondHalf.map(f.get), secondHalf.map(l.get));
    const survives =
      Math.abs(rhoH1) >= 0.15 &&
      Math.abs(rhoH2) >= 0.1 &&
      Math.sign(rhoH1) === Math.sign(rhoH2);
    survival.set(`${f.name}|${l.name}`, { h1: rhoH1, h2: rhoH2 });
    console.log(
      `${f.name.padEnd(18)} | ${l.name.padEnd(17)} | ${rhoH1.toFixed(3).padStart(7)} | ${rhoH2.toFixed(3).padStart(7)} | ${survives ? "SURVIVES" : "-"}`,
    );
  }
}

// KILL CRITERIA evaluation (pre-registered).
console.log("\n=== KILL CRITERIA (pre-registered) ===");
let anySpectralSurvives = false;
for (const key of survival.keys()) {
  if (key.startsWith("adx")) continue;
  const s = survival.get(key)!;
  if (
    Math.abs(s.h1) >= 0.15 &&
    Math.abs(s.h2) >= 0.1 &&
    Math.sign(s.h1) === Math.sign(s.h2)
  ) {
    anySpectralSurvives = true;
  }
}
const adxFwd = survival.get("adx_median|fwd_return")!;
let bestSpectral = 0;
for (const [key, s] of survival) {
  if (key.startsWith("adx")) continue;
  bestSpectral = Math.max(bestSpectral, Math.abs(s.h1));
}
const beatsAdxBy = bestSpectral - Math.abs(adxFwd.h1);
console.log(
  `any spectral feature survives both halves: ${anySpectralSurvives ? "YES" : "NO"}`,
);
console.log(
  `best spectral |rho_H1|=${bestSpectral.toFixed(3)} vs ADX ${Math.abs(adxFwd.h1).toFixed(3)} -> margin ${(beatsAdxBy * 100).toFixed(1)}pp`,
);
if (!anySpectralSurvives || beatsAdxBy < 0.05) {
  console.log(
    "\nVERDICT: NULL — spectral regime features do NOT beat the ADX baseline by the required margin. Idea closed; no funnel change.",
  );
  process.exit(0);
}
console.log(
  "\nVERDICT: SURVIVED pre-registered criteria — proceed to fresh-data walk-forward before ANY integration.",
);
