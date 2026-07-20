import { describe, expect, it } from "bun:test";
import { Effect } from "effect";
import { MarketDataGateway } from "../../market-data/gateway.js";
import { makeBinanceLiveAdapter } from "./binance-live.js";

const dummyGateway = MarketDataGateway.of({
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

describe("BinanceLiveExchangeAdapter", () => {
  it("fails placeOrder without credentials", async () => {
    const adapter = makeBinanceLiveAdapter({ apiKey: "", apiSecret: "" });

    const result = await Effect.runPromise(
      Effect.result(
        adapter.placeOrder({
          symbol: "BTC/USDT",
          side: "buy",
          type: "market",
          quantity: 0.001,
        }),
      ).pipe(Effect.provideService(MarketDataGateway, dummyGateway)),
    );

    expect(result._tag).toBe("Failure");
  });

  it("fails getBalance without credentials", async () => {
    const adapter = makeBinanceLiveAdapter({ apiKey: "", apiSecret: "" });

    const result = await Effect.runPromise(
      Effect.result(adapter.getBalance("USDT")).pipe(
        Effect.provideService(MarketDataGateway, dummyGateway),
      ),
    );

    expect(result._tag).toBe("Failure");
  });
});
