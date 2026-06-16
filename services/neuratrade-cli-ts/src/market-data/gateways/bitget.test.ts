import { describe, expect, it, beforeEach, afterEach } from "bun:test";
import { Effect } from "effect";
import * as Bitget from "./bitget.js";
import candlesFixture from "../../../tests/fixtures/bitget/candles.json";
import orderbookFixture from "../../../tests/fixtures/bitget/orderbook.json";
import tickersFixture from "../../../tests/fixtures/bitget/tickers.json";
import symbolsFixture from "../../../tests/fixtures/bitget/symbols.json";

describe("Bitget gateway", () => {
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

  it("fetchTick parses spot ticker", async () => {
    mockFetch(tickersFixture);

    const tick = await Effect.runPromise(Bitget.fetchTick("BTC/USDT", "spot"));

    expect(tick.exchange).toBe("bitget");
    expect(tick.symbol).toBe("BTC/USDT");
    expect(tick.price).toBe(67000);
    expect(tick.bid).toBe(66990);
    expect(tick.ask).toBe(67010);
    expect(tick.high24h).toBe(68000);
    expect(tick.low24h).toBe(66000);
    expect(tick.timestamp.toISOString()).toBe("2024-01-01T00:00:00.000Z");
  });

  it("fetchTick parses futures ticker", async () => {
    mockFetch(tickersFixture);

    const tick = await Effect.runPromise(
      Bitget.fetchTick("BTC/USDT:USDT", "futures"),
    );

    expect(tick.exchange).toBe("bitget-futures");
    expect(tick.symbol).toBe("BTC/USDT:USDT");
    expect(tick.price).toBe(67000);
  });

  it("fetchOHLCV parses candles for spot", async () => {
    mockFetch(candlesFixture);

    const candles = await Effect.runPromise(
      Bitget.fetchOHLCV("BTC/USDT", "5m", 2, undefined, "spot"),
    );

    expect(candles).toHaveLength(2);
    expect(candles[0].open).toBe(66000);
    expect(candles[0].high).toBe(68000);
    expect(candles[0].low).toBe(65000);
    expect(candles[0].close).toBe(67000);
    expect(candles[0].volume).toBe(100);
    expect(candles[0].exchange).toBe("bitget");
  });

  it("fetchOHLCV parses candles for futures", async () => {
    mockFetch(candlesFixture);

    const candles = await Effect.runPromise(
      Bitget.fetchOHLCV("BTC/USDT:USDT", "5m", 2, undefined, "futures"),
    );

    expect(candles).toHaveLength(2);
    expect(candles[0].exchange).toBe("bitget-futures");
  });

  it("fetchOrderBook parses depth", async () => {
    mockFetch(orderbookFixture);

    const ob = await Effect.runPromise(
      Bitget.fetchOrderBook("BTC/USDT", 5, "spot"),
    );

    expect(ob.bids).toEqual([
      { price: 66990, volume: 1.5 },
      { price: 66980, volume: 2 },
    ]);
    expect(ob.asks).toEqual([
      { price: 67010, volume: 0.5 },
      { price: 67020, volume: 1 },
    ]);
  });

  it("fetchSymbols filters offline pairs", async () => {
    mockFetch(symbolsFixture);

    const symbols = await Effect.runPromise(Bitget.fetchSymbols("spot"));

    expect(symbols).toEqual(["BTC/USDT"]);
  });

  it("fetch24hrVolumes returns quote-volume map", async () => {
    mockFetch(tickersFixture);

    const volumes = await Effect.runPromise(Bitget.fetch24hrVolumes("spot"));

    expect(volumes["BTCUSDT"]).toBe(78901234);
  });

  it("returns MarketDataError on HTTP failure", async () => {
    mockFetch({ msg: "bad request" }, 400);

    const result = await Effect.runPromise(
      Effect.either(Bitget.fetchTick("BTC/USDT", "spot")),
    );

    expect(result._tag).toBe("Left");
  });

  it("returns MarketDataError on API error code", async () => {
    mockFetch({ code: "40001", msg: "invalid symbol", data: [] }, 200);

    const result = await Effect.runPromise(
      Effect.either(Bitget.fetchTick("BTC/USDT", "spot")),
    );

    expect(result._tag).toBe("Left");
  });

  it("returns MarketDataError when ticker data is empty", async () => {
    mockFetch({ code: "00000", data: [] });

    const result = await Effect.runPromise(
      Effect.either(Bitget.fetchTick("BTC/USDT", "spot")),
    );

    expect(result._tag).toBe("Left");
  });
});
