/**
 * Exchange adapter types for deterministic scalping.
 *
 * The adapter abstracts order placement, balance queries, and position tracking
 * so the paper-trading engine and future live engine share the same interface.
 */

export type OrderSide = "buy" | "sell";
export type OrderType = "market" | "limit";

export interface OrderRequest {
  readonly symbol: string;
  readonly side: OrderSide;
  readonly type: OrderType;
  readonly quantity: number;
  readonly price?: number;
}

export interface OrderFill {
  readonly orderId: string;
  readonly symbol: string;
  readonly side: OrderSide;
  readonly filledQty: number;
  readonly filledPrice: number;
  readonly fee: number;
  readonly timestamp: Date;
}

export interface Balance {
  readonly asset: string;
  readonly free: number;
  readonly locked: number;
}

export interface Position {
  readonly symbol: string;
  readonly side: "long" | "short";
  readonly quantity: number;
  readonly entryPrice: number;
}
