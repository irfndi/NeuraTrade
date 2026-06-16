import { Context, Effect } from "effect";
import type { Candle, OrderBook, Tick } from "./types.js";

/**
 * Error raised when exchange market-data fetch fails.
 */
export class MarketDataError {
  readonly _tag = "MarketDataError" as const;
  constructor(
    readonly reason: string,
    readonly cause?: unknown,
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

  readonly fetch24hrVolumes: (
    exchange: string,
  ) => Effect.Effect<Readonly<Record<string, number>>, MarketDataError, never>;
}

export const MarketDataGateway =
  Context.GenericTag<MarketDataGatewayService>("MarketDataGateway");
