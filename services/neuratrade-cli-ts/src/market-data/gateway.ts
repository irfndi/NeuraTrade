import { Context, Effect } from "effect";
import type { Candle, FundingRate, OrderBook, Tick } from "./types.js";

/**
 * Error raised when exchange market-data fetch fails.
 */
export class MarketDataError {
  readonly _tag = "MarketDataError" as const;
  constructor(
    readonly reason: string,
    readonly cause?: unknown,
    /** Server-provided Retry-After hint in ms (429 responses); callers
     *  honoring it must not retry sooner. */
    readonly retryAfterMs?: number,
  ) {}
}

/**
 * Port for fetching normalized market data from an exchange.
 *
 * Implementations hide exchange-specific HTTP details (REST paths, response
 * shapes, rate limits) behind a deterministic, testable interface.
 */
export interface MarketDataGatewayService {
  readonly fetchTick: (
    exchange: string,
    symbol: string,
  ) => Effect.Effect<Tick, MarketDataError, never>;

  readonly fetchOHLCV: (
    exchange: string,
    symbol: string,
    timeframe: string,
    limit: number,
    startTime?: Date,
  ) => Effect.Effect<readonly Candle[], MarketDataError, never>;

  readonly fetchOrderBook: (
    exchange: string,
    symbol: string,
    limit: number,
  ) => Effect.Effect<OrderBook, MarketDataError, never>;

  readonly fetchSymbols: (
    exchange: string,
  ) => Effect.Effect<readonly string[], MarketDataError, never>;

  /**
   * The exchange's demo/simulated-trading instrument subset (normalized
   * "BASE/QUOTE" symbols), or the full instrument list when the exchange
   * has no demo environment. Consumers use it as the hard universe bound:
   * a symbol outside this set can never be paper-traded.
   */
  readonly fetchDemoSymbols: (
    exchange: string,
  ) => Effect.Effect<readonly string[], MarketDataError, never>;

  /**
   * 24h quote volume per symbol, keyed by the exchange's RAW wire symbol
   * (e.g. "BTCUSDT"), not the normalized "BASE/QUOTE" form used by
   * fetchSymbols — the ticker endpoints do not return base/quote split.
   * Consumers normalize canonical symbols with `symbol.replace("/", "")`.
   */
  readonly fetch24hrVolumes: (
    exchange: string,
  ) => Effect.Effect<Readonly<Record<string, number>>, MarketDataError, never>;

  readonly fetchFundingRates: (
    exchange: string,
    symbol: string,
    startTime?: Date,
    endTime?: Date,
    limit?: number,
  ) => Effect.Effect<readonly FundingRate[], MarketDataError, never>;
}

export const MarketDataGateway =
  Context.Service<MarketDataGatewayService>("MarketDataGateway");
