import { Effect, Layer } from "effect";
import { ExchangeError } from "../adapter.js";
import {
  FuturesExchangeAdapter,
  type ClosePositionRequest,
  type FuturesExchangeAdapterService,
  type FuturesOrderFill,
  type FuturesOrderRequest,
} from "../futures-adapter.js";
import { money } from "../../utils/money.js";

interface BackendRiskGatedConfig {
  readonly baseUrl: string;
  readonly apiKey: string;
  readonly chatId: string;
  readonly timeoutMs?: number;
}

interface OrderResponse {
  readonly intent_id: string;
  readonly order_id: string;
  readonly client_id: string;
  readonly exchange: string;
  readonly symbol: string;
  readonly side: "buy" | "sell";
  readonly filled_qty: string;
  readonly filled_price: string;
  readonly fee: string;
  readonly status: string;
  readonly timestamp: string;
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

      const response = yield* Effect.tryPromise({
        try: () =>
          fetch(`${baseUrl}/api/v1/execution/futures/order`, {
            method: "POST",
            headers: {
              "Content-Type": "application/json",
              "X-API-Key": config.apiKey,
            },
            signal: AbortSignal.timeout(timeoutMs),
            body: JSON.stringify({
              intent_id: request.clientOid ?? crypto.randomUUID(),
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
        try: () => response.json() as Promise<OrderResponse>,
        catch: (error) =>
          new ExchangeError(
            `backend live execution returned invalid JSON: ${error instanceof Error ? error.message : String(error)}`,
          ),
      });
      if (payload.status !== "filled") {
        return yield* Effect.fail(
          new ExchangeError(
            `backend live execution order ${payload.order_id} is not filled (status=${payload.status})`,
          ),
        );
      }
      return toFill(payload);
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
    getPosition: (_symbol, _productType) =>
      Effect.fail(
        new ExchangeError("backend live position lookup is unavailable"),
      ),
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

function toFill(payload: OrderResponse): FuturesOrderFill {
  return {
    orderId: payload.order_id,
    clientOid: payload.client_id,
    symbol: payload.symbol,
    side: payload.side,
    productType: "USDT-FUTURES",
    marginMode: "crossed",
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
