import { Effect } from "effect";
import { randomUUID } from "node:crypto";
import type { ComposerConfig, ScalpingSignal } from "../scalping/types.js";
import {
  calculateAnnualizedVolatility,
  calculateATR,
} from "../scalping/indicators.js";
import {
  ExitEngine,
  SignalComposer,
  type ExitEngineImpl,
  type SignalComposerImpl,
} from "../scalping/services.js";
import { Decimal, money, toNumber, type Money } from "../utils/money.js";
import {
  MarketDataError,
  MarketDataGateway,
  type MarketDataGatewayService,
} from "../market-data/gateway.js";
import type { Candle, OrderBook } from "../market-data/types.js";
import { ExchangeError } from "../exchange/adapter.js";
import {
  FuturesExchangeAdapter,
  type FuturesExchangeAdapterService,
  type FuturesMarginMode,
  type FuturesOrderFill,
  type FuturesProductType,
} from "../exchange/futures-adapter.js";
import { RiskError, RiskGuard, type RiskGuardService } from "../risk/guards.js";
import {
  KillSwitch,
  KillSwitchError,
  type KillSwitchService,
} from "../risk/kill-switch.js";
import {
  CircuitBreaker,
  CircuitBreakerError,
  type CircuitBreakerService,
} from "../risk/circuit-breaker.js";
import {
  PaperTradingRepository,
  PaperTradingRepositoryError,
  type PaperTradingRepositoryService,
} from "./repository.js";
import type { ContractSizeSpec, PaperPosition } from "./types.js";
import { orderableQty } from "./types.js";

export interface FuturesPaperTradingOptions {
  readonly exchange: string;
  readonly symbol: string;
  readonly timeframe: string;
  readonly composerConfig: ComposerConfig;
  readonly positionSizePct: number;
  readonly riskPerTradePct: number;
  readonly maxPositionSizePct: number;
  readonly feePct: number;
  readonly minConfidence: number;
  readonly useAtrStops: boolean;
  readonly atrStopMultiplier: number;
  readonly atrTakeProfitMultiplier: number;
  readonly atrRiskReward: number;
  readonly scaleOutAtR: number;
  readonly scaleOutPct: number;
  readonly volatilityLookback: number;
  readonly volatilityLowPct: number;
  readonly volatilityHighPct: number;
  readonly volatilityLowFactor: number;
  readonly volatilityHighFactor: number;
  readonly volatilityTargetAnnualPct: number;
  readonly stopLossPct: number;
  readonly takeProfitPct: number;
  readonly holdUntilStop: boolean;
  readonly minAtrPct: number;
  readonly initialCapital: number;
  readonly isLive: boolean;
  readonly leverage: number;
  readonly marginMode: FuturesMarginMode;
  readonly productType: FuturesProductType;
  /** Hard stop on realized daily loss as % of capital (e.g. 2 = 2%). When
   *  set, per-trade risk is additionally capped at maxDailyLossPct divided by
   *  maxConcurrentTrades so a full stop-out of every position cannot breach
   *  the daily stop. Undefined disables the bound (preserves prior sizing). */
  readonly maxDailyLossPct?: number;
  /** Number of positions the portfolio may hold concurrently; divides the
   *  daily-loss bound into a per-trade risk cap. Defaults to 1. */
  readonly maxConcurrentTrades?: number;
  /** Minimum notional (quote units) the exchange accepts for a futures order
   *  (Bitget observes ~5 USDT). When the risk-sized notional falls below this
   *  floor, the notional is raised to the floor, bounded by margin capacity
   *  capital * leverage. Defaults to 5. Ignored when contractSpecs is set. */
  readonly notionalFloor?: number;
  /** Exchange contract size constraints (minQty, qtyStep, minTradeUSDT) for
   *  this symbol, populated by the CLI from BitgetClient.getContracts on the
   *  live path and by tests directly. When set, sizing rounds the order qty
   *  UP to the size step, raises a sub-minimum qty to minQty, computes the
   *  effective orderability floor as max(minTradeUSDT, minQty * price), lifts
   *  leverage so the floor's margin fits the position cap, and SKIPS the
   *  trade (never placing an unorderable or over-cap order) when the margin
   *  still cannot fit. Absent => legacy sizing (flat notionalFloor, no step
   *  rounding). */
  readonly contractSpecs?: ContractSizeSpec;
}

