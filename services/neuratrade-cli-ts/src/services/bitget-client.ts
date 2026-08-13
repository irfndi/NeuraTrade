/**
 * Bitget REST client for real-money scalping.
 *
 * Implements signed-request authentication and the minimum exchange surface
 * needed for live deterministic scalping: balances, ticker, place order,
 * query order, and cancel order.
 */
import { Context, Data, Effect, Layer } from "effect";
import * as S from "effect/Schema";
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
  /** Parsed Bitget business code (e.g. "40034"), when the body carries one. */
  readonly code?: string;
}> {}

export type BitgetClientError =
  | BitgetNetworkError
  | BitgetRateLimitError
  | BitgetAuthError
  | BitgetApiError;

/** Raw key/value record from a Bitget API response element (scalar fields). */
type BitgetApiRecord = Record<
  string,
  string | number | boolean | null | undefined
>;

/** Signed request headers (all header values are strings). */
type AuthHeaders = Record<string, string>;

/** Request body for a signed Bitget mutation (all values are strings). */
type BitgetRequestBody = Record<string, string>;

/** Bitget error envelope: `{"code":"40034","msg":...}`. */
const BitgetEnvelopeSchema = S.Struct({
  code: S.optional(S.String),
  msg: S.optional(S.String),
});

/** Bitget error body carrying only an optional business `code`. */
const ErrorBodySchema = S.Struct({ code: S.optional(S.String) });

/**
 * Extract the Bitget business code from an error body. Bitget returns JSON
 * bodies (`{"code":"40034","msg":...}`), but some proxies/edges return plain
 * text prefixed with the code (`"40034: Parameter does not exist"`). We return
 * the code only when it is unambiguous so callers can compare it exactly
 * instead of substring-matching the raw body.
 */
function parseBitgetErrorCode(body: string): string | undefined {
  try {
    const decoded = S.decodeUnknownOption(ErrorBodySchema)(JSON.parse(body));
    if (decoded._tag === "Some" && decoded.value.code !== undefined) {
      return decoded.value.code;
    }
  } catch {
    // Not JSON — fall through to the text-prefix heuristic below.
  }
  const match = /^(\d{5})(?:\s*:|\s)/.exec(body);
  return match?.[1];
}

const SYMBOL_OR_CONTRACT_PARAM_RE =
  /\b(symbols?|contracts?|instruments?|instid|inst_id|tradecoin|trade_coin)\b/i;

/**
 * True when a `BitgetApiError` proves the instrument/contract itself is
 * unsupported. Bitget returns code 40034 ("parameter does not exist") for a
 * variety of parameter defects, so the code alone is not proof. To fail closed
 * we only treat a 40034 as an unsupported instrument when the message
 * *positively* names a symbol/contract/instrument parameter (e.g. "Parameter
 * symbol does not exist") or is the demo proxy's bare generic message with no
 * parameter named at all ("Parameter does not exist"). Any named parameter that
 * is not obviously a symbol — marginCoin, clientType, leverage, ... — is a
 * configuration defect and must propagate so callers never mistake it for an
 * absent position or an untradeable instrument.
 */
export function isBitgetUnsupportedInstrumentError(
  error: BitgetApiError,
): boolean {
  if (error.code !== "40034") return false;
  if (!/does not exist|not exist|no such parameter/i.test(error.body)) {
    return false;
  }
  // Bare "Parameter does not exist" (no parameter named) is the demo proxy's
  // generic contract-missing message -> unsupported instrument. Check it first
  // so the named forms below cannot capture "does" as a parameter name.
  if (/\bparameter\s+does not exist\b/i.test(error.body)) return true;
  const namedParameter =
    /(?:parameter\s+(\S+)\s+(?:does not exist|not exist)|no such parameter\s+(\S+))/i.exec(
      error.body,
    );
  const parameterName = namedParameter?.[1] ?? namedParameter?.[2];
  // Fail closed: an unrecognized named parameter is a config defect.
  if (parameterName === undefined) return false;
  // A named parameter only counts when it positively identifies a
  // symbol/contract/instrument. Anything else is a config defect.
  return SYMBOL_OR_CONTRACT_PARAM_RE.test(parameterName);
}

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
  readonly minTradeUSDT: string;
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

