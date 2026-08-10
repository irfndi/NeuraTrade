/**
 * Flow Ignition — flow-v1 backtest harness.
 *
 * Pure data-in → report-out module (no DB, no network, no RNG): the CLI
 * loads candles / OI / funding from SQLite and feeds them in. Every path is
 * deterministic by construction — z-scores are rolling per-symbol statistics
 * (the `computeFlowSignal` signature is single-series, so cross-sectional
 * universe z would degenerate; per-symbol rolling z keeps both the harness
 * and a one-symbol fixture meaningful).
 *
 * Strategy sketch:
 *  - Signals fire at every quarter-hour boundary (:00/:15/:30/:45) from the
 *    PREVIOUS 15m window of bars.
 *  - Score = 0.30*z_return + 0.25*z_dOI + 0.25*z_ofi + 0.15*z_volume
 *            - 0.05*max(0, z_funding)            (longs; mirrored for shorts)
 *  - Entry: LONG when score >= threshold AND price > 1h VWAP AND
 *    z_funding < +2 AND spreadOk; SHORT mirrored.
 *  - Exit: initial stop 1.25*ATR15; stop to breakeven after +1R, then trail
 *    1.25*ATR15; time exit per hold-time grid (each hold time runs its own
 *    full simulation); emergency exit when OFI flips sign AND |z_dOI| > 1.5.
 *  - Costs: taker on entry + exit, spread bps on both legs, exact historical
 *    funding rows charged for each held period.
 *  - Walk-forward: rolling train/test windows; signals whose 4-12h label
 *    window would cross a train/test boundary are purged.
 */

import type { CandleLike } from "./types.js";

// ---------------------------------------------------------------------------
// Options & constants
// ---------------------------------------------------------------------------

/** Hours per hold-time grid. The whole grid runs; each entry reported. */
export const DEFAULT_HOLD_TIMES = [0.5, 1, 2, 4, 8] as const;

/** Entry score threshold (z-units). */
export const DEFAULT_ENTRY_THRESHOLD = 1.0;

/** Funding z-cap for longs (+2) and shorts (-2). */
export const DEFAULT_FUNDING_Z_CAP = 2.0;

/** |z_dOI| required for the emergency OFI-flip exit. */
export const DEFAULT_EMERGENCY_OI_Z = 1.5;

/** Initial stop / trail distance in ATR15 units. */
export const ATR_STOP_MULT = 1.25;

/** Price move (in R = initial risk) that moves the stop to breakeven. */
export const BREAKEVEN_R = 1.0;

/** Rolling z-score lookback in 15m windows (~25h). */
export const Z_LOOKBACK = 100;

/** Minimum history before a z-score is meaningful (else 0). */
export const Z_MIN_SAMPLES = 30;

/** Score weights, long side (shorts mirror the first four terms). */
export const FLOW_SCORE_WEIGHTS = {
  zReturn: 0.3,
  zOi: 0.25,
  zOfi: 0.25,
  zVolume: 0.15,
  fundingPenalty: 0.05,
} as const;

/** Quarter-hour window length (ms). */
export const QUARTER_HOUR_MS = 15 * 60 * 1000;

export interface FlowFees {
  /** Taker fee as a fraction, e.g. 0.00055 = 0.055% per side. */
  readonly taker: number;
  /** Maker fee as a fraction (reserved; flow entries are taker-filled). */
  readonly maker: number;
}

export interface FlowThresholds {
  /** Entry score threshold in z-units (default 1.0). */
  readonly entry: number;
  /** Funding z-cap: longs need z_funding < +cap, shorts z_funding > -cap. */
  readonly funding: number;
  /** |z_dOI| threshold for the emergency OFI-flip exit (default 1.5). */
  readonly emergencyOiZ: number;
}

export interface FlowBacktestOptions {
  readonly fees: FlowFees;
  /** Round-trip spread in basis points (charged on BOTH entry and exit). */
  readonly spreadBps: number;
  readonly thresholds: FlowThresholds;
  /** Hold-time grid in hours. All run; the report shows each. */
  readonly holdTimes: readonly number[];
  /** Walk-forward train window length in days (~60-90). */
  readonly trainDays: number;
  /** Walk-forward test window length in days (~14-30). */
  readonly testDays: number;
  /** Number of rolling walk-forward windows. */
  readonly walkForwardSteps: number;
}

