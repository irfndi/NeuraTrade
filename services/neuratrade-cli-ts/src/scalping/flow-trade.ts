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
import { money, toNumber } from "../utils/money.js";
import type { ExchangeError } from "../exchange/adapter.js";
import type {
  FuturesExchangeAdapterService,
  FuturesMarginMode,
  FuturesPosition,
  FuturesProductType,
} from "../exchange/futures-adapter.js";
import type {
  MarketDataError,
  MarketDataGatewayService,
} from "../market-data/gateway.js";
import type {
  PaperTradingRepositoryError,
  PaperTradingRepositoryService,
} from "../paper-trading/repository.js";
import type { ContractSizeSpec } from "../paper-trading/types.js";
import { orderableQty } from "../paper-trading/types.js";
import type { CandleLike } from "./types.js";
import type {
  CircuitBreakerError,
  CircuitBreakerService,
} from "../risk/circuit-breaker.js";
import type {
  KillSwitchError,
  KillSwitchService,
} from "../risk/kill-switch.js";
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
  type BarContext,
  type FlowFundingPoint,
  type FlowOiPoint,
  type FlowSignal,
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

interface FlowGateResult {
  readonly state: FlowTradeState;
  readonly earlyResult?: FlowTradeIterationResult;
}

interface FlowMarketData {
  readonly candles: readonly CandleLike[];
  readonly oi: readonly FlowOiPoint[];
  readonly funding: readonly FlowFundingPoint[];
  readonly lastCtx: BarContext;
  readonly lastSignal?: FlowSignal;
  readonly price: number;
}

interface FlowMarketLoadResult {
  readonly market?: FlowMarketData;
  readonly earlyNote?: string;
}

function flowHoldResult(
  state: FlowTradeState,
  note: string,
): FlowTradeIterationResult {
  return { action: "hold", side: state.side, state, note };
}

function guardFlowState(
  repo: PaperTradingRepositoryService,
  killSwitch: KillSwitchService,
  circuitBreaker: CircuitBreakerService,
  state: FlowTradeState,
  now: number,
): Effect.Effect<
  FlowGateResult,
  PaperTradingRepositoryError | KillSwitchError | CircuitBreakerError
> {
  return Effect.gen(function* () {
    let nextState = state;
    if (nextState.killed && nextState.side === null) {
      if (!(yield* killSwitch.isEngaged())) {
        nextState = { ...nextState, killed: false, updatedAt: now };
        yield* repo.saveFlowTradeState(nextState);
      }
    }
    if (nextState.killed || (yield* killSwitch.isEngaged())) {
      const reason = yield* killSwitch
        .getReason()
        .pipe(Effect.orElseSucceed(() => "kill switch engaged"));
      return {
        state: nextState,
        earlyResult: flowHoldResult(
          nextState,
          `kill switch engaged: ${reason}`,
        ),
      };
    }
    if (yield* circuitBreaker.isOpen()) {
      const reason = yield* circuitBreaker
        .getReason()
        .pipe(Effect.orElseSucceed(() => "circuit breaker open"));
      return {
        state: nextState,
        earlyResult: flowHoldResult(
          nextState,
          `circuit breaker open: ${reason}`,
        ),
      };
    }
    return { state: nextState };
  });
}

function loadFlowMarketData(
  repo: PaperTradingRepositoryService,
  gateway: MarketDataGatewayService,
  opts: FlowTradeOptions,
  now: number,
): Effect.Effect<
  FlowMarketLoadResult,
  MarketDataError | PaperTradingRepositoryError
> {
  return Effect.gen(function* () {
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
        earlyNote: `no ${opts.timeframe} candles for ${opts.symbol} (run the flow recorder/fetch first)`,
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
    const oi = oiRows.map((row) => ({
      ts: row.ts,
      oi: row.oi,
      oiValue: row.oiValue,
    }));
    const funding = fundingRates.map((row) => ({
      ts: row.timestamp.getTime(),
      fundingRate: row.fundingRate,
    }));
    const ctxs = computeContexts(candles, oi, funding);
    const lastCtx = ctxs[ctxs.length - 1];
    if (!lastCtx) return { earlyNote: "no bar context" };
    const signals = computeFlowSignal(
      candles,
      oi,
      funding,
      signalBacktestOptions(opts),
      opts.symbol,
    );
    return {
      market: {
        candles,
        oi,
        funding,
        lastCtx,
        lastSignal: signals[signals.length - 1],
        price: lastCtx.close,
      },
    };
  });
}

interface FlowEntryInput {
  readonly repo: PaperTradingRepositoryService;
  readonly adapter: FuturesExchangeAdapterService;
  readonly riskGuard: RiskGuardService;
  readonly opts: FlowTradeOptions;
  readonly state: FlowTradeState;
  readonly market: FlowMarketData;
  readonly now: number;
}

