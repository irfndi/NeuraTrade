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
  type SetTradingStopRequest,
} from "../futures-adapter.js";
import { ExchangeError } from "../adapter.js";
import {
  MarketDataGateway,
  type MarketDataGatewayService,
} from "../../market-data/gateway.js";
import { Decimal, money } from "../../utils/money.js";
import { marginCoinForProductType } from "../../services/bitget-client.js";

interface SimulatedAccountState {
  readonly balances: Readonly<Record<string, Decimal>>;
  readonly positions: Readonly<Record<string, FuturesPosition>>;
  /** Per-position exchange-side absolute TP/SL, keyed by positionKey. */
  readonly tpsl: Readonly<Record<string, SimulatedTradingStop | undefined>>;
}

export interface SimulatedTradingStop {
  readonly side: "long" | "short";
  /** Absolute take-profit trigger price. Undefined when not set. */
  readonly takeProfit?: Decimal;
  /** Absolute stop-loss trigger price. Undefined when not set. */
  readonly stopLoss?: Decimal;
}

function positionKey(symbol: string, productType: FuturesProductType): string {
  return `${productType}:${symbol}`;
}

/**
 * Check whether a simulated position's exchange-side TP/SL has been hit at a
 * given current price. Long positions take profit above entry and stop out
 * below; short positions are the mirror image.
 */