export const defaultFlowBacktestOptions: FlowBacktestOptions = {
  fees: { taker: 0.00055, maker: 0.0002 },
  spreadBps: 2,
  thresholds: {
    entry: DEFAULT_ENTRY_THRESHOLD,
    funding: DEFAULT_FUNDING_Z_CAP,
    emergencyOiZ: DEFAULT_EMERGENCY_OI_Z,
  },
  holdTimes: [...DEFAULT_HOLD_TIMES],
  trainDays: 75,
  testDays: 21,
  walkForwardSteps: 3,
};

// ---------------------------------------------------------------------------
// Input series
// ---------------------------------------------------------------------------

export interface FlowOiPoint {
  /** Epoch ms. */
  readonly ts: number;
  /** Open interest in contracts. */
  readonly oi: number;
  /** Open interest in quote currency (optional). */
  readonly oiValue?: number;
}

export interface FlowFundingPoint {
  /** Epoch ms (row timestamp — charged exactly here). */
  readonly ts: number;
  /** Funding rate as a decimal, e.g. 0.0001 = 0.01%. */
  readonly fundingRate: number;
}

export interface FlowSymbolSeries {
  readonly symbol: string;
  readonly exchange: string;
  /** "1m" or "5m". */
  readonly timeframe: string;
  /** Ascending by timestamp; must span >= 15m per bar window. */
  readonly candles: readonly CandleLike[];
  /** Sparse OI points, ascending by ts. Optional (z_dOI = 0 when absent). */
  readonly oi?: readonly FlowOiPoint[];
  /** Sparse funding rows, ascending by ts. Optional (z_funding = 0). */
  readonly funding?: readonly FlowFundingPoint[];
}

export interface FlowBacktestData {
  readonly series: readonly FlowSymbolSeries[];
  readonly options: FlowBacktestOptions;
}

// ---------------------------------------------------------------------------
// Signals & trades
// ---------------------------------------------------------------------------

export type FlowSignalSide = "LONG" | "SHORT" | "NONE";

export interface FlowSignal {
  readonly symbol: string;
  /** Quarter-hour boundary ts (epoch ms). */
  readonly ts: number;
  readonly side: FlowSignalSide;
  readonly score: number;
  readonly zReturn: number;
  readonly zOiChange: number;
  readonly zVolume: number;
  readonly zOfi: number;
  readonly zFunding: number;
  /** Open of the first bar strictly after the boundary (execution price). */
  readonly entryPrice: number;
  /** Ts of the execution bar. */
  readonly entryTs: number;
  /** 1h VWAP as of the signal (NaN when unavailable — gate fails). */
  readonly vwap1h: number;
  /** ATR over the previous 15m window (0 → no entry, no stop). */
  readonly atr15: number;
  /** Always true — no spread feed in the data contract. */
  readonly spreadOk: boolean;
}

export type FlowExitReason =
  | "stop"
  | "breakeven"
  | "trail"
  | "time"
  | "emergency";

export interface FlowBacktestTrade {
  readonly symbol: string;
  readonly side: "LONG" | "SHORT";
  readonly entryTs: number;
  readonly exitTs: number;
  readonly entryPrice: number;
  readonly exitPrice: number;
  readonly holdHours: number;
  readonly exitReason: FlowExitReason;
  /** Gross return as % of notional (before costs/funding). */
  readonly grossReturnPct: number;
  /** Fees + spread as % of notional (both legs). */
  readonly costPct: number;
  /** Funding paid/received as % of notional (positive = paid). */
  readonly fundingPct: number;
  /** Net edge as % of notional (gross - costs - funding). */
  readonly netEdgePct: number;
  /** True when netEdgePct > 0; edge <= 0 counts as a loser. */
  readonly win: boolean;
}

export interface SymbolTradeAggregate {
  readonly symbol: string;
  readonly trades: number;
  readonly wins: number;
  readonly winRate: number;
  readonly avgEdgePct: number;
  /** Sum of net edges (drives the portfolio curve). */
  readonly netEdgeSumPct: number;
}

export interface HoldTimeResult {
  readonly holdTimeHours: number;
  readonly trades: readonly FlowBacktestTrade[];
  readonly totalTrades: number;
  readonly winRate: number;
  /** Mean net edge per trade, % of notional, after all costs. */
  readonly avgEdgePerTradePct: number;
  /** Peak-to-trough of the cumulative net-edge curve, in % points. */
  readonly maxDrawdownPct: number;
  /** Per-trade expected value in % of notional (= avg edge per trade). */
  readonly expectancyPct: number;
  readonly bySymbol: readonly SymbolTradeAggregate[];
}