/**
 * Build Bitget request auth headers.
 *
 * `isDemo` is REQUIRED (no default): Bitget demo trading uses the production
 * host and differs only by the PAPTRADING header, so omitting the flag would
 * silently route a "demo" request to the live matching engine. The demo
 * decision must be made exactly once by the layer that owns the config flag
 * and threaded through explicitly.
 */
export function authHeaders(
  credentials: BitgetCredentials,
  method: string,
  requestPath: string,
  body: string,
  isDemo: boolean,
  timestamp = String(Date.now()),
): AuthHeaders {
  const payload = `${timestamp}${method.toUpperCase()}${requestPath}${body}`;
  const signature = sign(credentials.apiSecret, payload);
  const headers: AuthHeaders = {
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
  readonly setTradingStop: (args: {
    symbol: string;
    productType: BitgetProductType;
    holdSide: "long" | "short";
    takeProfit?: string;
    stopLoss?: string;
  }) => Effect.Effect<void, BitgetClientError>;
}

export class BitgetClient extends Context.Service<
  BitgetClient,
  BitgetClientImpl
>()("BitgetClient") {}

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
      // Retry-After is normally delta-seconds, but may be an HTTP-date on
      // some proxies. Number() on an HTTP-date yields NaN; falling back to 0
      // would busy-loop into another 429 immediately, so default to a sane
      // constant backoff instead.
      const rawRetryAfter = response.headers.get("Retry-After");
      const retryAfter = Number(rawRetryAfter || "0");
      const retryAfterMs =
        Number.isFinite(retryAfter) && retryAfter > 0
          ? retryAfter * 1000
          : 5000;
      if (rawRetryAfter && !Number.isFinite(retryAfter)) {
        console.warn(
          `[bitget-client] unparseable Retry-After header ${JSON.stringify(rawRetryAfter)} on ${endpoint}; falling back to ${retryAfterMs}ms`,
        );
      }
      return yield* Effect.fail(
        new BitgetRateLimitError({ retryAfterMs, endpoint }),
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
          code: parseBitgetErrorCode(responseBody),
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

    const envelope = S.decodeUnknownOption(BitgetEnvelopeSchema)(parsed);
    if (envelope._tag === "Some" && envelope.value.code !== "00000") {
      const code = envelope.value.code;
      if (code !== undefined) {
        return yield* Effect.fail(
          new BitgetApiError({
            status: response.status,
            body: `${code}: ${envelope.value.msg ?? ""}`,
            endpoint,
            code,
          }),
        );
      }
    }

    return parsed as T;
  });
}

// ---------------------------------------------------------------------------
// Response parsers
// ---------------------------------------------------------------------------

/**
 * Run a response parser as a typed failure. Parsers below fail (throw) when a
 * money-critical field is missing or mistyped instead of fabricating a default
 * (side='buy', price='0'): an API shape drift must surface as a `BitgetApiError`
 * so callers never trade on invented values. Cosmetic fields keep tolerant
 * defaults.
 */
function strictParse<T>(
  endpoint: string,
  parse: () => T,
): Effect.Effect<T, BitgetApiError> {
  return Effect.suspend(() => {
    try {
      return Effect.succeed(parse());
    } catch (error) {
      return Effect.fail(
        new BitgetApiError({
          status: 0,
          body: error instanceof Error ? error.message : String(error),
          endpoint,
          code: "PARSE_CONTRACT",
        }),
      );
    }
  });
}

