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
  type CircuitBreakerService,
} from "../risk/circuit-breaker.js";
import {
  KillSwitch,
  KillSwitchError,
  type KillSwitchService,
} from "../risk/kill-switch.js";
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
   * Use warm-up candles only for indicators and process the newest candle.
   * Shadow/live starts must be forward-only; historical stepping is opt-in via
   * replayBars or the deterministic test harness.
   */
  readonly forwardOnly?: boolean;
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
  /**
   * Account-level realized drawdown kill: when the ledger's capital falls
   * this percent below its peak, new seeds are blocked and open rungs are
   * force-closed at the current close (0/undefined = disabled). Mirrors the
   * grid engine's realized-DD kill; --max-drawdown-pct feeds it.
   */
  readonly maxDrawdownPct?: number;
  /**
   * Funding cost accrued on open rung notional every 8h held, in percent of
   * notional per interval (signed: positive = longs pay). Default 0 = off.
   */
  readonly fundingRatePct8h?: number;
  /**
   * Maintenance-margin rate used by the liquidation price model (fraction of
   * notional, e.g. 0.005 = 0.5%). Default 0 keeps the legacy 1/L model.
   */
  readonly maintenanceMarginRate?: number;
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
  /**
   * Orderable quantity in base units resolved at fill time (contract-step
   * rounded when specs are available). The live executor places THIS size
   * and the rung persists it so the close sends the entry size.
   */
  readonly qty: number;
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
  /** The rung's persisted filledQty — the exact size the close must send. */
  readonly qty?: number;
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
  /** Mark-to-market account equity when a candle was processed. */
  readonly equity?: number;
  /** Estimated unrealized PnL at the processed candle close. */
  readonly unrealizedPnl?: number;
  /** Price used for the mark-to-market estimate. */
  readonly markPrice?: number;
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
  const funding = fundingCostForRung(
    r,
    capitalBefore,
    closedAt.getTime(),
    opts,
  );
  w.capital = Decimal.max(0, capitalBefore.mul(1 + equityReturn).plus(funding));
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
    // The rung's persisted entry size — the live close must send exactly it.
    qty: r.filledQty,
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
    strategyConfigFingerprint: ladderProvenance.fingerprint,
    cohortId: ladderProvenance.cohortId,
    candidateLockAt: ladderProvenance.lockedAt,
    datasetCutoffAt: ladderProvenance.lockedAt,
    entryOpenedAt: openedAt,
    executionEnvironment: ladderProvenance.executionEnvironment,
  };
}

/**
 * Readiness provenance for THIS process's ladder trades, set once by the CLI
 * when the soak starts (mirrors the grid engine's per-state fingerprint).
 * Trades recorded while this is unset are legacy rows without provenance.
 */
interface LadderTradeProvenance {
  fingerprint?: string;
  cohortId?: string;
  lockedAt?: Date;
  executionEnvironment?:
    | "bitget-demo"
    | "bitget-live"
    | "bybit-demo"
    | "bybit-live";
}

const ladderProvenance: LadderTradeProvenance = {};

/** Install the readiness provenance stamped onto every ladder trade. */
export function setLadderTradeProvenance(provenance: {
  fingerprint: string;
  cohortId: string;
  lockedAt: Date;
  executionEnvironment:
    | "bitget-demo"
    | "bitget-live"
    | "bybit-demo"
    | "bybit-live";
}): void {
  ladderProvenance.fingerprint = provenance.fingerprint;
  ladderProvenance.cohortId = provenance.cohortId;
  ladderProvenance.lockedAt = provenance.lockedAt;
  ladderProvenance.executionEnvironment = provenance.executionEnvironment;
}

function openRungCount(w: WorkingState): number {
  return (
    w.longRungs.filter((r) => r.filled).length +
    w.shortRungs.filter((r) => r.filled).length
  );
}

/** Estimate account-level unrealized PnL without mutating realized capital. */
function estimateUnrealizedPnl(
  w: WorkingState,
  markPrice: number,
  opts: LadderPaperTradingOptions,
): Decimal {
  if (!Number.isFinite(markPrice) || markPrice <= 0) return money(0);
  const leverage = Math.max(1, opts.leverage ?? 1);
  const positionFraction = Math.max(
    0,
    Math.min(1, (opts.maxPositionPct ?? 100) / 100),
  );
  const rungCount = Math.max(1, Math.floor(opts.rungs ?? 1));
  const sizePerRung = positionFraction / rungCount;
  const estimatedExitFee = (opts.takerExitFeePct ?? opts.feePct ?? 0) / 100;
  const openRungs = [...w.longRungs, ...w.shortRungs].filter(
    (r) => r.filled && r.entryPrice > 0,
  );
  let pnl = money(0);
  for (const rung of openRungs) {
    const priceReturn =
      rung.side === "long"
        ? (markPrice - rung.entryPrice) / rung.entryPrice
        : (rung.entryPrice - markPrice) / rung.entryPrice;
    const netReturn = priceReturn - estimatedExitFee;
    pnl = pnl.plus(w.capital.mul(sizePerRung).mul(netReturn).mul(leverage));
  }
  return pnl;
}