export function checkTpslHit(
  tpsl: SimulatedTradingStop | undefined,
  price: Decimal,
): "tp" | "sl" | null {
  if (!tpsl) return null;
  if (tpsl.side === "long") {
    if (
      tpsl.takeProfit !== undefined &&
      price.greaterThanOrEqualTo(tpsl.takeProfit)
    ) {
      return "tp";
    }
    if (tpsl.stopLoss !== undefined && price.lessThanOrEqualTo(tpsl.stopLoss)) {
      return "sl";
    }
  } else {
    if (
      tpsl.takeProfit !== undefined &&
      price.lessThanOrEqualTo(tpsl.takeProfit)
    ) {
      return "tp";
    }
    if (
      tpsl.stopLoss !== undefined &&
      price.greaterThanOrEqualTo(tpsl.stopLoss)
    ) {
      return "sl";
    }
  }
  return null;
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
      tpsl: {},
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
        const nextTpsl = { ...state.tpsl };
        if (!position) {
          delete nextTpsl[key];
        }
        return { ...state, positions: next, tpsl: nextTpsl };
      });

    const getTpslInternal = (symbol: string, productType: FuturesProductType) =>
      Effect.map(
        getState(),
        (state) => state.tpsl[positionKey(symbol, productType)],
      );

    const setTpsl = (
      symbol: string,
      productType: FuturesProductType,
      tpsl: SimulatedTradingStop | undefined,
    ) =>
      Ref.update(stateRef, (state) => {
        const next = { ...state.tpsl };
        if (tpsl) {
          next[positionKey(symbol, productType)] = tpsl;
        } else {
          delete next[positionKey(symbol, productType)];
        }
        return { ...state, tpsl: next };
      });

    const fillPrice = (
      symbol: string,
      side: "buy" | "sell",
    ): Effect.Effect<Decimal, ExchangeError> =>
      Effect.gen(function* () {
        const mid = yield* midPriceFromOrderBook(gateway, symbol, exchange);
        // Simulate a tiny spread/slippage: buys fill slightly above mid, sells below.
        const slippage = 0.0001;
        return money(mid).times(side === "buy" ? 1 + slippage : 1 - slippage);
      });

    const service: FuturesExchangeAdapterService & {
      checkTpslHit: (
        symbol: string,
        productType: FuturesProductType,
        price: Decimal,
      ) => Effect.Effect<"tp" | "sl" | null, ExchangeError>;
    } = {
      checkTpslHit: (symbol, productType, price) =>
        Effect.map(getTpslInternal(symbol, productType), (tpsl) =>
          checkTpslHit(tpsl, price),
        ),
      placeOrder: (request: FuturesOrderRequest) =>
        Effect.gen(function* () {
          if (request.size.lessThanOrEqualTo(0)) {
            return yield* Effect.fail(
              new ExchangeError("order size must be positive"),
            );
          }

          const marginCoin = marginCoinFor(request.productType, request.symbol);
          const balance = yield* getBalanceInternal(marginCoin);
          // Limit orders fill at the requested limit price (maker parity with
          // the backtest's fill-at-level model); market orders fill at mid
          // with a tiny simulated spread.
          const price =
            request.price !== undefined
              ? request.price
              : yield* fillPrice(request.symbol, request.side);
          const notional = price.times(request.size);
          const key = positionKey(request.symbol, request.productType);
          const existing = yield* getPositionInternal(
            request.symbol,
            request.productType,
          );

          // An opposite-side order first CLOSES the existing position and only
          // the remainder opens a new one. Treating the whole order as a fresh
          // position would leave the closed leg's margin locked and silently
          // drop the residual quantity on a flip.
          const reducing =
            existing !== undefined &&
            existing.side !== sideToPosition(request.side);
          const closedQty = reducing
            ? Decimal.min(request.size, existing.quantity)
            : money(0);
          const openingQty = request.size.minus(closedQty);
          const openingMargin = price.times(openingQty).div(request.leverage);

          if (!request.reduceOnly && balance.lessThan(openingMargin)) {
            return yield* Effect.fail(
              new ExchangeError(
                `insufficient ${marginCoin} margin: required ${openingMargin.toFixed(4)}, available ${balance.toFixed(4)}`,
              ),
            );
          }

          if (request.reduceOnly) {
            if (!existing || existing.side === sideToPosition(request.side)) {
              return yield* Effect.fail(
                new ExchangeError("reduce-only order has no opposing position"),
              );
            }
            if (request.size.greaterThan(existing.available)) {
              return yield* Effect.fail(
                new ExchangeError(
                  "reduce-only size exceeds available position",
                ),
              );
            }
          }

          const fee = notional.times(0.0006);
          // Margin is released at the leverage the position was OPENED at, not
          // the leverage this order happens to carry.
          const heldLeverage = (position: FuturesPosition): number =>
            position.leverage > 0 ? position.leverage : request.leverage;
          const closedMargin = reducing
            ? existing.entryPrice.times(closedQty).div(heldLeverage(existing))
            : money(0);
          const realizedPnl = reducing
            ? price
                .minus(existing.entryPrice)
                .times(closedQty)
                .times(existing.side === "long" ? 1 : -1)
            : money(0);
          const balanceAfterFill = request.reduceOnly
            ? balance
                .plus(
                  existing.entryPrice
                    .times(request.size)
                    .div(heldLeverage(existing)),
                )
                .plus(
                  price
                    .minus(existing.entryPrice)
                    .times(request.size)
                    .times(existing.side === "long" ? 1 : -1),
                )
                .minus(fee)
            : balance
                .plus(closedMargin)
                .plus(realizedPnl)
                .minus(openingMargin)
                .minus(fee);
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
            fee,
            timestamp: new Date(),
            leverage: request.leverage,
          };

          if (request.reduceOnly && existing) {
            const remaining = existing.quantity.minus(request.size);
            const remainingAvailable = existing.available.minus(request.size);
            if (remaining.lessThanOrEqualTo(0)) {
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

          if (reducing) {
            if (openingQty.greaterThan(0)) {
              // Flipped: the closed leg is gone, the remainder is a new
              // position on the other side, entered at this fill price.
              yield* setPosition(key, {
                symbol: request.symbol,
                side: sideToPosition(request.side),
                productType: request.productType,
                marginMode: request.marginMode,
                leverage: request.leverage,
                quantity: openingQty,
                available: openingQty,
                entryPrice: price,
                marginCoin,
              });
            } else {
              const remaining = existing.quantity.minus(closedQty);
              const remainingAvailable = existing.available.minus(closedQty);
              if (remaining.lessThanOrEqualTo(0)) {
                yield* setPosition(key, undefined);
              } else {
                yield* setPosition(key, {
                  ...existing,
                  quantity: remaining,
                  available: remainingAvailable,
                });
              }
            }
          } else if (existing) {
            const totalQty = existing.quantity.plus(request.size);
            const newEntry = existing.entryPrice
              .times(existing.quantity)
              .plus(price.times(request.size))
              .div(totalQty);
            yield* setPosition(key, {
              ...existing,
              quantity: totalQty,
              available: existing.available.plus(request.size),
              entryPrice: newEntry,
            });
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
          const closeSize = Decimal.min(request.size, existing.available);
          if (closeSize.lessThanOrEqualTo(0)) {
            // A live position that cannot be closed (nothing available —
            // e.g. in-flight fills) is a real anomaly; returning null here
            // would mask an open-order/fill divergence as a silent no-op.
            return yield* Effect.fail(
              new ExchangeError(
                `nothing closable for ${request.productType}:${request.symbol}: available 0 with open position`,
              ),
            );
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
            available: amount,
            locked: money(0),
            equity: amount,
            usdtEquity: amount,
          };
        }),

      setTradingStop: (request: SetTradingStopRequest) =>
        Effect.gen(function* () {
          const existing = yield* getPositionInternal(
            request.symbol,
            request.productType,
          );
          if (!existing || existing.side !== request.side) {
            return yield* Effect.fail(
              new ExchangeError(
                `no ${request.side} position for ${request.productType}:${request.symbol}`,
              ),
            );
          }
          const current = yield* getTpslInternal(
            request.symbol,
            request.productType,
          );
          yield* setTpsl(request.symbol, request.productType, {
            side: request.side,
            takeProfit:
              request.takeProfit !== undefined
                ? request.takeProfit
                : current?.takeProfit,
            stopLoss:
              request.stopLoss !== undefined
                ? request.stopLoss
                : current?.stopLoss,
          });
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
  return marginCoinForProductType(productType, symbol);
}
