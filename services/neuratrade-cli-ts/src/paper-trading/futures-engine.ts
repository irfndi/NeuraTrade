import { Effect } from "effect";
import { randomUUID } from "node:crypto";
import type { ComposerConfig } from "../scalping/types.js";
import { composeSignal } from "../scalping/composer.js";
import {
  calculateAnnualizedVolatility,
  calculateATR,
} from "../scalping/indicators.js";
import { computeExitLevels } from "../scalping/exit-engine.js";
import { Decimal, money, toNumber } from "../utils/money.js";
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
import type { PaperPosition } from "./types.js";

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
}

export interface FuturesPaperTradingIterationResult {
  readonly action: "opened" | "closed" | "hold" | "scaled_out";
  readonly position: PaperPosition | null;
  readonly capital: number;
  readonly note: string;
}

function calculateFuturesNotionalValue(
  capital: number,
  entryPrice: number,
  stopDistancePct: number,
  currentVolatility: number,
  options: FuturesPaperTradingOptions,
): number {
  // Risk-based notional sizing: riskPerTradePct of capital / stopDistancePct.
  const maxNotionalByRiskCap =
    capital * ((options.maxPositionSizePct ?? 100) / 100);

  let baseNotional: number;
  if (
    options.riskPerTradePct &&
    options.riskPerTradePct > 0 &&
    stopDistancePct > 0
  ) {
    const riskAmount = capital * (options.riskPerTradePct / 100);
    baseNotional = riskAmount / stopDistancePct;
  } else {
    // Fallback to legacy margin allocation.
    const allocatedMargin = capital * (options.positionSizePct / 100);
    const marginPerContract = entryPrice / options.leverage;
    const feePerContract = entryPrice * (options.feePct / 100);
    const size = allocatedMargin / (marginPerContract + feePerContract);
    baseNotional = size * entryPrice;
  }

  if (
    options.volatilityTargetAnnualPct &&
    options.volatilityTargetAnnualPct > 0 &&
    currentVolatility > 0
  ) {
    baseNotional *= options.volatilityTargetAnnualPct / currentVolatility;
  }

  const maxNotionalByMargin = capital * options.leverage;
  return Math.min(baseNotional, maxNotionalByRiskCap, maxNotionalByMargin);
}