/** Present-and-non-empty string field; throws on absent/empty/mistyped. */
function requiredString(
  data: BitgetApiRecord,
  key: string,
  endpoint: string,
  aliases: readonly string[] = [],
): string {
  const raw =
    data[key] ?? data[aliases.find((a) => data[a] !== undefined) ?? ""];
  if (raw === undefined || raw === null) {
    throw new Error(
      `response missing required field "${key}" (endpoint ${endpoint})`,
    );
  }
  const value = String(raw);
  if (value === "") {
    throw new Error(`response field "${key}" is empty (endpoint ${endpoint})`);
  }
  return value;
}

/** Enum-valued field; throws when absent or outside the allowed set. */
function requiredEnum<K extends string>(
  data: BitgetApiRecord,
  key: string,
  endpoint: string,
  allowed: readonly K[],
  aliases: readonly string[] = [],
): K {
  const raw =
    data[key] ?? data[aliases.find((a) => data[a] !== undefined) ?? ""];
  if (raw === undefined || raw === null) {
    throw new Error(
      `response missing required field "${key}" (endpoint ${endpoint})`,
    );
  }
  const value = String(raw);
  if (!(allowed as readonly string[]).includes(value)) {
    throw new Error(
      `response field "${key}" has unexpected value ${JSON.stringify(value)} (endpoint ${endpoint})`,
    );
  }
  return value as K;
}

function parseBalances(
  data: ReadonlyArray<BitgetApiRecord>,
): ReadonlyArray<BitgetBalance> {
  if (!Array.isArray(data)) return [];
  return data.map((item) => {
    const record = item ?? {};
    const asset = requiredString(
      record,
      "asset",
      "/api/v2/spot/account/assets",
      ["coin"],
    );
    const available = requiredString(
      record,
      "available",
      "/api/v2/spot/account/assets",
    );
    // Spot balances report frozen or locked depending on the endpoint version;
    // at least one must be present, but either spelling is tolerated.
    const frozenRaw = record.frozen ?? record.locked;
    if (frozenRaw === undefined || frozenRaw === null) {
      throw new Error(
        'response missing required field "frozen" (endpoint /api/v2/spot/account/assets)',
      );
    }
    return { asset, available, frozen: String(frozenRaw) };
  });
}

