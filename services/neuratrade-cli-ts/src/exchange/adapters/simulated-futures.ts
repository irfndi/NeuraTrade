import { Effect, Layer, Ref } from "effect";
import { randomUUID } from "node:crypto";
import {
  FuturesExchangeAdapter,
  type FuturesExchangeAdapterService,
  type FuturesOrderFill,
  type FuturesOrderRequest,
  type FuturesPosition,
  type ClosePositionRequest,
  type FuturesProductType,
} from "../futures-adapter.js";
import { ExchangeError } from "../adapter.js";
import {
  MarketDataGateway,
  type MarketDataGatewayService,
} from "../../market-data/gateway.js";
import { Decimal, money, toNumber } from "../../utils/money.js";

interface SimulatedAccountState {
  readonly balances: Readonly<Record<string, Decimal>>;
  readonly positions: Readonly<Record<string, FuturesPosition>>;
}

function positionKey(symbol: string, productType: FuturesProductType): string {
  return `${productType}:${symbol}`;
}

function midPriceFromOrderBook(
  gateway: MarketDataGatewayService,
  symbol: string,
  exchange: string,
): Effect.Effect<number, ExchangeError> {
  return Effect.gen(function* () {
    const ob = yield* gateway
      .fetchOrderBook(exchange, symbol, 5)
      .pipe(
        Effect.mapError(
          (err) =>
            new ExchangeError(
              `simulated futures fill error: ${err.reason}`,
              err,
            ),
        ),
      );
    if (ob.bids.length === 0 || ob.asks.length === 0) {
      return yield* Effect.fail(
        new ExchangeError("simulated futures fill error: empty order book"),
      );
    }
    return (ob.bids[0].price + ob.asks[0].price) / 2;
  });
}

