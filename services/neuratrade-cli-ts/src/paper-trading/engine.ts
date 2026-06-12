import { Effect, Either } from "effect";
import { randomUUID } from "node:crypto";
import type { ComposerConfig } from "../scalping/types.js";
import { composeSignal } from "../scalping/composer.js";
import { calculateATR } from "../scalping/indicators.js";
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
import {
  RiskError,
  RiskGuard,
  type RiskGuardService,
} from "../risk/guards.js";
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
  readonly feePct: number;
  readonly minConfidence: number;
  readonly useAtrStops: boolean;
  readonly atrStopMultiplier: number;
  readonly atrTakeProfitMultiplier: number;
  readonly holdUntilStop: boolean;
  readonly initialCapital: number;
  readonly isLive: boolean;
}

export interface PaperTradingIterationResult {
  readonly action: "opened" | "closed" | "hold";
  readonly position: PaperPosition | null;
  readonly capital: number;
  readonly note: string;
}

/**
 * Run a single paper-trading iteration: fetch market data, generate a signal,
 * execute entry/exit through the exchange adapter, persist state, and return a
 * human-readable result.
 */
export function runPaperTradingIteration(
  options: PaperTradingOptions,
): Effect.Effect<
  PaperTradingIterationResult,
  MarketDataError | PaperTradingRepositoryError | ExchangeError | RiskError,
  MarketDataGatewayService | PaperTradingRepositoryService | ExchangeAdapterService | RiskGuardService
> {
  return Effect.gen(function* () {
    const repo = yield* PaperTradingRepository;
    const gateway = yield* MarketDataGateway;
    const adapter = yield* ExchangeAdapter;

    yield* repo.ensureTables();

    const portfolio = yield* repo.getPortfolio();
    let capital = portfolio.capital <= 0 ? options.initialCapital : portfolio.capital;
    let peakCapital = Math.max(portfolio.peakCapital, capital);

    let position = yield* repo.getOpenPosition(options.exchange, options.symbol);

    const candles = yield* gateway.fetchOHLCV(
      options.exchange,
      options.symbol,
      options.timeframe,
      100,
    );
    const orderBook = yield* gateway.fetchOrderBook(options.exchange, options.symbol, 20);

    if (candles.length < 30) {
      return { action: "hold" as const, position, capital, note: "insufficient candles" };
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

    // Exit existing position first.
    if (position) {
      const exitReason = checkExitReason(position, currentCandle, signal, options);
      if (exitReason) {
        const fill = yield* adapter.closePosition(options.symbol);

        let exitPrice: number;
        if (fill) {
          exitPrice = fill.filledPrice;
          capital -= fill.fee;
        } else {
          exitPrice = fallbackExitPrice(position, currentCandle, exitReason);
        }

        const trade = yield* repo.closePosition(position, exitPrice, exitReason, currentCandle.timestamp);
        capital += trade.pnl - position.entryPrice * position.size * (options.feePct / 100);
        if (capital > peakCapital) peakCapital = capital;
        yield* repo.setPortfolio(capital, peakCapital);

        return {
          action: "closed" as const,
          position: null,
          capital,
          note: `${trade.side} ${trade.entryPrice.toFixed(2)} → ${trade.exitPrice.toFixed(2)} | PnL ${trade.pnlPct.toFixed(2)}% | ${trade.exitReason}`,
        };
      }
    }

    // Open new position if signal is strong enough and no position.
    if (!position && signal && isEntrySignal(signal, options.minConfidence)) {
      const side = signal.direction === "buy" ? "long" : "short";
      const positionValue = capital * (options.positionSizePct / 100);

      // Pre-compute size from orderbook mid to size the order.
      const entryPrice = midPrice(orderBook);
      const size = positionValue / entryPrice;

      // Pre-trade risk gate.
      const todayPnl = yield* repo.getTodayRealizedPnl();
      const tradesTodayCount = yield* repo.countTradesForDate(new Date());
      const startOfDayCapital = capital - todayPnl;
      const riskGuard = yield* RiskGuard;
      const riskCheck = yield* riskGuard
        .check({
          isLive: options.isLive,
          capital,
          peakCapital,
          startOfDayCapital,
          dailyRealizedPnl: todayPnl,
          tradesTodayCount,
          positionValue,
          symbol: options.symbol,
          side: signal.direction as "buy" | "sell",
        })
        .pipe(Effect.either);

      if (riskCheck._tag === "Left") {
        yield* repo.setPortfolio(capital, peakCapital);
        return {
          action: "hold" as const,
          position,
          capital,
          note: `RISK BLOCKED: ${riskCheck.left.violations.join("; ")}`,
        };
      }

      const fill = yield* adapter.placeOrder({
        symbol: options.symbol,
        side: signal.direction as "buy" | "sell",
        type: "market",
        quantity: size,
      });

      const filledPrice = fill.filledPrice;
      const filledSize = fill.filledQty;
      capital -= filledPrice * filledSize * (options.feePct / 100) + fill.fee;

      const atr = options.useAtrStops ? calculateATR(candles, 14) : null;
      const useAtr = options.useAtrStops && atr !== null && atr > 0;

      let stopLoss: number;
      let takeProfit: number;
      if (useAtr) {
        stopLoss =
          side === "long"
            ? filledPrice - atr * options.atrStopMultiplier
            : filledPrice + atr * options.atrStopMultiplier;
        takeProfit =
          side === "long"
            ? filledPrice + atr * options.atrTakeProfitMultiplier
            : filledPrice - atr * options.atrTakeProfitMultiplier;
      } else {
        stopLoss = side === "long" ? filledPrice * 0.985 : filledPrice * 1.015;
        takeProfit = side === "long" ? filledPrice * 1.03 : filledPrice * 0.97;
      }

      const newPosition: PaperPosition = {
        id: randomUUID(),
        exchange: options.exchange,
        symbol: options.symbol,
        timeframe: options.timeframe,
        side,
        entryPrice: filledPrice,
        size: filledSize,
        stopLoss,
        takeProfit,
        openedAt: new Date(),
        signalId: signal.id,
      };

      yield* repo.saveOpenPosition(newPosition);
      yield* repo.setPortfolio(capital, peakCapital);

      return {
        action: "opened" as const,
        position: newPosition,
        capital,
        note: `${side} ${filledPrice.toFixed(2)} size=${filledSize.toFixed(6)} SL=${stopLoss.toFixed(2)} TP=${takeProfit.toFixed(2)}`,
      };
    }

    yield* repo.setPortfolio(capital, peakCapital);
    return {
      action: "hold" as const,
      position,
      capital,
      note: signal ? `${signal.direction} (conf=${signal.confidence.toFixed(2)})` : "no signal",
    };
  });
}

function checkExitReason(
  position: PaperPosition,
  candle: Candle,
  signal: ReturnType<typeof composeSignal>,
  options: PaperTradingOptions,
): "stop_loss" | "take_profit" | "signal" | null {
  if (position.side === "long") {
    if (candle.low <= position.stopLoss) return "stop_loss";
    if (candle.high >= position.takeProfit) return "take_profit";
  } else {
    if (candle.high >= position.stopLoss) return "stop_loss";
    if (candle.low <= position.takeProfit) return "take_profit";
  }

  if (!options.holdUntilStop && signal && shouldExitPosition(position, signal)) {
    return "signal";
  }

  return null;
}

function fallbackExitPrice(
  position: PaperPosition,
  candle: Candle,
  reason: "stop_loss" | "take_profit" | "signal",
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

function isEntrySignal(signal: NonNullable<ReturnType<typeof composeSignal>>, minConfidence: number): boolean {
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