export interface FuturesPaperTradingIterationResult {
  readonly action: "opened" | "closed" | "hold" | "scaled_out";
  readonly position: PaperPosition | null;
  readonly capital: number;
  readonly note: string;
}

/** Result of risk-aware notional sizing for a futures paper trade. */
interface FuturesNotionalSizing {
  notional: Decimal;
  size: Decimal;
  leverage: number;
  /** Set when the minimum orderable position cannot fit the position cap;
   *  the caller must skip the trade (never place an unorderable order). */
  skipReason?: string;
  /** Effective orderability floor (max(minTradeUSDT, minQty * price)) when
   *  contract specs are present; passed to the risk guard so it can fail
   *  closed locally for paths that bypass this sizing. */
  minOrderableNotional?: number;
}

interface FuturesEntryPlan {
  readonly side: "long" | "short";
  readonly atr: number | null;
  readonly useAtr: boolean;
  readonly entryPrice: Money;
  readonly stopDistancePct: Money;
  readonly currentVolatility: number;
  readonly sizing: FuturesNotionalSizing;
}

interface FuturesEntryPreparation {
  readonly plan?: FuturesEntryPlan;
  readonly holdNote?: string;
}

interface FuturesIterationServices {
  readonly repo: PaperTradingRepositoryService;
  readonly adapter: FuturesExchangeAdapterService;
  readonly killSwitch: KillSwitchService;
  readonly circuitBreaker: CircuitBreakerService;
  readonly riskGuard: RiskGuardService;
  readonly composer: SignalComposerImpl;
  readonly exitEngine: ExitEngineImpl;
}

type FuturesIterationError =
  | MarketDataError
  | PaperTradingRepositoryError
  | ExchangeError
  | RiskError
  | KillSwitchError
  | CircuitBreakerError;

interface FuturesIterationState {
  readonly capital: Decimal;
  readonly peakCapital: Decimal;
  readonly todayPnl: Decimal;
  readonly startOfDayCapital: Decimal;
}

function futuresHoldResult(
  position: PaperPosition | null,
  capital: Decimal,
  note: string,
): FuturesPaperTradingIterationResult {
  return {
    action: "hold",
    position,
    capital: toNumber(capital),
    note,
  };
}

function prepareFuturesEntry(
  options: FuturesPaperTradingOptions,
  signal: ScalpingSignal,
  candles: readonly Candle[],
  orderBook: OrderBook,
  capital: Decimal,
): FuturesEntryPreparation {
  const side = signal.direction === "buy" ? "long" : "short";
  const needsAtr = options.useAtrStops || options.minAtrPct > 0;
  const atr = needsAtr ? calculateATR(candles, 14) : null;
  const useAtr = options.useAtrStops && atr !== null && atr > 0;
  const entryPrice = midPriceMoney(orderBook);
  const estimatedStopLoss = useAtr
    ? side === "long"
      ? entryPrice.minus(money(atr).times(options.atrStopMultiplier))
      : entryPrice.plus(money(atr).times(options.atrStopMultiplier))
    : side === "long"
      ? entryPrice.times(1 - options.stopLossPct / 100)
      : entryPrice.times(1 + options.stopLossPct / 100);
  const stopDistancePct = entryPrice.greaterThan(0)
    ? entryPrice.minus(estimatedStopLoss).abs().div(entryPrice)
    : money(0);
  const currentVolatility = calculateAnnualizedVolatility(
    candles,
    options.volatilityLookback,
    options.timeframe,
  );

  if (options.minAtrPct > 0) {
    const atrPct =
      atr !== null && entryPrice.greaterThan(0)
        ? money(atr).div(entryPrice).toNumber()
        : 0;
    if (atrPct < options.minAtrPct / 100) {
      return {
        holdNote: `LOW VOLATILITY: atrPct=${(atrPct * 100).toFixed(3)}% < ${options.minAtrPct}%`,
      };
    }
  }

  const sizing = calculateFuturesNotionalValue(
    capital,
    entryPrice,
    stopDistancePct,
    currentVolatility,
    options,
  );
  if (sizing.skipReason !== undefined) {
    return {
      holdNote: `RISK BLOCKED (orderability): ${sizing.skipReason}`,
    };
  }
  return {
    plan: {
      side,
      atr,
      useAtr,
      entryPrice,
      stopDistancePct,
      currentVolatility,
      sizing,
    },
  };
}

