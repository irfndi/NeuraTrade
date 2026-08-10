import { describe, expect, it, beforeEach, afterEach } from "bun:test";
import { Effect } from "effect";
import * as Bybit from "./bybit.js";

// Bybit v5 envelopes: { retCode: 0, retMsg: "OK", result: {...} }.
const klineFixture = {
  retCode: 0,
  retMsg: "OK",
  result: {
    category: "linear",
    list: [
      // [start, open, high, low, close, volume, turnover]
      ["1704067200000", "66000", "68000", "65000", "67000", "100", "6700000"],
      ["1704063600000", "65000", "67000", "64500", "66000", "90", "5940000"],
    ],
  },
};

const tickersFixture = {
  retCode: 0,
  retMsg: "OK",
  result: {
    category: "linear",
    list: [
      {
        symbol: "BTCUSDT",
        lastPrice: "67000",
        bid1Price: "66990",
        ask1Price: "67010",
        highPrice24h: "68000",
        lowPrice24h: "66000",
        volume24h: "1000",
        turnover24h: "67000000",
      },
    ],
  },
};

const instrumentsFixture = {
  retCode: 0,
  retMsg: "OK",
  result: {
    category: "linear",
    list: [
      { symbol: "BTCUSDT", baseCoin: "BTC", quoteCoin: "USDT", status: "Trading" },
      { symbol: "ETHUSDT", baseCoin: "ETH", quoteCoin: "USDT", status: "Trading" },
      {
        symbol: "SOLUSDT",
        baseCoin: "SOL",
        quoteCoin: "USDT",
        status: "PreLaunch",
      },
      {
        symbol: "XRPUSDT",
        baseCoin: "XRP",
        quoteCoin: "USDT",
        status: "Closed",
      },
    ],
    nextPageCursor: undefined,
  },
};

const orderbookFixture = {
  retCode: 0,
  retMsg: "OK",
  result: {
    s: "BTCUSDT",
    b: [
      ["66990", "1.5"],
      ["66980", "2"],
    ],
    a: [
      ["67010", "0.5"],
      ["67020", "1"],
    ],
    ts: "1704067200000",
  },
};

const fundingFixture = {
  retCode: 0,
  retMsg: "OK",
  result: {
    category: "linear",
    list: [
      { symbol: "BTCUSDT", fundingRate: "0.0001", fundingTime: "1704067200000" },
      {
        symbol: "BTCUSDT",
        fundingRate: "-0.00005",
        fundingTime: "1704038400000",
      },
    ],
    nextPageCursor: undefined,
  },
};

