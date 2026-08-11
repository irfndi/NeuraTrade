import { beforeEach, describe, expect, it } from "bun:test";
import { Effect, Layer } from "effect";
import { makeSimulatedFuturesExchangeAdapterService, checkTpslHit } from "./simulated-futures.js";
import {
  FuturesExchangeAdapter,
  type FuturesExchangeAdapterService,
} from "../futures-adapter.js";
import type { MarketDataGatewayService } from "../../market-data/gateway.js";
import { money } from "../../utils/money.js";

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
  fetchDemoSymbols: () => Effect.die("not used"),
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
          size: money(0.1),
          productType: "USDT-FUTURES",
          marginMode: "crossed",
          leverage: 10,
        });
      }),
    );

    expect(fill.side).toBe("buy");
    expect(fill.filledQty.toNumber()).toBe(0.1);
    expect(fill.filledPrice.greaterThan(0)).toBe(true);

    const position = await run(
      Effect.gen(function* () {
        const adapter = yield* FuturesExchangeAdapter;
        return yield* adapter.getPosition("BTC/USDT:USDT", "USDT-FUTURES");
      }),
    );

    expect(position).not.toBeNull();
    expect(position?.side).toBe("long");
    expect(position?.quantity.toNumber()).toBe(0.1);

    const balance = await run(
      Effect.gen(function* () {
        const adapter = yield* FuturesExchangeAdapter;
        return yield* adapter.getBalance("USDT");
      }),
    );

    expect(balance.available.toNumber()).toBeLessThan(10_000);
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
            size: money(100),
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
          size: money(0.1),
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
          size: money(0.1),
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
    expect(closeBalance.available.toNumber()).toBeGreaterThan(
      openBalance.available.toNumber(),
    );
    expect(closeBalance.available.toNumber()).toBeGreaterThan(9_980);
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
            size: money(0.1),
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
          size: money(0.1),
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
            size: money(size),
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
            size: money(size),
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
      expect(finalBalance.available.toNumber()).toBeLessThanOrEqual(
        initialBalance.available.toNumber(),
      );
      expect(finalBalance.available.toNumber()).toBeGreaterThan(
        initialBalance.available.toNumber() * 0.99,
      );
    }
  });

  it("records TP/SL for an open position", async () => {
    const result = await run(
      Effect.gen(function* () {
        const adapter = yield* FuturesExchangeAdapter;
        yield* adapter.placeOrder({
          symbol: "BTC/USDT:USDT",
          side: "buy",
          type: "market",
          size: money(0.1),
          productType: "USDT-FUTURES",
          marginMode: "crossed",
          leverage: 10,
        });
        yield* (adapter.setTradingStop as NonNullable<typeof adapter.setTradingStop>)({
          symbol: "BTC/USDT:USDT",
          productType: "USDT-FUTURES",
          marginMode: "crossed",
          side: "long",
          takeProfit: money(70000),
          stopLoss: money(60000),
        });
        const svc = adapter as typeof adapter & {
          checkTpslHit: (
            symbol: string,
            productType: string,
            price: import("../../utils/money.js").Decimal,
          ) => Effect.Effect<"tp" | "sl" | null>;
        };
        const tpHit = yield* svc.checkTpslHit("BTC/USDT:USDT", "USDT-FUTURES", money(70001));
        const slHit = yield* svc.checkTpslHit("BTC/USDT:USDT", "USDT-FUTURES", money(59999));
        const noHit = yield* svc.checkTpslHit("BTC/USDT:USDT", "USDT-FUTURES", money(65000));
        return { tpHit, slHit, noHit };
      }),
    );

    expect(result.tpHit).toBe("tp");
    expect(result.slHit).toBe("sl");
    expect(result.noHit).toBeNull();
  });

  it("fails when no position exists", async () => {
    const result = await run(
      Effect.gen(function* () {
        const adapter = yield* FuturesExchangeAdapter;
        return yield* (
          adapter.setTradingStop?.({
            symbol: "BTC/USDT:USDT",
            productType: "USDT-FUTURES",
            marginMode: "crossed",
            side: "long",
            takeProfit: money(70000),
          }) ?? Effect.void
        ).pipe(Effect.result);
      }),
    );

    expect(result._tag).toBe("Failure");
  });

  it("detects TP vs SL hits for long and short positions", () => {
    const longTpsl = {
      side: "long" as const,
      takeProfit: money(70000),
      stopLoss: money(60000),
    };
    expect(checkTpslHit(longTpsl, money(70001))).toBe("tp");
    expect(checkTpslHit(longTpsl, money(59999))).toBe("sl");
    expect(checkTpslHit(longTpsl, money(65000))).toBeNull();

    const shortTpsl = {
      side: "short" as const,
      takeProfit: money(60000),
      stopLoss: money(70000),
    };
    expect(checkTpslHit(shortTpsl, money(59999))).toBe("tp");
    expect(checkTpslHit(shortTpsl, money(70001))).toBe("sl");
    expect(checkTpslHit(shortTpsl, money(65000))).toBeNull();
  });

  it("handles TP-only and SL-only", () => {
    const tpOnly = { side: "long" as const, takeProfit: money(70000) };
    expect(checkTpslHit(tpOnly, money(70001))).toBe("tp");
    expect(checkTpslHit(tpOnly, money(60000))).toBeNull();

    const slOnly = { side: "long" as const, stopLoss: money(60000) };
    expect(checkTpslHit(slOnly, money(59999))).toBe("sl");
    expect(checkTpslHit(slOnly, money(70000))).toBeNull();

    expect(checkTpslHit(undefined, money(70000))).toBeNull();
  });
});