function openFlowTrade(
  input: FlowEntryInput,
): Effect.Effect<FlowTradeIterationResult, FlowTradeError> {
  return Effect.gen(function* () {
    const { repo, adapter, riskGuard, opts, state, market, now } = input;
    const signal = market.lastSignal;
    if (!signal || signal.side === "NONE") {
      const score = signal ? signal.score.toFixed(3) : "n/a";
      return flowHoldResult(
        { ...state, lastPrice: market.price, updatedAt: now },
        `no entry signal (score ${score})`,
      );
    }
    const side = signal.side;
    const atr = signal.atr15 > 0 ? signal.atr15 : market.lastCtx.atr15;
    if (atr <= 0 || market.price <= 0) {
      return flowHoldResult(state, "no entry: ATR15 or price unavailable");
    }

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
              opts.contractSpecs.minQty * market.price,
            ),
    });

    const allocation = money(opts.capital).times(opts.maxPositionSizePct / 100);
    const rawQty = allocation.div(market.price);
    const sizedQty =
      opts.contractSpecs === undefined
        ? rawQty
        : orderableQty(
            rawQty,
            opts.contractSpecs,
            money(market.price),
            allocation,
          );
    if (toNumber(sizedQty) <= 0) {
      return flowHoldResult(state, "no entry: position size below the minimum");
    }

    yield* adapter.setPositionMode(opts.productType, "one_way");
    yield* adapter.setMarginMode(
      opts.symbol,
      opts.productType,
      opts.marginMode,
    );
    const fill = yield* adapter.placeOrder({
      symbol: opts.symbol,
      side: side === "LONG" ? "buy" : "sell",
      type: "market",
      size: sizedQty,
      price: money(market.price),
      productType: opts.productType,
      marginMode: opts.marginMode,
      leverage: opts.leverage,
    });
    const entryPrice =
      toNumber(fill.filledPrice) > 0
        ? toNumber(fill.filledPrice)
        : market.price;
    const filledQty =
      toNumber(fill.filledQty) > 0
        ? toNumber(fill.filledQty)
        : toNumber(sizedQty);
    const stopPrice =
      side === "LONG"
        ? entryPrice - ATR_STOP_MULT * atr
        : entryPrice + ATR_STOP_MULT * atr;
    const nextState: FlowTradeState = {
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
        Math.sign(signal.zOfi) !== 0
          ? Math.sign(signal.zOfi)
          : Math.sign(signal.zReturn) || 1,
      lastPrice: entryPrice,
      updatedAt: now,
    };
    yield* repo.saveFlowTradeState(nextState);
    return {
      action: "opened",
      side,
      state: nextState,
      note: `opened ${side} @ ${entryPrice.toFixed(4)} qty ${filledQty} stop ${stopPrice.toFixed(4)} exit ${new Date(nextState.exitAt!).toISOString()}`,
    };
  });
}

interface FlowPositionInput {
  readonly repo: PaperTradingRepositoryService;
  readonly adapter: FuturesExchangeAdapterService;
  readonly killSwitch: KillSwitchService;
  readonly opts: FlowTradeOptions;
  readonly state: FlowTradeState;
  readonly market: FlowMarketData;
  readonly now: number;
}

function flowPositionSide(
  position: FuturesPosition | null,
): FlowTradeSide | null {
  if (position?.side === "long") return "LONG";
  if (position?.side === "short") return "SHORT";
  return null;
}

function guardFlowPosition(
  input: FlowPositionInput,
): Effect.Effect<FlowGateResult, FlowTradeError> {
  return Effect.gen(function* () {
    const { repo, adapter, killSwitch, opts, state, market, now } = input;
    const position = yield* adapter.getPosition(opts.symbol, opts.productType);
    const positionSide = flowPositionSide(position);
    const positionQty = position ? toNumber(position.quantity) : 0;
    if (position !== null && positionSide === state.side && positionQty > 0) {
      return { state };
    }

    const reason = `LIVE POSITION MISMATCH: state ${state.side} but exchange reports ${
      position === null
        ? "no position"
        : `${positionSide ?? "unknown"} qty ${positionQty}`
    }`;
    yield* killSwitch.engage(reason);
    const nextState = {
      ...state,
      killed: true,
      lastPrice: market.price,
      updatedAt: now,
    };
    yield* repo.saveFlowTradeState(nextState);
    return {
      state: nextState,
      earlyResult: flowHoldResult(nextState, `kill switch engaged: ${reason}`),
    };
  });
}

interface FlowTrailingResult {
  readonly stop: number;
  readonly stage: FlowTradeStage;
  readonly extreme: number;
}

function updateFlowTrailing(
  state: FlowTradeState,
  side: FlowTradeSide,
  price: number,
  entryPrice: number,
  atr: number,
): FlowTrailingResult {
  const riskDistance = ATR_STOP_MULT * atr;
  let stop = state.stopPrice!;
  let stage: FlowTradeStage = state.stage ?? "initial";
  let extreme = state.lastPeak ?? entryPrice;
  if (side === "LONG") {
    extreme = Math.max(extreme, price);
    if (extreme - entryPrice >= BREAKEVEN_R * riskDistance) {
      stage = stop === entryPrice ? stage : "breakeven";
      stop = Math.max(stop, entryPrice, extreme - riskDistance);
      if (stop > entryPrice) stage = "trail";
    }
  } else {
    extreme = Math.min(extreme, price);
    if (entryPrice - extreme >= BREAKEVEN_R * riskDistance) {
      stage = stop === entryPrice ? stage : "breakeven";
      stop = Math.min(stop, entryPrice, extreme + riskDistance);
      if (stop < entryPrice) stage = "trail";
    }
  }
  return { stop, stage, extreme };
}

