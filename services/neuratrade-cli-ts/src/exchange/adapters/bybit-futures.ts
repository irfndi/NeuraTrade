/**
 * Bybit linear (USDT-perp) futures adapter.
 *
 * Mirrors the Bitget futures adapter shape: contract-aware sizing against
 * instruments-info (effectiveFloor = max(minOrderAmt, minQty * price), qty
 * rounded UP to qtyStep, leverage raise capped by maxLeverage, margin-based
 * cap), ExchangeError mapping for every client failure, and the same
 * FuturesExchangeAdapterService surface (placeOrder/getBalance/getPosition/
 * closePosition/setLeverage/setMarginMode/setPositionMode).
 *
 * Differences from Bitget: the reference price for sizing comes from the
 * MarketDataGateway (shared market-data source) instead of a private ticker
 * call, and the REST client is embedded here (no separate bybit-client.ts).
 *
 * Bybit v5 auth: X-BAPI-API-KEY / X-BAPI-TIMESTAMP / X-BAPI-RECV-WINDOW /
 * X-BAPI-SIGN = HMAC-SHA256(secret, `${ts}${apiKey}${recvWindow}${body}`)
 * where body is the raw JSON for POST and the query string (with empty HTTP
 * body) for GET.
 */
import { Context, Data, Effect, Layer } from "effect";
import { createHmac } from "node:crypto";
import { ExchangeError } from "../adapter.js";
import {
  FuturesExchangeAdapter,
  type FuturesExchangeAdapterService,
  type FuturesBalance,
  type FuturesOrderFill,
  type FuturesOrderRequest,
  type FuturesPosition,
} from "../futures-adapter.js";
import { MarketDataGateway } from "../../market-data/gateway.js";
import type { MarketDataGatewayService } from "../../market-data/gateway.js";
import {
  BybitConfig,
  requireBybitCredentials,
  type BybitCredentials,
} from "../../services/bybit-config.js";
import { Decimal, money, type Money } from "../../utils/money.js";

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const BYBIT_LIVE_URL = "https://api.bybit.com";
const BYBIT_TESTNET_URL = "https://api-testnet.bybit.com";
const DEFAULT_TIMEOUT_MS = 30000;
const RECV_WINDOW_MS = "5000";

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

export class BybitNetworkError extends Data.TaggedError("BybitNetworkError")<{
  readonly cause: string;
  readonly endpoint: string;
}> {}

export class BybitRateLimitError extends Data.TaggedError(
  "BybitRateLimitError",
)<{
  readonly retryAfterMs: number;
  readonly endpoint: string;
}> {}

export class BybitAuthError extends Data.TaggedError("BybitAuthError")<{
  readonly cause: string;
}> {}

export class BybitApiError extends Data.TaggedError("BybitApiError")<{
  readonly status: number;
  readonly body: string;
  readonly endpoint: string;
  /** Bybit business retCode, when the body carries one. */
  readonly code?: string;
}> {}

export type BybitClientError =
  | BybitNetworkError
  | BybitRateLimitError
  | BybitAuthError
  | BybitApiError;

function toExchangeError(error: BybitClientError): ExchangeError {
  switch (error._tag) {
    case "BybitApiError":
      return new ExchangeError(
        `Bybit API ${error.status} on ${error.endpoint}: ${error.body.slice(0, 200)}`,
        error,
      );
    case "BybitNetworkError":
      return new ExchangeError(
        `Bybit network error on ${error.endpoint}: ${error.cause}`,
        error,
      );
    case "BybitRateLimitError":
      return new ExchangeError(
        `Bybit rate limit on ${error.endpoint}: retry after ${error.retryAfterMs}ms`,
        error,
      );
    case "BybitAuthError":
      return new ExchangeError(`Bybit auth error: ${error.cause}`, error);
    default:
      return new ExchangeError(`Bybit error: ${JSON.stringify(error)}`, error);
  }
}

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export type BybitOrderSide = "Buy" | "Sell";
export type BybitOrderType = "Market" | "Limit";