function liquidationPrice(
  side: "long" | "short",
  entryPrice: number,
  leverage: number,
  mmRate = 0,
): number {
  const l = Math.max(1, leverage);
  if (l <= 1) return 0;
  // Adverse move to liquidation = initial leverage distance minus the
  // maintenance-margin buffer the exchange holds (floored at 1% so a huge
  // mmRate can never produce a non-liquidating or inverted price).
  const move = Math.max(0.01, 1 / l - mmRate);
  return side === "long" ? entryPrice * (1 - move) : entryPrice * (1 + move);
}

/**
 * Funding accrued while a rung was open: notional x rate x whole-8h-intervals,
 * charged at close (longs pay a positive rate; shorts receive it). Charging at
 * close keeps the engine EXACTLY parity with the backtest's accounting.
 */
function fundingCostForRung(
  r: LadderPaperRungState,
  capitalBefore: Decimal,
  closedAtMs: number,
  opts: LadderPaperTradingOptions,
): Decimal {
  const ratePct = opts.fundingRatePct8h ?? 0;
  if (ratePct === 0 || r.entryTimestamp <= 0) return money(0);
  const intervals = Math.floor(
    (closedAtMs - r.entryTimestamp) / FUNDING_INTERVAL_MS,
  );
  if (intervals <= 0) return money(0);
  const positionFraction = Math.max(
    0,
    Math.min(1, (opts.maxPositionPct ?? 100) / 100),
  );
  const notional = capitalBefore
    .times(positionFraction)
    .times(Math.max(1, opts.leverage ?? 1))
    .div(Math.max(1, Math.floor(opts.rungs ?? 1)));
  const sign = r.side === "long" ? -1 : 1;
  return notional.times((ratePct / 100) * intervals).times(sign);
}

const FUNDING_INTERVAL_MS = 8 * 3_600_000;

/**
 * Account-level realized drawdown kill: true when capital has fallen
 * `maxDrawdownPct` percent below peak (0/undefined disables).
 */
function accountDrawdownBreached(
  w: WorkingState,
  opts: LadderPaperTradingOptions,
): boolean {
  const cap = opts.maxDrawdownPct ?? 0;
  if (cap <= 0 || cap >= 100) return false;
  if (w.peak.lessThanOrEqualTo(0)) return false;
  const ddPct = w.peak.minus(w.capital).div(w.peak).times(100);
  return ddPct.greaterThanOrEqualTo(cap);
}

interface LadderBarContext {
  readonly w: WorkingState;
  readonly candle: CandleLike;
  readonly barIndex: number;
  readonly opts: LadderPaperTradingOptions;
  readonly events: MutableBarEvents;
  readonly step: number;
  readonly slippage: number;
  readonly rungCount: number;
  readonly targetRatio: number;
  readonly leverage: number;
  readonly maxHoldBars: number;
  readonly millisecondsPerBar: number;
  readonly trend: number;
  readonly chopGateActive: boolean;
  readonly onlyWithTrend: boolean;
}

type LadderSide = "long" | "short";

interface LadderRiskExit {
  readonly closeRungs: readonly LadderPaperRungState[];
  readonly exitPrice: number;
  readonly reason: "stop" | "liquidation";
  readonly resetAll: boolean;
  readonly pauseAfterLoss: boolean;
}

function sideRungs(w: WorkingState, side: LadderSide): LadderPaperRungState[] {
  return side === "long" ? w.longRungs : w.shortRungs;
}

function setSideRungs(
  w: WorkingState,
  side: LadderSide,
  rungs: LadderPaperRungState[],
): void {
  if (side === "long") w.longRungs = rungs;
  else w.shortRungs = rungs;
}

function sideBase(w: WorkingState, side: LadderSide): number {
  return side === "long" ? w.longBase : w.shortBase;
}

function setSideBase(w: WorkingState, side: LadderSide, base: number): void {
  if (side === "long") w.longBase = base;
  else w.shortBase = base;
}

function buildSideRungs(
  side: LadderSide,
  base: number,
  step: number,
  rungCount: number,
): LadderPaperRungState[] {
  const rungs: LadderPaperRungState[] = [];
  for (let rungIndex = 1; rungIndex <= rungCount; rungIndex++) {
    rungs.push({
      rungIndex,
      side,
      level:
        side === "long" ? base - rungIndex * step : base + rungIndex * step,
      step,
      filled: false,
      entryPrice: 0,
      entryBar: 0,
      entryTimestamp: 0,
    });
  }
  return rungs;
}

function seedLadderSide(ctx: LadderBarContext, side: LadderSide): void {
  const { w, candle, opts } = ctx;
  const rungs = sideRungs(w, side);
  if (rungs.some((rung) => rung.filled)) return;
  const trendAllows =
    !ctx.onlyWithTrend ||
    (ctx.trend !== null &&
      !isNaN(ctx.trend) &&
      (side === "long" ? candle.close > ctx.trend : candle.close < ctx.trend));
  const allowed =
    !accountDrawdownBreached(w, opts) && !ctx.chopGateActive && trendAllows;
  if (!allowed) {
    setSideRungs(w, side, []);
    setSideBase(w, side, 0);
    return;
  }
  setSideBase(w, side, candle.open);
  setSideRungs(
    w,
    side,
    buildSideRungs(side, candle.open, ctx.step, ctx.rungCount),
  );
}

