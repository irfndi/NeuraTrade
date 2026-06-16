/**
 * Bitget REST client for real-money scalping.
 *
 * Implements signed-request authentication and the minimum exchange surface
 * needed for live deterministic scalping: balances, ticker, place order,
 * query order, and cancel order.
 */
import { Context, Data, Effect, Layer } from "effect";
import * as crypto from "crypto";
import { RateLimiter } from "./rate-limiter.ts";
import { BitgetConfig } from "./bitget-config.ts";

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const BITGET_BASE_URL = "https://api.bitget.com";
const BITGET_DEMO_URL = "https://api.bitget.com";
const DEFAULT_TIMEOUT_MS = 30000;

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

export class BitgetNetworkError extends Data.TaggedError("BitgetNetworkError")<{
  readonly cause: string;
  readonly endpoint: string;
}> {}

export class BitgetRateLimitError extends Data.TaggedError(
  "BitgetRateLimitError",
)<{
  readonly retryAfterMs: number;
  readonly endpoint: string;
}> {}

export class BitgetAuthError extends Data.TaggedError("BitgetAuthError")<{
  readonly cause: string;
}> {}

export class BitgetApiError extends Data.TaggedError("BitgetApiError")<{
  readonly status: number;
  readonly body: string;
  readonly endpoint: string;
}> {}

export type BitgetClientError =
  | BitgetNetworkError
  | BitgetRateLimitError
  | BitgetAuthError
  | BitgetApiError;

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface BitgetCredentials {
  readonly apiKey: string;
  readonly apiSecret: string;
  readonly passphrase: string;
}

export interface BitgetBalance {
  readonly asset: string;
  readonly available: string;
  readonly frozen: string;
}

export interface BitgetTicker {
  readonly symbol: string;
  readonly lastPrice: string;
  readonly bidPrice: string;
  readonly askPrice: string;
  readonly bidQty: string;
  readonly askQty: string;
  readonly volume24h: string;
}

export type BitgetOrderSide = "buy" | "sell";
export type BitgetOrderType = "market" | "limit";

export interface BitgetOrderRequest {
  readonly symbol: string;
  readonly side: BitgetOrderSide;
  readonly orderType: BitgetOrderType;
  readonly size: string;
  readonly price?: string;
  readonly clientOid?: string;
}

export interface BitgetOrder {
  readonly orderId: string;
  readonly clientOid: string;
  readonly symbol: string;
  readonly side: BitgetOrderSide;
  readonly orderType: BitgetOrderType;
  readonly status: string;
  readonly size: string;
  readonly price: string;
  readonly filledSize: string;
  readonly filledAmount: string;
  readonly fee: string;
}

export interface BitgetInstrument {
  readonly symbol: string;
  readonly baseCoin: string;
  readonly quoteCoin: string;
  readonly status: string;
  readonly minTradeAmount: string;
  readonly maxTradeAmount: string;
  readonly takerFeeRate: string;
  readonly makerFeeRate: string;
  readonly pricePrecision: string;
  readonly quantityPrecision: string;
  readonly quotePrecision: string;
}

// ---------------------------------------------------------------------------
// Futures types
// ---------------------------------------------------------------------------

export type BitgetProductType =
  | "USDT-FUTURES"
  | "COIN-FUTURES"
  | "USDC-FUTURES";
export type BitgetMarginMode = "isolated" | "crossed";
export type BitgetPositionMode = "one_way" | "hedge_mode";

export interface BitgetFuturesSymbol {
  readonly symbol: string;
  readonly productType: BitgetProductType;
}

export interface BitgetContract {
  readonly symbol: string;
  readonly baseCoin: string;
  readonly quoteCoin: string;
  readonly productType: BitgetProductType;
  readonly status: string;
  readonly symbolStatus: string;
  readonly pricePrecision: string;
  readonly quantityPrecision: string;
  readonly minTradeAmount: string;
  readonly minTradeNum: string;
  readonly minTradeUSDT: string;
  readonly maxLeverage: string;
  readonly minLeverage: string;
  readonly takerFeeRate: string;
  readonly makerFeeRate: string;
}

export interface BitgetFuturesTicker {
  readonly symbol: string;
  readonly lastPrice: string;
  readonly bidPrice: string;
  readonly askPrice: string;
  readonly bidQty: string;
  readonly askQty: string;
  readonly volume24h: string;
  readonly fundingRate?: string;
  readonly nextFundingTime?: string;
}

export interface BitgetFuturesBalance {
  readonly marginCoin: string;
  readonly available: string;
  readonly locked: string;
  readonly equity: string;
  readonly usdtEquity: string;
}

export interface BitgetFuturesPosition {
  readonly positionId: string;
  readonly symbol: string;
  readonly productType: BitgetProductType;
  readonly marginMode: BitgetMarginMode;
  readonly holdSide: "long" | "short";
  readonly openPrice: string;
  readonly total: string;
  readonly available: string;
  readonly leverage: string;
  readonly unrealizedPL: string;
  readonly liquidatedPrice: string;
}

