import { describe, expect, it, beforeEach, afterEach } from "bun:test";
import { Effect } from "effect";
import * as Bitget from "./bitget.js";
import candlesFixture from "../../../tests/fixtures/bitget/candles.json";
import orderbookFixture from "../../../tests/fixtures/bitget/orderbook.json";
import tickersFixture from "../../../tests/fixtures/bitget/tickers.json";
import symbolsFixture from "../../../tests/fixtures/bitget/symbols.json";
import fundingRatesFixture from "../../../tests/fixtures/bitget/funding-rates.json";

/** A Bitget funding page envelope returned by the mocks below. */
interface FundingPage {
  code?: string;
  msg?: string;
  data?: unknown;
}
/** Funding pages keyed by page number. */
interface FundingPageMap {
  [key: string]: FundingPage;
}

describe("Bitget gateway", () => {
  let originalFetch: typeof fetch;

  beforeEach(() => {
    originalFetch = globalThis.fetch;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  function mockFetch<T>(response: T, status = 200) {
    globalThis.fetch = (async (
      _input: RequestInfo | URL,
      _init?: RequestInit,
    ) =>
      new Response(JSON.stringify(response), {
        status,
        headers: { "Content-Type": "application/json" },
      })) as typeof fetch;
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

  it("fetchOHLCV uses history-candles for futures (deep backfills)", async () => {
    let requestedUrl = "";
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      requestedUrl = String(input);
      return new Response(JSON.stringify(candlesFixture), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }) as typeof fetch;

    await Effect.runPromise(
      Bitget.fetchOHLCV(
        "BTC/USDT:USDT",
        "5m",
        1000,
        new Date("2025-01-01T00:00:00Z"),
        "futures",
      ),
    );

    expect(requestedUrl).toContain("/api/v2/mix/market/history-candles");
    expect(requestedUrl).toContain("productType=USDT-FUTURES");
    // Bitget caps history-candles at 200 rows/request (error 40053 above).
    expect(requestedUrl).toContain("limit=200");
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

  it("fetchDemoSymbols sends the PAPTRADING demo header", async () => {
    let requestedUrl = "";
    let demoHeader: string | null = null;
    globalThis.fetch = (async (
      input: RequestInfo | URL,
      init?: RequestInit,
    ) => {
      requestedUrl = String(input);
      const headers = new Headers(init?.headers);
      demoHeader = headers.get("PAPTRADING");
      return new Response(JSON.stringify(symbolsFixture), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }) as typeof fetch;

    const symbols = await Effect.runPromise(Bitget.fetchDemoSymbols());

    expect(symbols).toEqual(["BTC/USDT"]);
    expect(requestedUrl).toContain("/api/v2/mix/market/contracts");
    expect(requestedUrl).toContain("productType=USDT-FUTURES");
    expect(demoHeader === "1").toBe(true);
  });

  it("maps spot granularity for fetchOHLCV URLs", async () => {
    let requestedUrl = "";
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      requestedUrl = String(input);
      return new Response(JSON.stringify(candlesFixture), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }) as typeof fetch;

    await Effect.runPromise(
      Bitget.fetchOHLCV("BTC/USDT", "5m", 2, undefined, "spot"),
    );

    expect(requestedUrl).toContain("/api/v2/spot/market/candles");
    expect(requestedUrl).toContain("granularity=5min");
  });

  it("maps futures granularity for fetchOHLCV URLs", async () => {
    let requestedUrl = "";
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      requestedUrl = String(input);
      return new Response(JSON.stringify(candlesFixture), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }) as typeof fetch;

    await Effect.runPromise(
      Bitget.fetchOHLCV("BTC/USDT:USDT", "1h", 2, undefined, "futures"),
    );

    expect(requestedUrl).toContain("/api/v2/mix/market/history-candles");
    expect(requestedUrl).toContain("granularity=1H");
  });

  it("fetchTick rejects zero-price tickers", async () => {
    mockFetch({ code: "00000", msg: "success", data: [{ symbol: "BTCUSDT" }] });

    const result = await Effect.runPromise(
      Bitget.fetchTick("BTC/USDT", "spot").pipe(
        Effect.match({
          onFailure: (err) => ({ ok: false, reason: err.reason }),
          onSuccess: () => ({ ok: true, reason: "" }),
        }),
      ),
    );

    expect(result.ok).toBe(false);
    expect(result.reason).toContain("invalid price");
  });

  it("fetchFundingRates stops paginating once a page predates startTime", async () => {
    const requests: string[] = [];
    const startMs = 1704009600000; // 2023-12-31T08:00:00Z
    const pages: FundingPageMap = {
      "1": {
        code: "00000",
        msg: "success",
        data: [
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
      },
      "2": {
        code: "00000",
        msg: "success",
        data: [
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
      },
    };
    globalThis.fetch = (async (url: string | URL | Request) => {
      const u = String(url);
      requests.push(u);
      const pageNo = new URL(u).searchParams.get("pageNo") ?? "1";
      const body = pages[pageNo] ?? { code: "00000", msg: "success", data: [] };
      return new Response(JSON.stringify(body), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }) as typeof fetch;

    const rates = await Effect.runPromise(
      Bitget.fetchFundingRates("BTC/USDT:USDT", new Date(startMs)),
    );

    // Page 2's oldest row (1703980800000) predates startTime: no page 3.
    expect(requests).toHaveLength(2);
    expect(rates.map((r) => r.fundingRate)).toEqual([0.0001, 0.00009, 0.00008]);
  });

  it("returns MarketDataError on HTTP failure", async () => {
    mockFetch({ msg: "bad request" }, 400);

    const result = await Effect.runPromise(
      Effect.result(Bitget.fetchTick("BTC/USDT", "spot")),
    );

    expect(result._tag).toBe("Failure");
  });

  it("returns MarketDataError on API error code", async () => {
    mockFetch({ code: "40001", msg: "invalid symbol", data: [] }, 200);

    const result = await Effect.runPromise(
      Effect.result(Bitget.fetchTick("BTC/USDT", "spot")),
    );

    expect(result._tag).toBe("Failure");
  });

  it("returns MarketDataError when ticker data is empty", async () => {
    mockFetch({ code: "00000", data: [] });

    const result = await Effect.runPromise(
      Effect.result(Bitget.fetchTick("BTC/USDT", "spot")),
    );

    expect(result._tag).toBe("Failure");
  });

  it("fetchFundingRates maps funding history rows", async () => {
    globalThis.fetch = (async (url: string | URL | Request) => {
      const pageNo = new URL(String(url)).searchParams.get("pageNo") ?? "1";
      const body =
        pageNo === "1"
          ? fundingRatesFixture
          : { code: "00000", msg: "success", data: [] };
      return new Response(JSON.stringify(body), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }) as typeof fetch;

    const rates = await Effect.runPromise(
      Bitget.fetchFundingRates("BTC/USDT:USDT"),
    );

    expect(rates).toHaveLength(2);
    expect(rates[0].exchange).toBe("bitget-futures");
    expect(rates[0].symbol).toBe("BTC/USDT:USDT");
    expect(rates[0].fundingRate).toBe(0.0001);
    expect(rates[0].timestamp.toISOString()).toBe("2024-01-01T00:00:00.000Z");
    expect(rates[1].fundingRate).toBe(-0.00005);
    expect(rates[1].timestamp.toISOString()).toBe("2023-12-31T16:00:00.000Z");
  });

  it("fetchFundingRates returns MarketDataError on API error code", async () => {
    mockFetch({ code: "40001", msg: "invalid symbol", data: null }, 200);

    const result = await Effect.runPromise(
      Effect.result(Bitget.fetchFundingRates("BTC/USDT:USDT")),
    );

    expect(result._tag).toBe("Failure");
  });

  it("fetchFundingRates paginates until an empty page", async () => {
    const requests: string[] = [];
    const pages: FundingPageMap = {
      "1": {
        code: "00000",
        msg: "success",
        data: [
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
      },
      "2": {
        code: "00000",
        msg: "success",
        data: [
          {
            symbol: "BTCUSDT",
            fundingRate: "0.00008",
            fundingTime: "1704009600000",
          },
        ],
      },
      "3": { code: "00000", msg: "success", data: [] },
    };
    globalThis.fetch = (async (url: string | URL | Request) => {
      const u = String(url);
      requests.push(u);
      const pageNo = new URL(u).searchParams.get("pageNo") ?? "1";
      const body = pages[pageNo] ?? { code: "00000", msg: "success", data: [] };
      return new Response(JSON.stringify(body), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }) as typeof fetch;

    const rates = await Effect.runPromise(
      Bitget.fetchFundingRates("BTC/USDT:USDT"),
    );

    expect(rates).toHaveLength(3);
    expect(requests).toHaveLength(3);
    expect(rates[0].fundingRate).toBe(0.0001);
    expect(rates[2].fundingRate).toBe(0.00008);
    for (const u of requests) {
      expect(u).toContain("/api/v2/mix/market/history-fund-rate");
      expect(u).toContain("symbol=BTCUSDT");
      expect(u).toContain("productType=USDT-FUTURES");
    }
  });
});
