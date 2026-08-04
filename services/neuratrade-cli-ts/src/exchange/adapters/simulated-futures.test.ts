import { beforeEach, describe, expect, it } from "bun:test";
import { Effect, Layer } from "effect";
import { makeSimulatedFuturesExchangeAdapterService } from "./simulated-futures.js";
import {
  FuturesExchangeAdapter,
  type FuturesExchangeAdapterService,
} from "../futures-adapter.js";
import type { MarketDataGatewayService } from "../../market-data/gateway.js";

const mockGateway: MarketDataGatewayService = {
  fetchTick: () => Effect.die("not used"),
  fetchOHLCV: () => Effect.die("not used"),
  fetchOrderBook: () =>
    Effect.succeed({
      exchange: "bitget-futures",
      symbol: "BTC/USDT:USDT",
      bids: [{ price: 66990, volume: 1 }],
      asks: [{ price: 67010, volume: 1 }],
      timestamp: new Date(),
    }),
  fetchSymbols: () => Effect.die("not used"),
  fetch24hrVolumes: () => Effect.die("not used"),
  fetchFundingRates: () => Effect.succeed([]),
};

let sharedAdapter: ReturnType<
  typeof makeSimulatedFuturesExchangeAdapterService
> extends Effect.Effect<infer A, never, never>
  ? A
  : never;
let testLayer: Layer.Layer<FuturesExchangeAdapterService>;

beforeEach(() => {
  sharedAdapter = Effect.runSync(
    makeSimulatedFuturesExchangeAdapterService(mockGateway, { USDT: 10_000 }),
  );
  testLayer = Layer.succeed(FuturesExchangeAdapter, sharedAdapter);
});

function run<T>(effect: Effect.Effect<T, unknown, unknown>): Promise<T> {
  return Effect.runPromise(
    effect.pipe(Effect.provide(testLayer)) as Effect.Effect<T, unknown>,
  );
}

