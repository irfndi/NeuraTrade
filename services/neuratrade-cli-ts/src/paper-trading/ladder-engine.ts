/**
 * Incremental (live-stepping) Ladder Grid paper-trading engine.
 *
 * The ladder backtest (`src/scalping/ladder-grid.ts`) replays a full candle
 * array from scratch. This engine persists the ladder's runtime state
 * (open rungs, base anchors, capital) and advances it ONE candle at a time,
 * per iteration — so the demo soak can trade a ladder config forward over
 * live candles without reprocessing history. It mirrors the backtest's
 * per-bar state machine (progressive rung arming, touch = fill, per-rung TP,
 * ladder stop boundary, pause-after-loss) so forward returns reproduce the
 * validated sweep.
 *
 * Paper fills are simulated (touch = fill, same fill model as the backtest).
 * When `isLive` is set, each bar's fill/close events are ALSO executed on the
 * exchange through FuturesExchangeAdapter (limit entry per rung, market close
 * per exit), gated by RiskGuard; the paper capital ledger stays the PnL
 * source of truth and the bar is rolled back if any live order fails.
 */

import { Effect, Option } from "effect";
import {
  FuturesExchangeAdapter,
  type FuturesExchangeAdapterService,
  type FuturesOrderFill,
  type FuturesOrderRequest,
  type FuturesProductType,
  type FuturesMarginMode,
  type FuturesOrderSide,
} from "../exchange/futures-adapter.js";
import { ExchangeError } from "../exchange/adapter.js";
import {
  MarketDataError,
  MarketDataGateway,
  type MarketDataGatewayService,
} from "../market-data/gateway.js";
import { RiskError, RiskGuard, type RiskGuardService } from "../risk/guards.js";
import type { CandleLike } from "../scalping/types.js";
import { calculateSMA } from "../scalping/indicators.js";
import { makeCausalSymbolStats } from "../scalping/symbol-stats.js";
import { Decimal, money, toNumber, type Money } from "../utils/money.js";
import {
  PaperTradingRepository,
  PaperTradingRepositoryError,
  type PaperTradingRepositoryService,
} from "./repository.js";
import {
  type ContractSizeSpec,
  orderableQty,
  type LadderPaperRungState,
  type LadderPaperState,
} from "./types.js";

export interface LadderPaperTradingOptions {
  readonly exchange: string;
  readonly symbol: string;
  readonly timeframe: string;
  /** Simultaneous rungs per side (>= 1). */
  readonly rungs: number;
  readonly gridStepPct: number;
  readonly gridMaxGrids: number;
  readonly gridPauseAfterLossBars: number;
  readonly feePct: number;
  readonly slippageBps: number;
  readonly initialCapital: number;
  readonly trendFilterPeriod: number;
  readonly leverage: number;
  readonly onlyWithTrend?: boolean;
  readonly targetRatio?: number;
  readonly chopGateAdxThreshold?: number;
  /**
   * When > 0, a filled rung is force-closed at the current close if it stays
   * open for more than this many bars (market exit, taker fee). Default 0
   * disables the time-based exit, matching the validated backtest model.
   */
  readonly maxHoldBars?: number;
  /** When > 0, step through the last N stored candles one per iteration. */
  readonly replayBars?: number;
  /**
   * When true, bar fill/close events are also executed on the exchange
   * through FuturesExchangeAdapter (live orders); the paper ledger stays the
   * PnL source of truth. Default false = paper-only.
   */
  readonly isLive?: boolean;
  readonly productType?: FuturesProductType;
  readonly marginMode?: FuturesMarginMode;
  /** Position size fraction for live sizing (mirrors the grid cap). */
  readonly maxPositionPct?: number;
  /** Exchange contract constraints for orderable live sizing. */
  readonly contractSpecs?: ContractSizeSpec;
}

/** Milliseconds per candle for the given timeframe (default 15m). */
function timeframeMs(timeframe: string): number {
  const m = /^(\d+)([mhd])$/.exec(timeframe);
  if (!m) return 15 * 60 * 1000;
  const n = Number(m[1]);
  return m[2] === "h" ? n * 3600000 : m[2] === "d" ? n * 86400000 : n * 60000;
}

