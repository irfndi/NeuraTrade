import { Context, Effect } from "effect";
import type { MarketDataGatewayService } from "../market-data/gateway.js";
import type { Balance, OrderFill, OrderRequest, Position } from "./types.js";

/**
 * Error raised when an exchange operation fails.
 */
export class ExchangeError {
  readonly _tag = "ExchangeError" as const;
  constructor(
    readonly reason: string,
    readonly cause?: unknown,
  ) {}
}

/**
 * Port for placing orders and querying balances on an exchange.
 *
 * Implementations may be simulated (paper trading) or live (real money).
 */
export interface ExchangeAdapterService {
  readonly placeOrder: (
    request: OrderRequest,
  ) => Effect.Effect<OrderFill, ExchangeError, MarketDataGatewayService>;

  readonly getBalance: (
    asset: string,
  ) => Effect.Effect<Balance, ExchangeError, MarketDataGatewayService>;

  readonly getPosition: (
    symbol: string,
  ) => Effect.Effect<Position | null, ExchangeError, MarketDataGatewayService>;

  readonly closePosition: (
    symbol: string,
  ) => Effect.Effect<OrderFill | null, ExchangeError, MarketDataGatewayService>;
}

export const ExchangeAdapter =
  Context.Service<ExchangeAdapterService>("ExchangeAdapter");
