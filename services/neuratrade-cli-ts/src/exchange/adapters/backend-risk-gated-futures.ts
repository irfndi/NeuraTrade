import { Effect, Layer } from "effect";
import * as S from "effect/Schema";
import { ExchangeError } from "../adapter.js";
import {
  FuturesExchangeAdapter,
  type ClosePositionRequest,
  type FuturesExchangeAdapterService,
  type FuturesOrderFill,
  type FuturesPosition,
  type FuturesOrderRequest,
} from "../futures-adapter.js";
import { money } from "../../utils/money.js";

interface BackendRiskGatedConfig {
  readonly baseUrl: string;
  readonly apiKey: string;
  readonly chatId: string;
  readonly timeoutMs?: number;
}

// Backend gate wire contracts (Effect Schema; fail closed on garbage).

const decimalText = S.String.check(
  S.makeFilter<string>(
    (value) => {
      try {
        return money(value).isFinite();
      } catch {
        return false;
      }
    },
    { message: "must be a finite decimal" },
  ),
);

const isoTimestamp = S.String.check(
  S.makeFilter<string>((value) => !Number.isNaN(Date.parse(value)), {
    message: "must be an ISO timestamp",
  }),
);

const nonEmpty = S.String.check(S.isMinLength(1));

const orderResponseSchema = S.Struct({
  intent_id: nonEmpty,
  order_id: nonEmpty,
  client_id: nonEmpty,
  exchange: nonEmpty,
  symbol: nonEmpty,
  side: S.Union([S.Literal("buy"), S.Literal("sell")]),
  filled_qty: decimalText,
  filled_price: decimalText,
  fee: decimalText,
  status: nonEmpty,
  timestamp: isoTimestamp,
});

type OrderResponse = typeof orderResponseSchema.Type;

const positionResponseSchema = S.Struct({
  id: nonEmpty,
  symbol: nonEmpty,
  side: S.Union([S.Literal("long"), S.Literal("short")]),
  product_type: S.Union([
    S.Literal("USDT-FUTURES"),
    S.Literal("COIN-FUTURES"),
    S.Literal("USDC-FUTURES"),
  ]),
  margin_mode: S.Union([S.Literal("isolated"), S.Literal("crossed")]),
  leverage: S.Natural,
  quantity: decimalText,
  available: decimalText,
  entry_price: decimalText,
  liquidation_price: decimalText,
  unrealized_pnl: decimalText,
  margin_coin: nonEmpty,
});

const positionsResponseSchema = S.Struct({
  exchange: nonEmpty,
  positions: S.Array(positionResponseSchema),
  count: S.Natural,
  timestamp: isoTimestamp,
});

type PositionsResponse = typeof positionsResponseSchema.Type;

function comparableSymbol(symbol: string): string {
  return symbol
    .split(":", 1)[0]
    .replace(/[^A-Za-z0-9]/g, "")
    .toUpperCase();
}

