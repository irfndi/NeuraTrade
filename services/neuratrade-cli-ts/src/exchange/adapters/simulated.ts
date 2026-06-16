import { Effect, Layer } from "effect";
import { randomUUID } from "node:crypto";
import {
  ExchangeAdapter,
  ExchangeError,
  type ExchangeAdapterService,
} from "../adapter.js";
import type { Balance, OrderFill, OrderRequest, Position } from "../types.js";
import { MarketDataGateway } from "../../market-data/gateway.js";

interface SimulatedState {
  balances: Record<string, number>;
  positions: Record<string, Position>;
}

/**
 * Simulated exchange adapter that fills orders against the live order book.
 *
 * This adapter is stateful (positions/balances are held in memory) and is
 * intended for paper-trading and unit tests. It is deterministic given the
 * same order-book snapshot.
 */
export function makeSimulatedExchangeAdapter(
  initialBalances: Record<string, number> = {},
): ExchangeAdapterService {
  const state: SimulatedState = {
    balances: { USDT: 10_000, ...initialBalances },
    positions: {},
  };

  const adapter: ExchangeAdapterService = {
    placeOrder: (request) =>
      Effect.gen(function* () {
        const gateway = yield* MarketDataGateway;
        const ob = yield* gateway.fetchOrderBook("binance", request.symbol, 5);

        if (ob.asks.length === 0 || ob.bids.length === 0) {
          return yield* Effect.fail(
            new ExchangeError(`No orderbook depth for ${request.symbol}`),
          );
        }

        const filledPrice =
          request.type === "market"
            ? request.side === "buy"
              ? ob.asks[0].price
              : ob.bids[0].price
            : (request.price ??
              (request.side === "buy" ? ob.asks[0].price : ob.bids[0].price));

        const fee = filledPrice * request.quantity * 0.001;
        const fillId = randomUUID();

        // Update balances (simplified: assume quote asset is USDT).
        const quoteDelta =
          request.side === "buy"
            ? -(filledPrice * request.quantity) - fee
            : filledPrice * request.quantity - fee;
        state.balances.USDT = (state.balances.USDT ?? 0) + quoteDelta;

        // Update position.
        const existing = state.positions[request.symbol];
        if (existing) {
          if (existing.side === (request.side === "buy" ? "long" : "short")) {
            const totalQty = existing.quantity + request.quantity;
            const avgEntry =
              (existing.entryPrice * existing.quantity +
                filledPrice * request.quantity) /
              totalQty;
            state.positions[request.symbol] = {
              ...existing,
              quantity: totalQty,
              entryPrice: avgEntry,
            };
          } else if (request.quantity >= existing.quantity) {
            delete state.positions[request.symbol];
          } else {
            state.positions[request.symbol] = {
              ...existing,
              quantity: existing.quantity - request.quantity,
            };
          }
        } else {
          state.positions[request.symbol] = {
            symbol: request.symbol,
            side: request.side === "buy" ? "long" : "short",
            quantity: request.quantity,
            entryPrice: filledPrice,
          };
        }

        return {
          orderId: fillId,
          symbol: request.symbol,
          side: request.side,
          filledQty: request.quantity,
          filledPrice,
          fee,
          timestamp: new Date(),
        };
      }).pipe(
        Effect.catchAll((err) =>
          Effect.fail(
            err instanceof ExchangeError
              ? err
              : new ExchangeError(
                  `Exchange fetch failed: ${"reason" in err ? err.reason : String(err)}`,
                  err,
                ),
          ),
        ),
      ),

    getBalance: (asset) =>
      Effect.succeed({
        asset,
        free: state.balances[asset] ?? 0,
        locked: 0,
      }),

    getPosition: (symbol) => Effect.succeed(state.positions[symbol] ?? null),

    closePosition: (symbol) =>
      Effect.gen(function* () {
        const position = state.positions[symbol];
        if (!position) return null;

        const fill = yield* adapter.placeOrder({
          symbol,
          side: position.side === "long" ? "sell" : "buy",
          type: "market",
          quantity: position.quantity,
        });

        delete state.positions[symbol];
        return fill;
      }),
  };

  return adapter;
}

export const SimulatedExchangeAdapterLive = (
  initialBalances?: Record<string, number>,
) =>
  Layer.succeed(ExchangeAdapter, makeSimulatedExchangeAdapter(initialBalances));