/** A rung fill detected on one bar (level touched, entry at the level). */
export interface LadderFillEvent {
  readonly rungIndex: number;
  readonly side: "long" | "short";
  /** Post-slippage entry price used by the paper ledger. */
  readonly fillPrice: number;
  /** Raw grid level (pre-slippage). */
  readonly level: number;
}

/** A rung close detected on one bar (target / stop / liquidation / max-hold). */
export interface LadderCloseEvent {
  readonly rungIndex: number;
  readonly side: "long" | "short";
  readonly entryPrice: number;
  readonly exitPrice: number;
  readonly reason: "target" | "stop" | "liquidation" | "max_hold";
}

export interface LadderBarEvents {
  readonly fills: readonly LadderFillEvent[];
  readonly closes: readonly LadderCloseEvent[];
}

/** Mutable accumulator used while advancing a bar. */
interface MutableBarEvents {
  fills: LadderFillEvent[];
  closes: LadderCloseEvent[];
}

export interface LadderPaperIterationResult {
  readonly action: "opened" | "closed" | "hold";
  readonly capital: number;
  readonly peakCapital: number;
  /** Number of rungs currently open (filled, not yet closed). */
  readonly openRungs: number;
  /** Trades closed this iteration. */
  readonly closedThisIteration: number;
  readonly note: string;
}

/** Internal mutable working state mirroring the backtest's loop variables. */
interface WorkingState {
  capital: Decimal;
  peak: Decimal;
  totalWins: number;
  totalLosses: number;
  longRungs: LadderPaperRungState[];
  shortRungs: LadderPaperRungState[];
  longBase: number;
  shortBase: number;
  paused: number;
}

export function freshWorkingState(initialCapital: number): WorkingState {
  const c = money(initialCapital);
  return {
    capital: c,
    peak: c,
    totalWins: 0,
    totalLosses: 0,
    longRungs: [],
    shortRungs: [],
    longBase: 0,
    shortBase: 0,
    paused: 0,
  };
}

/** Close one rung at `exitPrice`, updating working capital (mirrors the
 *  backtest's closeRung, with Decimal money accounting) and recording the
 *  close event for the live executor. */
function closeRung(
  w: WorkingState,
  r: LadderPaperRungState,
  exitPrice: number,
  reason: "target" | "stop" | "liquidation" | "max_hold",
  opts: LadderPaperTradingOptions,
  events: MutableBarEvents,
): void {
  const leverage = Math.max(1, opts.leverage ?? 1);
  const positionFraction = Math.max(0, Math.min(1, 1));
  const N = Math.max(1, Math.floor(opts.rungs ?? 1));
  const sizePerRung = positionFraction / N;
  const makerFee = (opts.feePct ?? 0) / 100;
  const takerFee = makerFee;
  const targetFee = makerFee * 2;
  const stopFee = makerFee + takerFee;
  const isLiquidation = reason === "liquidation";
  const fee = reason === "target" ? targetFee : stopFee;
  const pricePnl =
    r.side === "long"
      ? (exitPrice - r.entryPrice) / r.entryPrice
      : (r.entryPrice - exitPrice) / r.entryPrice;
  const net = pricePnl - fee;
  const leveragedReturn = isLiquidation ? -1 : net * leverage;
  const equityReturn = isLiquidation
    ? -sizePerRung
    : sizePerRung * leveragedReturn;
  const capitalBefore = w.capital;
  w.capital = Decimal.max(0, capitalBefore.mul(1 + equityReturn));
  if (w.peak.lessThan(w.capital)) w.peak = w.capital;
  if (isLiquidation || net < 0) {
    w.totalLosses += 1;
  } else {
    w.totalWins += 1;
  }
  events.closes.push({
    rungIndex: r.rungIndex,
    side: r.side,
    entryPrice: r.entryPrice,
    exitPrice,
    reason,
  });
}

function openRungCount(w: WorkingState): number {
  return (
    w.longRungs.filter((r) => r.filled).length +
    w.shortRungs.filter((r) => r.filled).length
  );
}