export interface BitgetFuturesOrderRequest {
  readonly symbol: string;
  readonly productType: BitgetProductType;
  readonly side: BitgetOrderSide;
  readonly orderType: BitgetOrderType;
  readonly size: string;
  readonly price?: string;
  readonly marginMode?: BitgetMarginMode;
  readonly clientOid?: string;
  readonly reduceOnly?: boolean;
  readonly leverage?: number;
}

export interface BitgetFuturesOrder {
  readonly orderId: string;
  readonly clientOid: string;
  readonly symbol: string;
  readonly productType: BitgetProductType;
  readonly side: BitgetOrderSide;
  readonly orderType: BitgetOrderType;
  readonly status: string;
  readonly size: string;
  readonly price: string;
  readonly priceAvg: string;
  readonly filledSize: string;
  readonly filledAmount: string;
  readonly fee: string;
  readonly marginMode: BitgetMarginMode;
}

// ---------------------------------------------------------------------------
// Symbol normalization
// ---------------------------------------------------------------------------

export function toBitgetSymbol(symbol: string): string {
  return symbol.replace("/", "").toUpperCase();
}

export function toBitgetFuturesSymbol(
  symbol: string,
  defaultProduct: BitgetProductType = "USDT-FUTURES",
): BitgetFuturesSymbol {
  const normalized = symbol.trim().toUpperCase();
  // Accept CCXT-style "BTC/USDT:USDT" or plain "BTC/USDT".
  const [pair, quoteHint] = normalized.split(":");
  const base = pair.replace("/", "");
  let productType: BitgetProductType = defaultProduct;
  if (quoteHint === "USDC") productType = "USDC-FUTURES";
  if (quoteHint === "USD" || normalized.endsWith("_DMCBL")) {
    productType = "COIN-FUTURES";
  }
  return { symbol: base, productType };
}

export function fromBitgetSymbol(rawSymbol: string): string {
  const upper = rawSymbol.toUpperCase();
  if (upper.includes("/")) return upper;
  const quoteAssets = [
    "USDT",
    "USDC",
    "BTC",
    "ETH",
    "EUR",
    "GBP",
    "USD",
    "TRY",
  ];
  for (const quote of quoteAssets) {
    if (upper.endsWith(quote) && upper.length > quote.length) {
      return `${upper.slice(0, upper.length - quote.length)}/${quote}`;
    }
  }
  return upper;
}

function marginCoinForProductType(
  productType: BitgetProductType,
  symbol: string,
): string {
  if (productType === "USDT-FUTURES") return "USDT";
  if (productType === "USDC-FUTURES") return "USDC";
  // COIN-FUTURES: margin coin is the base asset (e.g. BTC).
  return symbol.replace(/(USDT|USDC|USD|_DMCBL)/g, "").toUpperCase() || symbol;
}

// ---------------------------------------------------------------------------
// Authentication
// ---------------------------------------------------------------------------

function sign(secret: string, payload: string): string {
  return crypto.createHmac("sha256", secret).update(payload).digest("base64");
}

function authHeaders(
  credentials: BitgetCredentials,
  method: string,
  requestPath: string,
  body: string,
  isDemo = false,
): Record<string, string> {
  const timestamp = String(Date.now());
  const payload = `${timestamp}${method.toUpperCase()}${requestPath}${body}`;
  const signature = sign(credentials.apiSecret, payload);
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    "ACCESS-KEY": credentials.apiKey,
    "ACCESS-SIGN": signature,
    "ACCESS-TIMESTAMP": timestamp,
    "ACCESS-PASSPHRASE": credentials.passphrase,
    locale: "en-US",
  };
  // Bitget demo trading uses the production host but requires the PAPTRADING
  // header to route the request to the simulated matching engine.
  if (isDemo) {
    headers["PAPTRADING"] = "1";
  }
  return headers;
}

// ---------------------------------------------------------------------------
// Service interface
// ---------------------------------------------------------------------------

export interface BitgetClientImpl {
  readonly getBalances: () => Effect.Effect<
    ReadonlyArray<BitgetBalance>,
    BitgetClientError
  >;
  readonly getInstruments: () => Effect.Effect<
    ReadonlyArray<BitgetInstrument>,
    BitgetClientError
  >;
  readonly getTicker: (
    symbol: string,
  ) => Effect.Effect<BitgetTicker, BitgetClientError>;
  readonly placeOrder: (
    order: BitgetOrderRequest,
  ) => Effect.Effect<BitgetOrder, BitgetClientError>;
  readonly getOrder: (args: {
    symbol: string;
    orderId?: string;
    clientOid?: string;
  }) => Effect.Effect<BitgetOrder, BitgetClientError>;
  readonly cancelOrder: (args: {
    symbol: string;
    orderId?: string;
    clientOid?: string;
  }) => Effect.Effect<void, BitgetClientError>;