type FlowExitReason = "stop" | "emergency" | "time";

function flowStopTriggered(
  side: FlowTradeSide,
  price: number,
  stop: number,
): boolean {
  return side === "LONG" ? price <= stop : price >= stop;
}

function flowEmergencyTriggered(
  lastCtx: BarContext,
  state: FlowTradeState,
): boolean {
  const ofiSign = Math.sign(lastCtx.ofiRaw);
  const entryOfiSign = state.entryOfiSign ?? 0;
  return (
    Math.abs(lastCtx.zOi) > DEFAULT_EMERGENCY_OI_Z &&
    ofiSign !== 0 &&
    entryOfiSign !== 0 &&
    ofiSign !== entryOfiSign
  );
}

function resolveFlowExit(
  side: FlowTradeSide,
  price: number,
  stop: number,
  lastCtx: BarContext,
  state: FlowTradeState,
  now: number,
): FlowExitReason | null {
  if (flowStopTriggered(side, price, stop)) return "stop";
  if (flowEmergencyTriggered(lastCtx, state)) return "emergency";
  if (now >= (state.exitAt ?? Number.POSITIVE_INFINITY)) return "time";
  return null;
}

function closeFlowPosition(
  input: FlowPositionInput,
  side: FlowTradeSide,
  qty: number,
  price: number,
  reason: FlowExitReason,
): Effect.Effect<FlowTradeIterationResult, FlowTradeError> {
  return Effect.gen(function* () {
    const { repo, adapter, opts, now } = input;
    const close = yield* adapter.closePosition({
      symbol: opts.symbol,
      side: side === "LONG" ? "sell" : "buy",
      productType: opts.productType,
      marginMode: opts.marginMode,
      leverage: opts.leverage,
      size: money(qty),
    });
    const note = `closed ${side} (${reason}) @ ${price.toFixed(4)}${
      close === null ? " — no position on exchange to reduce" : ""
    }`;
    yield* repo.clearFlowTradeState(opts.exchange, opts.symbol);
    const state = {
      ...freshFlowTradeState(opts, now),
      lastPrice: price,
      updatedAt: now,
    };
    return { action: "closed", side, state, note };
  });
}

function manageFlowPosition(
  input: FlowPositionInput,
): Effect.Effect<FlowTradeIterationResult, FlowTradeError> {
  return Effect.gen(function* () {
    const { repo, state, market, now } = input;
    const positionGuard = yield* guardFlowPosition(input);
    if (positionGuard.earlyResult) return positionGuard.earlyResult;
    if (state.side === null) return flowHoldResult(state, "no flow position");

    const side = state.side;
    const entryPrice = state.entryPrice!;
    const qty = state.qty!;
    const atr = state.atr ?? market.lastCtx.atr15;
    const trailing = updateFlowTrailing(
      state,
      side,
      market.price,
      entryPrice,
      atr,
    );
    const exitReason = resolveFlowExit(
      side,
      market.price,
      trailing.stop,
      market.lastCtx,
      state,
      now,
    );
    if (exitReason !== null) {
      return yield* closeFlowPosition(
        input,
        side,
        qty,
        market.price,
        exitReason,
      );
    }

    const nextState = {
      ...state,
      stopPrice: trailing.stop,
      stage: trailing.stage,
      lastPeak: trailing.extreme,
      lastPrice: market.price,
      updatedAt: now,
    };
    yield* repo.saveFlowTradeState(nextState);
    return flowHoldResult(
      nextState,
      `in ${side}: price ${market.price.toFixed(4)} stop ${trailing.stop.toFixed(4)} (${trailing.stage})`,
    );
  });
}

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

    const loadedState =
      (yield* repo.getFlowTradeState(opts.exchange, opts.symbol)) ??
      freshFlowTradeState(opts, now);
    const gate = yield* guardFlowState(
      repo,
      killSwitch,
      circuitBreaker,
      loadedState,
      now,
    );
    if (gate.earlyResult) return gate.earlyResult;
    const state = gate.state;

    // The live engine reads the same recorded mainnet data as the backtest;
    // the exchange adapter is used only for orders and position checks.
    const marketResult = yield* loadFlowMarketData(repo, gateway, opts, now);
    if (marketResult.market === undefined) {
      return flowHoldResult(state, marketResult.earlyNote ?? "no market data");
    }
    const market = marketResult.market;

    // (c) Flat: enter on a signal that clears the threshold.
    if (state.side === null) {
      return yield* openFlowTrade({
        repo,
        adapter,
        riskGuard,
        opts,
        state,
        market,
        now,
      });
    }

    // (d) In a position: reconcile, trail, and apply the backtest exit order.
    return yield* manageFlowPosition({
      repo,
      adapter,
      killSwitch,
      opts,
      state,
      market,
      now,
    });
  });
}