function liquidationPrice(
  side: "long" | "short",
  entryPrice: number,
  leverage: number,
): number {
  const l = Math.max(1, leverage);
  if (l <= 1) return 0;
  return side === "long" ? entryPrice * (1 - 1 / l) : entryPrice * (1 + 1 / l);
}

/**
 * Advance the ladder by ONE candle (`candles[i]`). Mirrors the backtest's
 * per-bar loop: re-seed when flat, fill rungs progressively on touch, then
 * manage liquidation / ladder-stop / per-rung take-profits. Mutates `w` and
 * returns the bar's fill/close events for the live executor.
 */
export function advanceLadderBar(
  w: WorkingState,
  candles: readonly CandleLike[],
  i: number,
  opts: LadderPaperTradingOptions,
  trendSeries: readonly number[] | null = null,
): LadderBarEvents {
  const fills: LadderFillEvent[] = [];
  const closes: LadderCloseEvent[] = [];
  const events: MutableBarEvents = { fills, closes };
  if (w.paused > 0) {
    w.paused -= 1;
    return { fills, closes };
  }
  const c = candles[i];
  const mid = c.open;
  const step = mid * (opts.gridStepPct / 100);
  const slippage = 1 + opts.slippageBps / 10000;
  const N = Math.max(1, Math.floor(opts.rungs ?? 1));
  const targetRatio = Math.max(0.001, opts.targetRatio ?? 1);
  const leverage = Math.max(1, opts.leverage ?? 1);
  const maxHoldBars = Math.max(0, Math.floor(opts.maxHoldBars ?? 0));
  const msPerBar = timeframeMs(opts.timeframe);

  const chopGateActive =
    (opts.chopGateAdxThreshold ?? 0) > 0 &&
    makeCausalSymbolStats(candles, "15m")(i).adx14 >=
      (opts.chopGateAdxThreshold ?? 0);
  const trendFilterPeriod = Math.max(0, opts.trendFilterPeriod ?? 0);
  const onlyWithTrend = opts.onlyWithTrend ?? false;
  const trend =
    trendSeries !== null && trendSeries.length > i ? trendSeries[i] : NaN;
  if (trendFilterPeriod > 0 && (trend === null || isNaN(trend)))
    return { fills, closes };

  // Re-seed long ladder while flat.
  if (!w.longRungs.some((r) => r.filled)) {
    const allowLong =
      !chopGateActive &&
      (!onlyWithTrend || (trend !== null && !isNaN(trend) && c.close > trend));
    if (allowLong) {
      w.longBase = mid;
      w.longRungs = [];
      for (let k = 1; k <= N; k++) {
        w.longRungs.push({
          rungIndex: k,
          side: "long",
          level: mid - k * step,
          step,
          filled: false,
          entryPrice: 0,
          entryBar: 0,
          entryTimestamp: 0,
        });
      }
    } else {
      w.longRungs = [];
      w.longBase = 0;
    }
  }

  // Re-seed short ladder while flat.
  if (!w.shortRungs.some((r) => r.filled)) {
    const allowShort =
      !chopGateActive &&
      (!onlyWithTrend || (trend !== null && !isNaN(trend) && c.close < trend));
    if (allowShort) {
      w.shortBase = mid;
      w.shortRungs = [];
      for (let k = 1; k <= N; k++) {
        w.shortRungs.push({
          rungIndex: k,
          side: "short",
          level: mid + k * step,
          step,
          filled: false,
          entryPrice: 0,
          entryBar: 0,
          entryTimestamp: 0,
        });
      }
    } else {
      w.shortRungs = [];
      w.shortBase = 0;
    }
  }

  // Manage LONG ladder.
  if (w.longRungs.length > 0) {
    for (let k = 0; k < w.longRungs.length; k++) {
      const r = w.longRungs[k];
      if (r.filled) continue;
      const prevFilled = k === 0 || w.longRungs[k - 1].filled;
      if (prevFilled && c.low <= r.level) {
        w.longRungs[k] = {
          ...r,
          filled: true,
          entryPrice: r.level * slippage,
          entryBar: i,
          entryTimestamp: c.timestamp.getTime(),
        };
        fills.push({
          rungIndex: r.rungIndex,
          side: "long",
          fillPrice: r.level * slippage,
          level: r.level,
        });
      }
    }
    const filledLong = w.longRungs.filter((r) => r.filled);
    if (filledLong.length > 0) {
      const boundary = w.longBase - step * (N + opts.gridMaxGrids);
      const longLiqs = filledLong
        .map((r) => liquidationPrice("long", r.entryPrice, leverage))
        .filter((p) => p > 0);
      const liq = longLiqs.length > 0 ? Math.max(...longLiqs) : 0;
      if (liq > 0 && c.low <= liq) {
        for (const r of filledLong)
          closeRung(w, r, liq * slippage, "liquidation", opts, events);
        w.longRungs = [];
        w.longBase = 0;
        if (opts.gridPauseAfterLossBars > 0)
          w.paused = opts.gridPauseAfterLossBars;
      } else if (c.low <= boundary) {
        for (const r of filledLong)
          closeRung(w, r, boundary * slippage, "stop", opts, events);
        w.longRungs = [];
        w.longBase = 0;
        if (opts.gridPauseAfterLossBars > 0)
          w.paused = opts.gridPauseAfterLossBars;
      } else {
        const stillOpen: LadderPaperRungState[] = [];
        let anyFillClosed = false;
        for (const r of w.longRungs) {
          if (!r.filled) {
            stillOpen.push(r);
            continue;
          }
          const target = r.entryPrice + r.step * targetRatio;
          if (c.high >= target) {
            closeRung(w, r, target / slippage, "target", opts, events);
            anyFillClosed = true;
          } else if (
            maxHoldBars > 0 &&
            r.entryTimestamp > 0 &&
            c.timestamp.getTime() - r.entryTimestamp >= maxHoldBars * msPerBar
          ) {
            closeRung(w, r, c.close / slippage, "max_hold", opts, events);
            anyFillClosed = true;
          } else {
            stillOpen.push(r);
          }
        }
        w.longRungs = stillOpen;
        const openFilled = w.longRungs.filter((r) => r.filled).length;
        if (openFilled === 0 && anyFillClosed) {
          w.longRungs = [];
          w.longBase = 0;
        }
      }
    }
  }

  // Manage SHORT ladder.
  if (w.shortRungs.length > 0) {
    for (let k = 0; k < w.shortRungs.length; k++) {
      const r = w.shortRungs[k];
      if (r.filled) continue;
      const prevFilled = k === 0 || w.shortRungs[k - 1].filled;
      if (prevFilled && c.high >= r.level) {
        w.shortRungs[k] = {
          ...r,
          filled: true,
          entryPrice: r.level / slippage,
          entryBar: i,
          entryTimestamp: c.timestamp.getTime(),
        };
        fills.push({
          rungIndex: r.rungIndex,
          side: "short",
          fillPrice: r.level / slippage,
          level: r.level,
        });
      }
    }
    const filledShort = w.shortRungs.filter((r) => r.filled);
    if (filledShort.length > 0) {
      const boundary = w.shortBase + step * (N + opts.gridMaxGrids);
      const shortLiqs = filledShort
        .map((r) => liquidationPrice("short", r.entryPrice, leverage))
        .filter((p) => p > 0);
      const liq = shortLiqs.length > 0 ? Math.min(...shortLiqs) : 0;
      if (liq > 0 && c.high >= liq) {
        for (const r of filledShort)
          closeRung(w, r, liq / slippage, "liquidation", opts, events);
        w.shortRungs = [];
        w.shortBase = 0;
        if (opts.gridPauseAfterLossBars > 0)
          w.paused = opts.gridPauseAfterLossBars;
      } else if (c.high >= boundary) {
        for (const r of filledShort)
          closeRung(w, r, boundary / slippage, "stop", opts, events);
        w.shortRungs = [];
        w.shortBase = 0;
        if (opts.gridPauseAfterLossBars > 0)
          w.paused = opts.gridPauseAfterLossBars;
      } else {
        const stillOpen: LadderPaperRungState[] = [];
        let anyFillClosed = false;
        for (const r of w.shortRungs) {
          if (!r.filled) {
            stillOpen.push(r);
            continue;
          }
          const target = r.entryPrice - r.step * targetRatio;
          if (c.low <= target) {
            closeRung(w, r, target * slippage, "target", opts, events);
            anyFillClosed = true;
          } else if (
            maxHoldBars > 0 &&
            r.entryTimestamp > 0 &&
            c.timestamp.getTime() - r.entryTimestamp >= maxHoldBars * msPerBar
          ) {
            closeRung(w, r, c.close * slippage, "max_hold", opts, events);
            anyFillClosed = true;
          } else {
            stillOpen.push(r);
          }
        }
        w.shortRungs = stillOpen;
        const openFilled = w.shortRungs.filter((r) => r.filled).length;
        if (openFilled === 0 && anyFillClosed) {
          w.shortRungs = [];
          w.shortBase = 0;
        }
      }
    }
  }

  return { fills, closes };
}

