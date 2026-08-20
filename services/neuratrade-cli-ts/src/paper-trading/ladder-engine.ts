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
import {
  CircuitBreaker,
  CircuitBreakerError,
} from "../risk/circuit-breaker.js";
import { KillSwitch, KillSwitchError } from "../risk/kill-switch.js";
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
  type LadderPaperTrade,
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
  /** Risk limit cap on dynamic leverage (default 10x). */
  readonly maxLeverage?: number;
  /**
   * When true, leverage is fully DYNAMIC: it is computed from the account
   * size + per-position budget (the accountScaledLeverageCap), ignoring any
   * static --leverage. When false/omitted, leverage is the minimum of the
   * static --leverage and the account-scaled cap.
   */
  readonly fullyDynamicLeverage?: boolean;
  /**
   * How to resolve a persisted state whose config no longer matches the
   * current options while rungs are open. Default "hold" refuses to trade
   * stale geometry (the ladder freezes until manually reset). "force-reseed"
   * closes open rungs at the current close and re-seeds fresh — used by the
   * ladder soak so a whitelist edit (e.g. a gridMaxGrids change) self-heals
   * instead of deadlocking a symbol with an unmanaged position.
   */
  readonly configMismatchAction?: "hold" | "force-reseed";
  /**
   * When > 0, an open rung whose UNREALIZED loss exceeds this percent of its
   * entry (a single leveraged position bleeding too much) is force-closed at
   * the current close before the ladder-stop boundary can take it out deeper.
   * 0 disables the per-position guard (default). Mirrors the account-level
   * maxDrawdownPct but scoped to one position.
   */
  readonly maxPositionDrawdownPct?: number;
  /**
   * Stop distance as a multiple of the grid step. When > 0, losses run
   * looser than wins, and a 2:1 skew is typical — e.g. stopRatio 2 with
   * targetRatio 4 (SL 2 steps, TP 4 steps) gives a 2R win / 1R loss trade.
   * When 0 (default), the legacy ladder boundary
   * `base ± step*(N+gridMaxGrids)` applies unchanged (backtest parity).
   */
  readonly stopRatio?: number;
  /** Avoid assuming that an OHLC candle hits a newly entered rung's target. */
  readonly conservativeIntrabar?: boolean;
  /** Maximum total position notional as a percentage of capital. */
  readonly maxNotionalPct?: number;
  /** Per-side taker fee for non-target exits; defaults to feePct. */
  readonly takerExitFeePct?: number;
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

/** How long a symbol stays blocked after a "must agree to the Trading Terms"
 *  (110123/110125/110126) fill failure. The agreement is an exchange-UI action
 *  with no API, so the engine stops retrying for the cooldown. */
const AGREEMENT_BLOCK_MS = 6 * 60 * 60 * 1000;
const agreementBlockedUntil = new Map<string, number>();

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
  readonly capitalBefore: string;
  readonly capitalAfter: string;
  readonly pnl: string;
  readonly pnlPct: string;
  readonly entryTimestamp: number;
  readonly closedTimestamp: number;
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
  closedAt: Date,
): void {
  const leverage = Math.max(1, opts.leverage ?? 1);
  const positionFraction = Math.max(
    0,
    Math.min(1, (opts.maxPositionPct ?? 100) / 100),
  );
  const N = Math.max(1, Math.floor(opts.rungs ?? 1));
  const sizePerRung = positionFraction / N;
  const makerFee = (opts.feePct ?? 0) / 100;
  const takerFee = (opts.takerExitFeePct ?? opts.feePct ?? 0) / 100;
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
  const pnl = w.capital.minus(capitalBefore);
  events.closes.push({
    rungIndex: r.rungIndex,
    side: r.side,
    entryPrice: r.entryPrice,
    exitPrice,
    reason,
    capitalBefore: capitalBefore.toString(),
    capitalAfter: w.capital.toString(),
    pnl: pnl.toString(),
    pnlPct: capitalBefore.isZero()
      ? "0"
      : pnl.div(capitalBefore).times(100).toString(),
    entryTimestamp: r.entryTimestamp,
    closedTimestamp: closedAt.getTime(),
  });
}