function parseTicker(data: BitgetApiRecord): BitgetTicker {
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

function parseOrder(data: BitgetApiRecord, endpoint: string): BitgetOrder {
  return {
    orderId: String(data.orderId ?? data.id ?? ""),
    clientOid: String(data.clientOid ?? ""),
    symbol: String(data.symbol ?? ""),
    side: requiredEnum(data, "side", endpoint, ["buy", "sell"] as const),
    orderType: requiredEnum(data, "orderType", endpoint, [
      "market",
      "limit",
    ] as const),
    status: requiredString(data, "status", endpoint),
    size: requiredString(data, "size", endpoint),
    price: requiredString(data, "price", endpoint),
    filledSize: String(data.accBaseVolume ?? data.filledSize ?? "0"),
    filledAmount: String(data.accQuoteVolume ?? data.filledAmount ?? "0"),
    fee: String(data.fee ?? "0"),
  };
}

/**
 * Parse a place-order acknowledgement. Bitget's place-order response is an
 * acknowledgement: it carries only orderId/clientOid (orderId may even be
 * null for auto-replaced reduce-only orders) and never the full order state.
 * Failing unless at least one identifier is present catches a missing
 * envelope. The remaining BitgetOrder fields keep the documented ack defaults
 * — the API never returns them here, so they must not be treated as real
 * order state; callers that need side/status/size/price must query getOrder.
 */
function parsePlacedOrder(
  data: BitgetApiRecord,
  endpoint: string,
): BitgetOrder {
  const orderIdRaw = data.orderId ?? data.id;
  const clientOidRaw = data.clientOid;
  const hasOrderId = orderIdRaw !== undefined && orderIdRaw !== null;
  const hasClientOid = clientOidRaw !== undefined && clientOidRaw !== null;
  if (!hasOrderId && !hasClientOid) {
    throw new Error(
      `place-order response carries neither orderId nor clientOid (endpoint ${endpoint})`,
    );
  }
  return {
    orderId: hasOrderId ? String(orderIdRaw) : "",
    clientOid: hasClientOid ? String(clientOidRaw) : "",
    symbol: String(data.symbol ?? ""),
    // ponytail: the ack never carries these; filled with documented ack
    // defaults for interface parity (the CLI prints them for display only).
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

function parsePlacedFuturesOrder(
  data: BitgetApiRecord,
  endpoint: string,
): BitgetFuturesOrder {
  const base = parsePlacedOrder(data, endpoint);
  return {
    ...base,
    productType: String(
      data.productType ?? "USDT-FUTURES",
    ) as BitgetProductType,
    priceAvg: String(data.priceAvg ?? data.fillPrice ?? data.price ?? "0"),
    marginMode: String(data.marginMode ?? "crossed") as BitgetMarginMode,
  };
}

function parseInstrument(data: BitgetApiRecord): BitgetInstrument {
  return {
    symbol: String(data.symbol ?? ""),
    baseCoin: String(data.baseCoin ?? ""),
    quoteCoin: String(data.quoteCoin ?? ""),
    status: String(data.status ?? ""),
    minTradeAmount: String(data.minTradeAmount ?? data.minTradeAmt ?? "0"),
    maxTradeAmount: String(data.maxTradeAmount ?? data.maxTradeAmt ?? "0"),
    minTradeUSDT: String(data.minTradeUSDT ?? "0"),
    takerFeeRate: String(data.takerFeeRate ?? "0"),
    makerFeeRate: String(data.makerFeeRate ?? "0"),
    pricePrecision: String(data.pricePrecision ?? "0"),
    quantityPrecision: String(
      data.quantityPrecision ?? data.sizePrecision ?? "0",
    ),
    quotePrecision: String(data.quotePrecision ?? "0"),
  };
}

function parseContract(data: BitgetApiRecord): BitgetContract {
  return {
    symbol: String(data.symbol ?? ""),
    baseCoin: String(data.baseCoin ?? ""),
    quoteCoin: String(data.quoteCoin ?? ""),
    productType: String(
      data.productType ?? "USDT-FUTURES",
    ) as BitgetProductType,
    status: String(data.status ?? ""),
    symbolStatus: String(data.symbolStatus ?? data.status ?? ""),
    // The futures contracts endpoint reports decimal places as pricePlace
    // (e.g. ADA "4" => tick 0.0001); pricePrecision is absent there.
    pricePrecision: String(data.pricePlace ?? data.pricePrecision ?? "0"),
    quantityPrecision: String(
      data.volumePlace ?? data.quantityPrecision ?? "0",
    ),
    minTradeAmount: String(data.minTradeAmount ?? "0"),
    minTradeNum: String(data.minTradeNum ?? data.minTradeAmount ?? "0"),
    minTradeUSDT: String(data.minTradeUSDT ?? "0"),
    maxLeverage: String(data.maxLever ?? data.maxLeverage ?? "0"),
    minLeverage: String(data.minLever ?? data.minLeverage ?? "0"),
    takerFeeRate: String(data.takerFeeRate ?? "0"),
    makerFeeRate: String(data.makerFeeRate ?? "0"),
  };
}

function parseFuturesTicker(data: BitgetApiRecord): BitgetFuturesTicker {
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

function parseFuturesBalance(data: BitgetApiRecord): BitgetFuturesBalance {
  return {
    marginCoin: String(data.marginCoin ?? ""),
    available: String(data.available ?? "0"),
    locked: String(data.locked ?? "0"),
    equity: String(data.equity ?? "0"),
    usdtEquity: String(data.usdtEquity ?? "0"),
  };
}

function parseFuturesPosition(
  data: BitgetApiRecord,
  endpoint: string,
): BitgetFuturesPosition {
  return {
    positionId: String(data.positionId ?? ""),
    symbol: String(data.symbol ?? ""),
    productType: String(
      data.productType ?? "USDT-FUTURES",
    ) as BitgetProductType,
    marginMode: String(data.marginMode ?? "crossed") as BitgetMarginMode,
    holdSide: requiredEnum(data, "holdSide", endpoint, [
      "long",
      "short",
    ] as const),
    openPrice: requiredString(data, "openPrice", endpoint, [
      "openPriceAvg",
      "openAvgPrice",
    ]),
    total: requiredString(data, "total", endpoint),
    available: requiredString(data, "available", endpoint),
    leverage: requiredString(data, "leverage", endpoint),
    unrealizedPL: String(data.unrealizedPL ?? data.unrealizedpl ?? "0"),
    liquidatedPrice: String(data.liquidatedPrice ?? "0"),
  };
}

function parseFuturesOrder(
  data: BitgetApiRecord,
  endpoint: string,
): BitgetFuturesOrder {
  return {
    orderId: String(data.orderId ?? data.id ?? ""),
    clientOid: String(data.clientOid ?? ""),
    symbol: String(data.symbol ?? ""),
    productType: String(
      data.productType ?? "USDT-FUTURES",
    ) as BitgetProductType,
    side: requiredEnum(data, "side", endpoint, ["buy", "sell"] as const),
    orderType: requiredEnum(data, "orderType", endpoint, [
      "market",
      "limit",
    ] as const),
    status: requiredString(data, "status", endpoint, ["state"]),
    size: requiredString(data, "size", endpoint),
    price: requiredString(data, "price", endpoint),
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
  /**
   * Demo mode. REQUIRED to be `true` for sandbox: Bitget demo trading shares
   * the production host and is routed only by the PAPTRADING header, so
   * omitting this flag (or passing false) sends requests to the LIVE matching
   * engine. Set it explicitly whenever the client talks to demo credentials.
   */
  readonly isDemo?: boolean;
}

function makeBitgetClientImpl(
  credentials: BitgetCredentials,
  baseUrl: string,
  rateLimiter: RateLimiterLike,
  isDemo: boolean,
): BitgetClientImpl {
  const getBalances = (): Effect.Effect<
    ReadonlyArray<BitgetBalance>,
    BitgetClientError
  > => {
    const endpoint = "/api/v2/spot/account/assets";
    const headers = authHeaders(credentials, "GET", endpoint, "", isDemo);
    return fetchBitget<{ data: ReadonlyArray<BitgetApiRecord> }>(
      baseUrl,
      endpoint,
      { headers },
      rateLimiter,
    ).pipe(
      Effect.flatMap((resp) =>
        strictParse(endpoint, () => parseBalances(resp.data)),
      ),
    );
  };

  const getInstruments = (): Effect.Effect<
    ReadonlyArray<BitgetInstrument>,
    BitgetClientError
  > => {
    const endpoint = "/api/v2/spot/public/symbols";
    return fetchBitget<{ data: ReadonlyArray<BitgetApiRecord> }>(
      baseUrl,
      endpoint,
      {},
      rateLimiter,
    ).pipe(
      Effect.flatMap((resp) =>
        strictParse(endpoint, () => resp.data.map(parseInstrument)),
      ),
    );
  };

  const getTicker = (
    symbol: string,
  ): Effect.Effect<BitgetTicker, BitgetClientError> => {
    const bsymbol = toBitgetSymbol(symbol);
    const endpoint = `/api/v2/spot/market/tickers?symbol=${bsymbol}`;
    return fetchBitget<{ data: ReadonlyArray<BitgetApiRecord> }>(
      baseUrl,
      endpoint,
      {},
      rateLimiter,
    ).pipe(
      Effect.flatMap((resp) =>
        strictParse(endpoint, () => parseTicker(resp.data[0] ?? {})),
      ),
    );
  };

  const placeOrder = (
    order: BitgetOrderRequest,
  ): Effect.Effect<BitgetOrder, BitgetClientError> => {
    const endpoint = "/api/v2/spot/trade/place-order";
    const bsymbol = toBitgetSymbol(order.symbol);
    const bodyObj: BitgetRequestBody = {
      symbol: bsymbol,
      side: order.side,
      orderType: order.orderType,
      size: order.size,
    };
    if (order.orderType === "limit") {
      bodyObj.force = "gtc";
    }
    if (order.price !== undefined && order.price !== "") {
      bodyObj.price = order.price;
    }
    if (order.clientOid !== undefined && order.clientOid !== "") {
      bodyObj.clientOid = order.clientOid;
    }
    const body = JSON.stringify(bodyObj);
    const headers = authHeaders(credentials, "POST", endpoint, body, isDemo);
    return fetchBitget<{ data: BitgetApiRecord }>(
      baseUrl,
      endpoint,
      { method: "POST", headers, body },
      rateLimiter,
    ).pipe(
      Effect.flatMap((resp) =>
        strictParse(endpoint, () => parsePlacedOrder(resp.data, endpoint)),
      ),
    );
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
    return fetchBitget<{ data: BitgetApiRecord }>(
      baseUrl,
      endpoint,
      { headers },
      rateLimiter,
    ).pipe(
      Effect.flatMap((resp) =>
        strictParse(endpoint, () => parseOrder(resp.data, endpoint)),
      ),
    );
  };

  const cancelOrder = (args: {
    symbol: string;
    orderId?: string;
    clientOid?: string;
  }): Effect.Effect<void, BitgetClientError> => {
    const bsymbol = toBitgetSymbol(args.symbol);
    const bodyObj: BitgetRequestBody = { symbol: bsymbol };
    if (args.orderId) bodyObj.orderId = args.orderId;
    if (args.clientOid) bodyObj.clientOid = args.clientOid;
    const endpoint = "/api/v2/spot/trade/cancel-order";
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
    return fetchBitget<{ data: ReadonlyArray<BitgetApiRecord> }>(
      baseUrl,
      endpoint,
      {},
      rateLimiter,
    ).pipe(
      Effect.flatMap((resp) =>
        strictParse(endpoint, () => resp.data.map(parseContract)),
      ),
    );
  };

  const getFuturesTicker = (
    symbol: string,
    productType: BitgetProductType = "USDT-FUTURES",
  ): Effect.Effect<BitgetFuturesTicker, BitgetClientError> => {
    const { symbol: bsymbol } = toBitgetFuturesSymbol(symbol, productType);
    const endpoint = `/api/v2/mix/market/ticker?symbol=${bsymbol}&productType=${productType}`;
    return fetchBitget<{ data: ReadonlyArray<BitgetApiRecord> }>(
      baseUrl,
      endpoint,
      {},
      rateLimiter,
    ).pipe(
      Effect.flatMap((resp) =>
        strictParse(endpoint, () => parseFuturesTicker(resp.data[0] ?? {})),
      ),
    );
  };

  const getFuturesBalances = (
    productType: BitgetProductType = "USDT-FUTURES",
  ): Effect.Effect<ReadonlyArray<BitgetFuturesBalance>, BitgetClientError> => {
    const endpoint = `/api/v2/mix/account/accounts?productType=${productType}`;
    const headers = authHeaders(credentials, "GET", endpoint, "", isDemo);
    return fetchBitget<{ data: ReadonlyArray<BitgetApiRecord> }>(
      baseUrl,
      endpoint,
      { headers },
      rateLimiter,
    ).pipe(
      Effect.flatMap((resp) =>
        strictParse(endpoint, () => resp.data.map(parseFuturesBalance)),
      ),
    );
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
    return fetchBitget<{ data: ReadonlyArray<BitgetApiRecord> }>(
      baseUrl,
      endpoint,
      { headers },
      rateLimiter,
    ).pipe(
      Effect.catch((error) => {
        // Some listed contracts (e.g. demo proxies for delisted or exotic
        // pairs) return 40034 "Parameter ... does not exist" on the
        // single-position query even though no position is open. That is
        // not a fault of the caller — it means the position is absent.
        if (
          symbol.trim() !== "" &&
          error instanceof BitgetApiError &&
          isBitgetUnsupportedInstrumentError(error) &&
          error.endpoint.includes("/single-position")
        ) {
          return Effect.succeed({ data: [] });
        }
        return Effect.fail(error);
      }),
      Effect.flatMap((resp) =>
        strictParse(endpoint, () =>
          resp.data.map((item) => parseFuturesPosition(item, endpoint)),
        ),
      ),
    );
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
    const bodyObj: BitgetRequestBody = {
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
      data: BitgetApiRecord;
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

  // Attach / update exchange-side take-profit and stop-loss orders on an
  // already-open position. The Bitget v2 endpoint is
  // "POST /api/v2/mix/order/place-pos-tpsl" (Place Position TPSL); the trigger
  // prices are named `stopSurplusTriggerPrice` (take-profit) and
  // `stopLossTriggerPrice` (stop-loss), NOT `presetTakeProfitPrice` /
  // `presetStopLossPrice` (those are the place-order preset fields). Prices
  // are absolute trigger prices. Only the provided leg is sent so the other
  // remains unchanged.
  const setTradingStop = (args: {
    symbol: string;
    productType: BitgetProductType;
    holdSide: "long" | "short";
    takeProfit?: string;
    stopLoss?: string;
  }): Effect.Effect<void, BitgetClientError> => {
    const { symbol: bsymbol, productType } = toBitgetFuturesSymbol(
      args.symbol,
      args.productType,
    );
    const marginCoin = marginCoinForProductType(productType, bsymbol);
    const bodyObj: BitgetRequestBody = {
      symbol: bsymbol,
      productType,
      marginCoin,
      holdSide: args.holdSide,
    };
    if (args.takeProfit !== undefined) {
      bodyObj.stopSurplusTriggerPrice = args.takeProfit;
    }
    if (args.stopLoss !== undefined) {
      bodyObj.stopLossTriggerPrice = args.stopLoss;
    }
    const endpoint = "/api/v2/mix/order/place-pos-tpsl";
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
    const bodyObj: BitgetRequestBody = {
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
    return fetchBitget<{ data: BitgetApiRecord }>(
      baseUrl,
      endpoint,
      { method: "POST", headers, body },
      rateLimiter,
    ).pipe(
      Effect.flatMap((resp) =>
        strictParse(endpoint, () =>
          parsePlacedFuturesOrder(resp.data, endpoint),
        ),
      ),
    );
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
    return fetchBitget<{ data: BitgetApiRecord }>(
      baseUrl,
      endpoint,
      { headers },
      rateLimiter,
    ).pipe(
      Effect.flatMap((resp) =>
        strictParse(endpoint, () => parseFuturesOrder(resp.data, endpoint)),
      ),
    );
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
    const bodyObj: BitgetRequestBody = {
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
    setTradingStop,
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
      // This is the single demo-routing decision point. `isDemo` is threaded
      // explicitly (authHeaders and makeBitgetClientImpl take no default) so a
      // caller cannot silently drop the PAPTRADING flag mid-path. Bitget demo
      // trading shares the production host, so the PAPTRADING header IS the
      // routing: a demo request without it would hit the live matching engine.
      const isDemo = config.isDemo === true;
      return makeBitgetClientImpl(
        config.credentials,
        baseUrl,
        rateLimiter,
        isDemo,
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
    // BITGET_DEMO_URL equals BITGET_BASE_URL by design (Bitget demo trading
    // shares the production host); demo routing is the PAPTRADING header,
    // derived here from the single config flag and threaded explicitly into
    // every signed request. useSandbox=false means live orders.
    const baseUrl = config.useSandbox ? BITGET_DEMO_URL : BITGET_BASE_URL;
    return makeBitgetClientImpl(
      config.credentials,
      baseUrl,
      rateLimiter,
      config.useSandbox,
    );
  }),
);
