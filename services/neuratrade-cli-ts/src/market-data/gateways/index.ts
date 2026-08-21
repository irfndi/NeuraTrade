import { Effect, Layer, Ref } from "effect";
import type { Candle } from "../types.js";
import type { MarketDataGatewayService } from "../gateway.js";
import { MarketDataError, MarketDataGateway } from "../gateway.js";
import * as Binance from "./binance.js";
import * as Bitget from "./bitget.js";
import * as Bybit from "./bybit.js";

/**
 * Composite gateway that routes by exchange name.
 *
 * Currently supports:
 *   - binance
 *   - bitget (spot)
 *   - bitget-futures
 *   - bybit-futures (testnet)
 *
 * Additional exchanges can be added here without changing consumers.
 */

// ---------------------------------------------------------------------------
// Shared request pacing (token bucket)
// ---------------------------------------------------------------------------

/**
 * Public market-data pacing shared by EVERY gateway call. The per-exchange
 * modules have no limiter of their own; without this the 16-way concurrent
 * sweep in `scalp paper-trade` fires unthrottled request bursts (observed
 * HTTP 429). Conservative defaults sit well under the strictest venue limit
 * (Bybit public ~10/s; Bitget public 20/s).
 */
const PACING_PER_SECOND = 8;
const PACING_PER_MINUTE = 400;

interface Bucket {
  readonly tokens: number;
  readonly lastUpdated: number;
}

interface PacingState {
  readonly second: Bucket;
  readonly minute: Bucket;
}

const makePacing = Effect.gen(function* () {
  const stateRef = yield* Ref.make<PacingState>({
    second: { tokens: PACING_PER_SECOND, lastUpdated: Date.now() },
    minute: { tokens: PACING_PER_MINUTE, lastUpdated: Date.now() },
  });
  const refill = (
    bucket: Bucket,
    now: number,
    capacity: number,
    perMs: number,
  ): Bucket => ({
    tokens: Math.min(
      capacity,
      bucket.tokens + (now - bucket.lastUpdated) * perMs,
    ),
    lastUpdated: now,
  });
  const deficitMs = (
    bucket: Bucket,
    cost: number,
    capacity: number,
    perMs: number,
  ): number =>
    bucket.tokens >= cost ? 0 : Math.ceil((cost - bucket.tokens) / perMs);
  const acquire = (n: number): Effect.Effect<void> =>
    Effect.gen(function* () {
      type ModifyResult = readonly [readonly [boolean, number], PacingState];
      for (;;) {
        const now = Date.now();
        const [ok, sleepMs] = yield* Ref.modify(
          stateRef,
          (state): ModifyResult => {
            const s = refill(
              state.second,
              now,
              PACING_PER_SECOND,
              PACING_PER_SECOND / 1000,
            );
            const m = refill(
              state.minute,
              now,
              PACING_PER_MINUTE,
              PACING_PER_MINUTE / 60000,
            );
            if (s.tokens >= n && m.tokens >= n) {
              return [
                [true, 0],
                {
                  second: { ...s, tokens: s.tokens - n },
                  minute: { ...m, tokens: m.tokens - n },
                },
              ];
            }
            const wait = Math.max(
              deficitMs(s, n, PACING_PER_SECOND, PACING_PER_SECOND / 1000),
              deficitMs(m, n, PACING_PER_MINUTE, PACING_PER_MINUTE / 60000),
              1,
            );
            return [[false, wait], { second: s, minute: m }];
          },
        );
        if (ok) return;
        yield* Effect.sleep(`${sleepMs} millis`);
      }
    });
  return { acquire };
});

// ---------------------------------------------------------------------------
// Bar-boundary candle cache
// ---------------------------------------------------------------------------

