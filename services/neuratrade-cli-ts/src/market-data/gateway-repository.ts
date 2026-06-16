import { Context, Effect, Layer } from "effect";
import { MarketDataError, MarketDataGateway } from "./gateway.js";
import { MarketDataRepository } from "./repository.js";
import type { Candle, OrderBook, Tick } from "./types.js";

function toMarketDataError(err: { readonly reason: string }): MarketDataError {
  return new MarketDataError(err.reason);
}

/**
 * Repository-backed market-data gateway for offline / simulated trading.
 *
 * Uses locally stored candles as the source of truth and synthesizes tick and
 * order-book snapshots from the most recent candle. This lets paper-trading
 * and soak loops run without live exchange connectivity, which is required for
 * reproducible CI and for backtest-to-paper continuity.
 */
export const MarketDataGatewayRepositoryLive = Layer.effect(
  MarketDataGateway,
  Effect.gen(function* () {
    const repo = yield* MarketDataRepository;

    const latestCandle = (
      exchange: string,
      symbol: string,
      timeframe: string,
    ): Effect.Effect<Candle, MarketDataError, never> =>
      Effect.gen(function* () {
        const candles = yield* repo
          .getCandles({
            exchange,
            symbol,
            timeframe,
            limit: 1,
          })
          .pipe(Effect.mapError(toMarketDataError));
        if (candles.length === 0) {
          return yield* Effect.fail(
            new MarketDataError(
              `No stored candles for ${exchange}:${symbol}:${timeframe}. Run 'market fetch-candles' first.`,
            ),
          );
        }
        return candles[candles.length - 1];
      });

    const fetchTick = (
      exchange: string,
      symbol: string,
    ): Effect.Effect<Tick, MarketDataError, never> =>
      Effect.gen(function* () {
        const candle = yield* latestCandle(exchange, symbol, "1h");
        return {
          exchange,
          symbol,
          price: candle.close,
          volume: candle.volume,
          timestamp: candle.timestamp,
        };
      });

    const fetchOHLCV = (
      exchange: string,
      symbol: string,
      timeframe: string,
      limit: number,
    ): Effect.Effect<readonly Candle[], MarketDataError, never> =>
      Effect.gen(function* () {
        const candles = yield* repo
          .getCandles({
            exchange,
            symbol,
            timeframe,
            limit,
          })
          .pipe(Effect.mapError(toMarketDataError));
        if (candles.length === 0) {
          return yield* Effect.fail(
            new MarketDataError(
              `No stored candles for ${exchange}:${symbol}:${timeframe}. Run 'market fetch-candles' first.`,
            ),
          );
        }
        return candles;
      });

    const fetchOrderBook = (
      exchange: string,
      symbol: string,
      limit: number,
    ): Effect.Effect<OrderBook, MarketDataError, never> =>
      Effect.gen(function* () {
        const candle = yield* latestCandle(exchange, symbol, "1h");
        const spread = candle.high - candle.low;
        const mid = candle.close;
        const halfSpread = spread > 0 ? spread / 2 : mid * 0.0005;
        const bid = mid - halfSpread;
        const ask = mid + halfSpread;
        const levelCount = Math.max(1, Math.min(limit, 20));
        const bids = Array.from({ length: levelCount }, (_, i) => ({
          price: bid * (1 - i * 0.0001),
          volume: candle.volume / levelCount,
        }));
        const asks = Array.from({ length: levelCount }, (_, i) => ({
          price: ask * (1 + i * 0.0001),
          volume: candle.volume / levelCount,
        }));
        return {
          exchange,
          symbol,
          bids,
          asks,
          timestamp: candle.timestamp,
        };
      });

    const fetchSymbols = (
      exchange: string,
    ): Effect.Effect<readonly string[], MarketDataError, never> =>
      repo
        .listSymbols(exchange, "1h", 1)
        .pipe(Effect.mapError(toMarketDataError));

    const fetch24hrVolumes = (
      _exchange: string,
    ): Effect.Effect<
      Readonly<Record<string, number>>,
      MarketDataError,
      never
    > => Effect.succeed({});

    return {
      fetchTick,
      fetchOHLCV,
      fetchOrderBook,
      fetchSymbols,
      fetch24hrVolumes,
    };
  }),
);