function buildFuturesPosition(
  options: FuturesPaperTradingOptions,
  signal: ScalpingSignal,
  plan: FuturesEntryPlan,
  fill: FuturesOrderFill,
  exitLevels: {
    readonly stopLoss: number;
    readonly takeProfit: number;
    readonly scaleOutPrice?: number | null;
  },
): PaperPosition {
  return {
    id: randomUUID(),
    exchange: options.exchange,
    symbol: options.symbol,
    timeframe: options.timeframe,
    side: plan.side,
    entryPrice: money(fill.filledPrice),
    size: money(fill.filledQty),
    stopLoss: money(exitLevels.stopLoss),
    takeProfit: money(exitLevels.takeProfit),
    openedAt: new Date(),
    signalId: signal.id,
    leverage: plan.sizing.leverage,
    scaledOut: false,
    scaleOutPrice: money(exitLevels.scaleOutPrice ?? 0),
  };
}

function calculateFuturesNotionalValue(
  capital: Decimal,
  entryPrice: Decimal,
  stopDistancePct: Money,
  currentVolatility: number,
  options: FuturesPaperTradingOptions,
): FuturesNotionalSizing {
  // Risk-based notional sizing: riskPerTradePct of capital / stopDistancePct.
  const maxNotionalByRiskCap = capital.times(
    (options.maxPositionSizePct ?? 100) / 100,
  );
  const spec = options.contractSpecs;

  let baseNotional: Decimal;
  let leverage = options.leverage;
  if (
    options.riskPerTradePct &&
    options.riskPerTradePct > 0 &&
    stopDistancePct.greaterThan(0)
  ) {
    let riskAmount = capital.times(options.riskPerTradePct / 100);
    // Daily-cap bound: never risk more than maxDailyLossPct divided by the
    // number of concurrent positions in a single trade, so a simultaneous
    // stop-out of every position cannot breach the hard daily stop.
    if (options.maxDailyLossPct !== undefined && options.maxDailyLossPct > 0) {
      const maxConcurrentTrades = Math.max(1, options.maxConcurrentTrades ?? 1);
      const perTradeRiskCap = capital.times(
        options.maxDailyLossPct / 100 / maxConcurrentTrades,
      );
      riskAmount = Decimal.min(riskAmount, perTradeRiskCap);
    }
    baseNotional = riskAmount.div(stopDistancePct);
  } else {
    // Fallback to legacy margin allocation.
    const allocatedMargin = capital.times(options.positionSizePct / 100);
    const marginPerContract = entryPrice.div(leverage);
    const feePerContract = entryPrice.times(options.feePct / 100);
    const size = allocatedMargin.div(marginPerContract.plus(feePerContract));
    baseNotional = size.times(entryPrice);
  }

  if (
    options.volatilityTargetAnnualPct &&
    options.volatilityTargetAnnualPct > 0 &&
    currentVolatility > 0
  ) {
    baseNotional = baseNotional.times(
      options.volatilityTargetAnnualPct / currentVolatility,
    );
  }

  const maxNotionalByMargin = capital.times(leverage);
  let notional = Decimal.min(
    baseNotional,
    maxNotionalByRiskCap,
    maxNotionalByMargin,
  );

  if (spec !== undefined) {
    // Contract-aware orderability (live path). Effective floor comes from the
    // contract itself: max(minTradeUSDT, minQty x price). A 5 USDT BTC order
    // at $64,795 is 0.000077 BTC — below the 0.0001 minTradeNum; rounding the
    // qty UP to the size step yields 0.0001 (~$6.48), which is orderable.
    const effectiveFloor = Decimal.max(
      money(spec.minTradeUSDT),
      money(spec.minQty).times(entryPrice),
    );
    const allocation = capital.times((options.maxPositionSizePct ?? 100) / 100);

    notional = orderableQty(
      notional.div(entryPrice),
      spec,
      entryPrice,
      maxNotionalByRiskCap,
    ).times(entryPrice);

    // Sub-floor notional: raise to the effective floor and lift leverage so
    // the floor's margin (notional / leverage) fits the position cap, bounded
    // by the configured max leverage.
    if (notional.lessThan(effectiveFloor)) {
      const allocNum = toNumber(allocation);
      if (allocNum > 0) {
        leverage = Math.min(
          options.leverage,
          Math.max(1, Math.ceil(toNumber(effectiveFloor) / allocNum)),
        );
        notional = orderableQty(
          effectiveFloor.div(entryPrice),
          spec,
          entryPrice,
          maxNotionalByRiskCap,
        ).times(entryPrice);
      }
    }

    // Never attempt an unorderable or over-cap order: if the required margin
    // (notional / leverage) cannot fit within the position cap even after the
    // leverage raise, skip the trade instead of sending an order the exchange
    // will reject (or that breaches the account's risk budget).
    const margin = notional.div(leverage);
    if (margin.greaterThan(maxNotionalByRiskCap)) {
      return {
        notional,
        size: money(0),
        leverage,
        skipReason: `min orderable notional ${notional.toFixed(2)} USDT requires margin ${margin.toFixed(2)} at ${leverage}x, exceeding the ${toNumber(maxNotionalByRiskCap).toFixed(2)} USDT position cap`,
        minOrderableNotional: toNumber(effectiveFloor),
      };
    }

    return {
      notional,
      size: notional.div(entryPrice),
      leverage,
      minOrderableNotional: toNumber(effectiveFloor),
    };
  }

  // Legacy exchange minimum-notional floor (Bitget observes ~5 USDT). A
  // risk-sized position below the floor is not placeable, so raise it to the
  // floor and use leverage to make the required margin fit the account.
  // Bounded by margin capacity capital * leverage: an account that cannot
  // afford the floor even at max leverage trades at its full capacity instead.
  const notionalFloor = options.notionalFloor ?? 5;
  if (notional.lessThan(notionalFloor)) {
    const capitalNum = toNumber(capital);
    if (capitalNum > 0) {
      leverage = Math.min(
        options.leverage,
        Math.max(1, Math.ceil(notionalFloor / capitalNum)),
      );
      notional = Decimal.min(
        Decimal.max(notional, notionalFloor),
        capital.times(leverage),
      );
    }
  }

  return { notional, size: notional.div(entryPrice), leverage };
}