function sideLevelTouched(
  side: LadderSide,
  candle: CandleLike,
  level: number,
): boolean {
  return side === "long" ? candle.low <= level : candle.high >= level;
}

function fillLadderSide(ctx: LadderBarContext, side: LadderSide): void {
  const { w, candle, opts, events } = ctx;
  const rungs = sideRungs(w, side);
  for (let index = 0; index < rungs.length; index++) {
    const rung = rungs[index];
    if (rung.filled) continue;
    const previousFilled = index === 0 || rungs[index - 1].filled;
    if (!previousFilled || !sideLevelTouched(side, candle, rung.level))
      continue;
    const fillPrice =
      side === "long" ? rung.level * ctx.slippage : rung.level / ctx.slippage;
    const sized = ladderRungQty(w.capital, opts, money(fillPrice));
    rungs[index] = {
      ...rung,
      filled: true,
      entryPrice: fillPrice,
      entryBar: ctx.barIndex,
      entryTimestamp: candle.timestamp.getTime(),
      filledQty: sized.qty.greaterThan(0) ? toNumber(sized.qty) : undefined,
    };
    events.fills.push({
      rungIndex: rung.rungIndex,
      side,
      fillPrice,
      level: rung.level,
      qty: toNumber(sized.qty),
    });
  }
}

function sideStopBoundary(
  ctx: LadderBarContext,
  side: LadderSide,
  filled: readonly LadderPaperRungState[],
): number {
  const { opts, w } = ctx;
  const stopRatio = opts.stopRatio ?? 0;
  if (stopRatio > 0) {
    const entries = filled.map((rung) => rung.entryPrice);
    return side === "long"
      ? Math.min(...entries) - ctx.step * stopRatio
      : Math.max(...entries) + ctx.step * stopRatio;
  }
  return side === "long"
    ? sideBase(w, side) - ctx.step * (ctx.rungCount + opts.gridMaxGrids)
    : sideBase(w, side) + ctx.step * (ctx.rungCount + opts.gridMaxGrids);
}

function sideLiquidationLevel(
  ctx: LadderBarContext,
  side: LadderSide,
  filled: readonly LadderPaperRungState[],
): number {
  const levels = filled
    .map((rung) =>
      liquidationPrice(
        side,
        rung.entryPrice,
        ctx.leverage,
        ctx.opts.maintenanceMarginRate ?? 0,
      ),
    )
    .filter((price) => price > 0);
  if (levels.length === 0) return 0;
  return side === "long" ? Math.max(...levels) : Math.min(...levels);
}

function sideExitPrice(
  side: LadderSide,
  price: number,
  slippageBps: number,
): number {
  return side === "long"
    ? price * (1 - slippageBps / 10000)
    : price * (1 + slippageBps / 10000);
}

function positionLossesBeyondLimit(
  side: LadderSide,
  filled: readonly LadderPaperRungState[],
  close: number,
  limit: number,
): LadderPaperRungState[] {
  return filled.filter((rung) => {
    if (rung.entryPrice <= 0) return false;
    const lossPct =
      side === "long"
        ? ((rung.entryPrice - close) / rung.entryPrice) * 100
        : ((close - rung.entryPrice) / rung.entryPrice) * 100;
    return lossPct > limit;
  });
}

function resolveLadderRiskExit(
  ctx: LadderBarContext,
  side: LadderSide,
  filled: readonly LadderPaperRungState[],
): LadderRiskExit | undefined {
  const { candle, opts } = ctx;
  const liquidation = sideLiquidationLevel(ctx, side, filled);
  const liquidationTouched =
    liquidation > 0 && sideLevelTouched(side, candle, liquidation);
  if (liquidationTouched) {
    return {
      closeRungs: filled,
      exitPrice: sideExitPrice(side, liquidation, opts.slippageBps),
      reason: "liquidation",
      resetAll: true,
      pauseAfterLoss: true,
    };
  }

  const boundary = sideStopBoundary(ctx, side, filled);
  if (sideLevelTouched(side, candle, boundary)) {
    return {
      closeRungs: filled,
      exitPrice: sideExitPrice(side, boundary, opts.slippageBps),
      reason: "stop",
      resetAll: true,
      pauseAfterLoss: true,
    };
  }

  const maxPositionDrawdownPct = opts.maxPositionDrawdownPct ?? 0;
  if (maxPositionDrawdownPct > 0) {
    const killed = positionLossesBeyondLimit(
      side,
      filled,
      candle.close,
      maxPositionDrawdownPct,
    );
    if (killed.length > 0) {
      return {
        closeRungs: killed,
        exitPrice: sideExitPrice(side, candle.close, opts.slippageBps),
        reason: "stop",
        resetAll: false,
        pauseAfterLoss: true,
      };
    }
  }

  if (accountDrawdownBreached(ctx.w, opts)) {
    return {
      closeRungs: filled,
      exitPrice: sideExitPrice(side, candle.close, opts.slippageBps),
      reason: "stop",
      resetAll: true,
      pauseAfterLoss: false,
    };
  }
  return undefined;
}

