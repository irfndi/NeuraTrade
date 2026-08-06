import { Effect, Layer } from "effect";
import { MarketDataGateway } from "../market-data/gateway.js";
import {
  MarketDataRepository,
  MarketDataRepositoryError,
  type CandleQuery,
  type MarketDataRepositoryService,
} from "../market-data/repository.js";

/**
 * Worker-flavored MarketDataRepository: reads candles live from the exchange
 * gateway (Bitget public REST — fetch-based, Worker-safe) instead of a local
 * SQLite database.
 *
 * Only the two methods `runGridUniverseScan` exercises are implemented for
 * real (`listSymbolsByCandleCount`, `getCandles`). The rest are explicit
 * stubs that fail loudly so nothing silently pretends to persist on the
 * edge. This is the porting seam: a D1-backed implementation can replace
 * this later without touching the scanner.
 *
 * ponytail: bounded universe — `listSymbolsByCandleCount` only counts the
 * seed symbols supplied by the caller, never the full exchange symbol list.
 */
export const CloudflareMarketDataRepositoryLive = (
  seedSymbols: readonly string[],
) =>
  Layer.effect(
    MarketDataRepository,
    Effect.gen(function* () {
      const gateway = yield* MarketDataGateway;

      const toRepoError = (
        err: unknown,
        op: string,
      ): MarketDataRepositoryError =>
        new MarketDataRepositoryError(
          `${op}: ${err instanceof Error ? err.message : String(err)}`,
        );

      const getCandles = (query: CandleQuery) =>
        gateway
          .fetchOHLCV(
            query.exchange,
            query.symbol,
            query.timeframe,
            query.limit ?? 500,
            query.from,
          )
          .pipe(Effect.mapError((err) => toRepoError(err, "getCandles")));

      const service: MarketDataRepositoryService = {
        getCandles,
        listSymbolsByCandleCount: (exchange, timeframe, limit) =>
          // Bounded concurrency: the seed universe can be large and the
          // gateway is a public rate-limited API.
          Effect.forEach(
            seedSymbols,
            (symbol) =>
              gateway
                .fetchOHLCV(exchange, symbol, timeframe, limit)
                .pipe(
                  Effect.map((candles) => ({ symbol, count: candles.length })),
                ),
            { concurrency: 4 },
          ).pipe(
            Effect.mapError((err) =>
              toRepoError(err, "listSymbolsByCandleCount"),
            ),
          ),

        saveTick: () =>
          Effect.fail(
            new MarketDataRepositoryError(
              "saveTick: not implemented on Cloudflare worker",
            ),
          ),
        saveCandles: () =>
          Effect.fail(
            new MarketDataRepositoryError(
              "saveCandles: not implemented on Cloudflare worker",
            ),
          ),
        getLatestTick: () =>
          Effect.fail(
            new MarketDataRepositoryError(
              "getLatestTick: not implemented on Cloudflare worker",
            ),
          ),
        listSymbols: () => Effect.succeed([...seedSymbols]),
        deleteCandles: () =>
          Effect.fail(
            new MarketDataRepositoryError(
              "deleteCandles: not implemented on Cloudflare worker",
            ),
          ),
        getCandleRange: () =>
          Effect.fail(
            new MarketDataRepositoryError(
              "getCandleRange: not implemented on Cloudflare worker",
            ),
          ),
        getCoverageReport: () =>
          Effect.fail(
            new MarketDataRepositoryError(
              "getCoverageReport: not implemented on Cloudflare worker",
            ),
          ),
        ensureTables: () => Effect.void,
        ensureFundingRatesTable: () => Effect.void,
        saveFundingRates: () =>
          Effect.fail(
            new MarketDataRepositoryError(
              "saveFundingRates: not implemented on Cloudflare worker",
            ),
          ),
        getFundingRates: () => Effect.succeed([]),
        getLatestFundingRateBefore: () =>
          Effect.fail(
            new MarketDataRepositoryError(
              "getLatestFundingRateBefore: not implemented on Cloudflare worker",
            ),
          ),
      };

      return service;
    }).pipe(Effect.map((s) => s as MarketDataRepositoryService)),
  );
