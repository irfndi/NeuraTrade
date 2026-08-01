import { afterEach, describe, expect, it } from "bun:test";
import { Effect } from "effect";
import { money } from "../../utils/money.js";
import { FuturesExchangeAdapter } from "../futures-adapter.js";
import { BackendRiskGatedFuturesExchangeAdapterLive } from "./backend-risk-gated-futures.js";

const originalFetch = globalThis.fetch;

afterEach(() => {
  globalThis.fetch = originalFetch;
});

describe("BackendRiskGatedFuturesExchangeAdapter", () => {
  it("fails closed when backend credentials are absent", async () => {
    const adapterLayer = BackendRiskGatedFuturesExchangeAdapterLive({
      baseUrl: "http://localhost:8080",
      apiKey: "",
      chatId: "",
    });
    const result = await Effect.runPromise(
      Effect.gen(function* () {
        const adapter = yield* FuturesExchangeAdapter;
        return yield* adapter
          .placeOrder({
            symbol: "BTC/USDT:USDT",
            side: "buy",
            type: "market",
            size: money("0.00012345678901234567890"),
            price: money("70000.12345678901234567890"),
            productType: "USDT-FUTURES",
            marginMode: "crossed",
            leverage: 5,
          })
          .pipe(Effect.result);
      }).pipe(Effect.provide(adapterLayer)),
    );

    expect(result._tag).toBe("Failure");
  });

  it("sends exact decimal order values to the backend gate", async () => {
    let requestBody: Record<string, string | boolean> | undefined;
    Object.defineProperty(globalThis, "fetch", {
      configurable: true,
      value: async (_input: RequestInfo | URL, init?: RequestInit) => {
        requestBody = JSON.parse(String(init?.body)) as Record<
          string,
          string | boolean
        >;
        return new Response(
          JSON.stringify({
            intent_id: "intent-1",
            order_id: "exchange-1",
            client_id: "client-1",
            exchange: "bitget-futures",
            symbol: "BTC/USDT:USDT",
            side: "buy",
            filled_qty: "0.00012345678901234567890",
            filled_price: "70000.12345678901234567890",
            fee: "0.00000000000000000001",
            status: "filled",
            timestamp: "2026-08-01T00:00:00.000Z",
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        );
      },
    });

    const adapterLayer = BackendRiskGatedFuturesExchangeAdapterLive({
      baseUrl: "http://localhost:8080",
      apiKey: "admin-key",
      chatId: "chat-1",
    });
    const fill = await Effect.runPromise(
      Effect.gen(function* () {
        const adapter = yield* FuturesExchangeAdapter;
        return yield* adapter.placeOrder({
          symbol: "BTC/USDT:USDT",
          side: "buy",
          type: "market",
          size: money("0.00012345678901234567890"),
          price: money("70000.12345678901234567890"),
          productType: "USDT-FUTURES",
          marginMode: "crossed",
          leverage: 5,
          clientOid: "intent-1",
        });
      }).pipe(Effect.provide(adapterLayer)),
    );

    expect(requestBody?.size).toBe("0.0001234567890123456789");
    expect(requestBody?.price).toBe("70000.1234567890123456789");
    expect(requestBody?.intent_id).toBe("intent-1");
    expect(fill.filledQty.toString()).toBe("0.0001234567890123456789");
    expect(fill.fee.toString()).toBe("1e-20");
  });

  it("fails closed when the exchange has not confirmed a fill", async () => {
    Object.defineProperty(globalThis, "fetch", {
      configurable: true,
      value: async () =>
        new Response(
          JSON.stringify({
            intent_id: "intent-open",
            order_id: "exchange-open",
            client_id: "client-open",
            exchange: "bitget-futures",
            symbol: "BTC/USDT:USDT",
            side: "buy",
            filled_qty: "0",
            filled_price: "0",
            fee: "0",
            status: "open",
            timestamp: "2026-08-01T00:00:00.000Z",
          }),
          { status: 202, headers: { "Content-Type": "application/json" } },
        ),
    });

    const adapterLayer = BackendRiskGatedFuturesExchangeAdapterLive({
      baseUrl: "http://localhost:8080",
      apiKey: "admin-key",
      chatId: "chat-1",
    });
    const result = await Effect.runPromise(
      Effect.gen(function* () {
        const adapter = yield* FuturesExchangeAdapter;
        return yield* adapter
          .placeOrder({
            symbol: "BTC/USDT:USDT",
            side: "buy",
            type: "market",
            size: money("0.1"),
            price: money("70000"),
            productType: "USDT-FUTURES",
            marginMode: "crossed",
            leverage: 5,
          })
          .pipe(Effect.result);
      }).pipe(Effect.provide(adapterLayer)),
    );

    expect(result._tag).toBe("Failure");
  });
});
