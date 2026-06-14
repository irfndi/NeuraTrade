/**
 * Binance public REST client for market data.
 *
 * Fetches exchange info and klines, normalizes symbols, and returns candles
 * with string prices/volumes to avoid float money errors.
 */
import { Context, Data, Effect, Layer } from "effect";
import { RateLimiter } from "./rate-limiter.ts";

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const BINANCE_BASE_URL = "https://api.binance.com";
const DEFAULT_TIMEOUT_MS = 30000;
const DEFAULT_KLINE_LIMIT = 1000;

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

export class BinanceNetworkError extends Data.TaggedError(
  "BinanceNetworkError",
)<{
  readonly cause: string;
  readonly endpoint: string;
}> {}

export class BinanceRateLimitError extends Data.TaggedError(
  "BinanceRateLimitError",
)<{
  readonly retryAfterMs: number;
  readonly endpoint: string;
}> {}

export class BinanceApiError extends Data.TaggedError("BinanceApiError")<{
  readonly status: number;
  readonly body: string;
  readonly endpoint: string;
}> {}

export type BinanceClientError =
  | BinanceNetworkError
  | BinanceRateLimitError
  | BinanceApiError;

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface BinanceSymbolInfo {
  readonly symbol: string;
  readonly baseAsset: string;
  readonly quoteAsset: string;
  readonly status: string;
}

export interface ExchangeInfoResponse {
  readonly symbols: ReadonlyArray<BinanceSymbolInfo>;
}

export interface RawCandle {
  readonly timestamp: Date;
  readonly open: string;
  readonly high: string;
  readonly low: string;
  readonly close: string;
  readonly volume: string;
}

// ---------------------------------------------------------------------------
// Symbol normalization
// ---------------------------------------------------------------------------

export function toBinanceSymbol(symbol: string): string {
  return symbol.replace("/", "").toUpperCase();
}

export function fromBinanceSymbol(rawSymbol: string): string {
  const upper = rawSymbol.toUpperCase();
  if (upper.includes("/")) return upper;
  const quoteAssets = [
    "USDT",
    "USDC",
    "BTC",
    "ETH",
    "BNB",
    "FDUSD",
    "TUSD",
    "BUSD",
    "PAX",
    "TRY",
    "EUR",
    "GBP",
    "JPY",
    "AUD",
  ];
  for (const quote of quoteAssets) {
    if (upper.endsWith(quote) && upper.length > quote.length) {
      return `${upper.slice(0, upper.length - quote.length)}/${quote}`;
    }
  }
  return upper;
}

// ---------------------------------------------------------------------------
// Service interface
// ---------------------------------------------------------------------------

export interface BinanceClientImpl {
  readonly getExchangeInfo: () => Effect.Effect<
    ExchangeInfoResponse,
    BinanceClientError
  >;
  readonly getKlines: (args: {
    readonly symbol: string;
    readonly interval: string;
    readonly startTime: number;
    readonly endTime: number;
    readonly limit?: number;
  }) => Effect.Effect<ReadonlyArray<RawCandle>, BinanceClientError>;
}

export class BinanceClient extends Context.Tag("BinanceClient")<
  BinanceClient,
  BinanceClientImpl
>() {}

// ---------------------------------------------------------------------------
// Internal fetch helper
// ---------------------------------------------------------------------------

interface RateLimiterLike {
  readonly acquire: (n?: number) => Effect.Effect<void, never>;
}

function fetchBinance<T>(
  baseUrl: string,
  endpoint: string,
  rateLimiter: RateLimiterLike,
): Effect.Effect<T, BinanceClientError, never> {
  return Effect.gen(function* () {
    yield* rateLimiter.acquire();
    const url = `${baseUrl}${endpoint}`;

    const response = yield* Effect.tryPromise({
      try: () =>
        fetch(url, { signal: AbortSignal.timeout(DEFAULT_TIMEOUT_MS) }),
      catch: (error): BinanceClientError => {
        if (error instanceof DOMException && error.name === "TimeoutError") {
          return new BinanceNetworkError({
            cause: `request timed out after ${DEFAULT_TIMEOUT_MS}ms`,
            endpoint,
          });
        }
        return new BinanceNetworkError({
          cause: error instanceof Error ? error.message : String(error),
          endpoint,
        });
      },
    });

    if (response.status === 429) {
      const retryAfter = Number(response.headers.get("Retry-After") || "0");
      return yield* Effect.fail(
        new BinanceRateLimitError({
          retryAfterMs: retryAfter * 1000,
          endpoint,
        }),
      );
    }

    if (!response.ok) {
      const body = yield* Effect.tryPromise({
        try: () => response.text(),
        catch: () => Effect.succeed(""),
      }).pipe(Effect.catchAll(() => Effect.succeed("")));
      return yield* Effect.fail(
        new BinanceApiError({ status: response.status, body, endpoint }),
      );
    }

    return yield* Effect.tryPromise({
      try: () => response.json() as Promise<T>,
      catch: (error): BinanceClientError =>
        new BinanceApiError({
          status: response.status,
          body: error instanceof Error ? error.message : String(error),
          endpoint,
        }),
    });
  });
}

function parseKline(row: ReadonlyArray<unknown>): RawCandle {
  const openTime = Number(row[0]);
  return {
    timestamp: new Date(openTime),
    open: String(row[1]),
    high: String(row[2]),
    low: String(row[3]),
    close: String(row[4]),
    volume: String(row[5]),
  };
}

// ---------------------------------------------------------------------------
// Live layer
// ---------------------------------------------------------------------------

export const BinanceClientLive = (
  baseUrl: string = BINANCE_BASE_URL,
): Layer.Layer<BinanceClient, never, RateLimiter> =>
  Layer.effect(
    BinanceClient,
    Effect.gen(function* () {
      const rateLimiter = yield* RateLimiter;

      const getExchangeInfo = (): Effect.Effect<
        ExchangeInfoResponse,
        BinanceClientError
      > =>
        fetchBinance<ExchangeInfoResponse>(
          baseUrl,
          "/api/v3/exchangeInfo",
          rateLimiter,
        );

      const getKlines = (args: {
        readonly symbol: string;
        readonly interval: string;
        readonly startTime: number;
        readonly endTime: number;
        readonly limit?: number;
      }): Effect.Effect<ReadonlyArray<RawCandle>, BinanceClientError> => {
        const bsymbol = toBinanceSymbol(args.symbol);
        const params = new URLSearchParams({
          symbol: bsymbol,
          interval: args.interval,
          startTime: String(args.startTime),
          endTime: String(args.endTime),
          limit: String(args.limit ?? DEFAULT_KLINE_LIMIT),
        });
        return fetchBinance<ReadonlyArray<ReadonlyArray<unknown>>>(
          baseUrl,
          `/api/v3/klines?${params.toString()}`,
          rateLimiter,
        ).pipe(Effect.map((rows) => rows.map(parseKline)));
      };

      return {
        getExchangeInfo,
        getKlines,
      };
    }),
  );
