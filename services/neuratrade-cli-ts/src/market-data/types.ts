import { Schema } from "effect";

/**
 * A single price level in an order book.
 */
export interface PriceLevel {
  readonly price: number;
  readonly volume: number;
}

/**
 * Normalized tick/top-of-book snapshot for a single exchange + symbol.
 */
export interface Tick {
  readonly exchange: string;
  readonly symbol: string;
  readonly price: number;
  readonly volume: number;
  readonly bid?: number;
  readonly ask?: number;
  readonly high24h?: number;
  readonly low24h?: number;
  readonly volume24h?: number;
  readonly timestamp: Date;
}

/**
 * Normalized OHLCV candle.
 */
export interface Candle {
  readonly exchange: string;
  readonly symbol: string;
  readonly timeframe: string;
  readonly open: number;
  readonly high: number;
  readonly low: number;
  readonly close: number;
  readonly volume: number;
  readonly timestamp: Date;
}

/**
 * Normalized order book snapshot.
 */
export interface OrderBook {
  readonly exchange: string;
  readonly symbol: string;
  readonly bids: readonly PriceLevel[];
  readonly asks: readonly PriceLevel[];
  readonly timestamp: Date;
}

/**
 * Computed order-book metrics used by deterministic scalping strategies.
 */
export interface OrderBookMetrics {
  readonly exchange: string;
  readonly symbol: string;
  readonly spread: number;
  readonly spreadPercent: number;
  readonly bidDepth: number;
  readonly askDepth: number;
  readonly imbalance: number;
  readonly midPrice: number;
  readonly timestamp: Date;
}

// ---------------------------------------------------------------------------
// Schema-validated primitives for config and persistence
// ---------------------------------------------------------------------------

export const Timeframe = Schema.Literal(
  "1m",
  "5m",
  "15m",
  "30m",
  "1h",
  "4h",
  "1d",
);
export type Timeframe = typeof Timeframe.Type;

export const ExchangeId = Schema.String.pipe(Schema.minLength(1));
export type ExchangeId = typeof ExchangeId.Type;

export const Symbol = Schema.String.pipe(Schema.minLength(1));
export type Symbol = typeof Symbol.Type;

/**
 * Configuration for a market-data collection stream.
 */
export const CollectionConfig = Schema.Struct({
  exchange: ExchangeId,
  symbol: Symbol,
  timeframe: Schema.optionalWith(Timeframe, { default: () => "1m" }),
  enabled: Schema.optionalWith(Schema.Boolean, { default: () => true }),
});
export type CollectionConfig = typeof CollectionConfig.Type;