export interface WalkForwardWindowResult {
  readonly index: number;
  readonly trainStart: number;
  readonly trainEnd: number;
  readonly testStart: number;
  readonly testEnd: number;
  /** Signals kept after purging (those whose label window fits the test). */
  readonly signals: number;
  /** Signals dropped because their 4-12h window crossed the boundary. */
  readonly purged: number;
}

export interface FlowBacktestReport {
  readonly options: FlowBacktestOptions;
  readonly windows: readonly WalkForwardWindowResult[];
  /** One full portfolio simulation per hold time (union of OOS windows). */
  readonly byHoldTime: readonly HoldTimeResult[];
  /** Convenience: first hold-time run (the default presentation). */
  readonly portfolio: HoldTimeResult;
}

// ---------------------------------------------------------------------------
// Per-bar context (shared by signals, the trade engine, and the live engine)
// ---------------------------------------------------------------------------

export interface BarContext {
  readonly ts: number;
  readonly open: number;
  readonly high: number;
  readonly low: number;
  readonly close: number;
  readonly atr15: number;
  readonly vwap1h: number;
  /** Signed OFI over the trailing 15m window (sum of per-bar deltas). */
  readonly ofiRaw: number;
  /** Fractional OI change over the trailing 15m window. */
  readonly dOiRaw: number;
  readonly fundingRaw: number;
  readonly zReturn: number;
  readonly zOi: number;
  readonly zVolume: number;
  readonly zOfi: number;
  readonly zFunding: number;
}

const MS_PER_HOUR = 3_600_000;
const MS_PER_DAY = 86_400_000;
const EPS = 1e-12;

/** Per-bar signed OFI delta in [-1, 1]: (buyVol - sellVol) / volume. */
function barOfiDelta(c: CandleLike): number {
  const range = c.high - c.low;
  if (range <= EPS || c.volume <= 0) return 0;
  const buyVol = (c.volume * (c.close - c.low)) / range;
  const sellVol = (c.volume * (c.high - c.close)) / range;
  return (buyVol - sellVol) / c.volume;
}

function trueRange(c: CandleLike, prevClose: number): number {
  return Math.max(
    c.high - c.low,
    Math.abs(c.high - prevClose),
    Math.abs(c.low - prevClose),
  );
}

/** First index with ts >= target (candles sorted asc). */
function lowerBound(candles: readonly CandleLike[], target: number): number {
  let lo = 0;
  let hi = candles.length;
  while (lo < hi) {
    const mid = (lo + hi) >>> 1;
    if (candles[mid].timestamp.getTime() < target) lo = mid + 1;
    else hi = mid;
  }
  return lo;
}

/** Latest series point with ts <= t (points sorted asc), else undefined. */
function latestAt<T extends { readonly ts: number }>(
  points: readonly T[] | undefined,
  t: number,
): T | undefined {
  if (!points || points.length === 0) return undefined;
  let lo = 0;
  let hi = points.length;
  while (lo < hi) {
    const mid = (lo + hi) >>> 1;
    if (points[mid].ts <= t) lo = mid + 1;
    else hi = mid;
  }
  return lo === 0 ? undefined : points[lo - 1];
}

/** Next quarter-hour boundary strictly after ts. */
function ceilToQuarterHour(ts: number): number {
  return Math.floor(ts / QUARTER_HOUR_MS) * QUARTER_HOUR_MS + QUARTER_HOUR_MS;
}

/** Rolling z-score of `value` against the trailing history (self-excluded). */
function rollingZ(history: readonly number[], value: number): number {
  const n = history.length;
  if (n < Z_MIN_SAMPLES) return 0;
  const tail = n > Z_LOOKBACK ? history.slice(n - Z_LOOKBACK) : history;
  let sum = 0;
  for (const v of tail) sum += v;
  const mean = sum / tail.length;
  let sq = 0;
  for (const v of tail) sq += (v - mean) * (v - mean);
  const std = Math.sqrt(sq / tail.length);
  if (std < EPS) return 0;
  return (value - mean) / std;
}