/**
 * Run a single futures paper-trading iteration.
 *
 * Sizing uses margin (capital * positionSizePct / 100) multiplied by leverage
 * to determine the notional position. The adapter enforces reduce-only closes
 * so the position cannot be accidentally doubled on exit.
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
> {
  return Effect.gen(function* () {
    const repo = yield* PaperTradingRepository;
    const gateway = yield* MarketDataGateway;
    const adapter = yield* FuturesExchangeAdapter;
    const killSwitch = yield* KillSwitch;
    const circuitBreaker = yield* CircuitBreaker;

    yield* repo.ensureTables();

    const portfolio = yield* repo.getPortfolio();
    let capital =
      portfolio.capital <= 0
        ? money(options.initialCapital)
        : money(portfolio.capital);
    let peakCapital = Decimal.max(money(portfolio.peakCapital), capital);

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

    const currentCandle = candles[candles.length - 1];
    const obMetrics = toOrderBookMetrics(orderBook);

    const signal = composeSignal(
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
      toNumber(capital),
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
        const closeFee = money(exitPrice)
          .times(scaleOut.trade.size)
          .times(options.feePct / 100);
        capital = capital.plus(money(scaleOut.trade.pnl)).minus(closeFee);
        peakCapital = Decimal.max(peakCapital, capital);
        yield* repo.setPortfolio(toNumber(capital), toNumber(peakCapital));
        position = scaleOut.updatedPosition;
        yield* repo.saveOpenPosition(position);

        return {
          action: "scaled_out" as const,
          position,
          capital: toNumber(capital),
          note: `SCALE-OUT ${scaleOut.trade.side} ${scaleOut.trade.entryPrice.toFixed(2)} → ${scaleOut.trade.exitPrice.toFixed(2)} size=${scaleOut.trade.size.toFixed(6)} | PnL ${scaleOut.trade.pnlPct.toFixed(2)}%`,
        };
      }

      if (exitReason) {
        const fill = yield* adapter.closePosition({
          symbol: options.symbol,
          side: position.side === "long" ? "sell" : "buy",
          productType: options.productType,
          marginMode: options.marginMode,
          leverage: options.leverage,
          size: position.size,
        });

        let exitPrice: number;
        let closeFee: Decimal;
        if (fill) {
          exitPrice = fill.filledPrice;
          closeFee = money(fill.fee);
        } else {
          exitPrice = fallbackExitPrice(position, currentCandle, exitReason);
          closeFee = money(exitPrice)
            .times(position.size)
            .times(options.feePct / 100);
        }

        const trade = yield* repo.closePosition(
          position,
          exitPrice,
          exitReason,
          currentCandle.timestamp,
        );

        capital = capital.plus(money(trade.pnl)).minus(closeFee);
        peakCapital = Decimal.max(peakCapital, capital);
        yield* repo.setPortfolio(toNumber(capital), toNumber(peakCapital));

        yield* circuitBreaker.recordTradeResult(trade.pnl, startOfDayCapital);

        return {
          action: "closed" as const,
          position: null,
          capital: toNumber(capital),
          note: `${trade.side} ${trade.entryPrice.toFixed(2)} → ${trade.exitPrice.toFixed(2)} | PnL ${trade.pnlPct.toFixed(2)}% | ${trade.exitReason}`,
        };
      }
    }

    // Open new position if signal is strong enough and no position.
    if (!position && signal && isEntrySignal(signal, options.minConfidence)) {
      const side = signal.direction === "buy" ? "long" : "short";

      // Pre-compute ATR and estimate stop distance from orderbook mid so we
      // can size the order by risk if requested.
      const needsAtr = options.useAtrStops || options.minAtrPct > 0;
      const atr = needsAtr ? calculateATR(candles, 14) : null;
      const useAtr = options.useAtrStops && atr !== null && atr > 0;
      const entryPriceNum = midPrice(orderBook);
      const entryPrice = money(entryPriceNum);

      let estimatedStopLoss: number;
      if (useAtr) {
        estimatedStopLoss =
          side === "long"
            ? entryPriceNum - atr * options.atrStopMultiplier
            : entryPriceNum + atr * options.atrStopMultiplier;
      } else {
        estimatedStopLoss =
          side === "long"
            ? entryPriceNum * (1 - options.stopLossPct / 100)
            : entryPriceNum * (1 + options.stopLossPct / 100);
      }
      const stopDistancePct =
        entryPriceNum > 0
          ? Math.abs(entryPriceNum - estimatedStopLoss) / entryPriceNum
          : 0;
      const currentVolatility = calculateAnnualizedVolatility(
        candles,
        options.volatilityLookback,
        options.timeframe,
      );

      const notionalValue = calculateFuturesNotionalValue(
        toNumber(capital),
        entryPriceNum,
        stopDistancePct,
        currentVolatility,
        options,
      );
      const size = notionalValue / entryPriceNum;

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

      const tradesTodayCount = yield* repo.countTradesForDate(new Date());
      const riskGuard = yield* RiskGuard;
      const riskCheck = yield* riskGuard
        .check({
          isLive: options.isLive,
          capital: toNumber(capital),
          peakCapital: toNumber(peakCapital),
          startOfDayCapital,
          dailyRealizedPnl: todayPnl,
          tradesTodayCount,
          positionValue: notionalValue,
          symbol: options.symbol,
          side: signal.direction as "buy" | "sell",
          leverage: options.leverage,
          productType: options.productType,
        })
        .pipe(Effect.either);

      if (riskCheck._tag === "Left") {
        yield* repo.setPortfolio(toNumber(capital), toNumber(peakCapital));
        return {
          action: "hold" as const,
          position,
          capital: toNumber(capital),
          note: `RISK BLOCKED: ${riskCheck.left.violations.join("; ")}`,
        };
      }

      yield* adapter.setLeverage(
        options.symbol,
        options.productType,
        options.marginMode,
        options.leverage,
      );
      yield* adapter.setMarginMode(
        options.symbol,
        options.productType,
        options.marginMode,
      );

      const fill = yield* adapter.placeOrder({
        symbol: options.symbol,
        side: signal.direction as "buy" | "sell",
        type: "market",
        size: Number(size.toFixed(8)),
        productType: options.productType,
        marginMode: options.marginMode,
        leverage: options.leverage,
      });

      const filledPrice = money(fill.filledPrice);
      const openFee = money(fill.fee);
      capital = capital.minus(openFee);

      if (options.minAtrPct > 0) {
        const entryPriceNum = toNumber(filledPrice, 8);
        const atrPct = atr && entryPriceNum > 0 ? atr / entryPriceNum : 0;
        if (atrPct < options.minAtrPct / 100) {
          return {
            action: "hold" as const,
            position,
            capital: toNumber(capital),
            note: `LOW VOLATILITY: atrPct=${(atrPct * 100).toFixed(3)}% < ${options.minAtrPct}%`,
          };
        }
      }

      const stopMult = options.atrStopMultiplier;
      const tpMult = options.atrTakeProfitMultiplier;
      const atrRiskReward =
        options.atrRiskReward > 0 ? options.atrRiskReward : tpMult / stopMult;
      const filledPriceNum = toNumber(filledPrice, 8);
      const {
        stopLoss: stopLossNum,
        takeProfit: takeProfitNum,
        scaleOutPrice,
      } = computeExitLevels({
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
        entryPrice: fill.filledPrice,
        size: fill.filledQty,
        stopLoss: toNumber(stopLoss, 8),
        takeProfit: toNumber(takeProfit, 8),
        openedAt: new Date(),
        signalId: signal.id,
        scaledOut: false,
        scaleOutPrice: scaleOutPrice ?? 0,
      };

      yield* repo.saveOpenPosition(newPosition);
      yield* repo.setPortfolio(toNumber(capital), toNumber(peakCapital));

      return {
        action: "opened" as const,
        position: newPosition,
        capital: toNumber(capital),
        note: `${side} ${fill.filledPrice.toFixed(2)} size=${fill.filledQty.toFixed(6)} leverage=${options.leverage}x SL=${newPosition.stopLoss.toFixed(2)} TP=${newPosition.takeProfit.toFixed(2)}`,
      };
    }

    yield* repo.setPortfolio(toNumber(capital), toNumber(peakCapital));
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
  signal: ReturnType<typeof composeSignal>,
  options: FuturesPaperTradingOptions,
): "stop_loss" | "take_profit" | "signal" | "scale_out" | null {
  if (!position.scaledOut && position.scaleOutPrice > 0) {
    if (position.side === "long") {
      if (candle.high >= position.scaleOutPrice) return "scale_out";
    } else {
      if (candle.low <= position.scaleOutPrice) return "scale_out";
    }
  }

  if (position.side === "long") {
    if (candle.low <= position.stopLoss) return "stop_loss";
    if (candle.high >= position.takeProfit) return "take_profit";
  } else {
    if (candle.high >= position.stopLoss) return "stop_loss";
    if (candle.low <= position.takeProfit) return "take_profit";
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
): number {
  if (reason === "stop_loss") {
    return position.side === "long"
      ? Math.min(candle.open, position.stopLoss)
      : Math.max(candle.open, position.stopLoss);
  }
  if (reason === "take_profit") {
    return position.side === "long"
      ? Math.max(candle.open, position.takeProfit)
      : Math.min(candle.open, position.takeProfit);
  }
  if (reason === "scale_out") {
    return position.side === "long"
      ? Math.max(candle.open, position.scaleOutPrice)
      : Math.min(candle.open, position.scaleOutPrice);
  }
  return candle.close;
}

function shouldExitPosition(
  position: PaperPosition,
  signal: NonNullable<ReturnType<typeof composeSignal>>,
): boolean {
  return (
    (position.side === "long" && signal.direction === "sell") ||
    (position.side === "short" && signal.direction === "buy")
  );
}

function isEntrySignal(
  signal: NonNullable<ReturnType<typeof composeSignal>>,
  minConfidence: number,
): boolean {
  return signal.direction !== "hold" && signal.confidence >= minConfidence;
}

function midPrice(orderBook: OrderBook): number {
  if (orderBook.bids.length === 0 || orderBook.asks.length === 0) {
    return orderBook.bids[0]?.price ?? orderBook.asks[0]?.price ?? 0;
  }
  return (orderBook.bids[0].price + orderBook.asks[0].price) / 2;
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