/**
 * Candle cache keyed `exchange|symbol|timeframe`. A poll loop that runs more
 * often than the bar interval re-downloads identical candles ~14 of every 15
 * polls on a 15m timeframe. The cache serves the SAME array until the newest
 * bar's open time advances (the bar rolled), so a mid-bar poll costs zero
 * requests and a settled bar is never served stale. Entries expire after
 * TTL_MS so a halted venue eventually re-fetches even if its newest bar
 * timestamp somehow never advances.
 */
interface CandleCacheEntry {
  readonly candles: readonly Candle[];
  readonly newestBarOpenMs: number;
  readonly filledAtMs: number;
}

const CANDLE_TTL_MS = 5 * 60_000;

const makeCandleCache = Effect.gen(function* () {
  const cacheRef = yield* Ref.make(new Map<string, CandleCacheEntry>());
  return { cacheRef };
});

// ---------------------------------------------------------------------------
// Live layer
// ---------------------------------------------------------------------------

export const MarketDataGatewayLive = Layer.effect(
  MarketDataGateway,
  Effect.gen(function* () {
    const { acquire } = yield* makePacing;
    const { cacheRef } = yield* makeCandleCache;

    /** Route to the per-exchange module (pacing applied by the caller). */
    const routeOHLCV = (
      exchange: string,
      symbol: string,
      timeframe: string,
      limit: number,
      startTime?: Date,
    ): Effect.Effect<readonly Candle[], MarketDataError> => {
      if (exchange.toLowerCase() === "bitget") {
        return Bitget.fetchOHLCV(symbol, timeframe, limit, startTime, "spot");
      }
      if (exchange.toLowerCase() === "bitget-futures") {
        return Bitget.fetchOHLCV(
          symbol,
          timeframe,
          limit,
          startTime,
          "futures",
        );
      }
      if (exchange.toLowerCase() === "bybit-futures") {
        return Bybit.fetchOHLCV(symbol, timeframe, limit, startTime);
      }
      return Binance.fetchOHLCV(symbol, timeframe, limit, startTime);
    };

    /** Validate + spend a pacing token, then run the request. */
    const paced = <A>(
      exchange: string,
      operation: string,
      run: () => Effect.Effect<A, MarketDataError>,
    ): Effect.Effect<A, MarketDataError> =>
      Effect.suspend(() => {
        const supported = [
          "binance",
          "bitget",
          "bitget-futures",
          "bybit-futures",
        ];
        if (!supported.includes(exchange.toLowerCase())) {
          return Effect.fail(
            new MarketDataError(
              `Exchange "${exchange}" is not supported by the market-data gateway (operation: ${operation})`,
            ),
          );
        }
        return Effect.andThen(acquire(1), run);
      });

    return {
      fetchTick: (exchange, symbol) =>
        paced(exchange, "fetchTick", () => {
          if (exchange.toLowerCase() === "bitget") {
            return Bitget.fetchTick(symbol, "spot");
          }
          if (exchange.toLowerCase() === "bitget-futures") {
            return Bitget.fetchTick(symbol, "futures");
          }
          if (exchange.toLowerCase() === "bybit-futures") {
            return Bybit.fetchTick(symbol);
          }
          return Binance.fetchTick(symbol);
        }),

      fetchOHLCV: (exchange, symbol, timeframe, limit, startTime) =>
        Effect.gen(function* () {
          // Paged/backfilled calls carry an explicit cursor — never cached.
          if (startTime !== undefined) {
            return yield* paced(exchange, "fetchOHLCV", () =>
              routeOHLCV(exchange, symbol, timeframe, limit, startTime),
            );
          }
          const key = `${exchange.toLowerCase()}|${symbol}|${timeframe}`;
          const nowMs = Date.now();
          const hit = (yield* Ref.get(cacheRef)).get(key);
          if (
            hit !== undefined &&
            nowMs - hit.filledAtMs < CANDLE_TTL_MS &&
            // The newest bar in the cached window is still OPEN (its close
            // time is in the future): settled bars cannot change, so serve
            // them until the bar rolls.
            hit.newestBarOpenMs + BAR_ROLL_GRACE_MS > nowMs
          ) {
            return hit.candles;
          }
          const candles = yield* paced(exchange, "fetchOHLCV", () =>
            routeOHLCV(exchange, symbol, timeframe, limit),
          );
          const newestBarOpenMs =
            candles.length > 0
              ? candles[candles.length - 1].timestamp.getTime()
              : 0;
          yield* Ref.update(cacheRef, (map) => {
            map.set(key, { candles, newestBarOpenMs, filledAtMs: nowMs });
            return map;
          });
          return candles;
        }),

      fetchOrderBook: (exchange, symbol, limit) =>
        paced(exchange, "fetchOrderBook", () => {
          if (exchange.toLowerCase() === "bitget") {
            return Bitget.fetchOrderBook(symbol, limit, "spot");
          }
          if (exchange.toLowerCase() === "bitget-futures") {
            return Bitget.fetchOrderBook(symbol, limit, "futures");
          }
          if (exchange.toLowerCase() === "bybit-futures") {
            return Bybit.fetchOrderBook(symbol, limit);
          }
          return Binance.fetchOrderBook(symbol, limit);
        }),

      fetchSymbols: (exchange) =>
        paced(exchange, "fetchSymbols", () => {
          if (exchange.toLowerCase() === "bitget") {
            return Bitget.fetchSymbols("spot");
          }
          if (exchange.toLowerCase() === "bitget-futures") {
            return Bitget.fetchSymbols("futures");
          }
          if (exchange.toLowerCase() === "bybit-futures") {
            return Bybit.fetchSymbols();
          }
          return Binance.fetchSymbols();
        }),

      fetchDemoSymbols: (exchange) =>
        paced(exchange, "fetchDemoSymbols", () => {
          // Bitget futures has a simulated (PAPTRADING) environment; Bybit
          // testnet IS the demo (same tradeable list). The other gateways have
          // no demo concept and report their full list (a no-op filter for the
          // universe bound).
          if (exchange.toLowerCase() === "bitget-futures") {
            return Bitget.fetchDemoSymbols();
          }
          if (exchange.toLowerCase() === "bybit-futures") {
            return Bybit.fetchDemoSymbols();
          }
          if (exchange.toLowerCase() === "bitget") {
            return Bitget.fetchSymbols("spot");
          }
          return Binance.fetchSymbols();
        }),

      fetch24hrVolumes: (exchange) =>
        paced(exchange, "fetch24hrVolumes", () => {
          if (exchange.toLowerCase() === "bitget") {
            return Bitget.fetch24hrVolumes("spot");
          }
          if (exchange.toLowerCase() === "bitget-futures") {
            return Bitget.fetch24hrVolumes("futures");
          }
          if (exchange.toLowerCase() === "bybit-futures") {
            return Bybit.fetch24hrVolumes();
          }
          return Binance.fetch24hrVolumes();
        }),

      fetchFundingRates: (exchange, symbol, startTime, endTime, limit) =>
        paced(exchange, "fetchFundingRates", () => {
          if (exchange.toLowerCase() === "bitget") {
            // Spot has no funding rates; never fall through to another venue.
            return Effect.fail(
              new MarketDataError(
                "Funding rates not supported for bitget spot",
              ),
            );
          }
          if (exchange.toLowerCase() === "bitget-futures") {
            return Bitget.fetchFundingRates(symbol, startTime, endTime, limit);
          }
          if (exchange.toLowerCase() === "bybit-futures") {
            return Bybit.fetchFundingRates(symbol, startTime, endTime, limit);
          }
          return Binance.fetchFundingRates(symbol, startTime, endTime, limit);
        }),
    } satisfies MarketDataGatewayService;
  }),
);

/** Grace added to a bar's open time before it is considered rolled (candle
 *  timestamps are bar OPEN times; the newest bar closes at open+interval).
 *  Polls inside that window are mid-bar and would re-download identical
 *  candles. */
const BAR_ROLL_GRACE_MS = 60_000;
