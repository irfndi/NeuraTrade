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

/** Open-interest interval buckets keyed by timeframe. */
interface OiIntervalMap {
  [key: string]: string;
}
/** Kline interval buckets keyed by timeframe. */
interface BybitIntervalMap {
  [key: string]: string;
}

function getJSON<T>(
  path: string,
  baseUrl = BASE_URL,
  timeoutMs = DEFAULT_TIMEOUT_MS,
  extraHeaders: Record<string, string> = {},
): Effect.Effect<T, MarketDataError, never> {
  return Effect.gen(function* () {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), timeoutMs);

    const response = yield* Effect.tryPromise({
      try: () =>
        fetch(`${baseUrl}${path}`, {
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
  baseUrl = BASE_URL,
): Effect.Effect<Tick, MarketDataError, never> {
  const bSymbol = toBybitSymbol(symbol);
  return Effect.gen(function* () {
    const data = yield* getJSON<{ readonly list: readonly BybitTicker[] }>(
      `/v5/market/tickers?category=linear&symbol=${bSymbol}`,
      baseUrl,
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
  baseUrl = BASE_URL,
): Effect.Effect<readonly Candle[], MarketDataError, never> {
  const bSymbol = toBybitSymbol(symbol);
  const interval = bybitInterval(timeframe);
  // `startTime` is a BACKWARD cursor: Bybit v5 kline's `start` param is the
  // window START (ascending), so passing it there returns the same newest
  // window every call. Paging older requires `end` = cursor − 1ms
  // (verified 2026-08-10: without this every page returned the identical
  // newest 1000 bars). The funnel's deep-history deepening shares this
  // gateway — the old behavior capped the cache at ~1 window.
  const endParam = startTime ? `&end=${startTime.getTime()}` : "";

  return Effect.gen(function* () {
    const data = yield* getJSON<{
      readonly list: ReadonlyArray<
        readonly [string, string, string, string, string, string, string]
      >;
    }>(
      `/v5/market/kline?category=linear&symbol=${bSymbol}&interval=${interval}&limit=${limit}${endParam}`,
      baseUrl,
    );

    // Bybit returns klines newest-first.  Every consumer advances candles in
    // chronological order; leaving the wire order intact makes last-candle
    // state move backwards and reprocess the same bar forever.
    return data.list
      .map((c): Candle => ({
        exchange: EXCHANGE,
        symbol,
        timeframe,
        open: asNumber(c[1]),
        high: asNumber(c[2]),
        low: asNumber(c[3]),
        close: asNumber(c[4]),
        volume: asNumber(c[5]),
        timestamp: new Date(Number(c[0])),
      }))
      .sort((a, b) => a.timestamp.getTime() - b.timestamp.getTime());
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
  baseUrl = BASE_URL,
): Effect.Effect<OrderBook, MarketDataError, never> {
  const bSymbol = toBybitSymbol(symbol);
  return Effect.gen(function* () {
    const data = yield* getJSON<BybitOrderBook>(
      `/v5/market/orderbook?category=linear&symbol=${bSymbol}&limit=${limit}`,
      baseUrl,
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

export function fetchSymbols(
  baseUrl = BASE_URL,
): Effect.Effect<readonly string[], MarketDataError, never> {
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
        baseUrl,
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
export function fetchDemoSymbols(
  baseUrl = BASE_URL,
): Effect.Effect<readonly string[], MarketDataError, never> {
  return fetchSymbols(baseUrl);
}

export function fetch24hrVolumes(
  baseUrl = BASE_URL,
): Effect.Effect<Readonly<Record<string, number>>, MarketDataError, never> {
  return Effect.gen(function* () {
    const data = yield* getJSON<{ readonly list: readonly BybitTicker[] }>(
      "/v5/market/tickers?category=linear",
      baseUrl,
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

/**
 * A ticker row with the top-of-book spread kept: 24h quote turnover plus the
 * best bid/ask. `fetch24hrVolumes` drops the quotes; flow-universe builders
 * need them to rank by spread.
 */
export interface TickerInfo {
  readonly symbol: string;
  readonly turnover24h: number;
  readonly bid1Price?: number;
  readonly ask1Price?: number;
}

/** Fetch every linear ticker with turnover and top-of-book bid/ask. */
export function fetchTickers(
  baseUrl = BASE_URL,
): Effect.Effect<readonly TickerInfo[], MarketDataError, never> {
  return Effect.gen(function* () {
    const data = yield* getJSON<{ readonly list: readonly BybitTicker[] }>(
      "/v5/market/tickers?category=linear",
      baseUrl,
    );
    const tickers: TickerInfo[] = [];
    for (const ticker of data.list) {
      const bid = ticker.bid1Price;
      const ask = ticker.ask1Price;
      tickers.push({
        symbol: ticker.symbol,
        turnover24h: asNumber(
          ticker.quoteVol ?? ticker.quoteVolume ?? ticker.turnover24h,
        ),
        bid1Price: bid !== undefined && bid !== "" ? asNumber(bid) : undefined,
        ask1Price: ask !== undefined && ask !== "" ? asNumber(ask) : undefined,
      });
    }
    return tickers;
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
  baseUrl = BASE_URL,
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
        baseUrl,
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

interface BybitOpenInterestRow {
  readonly symbol: string;
  readonly timestamp: string;
  /** Mainnet v5 uses `openInterest`; testnet historically used `oi`. */
  readonly openInterest?: string;
  readonly oi?: string;
  /** Not present on mainnet v5; kept for testnet compatibility. */
  readonly oiValue?: string;
}

/**
 * A single open-interest history point (ms epoch, base-contract OI and
 * quote-currency OI value).
 */
export interface OpenInterestRow {
  readonly timestamp: number;
  readonly oi: number;
  readonly oiValue: number;
}

const OI_PAGE_SIZE = 1000;
const OI_MAX_PAGES = 50;

/**
 * Map a timeframe to the open-interest endpoint's intervalTime. Bybit only
 * exposes 5min/15min/30min/1h/4h/1d buckets; anything else falls back to
 * 5min.
 */
function oiInterval(timeframe: string): string {
  const map: OiIntervalMap = {
    "5m": "5min",
    "15m": "15min",
    "30m": "30min",
    "1h": "1h",
    "4h": "4h",
    "1d": "1d",
  };
  return map[timeframe] ?? "5min";
}

/**
 * Fetch open-interest history for a USDT-perp. Rows are newest-first;
 * pagination stops on an empty page or at OI_MAX_PAGES.
 */
export function fetchOpenInterest(
  symbol: string,
  timeframe = "5m",
  baseUrl = BASE_URL,
  startTime?: number,
  endTime?: number,
): Effect.Effect<readonly OpenInterestRow[], MarketDataError, never> {
  const bSymbol = toBybitSymbol(symbol);
  const interval = oiInterval(timeframe);
  return Effect.gen(function* () {
    const rows: OpenInterestRow[] = [];
    let cursor: string | undefined;
    for (let page = 0; page < OI_MAX_PAGES; page++) {
      const cursorParam = cursor ? `&cursor=${cursor}` : "";
      const startParam =
        startTime !== undefined ? `&startTime=${startTime}` : "";
      const endParam = endTime !== undefined ? `&endTime=${endTime}` : "";
      const data = yield* getJSON<{
        readonly list: readonly BybitOpenInterestRow[];
        readonly nextPageCursor?: string;
      }>(
        `/v5/market/open-interest?category=linear&symbol=${bSymbol}&intervalTime=${interval}&limit=${OI_PAGE_SIZE}${cursorParam}${startParam}${endParam}`,
        baseUrl,
      );
      if (data.list.length === 0) break;
      for (const r of data.list) {
        rows.push({
          timestamp: Number(r.timestamp),
          oi: asNumber(r.openInterest ?? r.oi),
          oiValue: asNumber(r.oiValue),
        });
      }
      const next = data.nextPageCursor;
      if (!next) break;
      cursor = next;
    }
    return rows;
  });
}

/**
 * A single recent public trade print (ms epoch, taker side, base-contract
 * size, price).
 */
export interface RecentTrade {
  readonly time: number;
  readonly side: "Buy" | "Sell";
  readonly size: number;
  readonly price: number;
}

interface BybitRecentTrade {
  readonly time: string;
  readonly side: "Buy" | "Sell";
  readonly size: string;
  readonly price: string;
}

/** Fetch the most recent public trades (max 1000 per call). */
export function fetchRecentTrades(
  symbol: string,
  baseUrl = BASE_URL,
  limit = 500,
): Effect.Effect<readonly RecentTrade[], MarketDataError, never> {
  const bSymbol = toBybitSymbol(symbol);
  return Effect.gen(function* () {
    const data = yield* getJSON<{ readonly list: readonly BybitRecentTrade[] }>(
      `/v5/market/recent-trade?category=linear&symbol=${bSymbol}&limit=${limit}`,
      baseUrl,
    );
    return data.list.map((t) => ({
      time: Number(t.time),
      side: t.side,
      size: asNumber(t.size),
      price: asNumber(t.price),
    }));
  });
}

/**
 * A single instrument-info row: trading status, listing time (ms epoch),
 * and the current top-of-book bid/ask (empty string when no quote exists).
 */
export interface InstrumentInfo {
  readonly symbol: string;
  readonly status?: string;
  readonly listedTime?: number;
  readonly bid1Price?: number;
  readonly ask1Price?: number;
}

interface BybitInstrumentInfo {
  readonly symbol: string;
  readonly status?: string;
  /** Mainnet v5 names the listing time `launchTime`. */
  readonly launchTime?: string;
  readonly listedTime?: string;
  readonly bid1Price?: string;
  readonly ask1Price?: string;
}

/**
 * Fetch the full linear instruments-info list (all statuses, not just
 * Trading) so callers can rank a universe by spread and contract age.
 */
export function fetchInstruments(
  baseUrl = BASE_URL,
): Effect.Effect<readonly InstrumentInfo[], MarketDataError, never> {
  return Effect.gen(function* () {
    const instruments: InstrumentInfo[] = [];
    let cursor: string | undefined;
    for (let page = 0; page < SYMBOLS_MAX_PAGES; page++) {
      const cursorParam = cursor ? `&cursor=${cursor}` : "";
      const data = yield* getJSON<{
        readonly list: readonly BybitInstrumentInfo[];
        readonly nextPageCursor?: string;
      }>(
        `/v5/market/instruments-info?category=linear&limit=${SYMBOLS_PAGE_SIZE}${cursorParam}`,
        baseUrl,
      );
      if (data.list.length === 0) break;
      for (const s of data.list) {
        const listedTimeRaw = s.launchTime ?? s.listedTime;
        instruments.push({
          symbol: s.symbol,
          status: s.status,
          listedTime:
            listedTimeRaw !== undefined ? Number(listedTimeRaw) : undefined,
          bid1Price:
            s.bid1Price !== undefined && s.bid1Price !== ""
              ? asNumber(s.bid1Price)
              : undefined,
          ask1Price:
            s.ask1Price !== undefined && s.ask1Price !== ""
              ? asNumber(s.ask1Price)
              : undefined,
        });
      }
      const next = data.nextPageCursor;
      if (!next) break;
      cursor = next;
    }
    return instruments;
  });
}

function bybitInterval(timeframe: string): string {
  const map: BybitIntervalMap = {
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
