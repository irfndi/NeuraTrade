import { Effect, Layer, Option } from "effect";
import { Console } from "effect";
import { MarketDataError, MarketDataGateway } from "../market-data/gateway.js";
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
        cause: unknown,
        op: string,
      ): MarketDataRepositoryError =>
        new MarketDataRepositoryError(
          `${op}: ${
            cause instanceof MarketDataError
              ? cause.reason
              : cause instanceof Error
                ? cause.message
                : String(cause)
          }`,
        );

      // getCandles' `limit` is a PAGE SIZE (candles per fetch), while
      // listSymbolsByCandleCount's `minCandles` is a COUNT TARGET. The page
      // size must stay at or below every gateway's per-request cap (Bitget
      // history-candles caps at 200/request; others accept more).
      const DEFAULT_CANDLE_PAGE = 200;
      // Bound the worker: count at most 2,000 candles per symbol per scan.
      // The count target is where paging stops; this cap only stops a
      // pathological target from fetching unbounded history.
      const MAX_COUNT_PAGES = 10;

      const timeframeToMs = (timeframe: string): number => {
        const value = Number.parseInt(timeframe, 10);
        if (!Number.isFinite(value)) return 60_000;
        switch (timeframe.slice(-1)) {
          case "h":
            return value * 3_600_000;
          case "d":
            return value * 86_400_000;
          case "w":
            return value * 604_800_000;
          default:
            return value * 60_000;
        }
      };

      /**
       * Count a symbol's available candles up to `minCandles`.
       *
       * A single gateway read is capped (Bitget: 200), so passing the count
       * target straight through as a page size silently reported every symbol
       * as "200 candles" and let the caller's `count >= minCandles` filter
       * drop liquid symbols (clever-cabin-q2e). Page backwards from the most
       * recent candle instead and stop once the target is reached or history
       * runs out. The reported count is capped at the target: callers only
       * compare against it.
       */
      const countCandles = (
        exchange: string,
        symbol: string,
        timeframe: string,
        minCandles: number,
      ) =>
        Effect.gen(function* () {
          const target = Math.max(1, Math.floor(minCandles));
          const pageSize = Math.min(target, DEFAULT_CANDLE_PAGE);
          const intervalMs = timeframeToMs(timeframe);
          const timestamps = new Set<number>();
          let oldest: number | undefined;
          for (
            let page = 0;
            page < MAX_COUNT_PAGES && timestamps.size < target;
            page += 1
          ) {
            const from =
              oldest === undefined
                ? undefined
                : new Date(oldest - pageSize * intervalMs);
            const candles = yield* gateway.fetchOHLCV(
              exchange,
              symbol,
              timeframe,
              pageSize,
              from,
            );
            if (candles.length === 0) break;
            for (const candle of candles) {
              timestamps.add(candle.timestamp.getTime());
            }
            const first = candles[0]!.timestamp.getTime();
            // No backward progress (the venue ignored the start cursor) or a
            // short page (listing start reached): stop instead of looping.
            if (oldest !== undefined && first >= oldest) break;
            oldest = first;
            if (candles.length < pageSize) break;
          }
          return Math.min(timestamps.size, target);
        });

      const getCandles = (query: CandleQuery) =>
        gateway
          .fetchOHLCV(
            query.exchange,
            query.symbol,
            query.timeframe,
            query.limit ?? DEFAULT_CANDLE_PAGE,
            query.from,
          )
          .pipe(Effect.mapError((err) => toRepoError(err, "getCandles")));

      const service: MarketDataRepositoryService = {
        getCandles,
        listSymbolsByCandleCount: (exchange, timeframe, minCandles) =>
          // Bounded concurrency: the seed universe can be large and the
          // gateway is a public rate-limited API.
          Effect.forEach(
            seedSymbols,
            (symbol) =>
              countCandles(exchange, symbol, timeframe, minCandles).pipe(
                Effect.map((count) => ({ symbol, count })),
                // Per-symbol tolerance: a delisted pair or a rate limit
                // must not abort the whole universe scan — failed symbols
                // are skipped and the rest still count, but every skip is
                // LOGGED so a systemic outage (rate limit, 5xx) is visible
                // as a shrinking universe instead of silent zero counts.
                Effect.tapError((err) =>
                  Console.warn(
                    `[listSymbolsByCandleCount] ${symbol} skipped: ${err.reason}`,
                  ),
                ),
                Effect.option,
              ),
            { concurrency: 4 },
          ).pipe(
            Effect.map((results) =>
              results.flatMap((r) => (Option.isSome(r) ? [r.value] : [])),
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
        getFundingRates: () =>
          Effect.fail(
            new MarketDataRepositoryError(
              "getFundingRates: not implemented on Cloudflare worker",
            ),
          ),
        getLatestFundingRateBefore: () =>
          Effect.fail(
            new MarketDataRepositoryError(
              "getLatestFundingRateBefore: not implemented on Cloudflare worker",
            ),
          ),
        deleteFundingRates: () =>
          Effect.fail(
            new MarketDataRepositoryError(
              "deleteFundingRates: not implemented on Cloudflare worker",
            ),
          ),
      };

      return service;
    }).pipe(Effect.map((s) => s as MarketDataRepositoryService)),
  );