function applyLadderRiskExit(
  ctx: LadderBarContext,
  side: LadderSide,
  filled: readonly LadderPaperRungState[],
  exit: LadderRiskExit,
): void {
  const { w, opts, events } = ctx;
  for (const rung of exit.closeRungs) {
    closeRung(
      w,
      rung,
      exit.exitPrice,
      exit.reason,
      opts,
      events,
      ctx.candle.timestamp,
    );
  }
  if (exit.resetAll) {
    setSideRungs(w, side, []);
    setSideBase(w, side, 0);
    if (exit.pauseAfterLoss && opts.gridPauseAfterLossBars > 0) {
      w.paused = opts.gridPauseAfterLossBars;
    }
    return;
  }
  const survivors = filled.filter((rung) => !exit.closeRungs.includes(rung));
  if (survivors.length === 0) {
    setSideRungs(w, side, []);
    setSideBase(w, side, 0);
    if (exit.pauseAfterLoss && opts.gridPauseAfterLossBars > 0) {
      w.paused = opts.gridPauseAfterLossBars;
    }
  } else {
    setSideRungs(w, side, survivors);
  }
}

function closeLadderTargets(ctx: LadderBarContext, side: LadderSide): void {
  const { w, candle, opts, events } = ctx;
  const rungs = sideRungs(w, side);
  const stillOpen: LadderPaperRungState[] = [];
  let anyFillClosed = false;
  for (const rung of rungs) {
    if (!rung.filled) {
      stillOpen.push(rung);
      continue;
    }
    const target =
      side === "long"
        ? rung.entryPrice + rung.step * ctx.targetRatio
        : rung.entryPrice - rung.step * ctx.targetRatio;
    const targetTouched =
      side === "long" ? candle.high >= target : candle.low <= target;
    const canCloseOnSameBar =
      (opts.conservativeIntrabar ?? true) === false ||
      rung.entryBar < ctx.barIndex;
    if (targetTouched && canCloseOnSameBar) {
      closeRung(
        w,
        rung,
        side === "long" ? target / ctx.slippage : target * ctx.slippage,
        "target",
        opts,
        events,
        candle.timestamp,
      );
      anyFillClosed = true;
      continue;
    }
    if (
      ctx.maxHoldBars > 0 &&
      rung.entryTimestamp > 0 &&
      candle.timestamp.getTime() - rung.entryTimestamp >=
        ctx.maxHoldBars * ctx.millisecondsPerBar
    ) {
      closeRung(
        w,
        rung,
        sideExitPrice(side, candle.close, opts.slippageBps),
        "max_hold",
        opts,
        events,
        candle.timestamp,
      );
      anyFillClosed = true;
      continue;
    }
    stillOpen.push(rung);
  }
  setSideRungs(w, side, stillOpen);
  if (stillOpen.filter((rung) => rung.filled).length === 0 && anyFillClosed) {
    setSideRungs(w, side, []);
    setSideBase(w, side, 0);
  }
}

function manageLadderSide(ctx: LadderBarContext, side: LadderSide): void {
  if (sideRungs(ctx.w, side).length === 0) return;
  fillLadderSide(ctx, side);
  const filled = sideRungs(ctx.w, side).filter((rung) => rung.filled);
  if (filled.length === 0) return;
  const riskExit = resolveLadderRiskExit(ctx, side, filled);
  if (riskExit) {
    applyLadderRiskExit(ctx, side, filled, riskExit);
    return;
  }
  closeLadderTargets(ctx, side);
}