function handleFuturesScaleOut(
  services: FuturesIterationServices,
  options: FuturesPaperTradingOptions,
  position: PaperPosition,
  currentCandle: Candle,
  state: FuturesIterationState,
): Effect.Effect<FuturesPaperTradingIterationResult, FuturesIterationError> {
  return Effect.gen(function* () {
    if (options.isLive) {
      return yield* Effect.fail(
        new ExchangeError(
          "live futures scale-out is disabled until exchange-fill reconciliation is implemented",
        ),
      );
    }
    const exitPrice = fallbackExitPrice(position, currentCandle, "scale_out");
    const scaleOut = yield* services.repo.scaleOutPosition(
      position,
      exitPrice,
      options.scaleOutPct,
      currentCandle.timestamp,
    );
    const closeFee = exitPrice
      .times(scaleOut.trade.size)
      .times(options.feePct / 100);
    const capital = state.capital.plus(scaleOut.trade.pnl).minus(closeFee);
    const peakCapital = Decimal.max(state.peakCapital, capital);
    yield* services.repo.setPortfolio(capital, peakCapital);
    yield* services.repo.saveOpenPosition(scaleOut.updatedPosition);
    yield* services.circuitBreaker.recordTradeResult(
      toNumber(scaleOut.trade.pnl),
      toNumber(state.startOfDayCapital),
    );
    return {
      action: "scaled_out" as const,
      position: scaleOut.updatedPosition,
      capital: toNumber(capital),
      note: `SCALE-OUT ${scaleOut.trade.side} ${scaleOut.trade.entryPrice.toFixed(2)} → ${scaleOut.trade.exitPrice.toFixed(2)} size=${scaleOut.trade.size.toFixed(6)} | PnL ${scaleOut.trade.pnlPct.toFixed(2)}%`,
    };
  });
}