/**
 * Compute one BarContext per candle: trailing-15m metrics and their rolling
 * z-scores. The boundary-window approximation used by signals: a quarter-hour
 * boundary t uses the context of the last bar with ts < t — exact for
 * :00-aligned bar grids, within one bar otherwise.
 */
export function computeContexts(
  candles: readonly CandleLike[],
  oi: readonly FlowOiPoint[] | undefined,
  funding: readonly FlowFundingPoint[] | undefined,
): BarContext[] {
  const sorted = [...candles].sort(
    (a, b) => a.timestamp.getTime() - b.timestamp.getTime(),
  );
  const n = sorted.length;
  const ts = sorted.map((c) => c.timestamp.getTime());

  // Per-bar primitive arrays for O(n log n) window sums.
  const deltas = sorted.map(barOfiDelta);
  const closes = sorted.map((c) => c.close);
  const typicals = sorted.map((c) => (c.high + c.low + c.close) / 3);
  const typicalVol = sorted.map((c, i) => typicals[i] * c.volume);

  const histReturn: number[] = [];
  const histOi: number[] = [];
  const histVolume: number[] = [];
  const histOfi: number[] = [];
  const histFunding: number[] = [];

  const ctxs: BarContext[] = [];
  for (let i = 0; i < n; i++) {
    const t = ts[i];
    const start = lowerBound(sorted, t - QUARTER_HOUR_MS);
    const end = i; // window = [t-15m, t) → bars start..i-1
    const windowBars = end - start;
    const safeStart = start > 0 ? start : 0;
    const firstClose = start < i ? closes[start] : closes[i];

    let volSum = 0;
    let ofiSum = 0;
    let typVolSum = 0;
    let trSum = 0;
    let prevClose = start > 0 ? closes[start - 1] : closes[0];
    for (let k = safeStart; k < end; k++) {
      volSum += sorted[k].volume;
      ofiSum += deltas[k];
      typVolSum += typicalVol[k];
      trSum += trueRange(sorted[k], prevClose);
      prevClose = closes[k];
    }
    const ret = windowBars > 0 && firstClose > 0 ? closes[i] / firstClose - 1 : 0;
    const atr15 = windowBars > 0 ? trSum / windowBars : 0;

    // 1h VWAP: bars in [t-1h, t).
    const hourStart = lowerBound(sorted, t - MS_PER_HOUR);
    let hTypVol = 0;
    let hVol = 0;
    for (let k = hourStart; k < end; k++) {
      hTypVol += typicalVol[k];
      hVol += sorted[k].volume;
    }
    const vwap1h = hVol > 0 ? hTypVol / hVol : Number.NaN;

    // OI fractional change over the window.
    const oiNow = latestAt(oi, t);
    const oiBefore = latestAt(oi, t - QUARTER_HOUR_MS);
    const dOiRaw =
      oiNow && oiBefore && oiBefore.oi > 0 ? oiNow.oi / oiBefore.oi - 1 : 0;

    const fundingRaw = latestAt(funding, t)?.fundingRate ?? 0;

    // z-scores vs the history of PRIOR windows (self-excluded → no lookahead).
    const zReturn = rollingZ(histReturn, ret);
    const zOi = rollingZ(histOi, dOiRaw);
    const zVolume = rollingZ(histVolume, volSum);
    const zOfi = rollingZ(histOfi, ofiSum);
    const zFunding = rollingZ(histFunding, fundingRaw);

    histReturn.push(ret);
    histOi.push(dOiRaw);
    histVolume.push(volSum);
    histOfi.push(ofiSum);
    histFunding.push(fundingRaw);

    ctxs.push({
      ts: t,
      open: sorted[i].open,
      high: sorted[i].high,
      low: sorted[i].low,
      close: closes[i],
      atr15,
      vwap1h,
      ofiRaw: ofiSum,
      dOiRaw,
      fundingRaw,
      zReturn,
      zOi,
      zVolume,
      zOfi,
      zFunding,
    });
  }
  return ctxs;
}

// ---------------------------------------------------------------------------
// Signal generation
// ---------------------------------------------------------------------------

function longScore(s: BarContext): number {
  return (
    FLOW_SCORE_WEIGHTS.zReturn * s.zReturn +
    FLOW_SCORE_WEIGHTS.zOi * s.zOi +
    FLOW_SCORE_WEIGHTS.zOfi * s.zOfi +
    FLOW_SCORE_WEIGHTS.zVolume * s.zVolume -
    FLOW_SCORE_WEIGHTS.fundingPenalty * Math.max(0, s.zFunding)
  );
}