  // Futures surface
  readonly getContracts: (
    productType?: BitgetProductType,
  ) => Effect.Effect<ReadonlyArray<BitgetContract>, BitgetClientError>;
  readonly getFuturesTicker: (
    symbol: string,
    productType?: BitgetProductType,
  ) => Effect.Effect<BitgetFuturesTicker, BitgetClientError>;
  readonly getFuturesBalances: (
    productType?: BitgetProductType,
  ) => Effect.Effect<ReadonlyArray<BitgetFuturesBalance>, BitgetClientError>;
  readonly getFuturesPositions: (
    symbol: string,
    productType?: BitgetProductType,
  ) => Effect.Effect<ReadonlyArray<BitgetFuturesPosition>, BitgetClientError>;
  readonly setLeverage: (args: {
    symbol: string;
    productType: BitgetProductType;
    marginMode: BitgetMarginMode;
    leverage: string;
    holdSide?: "long" | "short";
  }) => Effect.Effect<void, BitgetClientError>;
  readonly getLeverage: (args: {
    symbol: string;
    productType: BitgetProductType;
  }) => Effect.Effect<
    ReadonlyArray<{
      marginMode: BitgetMarginMode;
      leverage: string;
      minLeverage: string;
      maxLeverage: string;
    }>,
    BitgetClientError
  >;
  readonly setMarginMode: (args: {
    symbol: string;
    productType: BitgetProductType;
    marginMode: BitgetMarginMode;
  }) => Effect.Effect<void, BitgetClientError>;
  readonly setPositionMode: (args: {
    productType: BitgetProductType;
    positionMode: BitgetPositionMode;
  }) => Effect.Effect<void, BitgetClientError>;
  readonly placeFuturesOrder: (
    order: BitgetFuturesOrderRequest,
  ) => Effect.Effect<BitgetFuturesOrder, BitgetClientError>;
  readonly getFuturesOrder: (args: {
    symbol: string;
    productType: BitgetProductType;
    orderId?: string;
    clientOid?: string;
  }) => Effect.Effect<BitgetFuturesOrder, BitgetClientError>;
  readonly cancelFuturesOrder: (args: {
    symbol: string;
    productType: BitgetProductType;
    orderId?: string;
    clientOid?: string;
  }) => Effect.Effect<void, BitgetClientError>;
}

export class BitgetClient extends Context.Tag("BitgetClient")<
  BitgetClient,
  BitgetClientImpl
>() {}

// ---------------------------------------------------------------------------
// Internal fetch helper
// ---------------------------------------------------------------------------

interface RateLimiterLike {
  readonly acquire: (n?: number) => Effect.Effect<void, never>;
}

function fetchBitget<T>(
  baseUrl: string,
  endpoint: string,
  options: {
    readonly method?: string;
    readonly headers?: Record<string, string>;
    readonly body?: string;
  },
  rateLimiter: RateLimiterLike,
): Effect.Effect<T, BitgetClientError> {
  return Effect.gen(function* () {
    yield* rateLimiter.acquire();
    const method = options.method ?? "GET";
    const url = `${baseUrl}${endpoint}`;

    const response = yield* Effect.tryPromise({
      try: () =>
        fetch(url, {
          method,
          headers: options.headers,
          body: options.body,
          signal: AbortSignal.timeout(DEFAULT_TIMEOUT_MS),
        }),
      catch: (error): BitgetClientError => {
        if (error instanceof DOMException && error.name === "TimeoutError") {
          return new BitgetNetworkError({
            cause: `request timed out after ${DEFAULT_TIMEOUT_MS}ms`,
            endpoint,
          });
        }
        return new BitgetNetworkError({
          cause: error instanceof Error ? error.message : String(error),
          endpoint,
        });
      },
    });

    if (response.status === 429) {
      const retryAfter = Number(response.headers.get("Retry-After") || "0");
      return yield* Effect.fail(
        new BitgetRateLimitError({ retryAfterMs: retryAfter * 1000, endpoint }),
      );
    }

    const responseBody = yield* Effect.promise(() =>
      response.text().catch(() => ""),
    );

    if (!response.ok) {
      return yield* Effect.fail(
        new BitgetApiError({
          status: response.status,
          body: responseBody,
          endpoint,
        }),
      );
    }

    const parsed = yield* Effect.tryPromise({
      try: () => Promise.resolve(JSON.parse(responseBody) as unknown),
      catch: (error): BitgetClientError =>
        new BitgetApiError({
          status: response.status,
          body: error instanceof Error ? error.message : String(error),
          endpoint,
        }),
    });

    if (
      parsed &&
      typeof parsed === "object" &&
      "code" in parsed &&
      typeof parsed.code === "string" &&
      parsed.code !== "00000"
    ) {
      const msg =
        "msg" in parsed && typeof parsed.msg === "string" ? parsed.msg : "";
      return yield* Effect.fail(
        new BitgetApiError({
          status: response.status,
          body: `${parsed.code}: ${msg}`,
          endpoint,
        }),
      );
    }

    return parsed as T;
  });
}