function handleFuturesClose(
  services: FuturesIterationServices,
  options: FuturesPaperTradingOptions,
  position: PaperPosition,
  currentCandle: Candle,
  exitReason: "stop_loss" | "take_profit" | "signal",
  state: FuturesIterationState,
): Effect.Effect<FuturesPaperTradingIterationResult, FuturesIterationError> {
  return Effect.gen(function* () {
    const fill = yield* services.adapter.closePosition({
      symbol: options.symbol,
      side: position.side === "long" ? "sell" : "buy",
      productType: options.productType,
      marginMode: options.marginMode,
      leverage: position.leverage ?? options.leverage,
      size: position.size,
      price: money(currentCandle.close),
    });
    if (!fill && options.isLive) {
      return yield* Effect.fail(
        new ExchangeError(
          "live futures close returned no exchange fill; preserving the local position",
        ),
      );
    }

    const exitPrice = fill
      ? money(fill.filledPrice)
      : fallbackExitPrice(position, currentCandle, exitReason);
    const closeFee = fill
      ? money(fill.fee)
      : exitPrice.times(position.size).times(options.feePct / 100);
    const trade = yield* services.repo.closePosition(
      position,
      exitPrice,
      exitReason,
      currentCandle.timestamp,
    );
    const capital = state.capital.plus(trade.pnl).minus(closeFee);
    const peakCapital = Decimal.max(state.peakCapital, capital);
    yield* services.repo.setPortfolio(capital, peakCapital);
    yield* services.circuitBreaker.recordTradeResult(
      toNumber(trade.pnl),
      toNumber(state.startOfDayCapital),
    );
    return {
      action: "closed" as const,
      position: null,
      capital: toNumber(capital),
      note: `${trade.side} ${trade.entryPrice.toFixed(2)} → ${trade.exitPrice.toFixed(2)} | PnL ${trade.pnlPct.toFixed(2)}% | ${trade.exitReason}`,
    };
  });
}

function handleFuturesExistingPosition(
  services: FuturesIterationServices,
  options: FuturesPaperTradingOptions,
  position: PaperPosition,
  currentCandle: Candle,
  signal: ScalpingSignal | null,
  state: FuturesIterationState,
): Effect.Effect<
  FuturesPaperTradingIterationResult | null,
  FuturesIterationError
> {
  return Effect.gen(function* () {
    const exitReason = checkExitReason(
      position,
      currentCandle,
      signal,
      options,
    );
    if (exitReason === "scale_out") {
      return yield* handleFuturesScaleOut(
        services,
        options,
        position,
        currentCandle,
        state,
      );
    }
    if (exitReason === null) return null;
    return yield* handleFuturesClose(
      services,
      options,
      position,
      currentCandle,
      exitReason,
      state,
    );
  });
}

function checkFuturesEntrySafety(
  services: FuturesIterationServices,
  options: FuturesPaperTradingOptions,
  signal: ScalpingSignal,
  plan: FuturesEntryPlan,
  state: FuturesIterationState,
): Effect.Effect<
  FuturesPaperTradingIterationResult | "ready",
  FuturesIterationError
> {
  return Effect.gen(function* () {
    if (yield* services.killSwitch.isEngaged()) {
      const reason = yield* services.killSwitch.getReason();
      return futuresHoldResult(
        null,
        state.capital,
        `KILL SWITCH ENGAGED: ${reason}`,
      );
    }
    if (yield* services.circuitBreaker.isOpen()) {
      const reason = yield* services.circuitBreaker.getReason();
      return futuresHoldResult(
        null,
        state.capital,
        `CIRCUIT BREAKER OPEN: ${reason}`,
      );
    }

    const tradesTodayCount = yield* services.repo.countTradesForDate(
      new Date(),
    );
    const riskCheck = yield* services.riskGuard
      .check({
        isLive: options.isLive,
        capital: toNumber(state.capital),
        peakCapital: toNumber(state.peakCapital),
        startOfDayCapital: toNumber(state.startOfDayCapital),
        dailyRealizedPnl: toNumber(state.todayPnl),
        tradesTodayCount,
        positionValue: toNumber(plan.sizing.notional),
        symbol: options.symbol,
        side: signal.direction as "buy" | "sell",
        leverage: plan.sizing.leverage,
        productType: options.productType,
        minOrderableNotional: plan.sizing.minOrderableNotional,
      })
      .pipe(Effect.result);
    if (riskCheck._tag === "Failure") {
      yield* services.repo.setPortfolio(state.capital, state.peakCapital);
      return futuresHoldResult(
        null,
        state.capital,
        `RISK BLOCKED: ${riskCheck.failure.violations.join("; ")}`,
      );
    }
    return "ready" as const;
  });
}

