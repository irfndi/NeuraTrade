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

/**
 * Reject a candle page that carries a non-finite or non-positive OHLC price.
 *
 * Wire drift (an empty string field, a renamed key) coerces to 0 or NaN via
 * `Number(...)`, and a 0-priced candle flows straight into mid-price and
 * position sizing (`positionValue / 0`). Fail at the boundary instead.
 */
export function validateCandlePrices(
  venue: string,
  candles: readonly Candle[],
): Effect.Effect<void, MarketDataError, never> {
  for (let i = 0; i < candles.length; i += 1) {
    const c = candles[i];
    const prices: ReadonlyArray<readonly [string, number]> = [
      ["open", c.open],
      ["high", c.high],
      ["low", c.low],
      ["close", c.close],
    ];
    for (const [field, value] of prices) {
      if (!Number.isFinite(value) || value <= 0) {
        return Effect.fail(
          new MarketDataError(
            `${venue} candle ${i} (${c.timestamp.toISOString()}) has invalid ${field}=${value}`,
          ),
        );
      }
    }
  }
  return Effect.void;
}

/**
 * Reject order-book levels whose price/volume did not parse (0 or NaN), so an
 * empty-string field cannot become a 0-priced best bid/ask (mid = 0).
 */
export function validateDepthLevels(
  venue: string,
  side: "bids" | "asks",
  levels: readonly { readonly price: number; readonly volume: number }[],
): Effect.Effect<void, MarketDataError, never> {
  for (let i = 0; i < levels.length; i += 1) {
    const level = levels[i];
    if (!Number.isFinite(level.price) || level.price <= 0) {
      return Effect.fail(
        new MarketDataError(
          `${venue} ${side}[${i}] has invalid price=${level.price}`,
        ),
      );
    }
    if (!Number.isFinite(level.volume) || level.volume < 0) {
      return Effect.fail(
        new MarketDataError(
          `${venue} ${side}[${i}] has invalid volume=${level.volume}`,
        ),
      );
    }
  }
  return Effect.void;
}

export const MarketDataGateway =
  Context.Service<MarketDataGatewayService>("MarketDataGateway");
