import { Context, Effect } from "effect";
import { ExchangeError } from "./adapter.js";
import type { Money } from "../utils/money.js";

export type FuturesOrderSide = "buy" | "sell";
export type FuturesOrderType = "market" | "limit";

export type FuturesProductType =
  | "USDT-FUTURES"
  | "COIN-FUTURES"
  | "USDC-FUTURES";

export type FuturesMarginMode = "isolated" | "crossed";
export type FuturesPositionMode = "one_way" | "hedge_mode";

export interface FuturesOrderRequest {
  readonly symbol: string;
  readonly side: FuturesOrderSide;
  readonly type: FuturesOrderType;
  readonly size: Money;
  readonly price?: Money;
  readonly productType: FuturesProductType;
  readonly marginMode: FuturesMarginMode;
  readonly leverage: number;
  readonly reduceOnly?: boolean;
  readonly clientOid?: string;
}

export interface FuturesOrderFill {
  readonly orderId: string;
  readonly clientOid?: string;
  readonly symbol: string;
  readonly side: FuturesOrderSide;
  readonly productType: FuturesProductType;
  readonly marginMode: FuturesMarginMode;
  readonly filledQty: Money;
  readonly filledPrice: Money;
  readonly fee: Money;
  readonly timestamp: Date;
  /**
   * Effective leverage the order was placed with. Adapters that resize
   * and/or re-leverage sub-floor orders MUST report the leverage actually
   * set on the exchange here: recording the requested leverage instead
   * strands reconciliation ("exchange leverage X differs from expected Y").
   */
  readonly leverage?: number;
}

export interface FuturesPosition {
  readonly symbol: string;
  readonly side: "long" | "short";
  readonly productType: FuturesProductType;
  readonly marginMode: FuturesMarginMode;
  readonly leverage: number;
  readonly quantity: Money;
  readonly available: Money;
  readonly entryPrice: Money;
  readonly liquidationPrice?: Money;
  readonly unrealizedPnl?: Money;
  readonly marginCoin: string;
}

export interface FuturesBalance {
  readonly marginCoin: string;
  readonly available: Money;
  readonly locked: Money;
  readonly equity: Money;
  readonly usdtEquity: Money;
}

export interface ClosePositionRequest {
  readonly symbol: string;
  readonly side: FuturesOrderSide;
  readonly productType: FuturesProductType;
  readonly marginMode: FuturesMarginMode;
  readonly leverage: number;
  readonly size: Money;
  readonly price?: Money;
}

/**
 * Exchange-side take-profit / stop-loss attachment for an open position.
 *
 * Prices are ABSOLUTE (not percentages). The exchange holds the TP/SL orders
 * so they fire even if this client disconnects — this is the "Model B" manual
 * trading model, distinct from polling-in-a-loop closes.
 */
export interface SetTradingStopRequest {
  readonly symbol: string;
  readonly productType: FuturesProductType;
  readonly marginMode: FuturesMarginMode;
  /** The position leg the TP/SL attaches to. */
  readonly side: "long" | "short";
  /** Absolute take-profit trigger price. Omit to leave TP unchanged. */
  readonly takeProfit?: Money;
  /** Absolute stop-loss trigger price. Omit to leave SL unchanged. */
  readonly stopLoss?: Money;
}

/**
 * Port for placing orders and querying balances/positions on a futures exchange.
 *
 * Implementations may be simulated (paper trading) or live (real money).
 */
export interface FuturesExchangeAdapterService {
  readonly placeOrder: (
    request: FuturesOrderRequest,
  ) => Effect.Effect<FuturesOrderFill, ExchangeError>;

  readonly closePosition: (
    request: ClosePositionRequest,
  ) => Effect.Effect<FuturesOrderFill | null, ExchangeError>;

  readonly getPosition: (
    symbol: string,
    productType: FuturesProductType,
  ) => Effect.Effect<FuturesPosition | null, ExchangeError>;

  readonly getBalance: (
    marginCoin: string,
    productType?: FuturesProductType,
  ) => Effect.Effect<FuturesBalance, ExchangeError>;

  readonly setLeverage: (
    symbol: string,
    productType: FuturesProductType,
    marginMode: FuturesMarginMode,
    leverage: number,
    holdSide?: "long" | "short",
  ) => Effect.Effect<void, ExchangeError>;

  readonly setMarginMode: (
    symbol: string,
    productType: FuturesProductType,
    marginMode: FuturesMarginMode,
  ) => Effect.Effect<void, ExchangeError>;

  readonly setPositionMode: (
    productType: FuturesProductType,
    positionMode: FuturesPositionMode,
  ) => Effect.Effect<void, ExchangeError>;

  /**
   * Cancel all resting open orders for a symbol. Optional: implementations
   * without exchange-level order cancellation omit this member, and engines
   * must treat absence as "no cancellation available" and skip.
   */
  readonly cancelOpenOrders?: (
    symbol: string,
    productType: FuturesProductType,
  ) => Effect.Effect<void, ExchangeError>;

  /**
   * Attach exchange-side take-profit / stop-loss orders to an open position.
   * The exchange holds these orders so they fire even if the client
   * disconnects. Optional: implementations without exchange-side TP/SL (e.g.
   * calling adapters) omit this member, and engines must treat absence as
   * "no built-in TP/SL; fall back to polling closes".
   */
  readonly setTradingStop?: (
    request: SetTradingStopRequest,
  ) => Effect.Effect<void, ExchangeError>;
}

export const FuturesExchangeAdapter =
  Context.Service<FuturesExchangeAdapterService>("FuturesExchangeAdapter");