function createLadderBarContext(
  w: WorkingState,
  candles: readonly CandleLike[],
  i: number,
  opts: LadderPaperTradingOptions,
  events: MutableBarEvents,
  trendSeries: readonly number[] | null,
): LadderBarContext | undefined {
  const candle = candles[i];
  const trendFilterPeriod = Math.max(0, opts.trendFilterPeriod ?? 0);
  const trend =
    trendSeries !== null && trendSeries.length > i ? trendSeries[i] : NaN;
  if (trendFilterPeriod > 0 && (trend === null || isNaN(trend))) {
    return undefined;
  }
  const chopGateThreshold = opts.chopGateAdxThreshold ?? 0;
  return {
    w,
    candle,
    barIndex: i,
    opts,
    events,
    step: candle.open * (opts.gridStepPct / 100),
    slippage: 1 + opts.slippageBps / 10000,
    rungCount: Math.max(1, Math.floor(opts.rungs ?? 1)),
    targetRatio: Math.max(0.001, opts.targetRatio ?? 1),
    leverage: Math.max(1, opts.leverage ?? 1),
    maxHoldBars: Math.max(0, Math.floor(opts.maxHoldBars ?? 0)),
    millisecondsPerBar: timeframeMs(opts.timeframe),
    trend,
    chopGateActive:
      chopGateThreshold > 0 &&
      makeCausalSymbolStats(candles, opts.timeframe)(i).adx14 >=
        chopGateThreshold,
    onlyWithTrend: opts.onlyWithTrend ?? false,
  };
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
  const context = createLadderBarContext(
    w,
    candles,
    i,
    opts,
    events,
    trendSeries,
  );
  if (!context) return { fills, closes };
  // Drawdown-peak re-anchor: a flat account blocked by realized drawdown
  // can never trade its way back (no open rungs to recover with), so the
  // kill latched forever with no operator reset path (ENA shadow 2026-09-03:
  // a +2.64 book went permanently silent after an 8% peak-to-capital slide).
  // Re-anchor peak to capital while flat and take the post-loss pause
  // instead: permanent death becomes paused-then-retry. The per-bar pause
  // decrement above guarantees the pause expires; the chop gate still
  // applies every bar. Mirrored in runLadderGridBacktest (ladder-grid.ts).
  if (
    !w.longRungs.some((r) => r.filled) &&
    !w.shortRungs.some((r) => r.filled) &&
    accountDrawdownBreached(w, opts)
  ) {
    w.peak = w.capital;
    w.paused = Math.max(
      w.paused,
      Math.max(0, Math.floor(opts.gridPauseAfterLossBars ?? 24)),
    );
  }

  // Re-seed long ladder while flat.
  seedLadderSide(context, "long");

  // Re-seed short ladder while flat.
  seedLadderSide(context, "short");

  // Manage LONG ladder.
  manageLadderSide(context, "long");

  // Manage SHORT ladder.
  manageLadderSide(context, "short");

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

function matchesLadderIdentity(
  state: LadderPaperState,
  options: LadderPaperTradingOptions,
): boolean {
  return (
    state.exchange === options.exchange &&
    state.symbol === options.symbol &&
    state.timeframe === options.timeframe &&
    state.initialCapital === options.initialCapital
  );
}

function matchesLadderGridSettings(
  state: LadderPaperState,
  options: LadderPaperTradingOptions,
): boolean {
  return (
    state.gridStepPct === options.gridStepPct &&
    state.gridMaxGrids === options.gridMaxGrids &&
    state.gridPauseAfterLossBars === options.gridPauseAfterLossBars &&
    state.rungs === options.rungs &&
    state.targetRatio === (options.targetRatio ?? 1) &&
    state.onlyWithTrend === (options.onlyWithTrend ?? false) &&
    state.chopGateAdxThreshold === (options.chopGateAdxThreshold ?? 0)
  );
}
export function configMatchesLadderState(
  state: LadderPaperState,
  options: LadderPaperTradingOptions,
): boolean {
  return (
    matchesLadderIdentity(state, options) &&
    matchesLadderGridSettings(state, options) &&
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
function executeLadderFillLive(
  fill: LadderFillEvent,
  w: WorkingState,
  options: LadderPaperTradingOptions,
  adapter: FuturesExchangeAdapterService,
  riskGuard: RiskGuardService,
  repo: PaperTradingRepositoryService,
  productType: FuturesProductType,
  marginMode: FuturesMarginMode,
): Effect.Effect<
  void,
  ExchangeError | RiskError | PaperTradingRepositoryError,
  never
> {
  return Effect.gen(function* () {
    const side: FuturesOrderSide = fill.side === "long" ? "buy" : "sell";
    const fillPrice = money(fill.fillPrice);
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
    const placed = yield* adapter
      .placeOrder({
        symbol: options.symbol,
        side,
        type: "limit",
        productType,
        marginMode,
        leverage,
        size: sized.qty,
        price: fillPrice,
        reduceOnly: false,
      })
      .pipe(Effect.result);
    if (
      placed._tag === "Failure" ||
      placed.success.filledQty === null ||
      placed.success.filledQty.lessThan(sized.qty)
    ) {
      const detail =
        placed._tag === "Failure"
          ? (placed.failure.reason ?? String(placed.failure))
          : `partial fill ${placed.success.filledQty?.toString() ?? "0"}/${sized.qty.toString()}`;
      return yield* Effect.fail(
        new ExchangeError(
          `ladder rung fill not confirmed (${detail}) — rolling the bar back`,
        ),
      );
    }
    if (adapter.setTradingStop === undefined) return;
    const targetRatio = Math.max(0.001, options.targetRatio ?? 1);
    const stepAbs = toNumber(fillPrice) * (options.gridStepPct / 100);
    const takeProfit =
      fill.side === "long"
        ? toNumber(fillPrice) + stepAbs * targetRatio
        : toNumber(fillPrice) - stepAbs * targetRatio;
    const stopRatio = options.stopRatio ?? 0;
    const stopLoss =
      stopRatio > 0
        ? fill.side === "long"
          ? toNumber(fillPrice) - stepAbs * stopRatio
          : toNumber(fillPrice) + stepAbs * stopRatio
        : undefined;
    yield* adapter
      .setTradingStop({
        symbol: options.symbol,
        productType,
        marginMode,
        side: fill.side,
        takeProfit: money(takeProfit),
        stopLoss: stopLoss === undefined ? undefined : money(stopLoss),
      })
      .pipe(Effect.result);
  });
}

function executeLadderCloseLive(
  close: LadderCloseEvent,
  w: WorkingState,
  options: LadderPaperTradingOptions,
  adapter: FuturesExchangeAdapterService,
  productType: FuturesProductType,
  marginMode: FuturesMarginMode,
  leverage: number,
): Effect.Effect<
  void,
  ExchangeError | RiskError | PaperTradingRepositoryError,
  never
> {
  return Effect.gen(function* () {
    const side: FuturesOrderSide = close.side === "long" ? "sell" : "buy";
    const exitPrice = money(close.exitPrice);
    const closeQty =
      close.qty !== undefined && close.qty > 0
        ? money(close.qty)
        : ladderRungQty(w.capital, options, exitPrice).qty;
    if (closeQty.lessThanOrEqualTo(0)) return;
    yield* adapter.closePosition({
      symbol: options.symbol,
      side,
      productType,
      marginMode,
      leverage,
      size: closeQty,
      price: exitPrice,
    });
  });
}
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
      yield* executeLadderFillLive(
        fill,
        w,
        options,
        adapter,
        riskGuard,
        repo,
        productType,
        marginMode,
      );
    }
    for (const close of closes) {
      yield* executeLadderCloseLive(
        close,
        w,
        options,
        adapter,
        productType,
        marginMode,
        leverage,
      );
    }
  });
}

