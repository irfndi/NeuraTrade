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
 * Plausibility bound for the whole series, not one candle at a time.
 *
 * `validateCandlePrices` rejects non-positive or non-finite values, but a series
 * can still be corrupt: the local 1h BTC set spans 0.04232 to 126011.18, a
 * 3-million-x range that is a data defect rather than a price move. Feeding
 * such a series into the backtest produces absurd returns (a 59-trillion-percent
 * "OOS return" that then PASSED its gate) and lets a broken dataset justify
 * real money.
 *
 * The bound is deliberately loose — crypto has genuinely done 100x — so this
 * rejects only the orders-of-magnitude corruption a unit/venue/split mix-up
 * produces, never a real market move.
 */
export const MAX_SERIES_PRICE_RATIO = 1000;

export function validateCandleSeries(
  venue: string,
  candles: readonly Candle[],
): Effect.Effect<void, MarketDataError, never> {
  let lo = Number.POSITIVE_INFINITY;
  let hi = 0;
  for (const c of candles) {
    if (c.low > 0 && c.low < lo) lo = c.low;
    if (c.high > hi) hi = c.high;
  }
  if (!Number.isFinite(lo) || hi <= 0) return Effect.void;
  const ratio = hi / lo;
  if (ratio > MAX_SERIES_PRICE_RATIO) {
    return Effect.fail(
      new MarketDataError(
        `${venue}: series price range ${lo}..${hi} spans ${ratio.toFixed(0)}x ` +
          `(limit ${MAX_SERIES_PRICE_RATIO}x) - the data is corrupt (mixed symbols, ` +
          `units, or unadjusted splits), so any score computed from it is meaningless`,
      ),
    );
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
