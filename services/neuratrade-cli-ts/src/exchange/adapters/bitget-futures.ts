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
import { validateFuturesOrder } from "../../services/bitget-futures-guards.js";
import { validateLiveOrderSafety } from "../../services/bitget-futures-safety.js";

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

  const runPreTradeGuard = (order: BitgetFuturesOrderRequest) =>
    Effect.gen(function* () {
      const [contracts, balances, ticker] = yield* Effect.all([
        withError(client.getContracts(order.productType)),
        withError(client.getFuturesBalances(order.productType)),
        withError(client.getFuturesTicker(order.symbol, order.productType)),
      ]);
      const { symbol: bsymbol } = toBitgetFuturesSymbol(
        order.symbol,
        order.productType,
      );
      const contract = contracts.find(
        (c) => c.symbol.toUpperCase() === bsymbol.toUpperCase(),
      );
      if (contract === undefined) {
        return yield* Effect.fail(
          new ExchangeError(`contract ${bsymbol} not found`),
        );
      }
      yield* validateFuturesOrder({
        order,
        contract,
        balances,
        lastPrice: ticker.lastPrice,
        leverage: String(order.leverage ?? 1),
      }).pipe(
        Effect.mapError(
          (err) =>
            new ExchangeError(`futures guard rejected: ${err.reason}`, err),
        ),
      );
    });

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
        price: request.price !== undefined ? String(request.price) : undefined,
        leverage: request.leverage,
      };

      // Pre-trade guards must run inside the adapter so both the direct CLI
      // order path and the live paper-trading path reject unsafe orders before
      // any signed request leaves the process.
      yield* runPreTradeGuard(order);

      // Account-state safety checks catch reduce-only mismatches, margin-mode
      // conflicts, and leverage mismatches before a signed order is sent.
      const [positions, leverageInfo] = yield* Effect.all([
        withError(client.getFuturesPositions(order.symbol, order.productType)),
        withError(
          client.getLeverage({
            symbol: order.symbol,
            productType: order.productType,
          }),
        ),
      ]);
      yield* validateLiveOrderSafety({
        order,
        positions,
        leverageInfo,
        intendedLeverage: order.leverage ? String(order.leverage) : undefined,
      }).pipe(
        Effect.mapError(
          (err) =>
            new ExchangeError(`futures safety rejected: ${err.reason}`, err),
        ),
      );

      const ack = yield* withError(client.placeFuturesOrder(order));

      // Bitget's place-order response is an acknowledgement. Query the order
      // detail endpoint to obtain the actual filled size, price and fee before
      // the trading engine records the position.
      const data = yield* withError(
        client.getFuturesOrder({
          symbol: order.symbol,
          productType: order.productType,
          orderId: ack.orderId || undefined,
          clientOid: ack.clientOid || undefined,
        }),
      );

      const filledQty = Number(data.filledSize);
      const filledPrice = Number(data.priceAvg);
      if (filledQty <= 0 || filledPrice <= 0) {
        return yield* Effect.fail(
          new ExchangeError(
            `futures order ${data.orderId} not filled (status=${data.status}, filledQty=${filledQty}, filledPrice=${filledPrice})`,
          ),
        );
      }

      const fill: FuturesOrderFill = {
        orderId: data.orderId,
        clientOid: data.clientOid,
        symbol: request.symbol,
        side: data.side,
        productType: data.productType,
        marginMode: data.marginMode,
        filledQty,
        filledPrice,
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
      // In hedge mode there can be both a long and a short for the same
      // symbol. Close the leg that opposes the requested side: a sell close
      // reduces a long, a buy close reduces a short.
      const neededSide = request.side === "sell" ? "long" : "short";
      const existing = positions.find((p) => p.holdSide === neededSide);
      if (!existing) {
        return null;
      }
      const closeSize = Math.min(request.size, Number(existing.available));
      if (closeSize <= 0) {
        return null;
      }
      return yield* placeOrder({
        symbol: request.symbol,
        side: request.side,
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

    getBalance: (marginCoin, productType) =>
      Effect.gen(function* () {
        const inferredProductType =
          productType ??
          (marginCoin.toUpperCase() === "USDC"
            ? "USDC-FUTURES"
            : "USDT-FUTURES");
        const balances = yield* withError(
          client.getFuturesBalances(inferredProductType),
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
