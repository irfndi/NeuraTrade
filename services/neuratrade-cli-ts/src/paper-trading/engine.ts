import { Effect, Console } from "effect";
import { randomUUID } from "node:crypto";
import type { ComposerConfig } from "../scalping/types.js";
import { composeSignal } from "../scalping/composer.js";
import { calculateATR } from "../scalping/indicators.js";
import { MarketDataError, MarketDataGateway, type MarketDataGatewayService } from "../market-data/gateway.js";
import type { Candle, OrderBook } from "../market-data/types.js";
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
}

export interface PaperTradingIterationResult {
  readonly action: "opened" | "closed" | "hold";
  readonly position: PaperPosition | null;
  readonly capital: number;
  readonly note: string;
}

/**
 * Run a single paper-trading iteration: fetch market data, update or open
 * positions, persist state, and return a human-readable result.
 */
export function runPaperTradingIteration(
  options: PaperTradingOptions,
): Effect.Effect<
  PaperTradingIterationResult,
  MarketDataError | PaperTradingRepositoryError,
  MarketDataGatewayService | PaperTradingRepositoryService
> {
  return Effect.gen(function* () {
    const repo = yield* PaperTradingRepository;
    const gateway = yield* MarketDataGateway;

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
      const exit = checkExit(position, currentCandle, signal, options);
      if (exit) {
        const trade = yield* repo.closePosition(
          position,
          exit.price,
          exit.reason,
          currentCandle.timestamp,
        );
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
      const entryPrice = midPrice(orderBook);
      const positionValue = capital * (options.positionSizePct / 100);
      const size = positionValue / entryPrice;

      const atr = options.useAtrStops ? calculateATR(candles, 14) : null;
      const useAtr = options.useAtrStops && atr !== null && atr > 0;
      const side = signal.direction === "buy" ? "long" : "short";

      let stopLoss: number;
      let takeProfit: number;
      if (useAtr) {
        stopLoss =
          side === "long"
            ? entryPrice - atr * options.atrStopMultiplier
            : entryPrice + atr * options.atrStopMultiplier;
        takeProfit =
          side === "long"
            ? entryPrice + atr * options.atrTakeProfitMultiplier
            : entryPrice - atr * options.atrTakeProfitMultiplier;
      } else {
        // Fallback to a fixed 1.5% / 3.0% default if ATR stops disabled.
        stopLoss =
          side === "long" ? entryPrice * 0.985 : entryPrice * 1.015;
        takeProfit =
          side === "long" ? entryPrice * 1.03 : entryPrice * 0.97;
      }

      const newPosition: PaperPosition = {
        id: randomUUID(),
        exchange: options.exchange,
        symbol: options.symbol,
        timeframe: options.timeframe,
        side,
        entryPrice,
        size,
        stopLoss,
        takeProfit,
        openedAt: new Date(),
        signalId: signal.id,
      };

      capital -= entryPrice * size * (options.feePct / 100);
      yield* repo.saveOpenPosition(newPosition);
      yield* repo.setPortfolio(capital, peakCapital);

      return {
        action: "opened" as const,
        position: newPosition,
        capital,
        note: `${side} ${entryPrice.toFixed(2)} size=${size.toFixed(6)} SL=${stopLoss.toFixed(2)} TP=${takeProfit.toFixed(2)}`,
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

function checkExit(
  position: PaperPosition,
  candle: Candle,
  signal: ReturnType<typeof composeSignal>,
  options: PaperTradingOptions,
): { price: number; reason: "stop_loss" | "take_profit" | "signal" } | null {
  if (position.side === "long") {
    if (candle.low <= position.stopLoss) {
      return { price: Math.min(candle.open, position.stopLoss), reason: "stop_loss" };
    }
    if (candle.high >= position.takeProfit) {
      return { price: Math.max(candle.open, position.takeProfit), reason: "take_profit" };
    }
  } else {
    if (candle.high >= position.stopLoss) {
      return { price: Math.max(candle.open, position.stopLoss), reason: "stop_loss" };
    }
    if (candle.low <= position.takeProfit) {
      return { price: Math.min(candle.open, position.takeProfit), reason: "take_profit" };
    }
  }

  if (!options.holdUntilStop && signal && shouldExitPosition(position, signal)) {
    return { price: candle.close, reason: "signal" };
  }

  return null;
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
