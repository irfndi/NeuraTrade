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
import {
  ExchangeAdapter,
  ExchangeError,
  type ExchangeAdapterService,
} from "../exchange/adapter.js";
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
import type { PaperPosition } from "./types.js";

export interface PaperTradingOptions {
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
}

export interface PaperTradingIterationResult {
  readonly action: "opened" | "closed" | "hold" | "scaled_out";
  readonly position: PaperPosition | null;
  readonly capital: number;
  readonly note: string;
}

function calculatePaperPositionValue(
  capital: Decimal,
  entryPrice: Decimal,
  stopDistancePct: Money,
  currentVolatility: number,
  options: PaperTradingOptions,
): Decimal {
  const maxPositionValue = capital.times(
    (options.maxPositionSizePct ?? 100) / 100,
  );

  let positionValue: Decimal;
  if (
    options.riskPerTradePct &&
    options.riskPerTradePct > 0 &&
    stopDistancePct.greaterThan(0)
  ) {
    const riskAmount = capital.times(options.riskPerTradePct / 100);
    positionValue = riskAmount.div(stopDistancePct);
  } else {
    positionValue = capital.times(options.positionSizePct / 100);
  }

  if (
    options.volatilityTargetAnnualPct &&
    options.volatilityTargetAnnualPct > 0 &&
    currentVolatility > 0
  ) {
    positionValue = positionValue.times(
      options.volatilityTargetAnnualPct / currentVolatility,
    );
  }

  return Decimal.min(positionValue, maxPositionValue);
}

interface PaperIterationServices {
  readonly gateway: MarketDataGatewayService;
  readonly repo: PaperTradingRepositoryService;
  readonly adapter: ExchangeAdapterService;
  readonly killSwitch: KillSwitchService;
  readonly circuitBreaker: CircuitBreakerService;
  readonly riskGuard: RiskGuardService;
  readonly composer: SignalComposerImpl;
  readonly exitEngine: ExitEngineImpl;
}

type PaperIterationError =
  | MarketDataError
  | PaperTradingRepositoryError
  | ExchangeError
  | RiskError
  | KillSwitchError
  | CircuitBreakerError;

interface PaperIterationState {
  readonly capital: Decimal;
  readonly peakCapital: Decimal;
  readonly todayPnl: Decimal;
  readonly startOfDayCapital: Decimal;
}

interface PaperEntryPlan {
  readonly side: "long" | "short";
  readonly atr: number | null;
  readonly useAtr: boolean;
  readonly entryPrice: Money;
  readonly currentVolatility: number;
  readonly positionValue: Decimal;
  readonly size: Decimal;
}

interface PaperEntryPreparation {
  readonly plan?: PaperEntryPlan;
  readonly holdNote?: string;
}

function paperHoldResult(
  position: PaperPosition | null,
  capital: Decimal,
  note: string,
): PaperTradingIterationResult {
  return {
    action: "hold",
    position,
    capital: toNumber(capital),
    note,
  };
}

function preparePaperEntry(
  options: PaperTradingOptions,
  signal: ScalpingSignal,
  candles: readonly Candle[],
  orderBook: OrderBook,
  capital: Decimal,
): PaperEntryPreparation {
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
  const positionValue = calculatePaperPositionValue(
    capital,
    entryPrice,
    stopDistancePct,
    currentVolatility,
    options,
  );
  return {
    plan: {
      side,
      atr,
      useAtr,
      entryPrice,
      currentVolatility,
      positionValue,
      size: positionValue.div(entryPrice),
    },
  };
}

function buildPaperPosition(
  options: PaperTradingOptions,
  signal: ScalpingSignal,
  plan: PaperEntryPlan,
  fill: {
    readonly filledPrice: number;
    readonly filledQty: number;
  },
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
    scaledOut: false,
    scaleOutPrice: money(exitLevels.scaleOutPrice ?? 0),
  };
}

