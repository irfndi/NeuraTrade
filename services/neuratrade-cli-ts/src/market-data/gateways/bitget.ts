import { Effect } from "effect";
import type { Candle, OrderBook, Tick } from "../types.js";
import { MarketDataError } from "../gateway.js";
import {
  toBitgetSymbol,
  toBitgetFuturesSymbol,
  type BitgetProductType,
} from "../../services/bitget-client.js";

const BASE_URL = "https://api.bitget.com";
const DEFAULT_TIMEOUT_MS = 30_000;

type MarketType = "spot" | "futures";

interface NormalizedBitgetSymbol {
  readonly symbol: string;
  readonly productType: BitgetProductType | undefined;
}

function bitgetSymbol(
  symbol: string,
  marketType: MarketType,
): NormalizedBitgetSymbol {
  if (marketType === "futures") {
    const { symbol: bSymbol, productType } = toBitgetFuturesSymbol(symbol);
    return { symbol: bSymbol, productType };
  }
  return { symbol: toBitgetSymbol(symbol), productType: undefined };
}

function getJSON<T>(
  path: string,
  timeoutMs = DEFAULT_TIMEOUT_MS,
): Effect.Effect<T, MarketDataError, never> {
  return Effect.gen(function* () {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), timeoutMs);

    const response = yield* Effect.tryPromise({
      try: () =>
        fetch(`${BASE_URL}${path}`, {
          method: "GET",
          signal: controller.signal,
          headers: { Accept: "application/json" },
        }),
      catch: (err) =>
        new MarketDataError(
          `Bitget network error for ${path}: ${err instanceof Error ? err.message : String(err)}`,
          err,
        ),
    }).pipe(Effect.tap(() => Effect.sync(() => clearTimeout(timer))));

    if (!response.ok) {
      return yield* Effect.fail(
        new MarketDataError(`Bitget HTTP ${response.status} for ${path}`),
      );
    }

    const envelope = yield* Effect.tryPromise({
      try: () => response.json() as Promise<BitgetEnvelope<T>>,
      catch: (err) =>
        new MarketDataError(
          `Bitget JSON parse error for ${path}: ${err instanceof Error ? err.message : String(err)}`,
          err,
        ),
    });

    if (envelope.code && envelope.code !== "00000") {
      return yield* Effect.fail(
        new MarketDataError(
          `Bitget API error ${envelope.code} for ${path}: ${envelope.msg ?? "unknown"}`,
        ),
      );
    }

    return envelope.data;
  });
}

interface BitgetEnvelope<T> {
  readonly code?: string;
  readonly msg?: string;
  readonly data: T;
}

function bitgetExchange(marketType: MarketType): string {
  return marketType === "futures" ? "bitget-futures" : "bitget";
}

function asNumber(value: string | number | undefined): number {
  if (value === undefined || value === null) return 0;
  return Number(value);
}

interface BitgetTicker {
  readonly symbol: string;
  readonly lastPr?: string;
  readonly last?: string;
  readonly bidPr?: string;
  readonly askPr?: string;
  readonly high24h?: string;
  readonly low24h?: string;
  readonly baseVol?: string;
  readonly quoteVol?: string;
  readonly baseVolume?: string;
  readonly quoteVolume?: string;
  readonly ts?: string;
}

export function fetchTick(
  symbol: string,
  marketType: MarketType,
): Effect.Effect<Tick, MarketDataError, never> {
  const { symbol: bSymbol, productType } = bitgetSymbol(symbol, marketType);
  const path =
    marketType === "spot"
      ? `/api/v2/spot/market/tickers?symbol=${bSymbol}`
      : `/api/v2/mix/market/tickers?productType=${productType}&symbol=${bSymbol}`;

  return Effect.gen(function* () {
    const data = yield* getJSON<readonly BitgetTicker[]>(path);
    const ticker = data[0];
    if (!ticker) {
      return yield* Effect.fail(
        new MarketDataError(`Bitget ticker not found for ${symbol}`),
      );
    }

    return {
      exchange: bitgetExchange(marketType),
      symbol,
      price: asNumber(ticker.lastPr ?? ticker.last),
      volume: asNumber(ticker.baseVol ?? ticker.baseVolume),
      bid: asNumber(ticker.bidPr),
      ask: asNumber(ticker.askPr),
      high24h: asNumber(ticker.high24h),
      low24h: asNumber(ticker.low24h),
      volume24h: asNumber(ticker.quoteVol ?? ticker.quoteVolume),
      timestamp: ticker.ts ? new Date(Number(ticker.ts)) : new Date(),
    };
  });
}

