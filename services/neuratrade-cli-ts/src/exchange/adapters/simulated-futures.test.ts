import { beforeEach, describe, expect, it } from "bun:test";
import { Effect, Layer } from "effect";
import {
  makeSimulatedFuturesExchangeAdapterService,
  SimulatedFuturesExchangeAdapterLive,
} from "./simulated-futures.js";
import {
  FuturesExchangeAdapter,
  type FuturesExchangeAdapterService,
} from "../futures-adapter.js";
import { MarketDataGateway } from "../../market-data/gateway.js";
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
          .pipe(Effect.either);
      }),
    );

    expect(result._tag).toBe("Left");
  });

  it("closes a long position with reduce-only", async () => {
    await run(
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
          .pipe(Effect.either);
      }),
    );

    expect(result._tag).toBe("Left");
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
});