function shortScore(s: BarContext): number {
  return (
    -(FLOW_SCORE_WEIGHTS.zReturn * s.zReturn) -
    FLOW_SCORE_WEIGHTS.zOi * s.zOi -
    FLOW_SCORE_WEIGHTS.zOfi * s.zOfi -
    FLOW_SCORE_WEIGHTS.zVolume * s.zVolume -
    FLOW_SCORE_WEIGHTS.fundingPenalty * Math.max(0, -s.zFunding)
  );
}

/**
 * Compute flow signals at every quarter-hour boundary in the series.
 *
 * A signal at boundary t uses the context of the last bar strictly before t
 * (the previous 15m window of bars) and executes at the open of the first
 * bar strictly after t (no lookahead). Side is LONG/SHORT only when the full
 * entry gate passes, else NONE (kept in the list for inspection).
 */
export function computeFlowSignal(
  candles: readonly CandleLike[],
  oiSeries: readonly FlowOiPoint[] | undefined,
  fundingSeries: readonly FlowFundingPoint[] | undefined,
  options: FlowBacktestOptions = defaultFlowBacktestOptions,
  symbol = "SYNTH",
): readonly FlowSignal[] {
  const ctxs = computeContexts(candles, oiSeries, fundingSeries);
  const signals: FlowSignal[] = [];
  const { entry: entryTh, funding: fundingCap } = options.thresholds;

  for (let i = 0; i < ctxs.length - 1; i++) {
    // Every boundary in the gap (ctx_i is the last bar before the boundary).
    // Entry executes at the first bar with ts >= boundary — the next bar.
    let boundary = ceilToQuarterHour(ctxs[i].ts);
    const nextBarTs = ctxs[i + 1].ts;
    while (boundary <= nextBarTs) {
      const s = ctxs[i];
      const entryBar = ctxs[i + 1];
      const scoreLong = longScore(s);
      const scoreShort = shortScore(s);
      const vwapOk = Number.isFinite(s.vwap1h);
      const atrOk = s.atr15 > 0;

      let side: FlowSignalSide = "NONE";
      if (
        atrOk &&
        vwapOk &&
        s.close > s.vwap1h &&
        s.zFunding < fundingCap &&
        scoreLong >= entryTh
      ) {
        side = "LONG";
      } else if (
        atrOk &&
        vwapOk &&
        s.close < s.vwap1h &&
        s.zFunding > -fundingCap &&
        scoreShort >= entryTh
      ) {
        side = "SHORT";
      }

      signals.push({
        symbol,
        ts: boundary,
        side,
        score: side === "LONG" ? scoreLong : side === "SHORT" ? scoreShort : 0,
        zReturn: s.zReturn,
        zOiChange: s.zOi,
        zVolume: s.zVolume,
        zOfi: s.zOfi,
        zFunding: s.zFunding,
        entryPrice: entryBar.open,
        entryTs: entryBar.ts,
        vwap1h: s.vwap1h,
        atr15: s.atr15,
        spreadOk: true,
      });
      boundary += QUARTER_HOUR_MS;
    }
  }
  return signals;
}

// ---------------------------------------------------------------------------
// Trade engine (one symbol, one hold time, bars restricted to a window)
// ---------------------------------------------------------------------------

interface EngineOptions {
  readonly symbol: string;
  readonly holdTimeHours: number;
  readonly opts: FlowBacktestOptions;
  /** Exact historical funding rows (sorted asc) for this symbol. */
  readonly funding: readonly FlowFundingPoint[] | undefined;
}

