import { Effect, Layer, Ref } from "effect";
import { MarketDataError, MarketDataGateway } from "./gateway.js";
import { MarketDataRepository } from "./repository.js";
import type { Candle, FundingRate, OrderBook, Tick } from "./types.js";

function toMarketDataError(err: { readonly reason: string }): MarketDataError {
  return new MarketDataError(err.reason);
}

/** Timeframe used when no fetchOHLCV for that exchange|symbol has been seen
 *  in this gateway instance. */
const DEFAULT_TIMEFRAME = "1h";

/** Read the per-symbol last-seen timeframe; independent per symbol, so a
 *  concurrent fetchOHLCV on another symbol/timeframe cannot skew these
 *  reads. */
function findLastTimeframe(
  ref: Ref.Ref<Map<string, string>>,
  exchange: string,
  symbol: string,
): Effect.Effect<string> {
  return Effect.map(
    Ref.get(ref),
    (map) => map.get(`${exchange}|${symbol}`) ?? DEFAULT_TIMEFRAME,
  );
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
    // Last timeframe seen per exchange|symbol (NOT a single global): a
    // global mutable timeframe made concurrent tick/orderbook reads for one
    // symbol resolve against whatever timeframe another symbol's fetchOHLCV
    // had just written.
    const lastTimeframesRef = yield* Ref.make(new Map<string, string>());

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
        const timeframe = yield* findLastTimeframe(
          lastTimeframesRef,
          exchange,
          symbol,
        );
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
        yield* Ref.update(lastTimeframesRef, (map) =>
          new Map(map).set(`${exchange}|${symbol}`, timeframe),
        );
        return candles;
      });

    const fetchOrderBook = (
      exchange: string,
      symbol: string,
      limit: number,
    ): Effect.Effect<OrderBook, MarketDataError, never> =>
      Effect.gen(function* () {
        const timeframe = yield* findLastTimeframe(
          lastTimeframesRef,
          exchange,
          symbol,
        );
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
        // Each symbol is read at its own last-seen timeframe (falling back to
        // the default), never a shared mutable one.
        const symbols = yield* repo
          .listSymbols(exchange, DEFAULT_TIMEFRAME, 1)
          .pipe(Effect.mapError(toMarketDataError));
        const since = new Date(Date.now() - 24 * 60 * 60 * 1000);
        const volumes: Record<string, number> = {};
        for (const symbol of symbols) {
          const timeframe = yield* findLastTimeframe(
            lastTimeframesRef,
            exchange,
            symbol,
          );
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