export function freshLadderState(
  options: LadderPaperTradingOptions,
): LadderPaperState {
  return {
    exchange: options.exchange,
    symbol: options.symbol,
    timeframe: options.timeframe,
    initialCapital: options.initialCapital,
    capital: money(options.initialCapital),
    peakCapital: money(options.initialCapital),
    totalWins: 0,
    totalLosses: 0,
    longRungs: [],
    shortRungs: [],
    longBase: 0,
    shortBase: 0,
    paused: 0,
    gridStepPct: options.gridStepPct,
    gridMaxGrids: options.gridMaxGrids,
    gridPauseAfterLossBars: options.gridPauseAfterLossBars,
    rungs: options.rungs,
    targetRatio: options.targetRatio ?? 1,
    onlyWithTrend: options.onlyWithTrend ?? false,
    chopGateAdxThreshold: options.chopGateAdxThreshold ?? 0,
    maxHoldBars: Math.max(0, Math.floor(options.maxHoldBars ?? 0)),
    lastTimestamp: null,
    updatedAt: new Date(),
  };
}

function stateToWorking(state: LadderPaperState): WorkingState {
  return {
    capital: state.capital,
    peak: state.peakCapital,
    totalWins: state.totalWins,
    totalLosses: state.totalLosses,
    longRungs: [...state.longRungs],
    shortRungs: [...state.shortRungs],
    longBase: state.longBase,
    shortBase: state.shortBase,
    paused: state.paused,
  };
}