describe("SimulatedFuturesExchangeAdapter", () => {
  it("opens a long position", async () => {
    const fill = await run(
      Effect.gen(function* () {
        const adapter = yield* FuturesExchangeAdapter;
        return yield* adapter.placeOrder({
          symbol: "BTC/USDT:USDT",
          side: "buy",
          type: "market",
          size: 0.1,
          productType: "USDT-FUTURES",
          marginMode: "crossed",
          leverage: 10,
        });
      }),
    );

    expect(fill.side).toBe("buy");
    expect(fill.filledQty).toBe(0.1);
    expect(fill.filledPrice).toBeGreaterThan(0);

    const position = await run(
      Effect.gen(function* () {
        const adapter = yield* FuturesExchangeAdapter;
        return yield* adapter.getPosition("BTC/USDT:USDT", "USDT-FUTURES");
      }),
    );

    expect(position).not.toBeNull();
    expect(position?.side).toBe("long");
    expect(position?.quantity).toBe(0.1);

    const balance = await run(
      Effect.gen(function* () {
        const adapter = yield* FuturesExchangeAdapter;
        return yield* adapter.getBalance("USDT");
      }),
    );

    expect(balance.available).toBeLessThan(10_000);
  });

  it("rejects order with insufficient margin", async () => {
    const result = await run(
      Effect.gen(function* () {
        const adapter = yield* FuturesExchangeAdapter;
        return yield* adapter
          .placeOrder({
            symbol: "BTC/USDT:USDT",
            side: "buy",
            type: "market",
            size: 100,
            productType: "USDT-FUTURES",
            marginMode: "crossed",
            leverage: 1,
          })
          .pipe(Effect.result);
      }),
    );

    expect(result._tag).toBe("Failure");
  });

  it("closes a long position with reduce-only", async () => {
    const openBalance = await run(
      Effect.gen(function* () {
        const adapter = yield* FuturesExchangeAdapter;
        yield* adapter.placeOrder({
          symbol: "BTC/USDT:USDT",
          side: "buy",
          type: "market",
          size: 0.1,
          productType: "USDT-FUTURES",
          marginMode: "crossed",
          leverage: 10,
        });
        return yield* adapter.getBalance("USDT");
      }),
    );

    await run(
      Effect.gen(function* () {
        const adapter = yield* FuturesExchangeAdapter;
        return yield* adapter.closePosition({
          symbol: "BTC/USDT:USDT",
          side: "sell",
          productType: "USDT-FUTURES",
          marginMode: "crossed",
          leverage: 10,
          size: 0.1,
        });
      }),
    );

    const position = await run(
      Effect.gen(function* () {
        const adapter = yield* FuturesExchangeAdapter;
        return yield* adapter.getPosition("BTC/USDT:USDT", "USDT-FUTURES");
      }),
    );

    expect(position).toBeNull();

    const closeBalance = await run(
      Effect.gen(function* () {
        const adapter = yield* FuturesExchangeAdapter;
        return yield* adapter.getBalance("USDT");
      }),
    );

    // Margin is released and PnL is realized. The close balance recovers the
    // locked margin plus PnL (minus fees), so it is higher than the post-open
    // balance but still below the initial 10,000 due to fees and slippage.
    expect(closeBalance.available).toBeGreaterThan(openBalance.available);
    expect(closeBalance.available).toBeGreaterThan(9_980);
  });

  it("rejects reduce-only buy without a short position", async () => {
    const result = await run(
      Effect.gen(function* () {
        const adapter = yield* FuturesExchangeAdapter;
        return yield* adapter
          .placeOrder({
            symbol: "BTC/USDT:USDT",
            side: "buy",
            type: "market",
            size: 0.1,
            productType: "USDT-FUTURES",
            marginMode: "crossed",
            leverage: 10,
            reduceOnly: true,
          })
          .pipe(Effect.result);
      }),
    );

    expect(result._tag).toBe("Failure");
  });

  it("opens a short position", async () => {
    const fill = await run(
      Effect.gen(function* () {
        const adapter = yield* FuturesExchangeAdapter;
        return yield* adapter.placeOrder({
          symbol: "BTC/USDT:USDT",
          side: "sell",
          type: "market",
          size: 0.1,
          productType: "USDT-FUTURES",
          marginMode: "crossed",
          leverage: 10,
        });
      }),
    );

    expect(fill.side).toBe("sell");

    const position = await run(
      Effect.gen(function* () {
        const adapter = yield* FuturesExchangeAdapter;
        return yield* adapter.getPosition("BTC/USDT:USDT", "USDT-FUTURES");
      }),
    );

    expect(position?.side).toBe("short");
  });

  it("releases margin on reduce-only close for random sizes", async () => {
    for (let i = 0; i < 20; i++) {
      const size = 0.01 + Math.random() * 0.09;
      const leverage = 1 + Math.floor(Math.random() * 20);
      const adapter = Effect.runSync(
        makeSimulatedFuturesExchangeAdapterService(mockGateway, {
          USDT: 10_000,
        }),
      );
      const freshLayer = Layer.succeed(FuturesExchangeAdapter, adapter);
      const runFresh = <T>(effect: Effect.Effect<T, unknown, unknown>) =>
        Effect.runPromise(
          effect.pipe(Effect.provide(freshLayer)) as Effect.Effect<T, unknown>,
        );

      const initialBalance = await runFresh(
        Effect.gen(function* () {
          const a = yield* FuturesExchangeAdapter;
          return yield* a.getBalance("USDT");
        }),
      );

      await runFresh(
        Effect.gen(function* () {
          const a = yield* FuturesExchangeAdapter;
          yield* a.placeOrder({
            symbol: "BTC/USDT:USDT",
            side: "buy",
            type: "market",
            size,
            productType: "USDT-FUTURES",
            marginMode: "crossed",
            leverage,
          });
          return yield* a.closePosition({
            symbol: "BTC/USDT:USDT",
            side: "sell",
            productType: "USDT-FUTURES",
            marginMode: "crossed",
            leverage,
            size,
          });
        }),
      );

      const finalBalance = await runFresh(
        Effect.gen(function* () {
          const a = yield* FuturesExchangeAdapter;
          return yield* a.getBalance("USDT");
        }),
      );

      const position = await runFresh(
        Effect.gen(function* () {
          const a = yield* FuturesExchangeAdapter;
          return yield* a.getPosition("BTC/USDT:USDT", "USDT-FUTURES");
        }),
      );

      expect(position).toBeNull();
      // Margin released, only fees (both sides) reduce balance.
      expect(finalBalance.available).toBeLessThanOrEqual(
        initialBalance.available,
      );
      expect(finalBalance.available).toBeGreaterThan(
        initialBalance.available * 0.99,
      );
    }
  });
});