/** Contract-level size constraints from /v5/market/instruments-info. */
export interface BybitContract {
  readonly symbol: string;
  readonly status: string;
  /** lotSizeFilter.minOrderQty — minimum order quantity in base units. */
  readonly minOrderQty: string;
  /** lotSizeFilter.qtyStep — quantity step. */
  readonly qtyStep: string;
  /** lotSizeFilter.minOrderAmt — minimum order notional in quote units. */
  readonly minOrderAmt: string;
  /** priceFilter.tickSize — price tick. */
  readonly tickSize: string;
  /** leverageFilter.maxLeverage — maximum leverage. */
  readonly maxLeverage: string;
}

export interface BybitWalletCoin {
  readonly coin: string;
  readonly equity: string;
  readonly walletBalance: string;
  readonly availableToWithdraw: string;
  readonly usdValue: string;
}

export interface BybitPosition {
  readonly symbol: string;
  readonly side: BybitOrderSide;
  readonly size: string;
  readonly avgPrice: string;
  readonly unrealisedPnl: string;
  readonly liqPrice: string;
  readonly leverage: string;
  /** 0 = cross, 1 = isolated. */
  readonly tradeMode: number;
  readonly positionIdx: number;
}

export interface BybitOrderRequest {
  readonly symbol: string;
  readonly side: BybitOrderSide;
  readonly orderType: BybitOrderType;
  readonly qty: string;
  readonly price?: string;
  readonly reduceOnly?: boolean;
}

export interface BybitOrderAck {
  readonly orderId: string;
  readonly clientOrderId?: string;
}

export interface BybitOrder extends BybitOrderAck {
  readonly symbol: string;
  readonly side: BybitOrderSide;
  readonly orderType: string;
  readonly orderStatus: string;
  readonly qty: string;
  readonly price: string;
  readonly avgPrice: string;
  readonly cumExecQty: string;
  readonly cumExecFee: string;
}

// ---------------------------------------------------------------------------
// Symbol normalization
// ---------------------------------------------------------------------------

/** "BTC/USDT:USDT" / "BTC/USDT" -> "BTCUSDT". */
export function toBybitSymbol(symbol: string): string {
  return symbol.replace("/", "").split(":")[0].toUpperCase();
}

/**
 * Hedge-mode position leg for an order: 1 = long, 2 = short. A reduce-only
 * order acts on the opposing leg (Sell closes the long, Buy closes the
 * short); a regular order opens the leg matching its side. One-way mode
 * always uses 0, which is invalid for hedge-mode accounts.
 */
export function bybitPositionIdx(
  side: BybitOrderSide,
  reduceOnly: boolean,
): number {
  if (reduceOnly) {
    return side === "Sell" ? 1 : 2;
  }
  return side === "Buy" ? 1 : 2;
}

// ---------------------------------------------------------------------------
// Authentication
// ---------------------------------------------------------------------------

function bybitSign(secret: string, payload: string): string {
  return createHmac("sha256", secret).update(payload).digest("hex");
}

function bybitAuthHeaders(
  credentials: BybitCredentials,
  body: string,
): Record<string, string> {
  const timestamp = String(Date.now());
  const payload = `${timestamp}${credentials.apiKey}${RECV_WINDOW_MS}${body}`;
  return {
    "Content-Type": "application/json",
    "X-BAPI-API-KEY": credentials.apiKey,
    "X-BAPI-TIMESTAMP": timestamp,
    "X-BAPI-RECV-WINDOW": RECV_WINDOW_MS,
    "X-BAPI-SIGN": bybitSign(credentials.apiSecret, payload),
  };
}

// ---------------------------------------------------------------------------
// Internal fetch helper
// ---------------------------------------------------------------------------