interface LoadedLadderState {
  readonly state: LadderPaperState;
  readonly earlyResult?: LadderPaperIterationResult;
}

interface LadderOptionalServices {
  readonly killSwitch: Option.Option<KillSwitchService>;
  readonly circuitBreaker: Option.Option<CircuitBreakerService>;
  readonly adapter: Option.Option<FuturesExchangeAdapterService>;
  readonly riskGuard: Option.Option<RiskGuardService>;
}

interface LadderStartIndex {
  readonly index?: number;
  readonly holdNote?: string;
}

function ladderHoldResult(
  state: LadderPaperState,
  note: string,
  w?: WorkingState,
  markPrice?: number,
  options?: LadderPaperTradingOptions,
): LadderPaperIterationResult {
  const marked = w ?? stateToWorking(state);
  const result: LadderPaperIterationResult = {
    action: "hold",
    capital: toNumber(state.capital),
    peakCapital: toNumber(state.peakCapital),
    openRungs: openRungCount(marked),
    closedThisIteration: 0,
    note,
  };
  if (markPrice === undefined || options === undefined) return result;
  const unrealizedPnl = estimateUnrealizedPnl(marked, markPrice, options);
  return {
    ...result,
    equity: toNumber(marked.capital.plus(unrealizedPnl)),
    unrealizedPnl: toNumber(unrealizedPnl),
    markPrice,
  };
}

function forceReseedLadderState(
  repo: PaperTradingRepositoryService,
  gateway: MarketDataGatewayService,
  options: LadderPaperTradingOptions,
  previous: LadderPaperState,
): Effect.Effect<LoadedLadderState, PaperTradingRepositoryError> {
  return Effect.gen(function* () {
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
    if (closePrice === null || closePrice <= 0) {
      const state = freshLadderState(options);
      yield* repo.saveLadderState(state);
      return { state };
    }

    const w = stateToWorking(previous);
    const events: MutableBarEvents = { fills: [], closes: [] };
    for (const side of ["long", "short"] as const) {
      for (const rung of sideRungs(w, side)) {
        if (rung.filled) {
          closeRung(
            w,
            rung,
            closePrice,
            "max_hold",
            options,
            events,
            closeTimestamp,
          );
        }
      }
      setSideRungs(w, side, []);
    }
    w.longBase = 0;
    w.shortBase = 0;
    const state: LadderPaperState = {
      ...freshLadderState(options),
      capital: w.capital,
      peakCapital: Decimal.max(w.peak, money(options.initialCapital)),
      totalWins: w.totalWins,
      totalLosses: w.totalLosses,
      lastTimestamp: previous.lastTimestamp,
    };
    if (events.closes.length > 0 && repo.recordLadderTrades !== undefined) {
      yield* repo.recordLadderTrades(
        events.closes.map((close) => ladderTradeFromClose(options, close)),
      );
    }
    if (events.closes.length === 0) {
      const freshState = freshLadderState(options);
      yield* repo.saveLadderState(freshState);
      return { state: freshState };
    }
    yield* repo.saveLadderState(state);
    return {
      state,
      earlyResult: {
        action: "closed",
        capital: toNumber(state.capital),
        peakCapital: toNumber(state.peakCapital),
        openRungs: 0,
        closedThisIteration: events.closes.length,
        note: `config mismatch on open ladder — force-closed ${events.closes.length} stale rung(s) at ${closePrice}, re-seed next bar`,
      },
    };
  });
}

function loadLadderState(
  repo: PaperTradingRepositoryService,
  gateway: MarketDataGatewayService,
  options: LadderPaperTradingOptions,
): Effect.Effect<LoadedLadderState, PaperTradingRepositoryError> {
  return Effect.gen(function* () {
    const stored = yield* repo.getLadderState(
      options.exchange,
      options.symbol,
      options.timeframe,
    );
    if (stored === null) {
      const state = freshLadderState(options);
      yield* repo.saveLadderState(state);
      return { state };
    }
    if (configMatchesLadderState(stored, options)) return { state: stored };
    if (isFlat(stored)) {
      const state = freshLadderState(options);
      yield* repo.saveLadderState(state);
      return { state };
    }
    if (options.configMismatchAction === "force-reseed") {
      return yield* forceReseedLadderState(repo, gateway, options, stored);
    }
    return {
      state: stored,
      earlyResult: ladderHoldResult(
        stored,
        "config mismatch on open ladder (flat to re-seed)",
      ),
    };
  });
}