function ladderTradeFromClose(
  options: LadderPaperTradingOptions,
  close: LadderCloseEvent,
): LadderPaperTrade {
  const closedAt = new Date(close.closedTimestamp);
  const openedAt = new Date(
    close.entryTimestamp > 0 ? close.entryTimestamp : close.closedTimestamp,
  );
  return {
    id: [
      "ladder-trade",
      options.exchange,
      options.symbol,
      options.timeframe,
      close.side,
      close.rungIndex,
      close.entryTimestamp,
      close.closedTimestamp,
    ].join(":"),
    exchange: options.exchange,
    symbol: options.symbol,
    timeframe: options.timeframe,
    side: close.side,
    rungIndex: close.rungIndex,
    entryPrice: money(close.entryPrice),
    exitPrice: money(close.exitPrice),
    capitalBefore: money(close.capitalBefore),
    capitalAfter: money(close.capitalAfter),
    pnl: money(close.pnl),
    pnlPct: money(close.pnlPct),
    exitReason: close.reason,
    openedAt,
    closedAt,
  };
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
      const stopRatio = opts.stopRatio ?? 0;
      // Step-relative stop: each rung exits at entry - step*stopRatio
      // (long). The deepest filled rung's stop is the ladder's effective
      // downside; using the shallowest filled rung's stop would let a deep
      // fill run too far, so the stop level is anchored to the DEEPEST
      // filled rung's entry for a consistent per-position R:R.
      const stopLevel =
        stopRatio > 0
          ? Math.min(...filledLong.map((r) => r.entryPrice)) - step * stopRatio
          : w.longBase - step * (N + opts.gridMaxGrids);
      const boundary = stopLevel;
      const longLiqs = filledLong
        .map((r) => liquidationPrice("long", r.entryPrice, leverage))
        .filter((p) => p > 0);
      const liq = longLiqs.length > 0 ? Math.max(...longLiqs) : 0;
      const maxPosDd = opts.maxPositionDrawdownPct ?? 0;
      if (liq > 0 && c.low <= liq) {
        for (const r of filledLong)
          closeRung(
            w,
            r,
            liq * slippage,
            "liquidation",
            opts,
            events,
            c.timestamp,
          );
        w.longRungs = [];
        w.longBase = 0;
        if (opts.gridPauseAfterLossBars > 0)
          w.paused = opts.gridPauseAfterLossBars;
      } else if (c.low <= boundary) {
        for (const r of filledLong)
          closeRung(
            w,
            r,
            boundary * slippage,
            "stop",
            opts,
            events,
            c.timestamp,
          );
        w.longRungs = [];
        w.longBase = 0;
        if (opts.gridPauseAfterLossBars > 0)
          w.paused = opts.gridPauseAfterLossBars;
      } else if (
        maxPosDd > 0 &&
        filledLong.some(
          (r) =>
            r.entryPrice > 0 &&
            ((r.entryPrice - c.close) / r.entryPrice) * 100 > maxPosDd,
        )
      ) {
        // Per-position max-drawdown kill: a single leveraged rung bleeding
        // past the threshold is closed at the current close before the wider
        // ladder-stop boundary can take it out deeper (or a second rung
        // compounds the loss). Scoped to the losing rungs only.
        const killed = filledLong.filter(
          (r) =>
            r.entryPrice > 0 &&
            ((r.entryPrice - c.close) / r.entryPrice) * 100 > maxPosDd,
        );
        const survivors = filledLong.filter((r) => !killed.includes(r));
        for (const r of killed)
          closeRung(
            w,
            r,
            c.close / slippage,
            "stop",
            opts,
            events,
            c.timestamp,
          );
        if (survivors.length === 0) {
          w.longRungs = [];
          w.longBase = 0;
          if (opts.gridPauseAfterLossBars > 0)
            w.paused = opts.gridPauseAfterLossBars;
        } else {
          w.longRungs = survivors;
        }
      } else {
        const stillOpen: LadderPaperRungState[] = [];
        let anyFillClosed = false;
        for (const r of w.longRungs) {
          if (!r.filled) {
            stillOpen.push(r);
            continue;
          }
          const target = r.entryPrice + r.step * targetRatio;
          if (
            c.high >= target &&
            ((opts.conservativeIntrabar ?? true) === false || r.entryBar < i)
          ) {
            closeRung(
              w,
              r,
              target / slippage,
              "target",
              opts,
              events,
              c.timestamp,
            );
            anyFillClosed = true;
          } else if (
            maxHoldBars > 0 &&
            r.entryTimestamp > 0 &&
            c.timestamp.getTime() - r.entryTimestamp >= maxHoldBars * msPerBar
          ) {
            closeRung(
              w,
              r,
              c.close / slippage,
              "max_hold",
              opts,
              events,
              c.timestamp,
            );
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
      const stopRatio = opts.stopRatio ?? 0;
      // Step-relative stop (short mirror): stop at the deepest filled rung's
      // entry + step*stopRatio.
      const stopLevel =
        stopRatio > 0
          ? Math.max(...filledShort.map((r) => r.entryPrice)) + step * stopRatio
          : w.shortBase + step * (N + opts.gridMaxGrids);
      const boundary = stopLevel;
      const shortLiqs = filledShort
        .map((r) => liquidationPrice("short", r.entryPrice, leverage))
        .filter((p) => p > 0);
      const liq = shortLiqs.length > 0 ? Math.min(...shortLiqs) : 0;
      const maxPosDd = opts.maxPositionDrawdownPct ?? 0;
      if (liq > 0 && c.high >= liq) {
        for (const r of filledShort)
          closeRung(
            w,
            r,
            liq / slippage,
            "liquidation",
            opts,
            events,
            c.timestamp,
          );
        w.shortRungs = [];
        w.shortBase = 0;
        if (opts.gridPauseAfterLossBars > 0)
          w.paused = opts.gridPauseAfterLossBars;
      } else if (c.high >= boundary) {
        for (const r of filledShort)
          closeRung(
            w,
            r,
            boundary / slippage,
            "stop",
            opts,
            events,
            c.timestamp,
          );
        w.shortRungs = [];
        w.shortBase = 0;
        if (opts.gridPauseAfterLossBars > 0)
          w.paused = opts.gridPauseAfterLossBars;
      } else if (
        maxPosDd > 0 &&
        filledShort.some(
          (r) =>
            r.entryPrice > 0 &&
            ((c.close - r.entryPrice) / r.entryPrice) * 100 > maxPosDd,
        )
      ) {
        // Per-position max-drawdown kill (short mirror): a leveraged short
        // rung bleeding past the threshold is closed at the current close.
        const killed = filledShort.filter(
          (r) =>
            r.entryPrice > 0 &&
            ((c.close - r.entryPrice) / r.entryPrice) * 100 > maxPosDd,
        );
        const survivors = filledShort.filter((r) => !killed.includes(r));
        for (const r of killed)
          closeRung(
            w,
            r,
            c.close * slippage,
            "stop",
            opts,
            events,
            c.timestamp,
          );
        if (survivors.length === 0) {
          w.shortRungs = [];
          w.shortBase = 0;
          if (opts.gridPauseAfterLossBars > 0)
            w.paused = opts.gridPauseAfterLossBars;
        } else {
          w.shortRungs = survivors;
        }
      } else {
        const stillOpen: LadderPaperRungState[] = [];
        let anyFillClosed = false;
        for (const r of w.shortRungs) {
          if (!r.filled) {
            stillOpen.push(r);
            continue;
          }
          const target = r.entryPrice - r.step * targetRatio;
          if (
            c.low <= target &&
            ((opts.conservativeIntrabar ?? true) === false || r.entryBar < i)
          ) {
            closeRung(
              w,
              r,
              target * slippage,
              "target",
              opts,
              events,
              c.timestamp,
            );
            anyFillClosed = true;
          } else if (
            maxHoldBars > 0 &&
            r.entryTimestamp > 0 &&
            c.timestamp.getTime() - r.entryTimestamp >= maxHoldBars * msPerBar
          ) {
            closeRung(
              w,
              r,
              c.close * slippage,
              "max_hold",
              opts,
              events,
              c.timestamp,
            );
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
    stopRatio: Math.max(0, options.stopRatio ?? 0),
    conservativeIntrabar: options.conservativeIntrabar ?? true,
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
    state.maxHoldBars === Math.max(0, Math.floor(options.maxHoldBars ?? 0)) &&
    Math.max(0, (state as { stopRatio?: number }).stopRatio ?? 0) ===
      Math.max(0, options.stopRatio ?? 0) &&
    (state.conservativeIntrabar ?? true) ===
      (options.conservativeIntrabar ?? true)
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
interface LadderRungSizeResult {
  readonly size: Money;
  readonly skipReason?: string;
}

function ladderRungSize(
  capital: Decimal,
  options: LadderPaperTradingOptions,
): LadderRungSizeResult {
  const rungs = Math.max(1, Math.floor(options.rungs ?? 1));
  const positionFraction = Math.max(
    0,
    Math.min(1, (options.maxPositionPct ?? 100) / 100),
  );
  const perRungAllocation = capital.times(positionFraction).div(rungs);
  // The fill price is unknown here (caller supplies it via the event); size
  // is re-derived per event using the event's fill price, so this returns the
  // allocation only. The caller divides by the fill price.
  return { size: perRungAllocation } satisfies LadderRungSizeResult;
}

/**
 * Account-scaled leverage cap: leverage should rise with account size so small
 * accounts stay conservative (a $100 account at 100x on BTC is reckless) while
 * larger accounts can run high leverage on a small slice of capital (a $25k
 * account can use 150x on a 5% position). The effective cap is
 * min(configuredMax, sizeTier) then discounted by the per-position budget
 * fraction — committing 50% of a small account at high leverage is absurd.
 * The per-contract ceiling is enforced by the adapter at order time.
 */
function accountScaledLeverageCap(
  capital: Decimal,
  perPositionBudget: number,
  configuredMax: number,
): number {
  const cap = Math.max(1, Math.floor(configuredMax) || 1);
  const budgetFrac = Math.max(0, Math.min(1, perPositionBudget));
  if (capital.lessThanOrEqualTo(0)) return 1;
  const dollar = toNumber(capital);
  // Risk tiers by account size (USDT). Small accounts stay VERY conservative
  // (a $100 account at 100x on BTC is reckless), scaling up only as capital
  // grows so a big account can use high leverage on a small slice:
  //   < 500    : 10x   (small account — low, sane leverage)
  //   < 5000   : 25x
  //   < 25000  : 50x
  //   >= 25000 : 150x  (big account can afford a kick on a small slice)
  const sizeCap =
    dollar < 500 ? 10 : dollar < 5000 ? 25 : dollar < 25000 ? 50 : 150;
  // Discount when a large fraction of capital is committed per position.
  const budgetFactor = budgetFrac <= 0.1 ? 1 : budgetFrac <= 0.25 ? 0.75 : 0.5;
  const effective = Math.floor(sizeCap * budgetFactor);
  return Math.max(1, Math.min(cap, effective));
}

/**
 * Deployed leverage: the multiplier actually applied to the per-rung margin
 * budget, so notional = marginBudget x leverage (growing PnL with exposure).
 * In FULLY dynamic mode it is the account-scaled cap itself (the engine
 * decides leverage from account size + budget, ignoring any fixed --leverage).
 * Otherwise it is the minimum of the requested leverage and the account-scaled
 * cap. It never drops below 1x, and the per-contract ceiling is enforced by
 * the adapter at order time.
 */
function dynamicLeverage(
  rawNotional: Decimal,
  perRungAllocation: Decimal,
  requestedLeverage: number,
  maxLeverage: number,
  fullyDynamic: boolean,
): number {
  const cap = Math.max(1, maxLeverage);
  if (fullyDynamic) {
    // Leverage sized purely from the account: deploy the account-scaled cap
    // (from capital + per-position budget), ignoring any fixed --leverage.
    // The min-orderable-floor raise is handled by the adapter at order time.
    return Math.max(1, cap);
  }
  const requested = Math.max(1, requestedLeverage);
  if (
    perRungAllocation.lessThanOrEqualTo(0) ||
    rawNotional.lessThanOrEqualTo(0)
  ) {
    return requested;
  }
  // Leverage needed so the full raw notional fits the per-rung margin budget.
  const needed = rawNotional.div(perRungAllocation);
  // Deploy at least enough to fit the floor (min-orderable), and up to the
  // requested leverage, bounded by the account-scaled cap.
  const leverage = Math.min(
    cap,
    Math.max(requested, Math.ceil(needed.toNumber())),
  );
  return leverage >= 1 ? leverage : 1;
}

/**
 * Orderable qty for one rung at `fillPrice`, contract-step rounded. Uses
 * DYNAMIC leverage so the full notional (from the per-rung budget) fits the
 * margin: returns the qty and the leverage that keeps margin <= the per-rung
 * allocation, bounded by the risk cap. When no contract spec is available the
 * raw notional is used at the requested leverage.
 */
interface LadderRungQtyResult {
  readonly qty: Money;
  readonly leverage: number;
  readonly skipReason?: string;
}

function ladderRungQty(
  capital: Decimal,
  options: LadderPaperTradingOptions,
  fillPrice: Decimal,
): LadderRungQtyResult {
  const perRungAllocation = ladderRungSize(capital, options).size;
  if (perRungAllocation.lessThanOrEqualTo(0)) {
    return {
      qty: money(0),
      leverage: Math.max(1, options.leverage ?? 1),
      skipReason: "per-rung allocation is zero",
    } satisfies LadderRungQtyResult;
  }
  if (fillPrice.lessThanOrEqualTo(0)) {
    return {
      qty: money(0),
      leverage: Math.max(1, options.leverage ?? 1),
      skipReason: "non-positive fill price",
    } satisfies LadderRungQtyResult;
  }
  const configuredMax = Math.max(1, Math.floor(options.maxLeverage ?? 10) || 1);
  // Account-scaled cap: rises with account size, discounted by the fraction of
  // capital committed per position. Small accounts stay low; a $25k+ account
  // can use high leverage on a small slice.
  const maxLeverage = accountScaledLeverageCap(
    capital,
    Math.max(0, (options.maxPositionPct ?? 100) / 100),
    configuredMax,
  );
  const spec = options.contractSpecs;
  const fullyDynamic = options.fullyDynamicLeverage === true;
  const marginSizedRaw = perRungAllocation.div(fillPrice);
  const notionalCapPct = options.maxNotionalPct;
  const notionalSizedRaw =
    notionalCapPct === undefined
      ? marginSizedRaw
      : capital
          .times(Math.max(0, notionalCapPct) / 100)
          .div(Math.max(1, Math.floor(options.rungs ?? 1)))
          .div(fillPrice);
  const raw = Decimal.min(marginSizedRaw, notionalSizedRaw);
  if (spec === undefined) {
    const lev = dynamicLeverage(
      raw.times(fillPrice),
      perRungAllocation,
      options.leverage ?? 1,
      maxLeverage,
      fullyDynamic,
    );
    return {
      qty: Decimal.max(0, raw),
      leverage: lev,
    } satisfies LadderRungQtyResult;
  }
  const qty = orderableQty(raw, spec, fillPrice, perRungAllocation);
  const notional = qty.times(fillPrice);
  const leverage = dynamicLeverage(
    notional,
    perRungAllocation,
    options.leverage ?? 1,
    maxLeverage,
    fullyDynamic,
  );
  const margin = notional.div(leverage);
  if (margin.greaterThan(perRungAllocation)) {
    return {
      qty: money(0),
      leverage,
      skipReason: `min orderable notional ${notional.toFixed(2)} USDT requires margin ${margin.toFixed(2)} at ${leverage}x, exceeding the ${toNumber(perRungAllocation).toFixed(2)} USDT per-rung cap`,
    } satisfies LadderRungQtyResult;
  }
  return { qty, leverage } satisfies LadderRungQtyResult;
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
      // Size first: resolves the DYNAMIC leverage (fits notional to the
      // per-rung margin budget, capped at the risk limit) used for the risk
      // check and the order.
      const sized = ladderRungQty(w.capital, options, fillPrice);
      if (sized.qty.lessThanOrEqualTo(0)) {
        return yield* Effect.fail(
          new ExchangeError(
            `ladder rung size unavailable: ${sized.skipReason ?? "zero qty"}`,
          ),
        );
      }
      const leverage = Math.max(1, sized.leverage);
      const tradesTodayCount = yield* repo.countTradesForDate(new Date());
      const todayPnl = yield* repo
        .getTodayRealizedPnl()
        .pipe(Effect.orElseSucceed(() => money(0)));
      yield* riskGuard.check({
        isLive: true,
        capital: toNumber(w.capital),
        peakCapital: toNumber(w.peak),
        startOfDayCapital: toNumber(
          yield* repo.getStartOfDayCapital(new Date(), w.capital),
        ),
        dailyRealizedPnl: toNumber(todayPnl),
        tradesTodayCount,
        // A hair under the cap: capital × maxPositionPct/100 at the exact
        // cap computes to 50.00000000000001% (> 50%) and the guard rejects
        // every fill on float precision. Real exposure is a hair under the
        // cap anyway after fee/rounding.
        positionValue: toNumber(
          w.capital.times((options.maxPositionPct ?? 100) / 100).times(0.9999),
        ),
        notionalValue: toNumber(sized.qty.times(fillPrice)),
        symbol: options.symbol,
        side,
        leverage,
        productType,
      });
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

      // Attach exchange-side TP/SL after each live fill so the position is
      // protected even if this process dies or the polling loop stalls. The
      // exchange holds the resting orders; the engine's polling closes remain
      // as a second layer (and the single source of the paper ledger PnL).
      // TP = one targetRatio step, SL = the step-relative stop when stopRatio
      // is set, else fall back to the per-position drawdown kill, else skip
      // (legacy boundary geometry only exists in the engine's close path).
      if (adapter.setTradingStop !== undefined) {
        const targetRatio = Math.max(0.001, options.targetRatio ?? 1);
        const stepAbs = toNumber(fillPrice) * (options.gridStepPct / 100);
        const tp =
          fill.side === "long"
            ? toNumber(fillPrice) + stepAbs * targetRatio
            : toNumber(fillPrice) - stepAbs * targetRatio;
        const stopRatio = options.stopRatio ?? 0;
        const sl =
          stopRatio > 0
            ? fill.side === "long"
              ? toNumber(fillPrice) - stepAbs * stopRatio
              : toNumber(fillPrice) + stepAbs * stopRatio
            : undefined;
        const tradingStop = {
          symbol: options.symbol,
          productType,
          marginMode,
          side: fill.side,
          takeProfit: money(tp),
          stopLoss: sl === undefined ? undefined : money(sl),
        };
        yield* adapter.setTradingStop(tradingStop).pipe(Effect.result);
      }
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
  | MarketDataError
  | PaperTradingRepositoryError
  | KillSwitchError
  | CircuitBreakerError,
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
      } else if (options.configMismatchAction === "force-reseed") {
        // Open ladder under different params with force-reseed: close any
        // open rungs at the current close (a market-ish exit on stale
        // geometry) and re-seed fresh with the new config. Without this an
        // open ladder under a changed whitelist is deadlocked forever — the
        // engine refuses to advance, so the stale rung never closes and the
        // real exchange position it mirrors stays unmanaged (regression
        // 2026-08-19: FARTCOIN frozen at grids=2 with a filled rung while
        // the whitelist moved to grids=3).
        const gateway = yield* MarketDataGateway;
        const candles = yield* gateway
          .fetchOHLCV(options.exchange, options.symbol, options.timeframe, 2)
          .pipe(Effect.result);
        const closePrice =
          candles._tag === "Success" && candles.success.length > 0
            ? candles.success[candles.success.length - 1].close
            : null;
        const closeTimestamp =
          candles._tag === "Success" && candles.success.length > 0
            ? candles.success[candles.success.length - 1].timestamp
            : new Date();
        if (closePrice !== null && closePrice > 0) {
          const w = stateToWorking(state);
          const events: MutableBarEvents = { fills: [], closes: [] };
          for (const side of ["long", "short"] as const) {
            const rungs = side === "long" ? w.longRungs : w.shortRungs;
            for (const r of rungs) {
              if (r.filled) {
                closeRung(
                  w,
                  r,
                  closePrice,
                  "max_hold",
                  options,
                  events,
                  closeTimestamp,
                );
              }
            }
            if (side === "long") w.longRungs = [];
            else w.shortRungs = [];
          }
          w.longBase = 0;
          w.shortBase = 0;
          const closed = events.closes.length;
          // Re-seed with the NEW config but carry the realized capital forward
          // (workingToState would keep the stale config fields via `previous`).
          state = {
            ...freshLadderState(options),
            capital: w.capital,
            peakCapital: Decimal.max(w.peak, money(options.initialCapital)),
            totalWins: w.totalWins,
            totalLosses: w.totalLosses,
            lastTimestamp: state.lastTimestamp,
          };
          if (
            events.closes.length > 0 &&
            repo.recordLadderTrades !== undefined
          ) {
            yield* repo.recordLadderTrades(
              events.closes.map((close) =>
                ladderTradeFromClose(options, close),
              ),
            );
          }
          yield* repo.saveLadderState(state);
          if (closed > 0) {
            return {
              action: "closed" as const,
              capital: toNumber(state.capital),
              peakCapital: toNumber(state.peakCapital),
              openRungs: 0,
              closedThisIteration: closed,
              note: `config mismatch on open ladder — force-closed ${closed} stale rung(s) at ${closePrice}, re-seed next bar`,
            };
          }
        }
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

    // Ladder now shares the same account-level circuit/kill controls as the
    // single-position and grid engines. These services are optional for pure
    // unit-level paper stepping, but when present they are checked before any
    // candle is allowed to mutate the ladder.
    const killSwitch = yield* Effect.serviceOption(KillSwitch);
    const circuitBreaker = yield* Effect.serviceOption(CircuitBreaker);
    if (Option.isSome(killSwitch)) {
      if (yield* killSwitch.value.isEngaged()) {
        return {
          action: "hold",
          capital: toNumber(state.capital),
          peakCapital: toNumber(state.peakCapital),
          openRungs: openRungCount(w),
          closedThisIteration: 0,
          note: "ladder held: kill switch engaged",
        };
      }
    }
    if (Option.isSome(circuitBreaker)) {
      if (yield* circuitBreaker.value.isOpen()) {
        return {
          action: "hold",
          capital: toNumber(state.capital),
          peakCapital: toNumber(state.peakCapital),
          openRungs: openRungCount(w),
          closedThisIteration: 0,
          note: "ladder held: circuit breaker open",
        };
      }
    }

    // Agreement-block cooldown: when a fill failed with a "must agree to the
    // Trading Terms" error (110123/110125/110126), the contract is untradeable
    // until the user accepts the terms in the exchange UI. Skip fills for the
    // cooldown instead of re-attempting on every new bar (the exchange has no
    // API to accept the agreement, so retrying is pure noise).
    if (options.isLive === true) {
      const blockedUntil = agreementBlockedUntil.get(options.symbol) ?? 0;
      if (blockedUntil > Date.now()) {
        return {
          action: "hold",
          capital: toNumber(state.capital),
          peakCapital: toNumber(state.peakCapital),
          openRungs: openRungCount(stateToWorking(state)),
          closedThisIteration: 0,
          note: "symbol blocked: trading terms not accepted (110126) — sign the agreement in the exchange UI",
        };
      }
    }

    // Reconcile orphans: a REAL position the paper ledger does not track (left
    // by a rolled-back bar whose earlier order filled before a later event
    // failed) is closed on the exchange so it cannot sit unmanaged and consume
    // the account's margin. Only closes when the paper ledger is flat for this
    // symbol; a paper rung with a matching real position is left to manage.
    if (options.isLive === true) {
      const adapter = yield* Effect.serviceOption(FuturesExchangeAdapter);
      if (Option.isSome(adapter)) {
        const productType = options.productType ?? "USDT-FUTURES";
        const marginMode = options.marginMode ?? "isolated";
        const realPos = yield* adapter.value
          .getPosition(options.symbol, productType)
          .pipe(Effect.result);
        if (
          realPos._tag === "Success" &&
          realPos.success !== null &&
          Number(realPos.success.quantity) > 0 &&
          openRungCount(w) === 0
        ) {
          const side: FuturesOrderSide =
            realPos.success.side === "long" ? "sell" : "buy";
          yield* adapter.value
            .closePosition({
              symbol: options.symbol,
              side,
              productType,
              marginMode,
              leverage: Math.max(1, options.leverage ?? 1),
              size: realPos.success.quantity,
            })
            .pipe(Effect.result);
        }
      }
    }

    const trendSeries =
      (options.trendFilterPeriod ?? 0) > 0
        ? calculateSMA(
            candles.map((ck) => ck.close),
            options.trendFilterPeriod ?? 0,
          )
        : null;
    const openBefore = openRungCount(w);
    const snapshot = cloneWorking(w);
    const startOfDayCapital = yield* repo.getStartOfDayCapital(
      new Date(),
      w.capital,
    );
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
        // Roll the ledger back but ADVANCE lastTimestamp past the processed
        // bars: otherwise the same failing bar is reprocessed and re-fails
        // every cycle (the 'ab not enough' / risk-reject / agreement churn).
        // The failed bar's fills are not committed; any real orders it placed
        // are reconciled (closed) at the next iteration's start.
        const failureReason = outcome.failure.reason ?? String(outcome.failure);
        if (/11012[356]/.test(failureReason)) {
          // "must agree to the Trading Terms" — no API accepts it; block the
          // symbol for the cooldown so it is not re-attempted every bar.
          agreementBlockedUntil.set(
            options.symbol,
            Date.now() + AGREEMENT_BLOCK_MS,
          );
        }
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
        const rolledState = workingToState(options, w, last.timestamp, state);
        yield* repo.saveLadderState(rolledState);
        return {
          action: "hold",
          capital: toNumber(w.capital),
          peakCapital: toNumber(w.peak),
          openRungs: openRungCount(w),
          closedThisIteration: 0,
          note: `live ladder execution failed (bar rolled back): ${outcome.failure.reason ?? String(outcome.failure)}${"violations" in outcome.failure && Array.isArray(outcome.failure.violations) ? `: ${(outcome.failure as { violations: readonly string[] }).violations.join("; ")}` : ""}`,
        };
      }
    }

    if (iterationCloses.length > 0 && repo.recordLadderTrades !== undefined) {
      yield* repo.recordLadderTrades(
        iterationCloses.map((close) => ladderTradeFromClose(options, close)),
      );
    }

    if (Option.isSome(circuitBreaker) && iterationCloses.length > 0) {
      yield* circuitBreaker.value.recordTradeResult(
        toNumber(w.capital.minus(snapshot.capital)),
        toNumber(startOfDayCapital),
      );
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
    const chopGateThreshold = options.chopGateAdxThreshold ?? 0;
    const lastAdx =
      chopGateThreshold > 0
        ? makeCausalSymbolStats(candles, "15m")(candles.length - 1).adx14
        : 0;
    const seedsBlockedByChopGate =
      openBefore === 0 &&
      openAfter === 0 &&
      iterationFills.length === 0 &&
      closedThisIteration === 0 &&
      chopGateThreshold > 0 &&
      lastAdx >= chopGateThreshold;
    return {
      action,
      capital: toNumber(state.capital),
      peakCapital: toNumber(state.peakCapital),
      openRungs: openAfter,
      closedThisIteration,
      note:
        `ladder iter over ${candles.length - startIndex} bars${options.isLive ? " [LIVE]" : ""}` +
        (seedsBlockedByChopGate
          ? `; seeds blocked by ADX gate (${lastAdx.toFixed(2)} >= ${chopGateThreshold})`
          : ""),
    };
  });
}

function isFlat(state: LadderPaperState): boolean {
  return (
    state.longRungs.filter((r) => r.filled).length === 0 &&
    state.shortRungs.filter((r) => r.filled).length === 0
  );
}