function fetchBybit<T>(
  baseUrl: string,
  method: "GET" | "POST",
  path: string,
  options: {
    readonly query?: Record<string, string | number>;
    readonly body?: unknown;
    readonly signed?: boolean;
  },
  credentials?: BybitCredentials,
): Effect.Effect<T, BybitClientError> {
  return Effect.gen(function* () {
    const query = new URLSearchParams(
      Object.entries(options.query ?? {}).map(([k, v]) => [k, String(v)]),
    ).toString();
    const url = `${baseUrl}${path}${query ? `?${query}` : ""}`;
    const bodyString =
      options.body === undefined ? "" : JSON.stringify(options.body);
    const headers: Record<string, string> = {};
    if (options.signed === true && credentials !== undefined) {
      // GET signs the query string (no HTTP body); POST signs the raw JSON.
      Object.assign(
        headers,
        bybitAuthHeaders(credentials, method === "GET" ? query : bodyString),
      );
    }

    const response = yield* Effect.tryPromise({
      try: () =>
        fetch(url, {
          method,
          headers: { Accept: "application/json", ...headers },
          body:
            method === "POST" && options.body !== undefined
              ? bodyString
              : undefined,
          signal: AbortSignal.timeout(DEFAULT_TIMEOUT_MS),
        }),
      catch: (error): BybitClientError => {
        if (error instanceof DOMException && error.name === "TimeoutError") {
          return new BybitNetworkError({
            cause: `request timed out after ${DEFAULT_TIMEOUT_MS}ms`,
            endpoint: path,
          });
        }
        return new BybitNetworkError({
          cause: error instanceof Error ? error.message : String(error),
          endpoint: path,
        });
      },
    });

    if (response.status === 429) {
      // Retry-After is normally delta-seconds, but may be an HTTP-date on
      // some proxies. Number() on an HTTP-date yields NaN; falling back to 0
      // would busy-loop into another 429 immediately, so default to a sane
      // constant backoff instead. Mirrors bitget-client.ts fetchBitget.
      const rawRetryAfter = response.headers.get("Retry-After");
      const retryAfter = Number(rawRetryAfter || "0");
      const retryAfterMs = Number.isFinite(retryAfter) && retryAfter > 0
        ? retryAfter * 1000
        : 5000;
      if (rawRetryAfter && !Number.isFinite(retryAfter)) {
        console.warn(
          `[bybit-futures] unparseable Retry-After header ${JSON.stringify(rawRetryAfter)} on ${path}; falling back to ${retryAfterMs}ms`,
        );
      }
      return yield* Effect.fail(
        new BybitRateLimitError({ retryAfterMs, endpoint: path }),
      );
    }

    const responseBody = yield* Effect.promise(() =>
      response.text().catch(() => ""),
    );

    if (!response.ok) {
      return yield* Effect.fail(
        new BybitApiError({
          status: response.status,
          body: responseBody,
          endpoint: path,
        }),
      );
    }

    const parsed = yield* Effect.tryPromise({
      try: () => Promise.resolve(JSON.parse(responseBody) as unknown),
      catch: (error): BybitClientError =>
        new BybitApiError({
          status: response.status,
          body: error instanceof Error ? error.message : String(error),
          endpoint: path,
        }),
    });

    if (
      parsed !== null &&
      typeof parsed === "object" &&
      "retCode" in parsed &&
      typeof parsed.retCode === "number" &&
      parsed.retCode !== 0
    ) {
      const retCode = parsed.retCode;
      const retMsg =
        "retMsg" in parsed && typeof parsed.retMsg === "string"
          ? parsed.retMsg
          : "";
      if (retCode === 10004 || retCode === 10006) {
        return yield* Effect.fail(
          new BybitAuthError({ cause: `${retCode}: ${retMsg}` }),
        );
      }
      return yield* Effect.fail(
        new BybitApiError({
          status: response.status,
          body: `${retCode}: ${retMsg}`,
          endpoint: path,
          code: String(retCode),
        }),
      );
    }

    const result =
      parsed !== null && typeof parsed === "object" && "result" in parsed
        ? parsed.result
        : undefined;
    return result as T;
  });
}

// ---------------------------------------------------------------------------
// Response parsers
// ---------------------------------------------------------------------------

function asString(value: unknown, fallback = ""): string {
  return typeof value === "string" && value !== "" ? value : fallback;
}

function asRecord(value: unknown): Record<string, unknown> {
  return value !== null && typeof value === "object"
    ? (value as Record<string, unknown>)
    : {};
}

