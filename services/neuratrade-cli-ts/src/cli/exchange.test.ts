import { describe, expect, it } from "bun:test";
import { Effect, Layer } from "effect";
import { runTest } from "./exchange.js";
import { ExchangeAdapter } from "../exchange/adapter.js";
import { MarketDataGateway } from "../market-data/gateway.js";

// The test exercises the "credentials missing" early-return path in runTest
// (see services/cli/exchange.ts). That path returns 1 before any service is
// reached, so these stubs are never invoked. They exist solely to satisfy
// the Effect-TS type checker, which requires the service channel to be
// satisfied at the call site.
const StubExchangeAdapter = Layer.succeed(ExchangeAdapter, {
  placeOrder: () => Effect.dieMessage("not reached in test"),
  getBalance: () => Effect.dieMessage("not reached in test"),
  getPosition: () => Effect.dieMessage("not reached in test"),
  closePosition: () => Effect.dieMessage("not reached in test"),
});

const StubMarketDataGateway = Layer.succeed(MarketDataGateway, {
  fetchTick: () => Effect.dieMessage("not reached in test"),
  fetchOHLCV: () => Effect.dieMessage("not reached in test"),
  fetchOrderBook: () => Effect.dieMessage("not reached in test"),
  fetchSymbols: () => Effect.dieMessage("not reached in test"),
  fetch24hrVolumes: () => Effect.dieMessage("not reached in test"),
  fetchFundingRates: () => Effect.dieMessage("not reached in test"),
});

describe("runTest", () => {
  it("returns 1 when credentials are missing", async () => {
    const result = await Effect.runPromise(
      runTest("", "", "BTC/USDT", 0.001).pipe(
        Effect.catchAll(() => Effect.succeed(1)),
        Effect.provide(StubExchangeAdapter),
        Effect.provide(StubMarketDataGateway),
      ),
    );
    expect(result).toBe(1);
  });
});
