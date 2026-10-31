/**
 * Frozen evaluation harness (Karpathy prepare.py analogue).
 * Do not edit during autoresearch trials — change knobs.ts only.
 *
 * Speed: load the 15m panel ONCE via loadAlignedPanel(), then evaluateKnobsOnPanel().
 * Two-phase: screen (short forward) then confirm (full 30d) on KEEP candidates.
 * Claim gate: confirm candidates that meet claim bars are evaluated ONCE on the
 * frozen final holdout (trailing HOLDOUT_BARS, never touched by screen/confirm).
 *
 * Venue: the panel is filtered to a single live exchange + source timeframe
 * (default bybit-futures / 5m). Scores are bound to that dataset via
 * DatasetProvenance (exchange + timeframe + panel hash) persisted on the champion;
 * a panel change invalidates old, incomparable scores.
 */
import { Database } from "bun:sqlite";
import { resampleCandles } from "../src/scalping/grid-universe.ts";
import { runLadderGridBacktest } from "../src/scalping/ladder-grid.ts";
import {
  returnMoments,
  sharpeFromReturns,
} from "../src/scalping/deflated-sharpe.ts";
import type { Candle } from "../src/market-data/types.ts";
import type { AutoresearchKnobs } from "./knobs.ts";
import {
  checkKeepGuards,
  type GuardCheckResult,
  type GuardInput,
} from "./goals.ts";

export type EvalPhase = "screen" | "confirm" | "holdout";

export interface EvaluateOptions {
  readonly symbols?: number;
  readonly maxSteps?: number;
  readonly budgetSec?: number;
  readonly dbPath?: string;
  readonly phase?: EvalPhase;
  /** Preloaded panel — skips DB I/O when set. */
  readonly panel?: AlignedPanel;
  /** Venue filter for panel loads (ignored when panel is set). */
  readonly exchange?: string;
  readonly timeframe?: string;
}

export interface EvaluateResult {
  readonly score: number;
  readonly guardsOk: boolean;
  readonly medianLogReturn: number;
  readonly medianReturnPct: number;
  readonly medianDrawdownPct: number;
  readonly winRatePct: number;
  readonly tradesPerSymMonth: number;
  readonly expectancyPct: number;
  /**
   * Per-period Sharpe of the window-return series (undefined when there are too
   * few windows or zero dispersion). This is the input the Deflated Sharpe
   * Ratio needs — `score` is a location statistic and must never be fed to it.
   */
  readonly sharpe?: number;
  /** Skewness of the window returns. */
  readonly skewness?: number;
  /** RAW kurtosis of the window returns (3 = normal). */
  readonly kurtosis?: number;
  readonly windows: number;
  readonly symbols: number;
  readonly steps: number;
  readonly elapsedMs: number;
  readonly reason: string;
  readonly phase: EvalPhase;
}

export interface AlignedPanel {
  readonly symbols: readonly string[];
  readonly aligned: ReadonlyMap<string, Candle[]>;
  readonly refLen: number;
  readonly loadedMs: number;
  /** Venue provenance — binds every score to the dataset that produced it. */
  readonly exchange: string;
  /** Source timeframe read from the DB (resampled to panelTimeframe). */
  readonly timeframe: string;
  readonly panelTimeframe: string;
  /** Hash over venue + symbols + span + length. Changes => scores incomparable. */
  readonly panelHash: string;
  /** Trailing bars reserved as the frozen final holdout (untouched by selection). */
  readonly holdoutBars: number;
}

/** Live venue — the only exchange whose candles may enter the panel. */
export const DEFAULT_EXCHANGE = "bybit-futures" as const;
/** Source timeframe read from the DB (resampled to PANEL_TIMEFRAME). */
export const DEFAULT_TIMEFRAME = "5m" as const;
export const PANEL_TIMEFRAME = "15m" as const;
/**
 * Frozen tail (30d of 15m candles). Screen/confirm windows must end before it;
 * it is evaluated exactly once per claim candidate via evaluateHoldoutOnPanel().
 */
export const HOLDOUT_BARS = 2880 as const;

export interface DatasetProvenance {
  readonly exchange: string;
  readonly timeframe: string;
  readonly panelTimeframe: string;
  readonly panelHash: string;
  readonly symbols: readonly string[];
  readonly refLen: number;
  readonly holdoutBars: number;
}