// ---------------------------------------------------------------------------
// Response parsers
// ---------------------------------------------------------------------------

function parseBalances(data: unknown): ReadonlyArray<BitgetBalance> {
  if (!Array.isArray(data)) return [];
  return data.map((item) => ({
    asset: String(item.coin ?? item.asset ?? ""),
    available: String(item.available ?? "0"),
    frozen: String(item.frozen ?? item.locked ?? "0"),
  }));
}

function parseTicker(data: Record<string, unknown>): BitgetTicker {
  return {
    symbol: String(data.symbol ?? ""),
    lastPrice: String(data.lastPr ?? data.close ?? "0"),
    bidPrice: String(data.bidPr ?? "0"),
    askPrice: String(data.askPr ?? "0"),
    bidQty: String(data.bidSz ?? "0"),
    askQty: String(data.askSz ?? "0"),
    volume24h: String(data.baseVolume ?? data.volume24h ?? "0"),
  };
}

function parseOrder(data: Record<string, unknown>): BitgetOrder {
  return {
    orderId: String(data.orderId ?? data.id ?? ""),
    clientOid: String(data.clientOid ?? ""),
    symbol: String(data.symbol ?? ""),
    side: String(data.side ?? "buy") as BitgetOrderSide,
    orderType: String(data.orderType ?? "limit") as BitgetOrderType,
    status: String(data.status ?? ""),
    size: String(data.size ?? "0"),
    price: String(data.price ?? "0"),
    filledSize: String(data.accBaseVolume ?? data.filledSize ?? "0"),
    filledAmount: String(data.accQuoteVolume ?? data.filledAmount ?? "0"),
    fee: String(data.fee ?? "0"),
  };
}

function parseInstrument(data: Record<string, unknown>): BitgetInstrument {
  return {
    symbol: String(data.symbol ?? ""),
    baseCoin: String(data.baseCoin ?? ""),
    quoteCoin: String(data.quoteCoin ?? ""),
    status: String(data.status ?? ""),
    minTradeAmount: String(data.minTradeAmount ?? data.minTradeAmt ?? "0"),
    maxTradeAmount: String(data.maxTradeAmount ?? data.maxTradeAmt ?? "0"),
    takerFeeRate: String(data.takerFeeRate ?? "0"),
    makerFeeRate: String(data.makerFeeRate ?? "0"),
    pricePrecision: String(data.pricePrecision ?? "0"),
    quantityPrecision: String(
      data.quantityPrecision ?? data.sizePrecision ?? "0",
    ),
    quotePrecision: String(data.quotePrecision ?? "0"),
  };
}

function parseContract(data: Record<string, unknown>): BitgetContract {
  return {
    symbol: String(data.symbol ?? ""),
    baseCoin: String(data.baseCoin ?? ""),
    quoteCoin: String(data.quoteCoin ?? ""),
    productType: String(
      data.productType ?? "USDT-FUTURES",
    ) as BitgetProductType,
    status: String(data.status ?? ""),
    symbolStatus: String(data.symbolStatus ?? data.status ?? ""),
    pricePrecision: String(data.pricePrecision ?? "0"),
    quantityPrecision: String(data.quantityPrecision ?? "0"),
    minTradeAmount: String(data.minTradeAmount ?? "0"),
    minTradeNum: String(data.minTradeNum ?? data.minTradeAmount ?? "0"),
    minTradeUSDT: String(data.minTradeUSDT ?? "0"),
    maxLeverage: String(data.maxLever ?? data.maxLeverage ?? "0"),
    minLeverage: String(data.minLever ?? data.minLeverage ?? "0"),
    takerFeeRate: String(data.takerFeeRate ?? "0"),
    makerFeeRate: String(data.makerFeeRate ?? "0"),
  };
}

function parseFuturesTicker(
  data: Record<string, unknown>,
): BitgetFuturesTicker {
  return {
    symbol: String(data.symbol ?? ""),
    lastPrice: String(data.lastPr ?? data.close ?? "0"),
    bidPrice: String(data.bidPr ?? "0"),
    askPrice: String(data.askPr ?? "0"),
    bidQty: String(data.bidSz ?? "0"),
    askQty: String(data.askSz ?? "0"),
    volume24h: String(data.baseVolume ?? data.volume24h ?? "0"),
    fundingRate:
      data.fundingRate === undefined ? undefined : String(data.fundingRate),
    nextFundingTime:
      data.nextFundingTime === undefined
        ? undefined
        : String(data.nextFundingTime),
  };
}

function parseFuturesBalance(
  data: Record<string, unknown>,
): BitgetFuturesBalance {
  return {
    marginCoin: String(data.marginCoin ?? ""),
    available: String(data.available ?? "0"),
    locked: String(data.locked ?? "0"),
    equity: String(data.equity ?? "0"),
    usdtEquity: String(data.usdtEquity ?? "0"),
  };
}

