import { Effect } from "effect";
import type { Candle, FundingRate, OrderBook, Tick } from "../types.js";
import { MarketDataError } from "../gateway.js";

const BASE_URL = "https://api.binance.com";
const FUTURES_BASE_URL = "https://fapi.binance.com";
const DEFAULT_TIMEOUT_MS = 30_000;

function binanceSymbol(symbol: string): string {
  return symbol.replace("/", "").toUpperCase();
}

function getJSON<T>(
  path: string,
  timeoutMs = DEFAULT_TIMEOUT_MS,
  baseUrl = BASE_URL,
): Effect.Effect<T, MarketDataError, never> {
  return Effect.gen(function* () {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), timeoutMs);

    const response = yield* Effect.tryPromise({
      try: () =>
        fetch(`${baseUrl}${path}`, {
          method: "GET",
          signal: controller.signal,
          headers: { Accept: "application/json" },
        }),
      catch: (err) =>
        new MarketDataError(
          `Binance network error for ${path}: ${err instanceof Error ? err.message : String(err)}`,
          err,
        ),
    }).pipe(Effect.tap(() => Effect.sync(() => clearTimeout(timer))));

    if (!response.ok) {
      return yield* Effect.fail(
        new MarketDataError(`Binance HTTP ${response.status} for ${path}`),
      );
    }

    return yield* Effect.tryPromise({
      try: () => response.json() as Promise<T>,
      catch: (err) =>
        new MarketDataError(
          `Binance JSON parse error for ${path}: ${err instanceof Error ? err.message : String(err)}`,
          err,
        ),
    });
  });
}

export function fetchTick(
  symbol: string,
): Effect.Effect<Tick, MarketDataError, never> {
  const bSymbol = binanceSymbol(symbol);
  return Effect.gen(function* () {
    const data = yield* getJSON<{
      symbol: string;
      lastPrice: string;
      volume: string;
      bidPrice: string;
      askPrice: string;
      highPrice: string;
      lowPrice: string;
      quoteVolume: string;
      openTime: number;
      closeTime: number;
    }>(`/api/v3/ticker/24hr?symbol=${bSymbol}`);

    const price = Number(data.lastPrice);
    if (!Number.isFinite(price) || price <= 0) {
      return yield* Effect.fail(
        new MarketDataError(`Binance ticker has invalid price for ${symbol}`),
      );
    }

    return {
      exchange: "binance",
      symbol,
      price,
      volume: Number(data.volume),
      bid: Number(data.bidPrice),
      ask: Number(data.askPrice),
      high24h: Number(data.highPrice),
      low24h: Number(data.lowPrice),
      volume24h: Number(data.quoteVolume),
      timestamp: new Date(data.closeTime),
    };
  });
}

export function fetchOHLCV(
  symbol: string,
  timeframe: string,
  limit: number,
  startTime?: Date,
): Effect.Effect<readonly Candle[], MarketDataError, never> {
  const bSymbol = binanceSymbol(symbol);
  const interval = binanceInterval(timeframe);
  const startParam = startTime ? `&startTime=${startTime.getTime()}` : "";
  return Effect.gen(function* () {
    const data = yield* getJSON<
      Array<
        [
          number,
          string,
          string,
          string,
          string,
          string,
          number,
          string,
          number,
          string,
          string,
          string,
        ]
      >
    >(
      `/api/v3/klines?symbol=${bSymbol}&interval=${interval}&limit=${limit}${startParam}`,
    );

    return data.map((c): Candle => ({
      exchange: "binance",
      symbol,
      timeframe,
      open: Number(c[1]),
      high: Number(c[2]),
      low: Number(c[3]),
      close: Number(c[4]),
      volume: Number(c[5]),
      timestamp: new Date(c[0]),
    }));
  });
}

export function fetchOrderBook(
  symbol: string,
  limit: number,
): Effect.Effect<OrderBook, MarketDataError, never> {
  const bSymbol = binanceSymbol(symbol);
  return Effect.gen(function* () {
    const data = yield* getJSON<{
      lastUpdateId: number;
      bids: Array<[string, string]>;
      asks: Array<[string, string]>;
    }>(`/api/v3/depth?symbol=${bSymbol}&limit=${limit}`);

    return {
      exchange: "binance",
      symbol,
      bids: data.bids.map(([price, volume]) => ({
        price: Number(price),
        volume: Number(volume),
      })),
      asks: data.asks.map(([price, volume]) => ({
        price: Number(price),
        volume: Number(volume),
      })),
      timestamp: new Date(),
    };
  });
}

export function fetchSymbols(): Effect.Effect<
  readonly string[],
  MarketDataError,
  never
> {
  return Effect.gen(function* () {
    const data = yield* getJSON<{
      symbols: Array<{
        symbol: string;
        status: string;
        baseAsset: string;
        quoteAsset: string;
      }>;
    }>("/api/v3/exchangeInfo");

    return data.symbols
      .filter((s) => s.status === "TRADING")
      .map((s) => `${s.baseAsset}/${s.quoteAsset}`);
  });
}

export function fetch24hrVolumes(): Effect.Effect<
  Readonly<Record<string, number>>,
  MarketDataError,
  never
> {
  return Effect.gen(function* () {
    const data = yield* getJSON<
      Array<{ symbol: string; volume: string; quoteVolume: string }>
    >("/api/v3/ticker/24hr");

    const volumes: Record<string, number> = {};
    for (const ticker of data) {
      volumes[ticker.symbol] = Number(ticker.quoteVolume);
    }
    return volumes;
  });
}

function binanceInterval(timeframe: string): string {
  switch (timeframe) {
    case "1m":
      return "1m";
    case "5m":
      return "5m";
    case "15m":
      return "15m";
    case "30m":
      return "30m";
    case "1h":
      return "1h";
    case "4h":
      return "4h";
    case "1d":
      return "1d";
    default:
      return timeframe;
  }
}

const FUNDING_LIMIT = 1000;

export function fetchFundingRates(
  symbol: string,
  startTime?: Date,
  endTime?: Date,
  limit = FUNDING_LIMIT,
): Effect.Effect<readonly FundingRate[], MarketDataError, never> {
  const bSymbol = binanceSymbol(symbol);
  const effectiveEndTime = endTime ? endTime.getTime() : Date.now();
  let currentStart = startTime ? startTime.getTime() : 0;

  return Effect.gen(function* () {
    const results: FundingRate[] = [];
    while (currentStart < effectiveEndTime) {
      const path =
        `/fapi/v1/fundingRate?symbol=${bSymbol}&limit=${limit}` +
        `&startTime=${currentStart}`;
      const batch = yield* getJSON<
        Array<{ symbol: string; fundingRate: string; fundingTime: number }>
      >(path, DEFAULT_TIMEOUT_MS, FUTURES_BASE_URL);

      if (batch.length === 0) break;

      for (const r of batch) {
        const time = r.fundingTime;
        if (time > effectiveEndTime) continue;
        results.push({
          exchange: "binance",
          symbol,
          fundingRate: Number(r.fundingRate),
          timestamp: new Date(time),
        });
      }

      const lastTime = batch[batch.length - 1]?.fundingTime;
      if (!lastTime || lastTime <= currentStart) break;
      currentStart = lastTime + 1;
    }
    return results;
  });
}
