import { Effect } from "effect";
import type { Candle, FundingRate, OrderBook, Tick } from "../types.js";
import { MarketDataError } from "../gateway.js";

const BASE_URL = "https://api-testnet.bybit.com";
const DEFAULT_TIMEOUT_MS = 30_000;

/**
 * Bybit USDT-perpetual (linear) gateway.
 *
 * The testnet mirrors ~200+ USDT-perp contracts (vs Bitget demo's ~25), and
 * the testnet IS the demo environment: market data is public (no auth) and
 * the demo/funnel path trades against the same instrument set.
 */
const EXCHANGE = "bybit-futures";

function getJSON<T>(
  path: string,
  timeoutMs = DEFAULT_TIMEOUT_MS,
  extraHeaders: Record<string, string> = {},
): Effect.Effect<T, MarketDataError, never> {
  return Effect.gen(function* () {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), timeoutMs);

    const response = yield* Effect.tryPromise({
      try: () =>
        fetch(`${BASE_URL}${path}`, {
          method: "GET",
          signal: controller.signal,
          headers: { Accept: "application/json", ...extraHeaders },
        }),
      catch: (err) =>
        new MarketDataError(
          `Bybit network error for ${path}: ${err instanceof Error ? err.message : String(err)}`,
          err,
        ),
    }).pipe(Effect.tap(() => Effect.sync(() => clearTimeout(timer))));

    if (!response.ok) {
      return yield* Effect.fail(
        new MarketDataError(`Bybit HTTP ${response.status} for ${path}`),
      );
    }

    const envelope = yield* Effect.tryPromise({
      try: () => response.json() as Promise<BybitEnvelope<T>>,
      catch: (err) =>
        new MarketDataError(
          `Bybit JSON parse error for ${path}: ${err instanceof Error ? err.message : String(err)}`,
          err,
        ),
    });

    if (envelope.retCode !== 0) {
      return yield* Effect.fail(
        new MarketDataError(
          `Bybit API error ${envelope.retCode} for ${path}: ${envelope.retMsg ?? "unknown"}`,
        ),
      );
    }

    return envelope.result;
  });
}

interface BybitEnvelope<T> {
  readonly retCode: number;
  readonly retMsg?: string;
  readonly result: T;
}

/**
 * Normalize a canonical symbol ("BTC/USDT" or "BTC/USDT:USDT") to the Bybit
 * linear wire form "BTCUSDT".
 */
function toBybitSymbol(symbol: string): string {
  return symbol.replace("/", "").split(":")[0].toUpperCase();
}

function asNumber(value: string | number | undefined): number {
  if (value === undefined || value === null) return 0;
  return Number(value);
}

interface BybitTicker {
  readonly symbol: string;
  readonly lastPrice?: string;
  readonly bid1Price?: string;
  readonly ask1Price?: string;
  readonly highPrice24h?: string;
  readonly lowPrice24h?: string;
  readonly volume24h?: string;
  readonly quoteVol?: string;
  readonly quoteVolume?: string;
  readonly turnover24h?: string;
}

export function fetchTick(
  symbol: string,
): Effect.Effect<Tick, MarketDataError, never> {
  const bSymbol = toBybitSymbol(symbol);
  return Effect.gen(function* () {
    const data = yield* getJSON<{ readonly list: readonly BybitTicker[] }>(
      `/v5/market/tickers?category=linear&symbol=${bSymbol}`,
    );
    const ticker = data.list[0];
    if (!ticker) {
      return yield* Effect.fail(
        new MarketDataError(`Bybit ticker not found for ${symbol}`),
      );
    }

    const price = asNumber(ticker.lastPrice);
    if (!Number.isFinite(price) || price <= 0) {
      return yield* Effect.fail(
        new MarketDataError(`Bybit ticker has invalid price for ${symbol}`),
      );
    }

    return {
      exchange: EXCHANGE,
      symbol,
      price,
      volume: asNumber(ticker.volume24h),
      bid: asNumber(ticker.bid1Price),
      ask: asNumber(ticker.ask1Price),
      high24h: asNumber(ticker.highPrice24h),
      low24h: asNumber(ticker.lowPrice24h),
      volume24h: asNumber(
        ticker.quoteVol ?? ticker.quoteVolume ?? ticker.turnover24h,
      ),
      timestamp: new Date(),
    };
  });
}