describe("Bybit gateway", () => {
  let originalFetch: typeof fetch;

  beforeEach(() => {
    originalFetch = globalThis.fetch;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  function mockFetch(response: unknown, status = 200) {
    globalThis.fetch = (async () =>
      new Response(JSON.stringify(response), {
        status,
        headers: { "Content-Type": "application/json" },
      })) as unknown as typeof fetch;
  }

  it("fetchTick parses ticker", async () => {
    mockFetch(tickersFixture);

    const tick = await Effect.runPromise(Bybit.fetchTick("BTC/USDT"));

    expect(tick.exchange).toBe("bybit-futures");
    expect(tick.symbol).toBe("BTC/USDT");
    expect(tick.price).toBe(67000);
    expect(tick.bid).toBe(66990);
    expect(tick.ask).toBe(67010);
    expect(tick.high24h).toBe(68000);
    expect(tick.low24h).toBe(66000);
  });

  it("fetchTick converts canonical futures symbol", async () => {
    let requestedUrl = "";
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      requestedUrl = String(input);
      return new Response(JSON.stringify(tickersFixture), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }) as unknown as typeof fetch;

    const tick = await Effect.runPromise(Bybit.fetchTick("BTC/USDT:USDT"));

    expect(tick.symbol).toBe("BTC/USDT:USDT");
    expect(requestedUrl).toContain("symbol=BTCUSDT");
  });

  it("fetchOHLCV parses kline rows to candles", async () => {
    mockFetch(klineFixture);

    const candles = await Effect.runPromise(
      Bybit.fetchOHLCV("BTC/USDT", "15m", 2, undefined),
    );

    expect(candles).toHaveLength(2);
    expect(candles[0].open).toBe(66000);
    expect(candles[0].high).toBe(68000);
    expect(candles[0].low).toBe(65000);
    expect(candles[0].close).toBe(67000);
    expect(candles[0].volume).toBe(100);
    expect(candles[0].exchange).toBe("bybit-futures");
    expect(candles[0].timestamp.toISOString()).toBe("2024-01-01T00:00:00.000Z");
  });

  it("fetchOHLCV maps timeframe to bybit interval and passes start", async () => {
    let requestedUrl = "";
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      requestedUrl = String(input);
      return new Response(JSON.stringify(klineFixture), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }) as unknown as typeof fetch;

    await Effect.runPromise(
      Bybit.fetchOHLCV(
        "BTC/USDT:USDT",
        "15m",
        200,
        new Date("2025-01-01T00:00:00Z"),
      ),
    );

    expect(requestedUrl).toContain("/v5/market/kline");
    expect(requestedUrl).toContain("category=linear");
    expect(requestedUrl).toContain("symbol=BTCUSDT");
    expect(requestedUrl).toContain("interval=15");
    expect(requestedUrl).toContain("limit=200");
    expect(requestedUrl).toContain("start=1735689600000");
  });

  it("fetchOrderBook parses depth", async () => {
    mockFetch(orderbookFixture);

    const ob = await Effect.runPromise(Bybit.fetchOrderBook("BTC/USDT", 5));

    expect(ob.bids).toEqual([
      { price: 66990, volume: 1.5 },
      { price: 66980, volume: 2 },
    ]);
    expect(ob.asks).toEqual([
      { price: 67010, volume: 0.5 },
      { price: 67020, volume: 1 },
    ]);
    expect(ob.timestamp.toISOString()).toBe("2024-01-01T00:00:00.000Z");
  });

  it("fetchSymbols filters non-trading instruments and normalizes", async () => {
    mockFetch(instrumentsFixture);

    const symbols = await Effect.runPromise(Bybit.fetchSymbols());

    expect(symbols).toEqual(["BTC/USDT", "ETH/USDT"]);
  });

  it("fetchDemoSymbols returns the testnet list", async () => {
    mockFetch(instrumentsFixture);

    const symbols = await Effect.runPromise(Bybit.fetchDemoSymbols());

    expect(symbols).toEqual(["BTC/USDT", "ETH/USDT"]);
  });

  it("fetch24hrVolumes keys quote volume by raw symbol", async () => {
    mockFetch(tickersFixture);

    const volumes = await Effect.runPromise(Bybit.fetch24hrVolumes());

    expect(volumes["BTCUSDT"]).toBe(67000000);
  });

  it("fetchFundingRates maps funding history rows", async () => {
    mockFetch(fundingFixture);

    const rates = await Effect.runPromise(
      Bybit.fetchFundingRates("BTC/USDT:USDT"),
    );

    expect(rates).toHaveLength(2);
    expect(rates[0].exchange).toBe("bybit-futures");
    expect(rates[0].symbol).toBe("BTC/USDT:USDT");
    expect(rates[0].fundingRate).toBe(0.0001);
    expect(rates[0].timestamp.toISOString()).toBe("2024-01-01T00:00:00.000Z");
    expect(rates[1].fundingRate).toBe(-0.00005);
    expect(rates[1].timestamp.toISOString()).toBe("2023-12-31T16:00:00.000Z");
  });

  it("returns MarketDataError on HTTP failure", async () => {
    mockFetch({ retMsg: "bad request" }, 400);

    const result = await Effect.runPromise(
      Effect.result(Bybit.fetchTick("BTC/USDT")),
    );

    expect(result._tag).toBe("Failure");
  });

  it("returns MarketDataError on non-zero retCode", async () => {
    mockFetch({ retCode: 10001, retMsg: "invalid symbol", result: null }, 200);

    const result = await Effect.runPromise(
      Effect.result(Bybit.fetchTick("BTC/USDT")),
    );

    expect(result._tag).toBe("Failure");
  });

  it("returns MarketDataError when ticker data is empty", async () => {
    mockFetch({ retCode: 0, retMsg: "OK", result: { list: [] } });

    const result = await Effect.runPromise(
      Effect.result(Bybit.fetchTick("BTC/USDT")),
    );

    expect(result._tag).toBe("Failure");
  });

  it("fetchTick rejects zero-price tickers", async () => {
    mockFetch({
      retCode: 0,
      retMsg: "OK",
      result: {
        category: "linear",
        list: [{ symbol: "BTCUSDT", lastPrice: "" }],
      },
    });

    const result = await Effect.runPromise(
      Bybit.fetchTick("BTC/USDT").pipe(
        Effect.match({
          onFailure: (err) => ({ ok: false, reason: err.reason }),
          onSuccess: () => ({ ok: true, reason: "" }),
        }),
      ),
    );

    expect(result.ok).toBe(false);
    expect(result.reason).toContain("invalid price");
  });

  it("fetchFundingRates stops once a page predates startTime", async () => {
    const requests: string[] = [];
    const start = new Date("2023-12-31T08:00:00Z"); // 1704009600000
    const pages: Record<string, unknown> = {
      first: {
        retCode: 0,
        retMsg: "OK",
        result: {
          category: "linear",
          list: [
            {
              symbol: "BTCUSDT",
              fundingRate: "0.0001",
              fundingTime: "1704067200000",
            },
            {
              symbol: "BTCUSDT",
              fundingRate: "0.00009",
              fundingTime: "1704038400000",
            },
          ],
          nextPageCursor: "page2",
        },
      },
      page2: {
        retCode: 0,
        retMsg: "OK",
        result: {
          category: "linear",
          list: [
            {
              symbol: "BTCUSDT",
              fundingRate: "0.00008",
              fundingTime: "1704009600000",
            },
            {
              symbol: "BTCUSDT",
              fundingRate: "0.00007",
              fundingTime: "1703980800000",
            },
          ],
          nextPageCursor: "page3",
        },
      },
    };
    globalThis.fetch = (async (url: string | URL | Request) => {
      const u = String(url);
      requests.push(u);
      const body = u.includes("cursor=page2") ? pages.page2 : pages.first;
      return new Response(JSON.stringify(body), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }) as unknown as typeof fetch;

    const rates = await Effect.runPromise(
      Bybit.fetchFundingRates("BTC/USDT:USDT", start),
    );

    // Page 2's oldest row (1703980800000) predates startTime; page3 exists
    // in the fixture but must never be fetched.
    expect(requests).toHaveLength(2);
    expect(rates.map((r) => r.fundingRate)).toEqual([0.0001, 0.00009, 0.00008]);
    expect(requests.some((u) => u.includes("cursor=page3"))).toBe(false);
  });

  it("paginates instruments-info with cursor", async () => {
    const requests: string[] = [];
    const pages: Record<string, unknown> = {
      first: {
        retCode: 0,
        retMsg: "OK",
        result: {
          category: "linear",
          list: [{ symbol: "BTCUSDT", baseCoin: "BTC", quoteCoin: "USDT", status: "Trading" }],
          nextPageCursor: "page2",
        },
      },
      page2: {
        retCode: 0,
        retMsg: "OK",
        result: {
          category: "linear",
          list: [{ symbol: "ETHUSDT", baseCoin: "ETH", quoteCoin: "USDT", status: "Trading" }],
          nextPageCursor: undefined,
        },
      },
    };
    globalThis.fetch = (async (url: string | URL | Request) => {
      const u = String(url);
      requests.push(u);
      const body = u.includes("cursor=page2") ? pages.page2 : pages.first;
      return new Response(JSON.stringify(body), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }) as unknown as typeof fetch;

    const symbols = await Effect.runPromise(Bybit.fetchSymbols());

    expect(symbols).toEqual(["BTC/USDT", "ETH/USDT"]);
    expect(requests).toHaveLength(2);
    expect(requests[0]).toContain("/v5/market/instruments-info");
    expect(requests[1]).toContain("cursor=page2");
  });
});