function executeFuturesEntry(
  services: FuturesIterationServices,
  options: FuturesPaperTradingOptions,
  signal: ScalpingSignal,
  plan: FuturesEntryPlan,
  candles: readonly Candle[],
  currentCandle: Candle,
  state: FuturesIterationState,
): Effect.Effect<FuturesPaperTradingIterationResult, FuturesIterationError> {
  return Effect.gen(function* () {
    yield* services.adapter.setLeverage(
      options.symbol,
      options.productType,
      options.marginMode,
      plan.sizing.leverage,
    );
    yield* services.adapter.setMarginMode(
      options.symbol,
      options.productType,
      options.marginMode,
    );
    const fill = yield* services.adapter.placeOrder({
      symbol: options.symbol,
      side: signal.direction as "buy" | "sell",
      type: "market",
      size: plan.sizing.size,
      productType: options.productType,
      marginMode: options.marginMode,
      leverage: plan.sizing.leverage,
      price: money(currentCandle.close),
    });
    const capital = state.capital.minus(money(fill.fee));
    const stopMult = options.atrStopMultiplier;
    const tpMult = options.atrTakeProfitMultiplier;
    const atrRiskReward =
      options.atrRiskReward > 0 ? options.atrRiskReward : tpMult / stopMult;
    const exitLevels = yield* services.exitEngine.computeExitLevels({
      side: plan.side,
      entryPrice: toNumber(fill.filledPrice, 8),
      atr: plan.atr,
      useAtr: plan.useAtr,
      atrStopMultiplier: stopMult,
      atrRiskReward,
      stopLossPct: options.stopLossPct,
      takeProfitPct: options.takeProfitPct,
      scaleOutAtR: options.scaleOutAtR,
      candles,
      volatilityLookback: options.volatilityLookback,
      volatilityLowPct: options.volatilityLowPct,
      volatilityHighPct: options.volatilityHighPct,
      volatilityLowFactor: options.volatilityLowFactor,
      volatilityHighFactor: options.volatilityHighFactor,
    });
    const newPosition = buildFuturesPosition(
      options,
      signal,
      plan,
      fill,
      exitLevels,
    );
    yield* services.repo.saveOpenPosition(newPosition);
    yield* services.repo.setPortfolio(capital, state.peakCapital);
    return {
      action: "opened" as const,
      position: newPosition,
      capital: toNumber(capital),
      note: `${plan.side} ${fill.filledPrice.toFixed(2)} size=${fill.filledQty.toFixed(6)} leverage=${plan.sizing.leverage}x SL=${newPosition.stopLoss.toFixed(2)} TP=${newPosition.takeProfit.toFixed(2)}`,
    };
  });
}

function openFuturesEntry(
  services: FuturesIterationServices,
  options: FuturesPaperTradingOptions,
  signal: ScalpingSignal,
  candles: readonly Candle[],
  orderBook: OrderBook,
  state: FuturesIterationState,
): Effect.Effect<FuturesPaperTradingIterationResult, FuturesIterationError> {
  return Effect.gen(function* () {
    if (state.capital.lessThanOrEqualTo(0)) {
      return futuresHoldResult(
        null,
        state.capital,
        "capital exhausted (blown paper account)",
      );
    }
    const preparation = prepareFuturesEntry(
      options,
      signal,
      candles,
      orderBook,
      state.capital,
    );
    if (preparation.holdNote !== undefined) {
      return futuresHoldResult(null, state.capital, preparation.holdNote);
    }
    const plan = preparation.plan;
    if (plan === undefined) {
      return futuresHoldResult(null, state.capital, "entry plan unavailable");
    }
    const safety = yield* checkFuturesEntrySafety(
      services,
      options,
      signal,
      plan,
      state,
    );
    if (safety !== "ready") return safety;
    return yield* executeFuturesEntry(
      services,
      options,
      signal,
      plan,
      candles,
      candles[candles.length - 1],
      state,
    );
  });
}

