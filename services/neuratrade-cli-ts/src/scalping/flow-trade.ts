/**
 * Flow Ignition — flow-v1 live trade engine (TESTNET execution validation).
 *
 * The signal is computed from MAINNET research data in the local DB (the same
 * candles/OI/funding the flow backtest consumes), while orders are routed
 * through the configured exchange adapter (bybit testnet creds for live
 * validation). One directional position per symbol at a time, with the same
 * risk discipline as the grid engines:
 *
 *   - kill switch + circuit breaker gates run BEFORE every action (engaged →
 *     hold, no entries, no exits); a live-position mismatch engages the kill
 *     switch and latches state.killed.
 *   - entries require a flow signal past the threshold; the position is
 *     stopped at entry ∓ 1.25×ATR15, exits on the hold-time grid, and has an
 *     emergency market close when |z_dOI| > 1.5 AND the OFI sign flips against
 *     entry (mirrors the backtest engine's exit rules exactly).
 *   - after +1R the stop moves to breakeven, then trails at 1.25×ATR15.
 *
 * State persists to flow_trade_state after every iteration, so a restart
 * resumes the open position instead of double-entering.
 */

import { Effect } from "effect";
import { Decimal, money, toNumber } from "../utils/money.js";
import type { ExchangeError } from "../exchange/adapter.js";
import type {
  FuturesExchangeAdapterService,
  FuturesMarginMode,
  FuturesProductType,
} from "../exchange/futures-adapter.js";
import type { MarketDataError, MarketDataGatewayService } from "../market-data/gateway.js";
import type {
  PaperTradingRepositoryError,
  PaperTradingRepositoryService,
} from "../paper-trading/repository.js";
import type { ContractSizeSpec } from "../paper-trading/types.js";
import { orderableQty } from "../paper-trading/types.js";
import type { CircuitBreakerError, CircuitBreakerService } from "../risk/circuit-breaker.js";
import type { KillSwitchError, KillSwitchService } from "../risk/kill-switch.js";
import type { RiskError, RiskGuardService } from "../risk/guards.js";
import {
  ATR_STOP_MULT,
  BREAKEVEN_R,
  DEFAULT_EMERGENCY_OI_Z,
  DEFAULT_ENTRY_THRESHOLD,
  DEFAULT_FUNDING_Z_CAP,
  computeContexts,
  computeFlowSignal,
  defaultFlowBacktestOptions,
  type FlowBacktestOptions,
} from "./flow-backtest.js";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export type FlowTradeSide = "LONG" | "SHORT";
export type FlowTradeStage = "initial" | "breakeven" | "trail";

/**
 * Persisted flow-trade state. side/entry fields are null while flat; the
 * UNIQUE(exchange, symbol) row means "one position per symbol at a time".
 */
export interface FlowTradeState {
  readonly exchange: string;
  readonly symbol: string;
  readonly side: FlowTradeSide | null;
  readonly entryPrice: number | null;
  readonly qty: number | null;
  readonly entryTime: number | null;
  /** Entry ∓ 1.25×ATR15, moved to breakeven then trailed after +1R. */
  readonly stopPrice: number | null;
  /** Favorable extreme since entry (high for LONG, low for SHORT). */
  readonly lastPeak: number | null;
  /** Epoch ms at which the time exit fires (entryTime + holdMinutes). */
  readonly exitAt: number | null;
  readonly orderId: string | null;
  readonly capital: number;
  /** ATR15 at entry (stop/trail distance anchor). */
  readonly atr: number | null;
  readonly stage: FlowTradeStage | null;
  /** Sign of the entry OFI (emergency exit compares the current sign). */
  readonly entryOfiSign: number | null;
  readonly lastPrice: number | null;
  /** Sticky kill latch: set by the engine, cleared only when flat + switch
   *  disengaged (grid-engine lifecycle). */
  readonly killed: boolean;
  readonly updatedAt: number;
}

export interface FlowTradeOptions {
  /** Data/exchange key the flow series and adapter orders use
   *  ("bybit-futures" for the mainnet recorder + testnet adapter). */
  readonly exchange: string;
  /** Wire symbol, e.g. "BTCUSDT". */
  readonly symbol: string;
  /** Candle timeframe the flow signal is computed on ("5m" matches the
   *  backtest default; "1m" is supported). */
  readonly timeframe: "1m" | "5m";
  readonly capital: number;
  /** Position size as % of capital (whole number, e.g. 10 = 10%). */
  readonly maxPositionSizePct: number;
  readonly leverage: number;
  readonly productType: FuturesProductType;
  readonly marginMode: FuturesMarginMode;
  /** Entry score threshold in z-units (default 1.0). */
  readonly threshold?: number;
  /** Time exit, in minutes. */
  readonly holdMinutes: number;
  readonly isLive: boolean;
  /** Exchange contract size spec; when present the qty is rounded to the
   *  orderable step (orderableQty) before the order is placed. */
  readonly contractSpecs?: ContractSizeSpec;
  /** Candle-history limit loaded for the signal. Defaults sized to cover the
   *  rolling-z lookback (Z_LOOKBACK windows): 2000×1m / 600×5m. */
  readonly historyLimit?: number;
}