function parseContract(data: unknown): BybitContract {
  const rec = asRecord(data);
  const lot = asRecord(rec.lotSizeFilter);
  const price = asRecord(rec.priceFilter);
  const leverage = asRecord(rec.leverageFilter);
  return {
    symbol: asString(rec.symbol),
    status: asString(rec.status),
    minOrderQty: asString(lot.minOrderQty, "0"),
    qtyStep: asString(lot.qtyStep, "0"),
    minOrderAmt: asString(lot.minOrderAmt, "0"),
    tickSize: asString(price.tickSize, "0"),
    maxLeverage: asString(leverage.maxLeverage, "1"),
  };
}

function parseCoin(data: unknown): BybitWalletCoin {
  const rec = asRecord(data);
  return {
    coin: asString(rec.coin),
    equity: asString(rec.equity, "0"),
    walletBalance: asString(rec.walletBalance, "0"),
    availableToWithdraw: asString(rec.availableToWithdraw, "0"),
    usdValue: asString(rec.usdValue, "0"),
  };
}

function parsePosition(data: unknown): BybitPosition {
  const rec = asRecord(data);
  return {
    symbol: asString(rec.symbol),
    side: rec.side === "Sell" ? "Sell" : "Buy",
    size: asString(rec.size, "0"),
    avgPrice: asString(rec.avgPrice, "0"),
    unrealisedPnl: asString(rec.unrealisedPnl, "0"),
    liqPrice: asString(rec.liqPrice, "0"),
    leverage: asString(rec.leverage, "1"),
    tradeMode: typeof rec.tradeMode === "number" ? rec.tradeMode : 0,
    positionIdx: typeof rec.positionIdx === "number" ? rec.positionIdx : 0,
  };
}

function parseOrder(data: unknown): BybitOrder {
  const rec = asRecord(data);
  return {
    orderId: asString(rec.orderId),
    clientOrderId: asString(rec.orderLinkId),
    symbol: asString(rec.symbol),
    side: rec.side === "Sell" ? "Sell" : "Buy",
    orderType: asString(rec.orderType),
    orderStatus: asString(rec.orderStatus),
    qty: asString(rec.qty, "0"),
    price: asString(rec.price, "0"),
    avgPrice: asString(rec.avgPrice, "0"),
    cumExecQty: asString(rec.cumExecQty, "0"),
    cumExecFee: asString(rec.cumExecFee, "0"),
  };
}

// ---------------------------------------------------------------------------
// Service interface
// ---------------------------------------------------------------------------

export interface BybitClientImpl {
  readonly getContract: (
    symbol: string,
  ) => Effect.Effect<BybitContract, BybitClientError>;
  readonly getBalance: () => Effect.Effect<
    ReadonlyArray<BybitWalletCoin>,
    BybitClientError
  >;
  readonly getPositions: (
    symbol: string,
  ) => Effect.Effect<ReadonlyArray<BybitPosition>, BybitClientError>;
  readonly placeOrder: (
    order: BybitOrderRequest,
  ) => Effect.Effect<BybitOrderAck, BybitClientError>;
  readonly getOrder: (args: {
    symbol: string;
    orderId: string;
  }) => Effect.Effect<BybitOrder, BybitClientError>;
  /** All resting open orders for a symbol (realtime endpoint, no orderId). */
  readonly getOpenOrders: (
    symbol: string,
  ) => Effect.Effect<ReadonlyArray<BybitOrder>, BybitClientError>;
  readonly cancelOrder: (args: {
    symbol: string;
    orderId: string;
  }) => Effect.Effect<void, BybitClientError>;
  readonly setLeverage: (args: {
    symbol: string;
    buyLeverage: string;
    sellLeverage: string;
  }) => Effect.Effect<void, BybitClientError>;
  readonly setMarginMode: (args: {
    symbol: string;
    marginMode: "isolated" | "crossed";
  }) => Effect.Effect<void, BybitClientError>;
  readonly setPositionMode: (args: {
    mode: "one_way" | "hedge_mode";
  }) => Effect.Effect<void, BybitClientError>;
}