function parseFuturesPosition(
  data: Record<string, unknown>,
): BitgetFuturesPosition {
  return {
    positionId: String(data.positionId ?? ""),
    symbol: String(data.symbol ?? ""),
    productType: String(
      data.productType ?? "USDT-FUTURES",
    ) as BitgetProductType,
    marginMode: String(data.marginMode ?? "crossed") as BitgetMarginMode,
    holdSide: String(data.holdSide ?? "long") as "long" | "short",
    openPrice: String(
      data.openPrice ?? data.openPriceAvg ?? data.openAvgPrice ?? "0",
    ),
    total: String(data.total ?? "0"),
    available: String(data.available ?? "0"),
    leverage: String(data.leverage ?? "0"),
    unrealizedPL: String(data.unrealizedPL ?? data.unrealizedpl ?? "0"),
    liquidatedPrice: String(data.liquidatedPrice ?? "0"),
  };
}

function parseFuturesOrder(data: Record<string, unknown>): BitgetFuturesOrder {
  return {
    orderId: String(data.orderId ?? data.id ?? ""),
    clientOid: String(data.clientOid ?? ""),
    symbol: String(data.symbol ?? ""),
    productType: String(
      data.productType ?? "USDT-FUTURES",
    ) as BitgetProductType,
    side: String(data.side ?? "buy") as BitgetOrderSide,
    orderType: String(data.orderType ?? "limit") as BitgetOrderType,
    status: String(data.state ?? data.status ?? ""),
    size: String(data.size ?? "0"),
    price: String(data.price ?? "0"),
    priceAvg: String(data.priceAvg ?? data.fillPrice ?? data.price ?? "0"),
    filledSize: String(
      data.filledQty ??
        data.accBaseVolume ??
        data.baseVolume ??
        data.filledSize ??
        "0",
    ),
    filledAmount: String(
      data.filledAmount ?? data.accQuoteVolume ?? data.quoteVolume ?? "0",
    ),
    fee: String(data.fee ?? "0"),
    marginMode: String(data.marginMode ?? "crossed") as BitgetMarginMode,
  };
}

// ---------------------------------------------------------------------------
// Live layer
// ---------------------------------------------------------------------------

export interface BitgetClientConfig {
  readonly credentials: BitgetCredentials;
  readonly baseUrl?: string;
  readonly isDemo?: boolean;
}

