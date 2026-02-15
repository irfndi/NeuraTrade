import type { Exchange, Ticker, OrderBook, OHLCV, Order, Trade } from "ccxt";

// Order Execution Types

/**
 * Order side enumeration.
 */
export type OrderSide = "buy" | "sell";

/**
 * Order type enumeration.
 */
export type OrderType = "market" | "limit" | "stop" | "stop_limit";

/**
 * Request to place an order.
 */
export interface PlaceOrderRequest {
  exchange: string;
  symbol: string;
  side: OrderSide;
  type: OrderType;
  amount: number;
  price?: number;
  params?: Record<string, unknown>;
}

/**
 * Response from placing an order.
 */
export interface PlaceOrderResponse {
  order: Order;
  timestamp: string;
}

/**
 * Request to cancel an order.
 */
export interface CancelOrderRequest {
  exchange: string;
  orderId: string;
  symbol?: string;
}

/**
 * Response from cancelling an order.
 */
export interface CancelOrderResponse {
  success: boolean;
  orderId: string;
  timestamp: string;
}

/**
 * Request to get order status.
 */
export interface GetOrderRequest {
  exchange: string;
  orderId: string;
  symbol?: string;
}

/**
 * Response containing order details.
 */
export interface GetOrderResponse {
  order: Order;
  timestamp: string;
}

/**
 * Request to get open orders.
 */
export interface GetOpenOrdersRequest {
  exchange: string;
  symbol?: string;
}

/**
 * Response containing open orders.
 */
export interface GetOpenOrdersResponse {
  orders: Order[];
  timestamp: string;
}

/**
 * Request to get closed orders.
 */
export interface GetClosedOrdersRequest {
  exchange: string;
  symbol?: string;
  limit?: number;
}

/**
 * Response containing closed orders.
 */
export interface GetClosedOrdersResponse {
  orders: Order[];
  timestamp: string;
}

/**
 * Request to get order trades/fills.
 */
export interface GetOrderTradesRequest {
  exchange: string;
  orderId: string;
  symbol?: string;
}

/**
 * Response containing order trades.
 */
export interface GetOrderTradesResponse {
  trades: Trade[];
  timestamp: string;
}

// API Response Types

/**
 * Health check response structure.
 */
export interface HealthResponse {
  status: "healthy" | "unhealthy";
  timestamp: string;
  service: string;
  version: string;
}

/**
 * Exchange information structure.
 */
export interface ExchangeInfo {
  id: string;
  name: string;
  countries: string[];
  urls: Record<string, any>;
}

/**
 * Response containing a list of supported exchanges.
 */
export interface ExchangesResponse {
  exchanges: ExchangeInfo[];
}

/**
 * Response containing ticker data for a specific symbol.
 */
export interface TickerResponse {
  exchange: string;
  symbol: string;
  ticker: Ticker;
  timestamp: string;
}

/**
 * Response containing order book data for a specific symbol.
 */
export interface OrderBookResponse {
  exchange: string;
  symbol: string;
  orderbook: OrderBook;
  timestamp: string;
}

/**
 * Response containing OHLCV (Open, High, Low, Close, Volume) data.
 */
export interface OHLCVResponse {
  exchange: string;
  symbol: string;
  timeframe: string;
  ohlcv: OHLCV[];
  timestamp: string;
}

/**
 * Response containing available markets/pairs for an exchange.
 */
export interface MarketsResponse {
  exchange: string;
  symbols: string[];
  count: number;
  timestamp: string;
}

/**
 * Request payload for fetching multiple tickers at once.
 */
export interface MultiTickerRequest {
  symbols: string[];
  exchanges?: string[];
}

/**
 * A ticker entry with exchange and symbol context.
 */
export interface TickerEntry extends Ticker {
  exchange: string;
  symbol: string;
}

/**
 * Response containing multiple tickers across exchanges.
 * Returns a flat array of tickers for easier Go client parsing.
 */
export interface MultiTickerResponse {
  tickers: TickerEntry[];
  timestamp: string;
}

/**
 * Standard error response structure.
 */
export interface ErrorResponse {
  error: string;
  message?: string;
  timestamp: string;
  availableExchanges?: string[];
}

// Funding Rate Types

/**
 * Structure representing funding rate data for a futures contract.
 */
export interface FundingRate {
  symbol: string;
  fundingRate: number;
  fundingTimestamp: number;
  nextFundingTime: number;
  markPrice: number;
  indexPrice: number;
  timestamp: number;
}

/**
 * Response containing funding rates for an exchange.
 */
export interface FundingRateResponse {
  exchange: string;
  fundingRates: FundingRate[];
  count: number;
  timestamp: string;
}

/**
 * Query parameters for funding rate requests.
 */
export interface FundingRateQuery {
  symbols?: string[];
}

// Exchange Management

/**
 * Dictionary mapping exchange IDs to initialized CCXT exchange instances.
 */
export interface ExchangeManager {
  [key: string]: Exchange;
}

// Query Parameters

/**
 * Query parameters for OHLCV data requests.
 */
export interface OHLCVQuery {
  timeframe?: string;
  limit?: string;
}

/**
 * Query parameters for order book requests.
 */
export interface OrderBookQuery {
  limit?: string;
}

// Environment Variables

/**
 * Required environment variables configuration.
 */
export interface EnvConfig {
  PORT: string;
  NODE_ENV: string;
}