/** FNV-1a 32-bit hash — deterministic dataset fingerprint, no deps. */
export function computePanelHash(input: {
  readonly exchange: string;
  readonly timeframe: string;
  readonly panelTimeframe: string;
  readonly symbols: readonly string[];
  readonly refLen: number;
  readonly t0Ms: number;
  readonly t1Ms: number;
}): string {
  const text = [
    input.exchange,
    input.timeframe,
    input.panelTimeframe,
    [...input.symbols].sort().join(","),
    String(input.refLen),
    String(input.t0Ms),
    String(input.t1Ms),
  ].join("|");
  let h = 0x811c9dc5;
  for (let i = 0; i < text.length; i++) {
    h ^= text.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }
  return (h >>> 0).toString(16).padStart(8, "0");
}

export function toDatasetProvenance(panel: AlignedPanel): DatasetProvenance {
  return {
    exchange: panel.exchange,
    timeframe: panel.timeframe,
    panelTimeframe: panel.panelTimeframe,
    panelHash: panel.panelHash,
    symbols: [...panel.symbols],
    refLen: panel.refLen,
    holdoutBars: panel.holdoutBars,
  };
}

/** True when a stored champion ran on this exact panel (scores comparable). */
export function datasetsMatch(
  a: DatasetProvenance | null | undefined,
  b: DatasetProvenance | null | undefined,
): boolean {
  if (!a || !b) return false;
  return (
    a.exchange === b.exchange &&
    a.timeframe === b.timeframe &&
    a.panelTimeframe === b.panelTimeframe &&
    a.panelHash === b.panelHash &&
    a.holdoutBars === b.holdoutBars
  );
}

/** True when a stored champion dataset may be compared against this panel. */
export function isProvenanceCompatible(
  champ: DatasetProvenance | null | undefined,
  panel: AlignedPanel,
): boolean {
  return datasetsMatch(champ, toDatasetProvenance(panel));
}

/** Selection prefix length — the holdout tail is excluded from screen/confirm. */
export function selectionRefLen(panel: AlignedPanel): number {
  return Math.max(0, panel.refLen - (panel.holdoutBars ?? HOLDOUT_BARS));
}

const FEE_PCT = 0.02; // maker per side (limit entries + target exits)
const TAKER_EXIT_FEE_PCT = 0.06; // Bybit taker per side (stop/liquidation/max-hold exits)
const SLIPPAGE_BPS = 2;

/** Phase geometry — screen is cheap; confirm is the claim gate. */
export const PHASE_GEOM = {
  screen: {
    stepBars: 96,
    forwardBars: 672, // ~7d of 15m
    firstBar: 336,
    minCandles: 2500,
    minWindows: 4,
    minSymbols: 3,
  },
  confirm: {
    stepBars: 96,
    forwardBars: 2880, // ~30d of 15m
    firstBar: 672,
    minCandles: 6000,
    minWindows: 8,
    minSymbols: 3,
  },
  holdout: {
    stepBars: 96, // unused — the holdout is one trailing window per symbol
    forwardBars: HOLDOUT_BARS, // 30d of 15m, must equal the reserved tail
    firstBar: 0,
    minCandles: 6000,
    minWindows: 3,
    minSymbols: 3,
  },
} as const;

function median(xs: number[]): number {
  const s = xs.filter(Number.isFinite).sort((a, b) => a - b);
  if (!s.length) return Number.NaN;
  const m = Math.floor(s.length / 2);
  return s.length % 2 ? s[m]! : (s[m - 1]! + s[m]!) / 2;
}

function homeDb(explicit?: string): string {
  if (explicit) return explicit;
  const home = process.env.NEURATRADE_HOME ?? `${process.env.HOME}/.neuratrade`;
  return `${home}/data/neuratrade.db`;
}

interface Raw5m {
  open: number;
  high: number;
  low: number;
  close: number;
  volume: number;
  timestamp: string;
}

