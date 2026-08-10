import { describe, expect, it } from "bun:test";
import { Effect } from "effect";
import { MarketDataGateway } from "../gateway.js";
import { MarketDataGatewayLive } from "./index.js";

describe("MarketDataGatewayLive", () => {
  it("fetchFundingRates for bitget spot fails instead of falling through to Binance", async () => {
    const result = await Effect.runPromise(
      Effect.gen(function* () {
        const gateway = yield* MarketDataGateway;
        return yield* gateway.fetchFundingRates("bitget", "BTC/USDT").pipe(
          Effect.match({
            onFailure: (err) => ({ ok: false, reason: err.reason }),
            onSuccess: () => ({ ok: true, reason: "" }),
          }),
        );
      }).pipe(Effect.provide(MarketDataGatewayLive)),
    );

    expect(result.ok).toBe(false);
    expect(result.reason).toContain("bitget spot");
  });
});