export function makeSimulatedFuturesExchangeAdapterService(
  gateway: MarketDataGatewayService,
  initialBalances: Readonly<Record<string, number>> = { USDT: 10_000 },
  exchange = "bitget-futures",
): Effect.Effect<FuturesExchangeAdapterService, never, never> {
  return Effect.gen(function* () {
    const stateRef = yield* Ref.make<SimulatedAccountState>({
      balances: Object.fromEntries(
        Object.entries(initialBalances).map(([asset, amount]) => [
          asset,
          money(amount),
        ]),
      ),
      positions: {},
    });

    const getState = () => Ref.get(stateRef);

    const getBalanceInternal = (marginCoin: string) =>
      Effect.map(getState(), (state) => state.balances[marginCoin] ?? money(0));

    const setBalance = (marginCoin: string, amount: Decimal) =>
      Ref.update(stateRef, (state) => ({
        ...state,
        balances: { ...state.balances, [marginCoin]: amount },
      }));

    const getPositionInternal = (
      symbol: string,
      productType: FuturesProductType,
    ) =>
      Effect.map(
        getState(),
        (state) => state.positions[positionKey(symbol, productType)],
      );

    const setPosition = (key: string, position: FuturesPosition | undefined) =>
      Ref.update(stateRef, (state) => {
        const next = { ...state.positions };
        if (position) {
          next[key] = position;
        } else {
          delete next[key];
        }
        return { ...state, positions: next };
      });

    const fillPrice = (
      symbol: string,
      side: "buy" | "sell",
    ): Effect.Effect<number, ExchangeError> =>
      Effect.gen(function* () {
        const mid = yield* midPriceFromOrderBook(gateway, symbol, exchange);
        // Simulate a tiny spread/slippage: buys fill slightly above mid, sells below.
        const slippage = 0.0001;
        return side === "buy" ? mid * (1 + slippage) : mid * (1 - slippage);
      });

    const service: FuturesExchangeAdapterService = {
      placeOrder: (request: FuturesOrderRequest) =>
        Effect.gen(function* () {
          if (request.size <= 0) {
            return yield* Effect.fail(
              new ExchangeError("order size must be positive"),
            );
          }

          const marginCoin = marginCoinFor(request.productType, request.symbol);
          const balance = yield* getBalanceInternal(marginCoin);
          const price = yield* fillPrice(request.symbol, request.side);
          const notional = money(price).times(request.size);
          const marginRequired = notional.div(money(request.leverage));

          if (!request.reduceOnly && balance.lessThan(marginRequired)) {
            return yield* Effect.fail(
              new ExchangeError(
                `insufficient ${marginCoin} margin: required ${marginRequired.toFixed(4)}, available ${balance.toFixed(4)}`,
              ),
            );
          }

          const key = positionKey(request.symbol, request.productType);
          const existing = yield* getPositionInternal(
            request.symbol,
            request.productType,
          );

          if (request.reduceOnly) {
            if (!existing || existing.side === sideToPosition(request.side)) {
              return yield* Effect.fail(
                new ExchangeError("reduce-only order has no opposing position"),
              );
            }
            if (request.size > existing.available) {
              return yield* Effect.fail(
                new ExchangeError(
                  "reduce-only size exceeds available position",
                ),
              );
            }
          }

          const fee = notional.times(0.0006);
          const balanceAfterFill = request.reduceOnly
            ? balance.plus(marginRequired).minus(fee)
            : balance.minus(marginRequired).minus(fee);
          yield* setBalance(marginCoin, balanceAfterFill);

          const fill: FuturesOrderFill = {
            orderId: randomUUID(),
            clientOid: request.clientOid,
            symbol: request.symbol,
            side: request.side,
            productType: request.productType,
            marginMode: request.marginMode,
            filledQty: request.size,
            filledPrice: price,
            fee: toNumber(fee, 8),
            timestamp: new Date(),
          };

          if (request.reduceOnly && existing) {
            const remaining = existing.quantity - request.size;
            const remainingAvailable = existing.available - request.size;
            if (remaining <= 0) {
              yield* setPosition(key, undefined);
            } else {
              yield* setPosition(key, {
                ...existing,
                quantity: remaining,
                available: remainingAvailable,
              });
            }
            return fill;
          }

          if (existing) {
            if (existing.side === sideToPosition(request.side)) {
              const totalQty = existing.quantity + request.size;
              const newEntry =
                (existing.entryPrice * existing.quantity +
                  price * request.size) /
                totalQty;
              yield* setPosition(key, {
                ...existing,
                quantity: totalQty,
                available: existing.available + request.size,
                entryPrice: newEntry,
              });
            } else {
              if (request.size >= existing.quantity) {
                yield* setPosition(key, undefined);
              } else {
                yield* setPosition(key, {
                  ...existing,
                  quantity: existing.quantity - request.size,
                  available: existing.available - request.size,
                });
              }
            }
          } else {
            yield* setPosition(key, {
              symbol: request.symbol,
              side: sideToPosition(request.side),
              productType: request.productType,
              marginMode: request.marginMode,
              leverage: request.leverage,
              quantity: request.size,
              available: request.size,
              entryPrice: price,
              marginCoin,
            });
          }

          return fill;
        }),

      closePosition: (request: ClosePositionRequest) =>
        Effect.gen(function* () {
          const existing = yield* getPositionInternal(
            request.symbol,
            request.productType,
          );
          if (!existing) {
            return null;
          }
          const closeSize = Math.min(request.size, existing.available);
          if (closeSize <= 0) {
            return null;
          }
          const closeSide = existing.side === "long" ? "sell" : "buy";
          return yield* service.placeOrder({
            symbol: request.symbol,
            side: closeSide,
            type: request.price ? "limit" : "market",
            size: closeSize,
            price: request.price,
            productType: request.productType,
            marginMode: request.marginMode,
            leverage: request.leverage,
            reduceOnly: true,
          });
        }),

      getPosition: (symbol, productType) =>
        Effect.map(getPositionInternal(symbol, productType), (p) => p ?? null),

      getBalance: (marginCoin, _productType) =>
        Effect.gen(function* () {
          const amount = yield* getBalanceInternal(marginCoin);
          return {
            marginCoin,
            available: toNumber(amount, 8),
            locked: 0,
            equity: toNumber(amount, 8),
            usdtEquity: toNumber(amount, 8),
          };
        }),

      setLeverage: () => Effect.void,
      setMarginMode: () => Effect.void,
      setPositionMode: () => Effect.void,
    };

    return service;
  });
}

export const SimulatedFuturesExchangeAdapterLive = (
  initialBalances: Readonly<Record<string, number>> = { USDT: 10_000 },
  exchange = "bitget-futures",
) =>
  Layer.effect(
    FuturesExchangeAdapter,
    Effect.gen(function* () {
      const gateway = yield* MarketDataGateway;
      return yield* makeSimulatedFuturesExchangeAdapterService(
        gateway,
        initialBalances,
        exchange,
      );
    }),
  );

function sideToPosition(side: "buy" | "sell"): "long" | "short" {
  return side === "buy" ? "long" : "short";
}

function marginCoinFor(
  productType: FuturesProductType,
  symbol: string,
): string {
  if (productType === "USDT-FUTURES") return "USDT";
  if (productType === "USDC-FUTURES") return "USDC";
  // COIN-FUTURES: crude fallback to base asset.
  return symbol.replace(/(USDT|USDC|USD|_DMCBL)/g, "").toUpperCase() || symbol;
}