function runSymbolEngine(
  ctxs: readonly BarContext[],
  signals: readonly FlowSignal[],
  engineOpts: EngineOptions,
): readonly FlowBacktestTrade[] {
  const { opts } = engineOpts;
  const holdMs = engineOpts.holdTimeHours * MS_PER_HOUR;
  const taker = opts.fees.taker;
  const spreadLeg = opts.spreadBps / 10_000;
  const entryCost = taker + spreadLeg;
  const exitCost = taker + spreadLeg;

  // Signals sorted by entryTs; one position per symbol at a time.
  const byEntryTs = [...signals].sort((a, b) => a.entryTs - b.entryTs);
  const trades: FlowBacktestTrade[] = [];

  let sigIdx = 0;
  let pos: {
    side: "LONG" | "SHORT";
    entryPrice: number;
    entryTs: number;
    atr: number;
    stop: number;
    stage: "initial" | "breakeven" | "trail";
    extreme: number;
    entryOfiSign: number;
  } | null = null;

  for (let i = 0; i < ctxs.length; i++) {
    const ctx = ctxs[i];

    // Enter: first pending signal whose entry bar is this bar.
    if (!pos && sigIdx < byEntryTs.length) {
      const sig = byEntryTs[sigIdx];
      if (sig.entryTs === ctx.ts) {
        sigIdx++;
        if (sig.side === "LONG" || sig.side === "SHORT") {
          const atr = sig.atr15 > 0 ? sig.atr15 : ctx.atr15;
          if (atr > 0) {
            const side = sig.side;
            const stop =
              side === "LONG"
                ? ctx.open - ATR_STOP_MULT * atr
                : ctx.open + ATR_STOP_MULT * atr;
            pos = {
              side,
              entryPrice: ctx.open,
              entryTs: ctx.ts,
              atr,
              stop,
              stage: "initial",
              extreme: side === "LONG" ? ctx.high : ctx.low,
              entryOfiSign:
                Math.sign(sig.zOfi) !== 0
                  ? Math.sign(sig.zOfi)
                  : Math.sign(sig.zReturn) || 1,
            };
          }
        }
      } else if (sig.entryTs < ctx.ts) {
        // Signal's entry bar never materialized inside the window — drop it.
        sigIdx++;
      }
    }

    if (!pos) continue;

    // Stop/trail management.
    const side = pos.side;
    if (side === "LONG") {
      if (ctx.high > pos.extreme) pos.extreme = ctx.high;
      const r = ATR_STOP_MULT * pos.atr;
      if (pos.extreme - pos.entryPrice >= BREAKEVEN_R * r) {
        pos.stage = pos.stop === pos.entryPrice ? pos.stage : "breakeven";
        pos.stop = Math.max(pos.stop, pos.entryPrice, pos.extreme - r);
        if (pos.stop > pos.entryPrice) pos.stage = "trail";
      }
    } else {
      if (ctx.low < pos.extreme) pos.extreme = ctx.low;
      const r = ATR_STOP_MULT * pos.atr;
      if (pos.entryPrice - pos.extreme >= BREAKEVEN_R * r) {
        pos.stage = pos.stop === pos.entryPrice ? pos.stage : "breakeven";
        pos.stop = Math.min(pos.stop, pos.entryPrice, pos.extreme + r);
        if (pos.stop < pos.entryPrice) pos.stage = "trail";
      }
    }

    let exitPrice: number | null = null;
    let reason: FlowExitReason | null = null;

    // 1) Stop hit.
    if (side === "LONG" && ctx.low <= pos.stop) {
      exitPrice = Math.min(ctx.open, pos.stop);
      reason = pos.stage === "initial" ? "stop" : pos.stage;
    } else if (side === "SHORT" && ctx.high >= pos.stop) {
      exitPrice = Math.max(ctx.open, pos.stop);
      reason = pos.stage === "initial" ? "stop" : pos.stage;
    }
    // 2) Emergency: OFI flips sign against entry AND |z_dOI| > 1.5.
    if (reason === null) {
      const ofiSign = Math.sign(ctx.ofiRaw);
      if (
        Math.abs(ctx.zOi) > opts.thresholds.emergencyOiZ &&
        ofiSign !== 0 &&
        pos.entryOfiSign !== 0 &&
        ofiSign !== pos.entryOfiSign
      ) {
        exitPrice = ctx.close;
        reason = "emergency";
      }
    }
    // 3) Time exit at the hold-time grid.
    if (reason === null && ctx.ts >= pos.entryTs + holdMs) {
      exitPrice = ctx.close;
      reason = "time";
    }

    if (exitPrice === null && reason === null && i === ctxs.length - 1) {
      // End of simulation range — force-close so no trade dangles.
      exitPrice = ctx.close;
      reason = "time";
    }

    if (exitPrice !== null && reason !== null) {
      const grossPct =
        side === "LONG"
          ? ((exitPrice - pos.entryPrice) / pos.entryPrice) * 100
          : ((pos.entryPrice - exitPrice) / pos.entryPrice) * 100;
      const costPct = (entryCost + exitCost) * 100;
      const fundingPct = fundingPaidPct(
        engineOpts.funding,
        pos.entryTs,
        ctx.ts,
        side,
      );
      const netEdgePct = grossPct - costPct - fundingPct;
      trades.push({
        symbol: engineOpts.symbol,
        side,
        entryTs: pos.entryTs,
        exitTs: ctx.ts,
        entryPrice: pos.entryPrice,
        exitPrice,
        holdHours: (ctx.ts - pos.entryTs) / MS_PER_HOUR,
        exitReason: reason,
        grossReturnPct: grossPct,
        costPct,
        fundingPct,
        netEdgePct,
        win: netEdgePct > 0,
      });
      pos = null;
    }
  }
  return trades;
}

