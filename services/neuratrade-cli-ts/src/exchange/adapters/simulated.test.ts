import { describe, expect, it, beforeEach, afterEach } from "bun:test";
import { Effect } from "effect";
import { MarketDataGateway } from "../../market-data/gateway.js";
import { makeSimulatedExchangeAdapter } from "./simulated.js";

describe("SimulatedExchangeAdapter", () => {
  let originalFetch: typeof fetch;

  beforeEach(() => {
    originalFetch = globalThis.fetch;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  function mockOrderBook(price: number) {
    globalThis.fetch = (async () =>
      new Response(
        JSON.stringify({
          lastUpdateId: 1,
          bids: [[String(price * 0.999), "1"]],
          asks: [[String(price * 1.001), "1"]],
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      )) as unknown as typeof fetch;
  }

  it("places a market buy and updates position", async () => {
    mockOrderBook(70_000);

    const adapter = makeSimulatedExchangeAdapter({ USDT: 10_000 });
    const gateway = MarketDataGateway.of({
      fetchTick: () => Effect.fail({ reason: "not implemented" } as never),
      fetchOHLCV: () => Effect.fail({ reason: "not implemented" } as never),
      fetchOrderBook: () =>
        Effect.succeed({
          exchange: "binance",
          symbol: "BTC/USDT",
          bids: [{ price: 69_930, volume: 1 }],
          asks: [{ price: 70_070, volume: 1 }],
          timestamp: new Date(),
        }),
      fetchSymbols: () => Effect.fail({ reason: "not implemented" } as never),
      fetch24hrVolumes: () => Effect.succeed({}),
      fetchFundingRates: () => Effect.succeed([]),
    });

    const fill = await Effect.runPromise(
      adapter
        .placeOrder({
          symbol: "BTC/USDT",
          side: "buy",
          type: "market",
          quantity: 0.1,
        })
        .pipe(Effect.provideService(MarketDataGateway, gateway)),
    );

    expect(fill.symbol).toBe("BTC/USDT");
    expect(fill.side).toBe("buy");
    expect(fill.filledQty).toBe(0.1);
    expect(fill.filledPrice).toBe(70_070);

    const position = await Effect.runPromise(
      adapter
        .getPosition("BTC/USDT")
        .pipe(Effect.provideService(MarketDataGateway, gateway)),
    );
    expect(position).not.toBeNull();
    expect(position?.side).toBe("long");
    expect(position?.quantity).toBe(0.1);
  });

  it("closes a long position with a sell", async () => {
    mockOrderBook(70_000);

    const adapter = makeSimulatedExchangeAdapter({ USDT: 10_000 });
    const gateway = MarketDataGateway.of({
      fetchTick: () => Effect.fail({ reason: "not implemented" } as never),
      fetchOHLCV: () => Effect.fail({ reason: "not implemented" } as never),
      fetchOrderBook: () =>
        Effect.succeed({
          exchange: "binance",
          symbol: "BTC/USDT",
          bids: [{ price: 69_930, volume: 1 }],
          asks: [{ price: 70_070, volume: 1 }],
          timestamp: new Date(),
        }),
      fetchSymbols: () => Effect.fail({ reason: "not implemented" } as never),
      fetch24hrVolumes: () => Effect.succeed({}),
      fetchFundingRates: () => Effect.succeed([]),
    });

    await Effect.runPromise(
      adapter
        .placeOrder({
          symbol: "BTC/USDT",
          side: "buy",
          type: "market",
          quantity: 0.1,
        })
        .pipe(Effect.provideService(MarketDataGateway, gateway)),
    );

    const fill = await Effect.runPromise(
      adapter
        .closePosition("BTC/USDT")
        .pipe(Effect.provideService(MarketDataGateway, gateway)),
    );

    expect(fill).not.toBeNull();
    expect(fill?.side).toBe("sell");

    const position = await Effect.runPromise(
      adapter
        .getPosition("BTC/USDT")
        .pipe(Effect.provideService(MarketDataGateway, gateway)),
    );
    expect(position).toBeNull();
  });
});