function resolveLadderStartIndex(
  state: LadderPaperState,
  options: LadderPaperTradingOptions,
  candles: readonly CandleLike[],
): LadderStartIndex {
  const replayBars = options.replayBars ?? 0;
  if (replayBars > 0) {
    if (state.lastTimestamp === null) {
      return {
        index: Math.max(options.trendFilterPeriod, candles.length - replayBars),
      };
    }
    const nextIndex = candles.findIndex(
      (candle) => candle.timestamp.getTime() > state.lastTimestamp!.getTime(),
    );
    return nextIndex === -1
      ? { holdNote: "no new replay candle" }
      : { index: nextIndex };
  }
  if (state.lastTimestamp === null) {
    return {
      index:
        options.forwardOnly === true
          ? Math.max(0, candles.length - 1)
          : Math.max(1, options.trendFilterPeriod),
    };
  }
  const nextIndex = candles.findIndex(
    (candle) => candle.timestamp.getTime() > state.lastTimestamp!.getTime(),
  );
  return nextIndex === -1
    ? { holdNote: "no new candle" }
    : { index: nextIndex };
}

function loadLadderOptionalServices(): Effect.Effect<LadderOptionalServices> {
  return Effect.gen(function* () {
    return {
      killSwitch: yield* Effect.serviceOption(KillSwitch),
      circuitBreaker: yield* Effect.serviceOption(CircuitBreaker),
      adapter: yield* Effect.serviceOption(FuturesExchangeAdapter),
      riskGuard: yield* Effect.serviceOption(RiskGuard),
    };
  });
}

function guardLadderIteration(
  options: LadderPaperTradingOptions,
  state: LadderPaperState,
  w: WorkingState,
  repo: PaperTradingRepositoryService,
  services: LadderOptionalServices,
): Effect.Effect<
  LadderPaperIterationResult | null,
  CircuitBreakerError | KillSwitchError
> {
  return Effect.gen(function* () {
    if (
      Option.isSome(services.killSwitch) &&
      (yield* services.killSwitch.value.isEngaged())
    ) {
      return ladderHoldResult(state, "ladder held: kill switch engaged", w);
    }
    if (
      Option.isSome(services.circuitBreaker) &&
      (yield* services.circuitBreaker.value.isOpen())
    ) {
      return ladderHoldResult(state, "ladder held: circuit breaker open", w);
    }
    if (options.isLive === true) {
      const blockedUntil = agreementBlockedUntil.get(options.symbol) ?? 0;
      if (blockedUntil > Date.now()) {
        return ladderHoldResult(
          state,
          "symbol blocked: trading terms not accepted (110126) — sign the agreement in the exchange UI",
          w,
        );
      }
      if (Option.isSome(services.adapter)) {
        const productType = options.productType ?? "USDT-FUTURES";
        const realPosition = yield* services.adapter.value
          .getPosition(options.symbol, productType)
          .pipe(Effect.result);
        if (
          realPosition._tag === "Success" &&
          realPosition.success !== null &&
          Number(realPosition.success.quantity) > 0 &&
          openRungCount(w) === 0
        ) {
          yield* services.adapter.value
            .closePosition({
              symbol: options.symbol,
              side: realPosition.success.side === "long" ? "sell" : "buy",
              productType,
              marginMode: options.marginMode ?? "isolated",
              leverage: Math.max(1, options.leverage ?? 1),
              size: realPosition.success.quantity,
            })
            .pipe(Effect.result);
        }
      }
    }
    return null;
  });
}

function rollbackWorkingState(
  target: WorkingState,
  snapshot: WorkingState,
): void {
  target.capital = snapshot.capital;
  target.peak = snapshot.peak;
  target.totalWins = snapshot.totalWins;
  target.totalLosses = snapshot.totalLosses;
  target.longRungs = snapshot.longRungs;
  target.shortRungs = snapshot.shortRungs;
  target.longBase = snapshot.longBase;
  target.shortBase = snapshot.shortBase;
  target.paused = snapshot.paused;
}

function ladderLiveFailureReason(
  failure: ExchangeError | RiskError | PaperTradingRepositoryError,
): string {
  const violations = "violations" in failure ? failure.violations : [];
  return `${failure.reason}${violations.length > 0 ? `: ${violations.join("; ")}` : ""}`;
}

function executeLadderLiveEvents(
  options: LadderPaperTradingOptions,
  state: LadderPaperState,
  w: WorkingState,
  snapshot: WorkingState,
  fills: readonly LadderFillEvent[],
  closes: readonly LadderCloseEvent[],
  last: CandleLike,
  repo: PaperTradingRepositoryService,
  services: LadderOptionalServices,
): Effect.Effect<
  LadderPaperIterationResult | null,
  PaperTradingRepositoryError
> {
  return Effect.gen(function* () {
    if (
      options.isLive !== true ||
      (fills.length === 0 && closes.length === 0)
    ) {
      return null;
    }
    if (Option.isNone(services.adapter) || Option.isNone(services.riskGuard)) {
      return ladderHoldResult(
        state,
        "live ladder requested but FuturesExchangeAdapter/RiskGuard not provided",
      );
    }
    const outcome = yield* executeLadderBarLive(
      fills,
      closes,
      w,
      options,
      services.adapter.value,
      services.riskGuard.value,
      repo,
    ).pipe(Effect.result);
    if (outcome._tag === "Success") return null;
    const failureReason = ladderLiveFailureReason(outcome.failure);
    if (/11012[356]/.test(failureReason)) {
      agreementBlockedUntil.set(
        options.symbol,
        Date.now() + AGREEMENT_BLOCK_MS,
      );
    }
    rollbackWorkingState(w, cloneWorking(snapshot));
    const rolledState = workingToState(options, w, last.timestamp, state);
    yield* repo.saveLadderState(rolledState);
    return {
      action: "hold",
      capital: toNumber(w.capital),
      peakCapital: toNumber(w.peak),
      openRungs: openRungCount(w),
      closedThisIteration: 0,
      note: `live ladder execution failed (bar rolled back): ${failureReason}`,
    };
  });
}