export function fetchOHLCV(
  symbol: string,
  timeframe: string,
  limit: number,
  startTime: Date | undefined,
): Effect.Effect<readonly Candle[], MarketDataError, never> {
  const bSymbol = toBybitSymbol(symbol);
  const interval = bybitInterval(timeframe);
  const startParam = startTime ? `&start=${startTime.getTime()}` : "";

  return Effect.gen(function* () {
    const data = yield* getJSON<{
      readonly list: ReadonlyArray<
        readonly [string, string, string, string, string, string, string]
      >;
    }>(
      `/v5/market/kline?category=linear&symbol=${bSymbol}&interval=${interval}&limit=${limit}${startParam}`,
    );

    return data.list.map((c): Candle => ({
      exchange: EXCHANGE,
      symbol,
      timeframe,
      open: asNumber(c[1]),
      high: asNumber(c[2]),
      low: asNumber(c[3]),
      close: asNumber(c[4]),
      volume: asNumber(c[5]),
      timestamp: new Date(Number(c[0])),
    }));
  });
}

interface BybitOrderBook {
  readonly b: ReadonlyArray<readonly [string, string]>;
  readonly a: ReadonlyArray<readonly [string, string]>;
  readonly ts?: string;
}

export function fetchOrderBook(
  symbol: string,
  limit: number,
): Effect.Effect<OrderBook, MarketDataError, never> {
  const bSymbol = toBybitSymbol(symbol);
  return Effect.gen(function* () {
    const data = yield* getJSON<BybitOrderBook>(
      `/v5/market/orderbook?category=linear&symbol=${bSymbol}&limit=${limit}`,
    );

    return {
      exchange: EXCHANGE,
      symbol,
      bids: data.b.map(([price, volume]) => ({
        price: asNumber(price),
        volume: asNumber(volume),
      })),
      asks: data.a.map(([price, volume]) => ({
        price: asNumber(price),
        volume: asNumber(volume),
      })),
      timestamp: data.ts ? new Date(Number(data.ts)) : new Date(),
    };
  });
}

interface BybitSymbolInfo {
  readonly symbol: string;
  readonly baseCoin: string;
  readonly quoteCoin: string;
  readonly status?: string;
}

const SYMBOLS_PAGE_SIZE = 1000;
const SYMBOLS_MAX_PAGES = 20;

export function fetchSymbols(): Effect.Effect<
  readonly string[],
  MarketDataError,
  never
> {
  return Effect.gen(function* () {
    const symbols: string[] = [];
    let cursor: string | undefined;
    for (let page = 0; page < SYMBOLS_MAX_PAGES; page++) {
      const cursorParam = cursor ? `&cursor=${cursor}` : "";
      const data = yield* getJSON<{
        readonly list: readonly BybitSymbolInfo[];
        readonly nextPageCursor?: string;
      }>(
        `/v5/market/instruments-info?category=linear&limit=${SYMBOLS_PAGE_SIZE}${cursorParam}`,
      );
      for (const s of data.list) {
        // Bybit instruments-info: "Trading" is the online status
        // ("PreLaunch"/"Settling"/"Delivering"/"Closed" are not tradeable).
        if (s.status === "Trading") {
          symbols.push(`${s.baseCoin}/${s.quoteCoin}`);
        }
      }
      const next = data.nextPageCursor;
      if (!next || data.list.length === 0) break;
      cursor = next;
    }
    return symbols;
  });
}