function workingToState(
  options: LadderPaperTradingOptions,
  w: WorkingState,
  lastTimestamp: Date | null,
  previous: LadderPaperState,
): LadderPaperState {
  return {
    ...previous,
    capital: w.capital,
    peakCapital: w.peak,
    totalWins: w.totalWins,
    totalLosses: w.totalLosses,
    longRungs: w.longRungs,
    shortRungs: w.shortRungs,
    longBase: w.longBase,
    shortBase: w.shortBase,
    paused: w.paused,
    lastTimestamp,
    updatedAt: new Date(),
  };
}

export function configMatchesLadderState(
  state: LadderPaperState,
  options: LadderPaperTradingOptions,
): boolean {
  return (
    state.exchange === options.exchange &&
    state.symbol === options.symbol &&
    state.timeframe === options.timeframe &&
    state.initialCapital === options.initialCapital &&
    state.gridStepPct === options.gridStepPct &&
    state.gridMaxGrids === options.gridMaxGrids &&
    state.gridPauseAfterLossBars === options.gridPauseAfterLossBars &&
    state.rungs === options.rungs &&
    state.targetRatio === (options.targetRatio ?? 1) &&
    state.onlyWithTrend === (options.onlyWithTrend ?? false) &&
    state.chopGateAdxThreshold === (options.chopGateAdxThreshold ?? 0) &&
    state.maxHoldBars === Math.max(0, Math.floor(options.maxHoldBars ?? 0))
  );
}

/**
 * Deep-ish snapshot of the working state for the live rollback path (rung
 * arrays are copied; Decimal capital is immutable).
 */
function cloneWorking(w: WorkingState): WorkingState {
  return {
    capital: w.capital,
    peak: w.peak,
    totalWins: w.totalWins,
    totalLosses: w.totalLosses,
    longRungs: w.longRungs.map((r) => ({ ...r })),
    shortRungs: w.shortRungs.map((r) => ({ ...r })),
    longBase: w.longBase,
    shortBase: w.shortBase,
    paused: w.paused,
  };
}