function handlePaperScaleOut(
  services: PaperIterationServices,
  options: PaperTradingOptions,
  position: PaperPosition,
  currentCandle: Candle,
  state: PaperIterationState,
): Effect.Effect<PaperTradingIterationResult, PaperIterationError> {
  return Effect.gen(function* () {
    const exitPrice = fallbackExitPrice(position, currentCandle, "scale_out");
    const scaleOut = yield* services.repo.scaleOutPosition(
      position,
      exitPrice,
      options.scaleOutPct,
      currentCandle.timestamp,
    );
    const exitFee = exitPrice
      .times(scaleOut.trade.size)
      .times(options.feePct / 100);
    const capital = state.capital.plus(scaleOut.trade.pnl).minus(exitFee);
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

function handlePaperClose(
  services: PaperIterationServices,
  options: PaperTradingOptions,
  position: PaperPosition,
  currentCandle: Candle,
  exitReason: "stop_loss" | "take_profit" | "signal",
  state: PaperIterationState,
): Effect.Effect<PaperTradingIterationResult, PaperIterationError> {
  return Effect.gen(function* () {
    const fill = yield* services.adapter
      .closePosition(options.symbol)
      .pipe(Effect.provideService(MarketDataGateway, services.gateway));
    const exitPrice = fill
      ? money(fill.filledPrice)
      : fallbackExitPrice(position, currentCandle, exitReason);
    const exitFee = fill
      ? money(fill.fee)
      : money(exitPrice)
          .times(position.size)
          .times(options.feePct / 100);
    const trade = yield* services.repo.closePosition(
      position,
      exitPrice,
      exitReason,
      currentCandle.timestamp,
    );
    const capital = state.capital.plus(trade.pnl).minus(exitFee);
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

function handlePaperExistingPosition(
  services: PaperIterationServices,
  options: PaperTradingOptions,
  position: PaperPosition,
  currentCandle: Candle,
  signal: ScalpingSignal | null,
  state: PaperIterationState,
): Effect.Effect<PaperTradingIterationResult | null, PaperIterationError> {
  return Effect.gen(function* () {
    const exitReason = checkExitReason(
      position,
      currentCandle,
      signal,
      options,
    );
    if (exitReason === "scale_out") {
      return yield* handlePaperScaleOut(
        services,
        options,
        position,
        currentCandle,
        state,
      );
    }
    if (exitReason === null) return null;
    return yield* handlePaperClose(
      services,
      options,
      position,
      currentCandle,
      exitReason,
      state,
    );
  });
}

function openPaperEntry(
  services: PaperIterationServices,
  options: PaperTradingOptions,
  signal: ScalpingSignal,
  candles: readonly Candle[],
  orderBook: OrderBook,
  state: PaperIterationState,
): Effect.Effect<PaperTradingIterationResult, PaperIterationError> {
  return Effect.gen(function* () {
    const preparation = preparePaperEntry(
      options,
      signal,
      candles,
      orderBook,
      state.capital,
    );
    if (preparation.holdNote !== undefined) {
      return paperHoldResult(null, state.capital, preparation.holdNote);
    }
    const plan = preparation.plan;
    if (plan === undefined) {
      return paperHoldResult(null, state.capital, "entry plan unavailable");
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
        positionValue: toNumber(plan.positionValue),
        symbol: options.symbol,
        side: signal.direction as "buy" | "sell",
      })
      .pipe(Effect.result);
    if (riskCheck._tag === "Failure") {
      yield* services.repo.setPortfolio(state.capital, state.peakCapital);
      return paperHoldResult(
        null,
        state.capital,
        `RISK BLOCKED: ${riskCheck.failure.violations.join("; ")}`,
      );
    }
    const fill = yield* services.adapter
      .placeOrder({
        symbol: options.symbol,
        side: signal.direction as "buy" | "sell",
        type: "market",
        quantity: toNumber(plan.size, 8),
      })
      .pipe(Effect.provideService(MarketDataGateway, services.gateway));
    const capital = state.capital.minus(money(fill.fee));
    const stopMult = options.atrStopMultiplier;
    const tpMult = options.atrTakeProfitMultiplier;
    const atrRiskReward =
      options.atrRiskReward > 0 ? options.atrRiskReward : tpMult / stopMult;
    const exitLevels = yield* services.exitEngine.computeExitLevels({
      side: plan.side,
      entryPrice: fill.filledPrice,
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
    const newPosition = buildPaperPosition(
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
      note: `${plan.side} ${fill.filledPrice.toFixed(2)} size=${fill.filledQty.toFixed(6)} SL=${newPosition.stopLoss.toFixed(2)} TP=${newPosition.takeProfit.toFixed(2)}`,
    };
  });
}

function processPaperMarket(
  services: PaperIterationServices,
  options: PaperTradingOptions,
  position: PaperPosition | null,
  candles: readonly Candle[],
  orderBook: OrderBook,
  capital: Decimal,
  peakCapital: Decimal,
): Effect.Effect<PaperTradingIterationResult, PaperIterationError> {
  return Effect.gen(function* () {
    const currentCandle = candles[candles.length - 1];
    const signal = yield* services.composer.composeSignal(
      {
        exchange: options.exchange,
        symbol: options.symbol,
        timeframe: options.timeframe,
        candles,
      },
      toOrderBookMetrics(orderBook),
      options.composerConfig,
    );
    const todayPnl = yield* services.repo.getTodayRealizedPnl();
    const startOfDayCapital = yield* services.repo.getStartOfDayCapital(
      new Date(),
      capital,
    );
    const state = { capital, peakCapital, todayPnl, startOfDayCapital };
    if (position) {
      const existingOutcome = yield* handlePaperExistingPosition(
        services,
        options,
        position,
        currentCandle,
        signal,
        state,
      );
      if (existingOutcome) return existingOutcome;
    }
    if (yield* services.killSwitch.isEngaged()) {
      const reason = yield* services.killSwitch.getReason();
      return paperHoldResult(
        position,
        capital,
        `KILL SWITCH ENGAGED: ${reason}`,
      );
    }
    if (yield* services.circuitBreaker.isOpen()) {
      const reason = yield* services.circuitBreaker.getReason();
      return paperHoldResult(
        position,
        capital,
        `CIRCUIT BREAKER OPEN: ${reason}`,
      );
    }
    if (capital.lessThanOrEqualTo(0)) {
      return paperHoldResult(
        position,
        capital,
        "capital exhausted (blown paper account)",
      );
    }
    if (!position && signal && isEntrySignal(signal, options.minConfidence)) {
      return yield* openPaperEntry(
        services,
        options,
        signal,
        candles,
        orderBook,
        state,
      );
    }
    yield* services.repo.setPortfolio(capital, peakCapital);
    return paperHoldResult(
      position,
      capital,
      signal
        ? `${signal.direction} (conf=${signal.confidence.toFixed(2)})`
        : "no signal",
    );
  });
}

/**
 * Run a single paper-trading iteration: fetch market data, generate a signal,
 * execute entry/exit through the exchange adapter, persist state, and return a
 * human-readable result.
 *
 * All capital, position-size, and PnL math uses Decimal.js internally. Numbers
 * are converted back only at persistence and exchange boundaries.
 */
export function runPaperTradingIteration(
  options: PaperTradingOptions,
): Effect.Effect<
  PaperTradingIterationResult,
  | MarketDataError
  | PaperTradingRepositoryError
  | ExchangeError
  | RiskError
  | KillSwitchError
  | CircuitBreakerError,
  | MarketDataGatewayService
  | PaperTradingRepositoryService
  | ExchangeAdapterService
  | RiskGuardService
  | KillSwitchService
  | CircuitBreakerService
  | SignalComposer
  | ExitEngine
> {
  return Effect.gen(function* () {
    const gateway = yield* MarketDataGateway;
    const services: PaperIterationServices = {
      gateway,
      repo: yield* PaperTradingRepository,
      adapter: yield* ExchangeAdapter,
      killSwitch: yield* KillSwitch,
      circuitBreaker: yield* CircuitBreaker,
      riskGuard: yield* RiskGuard,
      composer: yield* SignalComposer,
      exitEngine: yield* ExitEngine,
    };
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
      return paperHoldResult(position, capital, "insufficient candles");
    }

    if (orderBook.bids.length === 0 && orderBook.asks.length === 0) {
      return paperHoldResult(position, capital, "empty order book");
    }
    return yield* processPaperMarket(
      services,
      options,
      position,
      candles,
      orderBook,
      capital,
      peakCapital,
    );
  });
}

function checkExitReason(
  position: PaperPosition,
  candle: Candle,
  signal: ScalpingSignal | null,
  options: PaperTradingOptions,
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