export class BybitClient extends Context.Service<
  BybitClient,
  BybitClientImpl
>()("BybitClient") {}

// ---------------------------------------------------------------------------
// Live client
// ---------------------------------------------------------------------------

function makeBybitClientImpl(
  credentials: BybitCredentials,
  baseUrl: string,
): BybitClientImpl {
  const get = <T>(
    path: string,
    query: Record<string, string | number> = {},
    signed = true,
  ) => fetchBybit<T>(baseUrl, "GET", path, { query, signed }, credentials);
  const post = <T>(path: string, body: unknown) =>
    fetchBybit<T>(baseUrl, "POST", path, { body, signed: true }, credentials);

  // Account position mode, kept in sync with setPositionMode so placeOrder
  // can pick the correct hedge-mode leg (0 is only valid for one-way).
  let positionMode: "one_way" | "hedge_mode" = "one_way";

  return {
    getContract: (symbol) =>
      get<{ list?: ReadonlyArray<unknown> }>("/v5/market/instruments-info", {
        category: "linear",
        symbol,
      }, false).pipe(
        Effect.map((result) => {
          const item = result?.list?.[0];
          return parseContract(item ?? {});
        }),
      ),
    getBalance: () =>
      get<{ list?: ReadonlyArray<{ coin?: ReadonlyArray<unknown> }> }>(
        "/v5/wallet/balance",
        { accountType: "UNIFIED" },
      ).pipe(
        Effect.map((result) =>
          (result?.list?.[0]?.coin ?? []).map((coin) => parseCoin(coin)),
        ),
      ),
    getPositions: (symbol) =>
      get<{ list?: ReadonlyArray<unknown> }>("/v5/position/list", {
        category: "linear",
        symbol,
      }).pipe(
        Effect.map((result) =>
          (result?.list ?? []).map((position) => parsePosition(position)),
        ),
      ),
    placeOrder: (order) =>
      post<{ orderId?: string; orderLinkId?: string }>("/v5/order/create", {
        category: "linear",
        symbol: order.symbol,
        side: order.side,
        orderType: order.orderType,
        qty: order.qty,
        ...(order.orderType === "Limit" ? { timeInForce: "GTC" } : {}),
        ...(order.price !== undefined ? { price: order.price } : {}),
        positionIdx:
          positionMode === "hedge_mode"
            ? bybitPositionIdx(order.side, order.reduceOnly === true)
            : 0,
        ...(order.reduceOnly === true ? { reduceOnly: true } : {}),
      }).pipe(
        Effect.map((ack) => ({
          orderId: ack?.orderId ?? "",
          clientOrderId: ack?.orderLinkId,
        })),
      ),
    getOrder: ({ symbol, orderId }) =>
      get<{ list?: ReadonlyArray<unknown> }>("/v5/order/realtime", {
        category: "linear",
        symbol,
        orderId,
      }).pipe(
        Effect.map((result) => {
          const item = result?.list?.[0];
          return parseOrder(item ?? {});
        }),
      ),
    getOpenOrders: (symbol) =>
      get<{ list?: ReadonlyArray<unknown> }>("/v5/order/realtime", {
        category: "linear",
        symbol,
      }).pipe(
        Effect.map((result) =>
          (result?.list ?? []).map((order) => parseOrder(order)),
        ),
      ),
    cancelOrder: ({ symbol, orderId }) =>
      post<unknown>("/v5/order/cancel", {
        category: "linear",
        symbol,
        orderId,
      }).pipe(Effect.as(undefined)),
    setLeverage: ({ symbol, buyLeverage, sellLeverage }) =>
      post<unknown>("/v5/position/set-leverage", {
        category: "linear",
        symbol,
        buyLeverage,
        sellLeverage,
      }).pipe(Effect.as(undefined)),
    setMarginMode: ({ symbol, marginMode }) =>
      post<unknown>("/v5/position/set-margin-mode", {
        category: "linear",
        symbol,
        marginMode: marginMode === "isolated" ? "ISOLATED" : "CROSSED",
      }).pipe(Effect.as(undefined)),
    setPositionMode: ({ mode }) =>
      post<unknown>("/v5/position/set-mode", {
        category: "linear",
        mode: mode === "hedge_mode" ? 3 : 0,
      }).pipe(
        Effect.tap(() =>
          Effect.sync(() => {
            positionMode = mode;
          }),
        ),
        Effect.as(undefined),
      ),
  };
}