function processFuturesMarket(
  services: FuturesIterationServices,
  options: FuturesPaperTradingOptions,
  position: PaperPosition | null,
  candles: readonly Candle[],
  orderBook: OrderBook,
  state: Pick<FuturesIterationState, "capital" | "peakCapital">,
): Effect.Effect<FuturesPaperTradingIterationResult, FuturesIterationError> {
  return Effect.gen(function* () {
    const currentCandle = candles[candles.length - 1];
    const obMetrics = toOrderBookMetrics(orderBook);
    const signal = yield* services.composer.composeSignal(
      {
        exchange: options.exchange,
        symbol: options.symbol,
        timeframe: options.timeframe,
        candles,
      },
      obMetrics,
      options.composerConfig,
    );
    const todayPnl = yield* services.repo.getTodayRealizedPnl();
    const startOfDayCapital = yield* services.repo.getStartOfDayCapital(
      new Date(),
      state.capital,
    );
    const iterationState = { ...state, todayPnl, startOfDayCapital };

    if (position) {
      const existingOutcome = yield* handleFuturesExistingPosition(
        services,
        options,
        position,
        currentCandle,
        signal,
        iterationState,
      );
      if (existingOutcome) return existingOutcome;
    }
    if (!position && signal && isEntrySignal(signal, options.minConfidence)) {
      return yield* openFuturesEntry(
        services,
        options,
        signal,
        candles,
        orderBook,
        iterationState,
      );
    }

    yield* services.repo.setPortfolio(
      iterationState.capital,
      iterationState.peakCapital,
    );
    const note = signal
      ? `${signal.direction} (conf=${signal.confidence.toFixed(2)})`
      : "no signal";
    return futuresHoldResult(position, iterationState.capital, note);
  });
}

/**
 * Run a single futures paper-trading iteration.
 *
 * Sizing is account-scaled: with riskPerTradePct set, the notional is
 * (capital * riskPerTradePct/100) / stopDistancePct, capped by
 * maxPositionSizePct, the per-trade daily-loss bound (maxDailyLossPct /
 * maxConcurrentTrades) and margin capacity; sub-floor notionals are raised to
 * notionalFloor with leverage lifted to fit the account. Without
 * riskPerTradePct it falls back to margin allocation
 * (capital * positionSizePct/100 * leverage). When contractSpecs is set, the
 * qty is rounded to the exchange size step, the effective floor becomes
 * max(minTradeUSDT, minQty * price), and the trade is skipped (never placed)
 * when the minimum orderable margin cannot fit the position cap even at max
 * leverage. The adapter enforces reduce-only closes so the position cannot be
 * accidentally doubled on exit.
 */
export function runFuturesPaperTradingIteration(
  options: FuturesPaperTradingOptions,
): Effect.Effect<
  FuturesPaperTradingIterationResult,
  | MarketDataError
  | PaperTradingRepositoryError
  | ExchangeError
  | RiskError
  | KillSwitchError
  | CircuitBreakerError,
  | MarketDataGatewayService
  | PaperTradingRepositoryService
  | FuturesExchangeAdapterService
  | RiskGuardService
  | KillSwitchService
  | CircuitBreakerService
  | SignalComposer
  | ExitEngine
> {
  return Effect.gen(function* () {
    const services: FuturesIterationServices = {
      repo: yield* PaperTradingRepository,
      adapter: yield* FuturesExchangeAdapter,
      killSwitch: yield* KillSwitch,
      circuitBreaker: yield* CircuitBreaker,
      riskGuard: yield* RiskGuard,
      composer: yield* SignalComposer,
      exitEngine: yield* ExitEngine,
    };
    const gateway = yield* MarketDataGateway;
    yield* services.repo.ensureTables();

    const portfolio = yield* services.repo.getPortfolio();
    const isFreshPortfolio =
      portfolio.capital.lessThanOrEqualTo(0) &&
      portfolio.peakCapital.lessThanOrEqualTo(0);
    const capital = isFreshPortfolio
      ? money(options.initialCapital)
      : portfolio.capital;
    const peakCapital = Decimal.max(portfolio.peakCapital, capital);
    const position = yield* services.repo.getOpenPosition(
      options.exchange,
      options.symbol,
    );
    const candles = yield* gateway.fetchOHLCV(
      options.exchange,
      options.symbol,
      options.timeframe,
      100,
    );
    const orderBook = yield* gateway.fetchOrderBook(
      options.exchange,
      options.symbol,
      20,
    );

    if (candles.length < 30) {
      return futuresHoldResult(position, capital, "insufficient candles");
    }
    if (orderBook.bids.length === 0 && orderBook.asks.length === 0) {
      return futuresHoldResult(position, capital, "empty order book");
    }
    return yield* processFuturesMarket(
      services,
      options,
      position,
      candles,
      orderBook,
      { capital, peakCapital },
    );
  });
}