function makeAdapter(
  config: BackendRiskGatedConfig,
): FuturesExchangeAdapterService {
  const baseUrl = config.baseUrl.replace(/\/$/, "");
  const timeoutMs = config.timeoutMs ?? 30_000;

  const placeOrder = (request: FuturesOrderRequest) =>
    Effect.gen(function* () {
      if (config.apiKey === "" || config.chatId === "") {
        return yield* Effect.fail(
          new ExchangeError(
            "backend live execution requires ADMIN_API_KEY and TELEGRAM_CHAT_ID",
          ),
        );
      }
      if (request.price === undefined) {
        return yield* Effect.fail(
          new ExchangeError("backend live execution requires an order price"),
        );
      }
      const price = request.price;
      const intentId = request.clientOid ?? crypto.randomUUID();

      const response = yield* Effect.tryPromise({
        try: () =>
          fetch(`${baseUrl}/api/v1/execution/futures/order`, {
            method: "POST",
            headers: {
              "Content-Type": "application/json",
              "X-API-Key": config.apiKey,
            },
            body: JSON.stringify({
              intent_id: intentId,
              chat_id: config.chatId,
              exchange:
                request.productType === "USDT-FUTURES"
                  ? "bitget-futures"
                  : "bitget-futures",
              symbol: request.symbol,
              side: request.side,
              order_type: request.type,
              size: request.size.toString(),
              price: price.toString(),
              product_type: request.productType,
              margin_mode: request.marginMode,
              leverage: request.leverage.toString(),
              reduce_only: request.reduceOnly ?? false,
              portfolio_value: price.times(request.size).toString(),
              current_position: "0",
            }),
            signal:
              timeoutMs === undefined
                ? undefined
                : AbortSignal.timeout(timeoutMs),
          }),
        catch: (error) =>
          new ExchangeError(
            `backend live execution request failed: ${error instanceof Error ? error.message : String(error)}`,
          ),
      });
      if (!response.ok) {
        const body = yield* Effect.tryPromise({
          try: () => response.text(),
          catch: (error) =>
            new ExchangeError(
              `backend live execution error response unavailable: ${error instanceof Error ? error.message : String(error)}`,
            ),
        });
        return yield* Effect.fail(
          new ExchangeError(
            `backend live execution rejected (${response.status}): ${body.slice(0, 240)}`,
          ),
        );
      }
      const payload = yield* Effect.tryPromise({
        try: async () => {
          const body: unknown = await response.json();
          return S.decodeUnknownSync(orderResponseSchema)(body);
        },
        catch: (error) =>
          new ExchangeError(
            `backend live execution returned invalid response: ${error instanceof Error ? error.message : String(error)}`,
          ),
      });
      if (payload.status !== "filled") {
        return yield* Effect.fail(
          new ExchangeError(
            `backend live execution order ${payload.order_id} is not filled (status=${payload.status})`,
          ),
        );
      }
      if (payload.intent_id !== intentId) {
        return yield* Effect.fail(
          new ExchangeError(
            `backend live execution intent mismatch (${payload.intent_id})`,
          ),
        );
      }
      if (
        comparableSymbol(payload.symbol) !== comparableSymbol(request.symbol) ||
        payload.side !== request.side
      ) {
        return yield* Effect.fail(
          new ExchangeError(
            `backend live execution fill identity mismatch for ${request.symbol}`,
          ),
        );
      }
      const filledQty = money(payload.filled_qty);
      const filledPrice = money(payload.filled_price);
      const fee = money(payload.fee);
      if (filledQty.lessThanOrEqualTo(0)) {
        return yield* Effect.fail(
          new ExchangeError(
            "backend live execution returned zero filled quantity",
          ),
        );
      }
      if (filledPrice.lessThanOrEqualTo(0)) {
        return yield* Effect.fail(
          new ExchangeError(
            "backend live execution returned non-positive fill price",
          ),
        );
      }
      if (fee.lessThan(0)) {
        return yield* Effect.fail(
          new ExchangeError("backend live execution returned negative fee"),
        );
      }
      return toFill(payload, request);
    });

  const getPosition = (
    symbol: string,
    productType: FuturesOrderRequest["productType"],
  ) =>
    Effect.gen(function* () {
      if (config.apiKey === "" || config.chatId === "") {
        return yield* Effect.fail(
          new ExchangeError(
            "backend live position lookup requires ADMIN_API_KEY and TELEGRAM_CHAT_ID",
          ),
        );
      }
      const query = new URLSearchParams({
        exchange: "bitget-futures",
        product_type: productType,
      });
      const response = yield* Effect.tryPromise({
        try: () =>
          fetch(
            `${baseUrl}/api/v1/execution/futures/positions?${query.toString()}`,
            {
              headers: { "X-API-Key": config.apiKey },
              signal:
                timeoutMs === undefined
                  ? undefined
                  : AbortSignal.timeout(timeoutMs),
            },
          ),
        catch: (error) =>
          new ExchangeError(
            `backend live position request failed: ${error instanceof Error ? error.message : String(error)}`,
          ),
      });
      if (!response.ok) {
        const body = yield* Effect.tryPromise({
          try: () => response.text(),
          catch: (error) =>
            new ExchangeError(
              `backend live position error response unavailable: ${error instanceof Error ? error.message : String(error)}`,
            ),
        });
        return yield* Effect.fail(
          new ExchangeError(
            `backend live position lookup rejected (${response.status}): ${body.slice(0, 240)}`,
          ),
        );
      }
      const payload: PositionsResponse = yield* Effect.tryPromise({
        try: async () => {
          const body: unknown = await response.json();
          return S.decodeUnknownSync(positionsResponseSchema)(
            body,
          ) as PositionsResponse;
        },
        catch: (error) =>
          new ExchangeError(
            `backend live position response invalid: ${error instanceof Error ? error.message : String(error)}`,
          ),
      });
      const activePositions = payload.positions.filter(
        (position) =>
          position.product_type === productType &&
          comparableSymbol(position.symbol) === comparableSymbol(symbol) &&
          money(position.quantity).greaterThan(0),
      );
      if (activePositions.length > 1) {
        return yield* Effect.fail(
          new ExchangeError(
            `multiple active ${productType} positions returned for ${symbol}`,
          ),
        );
      }
      const position = activePositions.at(0);
      if (!position) {
        return null;
      }
      return {
        symbol,
        side: position.side,
        productType: position.product_type,
        marginMode: position.margin_mode,
        leverage: position.leverage,
        quantity: money(position.quantity),
        available: money(position.available),
        entryPrice: money(position.entry_price),
        liquidationPrice: money(position.liquidation_price).isZero()
          ? undefined
          : money(position.liquidation_price),
        unrealizedPnl: money(position.unrealized_pnl),
        marginCoin: position.margin_coin,
      } satisfies FuturesPosition;
    });

  const service: FuturesExchangeAdapterService = {
    placeOrder,
    closePosition: (request: ClosePositionRequest) =>
      placeOrder({
        symbol: request.symbol,
        side: request.side,
        type: request.price === undefined ? "market" : "limit",
        size: request.size,
        price: request.price,
        productType: request.productType,
        marginMode: request.marginMode,
        leverage: request.leverage,
        reduceOnly: true,
      }),
    getPosition,
    getBalance: (_marginCoin, _productType) =>
      Effect.fail(
        new ExchangeError("backend live balance lookup is unavailable"),
      ),
    setLeverage: () => Effect.void,
    setMarginMode: () => Effect.void,
    setPositionMode: () => Effect.void,
  };

  return service;
}

function toFill(
  payload: OrderResponse,
  request: FuturesOrderRequest,
): FuturesOrderFill {
  return {
    orderId: payload.order_id,
    clientOid: payload.client_id,
    symbol: payload.symbol,
    side: payload.side,
    productType: request.productType,
    marginMode: request.marginMode,
    filledQty: money(payload.filled_qty),
    filledPrice: money(payload.filled_price),
    fee: money(payload.fee),
    timestamp: new Date(payload.timestamp),
  };
}

export const BackendRiskGatedFuturesExchangeAdapterLive = (
  config: BackendRiskGatedConfig,
): Layer.Layer<FuturesExchangeAdapterService> =>
  Layer.succeed(FuturesExchangeAdapter, makeAdapter(config));

export function backendRiskGatedFuturesConfigFromEnv(): BackendRiskGatedConfig {
  return {
    baseUrl:
      process.env.NEURATRADE_BACKEND_URL ??
      process.env.BACKEND_URL ??
      "http://localhost:8080",
    apiKey: process.env.ADMIN_API_KEY ?? "",
    chatId: process.env.TELEGRAM_CHAT_ID ?? "",
  };
}