// ---------------------------------------------------------------------------
// Adapter
// ---------------------------------------------------------------------------

function normalizePrice(price: Money, tickSize: Money): Money {
  return tickSize.greaterThan(0)
    ? price.div(tickSize).round().times(tickSize)
    : price;
}

export function makeBybitFuturesAdapter(
  client: BybitClientImpl,
  gateway: MarketDataGatewayService,
): FuturesExchangeAdapterService {
  const withError = <A>(effect: Effect.Effect<A, BybitClientError>) =>
    effect.pipe(Effect.mapError(toExchangeError));

  const placeOrder = (request: FuturesOrderRequest) =>
    Effect.gen(function* () {
      const symbol = toBybitSymbol(request.symbol);
      const contract = yield* withError(client.getContract(symbol));
      if (contract.symbol.toUpperCase() !== symbol) {
        return yield* Effect.fail(
          new ExchangeError(`contract ${symbol} not found`),
        );
      }
      const contractStatus = contract.status;
      if (contractStatus !== "" && contractStatus !== "Trading") {
        return yield* Effect.fail(
          new ExchangeError(
            `contract ${symbol} is not tradable (status=${contractStatus})`,
          ),
        );
      }

      const minQty = money(contract.minOrderQty);
      const qtyStep = money(contract.qtyStep);
      const minOrderAmt = money(contract.minOrderAmt);
      const maxLeverage = money(contract.maxLeverage);
      const tickSize = money(contract.tickSize);

      // Reference price: the order's limit price (tick-normalized) when
      // present, otherwise the market-data gateway's tick for the canonical
      // symbol. Market orders drop the price from the request entirely.
      const reference = yield* (request.price !== undefined
        ? Effect.succeed(request.price)
        : gateway.fetchTick("bybit-futures", request.symbol).pipe(
            Effect.mapError(
              (err) =>
                new ExchangeError(
                  `bybit reference price failed for ${request.symbol}: ${err.reason}`,
                  err,
                ),
            ),
            Effect.map((tick) => money(tick.price)),
          ));
      const price =
        request.type === "limit" ? normalizePrice(reference, tickSize) : reference;

      const rawQty = request.size;
      let qty = rawQty;
      let leverage = request.leverage;
      if (request.reduceOnly !== true) {
        if (rawQty.lessThanOrEqualTo(0)) {
          return yield* Effect.fail(
            new ExchangeError(
              `futures guard rejected: order size ${rawQty.toString()} must be positive for ${symbol}`,
            ),
          );
        }
        if (rawQty.lessThan(minQty)) {
          return yield* Effect.fail(
            new ExchangeError(
              `futures guard rejected: order size ${rawQty.toString()} below min order qty ${minQty.toString()} for ${symbol}`,
            ),
          );
        }

        // Contract-spec sizing: effectiveFloor = max(minOrderAmt, minQty *
        // price). When the requested notional sits below the floor, size the
        // qty UP to the floor (rounded UP to qtyStep) and raise leverage so
        // the floor's margin fits the requested notional budget — bounded by
        // both the operator's requested leverage and the contract's
        // maxLeverage.
        const requestedNotional = rawQty.times(price);
        const effectiveFloor = Decimal.max(minOrderAmt, minQty.times(price));
        if (requestedNotional.lessThan(effectiveFloor)) {
          qty = qtyStep.greaterThan(0)
            ? effectiveFloor.div(price).div(qtyStep).ceil().times(qtyStep)
            : effectiveFloor.div(price);
          if (qty.lessThan(minQty)) qty = minQty;
          const raised = Decimal.max(
            1,
            effectiveFloor.div(requestedNotional).ceil(),
          );
          leverage = Decimal.min(
            raised,
            Decimal.min(money(request.leverage), maxLeverage),
          ).toNumber();
        } else if (qtyStep.greaterThan(0)) {
          qty = qty.div(qtyStep).ceil().times(qtyStep);
        }

        // Margin-based cap: the sized order's margin (notional / leverage)
        // must fit the wallet's available USDT, mirroring the bitget guard's
        // fail-closed insufficient-margin rejection.
        const coins = yield* withError(client.getBalance());
        const usdt = coins.find((c) => c.coin.toUpperCase() === "USDT");
        const available = money(
          usdt?.availableToWithdraw || usdt?.walletBalance || "0",
        );
        const margin = qty.times(price).div(money(leverage));
        if (margin.greaterThan(available)) {
          return yield* Effect.fail(
            new ExchangeError(
              `futures guard rejected: insufficient USDT margin: available ${available.toString()}, required ~${margin.toString()} for ${symbol}`,
            ),
          );
        }

        if (leverage !== request.leverage) {
          yield* withError(
            client.setLeverage({
              symbol,
              buyLeverage: String(leverage),
              sellLeverage: String(leverage),
            }),
          );
        }
      }

      const order: BybitOrderRequest = {
        symbol,
        side: request.side === "buy" ? "Buy" : "Sell",
        orderType: request.type === "market" ? "Market" : "Limit",
        qty: qty.toString(),
        ...(request.type === "limit" ? { price: price.toString() } : {}),
        ...(request.reduceOnly === true ? { reduceOnly: true } : {}),
      };
      const ack = yield* withError(client.placeOrder(order));
      if (!ack.orderId) {
        return yield* Effect.fail(
          new ExchangeError(`futures order rejected for ${symbol}`),
        );
      }

      // The create response is an acknowledgement; query the realtime order
      // for the actual filled size/price/fee before recording the position.
      const data = yield* withError(client.getOrder({ symbol, orderId: ack.orderId }));
      const filledQty = money(data.cumExecQty);
      const filledPrice = money(data.avgPrice);
      if (filledQty.lessThanOrEqualTo(0) || filledPrice.lessThanOrEqualTo(0)) {
        // The order did not fill. A limit order may still be resting on the
        // exchange — failing without cleanup lets a retry open a duplicate.
        // Cancel it before reporting the error, and say so in the message.
        const orderId = data.orderId || ack.orderId;
        const status = data.orderStatus;
        const resting =
          status === "Created" ||
          status === "New" ||
          status === "Untriggered" ||
          status === "PartiallyFilled";
        const cleanup = yield* (resting
          ? client.cancelOrder({ symbol, orderId }).pipe(
              Effect.mapError(toExchangeError),
              Effect.match({
                onSuccess: () => `resting order ${orderId} canceled`,
                onFailure: (err) =>
                  `cancel failed: ${err.reason}; order ${orderId} may still be resting`,
              }),
            )
          : Effect.succeed(`final state ${status}`));
        return yield* Effect.fail(
          new ExchangeError(
            `futures order ${orderId} not filled (status=${status}, qty=${data.cumExecQty}, avgPrice=${data.avgPrice}); ${cleanup}`,
          ),
        );
      }

      const fill: FuturesOrderFill = {
        orderId: data.orderId || ack.orderId,
        clientOid: data.clientOrderId || ack.clientOrderId,
        symbol: request.symbol,
        side: request.side,
        productType: request.productType,
        marginMode: request.marginMode,
        filledQty,
        filledPrice,
        // Bybit signs fill fees as negative debits; store fees as non-negative
        // costs like the bitget adapter does.
        fee: money(data.cumExecFee).abs(),
        timestamp: new Date(),
      };
      return fill;
    });

  const closePosition: FuturesExchangeAdapterService["closePosition"] = (
    request,
  ) =>
    Effect.gen(function* () {
      const symbol = toBybitSymbol(request.symbol);
      const positions = yield* withError(client.getPositions(symbol));
      // Close the leg that opposes the requested side: a sell close reduces a
      // long, a buy close reduces a short.
      const neededSide = request.side === "sell" ? "Buy" : "Sell";
      const existing = positions.find((p) => p.side === neededSide);
      if (!existing) {
        return null;
      }
      const available = money(existing.size).abs();
      const closeSize = request.size.lessThan(available)
        ? request.size
        : available;
      if (closeSize.lessThanOrEqualTo(0)) {
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

  const cancelOpenOrders: FuturesExchangeAdapterService["cancelOpenOrders"] = (
    symbol,
    _productType,
  ) =>
    Effect.gen(function* () {
      const bybitSymbol = toBybitSymbol(symbol);
      const open = yield* withError(client.getOpenOrders(bybitSymbol));
      for (const order of open) {
        yield* withError(
          client.cancelOrder({ symbol: bybitSymbol, orderId: order.orderId }),
        );
      }
    });

  return {
    placeOrder,
    closePosition,
    cancelOpenOrders,

    getPosition: (symbol, productType) =>
      Effect.gen(function* () {
        const positions = yield* withError(
          client.getPositions(toBybitSymbol(symbol)),
        );
        const activePositions = positions.filter((p) =>
          money(p.size).abs().greaterThan(0),
        );
        if (activePositions.length > 1) {
          return yield* Effect.fail(
            new ExchangeError(
              `multiple active ${productType} positions returned for ${symbol}`,
            ),
          );
        }
        const p = activePositions.at(0);
        if (!p) {
          return null;
        }
        const position: FuturesPosition = {
          symbol,
          side: p.side === "Sell" ? "short" : "long",
          productType,
          marginMode: p.tradeMode === 1 ? "isolated" : "crossed",
          leverage: Number(p.leverage),
          quantity: money(p.size).abs(),
          available: money(p.size).abs(),
          entryPrice: money(p.avgPrice),
          liquidationPrice:
            p.liqPrice === "" || p.liqPrice === "0"
              ? undefined
              : money(p.liqPrice),
          unrealizedPnl: money(p.unrealisedPnl),
          marginCoin: "USDT",
        };
        return position;
      }),

    getBalance: (marginCoin) =>
      Effect.gen(function* () {
        const coins = yield* withError(client.getBalance());
        const match = coins.find(
          (c) => c.coin.toUpperCase() === marginCoin.toUpperCase(),
        );
        const available = money(
          match?.availableToWithdraw || match?.walletBalance || "0",
        );
        const balance: FuturesBalance = {
          marginCoin,
          available,
          locked: Decimal.max(
            0,
            money(match?.walletBalance ?? "0").minus(available),
          ),
          equity: money(match?.equity ?? "0"),
          usdtEquity: money(match?.usdValue ?? "0"),
        };
        return balance;
      }),

    setLeverage: (symbol, _productType, _marginMode, leverage, _holdSide) =>
      withError(
        client.setLeverage({
          symbol: toBybitSymbol(symbol),
          buyLeverage: String(leverage),
          sellLeverage: String(leverage),
        }),
      ),

    setMarginMode: (symbol, _productType, marginMode) =>
      withError(
        client.setMarginMode({ symbol: toBybitSymbol(symbol), marginMode }),
      ),

    setPositionMode: (_productType, positionMode) =>
      withError(client.setPositionMode({ mode: positionMode })),
  };
}

// ---------------------------------------------------------------------------
// Layers
// ---------------------------------------------------------------------------

export const BybitClientLiveConfig: Layer.Layer<
  BybitClient,
  never,
  BybitConfig
> = Layer.effect(
  BybitClient,
  Effect.gen(function* () {
    const config = yield* BybitConfig;
    const credentials = yield* requireBybitCredentials(config).pipe(
      Effect.orDie,
    );
    return makeBybitClientImpl(
      credentials,
      config.useTestnet ? BYBIT_TESTNET_URL : BYBIT_LIVE_URL,
    );
  }),
);

export const BybitFuturesExchangeAdapterLive = Layer.effect(
  FuturesExchangeAdapter,
  Effect.gen(function* () {
    const client = yield* BybitClient;
    const gateway = yield* MarketDataGateway;
    return makeBybitFuturesAdapter(client, gateway);
  }),
);