function checkExitReason(
  position: PaperPosition,
  candle: Candle,
  signal: ScalpingSignal | null,
  options: FuturesPaperTradingOptions,
): "stop_loss" | "take_profit" | "signal" | "scale_out" | null {
  if (!position.scaledOut && position.scaleOutPrice.greaterThan(0)) {
    if (position.side === "long") {
      if (money(candle.high).greaterThanOrEqualTo(position.scaleOutPrice))
        return "scale_out";
    } else {
      if (money(candle.low).lessThanOrEqualTo(position.scaleOutPrice))
        return "scale_out";
    }
  }

  if (position.side === "long") {
    if (money(candle.low).lessThanOrEqualTo(position.stopLoss))
      return "stop_loss";
    if (money(candle.high).greaterThanOrEqualTo(position.takeProfit))
      return "take_profit";
  } else {
    if (money(candle.high).greaterThanOrEqualTo(position.stopLoss))
      return "stop_loss";
    if (money(candle.low).lessThanOrEqualTo(position.takeProfit))
      return "take_profit";
  }

  if (
    !options.holdUntilStop &&
    signal &&
    shouldExitPosition(position, signal)
  ) {
    return "signal";
  }

  return null;
}

function fallbackExitPrice(
  position: PaperPosition,
  candle: Candle,
  reason: "stop_loss" | "take_profit" | "signal" | "scale_out",
): Decimal {
  const open = money(candle.open);
  if (reason === "stop_loss") {
    return position.side === "long"
      ? Decimal.min(open, position.stopLoss)
      : Decimal.max(open, position.stopLoss);
  }
  if (reason === "take_profit") {
    return position.side === "long"
      ? Decimal.max(open, position.takeProfit)
      : Decimal.min(open, position.takeProfit);
  }
  if (reason === "scale_out") {
    return position.side === "long"
      ? Decimal.max(open, position.scaleOutPrice)
      : Decimal.min(open, position.scaleOutPrice);
  }
  return money(candle.close);
}

function shouldExitPosition(
  position: PaperPosition,
  signal: ScalpingSignal,
): boolean {
  return (
    (position.side === "long" && signal.direction === "sell") ||
    (position.side === "short" && signal.direction === "buy")
  );
}

function isEntrySignal(signal: ScalpingSignal, minConfidence: number): boolean {
  return signal.direction !== "hold" && signal.confidence >= minConfidence;
}

function midPrice(orderBook: OrderBook): number {
  if (orderBook.bids.length === 0 || orderBook.asks.length === 0) {
    return orderBook.bids[0]?.price ?? orderBook.asks[0]?.price ?? 0;
  }
  return (orderBook.bids[0].price + orderBook.asks[0].price) / 2;
}

/** Orderbook mid as money (Decimal), used for entry-price math. */
function midPriceMoney(orderBook: OrderBook): Money {
  if (orderBook.bids.length === 0 || orderBook.asks.length === 0) {
    return money(orderBook.bids[0]?.price ?? orderBook.asks[0]?.price ?? 0);
  }
  return money(orderBook.bids[0].price).plus(orderBook.asks[0].price).div(2);
}

function toOrderBookMetrics(orderBook: OrderBook) {
  const mid = midPrice(orderBook);
  const bestBid = orderBook.bids[0]?.price ?? mid;
  const bestAsk = orderBook.asks[0]?.price ?? mid;
  const spread = bestAsk - bestBid;
  const spreadPercent = mid > 0 ? spread / mid : 0;

  const bidDepth = orderBook.bids.reduce((sum, b) => sum + b.volume, 0);
  const askDepth = orderBook.asks.reduce((sum, a) => sum + a.volume, 0);
  const totalDepth = bidDepth + askDepth;
  const imbalance = totalDepth > 0 ? (bidDepth - askDepth) / totalDepth : 0;

  return {
    exchange: orderBook.exchange,
    symbol: orderBook.symbol,
    spread,
    spreadPercent,
    bidDepth,
    askDepth,
    imbalance,
    midPrice: mid,
    timestamp: orderBook.timestamp,
  };
}