function makeBitgetClientImpl(
  credentials: BitgetCredentials,
  baseUrl: string,
  rateLimiter: RateLimiterLike,
  isDemo = false,
): BitgetClientImpl {
  const getBalances = (): Effect.Effect<
    ReadonlyArray<BitgetBalance>,
    BitgetClientError
  > => {
    const endpoint = "/api/v2/spot/account/assets";
    const headers = authHeaders(credentials, "GET", endpoint, "", isDemo);
    return fetchBitget<{ data: unknown }>(
      baseUrl,
      endpoint,
      { headers },
      rateLimiter,
    ).pipe(Effect.map((resp) => parseBalances(resp.data)));
  };

  const getInstruments = (): Effect.Effect<
    ReadonlyArray<BitgetInstrument>,
    BitgetClientError
  > => {
    const endpoint = "/api/v2/spot/public/symbols";
    return fetchBitget<{ data: ReadonlyArray<Record<string, unknown>> }>(
      baseUrl,
      endpoint,
      {},
      rateLimiter,
    ).pipe(Effect.map((resp) => resp.data.map(parseInstrument)));
  };

  const getTicker = (
    symbol: string,
  ): Effect.Effect<BitgetTicker, BitgetClientError> => {
    const bsymbol = toBitgetSymbol(symbol);
    const endpoint = `/api/v2/spot/market/tickers?symbol=${bsymbol}`;
    return fetchBitget<{ data: ReadonlyArray<Record<string, unknown>> }>(
      baseUrl,
      endpoint,
      {},
      rateLimiter,
    ).pipe(Effect.map((resp) => parseTicker(resp.data[0] ?? {})));
  };

  const placeOrder = (
    order: BitgetOrderRequest,
  ): Effect.Effect<BitgetOrder, BitgetClientError> => {
    const endpoint = "/api/v2/spot/trade/placeOrder";
    const bsymbol = toBitgetSymbol(order.symbol);
    const bodyObj: Record<string, unknown> = {
      symbol: bsymbol,
      side: order.side,
      orderType: order.orderType,
      force: "gtc",
      size: order.size,
    };
    if (order.price !== undefined && order.price !== "") {
      bodyObj.price = order.price;
    }
    if (order.clientOid !== undefined && order.clientOid !== "") {
      bodyObj.clientOid = order.clientOid;
    }
    const body = JSON.stringify(bodyObj);
    const headers = authHeaders(credentials, "POST", endpoint, body, isDemo);
    return fetchBitget<{ data: Record<string, unknown> }>(
      baseUrl,
      endpoint,
      { method: "POST", headers, body },
      rateLimiter,
    ).pipe(Effect.map((resp) => parseOrder(resp.data)));
  };

  const getOrder = (args: {
    symbol: string;
    orderId?: string;
    clientOid?: string;
  }): Effect.Effect<BitgetOrder, BitgetClientError> => {
    const bsymbol = toBitgetSymbol(args.symbol);
    const params = new URLSearchParams({ symbol: bsymbol });
    if (args.orderId) params.append("orderId", args.orderId);
    if (args.clientOid) params.append("clientOid", args.clientOid);
    const endpoint = `/api/v2/spot/trade/orderInfo?${params.toString()}`;
    const headers = authHeaders(credentials, "GET", endpoint, "", isDemo);
    return fetchBitget<{ data: Record<string, unknown> }>(
      baseUrl,
      endpoint,
      { headers },
      rateLimiter,
    ).pipe(Effect.map((resp) => parseOrder(resp.data)));
  };

  const cancelOrder = (args: {
    symbol: string;
    orderId?: string;
    clientOid?: string;
  }): Effect.Effect<void, BitgetClientError> => {
    const bsymbol = toBitgetSymbol(args.symbol);
    const bodyObj: Record<string, unknown> = { symbol: bsymbol };
    if (args.orderId) bodyObj.orderId = args.orderId;
    if (args.clientOid) bodyObj.clientOid = args.clientOid;
    const endpoint = "/api/v2/spot/trade/cancelOrder";
    const body = JSON.stringify(bodyObj);
    const headers = authHeaders(credentials, "POST", endpoint, body, isDemo);
    return fetchBitget<unknown>(
      baseUrl,
      endpoint,
      { method: "POST", headers, body },
      rateLimiter,
    ).pipe(Effect.map(() => undefined));
  };

  // ---------------------------------------------------------------------------
  // Futures methods
  // ---------------------------------------------------------------------------

  const getContracts = (
    productType: BitgetProductType = "USDT-FUTURES",
  ): Effect.Effect<ReadonlyArray<BitgetContract>, BitgetClientError> => {
    const endpoint = `/api/v2/mix/market/contracts?productType=${productType}`;
    return fetchBitget<{ data: ReadonlyArray<Record<string, unknown>> }>(
      baseUrl,
      endpoint,
      {},
      rateLimiter,
    ).pipe(Effect.map((resp) => resp.data.map(parseContract)));
  };

  const getFuturesTicker = (
    symbol: string,
    productType: BitgetProductType = "USDT-FUTURES",
  ): Effect.Effect<BitgetFuturesTicker, BitgetClientError> => {
    const { symbol: bsymbol } = toBitgetFuturesSymbol(symbol, productType);
    const endpoint = `/api/v2/mix/market/ticker?symbol=${bsymbol}&productType=${productType}`;
    return fetchBitget<{ data: ReadonlyArray<Record<string, unknown>> }>(
      baseUrl,
      endpoint,
      {},
      rateLimiter,
    ).pipe(Effect.map((resp) => parseFuturesTicker(resp.data[0] ?? {})));
  };

  const getFuturesBalances = (
    productType: BitgetProductType = "USDT-FUTURES",
  ): Effect.Effect<ReadonlyArray<BitgetFuturesBalance>, BitgetClientError> => {
    const endpoint = `/api/v2/mix/account/accounts?productType=${productType}`;
    const headers = authHeaders(credentials, "GET", endpoint, "", isDemo);
    return fetchBitget<{ data: ReadonlyArray<Record<string, unknown>> }>(
      baseUrl,
      endpoint,
      { headers },
      rateLimiter,
    ).pipe(Effect.map((resp) => resp.data.map(parseFuturesBalance)));
  };

  const getFuturesPositions = (
    symbol: string,
    productType: BitgetProductType = "USDT-FUTURES",
  ): Effect.Effect<ReadonlyArray<BitgetFuturesPosition>, BitgetClientError> => {
    const bsymbol =
      symbol.trim() !== ""
        ? toBitgetFuturesSymbol(symbol, productType).symbol
        : "";
    const marginCoin = marginCoinForProductType(productType, bsymbol);
    const endpoint =
      bsymbol !== ""
        ? `/api/v2/mix/position/single-position?symbol=${bsymbol}&productType=${productType}&marginCoin=${marginCoin}`
        : `/api/v2/mix/position/all-position?productType=${productType}&marginCoin=${marginCoin}`;
    const headers = authHeaders(credentials, "GET", endpoint, "", isDemo);
    return fetchBitget<{ data: ReadonlyArray<Record<string, unknown>> }>(
      baseUrl,
      endpoint,
      { headers },
      rateLimiter,
    ).pipe(Effect.map((resp) => resp.data.map(parseFuturesPosition)));
  };

  const setLeverage = (args: {
    symbol: string;
    productType: BitgetProductType;
    marginMode: BitgetMarginMode;
    leverage: string;
    holdSide?: "long" | "short";
  }): Effect.Effect<void, BitgetClientError> => {
    const { symbol: bsymbol, productType } = toBitgetFuturesSymbol(
      args.symbol,
      args.productType,
    );
    const marginCoin = marginCoinForProductType(productType, bsymbol);
    const bodyObj: Record<string, unknown> = {
      symbol: bsymbol,
      productType,
      marginCoin,
      marginMode: args.marginMode,
      leverage: args.leverage,
    };
    if (args.holdSide) bodyObj.holdSide = args.holdSide;
    const endpoint = "/api/v2/mix/account/set-leverage";
    const body = JSON.stringify(bodyObj);
    const headers = authHeaders(credentials, "POST", endpoint, body, isDemo);
    return fetchBitget<unknown>(
      baseUrl,
      endpoint,
      { method: "POST", headers, body },
      rateLimiter,
    ).pipe(Effect.map(() => undefined));
  };

  const getLeverage = (args: {
    symbol: string;
    productType: BitgetProductType;
  }): Effect.Effect<
    ReadonlyArray<{
      marginMode: BitgetMarginMode;
      leverage: string;
      minLeverage: string;
      maxLeverage: string;
    }>,
    BitgetClientError
  > => {
    const { symbol: bsymbol, productType } = toBitgetFuturesSymbol(
      args.symbol,
      args.productType,
    );
    const marginCoin = marginCoinForProductType(productType, bsymbol);
    const endpoint = `/api/v2/mix/account/account?symbol=${bsymbol}&productType=${productType}&marginCoin=${marginCoin}`;
    const headers = authHeaders(credentials, "GET", endpoint, "", isDemo);
    return fetchBitget<{
      data: Record<string, unknown>;
    }>(baseUrl, endpoint, { headers }, rateLimiter).pipe(
      Effect.map((resp) => {
        const d = resp.data;
        const items: Array<{
          marginMode: BitgetMarginMode;
          leverage: string;
          minLeverage: string;
          maxLeverage: string;
        }> = [];
        if (d.crossedMarginLeverage !== undefined) {
          items.push({
            marginMode: "crossed",
            leverage: String(d.crossedMarginLeverage),
            minLeverage: "1",
            maxLeverage: String(d.crossedMarginLeverage),
          });
        }
        if (d.isolatedLongLever !== undefined) {
          items.push({
            marginMode: "isolated",
            leverage: String(d.isolatedLongLever),
            minLeverage: "1",
            maxLeverage: String(d.isolatedLongLever),
          });
        }
        return items;
      }),
    );
  };

  const setMarginMode = (args: {
    symbol: string;
    productType: BitgetProductType;
    marginMode: BitgetMarginMode;
  }): Effect.Effect<void, BitgetClientError> => {
    const { symbol: bsymbol, productType } = toBitgetFuturesSymbol(
      args.symbol,
      args.productType,
    );
    const marginCoin = marginCoinForProductType(productType, bsymbol);
    const bodyObj = {
      symbol: bsymbol,
      productType,
      marginCoin,
      marginMode: args.marginMode,
    };
    const endpoint = "/api/v2/mix/account/set-margin-mode";
    const body = JSON.stringify(bodyObj);
    const headers = authHeaders(credentials, "POST", endpoint, body, isDemo);
    return fetchBitget<unknown>(
      baseUrl,
      endpoint,
      { method: "POST", headers, body },
      rateLimiter,
    ).pipe(Effect.map(() => undefined));
  };

  const setPositionMode = (args: {
    productType: BitgetProductType;
    positionMode: BitgetPositionMode;
  }): Effect.Effect<void, BitgetClientError> => {
    const posMode =
      args.positionMode === "one_way" ? "one_way_mode" : "hedge_mode";
    const bodyObj = {
      productType: args.productType,
      posMode,
    };
    const endpoint = "/api/v2/mix/account/set-position-mode";
    const body = JSON.stringify(bodyObj);
    const headers = authHeaders(credentials, "POST", endpoint, body, isDemo);
    return fetchBitget<unknown>(
      baseUrl,
      endpoint,
      { method: "POST", headers, body },
      rateLimiter,
    ).pipe(Effect.map(() => undefined));
  };

  const placeFuturesOrder = (
    order: BitgetFuturesOrderRequest,
  ): Effect.Effect<BitgetFuturesOrder, BitgetClientError> => {
    const { symbol: bsymbol, productType } = toBitgetFuturesSymbol(
      order.symbol,
      order.productType,
    );
    const marginCoin = marginCoinForProductType(productType, bsymbol);
    const bodyObj: Record<string, unknown> = {
      symbol: bsymbol,
      productType,
      marginCoin,
      side: order.side,
      orderType: order.orderType,
      size: order.size,
      timeInForceValue: "GTC",
    };
    if (order.price !== undefined && order.price !== "") {
      bodyObj.price = order.price;
    }
    if (order.marginMode !== undefined) {
      bodyObj.marginMode = order.marginMode;
    }
    if (order.clientOid !== undefined && order.clientOid !== "") {
      bodyObj.clientOid = order.clientOid;
    }
    if (order.reduceOnly === true) {
      bodyObj.reduceOnly = "yes";
    }
    const endpoint = "/api/v2/mix/order/place-order";
    const body = JSON.stringify(bodyObj);
    const headers = authHeaders(credentials, "POST", endpoint, body, isDemo);
    return fetchBitget<{ data: Record<string, unknown> }>(
      baseUrl,
      endpoint,
      { method: "POST", headers, body },
      rateLimiter,
    ).pipe(Effect.map((resp) => parseFuturesOrder(resp.data)));
  };

  const getFuturesOrder = (args: {
    symbol: string;
    productType: BitgetProductType;
    orderId?: string;
    clientOid?: string;
  }): Effect.Effect<BitgetFuturesOrder, BitgetClientError> => {
    const { symbol: bsymbol, productType } = toBitgetFuturesSymbol(
      args.symbol,
      args.productType,
    );
    const params = new URLSearchParams({
      symbol: bsymbol,
      productType,
    });
    if (args.orderId) params.append("orderId", args.orderId);
    if (args.clientOid) params.append("clientOid", args.clientOid);
    const endpoint = `/api/v2/mix/order/detail?${params.toString()}`;
    const headers = authHeaders(credentials, "GET", endpoint, "", isDemo);
    return fetchBitget<{ data: Record<string, unknown> }>(
      baseUrl,
      endpoint,
      { headers },
      rateLimiter,
    ).pipe(Effect.map((resp) => parseFuturesOrder(resp.data)));
  };

  const cancelFuturesOrder = (args: {
    symbol: string;
    productType: BitgetProductType;
    orderId?: string;
    clientOid?: string;
  }): Effect.Effect<void, BitgetClientError> => {
    const { symbol: bsymbol, productType } = toBitgetFuturesSymbol(
      args.symbol,
      args.productType,
    );
    const bodyObj: Record<string, unknown> = {
      symbol: bsymbol,
      productType,
    };
    if (args.orderId) bodyObj.orderId = args.orderId;
    if (args.clientOid) bodyObj.clientOid = args.clientOid;
    const endpoint = "/api/v2/mix/order/cancel-order";
    const body = JSON.stringify(bodyObj);
    const headers = authHeaders(credentials, "POST", endpoint, body, isDemo);
    return fetchBitget<unknown>(
      baseUrl,
      endpoint,
      { method: "POST", headers, body },
      rateLimiter,
    ).pipe(Effect.map(() => undefined));
  };

  return {
    getBalances,
    getInstruments,
    getTicker,
    placeOrder,
    getOrder,
    cancelOrder,
    getContracts,
    getFuturesTicker,
    getFuturesBalances,
    getFuturesPositions,
    setLeverage,
    getLeverage,
    setMarginMode,
    setPositionMode,
    placeFuturesOrder,
    getFuturesOrder,
    cancelFuturesOrder,
  };
}

