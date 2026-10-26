import { Effect, Layer, Ref } from "effect";
import { randomUUID } from "node:crypto";
import {
  ExchangeAdapter,
  ExchangeError,
  type ExchangeAdapterService,
} from "../adapter.js";
import type { Position } from "../types.js";
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
  const stateRef = Effect.runSync(
    Ref.make<SimulatedState>({
      balances: { USDT: 10_000, ...initialBalances },
      positions: {},
    }),
  );

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
        // Update balances (simplified: assume quote asset is USDT).
        const quoteDelta =
          request.side === "buy"
            ? -(filledPrice * request.quantity) - fee
            : filledPrice * request.quantity - fee;

        // The balance/position bookkeeping is ONE atomic state transition:
        // a read-modify-write that straddled the awaited order-book fetch
        // would lose updates when two orders are placed concurrently.
        const fill = yield* Ref.modify(stateRef, (state): [
          {
            orderId: string;
            symbol: string;
            side: "buy" | "sell";
            filledQty: number;
            filledPrice: number;
            fee: number;
            timestamp: Date;
          },
          SimulatedState,
        ] => {
          const balances = { ...state.balances };
          const positions = { ...state.positions };
          balances.USDT = (balances.USDT ?? 0) + quoteDelta;

          const existing = positions[request.symbol];
          if (existing) {
            if (
              existing.side === (request.side === "buy" ? "long" : "short")
            ) {
              const totalQty = existing.quantity + request.quantity;
              const avgEntry =
                (existing.entryPrice * existing.quantity +
                  filledPrice * request.quantity) /
                totalQty;
              positions[request.symbol] = {
                ...existing,
                quantity: totalQty,
                entryPrice: avgEntry,
              };
            } else {
              // Opposite side: the order closes the existing position and any
              // excess opens a new one on the other side (a flip). Dropping
              // the excess silently loses quantity the fill reported as
              // traded, so the residual keeps its own position instead.
              const residual = request.quantity - existing.quantity;
              if (residual > 0) {
                positions[request.symbol] = {
                  symbol: request.symbol,
                  side: request.side === "buy" ? "long" : "short",
                  quantity: residual,
                  entryPrice: filledPrice,
                };
              } else if (residual < 0) {
                positions[request.symbol] = {
                  ...existing,
                  quantity: -residual,
                };
              } else {
                delete positions[request.symbol];
              }
            }
          } else {
            positions[request.symbol] = {
              symbol: request.symbol,
              side: request.side === "buy" ? "long" : "short",
              quantity: request.quantity,
              entryPrice: filledPrice,
            };
          }

          return [
            {
              orderId: randomUUID(),
              symbol: request.symbol,
              side: request.side,
              filledQty: request.quantity,
              filledPrice,
              fee,
              timestamp: new Date(),
            },
            { balances, positions },
          ];
        });
        return fill;
      }).pipe(
        Effect.catch((err) =>
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
      Effect.map(
        Ref.get(stateRef),
        (state) => ({
          asset,
          free: state.balances[asset] ?? 0,
          locked: 0,
        }),
      ),

    getPosition: (symbol) =>
      Effect.map(Ref.get(stateRef), (state) => state.positions[symbol] ?? null),

    closePosition: (symbol) =>
      Effect.gen(function* () {
        const position = (yield* Ref.get(stateRef)).positions[symbol];
        if (!position) return null;

        // placeOrder's opposite-side branching already reduces or removes
        // the position; deleting here again would wipe a flipped residual.
        return yield* adapter.placeOrder({
          symbol,
          side: position.side === "long" ? "sell" : "buy",
          type: "market",
          quantity: position.quantity,
        });
      }),
  };

  return adapter;
}

export const SimulatedExchangeAdapterLive = (
  initialBalances?: Record<string, number>,
) =>
  Layer.succeed(ExchangeAdapter, makeSimulatedExchangeAdapter(initialBalances));
