import { Effect, Layer, Ref } from "effect";
import { MarketDataError, MarketDataGateway } from "./gateway.js";
import { MarketDataRepository } from "./repository.js";
import type { Candle, FundingRate, OrderBook, Tick } from "./types.js";

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
    const lastTimeframeRef = yield* Ref.make<string>("1h");

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
        const timeframe = yield* Ref.get(lastTimeframeRef);
        const candle = yield* latestCandle(exchange, symbol, timeframe);
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
        yield* Ref.set(lastTimeframeRef, timeframe);
        return candles;
      });

    const fetchOrderBook = (
      exchange: string,
      symbol: string,
      limit: number,
    ): Effect.Effect<OrderBook, MarketDataError, never> =>
      Effect.gen(function* () {
        const timeframe = yield* Ref.get(lastTimeframeRef);
        const candle = yield* latestCandle(exchange, symbol, timeframe);
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

    const fetchDemoSymbols = (
      exchange: string,
    ): Effect.Effect<readonly string[], MarketDataError, never> =>
      fetchSymbols(exchange);

    const fetch24hrVolumes = (
      exchange: string,
    ): Effect.Effect<
      Readonly<Record<string, number>>,
      MarketDataError,
      never
    > =>
      Effect.gen(function* () {
        // Sum base-volume of stored candles over the last 24h per symbol.
        const timeframe = yield* Ref.get(lastTimeframeRef);
        const symbols = yield* repo
          .listSymbols(exchange, timeframe, 1)
          .pipe(Effect.mapError(toMarketDataError));
        const since = new Date(Date.now() - 24 * 60 * 60 * 1000);
        const volumes: Record<string, number> = {};
        for (const symbol of symbols) {
          const candles = yield* repo
            .getCandles({ exchange, symbol, timeframe, from: since })
            .pipe(Effect.mapError(toMarketDataError));
          volumes[symbol] = candles.reduce((sum, c) => sum + c.volume, 0);
        }
        return volumes;
      });

    const fetchFundingRates = (
      exchange: string,
      symbol: string,
      startTime?: Date,
      endTime?: Date,
      limit?: number,
    ): Effect.Effect<readonly FundingRate[], MarketDataError, never> =>
      repo
        .getFundingRates(exchange, symbol, startTime, endTime)
        .pipe(Effect.map((rates) => (limit ? rates.slice(-limit) : rates)))
        .pipe(Effect.mapError(toMarketDataError));

    return {
      fetchTick,
      fetchOHLCV,
      fetchOrderBook,
      fetchSymbols,
      fetchDemoSymbols,
      fetch24hrVolumes,
      fetchFundingRates,
    };
  }),
);