function tableExists(db: Database, name: string): boolean {
  try {
    const row = db
      .query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`)
      .get(name) as { name: string } | null;
    return !!row;
  } catch {
    return false;
  }
}

/**
 * Load one wire symbol from ONE venue. Exact symbol match only — aliases
 * (BTC/USDT vs BTC/USDT:USDT) and venues are never merged. Candles are
 * labeled with the venue they actually came from.
 */
function load15m(
  db: Database,
  symbolWire: string,
  exchange: string,
  timeframe: string,
): Candle[] {
  const select = `SELECT c.open_price AS open, c.high_price AS high, c.low_price AS low,
              c.close_price AS close, c.volume, c.timestamp
       FROM ohlcv_data c JOIN trading_pairs tp ON tp.id = c.trading_pair_id`;
  let rowsDb: Raw5m[];
  if (tableExists(db, "exchanges")) {
    rowsDb = db
      .query(
        `${select} JOIN exchanges e ON e.id = c.exchange_id
       WHERE tp.symbol = ? AND e.name = ? AND c.timeframe = ?
       ORDER BY c.timestamp DESC LIMIT ?`,
      )
      .all(symbolWire, exchange, timeframe, 200_000) as Raw5m[];
  } else {
    // Minimal schema (unit fixtures): exact symbol + timeframe, no alias merge.
    rowsDb = db
      .query(
        `${select}
       WHERE tp.symbol = ? AND c.timeframe = ?
       ORDER BY c.timestamp DESC LIMIT ?`,
      )
      .all(symbolWire, timeframe, 200_000) as Raw5m[];
  }
  const base: Candle[] = rowsDb.toReversed().map((r) => ({
    exchange,
    symbol: symbolWire,
    timeframe,
    open: r.open,
    high: r.high,
    low: r.low,
    close: r.close,
    volume: r.volume,
    timestamp: new Date(r.timestamp),
  }));
  return resampleCandles(base, 15, PANEL_TIMEFRAME);
}

export interface LoadPanelOptions {
  readonly symbols?: number;
  readonly dbPath?: string;
  readonly minCandles?: number;
  /** Venue filter — defaults to the live venue (bybit-futures / 5m). */
  readonly exchange?: string;
  readonly timeframe?: string;
}

/** Load + align once per process. Reuse across all trials. */
export function loadAlignedPanel(opts: LoadPanelOptions): AlignedPanel {
  const started = Date.now();
  const topN = opts.symbols ?? 8;
  const minCandles = opts.minCandles ?? PHASE_GEOM.confirm.minCandles;
  const exchange = opts.exchange ?? DEFAULT_EXCHANGE;
  const timeframe = opts.timeframe ?? DEFAULT_TIMEFRAME;

  const db = new Database(homeDb(opts.dbPath), { readonly: true });
  db.exec("PRAGMA busy_timeout = 30000;");

  const panel = loadPanelCandles(
    db,
    queryPanelSymbols(db, exchange, timeframe, topN),
    topN,
    exchange,
    timeframe,
    minCandles,
  );
  db.close();

  const { t0, t1 } = panelTimeRange(panel);
  const aligned = clipPanelToRange(panel, t0, t1, minCandles);
  return finalizeAlignedPanel(aligned, t0, t1, exchange, timeframe, started);
}

interface PanelSymbolRow {
  readonly symbol: string;
  readonly count: number;
}

// Top symbols WITHIN the venue. Grouping by pair id keeps venues and
// aliases (BTC/USDT vs BTC/USDT:USDT) as distinct candidates.
function queryPanelSymbols(
  db: Database,
  exchange: string,
  timeframe: string,
  topN: number,
): PanelSymbolRow[] {
  if (tableExists(db, "exchanges")) {
    return db
      .query(
        `SELECT tp.symbol AS symbol, COUNT(*) AS count
       FROM ohlcv_data c JOIN trading_pairs tp ON tp.id = c.trading_pair_id
       JOIN exchanges e ON e.id = c.exchange_id
       WHERE e.name = ? AND c.timeframe = ?
       GROUP BY tp.id, tp.symbol ORDER BY count DESC LIMIT ?`,
      )
      .all(exchange, timeframe, topN + 4) as Array<{
      symbol: string;
      count: number;
    }>;
  }
  return db
    .query(
      `SELECT tp.symbol AS symbol, COUNT(*) AS count
       FROM ohlcv_data c JOIN trading_pairs tp ON tp.id = c.trading_pair_id
       WHERE c.timeframe = ?
       GROUP BY tp.symbol ORDER BY count DESC LIMIT ?`,
    )
    .all(timeframe, topN + 4) as Array<{ symbol: string; count: number }>;
}

function loadPanelCandles(
  db: Database,
  symbolRows: PanelSymbolRow[],
  topN: number,
  exchange: string,
  timeframe: string,
  minCandles: number,
): Map<string, Candle[]> {
  const panel = new Map<string, Candle[]>();
  for (const row of symbolRows.slice(0, topN)) {
    const candles = load15m(db, row.symbol, exchange, timeframe);
    if (candles.length >= minCandles) panel.set(row.symbol, candles);
  }
  return panel;
}

interface PanelTimeRange {
  readonly t0: number;
  readonly t1: number;
}

function panelTimeRange(panel: Map<string, Candle[]>): PanelTimeRange {
  let t0 = 0;
  let t1 = Number.POSITIVE_INFINITY;
  for (const candles of panel.values()) {
    t0 = Math.max(t0, candles[0]!.timestamp.getTime());
    t1 = Math.min(t1, candles[candles.length - 1]!.timestamp.getTime());
  }
  return { t0, t1 };
}

function isInPanelRange(c: Candle, t0: number, t1: number): boolean {
  return (
    c.timestamp.getTime() >= t0 &&
    c.timestamp.getTime() <= t1 &&
    Number.isFinite(c.close) &&
    c.close > 0
  );
}

function clipPanelToRange(
  panel: Map<string, Candle[]>,
  t0: number,
  t1: number,
  minCandles: number,
): Map<string, Candle[]> {
  const aligned = new Map<string, Candle[]>();
  for (const [symbol, candles] of panel) {
    const clipped = candles.filter((c) => isInPanelRange(c, t0, t1));
    if (clipped.length >= minCandles) aligned.set(symbol, clipped);
  }
  return aligned;
}

function finalizeAlignedPanel(
  aligned: Map<string, Candle[]>,
  t0: number,
  t1: number,
  exchange: string,
  timeframe: string,
  started: number,
): AlignedPanel {
  const symbols = [...aligned.keys()].sort();
  const refLen =
    symbols.length > 0
      ? Math.min(...symbols.map((s) => aligned.get(s)!.length))
      : 0;
  const t0Ms = symbols.length > 0 ? t0 : 0;
  const t1Ms = symbols.length > 0 && t1 !== Number.POSITIVE_INFINITY ? t1 : 0;
  const panelHash = computePanelHash({
    exchange,
    timeframe,
    panelTimeframe: PANEL_TIMEFRAME,
    symbols,
    refLen,
    t0Ms,
    t1Ms,
  });

  return {
    symbols,
    aligned,
    refLen,
    loadedMs: Date.now() - started,
    exchange,
    timeframe,
    panelTimeframe: PANEL_TIMEFRAME,
    panelHash,
    holdoutBars: HOLDOUT_BARS,
  };
}

function emptyResult(
  reason: string,
  started: number,
  phase: EvalPhase,
  symbolCount: number,
): EvaluateResult {
  return {
    score: Number.NEGATIVE_INFINITY,
    guardsOk: false,
    medianLogReturn: Number.NaN,
    medianReturnPct: Number.NaN,
    medianDrawdownPct: Number.NaN,
    winRatePct: Number.NaN,
    tradesPerSymMonth: Number.NaN,
    expectancyPct: Number.NaN,
    windows: 0,
    symbols: symbolCount,
    steps: 0,
    elapsedMs: Date.now() - started,
    reason,
    phase,
  };
}

export interface PanelEvaluationOptions {
  readonly maxSteps?: number;
  readonly budgetSec?: number;
  readonly phase?: EvalPhase;
  /** Optional symbol shard for parallel workers (indices into panel.symbols). */
  readonly symbolOffset?: number;
  readonly symbolStride?: number;
}

interface WindowStats {
  readonly rets: number[];
  readonly dds: number[];
  readonly trades: number;
  readonly wins: number;
  readonly pnlSum: number;
  readonly steps: number;
}

function selectShardSymbols(
  panel: AlignedPanel,
  stride: number,
  offset: number,
): string[] {
  return panel.symbols.filter((_, i) => i % stride === offset);
}

function buildBacktestBaseOptions(knobs: AutoresearchKnobs) {
  return {
    rungs: knobs.rungs,
    gridStepPct: knobs.gridStepPct,
    gridMaxGrids: knobs.gridMaxGrids,
    gridPauseAfterLossBars: knobs.gridPauseAfterLossBars,
    feePct: FEE_PCT,
    takerExitFeePct: TAKER_EXIT_FEE_PCT,
    slippageBps: SLIPPAGE_BPS,
    initialCapital: 10_000,
    leverage: 1,
    trendFilterPeriod: knobs.trendFilterPeriod,
    stopRatio: knobs.stopRatio,
    targetRatio: knobs.targetRatio,
    maxHoldBars: knobs.maxHoldBars,
    chopGateAdxThreshold: knobs.chopGateAdxThreshold,
    positionFraction: knobs.positionFraction,
    conservativeIntrabar: true,
  };
}

function accumulateWindowSlice(
  slice: Candle[],
  baseOpts: ReturnType<typeof buildBacktestBaseOptions>,
  stats: {
    rets: number[];
    dds: number[];
    trades: number;
    wins: number;
    pnlSum: number;
  },
): void {
  try {
    const r = runLadderGridBacktest(slice, baseOpts);
    stats.trades += r.trades.length;
    stats.wins += r.trades.filter((t) => t.win).length;
    stats.pnlSum += r.trades.reduce((sum, t) => sum + (t.pnlPct ?? 0), 0);
    stats.rets.push(r.totalReturnPct);
    stats.dds.push(r.maxDrawdownPct);
  } catch {
    // Transient backtest failure on one window: skip it.
  }
}

function overBudget(started: number, budgetMs: number): boolean {
  return Date.now() - started > budgetMs;
}

function collectWindowStats(
  panel: AlignedPanel,
  symbols: readonly string[],
  geom: (typeof PHASE_GEOM)[EvalPhase],
  baseOpts: ReturnType<typeof buildBacktestBaseOptions>,
  refLen: number,
  lastStartBar: number,
  maxSteps: number,
  budgetMs: number,
  started: number,
): WindowStats {
  const rets: number[] = [];
  const dds: number[] = [];
  let trades = 0;
  let wins = 0;
  let pnlSum = 0;
  let steps = 0;
  const mutable = { rets, dds, trades: 0, wins: 0, pnlSum: 0 };
  for (
    let bar = geom.firstBar;
    bar <= lastStartBar && steps < maxSteps;
    bar += geom.stepBars, steps++
  ) {
    if (overBudget(started, budgetMs)) break;
    for (const symbol of symbols) {
      if (overBudget(started, budgetMs)) break;
      const candles = panel.aligned.get(symbol)!;
      const startIdx = candles.length - refLen + bar;
      const endIdx = Math.min(candles.length, startIdx + geom.forwardBars);
      if (endIdx - startIdx < geom.forwardBars * 0.9) continue;
      accumulateWindowSlice(candles.slice(startIdx, endIdx), baseOpts, mutable);
    }
  }
  trades = mutable.trades;
  wins = mutable.wins;
  pnlSum = mutable.pnlSum;
  return { rets, dds, trades, wins, pnlSum, steps };
}

function summarizeWindowStats(
  stats: WindowStats,
  geom: (typeof PHASE_GEOM)[EvalPhase],
  symbolCount: number,
  started: number,
  phase: EvalPhase,
): EvaluateResult {
  const windows = stats.rets.length;
  // Throughput: average closed trades per window, scaled to a 30d month.
  // Do NOT divide by the step-timeline span — overlapping windows would
  // inflate trades/sym-mo into the thousands and make the guard meaningless.
  const windowMonths = (geom.forwardBars * 15) / (60 * 24 * 30);
  const avgTradesPerWindow = stats.trades / windows;
  const tradesPerSymMonth =
    windowMonths > 0 ? avgTradesPerWindow / windowMonths : Number.NaN;
  const winRatePct =
    stats.trades > 0 ? (stats.wins / stats.trades) * 100 : Number.NaN;
  const expectancyPct =
    stats.trades > 0 ? stats.pnlSum / stats.trades : Number.NaN;
  const medianReturnPct = median(stats.rets);
  const medianDrawdownPct = median(stats.dds);
  const logs = stats.rets
    .filter(Number.isFinite)
    .map((r) => Math.log(1 + Math.max(-0.95, r / 100)));
  const medianLogReturn = median(logs);

  // Sharpe-family inputs for selection-bias correction, computed over the same
  // percentage window returns the score summarizes so every ledger entry
  // carries comparable per-window units.
  const sharpe = sharpeFromReturns(stats.rets);
  const moments = returnMoments(stats.rets);

  const g = checkGuards({
    medianLogReturn,
    winRatePct,
    medianDrawdownPct,
    tradesPerSymMonth,
    expectancyPct,
  });

  const score = Number.isFinite(medianLogReturn)
    ? medianLogReturn
    : Number.NEGATIVE_INFINITY;

  return {
    score,
    guardsOk: g.ok,
    medianLogReturn,
    medianReturnPct,
    medianDrawdownPct,
    winRatePct,
    tradesPerSymMonth,
    expectancyPct,
    sharpe,
    skewness: moments.skewness,
    kurtosis: moments.kurtosis,
    windows,
    symbols: symbolCount,
    steps: stats.steps,
    elapsedMs: Date.now() - started,
    reason: g.reason,
    phase,
  };
}

/** Fast path: evaluate against an already-loaded panel (no DB). */
export function evaluateKnobsOnPanel(
  knobs: AutoresearchKnobs,
  panel: AlignedPanel,
  opts: PanelEvaluationOptions = {},
): EvaluateResult {
  const started = Date.now();
  const phase: EvalPhase = opts.phase ?? "confirm";
  if (phase === "holdout") {
    return evaluateHoldoutOnPanel(knobs, panel, {
      budgetSec: opts.budgetSec,
      symbolOffset: opts.symbolOffset,
      symbolStride: opts.symbolStride,
    });
  }

  const budget = resolvePanelEvalBudget(phase, opts.budgetSec, opts.maxSteps);
  const stride = Math.max(1, opts.symbolStride ?? 1);
  const offset = Math.max(0, opts.symbolOffset ?? 0);
  return evaluatePanelSelection(
    knobs,
    panel,
    stride,
    offset,
    budget.budgetMs,
    budget.maxSteps,
    started,
    phase,
  );
}

interface PanelEvalBudget {
  readonly budgetMs: number;
  readonly maxSteps: number;
}

function resolvePanelEvalBudget(
  phase: EvalPhase,
  budgetSec: number | undefined,
  maxSteps: number | undefined,
): PanelEvalBudget {
  const screen = phase === "screen";
  return {
    budgetMs: (budgetSec ?? (screen ? 45 : 180)) * 1000,
    maxSteps: maxSteps ?? (screen ? 12 : 40),
  };
}

function evaluatePanelSelection(
  knobs: AutoresearchKnobs,
  panel: AlignedPanel,
  stride: number,
  offset: number,
  budgetMs: number,
  maxSteps: number,
  started: number,
  phase: EvalPhase,
): EvaluateResult {
  const geom = PHASE_GEOM[phase];
  const symbols = selectShardSymbols(panel, stride, offset);
  if (symbols.length < geom.minSymbols) {
    return emptyResult("insufficient_symbols", started, phase, symbols.length);
  }

  // Selection runs on the prefix only — windows must end before the frozen tail.
  const refLen = panel.refLen;
  const holdoutBars = panel.holdoutBars ?? HOLDOUT_BARS;
  const selectionLen = Math.max(0, refLen - holdoutBars);
  const lastStartBar = selectionLen - geom.forwardBars - 1;
  if (lastStartBar <= geom.firstBar) {
    return emptyResult("insufficient_bars", started, phase, symbols.length);
  }

  const baseOpts = buildBacktestBaseOptions(knobs);
  const stats = collectWindowStats(
    panel,
    symbols,
    geom,
    baseOpts,
    refLen,
    lastStartBar,
    maxSteps,
    budgetMs,
    started,
  );
  if (stats.rets.length < geom.minWindows) {
    return emptyResult("insufficient_windows", started, phase, symbols.length);
  }
  return summarizeWindowStats(stats, geom, symbols.length, started, phase);
}

export interface HoldoutEvaluationOptions {
  readonly budgetSec?: number;
  /** Optional symbol shard for parallel workers (indices into panel.symbols). */
  readonly symbolOffset?: number;
  readonly symbolStride?: number;
}

/**
 * Frozen final holdout: ONE trailing window per symbol over the reserved tail.
 * Never used for knob selection (screen/confirm cannot see these bars).
 * Evaluate exactly once per claim candidate — a second look is selection.
 */
export function evaluateHoldoutOnPanel(
  knobs: AutoresearchKnobs,
  panel: AlignedPanel,
  opts: HoldoutEvaluationOptions = {},
): EvaluateResult {
  const started = Date.now();
  const geom = PHASE_GEOM.holdout;
  const budgetMs = (opts.budgetSec ?? 180) * 1000;
  const stride = Math.max(1, opts.symbolStride ?? 1);
  const offset = Math.max(0, opts.symbolOffset ?? 0);

  const symbols = selectShardSymbols(panel, stride, offset);
  const holdoutBars = panel.holdoutBars ?? HOLDOUT_BARS;
  const guard = checkHoldoutGuards(symbols, geom, holdoutBars, started);
  if (guard !== null) return guard;

  const baseOpts = buildBacktestBaseOptions(knobs);
  const stats = accumulateHoldoutStats(
    panel,
    symbols,
    holdoutBars,
    baseOpts,
    started,
    budgetMs,
  );
  if (stats.rets.length < geom.minWindows) {
    return emptyResult(
      "insufficient_holdout_windows",
      started,
      "holdout",
      symbols.length,
    );
  }
  return summarizeWindowStats(stats, geom, symbols.length, started, "holdout");
}

function checkHoldoutGuards(
  symbols: readonly string[],
  geom: (typeof PHASE_GEOM)[EvalPhase],
  holdoutBars: number,
  started: number,
): EvaluateResult | null {
  if (symbols.length < geom.minSymbols) {
    return emptyResult(
      "insufficient_symbols",
      started,
      "holdout",
      symbols.length,
    );
  }
  if (!Number.isFinite(holdoutBars) || holdoutBars <= 0) {
    return emptyResult("holdout_disabled", started, "holdout", symbols.length);
  }
  return null;
}

function accumulateHoldoutStats(
  panel: AlignedPanel,
  symbols: readonly string[],
  holdoutBars: number,
  baseOpts: ReturnType<typeof buildBacktestBaseOptions>,
  started: number,
  budgetMs: number,
): WindowStats {
  const mutable = {
    rets: [] as number[],
    dds: [] as number[],
    trades: 0,
    wins: 0,
    pnlSum: 0,
  };
  for (const symbol of symbols) {
    if (overBudget(started, budgetMs)) break;
    const candles = panel.aligned.get(symbol)!;
    if (candles.length < holdoutBars * 0.9) continue;
    accumulateWindowSlice(
      candles.slice(candles.length - holdoutBars),
      baseOpts,
      mutable,
    );
  }
  return {
    rets: mutable.rets,
    dds: mutable.dds,
    trades: mutable.trades,
    wins: mutable.wins,
    pnlSum: mutable.pnlSum,
    steps: mutable.rets.length,
  };
}

/** Convenience: load panel (or use opts.panel) then evaluate. */
export function evaluateKnobs(
  knobs: AutoresearchKnobs,
  opts: EvaluateOptions = {},
): EvaluateResult {
  const phase = opts.phase ?? "confirm";
  const panel =
    opts.panel ??
    loadAlignedPanel({
      symbols: opts.symbols ?? 8,
      dbPath: opts.dbPath,
      minCandles: PHASE_GEOM[phase].minCandles,
      exchange: opts.exchange,
      timeframe: opts.timeframe,
    });
  return evaluateKnobsOnPanel(knobs, panel, {
    maxSteps: opts.maxSteps,
    budgetSec: opts.budgetSec,
    phase,
  });
}

/** Pure guard check used by unit tests (no DB). */
export function checkGuards(input: GuardInput): GuardCheckResult {
  return checkKeepGuards(input);
}