/**
 * Demo-tradeable futures contracts. Bybit's testnet IS the demo
 * environment: the same instrument list is tradeable on the demo/funnel
 * path, so the demo universe is the full testnet list (~200+ USDT-perp
 * contracts, vs Bitget demo's ~25). The market funnel scans this set.
 */
export function fetchDemoSymbols(): Effect.Effect<
  readonly string[],
  MarketDataError,
  never
> {
  return fetchSymbols();
}

export function fetch24hrVolumes(): Effect.Effect<
  Readonly<Record<string, number>>,
  MarketDataError,
  never
> {
  return Effect.gen(function* () {
    const data = yield* getJSON<{ readonly list: readonly BybitTicker[] }>(
      "/v5/market/tickers?category=linear",
    );

    const volumes: Record<string, number> = {};
    for (const ticker of data.list) {
      volumes[ticker.symbol] = asNumber(
        ticker.quoteVol ?? ticker.quoteVolume ?? ticker.turnover24h,
      );
    }
    return volumes;
  });
}

interface BybitFundingRate {
  readonly symbol: string;
  readonly fundingRate: string;
  readonly fundingTime: string;
}

const FUNDING_PAGE_SIZE = 200;
const FUNDING_MAX_PAGES = 50;

/**
 * Fetch USDT-perp funding-rate history from the testnet. Rows are
 * newest-first; pagination stops on an empty page, when `limit` rows are
 * accumulated, or at FUNDING_MAX_PAGES.
 */
export function fetchFundingRates(
  symbol: string,
  startTime?: Date,
  endTime?: Date,
  limit = FUNDING_PAGE_SIZE * FUNDING_MAX_PAGES,
): Effect.Effect<readonly FundingRate[], MarketDataError, never> {
  const bSymbol = toBybitSymbol(symbol);
  const startMs = startTime?.getTime();
  const endMs = endTime?.getTime();

  return Effect.gen(function* () {
    const results: FundingRate[] = [];
    let cursor: string | undefined;
    for (let page = 0; page < FUNDING_MAX_PAGES; page++) {
      const startParam = startMs !== undefined ? `&startTime=${startMs}` : "";
      const endParam = endMs !== undefined ? `&endTime=${endMs}` : "";
      const cursorParam = cursor ? `&cursor=${cursor}` : "";
      const data = yield* getJSON<{
        readonly list: readonly BybitFundingRate[];
        readonly nextPageCursor?: string;
      }>(
        `/v5/market/funding/history?category=linear&symbol=${bSymbol}&limit=${FUNDING_PAGE_SIZE}${startParam}${endParam}${cursorParam}`,
      );
      if (data.list.length === 0) break;

      let oldestOnPage = Number.POSITIVE_INFINITY;
      for (const r of data.list) {
        const time = Number(r.fundingTime);
        if (time < oldestOnPage) oldestOnPage = time;
        if (startMs !== undefined && time < startMs) continue;
        if (endMs !== undefined && time > endMs) continue;
        results.push({
          exchange: EXCHANGE,
          symbol,
          fundingRate: asNumber(r.fundingRate),
          timestamp: new Date(time),
        });
      }

      if (results.length >= limit) break;
      // Rows are newest-first, so once the oldest row on a page predates
      // startMs no later page can contain in-window rows (this also covers
      // a fully-skipped page whose rows are all older than startMs).
      if (startMs !== undefined && oldestOnPage < startMs) break;
      const next = data.nextPageCursor;
      if (!next) break;
      cursor = next;
    }
    return results;
  });
}

function bybitInterval(timeframe: string): string {
  const map: Record<string, string> = {
    "1m": "1",
    "5m": "5",
    "15m": "15",
    "30m": "30",
    "1h": "60",
    "4h": "240",
    "1d": "D",
    "1w": "W",
    "1M": "M",
  };
  return map[timeframe] ?? timeframe;
}