export interface FlowTradeIterationResult {
  readonly action: "opened" | "closed" | "hold";
  readonly side: FlowTradeSide | null;
  readonly state: FlowTradeState;
  readonly note: string;
}

export type FlowTradeError =
  | ExchangeError
  | MarketDataError
  | PaperTradingRepositoryError
  | RiskError
  | KillSwitchError
  | CircuitBreakerError;

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Fresh (flat) state for an exchange+symbol+capital configuration. */
export function freshFlowTradeState(
  opts: FlowTradeOptions,
  updatedAt: number,
): FlowTradeState {
  return {
    exchange: opts.exchange,
    symbol: opts.symbol,
    side: null,
    entryPrice: null,
    qty: null,
    entryTime: null,
    stopPrice: null,
    lastPeak: null,
    exitAt: null,
    orderId: null,
    capital: opts.capital,
    atr: null,
    stage: null,
    entryOfiSign: null,
    lastPrice: null,
    killed: false,
    updatedAt,
  };
}

function signalBacktestOptions(opts: FlowTradeOptions): FlowBacktestOptions {
  return {
    ...defaultFlowBacktestOptions,
    thresholds: {
      entry: opts.threshold ?? DEFAULT_ENTRY_THRESHOLD,
      funding: DEFAULT_FUNDING_Z_CAP,
      emergencyOiZ: DEFAULT_EMERGENCY_OI_Z,
    },
  };
}

const THREE_DAYS_MS = 3 * 86_400_000;

// ---------------------------------------------------------------------------
// Engine
// ---------------------------------------------------------------------------

/**
 * Run one flow-trade iteration for opts.symbol:
 *
 *  1. load persisted state;
 *  2. kill-switch / circuit-breaker gates (engaged → hold, no action);
 *  3. load the latest candles + OI + funding for the symbol from the DB;
 *  4. flat   → enter when the latest flow signal clears the threshold
 *              (size = capital × maxPositionSizePct / price, rounded to the
 *              adapter's step via contract-spec logic when available; stop =
 *              entry ∓ 1.25×ATR15; exitAt = entry + holdMinutes);
 *  5. filled → ATR stop (market close), time exit (market close),
 *              OFI-flip emergency (|z_dOI| > 1.5 ∧ OFI sign flipped → market
 *              close); after +1R trail at 1.25×ATR15;
 *  6. persist state after every iteration.
 */
