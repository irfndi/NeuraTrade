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
});