/**
 * Funding charged over (entryTs, exitTs]: every exact historical funding row
 * inside the held period. Positive funding = longs pay → a LONG adds the row
 * rate, a SHORT subtracts it. Returns % of notional.
 */
function fundingPaidPct(
  funding: readonly FlowFundingPoint[] | undefined,
  entryTs: number,
  exitTs: number,
  side: "LONG" | "SHORT",
): number {
  if (!funding || funding.length === 0) return 0;
  let total = 0;
  for (const row of funding) {
    if (row.ts <= entryTs) continue;
    if (row.ts > exitTs) break; // sorted asc
    total += side === "LONG" ? row.fundingRate : -row.fundingRate;
  }
  return total * 100;
}

// ---------------------------------------------------------------------------
// Walk-forward + report
// ---------------------------------------------------------------------------

interface WalkForwardPlan {
  readonly windows: readonly {
    readonly index: number;
    readonly trainStart: number;
    readonly trainEnd: number;
    readonly testStart: number;
    readonly testEnd: number;
  }[];
}

function planWalkForward(
  t0: number,
  t1: number,
  opts: FlowBacktestOptions,
): WalkForwardPlan {
  const trainMs = opts.trainDays * MS_PER_DAY;
  const testMs = opts.testDays * MS_PER_DAY;
  const span = t1 - t0;
  const maxSteps = Math.floor((span - trainMs) / testMs);
  const steps = Math.max(0, Math.min(opts.walkForwardSteps, maxSteps));
  const windows = [];
  for (let i = 0; i < steps; i++) {
    const testStart = t0 + trainMs + i * testMs;
    windows.push({
      index: i,
      trainStart: t0 + i * testMs,
      trainEnd: testStart,
      testStart,
      testEnd: testStart + testMs,
    });
  }
  return { windows };
}

function maxDrawdownPct(edges: readonly number[]): number {
  let peak = 0;
  let maxDd = 0;
  let cum = 0;
  for (const e of edges) {
    cum += e;
    if (cum > peak) peak = cum;
    const dd = peak - cum;
    if (dd > maxDd) maxDd = dd;
  }
  return maxDd;
}

function aggregateTrades(
  trades: readonly FlowBacktestTrade[],
): Omit<HoldTimeResult, "holdTimeHours"> {
  const sorted = [...trades].sort((a, b) => a.exitTs - b.exitTs);
  const total = sorted.length;
  const wins = sorted.filter((t) => t.win).length;
  const edgeSum = sorted.reduce((s, t) => s + t.netEdgePct, 0);

  const bySymbolMap = new Map<
    string,
    {
      symbol: string;
      trades: number;
      wins: number;
      netEdgeSumPct: number;
    }
  >();
  for (const t of sorted) {
    const agg = bySymbolMap.get(t.symbol) ?? {
      symbol: t.symbol,
      trades: 0,
      wins: 0,
      netEdgeSumPct: 0,
    };
    agg.trades++;
    if (t.win) agg.wins++;
    agg.netEdgeSumPct += t.netEdgePct;
    bySymbolMap.set(t.symbol, agg);
  }
  const bySymbol: SymbolTradeAggregate[] = [...bySymbolMap.values()]
    .map((a) => ({
      symbol: a.symbol,
      trades: a.trades,
      wins: a.wins,
      winRate: a.trades > 0 ? a.wins / a.trades : 0,
      avgEdgePct: a.trades > 0 ? a.netEdgeSumPct / a.trades : 0,
      netEdgeSumPct: a.netEdgeSumPct,
    }))
    .sort((a, b) => a.symbol.localeCompare(b.symbol));

  return {
    trades: sorted,
    totalTrades: total,
    winRate: total > 0 ? wins / total : 0,
    avgEdgePerTradePct: total > 0 ? edgeSum / total : 0,
    maxDrawdownPct: maxDrawdownPct(sorted.map((t) => t.netEdgePct)),
    expectancyPct: total > 0 ? edgeSum / total : 0,
    bySymbol,
  };
}