/** Per-rung orderable size (one rung's share of the position cap). */
function ladderRungSize(
  capital: Decimal,
  options: LadderPaperTradingOptions,
): { size: Money; skipReason?: string } {
  const rungs = Math.max(1, Math.floor(options.rungs ?? 1));
  const positionFraction = Math.max(
    0,
    Math.min(1, (options.maxPositionPct ?? 100) / 100),
  );
  const perRungAllocation = capital.times(positionFraction).div(rungs);
  // The fill price is unknown here (caller supplies it via the event); size
  // is re-derived per event using the event's fill price, so this returns the
  // allocation only. The caller divides by the fill price.
  return { size: perRungAllocation };
}

/** Orderable qty for one rung at `fillPrice`, contract-step rounded. */
function ladderRungQty(
  capital: Decimal,
  options: LadderPaperTradingOptions,
  fillPrice: Decimal,
): { qty: Money; skipReason?: string } {
  const perRungAllocation = ladderRungSize(capital, options).size;
  if (perRungAllocation.lessThanOrEqualTo(0)) {
    return { qty: money(0), skipReason: "per-rung allocation is zero" };
  }
  if (fillPrice.lessThanOrEqualTo(0)) {
    return { qty: money(0), skipReason: "non-positive fill price" };
  }
  const spec = options.contractSpecs;
  const raw = perRungAllocation.div(fillPrice);
  if (spec === undefined) return { qty: Decimal.max(0, raw) };
  const qty = orderableQty(raw, spec, fillPrice, perRungAllocation);
  const notional = qty.times(fillPrice);
  const leverage = Math.max(1, options.leverage ?? 1);
  const margin = notional.div(leverage);
  if (margin.greaterThan(perRungAllocation)) {
    return {
      qty: money(0),
      skipReason: `min orderable notional ${notional.toFixed(2)} USDT requires margin ${margin.toFixed(2)} at ${leverage}x, exceeding the ${toNumber(perRungAllocation).toFixed(2)} USDT per-rung cap`,
    };
  }
  return { qty };
}

/**
 * Execute one bar's fill/close events on the exchange. Fills place limit
 * entries at the rung level (the bar already touched it, so they fill
 * immediately); closes market-close the rung's size. Any failure fails the
 * effect so the caller rolls the paper ledger back and retries next bar.
 */
function executeLadderBarLive(
  fills: readonly LadderFillEvent[],
  closes: readonly LadderCloseEvent[],
  w: WorkingState,
  options: LadderPaperTradingOptions,
  adapter: FuturesExchangeAdapterService,
  riskGuard: RiskGuardService,
  repo: PaperTradingRepositoryService,
): Effect.Effect<
  void,
  ExchangeError | RiskError | PaperTradingRepositoryError,
  never
> {
  return Effect.gen(function* () {
    const productType = options.productType ?? "USDT-FUTURES";
    const marginMode = options.marginMode ?? "isolated";
    const leverage = Math.max(1, options.leverage ?? 1);
    for (const fill of fills) {
      const side: FuturesOrderSide = fill.side === "long" ? "buy" : "sell";
      const fillPrice = money(fill.fillPrice);
      const tradesTodayCount = yield* repo.countTradesForDate(new Date());
      const todayPnl = yield* repo
        .getTodayRealizedPnl()
        .pipe(Effect.orElseSucceed(() => money(0)));
      yield* riskGuard.check({
        isLive: true,
        capital: toNumber(w.capital),
        peakCapital: toNumber(w.peak),
        startOfDayCapital: toNumber(w.capital),
        dailyRealizedPnl: toNumber(todayPnl),
        tradesTodayCount,
        positionValue: toNumber(
          w.capital.times((options.maxPositionPct ?? 100) / 100),
        ),
        symbol: options.symbol,
        side,
        leverage,
        productType,
      });
      const sized = ladderRungQty(w.capital, options, fillPrice);
      if (sized.qty.lessThanOrEqualTo(0)) {
        return yield* Effect.fail(
          new ExchangeError(
            `ladder rung size unavailable: ${sized.skipReason ?? "zero qty"}`,
          ),
        );
      }
      yield* adapter.setLeverage(
        options.symbol,
        productType,
        marginMode,
        leverage,
      );
      const request: FuturesOrderRequest = {
        symbol: options.symbol,
        side,
        type: "limit",
        productType,
        marginMode,
        leverage,
        size: sized.qty,
        price: fillPrice,
        reduceOnly: false,
      };
      yield* adapter.placeOrder(request);
    }
    for (const close of closes) {
      const side: FuturesOrderSide = close.side === "long" ? "sell" : "buy";
      const exitPrice = money(close.exitPrice);
      const sized = ladderRungQty(w.capital, options, exitPrice);
      if (sized.qty.lessThanOrEqualTo(0)) continue;
      yield* adapter.closePosition({
        symbol: options.symbol,
        side,
        productType,
        marginMode,
        leverage,
        size: sized.qty,
        price: exitPrice,
      });
    }
  });
}

