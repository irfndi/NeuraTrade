/**
 * Frozen evaluation harness (Karpathy prepare.py analogue).
 * Do not edit during autoresearch trials — change knobs.ts only.
 */
import { Database } from "bun:sqlite";
import { resampleCandles } from "../src/scalping/grid-universe.ts";
import { runLadderGridBacktest } from "../src/scalping/ladder-grid.ts";
import type { Candle } from "../src/market-data/types.ts";
import type { AutoresearchKnobs } from "./knobs.ts";

export interface EvaluateOptions {
  readonly symbols?: number;
  readonly maxSteps?: number;
  readonly budgetSec?: number;
  readonly dbPath?: string;
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
  readonly windows: number;
  readonly symbols: number;
  readonly steps: number;
  readonly elapsedMs: number;
  readonly reason: string;
}

const FEE_PCT = 0.02;
const SLIPPAGE_BPS = 2;
const STEP_BARS = 96;
const FORWARD_BARS = 2880; // ~30d of 15m
const FIRST_BAR = 672;

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

function load15m(db: Database, symbolWire: string): Candle[] {
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

export function evaluateKnobs(
  knobs: AutoresearchKnobs,
  opts: EvaluateOptions = {},
): EvaluateResult {
  const started = Date.now();
  const budgetMs = (opts.budgetSec ?? 180) * 1000;
  const topN = opts.symbols ?? 8;
  const maxSteps = opts.maxSteps ?? 40;

  const db = new Database(homeDb(opts.dbPath), { readonly: true });
  db.exec("PRAGMA busy_timeout = 30000;");

  const symbolRows = db
    .query(
      `SELECT tp.symbol AS symbol, COUNT(*) AS count
       FROM ohlcv_data c JOIN trading_pairs tp ON tp.id = c.trading_pair_id
       WHERE c.timeframe = '5m'
       GROUP BY tp.symbol ORDER BY count DESC LIMIT ?`,
    )
    .all(topN + 4) as Array<{ symbol: string; count: number }>;

  const panel = new Map<string, Candle[]>();
  for (const row of symbolRows.slice(0, topN)) {
    if (Date.now() - started > budgetMs * 0.35) break;
    const candles = load15m(db, row.symbol);
    if (candles.length >= 6000) panel.set(row.symbol, candles);
  }
  db.close();

  let t0 = 0;
  let t1 = Number.POSITIVE_INFINITY;
  for (const candles of panel.values()) {
    t0 = Math.max(t0, candles[0]!.timestamp.getTime());
    t1 = Math.min(t1, candles[candles.length - 1]!.timestamp.getTime());
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
  const empty = (reason: string): EvaluateResult => ({
    score: Number.NEGATIVE_INFINITY,
    guardsOk: false,
    medianLogReturn: Number.NaN,
    medianReturnPct: Number.NaN,
    medianDrawdownPct: Number.NaN,
    winRatePct: Number.NaN,
    tradesPerSymMonth: Number.NaN,
    expectancyPct: Number.NaN,
    windows: 0,
    symbols: symbols.length,
    steps: 0,
    elapsedMs: Date.now() - started,
    reason,
  });

  if (symbols.length < 3) return empty("insufficient_symbols");

  const refLen = Math.min(...symbols.map((s) => aligned.get(s)!.length));
  const lastStartBar = refLen - FORWARD_BARS - 1;
  if (lastStartBar <= FIRST_BAR) return empty("insufficient_bars");

  const rets: number[] = [];
  const dds: number[] = [];
  let trades = 0;
  let wins = 0;
  let pnlSum = 0;
  let steps = 0;

  const baseOpts = {
    rungs: knobs.rungs,
    gridStepPct: knobs.gridStepPct,
    gridMaxGrids: knobs.gridMaxGrids,
    gridPauseAfterLossBars: knobs.gridPauseAfterLossBars,
    feePct: FEE_PCT,
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

  for (
    let bar = FIRST_BAR;
    bar <= lastStartBar && steps < maxSteps;
    bar += STEP_BARS, steps++
  ) {
    if (Date.now() - started > budgetMs) break;
    for (const symbol of symbols) {
      if (Date.now() - started > budgetMs) break;
      const candles = aligned.get(symbol)!;
      const startIdx = candles.length - refLen + bar;
      const endIdx = Math.min(candles.length, startIdx + FORWARD_BARS);
      if (endIdx - startIdx < FORWARD_BARS * 0.9) continue;
      const slice = candles.slice(startIdx, endIdx);
      try {
        const r = runLadderGridBacktest(slice, baseOpts);
        trades += r.trades.length;
        wins += r.trades.filter((t) => t.win).length;
        pnlSum += r.trades.reduce((sum, t) => sum + (t.pnlPct ?? 0), 0);
        rets.push(r.totalReturnPct);
        dds.push(r.maxDrawdownPct);
      } catch {
        continue;
      }
    }
  }

  const windows = rets.length;
  if (windows < 8) return empty("insufficient_windows");

  const months = (steps * STEP_BARS * 15) / (60 * 24 * 30);
  const tradesPerSymMonth =
    months > 0 ? trades / Math.max(1, symbols.length * months) : Number.NaN;
  const winRatePct = trades > 0 ? (wins / trades) * 100 : Number.NaN;
  const expectancyPct = trades > 0 ? pnlSum / trades : Number.NaN;
  const medianReturnPct = median(rets);
  const medianDrawdownPct = median(dds);
  const logs = rets
    .filter(Number.isFinite)
    .map((r) => Math.log(1 + Math.max(-0.95, r / 100)));
  const medianLogReturn = median(logs);

  const guards: string[] = [];
  if (!(medianLogReturn > 0)) guards.push("log_return_nonpositive");
  if (!(winRatePct >= 48)) guards.push("winrate_below_48");
  if (!(medianDrawdownPct <= 15)) guards.push("drawdown_above_15");
  if (!(tradesPerSymMonth >= 4)) guards.push("throughput_below_4");
  if (!(expectancyPct > 0)) guards.push("expectancy_nonpositive");

  const guardsOk = guards.length === 0;
  const score = Number.isFinite(medianLogReturn)
    ? medianLogReturn
    : Number.NEGATIVE_INFINITY;

  return {
    score,
    guardsOk,
    medianLogReturn,
    medianReturnPct,
    medianDrawdownPct,
    winRatePct,
    tradesPerSymMonth,
    expectancyPct,
    windows,
    symbols: symbols.length,
    steps,
    elapsedMs: Date.now() - started,
    reason: guardsOk ? "ok" : guards.join(","),
  };
}

/** Pure guard check used by unit tests (no DB). */
export function checkGuards(input: {
  medianLogReturn: number;
  winRatePct: number;
  medianDrawdownPct: number;
  tradesPerSymMonth: number;
  expectancyPct: number;
}): { ok: boolean; reason: string } {
  const guards: string[] = [];
  if (!(input.medianLogReturn > 0)) guards.push("log_return_nonpositive");
  if (!(input.winRatePct >= 48)) guards.push("winrate_below_48");
  if (!(input.medianDrawdownPct <= 15)) guards.push("drawdown_above_15");
  if (!(input.tradesPerSymMonth >= 4)) guards.push("throughput_below_4");
  if (!(input.expectancyPct > 0)) guards.push("expectancy_nonpositive");
  return {
    ok: guards.length === 0,
    reason: guards.length === 0 ? "ok" : guards.join(","),
  };
}