function buildLadderIterationResult(
  state: LadderPaperState,
  w: WorkingState,
  options: LadderPaperTradingOptions,
  candles: readonly CandleLike[],
  startIndex: number,
  openBefore: number,
  closedThisIteration: number,
  iterationFills: readonly LadderFillEvent[],
): LadderPaperIterationResult {
  const openAfter = openRungCount(w);
  const last = candles[candles.length - 1];
  const unrealizedPnl = estimateUnrealizedPnl(w, last.close, options);
  const action: "opened" | "closed" | "hold" =
    closedThisIteration > 0
      ? "closed"
      : openAfter > openBefore
        ? "opened"
        : "hold";
  const chopGateThreshold = options.chopGateAdxThreshold ?? 0;
  const lastAdx =
    chopGateThreshold > 0
      ? makeCausalSymbolStats(candles, options.timeframe)(candles.length - 1)
          .adx14
      : 0;
  const seedsBlockedByChopGate =
    openBefore === 0 &&
    openAfter === 0 &&
    iterationFills.length === 0 &&
    closedThisIteration === 0 &&
    chopGateThreshold > 0 &&
    lastAdx >= chopGateThreshold;
  const ddKilled = accountDrawdownBreached(w, options);
  return {
    action,
    capital: toNumber(state.capital),
    peakCapital: toNumber(state.peakCapital),
    openRungs: openAfter,
    closedThisIteration,
    equity: toNumber(w.capital.plus(unrealizedPnl)),
    unrealizedPnl: toNumber(unrealizedPnl),
    markPrice: last.close,
    note:
      `ladder iter over ${candles.length - startIndex} bars${options.isLive ? " [LIVE]" : ""}` +
      (seedsBlockedByChopGate
        ? `; seeds blocked by ADX gate (${lastAdx.toFixed(2)} >= ${chopGateThreshold})`
        : "") +
      (ddKilled
        ? `; ACCOUNT DRAWDOWN KILL active (max ${options.maxDrawdownPct ?? 0}% from peak — no new seeds)`
        : ""),
  };
}

function ladderRequiredCandles(
  state: LadderPaperState,
  options: LadderPaperTradingOptions,
): number {
  const replayBars = options.replayBars ?? 0;
  const adxWarmup = (options.chopGateAdxThreshold ?? 0) > 0 ? 14 * 2 + 2 : 0;
  const tailWarmup = Math.max(options.trendFilterPeriod + 1, adxWarmup) || 2;
  return replayBars > 0 && state.lastTimestamp === null
    ? replayBars + options.trendFilterPeriod + 5
    : Math.max(tailWarmup + 1, 2);
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
    const gateway = yield* MarketDataGateway;
    yield* repo.ensureTables();
    const loaded = yield* loadLadderState(repo, gateway, options);
    if (loaded.earlyResult) return loaded.earlyResult;
    const state = loaded.state;
    const candles = yield* gateway.fetchOHLCV(
      options.exchange,
      options.symbol,
      options.timeframe,
      ladderRequiredCandles(state, options),
    );
    if (candles.length === 0) {
      return ladderHoldResult(state, "no candles");
    }
    const start = resolveLadderStartIndex(state, options, candles);
    if (start.holdNote) {
      return ladderHoldResult(
        state,
        start.holdNote,
        stateToWorking(state),
        candles[candles.length - 1].close,
        options,
      );
    }
    const startIndex = start.index ?? 0;

    const w = stateToWorking(state);

    const services = yield* loadLadderOptionalServices();
    const guardResult = yield* guardLadderIteration(
      options,
      state,
      w,
      repo,
      services,
    );
    if (guardResult) return guardResult;

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

    const liveResult = yield* executeLadderLiveEvents(
      options,
      state,
      w,
      snapshot,
      iterationFills,
      iterationCloses,
      last,
      repo,
      services,
    );
    if (liveResult) return liveResult;

    if (iterationCloses.length > 0 && repo.recordLadderTrades !== undefined) {
      yield* repo.recordLadderTrades(
        iterationCloses.map((close) => ladderTradeFromClose(options, close)),
      );
    }
    if (Option.isSome(services.circuitBreaker) && iterationCloses.length > 0) {
      yield* services.circuitBreaker.value.recordTradeResult(
        toNumber(w.capital.minus(snapshot.capital)),
        toNumber(startOfDayCapital),
      );
    }

    const nextState = workingToState(options, w, last.timestamp, state);
    yield* repo.saveLadderState(nextState);
    return buildLadderIterationResult(
      nextState,
      w,
      options,
      candles,
      startIndex,
      openBefore,
      closedThisIteration,
      iterationFills,
    );
  });
}

function isFlat(state: LadderPaperState): boolean {
  return (
    state.longRungs.filter((r) => r.filled).length === 0 &&
    state.shortRungs.filter((r) => r.filled).length === 0
  );
}