export function iterateFlowTrade(
  repo: PaperTradingRepositoryService,
  gateway: MarketDataGatewayService,
  adapter: FuturesExchangeAdapterService,
  riskGuard: RiskGuardService,
  killSwitch: KillSwitchService,
  circuitBreaker: CircuitBreakerService,
  opts: FlowTradeOptions,
): Effect.Effect<FlowTradeIterationResult, FlowTradeError, never> {
  return Effect.gen(function* () {
    const now = Date.now();

    let state =
      (yield* repo.getFlowTradeState(opts.exchange, opts.symbol)) ??
      freshFlowTradeState(opts, now);

    // Kill-switch gate BEFORE any action (grid-engine discipline). A sticky
    // killed flag with a clean (flat) state clears only when the switch has
    // been disengaged; otherwise hold and touch nothing.
    if (state.killed && state.side === null && !(yield* killSwitch.isEngaged())) {
      state = { ...state, killed: false, updatedAt: now };
      yield* repo.saveFlowTradeState(state);
    } else if (state.killed || (yield* killSwitch.isEngaged())) {
      const reason = yield* killSwitch
        .getReason()
        .pipe(Effect.orElseSucceed(() => "kill switch engaged"));
      return {
        action: "hold",
        side: state.side,
        state,
        note: `kill switch engaged: ${reason}`,
      };
    }

    if (yield* circuitBreaker.isOpen()) {
      const reason = yield* circuitBreaker
        .getReason()
        .pipe(Effect.orElseSucceed(() => "circuit breaker open"));
      return {
        action: "hold",
        side: state.side,
        state,
        note: `circuit breaker open: ${reason}`,
      };
    }

    // (a) Load the latest candles + OI + funding for the current symbol from
    // the DB (mainnet research data — the live engine never fetches market
    // data from an exchange).
    const historyLimit =
      opts.historyLimit ?? (opts.timeframe === "1m" ? 2000 : 600);
    const since = new Date(now - THREE_DAYS_MS);
    const candles = yield* gateway.fetchOHLCV(
      opts.exchange,
      opts.symbol,
      opts.timeframe,
      historyLimit,
    );
    if (candles.length === 0) {
      return {
        action: "hold",
        side: state.side,
        state,
        note: `no ${opts.timeframe} candles for ${opts.symbol} (run the flow recorder/fetch first)`,
      };
    }
    const oiRows = yield* repo.getOpenInterest(
      opts.symbol,
      opts.timeframe,
      since.getTime(),
    );
    const fundingRates = yield* gateway.fetchFundingRates(
      opts.exchange,
      opts.symbol,
      since,
      new Date(now),
    );
    const oi = oiRows.map((r) => ({ ts: r.ts, oi: r.oi, oiValue: r.oiValue }));
    const funding = fundingRates.map((r) => ({
      ts: r.timestamp.getTime(),
      fundingRate: r.fundingRate,
    }));

    // (b) Compute the flow signal — the SAME computation as the backtest.
    const ctxs = computeContexts(candles, oi, funding);
    const lastCtx = ctxs[ctxs.length - 1];
    if (!lastCtx) {
      return { action: "hold", side: state.side, state, note: "no bar context" };
    }
    const price = lastCtx.close;
    const signals = computeFlowSignal(
      candles,
      oi,
      funding,
      signalBacktestOptions(opts),
      opts.symbol,
    );
    const lastSignal = signals[signals.length - 1];

    const holdWith = (next: FlowTradeState, note: string) =>
      ({ action: "hold" as const, side: next.side, state: next, note });

    // (c) Flat: enter on a signal that clears the threshold.
    if (state.side === null) {
      if (!lastSignal || lastSignal.side === "NONE") {
        return holdWith(
          { ...state, lastPrice: price, updatedAt: now },
          `no entry signal (score ${lastSignal ? lastSignal.score.toFixed(3) : "n/a"})`,
        );
      }
      const side = lastSignal.side;
      const atr = lastSignal.atr15 > 0 ? lastSignal.atr15 : lastCtx.atr15;
      if (atr <= 0 || price <= 0) {
        return holdWith(state, "no entry: ATR15 or price unavailable");
      }

      // Pre-trade risk guard (grid-engine discipline).
      yield* riskGuard.check({
        isLive: opts.isLive,
        capital: opts.capital,
        peakCapital: opts.capital,
        startOfDayCapital: opts.capital,
        dailyRealizedPnl: 0,
        tradesTodayCount: 0,
        positionValue: 0,
        symbol: opts.symbol,
        side: side === "LONG" ? "buy" : "sell",
        leverage: opts.leverage,
        productType: opts.productType,
        minOrderableNotional:
          opts.contractSpecs === undefined
            ? undefined
            : Math.max(
                opts.contractSpecs.minTradeUSDT,
                opts.contractSpecs.minQty * price,
              ),
      });

      // Size = capital × maxPositionSizePct / price, rounded to the adapter's
      // step via the existing contract-spec logic when available.
      const allocation = money(opts.capital).times(opts.maxPositionSizePct / 100);
      const rawQty = allocation.div(price);
      const sizedQty =
        opts.contractSpecs !== undefined
          ? orderableQty(rawQty, opts.contractSpecs, money(price), allocation)
          : rawQty;
      if (toNumber(sizedQty) <= 0) {
        return holdWith(state, "no entry: position size below the minimum");
      }

      yield* adapter.setPositionMode(opts.productType, "one_way");
      yield* adapter.setMarginMode(opts.symbol, opts.productType, opts.marginMode);
      // price is passed as the sizing reference even for market orders so the
      // bybit adapter does not need a live gateway tick for its notional math.
      const fill = yield* adapter.placeOrder({
        symbol: opts.symbol,
        side: side === "LONG" ? "buy" : "sell",
        type: "market",
        size: sizedQty,
        price: money(price),
        productType: opts.productType,
        marginMode: opts.marginMode,
        leverage: opts.leverage,
      });

      const entryPrice = toNumber(fill.filledPrice) > 0 ? toNumber(fill.filledPrice) : price;
      const filledQty = toNumber(fill.filledQty) > 0 ? toNumber(fill.filledQty) : toNumber(sizedQty);
      const stopPrice =
        side === "LONG"
          ? entryPrice - ATR_STOP_MULT * atr
          : entryPrice + ATR_STOP_MULT * atr;
      state = {
        ...freshFlowTradeState(opts, now),
        side,
        entryPrice,
        qty: filledQty,
        entryTime: now,
        stopPrice,
        lastPeak: entryPrice,
        exitAt: now + opts.holdMinutes * 60_000,
        orderId: fill.orderId,
        capital: opts.capital,
        atr,
        stage: "initial",
        entryOfiSign:
          Math.sign(lastSignal.zOfi) !== 0
            ? Math.sign(lastSignal.zOfi)
            : Math.sign(lastSignal.zReturn) || 1,
        lastPrice: entryPrice,
        updatedAt: now,
      };
      yield* repo.saveFlowTradeState(state);
      return {
        action: "opened",
        side,
        state,
        note: `opened ${side} @ ${entryPrice.toFixed(4)} qty ${filledQty} stop ${stopPrice.toFixed(4)} exit ${new Date(state.exitAt!).toISOString()}`,
      };
    }

    // (d) In a position: exit management + trailing.
    const side = state.side;
    const entryPrice = state.entryPrice!;
    const qty = state.qty!;
    const atr = state.atr ?? lastCtx.atr15;

    // Live-position mismatch → engage the kill switch and hold (fail-closed;
    // grid-engine reconciliation discipline).
    const pos = yield* adapter.getPosition(opts.symbol, opts.productType);
    const posSide: FlowTradeSide | null =
      pos?.side === "long" ? "LONG" : pos?.side === "short" ? "SHORT" : null;
    const posQty = pos ? toNumber(pos.quantity) : 0;
    if (pos === null || posSide !== side || posQty <= 0) {
      const reason = `LIVE POSITION MISMATCH: state ${side} but exchange reports ${
        pos === null ? "no position" : `${posSide ?? "unknown"} qty ${posQty}`
      }`;
      yield* killSwitch.engage(reason);
      state = { ...state, killed: true, lastPrice: price, updatedAt: now };
      yield* repo.saveFlowTradeState(state);
      return {
        action: "hold",
        side,
        state,
        note: `kill switch engaged: ${reason}`,
      };
    }

    // Trail: after +1R move the stop to breakeven, then trail at 1.25×ATR15
    // (identical rules to the backtest engine).
    const r = ATR_STOP_MULT * atr;
    let stop = state.stopPrice!;
    let stage: FlowTradeStage = state.stage ?? "initial";
    let extreme = state.lastPeak ?? entryPrice;
    if (side === "LONG") {
      extreme = Math.max(extreme, price);
      if (extreme - entryPrice >= BREAKEVEN_R * r) {
        stage = stop === entryPrice ? stage : "breakeven";
        stop = Math.max(stop, entryPrice, extreme - r);
        if (stop > entryPrice) stage = "trail";
      }
    } else {
      extreme = Math.min(extreme, price);
      if (entryPrice - extreme >= BREAKEVEN_R * r) {
        stage = stop === entryPrice ? stage : "breakeven";
        stop = Math.min(stop, entryPrice, extreme + r);
        if (stop < entryPrice) stage = "trail";
      }
    }

    // Exit checks, in backtest priority order: stop → emergency → time.
    let exitReason: "stop" | "emergency" | "time" | null = null;
    if (side === "LONG" && price <= stop) {
      exitReason = "stop";
    } else if (side === "SHORT" && price >= stop) {
      exitReason = "stop";
    }
    if (exitReason === null) {
      const ofiSign = Math.sign(lastCtx.ofiRaw);
      const entryOfiSign = state.entryOfiSign ?? 0;
      if (
        Math.abs(lastCtx.zOi) > DEFAULT_EMERGENCY_OI_Z &&
        ofiSign !== 0 &&
        entryOfiSign !== 0 &&
        ofiSign !== entryOfiSign
      ) {
        exitReason = "emergency";
      }
    }
    if (exitReason === null && now >= (state.exitAt ?? Number.POSITIVE_INFINITY)) {
      exitReason = "time";
    }

    if (exitReason !== null) {
      const close = yield* adapter.closePosition({
        symbol: opts.symbol,
        side: side === "LONG" ? "sell" : "buy",
        productType: opts.productType,
        marginMode: opts.marginMode,
        leverage: opts.leverage,
        size: money(qty),
      });
      const note = `closed ${side} (${exitReason}) @ ${price.toFixed(4)}${
        close === null ? " — no position on exchange to reduce" : ""
      }`;
      yield* repo.clearFlowTradeState(opts.exchange, opts.symbol);
      return {
        action: "closed",
        side,
        state: { ...freshFlowTradeState(opts, now), lastPrice: price, updatedAt: now },
        note,
      };
    }

    state = {
      ...state,
      stopPrice: stop,
      stage,
      lastPeak: extreme,
      lastPrice: price,
      updatedAt: now,
    };
    yield* repo.saveFlowTradeState(state);
    return holdWith(
      state,
      `in ${side}: price ${price.toFixed(4)} stop ${stop.toFixed(4)} (${stage})`,
    );
  });
}
