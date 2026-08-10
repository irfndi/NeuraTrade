import { describe, expect, it, beforeEach, afterEach } from "bun:test";
import { Effect } from "effect";
import * as Binance from "./binance.js";

describe("Binance gateway", () => {
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

  it("fetchTick parses 24hr ticker", async () => {
    mockFetch({
      symbol: "BTCUSDT",
      lastPrice: "67000.00",
      volume: "1234.56",
      bidPrice: "66990.00",
      askPrice: "67010.00",
      highPrice: "68000.00",
      lowPrice: "66000.00",
      quoteVolume: "78901234.00",
      closeTime: 1704067200000,
    });

    const tick = await Effect.runPromise(Binance.fetchTick("BTC/USDT"));

    expect(tick.exchange).toBe("binance");
    expect(tick.symbol).toBe("BTC/USDT");
    expect(tick.price).toBe(67000);
    expect(tick.bid).toBe(66990);
    expect(tick.ask).toBe(67010);
    expect(tick.timestamp.toISOString()).toBe("2024-01-01T00:00:00.000Z");
  });

  it("fetchOHLCV parses klines", async () => {
    mockFetch([
      [
        1704067200000,
        "66000",
        "68000",
        "65000",
        "67000",
        "100",
        1704067260000,
        "200",
        100,
        "50",
        "100",
        "0",
      ],
    ]);

    const candles = await Effect.runPromise(
      Binance.fetchOHLCV("BTC/USDT", "1m", 1),
    );

    expect(candles).toHaveLength(1);
    expect(candles[0].open).toBe(66000);
    expect(candles[0].high).toBe(68000);
    expect(candles[0].low).toBe(65000);
    expect(candles[0].close).toBe(67000);
    expect(candles[0].volume).toBe(100);
  });

  it("fetchOrderBook parses depth", async () => {
    mockFetch({
      lastUpdateId: 1,
      bids: [["66990", "1.5"]],
      asks: [["67010", "0.5"]],
    });

    const ob = await Effect.runPromise(Binance.fetchOrderBook("BTC/USDT", 5));

    expect(ob.bids).toEqual([{ price: 66990, volume: 1.5 }]);
    expect(ob.asks).toEqual([{ price: 67010, volume: 0.5 }]);
  });

  it("fetchSymbols filters only TRADING pairs", async () => {
    mockFetch({
      symbols: [
        {
          symbol: "BTCUSDT",
          status: "TRADING",
          baseAsset: "BTC",
          quoteAsset: "USDT",
        },
        {
          symbol: "BROKENPAIR",
          status: "BREAK",
          baseAsset: "BROKEN",
          quoteAsset: "USDT",
        },
      ],
    });

    const symbols = await Effect.runPromise(Binance.fetchSymbols());

    expect(symbols).toEqual(["BTC/USDT"]);
  });

  it("returns MarketDataError on HTTP failure", async () => {
    mockFetch({ msg: "bad request" }, 400);

    const result = await Effect.runPromise(
      Effect.result(Binance.fetchTick("BTC/USDT")),
    );

    expect(result._tag).toBe("Failure");
  });

  it("fetchTick rejects zero-price tickers", async () => {
    mockFetch({
      symbol: "BTCUSDT",
      lastPrice: "",
      volume: "0",
      bidPrice: "0",
      askPrice: "0",
      highPrice: "0",
      lowPrice: "0",
      quoteVolume: "0",
      openTime: 0,
      closeTime: 1704067200000,
    });

    const result = await Effect.runPromise(
      Binance.fetchTick("BTC/USDT").pipe(
        Effect.match({
          onFailure: (err) => ({ ok: false, reason: err.reason }),
          onSuccess: () => ({ ok: true, reason: "" }),
        }),
      ),
    );

    expect(result.ok).toBe(false);
    expect(result.reason).toContain("invalid price");
  });

  it("fetchFundingRates paginates until history is exhausted", async () => {
    const requests: string[] = [];
    const batch = [
      {
        symbol: "BTCUSDT",
        fundingRate: "0.0001",
        fundingTime: 1704067200000,
      },
      {
        symbol: "BTCUSDT",
        fundingRate: "0.00009",
        fundingTime: 1704038400000,
      },
    ];
    globalThis.fetch = (async (url: string | URL | Request) => {
      const u = String(url);
      requests.push(u);
      // Second page is past the end of history: empty batch terminates.
      const body = requests.length >= 2 ? [] : batch;
      return new Response(JSON.stringify(body), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }) as unknown as typeof fetch;

    const rates = await Effect.runPromise(
      Binance.fetchFundingRates(
        "BTC/USDT",
        new Date("2023-12-31T00:00:00Z"),
      ),
    );

    expect(requests).toHaveLength(2);
    expect(rates.map((r) => r.fundingRate)).toEqual([0.0001, 0.00009]);
    // Second request advances past the last row's fundingTime.
    const secondStart = new URL(requests[1]!).searchParams.get("startTime");
    expect(Number(secondStart)).toBe(1704038400000 + 1);
    expect(requests[1]).toContain("fapi.binance.com");
  });

  it("fetchFundingRates terminates when the API repeats the same window", async () => {
    const requests: string[] = [];
    const batch = [
      {
        symbol: "BTCUSDT",
        fundingRate: "0.0001",
        fundingTime: 1704067200000,
      },
    ];
    globalThis.fetch = (async (url: string | URL | Request) => {
      requests.push(String(url));
      return new Response(JSON.stringify(batch), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }) as unknown as typeof fetch;

    const rates = await Effect.runPromise(
      Binance.fetchFundingRates(
        "BTC/USDT",
        new Date("2023-12-31T00:00:00Z"),
      ),
    );

    // lastTime <= currentStart on the repeat page -> no infinite loop.
    expect(requests).toHaveLength(2);
    expect(rates).toHaveLength(2);
  });
});
