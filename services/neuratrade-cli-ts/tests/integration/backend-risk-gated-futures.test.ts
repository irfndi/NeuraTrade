import { describe, expect, it } from "bun:test";
import { Effect } from "effect";
import { FuturesExchangeAdapter } from "../../src/exchange/futures-adapter.js";
import { BackendRiskGatedFuturesExchangeAdapterLive } from "../../src/exchange/adapters/backend-risk-gated-futures.js";
import { money } from "../../src/utils/money.js";
import liveOrderFilledFixture from "../fixtures/backend/live-order-filled.json";
import livePositionsFixture from "../fixtures/backend/live-positions.json";

interface RequestLog {
  readonly method: string;
  readonly path: string;
  readonly apiKey: string | null;
}

function startBackendFixture(orderResponse: unknown = liveOrderFilledFixture): {
  readonly server: Bun.Server<undefined>;
  readonly requests: RequestLog[];
} {
  const requests: RequestLog[] = [];
  const server = Bun.serve({
    port: 0,
    fetch: (request) => {
      const url = new URL(request.url);
      requests.push({
        method: request.method,
        path: `${url.pathname}${url.search}`,
        apiKey: request.headers.get("X-API-Key"),
      });
      if (
        request.method === "GET" &&
        url.pathname === "/api/v1/execution/futures/positions"
      ) {
        return new Response(JSON.stringify(livePositionsFixture), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      if (
        request.method === "POST" &&
        url.pathname === "/api/v1/execution/futures/order"
      ) {
        return new Response(JSON.stringify(orderResponse), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      return new Response("not found", { status: 404 });
    },
  });
  return { server, requests };
}

describe("backend-gated futures HTTP integration", () => {
  it("reads an active position through a real HTTP server", async () => {
    const fixture = startBackendFixture();
    try {
      const layer = BackendRiskGatedFuturesExchangeAdapterLive({
        baseUrl: fixture.server.url.toString(),
        apiKey: "integration-admin-key",
        chatId: "integration-chat",
        timeoutMs: 2_000,
      });
      const position = await Effect.runPromise(
        Effect.gen(function* () {
          const adapter = yield* FuturesExchangeAdapter;
          return yield* adapter.getPosition("BTC/USDT:USDT", "USDT-FUTURES");
        }).pipe(Effect.provide(layer)),
      );

      expect(position?.side).toBe("long");
      expect(position?.quantity.toString()).toBe("0.1");
      expect(fixture.requests).toEqual([
        {
          method: "GET",
          path: "/api/v1/execution/futures/positions?exchange=bitget-futures&product_type=USDT-FUTURES",
          apiKey: "integration-admin-key",
        },
      ]);
    } finally {
      fixture.server.stop(true);
    }
  });

  it("places a filled order through a real HTTP server", async () => {
    const fixture = startBackendFixture();
    try {
      const layer = BackendRiskGatedFuturesExchangeAdapterLive({
        baseUrl: fixture.server.url.toString(),
        apiKey: "integration-admin-key",
        chatId: "integration-chat",
        timeoutMs: 2_000,
      });
      const fill = await Effect.runPromise(
        Effect.gen(function* () {
          const adapter = yield* FuturesExchangeAdapter;
          return yield* adapter.placeOrder({
            symbol: "BTC/USDT:USDT",
            side: "buy",
            type: "market",
            size: money("0.01"),
            price: money("70000"),
            productType: "USDT-FUTURES",
            marginMode: "crossed",
            leverage: 1,
            clientOid: "intent-1",
          });
        }).pipe(Effect.provide(layer)),
      );

      expect(fill.orderId).toBe("exchange-1");
      expect(fill.filledQty.toString()).toBe("0.0001234567890123456789");
      expect(fixture.requests[0]?.method).toBe("POST");
      expect(fixture.requests[0]?.path).toBe("/api/v1/execution/futures/order");
      expect(fixture.requests[0]?.apiKey).toBe("integration-admin-key");
    } finally {
      fixture.server.stop(true);
    }
  });

  it("fails closed when a real HTTP server returns a different intent", async () => {
    const fixture = startBackendFixture({
      ...liveOrderFilledFixture,
      intent_id: "different-intent",
    });
    try {
      const layer = BackendRiskGatedFuturesExchangeAdapterLive({
        baseUrl: fixture.server.url.toString(),
        apiKey: "integration-admin-key",
        chatId: "integration-chat",
        timeoutMs: 2_000,
      });
      const result = await Effect.runPromise(
        Effect.gen(function* () {
          const adapter = yield* FuturesExchangeAdapter;
          return yield* adapter
            .placeOrder({
              symbol: "BTC/USDT:USDT",
              side: "buy",
              type: "market",
              size: money("0.01"),
              price: money("70000"),
              productType: "USDT-FUTURES",
              marginMode: "crossed",
              leverage: 1,
              clientOid: "intent-1",
            })
            .pipe(Effect.result);
        }).pipe(Effect.provide(layer)),
      );

      expect(result._tag).toBe("Failure");
      expect(fixture.requests).toHaveLength(1);
    } finally {
      fixture.server.stop(true);
    }
  });
});