/**
 * Run the full flow-v1 walk-forward backtest over the given series.
 *
 * Walk-forward: rolling train/test windows over the merged history. A signal
 * is kept for a test window only when its whole label window (entry through
 * entry + max hold time) lies inside the window — signals whose 4-12h label
 * would cross a train/test boundary are purged. Every hold time in the grid
 * runs its own full simulation; the report carries all of them.
 */
export function runFlowBacktest(
  data: FlowBacktestData,
): FlowBacktestReport {
  const opts = data.options;

  // Per-symbol contexts + signals (computed once; windows filter them).
  const prepared = data.series.map((s) => {
    const ctxs = computeContexts(s.candles, s.oi, s.funding);
    const signals = computeFlowSignal(s.candles, s.oi, s.funding, opts, s.symbol);
    return { series: s, ctxs, signals };
  });

  const allTs = prepared.flatMap((p) => p.ctxs.map((c) => c.ts));
  if (allTs.length === 0) {
    return {
      options: opts,
      windows: [],
      byHoldTime: [],
      portfolio: emptyHoldTime(0),
    };
  }
  const t0 = Math.min(...allTs);
  const t1 = Math.max(...allTs);
  const plan = planWalkForward(t0, t1, opts);
  const maxHoldMs = Math.max(...opts.holdTimes) * MS_PER_HOUR;

  // Purge rule: label window [entryTs, entryTs + maxHoldMs] must sit fully
  // inside the test segment — never straddle a train/test boundary.
  const windowSignalCounts = plan.windows.map((w) => ({ signals: 0, purged: 0 }));
  const keptByWindow: readonly FlowSignal[][][] = plan.windows.map(() =>
    prepared.map(() => [] as FlowSignal[]),
  );

  prepared.forEach((p, si) => {
    for (const sig of p.signals) {
      if (sig.side === "NONE") continue;
      for (let wi = 0; wi < plan.windows.length; wi++) {
        const w = plan.windows[wi];
        if (sig.entryTs < w.testStart) continue;
        // A signal past the current window's end may still belong to a
        // later window — never break out of the window loop here.
        if (sig.entryTs >= w.testEnd) continue;
        if (sig.entryTs + maxHoldMs <= w.testEnd) {
          keptByWindow[wi][si].push(sig);
          windowSignalCounts[wi].signals++;
        } else {
          windowSignalCounts[wi].purged++;
        }
      }
    }
  });

  // Per hold time: run each window/symbol and union the trades.
  const byHoldTime: HoldTimeResult[] = opts.holdTimes.map((holdHours) => {
    const trades: FlowBacktestTrade[] = [];
    plan.windows.forEach((w, wi) => {
      prepared.forEach((p, si) => {
        const winSignals = keptByWindow[wi][si];
        if (winSignals.length === 0) return;
        const firstEntry = Math.min(...winSignals.map((s) => s.entryTs));
        const firstBarIdx = p.ctxs.findIndex((c) => c.ts >= firstEntry);
        if (firstBarIdx < 0) return;
        const ctxs = p.ctxs.slice(firstBarIdx);
        trades.push(
          ...runSymbolEngine(ctxs, winSignals, {
            symbol: p.series.symbol,
            holdTimeHours: holdHours,
            opts,
            funding: p.series.funding,
          }),
        );
      });
    });
    return { holdTimeHours: holdHours, ...aggregateTrades(trades) };
  });

  const windows = plan.windows.map((w, i) => ({
    ...w,
    signals: windowSignalCounts[i].signals,
    purged: windowSignalCounts[i].purged,
  }));

  return {
    options: opts,
    windows,
    byHoldTime,
    portfolio: byHoldTime[0] ?? emptyHoldTime(0),
  };
}

function emptyHoldTime(holdTimeHours: number): HoldTimeResult {
  return {
    holdTimeHours,
    trades: [],
    totalTrades: 0,
    winRate: 0,
    avgEdgePerTradePct: 0,
    maxDrawdownPct: 0,
    expectancyPct: 0,
    bySymbol: [],
  };
}