export const BitgetClientLive = (
  config: BitgetClientConfig,
): Layer.Layer<BitgetClient, never, RateLimiter> =>
  Layer.effect(
    BitgetClient,
    Effect.gen(function* () {
      const rateLimiter = yield* RateLimiter;
      const baseUrl = config.baseUrl ?? BITGET_BASE_URL;
      return makeBitgetClientImpl(
        config.credentials,
        baseUrl,
        rateLimiter,
        config.isDemo,
      );
    }),
  );

// ---------------------------------------------------------------------------
// Credential validation
// ---------------------------------------------------------------------------

export function validateCredentials(
  credentials: Partial<BitgetCredentials>,
): Effect.Effect<BitgetCredentials, BitgetAuthError> {
  return Effect.gen(function* () {
    const apiKey = credentials.apiKey?.trim() ?? "";
    const apiSecret = credentials.apiSecret?.trim() ?? "";
    const passphrase = credentials.passphrase?.trim() ?? "";
    if (!apiKey || !apiSecret || !passphrase) {
      return yield* Effect.fail(
        new BitgetAuthError({
          cause:
            "Bitget credentials incomplete: BITGET_API_KEY, BITGET_API_SECRET and BITGET_PASSPHRASE are required",
        }),
      );
    }
    return { apiKey, apiSecret, passphrase };
  });
}

// ---------------------------------------------------------------------------
// Config-driven layer
// ---------------------------------------------------------------------------

/**
 * Layer that builds a BitgetClient from BitgetConfig.
 *
 * This is the layer wired into the CLI root so credentials are loaded from
 * environment variables and the client is only built when requested.
 */
export const BitgetClientLiveConfig: Layer.Layer<
  BitgetClient,
  never,
  BitgetConfig | RateLimiter
> = Layer.effect(
  BitgetClient,
  Effect.gen(function* () {
    const config = yield* BitgetConfig;
    const rateLimiter = yield* RateLimiter;
    const baseUrl = config.useSandbox ? BITGET_DEMO_URL : BITGET_BASE_URL;
    return makeBitgetClientImpl(
      config.credentials,
      baseUrl,
      rateLimiter,
      config.useSandbox,
    );
  }),
);