/**
 * Run one paper-trading iteration: fetch candles, advance the ladder over any
 * candles newer than the last processed one, persist, and report.
 */
export function runLadderPaperTradingIteration(
  options: LadderPaperTradingOptions,
): Effect.Effect<
  LadderPaperIterationResult,
  MarketDataError | PaperTradingRepositoryError,
  MarketDataGatewayService | PaperTradingRepositoryService
> {
  return Effect.gen(function* () {
    const repo = yield* PaperTradingRepository;
    yield* repo.ensureTables();

    let state: LadderPaperState | null = yield* repo.getLadderState(
      options.exchange,
      options.symbol,
      options.timeframe,
    );
    if (state === null) {
      state = freshLadderState(options);
      yield* repo.saveLadderState(state);
    } else if (!configMatchesLadderState(state, options)) {
      if (isFlat(state)) {
        // Flat state under different params: re-seed fresh so the next entry
        // trades the current rung geometry.
        state = freshLadderState(options);
        yield* repo.saveLadderState(state);
      } else {
        // Open ladder under different params: refuse rather than silently
        // trading a stale rung geometry against the new config.
        return {
          action: "hold",
          capital: toNumber(state.capital),
          peakCapital: toNumber(state.peakCapital),
          openRungs: openRungCount(stateToWorking(state)),
          closedThisIteration: 0,
          note: "config mismatch on open ladder (flat to re-seed)",
        };
      }
    }

    const gateway = yield* MarketDataGateway;
    const replayBars = options.replayBars ?? 0;
    const adxWarmup = (options.chopGateAdxThreshold ?? 0) > 0 ? 14 * 2 + 2 : 0;
    const requiredCandles =
      replayBars > 0
        ? replayBars + options.trendFilterPeriod + 5
        : Math.max(options.trendFilterPeriod + 1, 2, adxWarmup);
    const candles = yield* gateway.fetchOHLCV(
      options.exchange,
      options.symbol,
      options.timeframe,
      requiredCandles,
    );
    if (candles.length === 0) {
      return {
        action: "hold",
        capital: toNumber(state.capital),
        peakCapital: toNumber(state.peakCapital),
        openRungs: openRungCount(stateToWorking(state)),
        closedThisIteration: 0,
        note: "no candles",
      };
    }

    let startIndex: number;
    if (replayBars > 0) {
      if (state.lastTimestamp === null) {
        startIndex = Math.max(
          options.trendFilterPeriod,
          candles.length - replayBars,
        );
      } else {
        const nextIndex = candles.findIndex(
          (c) => c.timestamp.getTime() > state!.lastTimestamp!.getTime(),
        );
        if (nextIndex === -1) {
          return {
            action: "hold",
            capital: toNumber(state.capital),
            peakCapital: toNumber(state.peakCapital),
            openRungs: openRungCount(stateToWorking(state)),
            closedThisIteration: 0,
            note: "no new replay candle",
          };
        }
        startIndex = nextIndex;
      }
    } else {
      // Live forward stepping: only advance bars NEWER than the last
      // processed candle. When no new bar exists yet (fetch raced ahead of
      // the candle close), hold instead of reprocessing history — replaying
      // already-settled bars would double-count fills and closes.
      if (state.lastTimestamp === null) {
        startIndex = Math.max(1, options.trendFilterPeriod);
      } else {
        const nextIndex = candles.findIndex(
          (c) => c.timestamp.getTime() > state!.lastTimestamp!.getTime(),
        );
        if (nextIndex === -1) {
          return {
            action: "hold",
            capital: toNumber(state.capital),
            peakCapital: toNumber(state.peakCapital),
            openRungs: openRungCount(stateToWorking(state)),
            closedThisIteration: 0,
            note: "no new candle",
          };
        }
        startIndex = nextIndex;
      }
    }

    const w = stateToWorking(state);
    const trendSeries =
      (options.trendFilterPeriod ?? 0) > 0
        ? calculateSMA(
            candles.map((ck) => ck.close),
            options.trendFilterPeriod ?? 0,
          )
        : null;
    const openBefore = openRungCount(w);
    const snapshot = cloneWorking(w);
    let closedThisIteration = 0;
    const iterationFills: LadderFillEvent[] = [];
    const iterationCloses: LadderCloseEvent[] = [];
    for (let i = startIndex; i < candles.length; i++) {
      const barEvents = advanceLadderBar(w, candles, i, options, trendSeries);
      closedThisIteration += barEvents.closes.length;
      iterationFills.push(...barEvents.fills);
      iterationCloses.push(...barEvents.closes);
    }
    const last = candles[candles.length - 1];

    // Live path: execute this bar's events on the exchange. On ANY failure the
    // paper ledger rolls back to the pre-loop snapshot (the state is not
    // committed) and the iteration holds — the exchange may hold orphan orders
    // from the partially executed batch, which the operator can review.
    if (
      options.isLive === true &&
      (iterationFills.length > 0 || iterationCloses.length > 0)
    ) {
      const adapter = yield* Effect.serviceOption(FuturesExchangeAdapter);
      const riskGuard = yield* Effect.serviceOption(RiskGuard);
      if (Option.isNone(adapter) || Option.isNone(riskGuard)) {
        return {
          action: "hold",
          capital: toNumber(state.capital),
          peakCapital: toNumber(state.peakCapital),
          openRungs: openRungCount(stateToWorking(state)),
          closedThisIteration: 0,
          note: "live ladder requested but FuturesExchangeAdapter/RiskGuard not provided",
        };
      }
      const outcome = yield* executeLadderBarLive(
        iterationFills,
        iterationCloses,
        w,
        options,
        adapter.value,
        riskGuard.value,
        repo,
      ).pipe(Effect.result);
      if (outcome._tag === "Failure") {
        // Roll the ledger back; do not commit the bar's simulated trades.
        const rolled = cloneWorking(snapshot);
        w.capital = rolled.capital;
        w.peak = rolled.peak;
        w.totalWins = rolled.totalWins;
        w.totalLosses = rolled.totalLosses;
        w.longRungs = rolled.longRungs;
        w.shortRungs = rolled.shortRungs;
        w.longBase = rolled.longBase;
        w.shortBase = rolled.shortBase;
        w.paused = rolled.paused;
        return {
          action: "hold",
          capital: toNumber(w.capital),
          peakCapital: toNumber(w.peak),
          openRungs: openRungCount(w),
          closedThisIteration: 0,
          note: `live ladder execution failed (bar rolled back): ${outcome.failure.reason ?? String(outcome.failure)}`,
        };
      }
    }

    state = workingToState(options, w, last.timestamp, state);
    yield* repo.saveLadderState(state);

    const openAfter = openRungCount(w);
    const action: "opened" | "closed" | "hold" =
      closedThisIteration > 0
        ? "closed"
        : openAfter > openBefore
          ? "opened"
          : "hold";
    return {
      action,
      capital: toNumber(state.capital),
      peakCapital: toNumber(state.peakCapital),
      openRungs: openAfter,
      closedThisIteration,
      note: `ladder iter over ${candles.length - startIndex} bars${options.isLive ? " [LIVE]" : ""}`,
    };
  });
}

function isFlat(state: LadderPaperState): boolean {
  return (
    state.longRungs.filter((r) => r.filled).length === 0 &&
    state.shortRungs.filter((r) => r.filled).length === 0
  );
}
