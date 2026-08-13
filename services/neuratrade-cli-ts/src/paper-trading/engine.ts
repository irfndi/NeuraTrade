import { Effect } from "effect";
import { randomUUID } from "node:crypto";
import type { ComposerConfig, ScalpingSignal } from "../scalping/types.js";
import {
  calculateAnnualizedVolatility,
  calculateATR,
} from "../scalping/indicators.js";
import { ExitEngine, SignalComposer } from "../scalping/services.js";
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
    const repo = yield* PaperTradingRepository;
    const gateway = yield* MarketDataGateway;
    const adapter = yield* ExchangeAdapter;
    const killSwitch = yield* KillSwitch;
    const circuitBreaker = yield* CircuitBreaker;
    const composer = yield* SignalComposer;
    const exitEngine = yield* ExitEngine;

    yield* repo.ensureTables();

    const portfolio = yield* repo.getPortfolio();
    // Seed initial capital only for a FRESH portfolio (no row persisted yet:
    // the repository reports capital 0/peak 0). A blown account — persisted
    // capital <= 0 with a positive peak — is a terminal state and must NOT
    // be silently resurrected, or soak/drawdown results are falsified.
    const isFreshPortfolio =
      portfolio.capital.lessThanOrEqualTo(0) &&
      portfolio.peakCapital.lessThanOrEqualTo(0);
    let capital = isFreshPortfolio
      ? money(options.initialCapital)
      : portfolio.capital;
    let peakCapital = Decimal.max(portfolio.peakCapital, capital);

    let position = yield* repo.getOpenPosition(
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
      return {
        action: "hold" as const,
        position,
        capital: toNumber(capital),
        note: "insufficient candles",
      };
    }

    if (orderBook.bids.length === 0 && orderBook.asks.length === 0) {
      return {
        action: "hold" as const,
        position,
        capital: toNumber(capital),
        note: "empty order book",
      };
    }

    const currentCandle = candles[candles.length - 1];
    const obMetrics = toOrderBookMetrics(orderBook);

    const signal = yield* composer.composeSignal(
      {
        exchange: options.exchange,
        symbol: options.symbol,
        timeframe: options.timeframe,
        candles,
      },
      obMetrics,
      options.composerConfig,
    );

    const todayPnl = yield* repo.getTodayRealizedPnl();
    const startOfDayCapital = yield* repo.getStartOfDayCapital(
      new Date(),
      capital,
    );

    // Exit existing position first.
    if (position) {
      const exitReason = checkExitReason(
        position,
        currentCandle,
        signal,
        options,
      );
      if (exitReason === "scale_out") {
        const exitPrice = fallbackExitPrice(
          position,
          currentCandle,
          exitReason,
        );
        const scaleOut = yield* repo.scaleOutPosition(
          position,
          exitPrice,
          options.scaleOutPct,
          currentCandle.timestamp,
        );
        const exitFee = exitPrice
          .times(scaleOut.trade.size)
          .times(options.feePct / 100);
        capital = capital.plus(scaleOut.trade.pnl).minus(exitFee);
        peakCapital = Decimal.max(peakCapital, capital);
        yield* repo.setPortfolio(capital, peakCapital);
        position = scaleOut.updatedPosition;
        yield* repo.saveOpenPosition(position);
        yield* circuitBreaker.recordTradeResult(
          toNumber(scaleOut.trade.pnl),
          toNumber(startOfDayCapital),
        );

        return {
          action: "scaled_out" as const,
          position,
          capital: toNumber(capital),
          note: `SCALE-OUT ${scaleOut.trade.side} ${scaleOut.trade.entryPrice.toFixed(2)} → ${scaleOut.trade.exitPrice.toFixed(2)} size=${scaleOut.trade.size.toFixed(6)} | PnL ${scaleOut.trade.pnlPct.toFixed(2)}%`,
        };
      }

      if (exitReason) {
        const fill = yield* adapter.closePosition(options.symbol);

        let exitPrice: Decimal;
        let exitFee: Decimal;
        if (fill) {
          exitPrice = money(fill.filledPrice);
          exitFee = money(fill.fee);
        } else {
          exitPrice = fallbackExitPrice(position, currentCandle, exitReason);
          exitFee = money(exitPrice)
            .times(position.size)
            .times(options.feePct / 100);
        }

        const trade = yield* repo.closePosition(
          position,
          exitPrice,
          exitReason,
          currentCandle.timestamp,
        );
        capital = capital.plus(trade.pnl).minus(exitFee);
        peakCapital = Decimal.max(peakCapital, capital);
        yield* repo.setPortfolio(capital, peakCapital);

        yield* circuitBreaker.recordTradeResult(
          toNumber(trade.pnl),
          toNumber(startOfDayCapital),
        );

        return {
          action: "closed" as const,
          position: null,
          capital: toNumber(capital),
          note: `${trade.side} ${trade.entryPrice.toFixed(2)} → ${trade.exitPrice.toFixed(2)} | PnL ${trade.pnlPct.toFixed(2)}% | ${trade.exitReason}`,
        };
      }
    }

    if (yield* killSwitch.isEngaged()) {
      const reason = yield* killSwitch.getReason();
      return {
        action: "hold" as const,
        position,
        capital: toNumber(capital),
        note: `KILL SWITCH ENGAGED: ${reason}`,
      };
    }

    if (yield* circuitBreaker.isOpen()) {
      const reason = yield* circuitBreaker.getReason();
      return {
        action: "hold" as const,
        position,
        capital: toNumber(capital),
        note: `CIRCUIT BREAKER OPEN: ${reason}`,
      };
    }

    if (capital.lessThanOrEqualTo(0)) {
      // Blown paper account: capital is exhausted. Exits above may still
      // recover it; entry is terminal until the position closes with a win.
      return {
        action: "hold" as const,
        position,
        capital: toNumber(capital),
        note: "capital exhausted (blown paper account)",
      };
    }

    // Open new position if signal is strong enough and no position.
    if (!position && signal && isEntrySignal(signal, options.minConfidence)) {
      const side = signal.direction === "buy" ? "long" : "short";

      // Pre-compute ATR and estimate stop distance from orderbook mid so we
      // can size the order by risk if requested.
      const needsAtr = options.useAtrStops || options.minAtrPct > 0;
      const atr = needsAtr ? calculateATR(candles, 14) : null;
      const useAtr = options.useAtrStops && atr !== null && atr > 0;
      const entryPrice = midPriceMoney(orderBook);

      let estimatedStopLoss: Money;
      if (useAtr) {
        const stopDistance = money(atr).times(options.atrStopMultiplier);
        estimatedStopLoss =
          side === "long"
            ? entryPrice.minus(stopDistance)
            : entryPrice.plus(stopDistance);
      } else {
        estimatedStopLoss =
          side === "long"
            ? entryPrice.times(1 - options.stopLossPct / 100)
            : entryPrice.times(1 + options.stopLossPct / 100);
      }
      const stopDistancePct = entryPrice.greaterThan(0)
        ? entryPrice.minus(estimatedStopLoss).abs().div(entryPrice)
        : money(0);
      const currentVolatility = calculateAnnualizedVolatility(
        candles,
        options.volatilityLookback,
        options.timeframe,
      );

      // Volatility gate BEFORE any order placement: a below-threshold entry
      // must not place an exchange order (leaving an untracked live position
      // and a phantom fee deduction). Mirrors futures-engine.ts.
      if (options.minAtrPct > 0) {
        const atrPct =
          atr && entryPrice.greaterThan(0)
            ? money(atr).div(entryPrice).toNumber()
            : 0;
        if (atrPct < options.minAtrPct / 100) {
          return {
            action: "hold" as const,
            position,
            capital: toNumber(capital),
            note: `LOW VOLATILITY: atrPct=${(atrPct * 100).toFixed(3)}% < ${options.minAtrPct}%`,
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
      const size = positionValue.div(entryPrice);

      // Pre-trade risk gate.
      const tradesTodayCount = yield* repo.countTradesForDate(new Date());
      const riskGuard = yield* RiskGuard;
      const riskCheck = yield* riskGuard
        .check({
          isLive: options.isLive,
          capital: toNumber(capital),
          peakCapital: toNumber(peakCapital),
          startOfDayCapital: toNumber(startOfDayCapital),
          dailyRealizedPnl: toNumber(todayPnl),
          tradesTodayCount,
          positionValue: toNumber(positionValue),
          symbol: options.symbol,
          side: signal.direction as "buy" | "sell",
        })
        .pipe(Effect.result);

      if (riskCheck._tag === "Failure") {
        yield* repo.setPortfolio(capital, peakCapital);
        return {
          action: "hold" as const,
          position,
          capital: toNumber(capital),
          note: `RISK BLOCKED: ${riskCheck.failure.violations.join("; ")}`,
        };
      }

      const fill = yield* adapter.placeOrder({
        symbol: options.symbol,
        side: signal.direction as "buy" | "sell",
        type: "market",
        quantity: toNumber(size, 8),
      });

      const filledPrice = money(fill.filledPrice);
      const entryFee = money(fill.fee);
      capital = capital.minus(entryFee);

      const stopMult = options.atrStopMultiplier;
      const tpMult = options.atrTakeProfitMultiplier;
      const atrRiskReward =
        options.atrRiskReward > 0 ? options.atrRiskReward : tpMult / stopMult;
      const filledPriceNum = toNumber(filledPrice, 8);
      const {
        stopLoss: stopLossNum,
        takeProfit: takeProfitNum,
        scaleOutPrice,
      } = yield* exitEngine.computeExitLevels({
        side,
        entryPrice: filledPriceNum,
        atr,
        useAtr,
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
      const stopLoss = money(stopLossNum);
      const takeProfit = money(takeProfitNum);

      const newPosition: PaperPosition = {
        id: randomUUID(),
        exchange: options.exchange,
        symbol: options.symbol,
        timeframe: options.timeframe,
        side,
        entryPrice: filledPrice,
        size: money(fill.filledQty),
        stopLoss,
        takeProfit,
        openedAt: new Date(),
        signalId: signal.id,
        scaledOut: false,
        scaleOutPrice: money(scaleOutPrice ?? 0),
      };

      yield* repo.saveOpenPosition(newPosition);
      yield* repo.setPortfolio(capital, peakCapital);

      return {
        action: "opened" as const,
        position: newPosition,
        capital: toNumber(capital),
        note: `${side} ${fill.filledPrice.toFixed(2)} size=${fill.filledQty.toFixed(6)} SL=${newPosition.stopLoss.toFixed(2)} TP=${newPosition.takeProfit.toFixed(2)}`,
      };
    }

    yield* repo.setPortfolio(capital, peakCapital);
    return {
      action: "hold" as const,
      position,
      capital: toNumber(capital),
      note: signal
        ? `${signal.direction} (conf=${signal.confidence.toFixed(2)})`
        : "no signal",
    };
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
