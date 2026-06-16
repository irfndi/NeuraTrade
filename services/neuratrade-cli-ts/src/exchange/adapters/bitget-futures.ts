import { Effect, Layer } from "effect";
import { ExchangeError } from "../adapter.js";
import {
  FuturesExchangeAdapter,
  type FuturesExchangeAdapterService,
  type FuturesBalance,
  type FuturesOrderFill,
  type FuturesOrderRequest,
  type FuturesPosition,
} from "../futures-adapter.js";
import {
  BitgetClient,
  type BitgetClientError,
  type BitgetClientImpl,
  type BitgetFuturesOrderRequest,
  type BitgetProductType,
  toBitgetFuturesSymbol,
} from "../../services/bitget-client.js";

function toExchangeError(error: BitgetClientError): ExchangeError {
  switch (error._tag) {
    case "BitgetApiError":
      return new ExchangeError(
        `Bitget API ${error.status} on ${error.endpoint}: ${error.body.slice(0, 200)}`,
        error,
      );
    case "BitgetNetworkError":
      return new ExchangeError(
        `Bitget network error on ${error.endpoint}: ${error.cause}`,
        error,
      );
    case "BitgetRateLimitError":
      return new ExchangeError(
        `Bitget rate limit on ${error.endpoint}: retry after ${error.retryAfterMs}ms`,
        error,
      );
    case "BitgetAuthError":
      return new ExchangeError(`Bitget auth error: ${error.cause}`, error);
    default:
      return new ExchangeError(`Bitget error: ${JSON.stringify(error)}`, error);
  }
}

function toBitgetProductType(
  productType: FuturesOrderRequest["productType"],
): BitgetProductType {
  return productType === "USDT-FUTURES" ? "USDT-FUTURES" : productType;
}

export function makeBitgetFuturesAdapter(
  client: BitgetClientImpl,
): FuturesExchangeAdapterService {
  const withError = <A>(effect: Effect.Effect<A, BitgetClientError>) =>
    effect.pipe(Effect.mapError(toExchangeError));

  const placeOrder = (request: FuturesOrderRequest) =>
    Effect.gen(function* () {
      const { symbol: bsymbol, productType } = toBitgetFuturesSymbol(
        request.symbol,
        toBitgetProductType(request.productType),
      );
      const order: BitgetFuturesOrderRequest = {
        symbol: bsymbol,
        productType,
        side: request.side,
        orderType: request.type,
        size: String(request.size),
        marginMode: request.marginMode,
        clientOid: request.clientOid,
        reduceOnly: request.reduceOnly,
      };
      const data = yield* withError(client.placeFuturesOrder(order));
      const fill: FuturesOrderFill = {
        orderId: data.orderId,
        clientOid: data.clientOid,
        symbol: request.symbol,
        side: data.side,
        productType: data.productType,
        marginMode: data.marginMode,
        filledQty: Number(data.filledSize) || Number(data.size),
        filledPrice: Number(data.price),
        fee: Number(data.fee),
        timestamp: new Date(),
      };
      return fill;
    });

  const closePosition: FuturesExchangeAdapterService["closePosition"] = (
    request,
  ) =>
    Effect.gen(function* () {
      const positions = yield* withError(
        client.getFuturesPositions(request.symbol, request.productType),
      );
      const existing = positions[0];
      if (!existing) {
        return null;
      }
      const closeSize = Math.min(request.size, Number(existing.available));
      if (closeSize <= 0) {
        return null;
      }
      const closeSide = existing.holdSide === "long" ? "sell" : "buy";
      return yield* placeOrder({
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
    });

  return {
    placeOrder,
    closePosition,

    getPosition: (symbol, productType) =>
      Effect.gen(function* () {
        const positions = yield* withError(
          client.getFuturesPositions(symbol, productType),
        );
        const p = positions[0];
        if (!p) {
          return null;
        }
        const position: FuturesPosition = {
          symbol,
          side: p.holdSide,
          productType: p.productType,
          marginMode: p.marginMode,
          leverage: Number(p.leverage),
          quantity: Number(p.total),
          available: Number(p.available),
          entryPrice: Number(p.openPrice),
          liquidationPrice:
            p.liquidatedPrice === "" || p.liquidatedPrice === "0"
              ? undefined
              : Number(p.liquidatedPrice),
          marginCoin: bitgetMarginCoin(p.productType, symbol),
        };
        return position;
      }),

    getBalance: (marginCoin) =>
      Effect.gen(function* () {
        const balances = yield* withError(
          client.getFuturesBalances("USDT-FUTURES"),
        );
        const match = balances.find(
          (b) => b.marginCoin.toUpperCase() === marginCoin.toUpperCase(),
        );
        const balance: FuturesBalance = {
          marginCoin,
          available: match ? Number(match.available) : 0,
          locked: match ? Number(match.locked) : 0,
          equity: match ? Number(match.equity) : 0,
          usdtEquity: match ? Number(match.usdtEquity) : 0,
        };
        return balance;
      }),

    setLeverage: (symbol, productType, marginMode, leverage, holdSide) =>
      Effect.gen(function* () {
        const { symbol: bsymbol } = toBitgetFuturesSymbol(symbol, productType);
        yield* withError(
          client.setLeverage({
            symbol: bsymbol,
            productType,
            marginMode,
            leverage: String(leverage),
            holdSide,
          }),
        );
      }),

    setMarginMode: (symbol, productType, marginMode) =>
      Effect.gen(function* () {
        const { symbol: bsymbol } = toBitgetFuturesSymbol(symbol, productType);
        yield* withError(
          client.setMarginMode({ symbol: bsymbol, productType, marginMode }),
        );
      }),

    setPositionMode: (productType, positionMode) =>
      withError(
        client.setPositionMode({
          productType,
          positionMode,
        }),
      ),
  };
}

function bitgetMarginCoin(
  productType: BitgetProductType,
  symbol: string,
): string {
  if (productType === "USDT-FUTURES") return "USDT";
  if (productType === "USDC-FUTURES") return "USDC";
  return symbol.replace(/(USDT|USDC|USD|_DMCBL)/g, "").toUpperCase() || symbol;
}

export const BitgetFuturesExchangeAdapterLive: Layer.Layer<
  FuturesExchangeAdapterService,
  never,
  BitgetClient
> = Layer.effect(
  FuturesExchangeAdapter,
  Effect.gen(function* () {
    const client = yield* BitgetClient;
    return makeBitgetFuturesAdapter(client);
  }),
);