export function fetchOHLCV(
  symbol: string,
  timeframe: string,
  limit: number,
  startTime: Date | undefined,
  marketType: MarketType,
): Effect.Effect<readonly Candle[], MarketDataError, never> {
  const { symbol: bSymbol, productType } = bitgetSymbol(symbol, marketType);
  const granularity = bitgetGranularity(timeframe, marketType);
  const startParam = startTime
    ? `&startTime=${startTime.getTime()}&endTime=${Date.now()}`
    : "";

  const path =
    marketType === "spot"
      ? `/api/v2/spot/market/candles?symbol=${bSymbol}&granularity=${granularity}&limit=${limit}${startParam}`
      : `/api/v2/mix/market/candles?symbol=${bSymbol}&productType=${productType}&granularity=${granularity}&limit=${limit}${startParam}`;

  return Effect.gen(function* () {
    const data =
      yield* getJSON<
        ReadonlyArray<
          readonly [
            string,
            string,
            string,
            string,
            string,
            string,
            string,
            string,
          ]
        >
      >(path);

    return data.map(
      (c): Candle => ({
        exchange: bitgetExchange(marketType),
        symbol,
        timeframe,
        open: asNumber(c[1]),
        high: asNumber(c[2]),
        low: asNumber(c[3]),
        close: asNumber(c[4]),
        volume: asNumber(c[5]),
        timestamp: new Date(Number(c[0])),
      }),
    );
  });
}

interface BitgetOrderBook {
  readonly bids: ReadonlyArray<readonly [string, string]>;
  readonly asks: ReadonlyArray<readonly [string, string]>;
  readonly ts?: string;
}

export function fetchOrderBook(
  symbol: string,
  limit: number,
  marketType: MarketType,
): Effect.Effect<OrderBook, MarketDataError, never> {
  const { symbol: bSymbol, productType } = bitgetSymbol(symbol, marketType);
  const path =
    marketType === "spot"
      ? `/api/v2/spot/market/orderbook?symbol=${bSymbol}&type=step0&limit=${limit}`
      : `/api/v2/mix/market/orderbook?symbol=${bSymbol}&productType=${productType}&limit=${limit}`;

  return Effect.gen(function* () {
    const data = yield* getJSON<BitgetOrderBook>(path);

    return {
      exchange: bitgetExchange(marketType),
      symbol,
      bids: data.bids.map(([price, volume]) => ({
        price: asNumber(price),
        volume: asNumber(volume),
      })),
      asks: data.asks.map(([price, volume]) => ({
        price: asNumber(price),
        volume: asNumber(volume),
      })),
      timestamp: data.ts ? new Date(Number(data.ts)) : new Date(),
    };
  });
}

interface BitgetSymbolInfo {
  readonly symbol: string;
  readonly baseCoin: string;
  readonly quoteCoin: string;
  readonly status?: string;
}

export function fetchSymbols(
  marketType: MarketType,
): Effect.Effect<readonly string[], MarketDataError, never> {
  const path =
    marketType === "spot"
      ? "/api/v2/spot/public/symbols"
      : `/api/v2/mix/market/contracts?productType=USDT-FUTURES`;

  return Effect.gen(function* () {
    const data = yield* getJSON<readonly BitgetSymbolInfo[]>(path);

    return data
      .filter((s) => (s.status ?? "online") !== "offline")
      .map((s) => `${s.baseCoin}/${s.quoteCoin}`);
  });
}

export function fetch24hrVolumes(
  marketType: MarketType,
): Effect.Effect<Readonly<Record<string, number>>, MarketDataError, never> {
  const path =
    marketType === "spot"
      ? "/api/v2/spot/market/tickers"
      : "/api/v2/mix/market/tickers?productType=USDT-FUTURES";

  return Effect.gen(function* () {
    const data = yield* getJSON<readonly BitgetTicker[]>(path);

    const volumes: Record<string, number> = {};
    for (const ticker of data) {
      const normalized = `${ticker.symbol}`;
      volumes[normalized] = asNumber(ticker.quoteVol ?? ticker.quoteVolume);
    }
    return volumes;
  });
}

function bitgetGranularity(timeframe: string, marketType: MarketType): string {
  const spotMap: Record<string, string> = {
    "1m": "1min",
    "5m": "5min",
    "15m": "15min",
    "30m": "30min",
    "1h": "1h",
    "4h": "4h",
    "1d": "1day",
  };
  const futuresMap: Record<string, string> = {
    "1m": "1m",
    "5m": "5m",
    "15m": "15m",
    "30m": "30m",
    "1h": "1H",
    "4h": "4H",
    "1d": "1D",
  };
  return (marketType === "spot" ? spotMap : futuresMap)[timeframe] ?? timeframe;
}
