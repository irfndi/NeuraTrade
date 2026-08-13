/**
 * Acceptance-flow test for driving bd issue clever-cabin-va5 to its criterion:
 * place and query a scalpel-sized market order (testnet-equivalent via a mocked
 * REST endpoint) with pre-trade guards running before any network call.
 *
 * This ties together the pieces that are tested individually elsewhere:
 *   - `validateOrder` (src/services/bitget-guards.ts) rejects a rule-violating
 *     order *before* any request is sent;
 *   - a valid scalpel-sized order is normalized by the guards then placed and
 *     its status queried through the mocked `BitgetClient` REST surface.
 */
import { describe, expect, it } from "bun:test";
import { Cause, Effect, Layer } from "effect";
import {
  BitgetClient,
  BitgetClientLive,
  type BitgetInstrument,
} from "./bitget-client.ts";
import { RateLimiterLive } from "./rate-limiter.ts";
import { BitgetGuardError, validateOrder } from "./bitget-guards.ts";

// ---------------------------------------------------------------------------
// Mock REST server helpers (same harness as bitget-client.test.ts)
// ---------------------------------------------------------------------------

type JsonPrimitive = string | number | boolean | null;
type JsonValue =
  | JsonPrimitive
  | { readonly [key: string]: JsonValue }
  | readonly JsonValue[];

/** A running ephemeral HTTP mock server. */
interface MockServer {
  stop: () => void;
  url: string;
}

function startMock(
  handler: (req: Request) => Response | Promise<Response>,
): MockServer {
  const server = Bun.serve({ port: 0, fetch: handler });
  return { stop: () => server.stop(), url: `http://localhost:${server.port}` };
}

function json(body: JsonValue, status = 200): Response {
  return Response.json(body, { status });
}

const testCreds = {
  apiKey: "acceptance-key",
  apiSecret: "acceptance-secret",
  passphrase: "acceptance-pass",
};

function provideClient(baseUrl: string) {
  return Layer.provide(
    BitgetClientLive({ credentials: testCreds, baseUrl }),
    RateLimiterLive({ perSecond: 1000, perMinute: 60_000 }),
  );
}

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// A scalpel-sized BTC/USDT instrument with realistic precision.
const btcInstrument: BitgetInstrument = {
  symbol: "BTCUSDT",
  baseCoin: "BTC",
  quoteCoin: "USDT",
  status: "online",
  minTradeAmount: "0.0001",
  maxTradeAmount: "1000000",
  minTradeUSDT: "5",
  takerFeeRate: "0.001",
  makerFeeRate: "0.001",
  pricePrecision: "1",
  quantityPrecision: "4",
  quotePrecision: "2",
};

const balances = [
  { asset: "BTC", available: "0.5", frozen: "0" },
  // Enough USDT for a small market order, but too little for a whale order.
  { asset: "USDT", available: "100", frozen: "0" },
];

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("clever-cabin-va5 acceptance flow: guards -> place -> query", () => {
  it("rejects a rule-violating order before any network call is made", async () => {
    // A tiny order below the 5 USDT minimum notional.
    const underNotional = {
      symbol: "BTC/USDT",
      side: "buy" as const,
      orderType: "market" as const,
      size: "0.5", // 0.5 USDT quoted, below minTradeUSDT=5
    };
    const exit = await Effect.runPromiseExit(
      Effect.gen(function* () {
        return yield* validateOrder(
          { order: underNotional, instrument: btcInstrument, balances },
          "60000",
        );
      }),
    );
    expect(exit._tag).toBe("Failure");
    if (exit._tag !== "Failure") {
      throw new Error("expected guard failure");
    }
    const err = Cause.squash(exit.cause) as BitgetGuardError;
    expect(err).toBeInstanceOf(BitgetGuardError);
    expect(err.reason).toContain("below min trade amount");
  });

  it("rejects an order the account balance cannot cover (before network call)", async () => {
    const whaleBuy = {
      symbol: "BTC/USDT",
      side: "buy" as const,
      orderType: "limit" as const,
      size: "1", // 1 BTC
      price: "60000", // 60000 USDT gross, far above the 100 USDT available
    };
    const exit = await Effect.runPromiseExit(
      Effect.gen(function* () {
        return yield* validateOrder(
          { order: whaleBuy, instrument: btcInstrument, balances },
          "60000",
        );
      }),
    );
    expect(exit._tag).toBe("Failure");
    if (exit._tag !== "Failure") {
      throw new Error("expected guard failure");
    }
    const err = Cause.squash(exit.cause) as BitgetGuardError;
    expect(err).toBeInstanceOf(BitgetGuardError);
    expect(err.reason).toMatch(/insufficient USDT balance/i);
  });

  it("places and queries a scalpel-sized market order with guards applied", async () => {
    const placed = { orderId: "accept-order-1", clientOid: "scalpel-001" };

    const mock = startMock(async (req) => {
      const url = new URL(req.url);
      if (url.pathname === "/api/v2/spot/trade/place-order") {
        expect(req.method).toBe("POST");
        // Signed-request auth headers must be present on every protected call.
        expect(req.headers.get("ACCESS-KEY")).toBe(testCreds.apiKey);
        expect(req.headers.get("ACCESS-PASSPHRASE")).toBe(testCreds.passphrase);
        expect(req.headers.get("ACCESS-TIMESTAMP")).toBeTruthy();
        expect(req.headers.get("ACCESS-SIGN")).toBeTruthy();
        const body = JSON.parse(await req.text());
        return json({ code: "00000", data: { ...placed, ...body } });
      }
      if (url.pathname === "/api/v2/spot/trade/orderInfo") {
        return json({
          code: "00000",
          data: {
            orderId: placed.orderId,
            clientOid: placed.clientOid,
            symbol: "BTCUSDT",
            side: "sell",
            orderType: "market",
            status: "filled",
            size: "0.0012",
            price: "60000",
            accBaseVolume: "0.0012",
            accQuoteVolume: "72",
            fee: "0",
          },
        });
      }
      return json({ code: "00000", data: {} });
    });

    try {
      const program = Effect.gen(function* () {
        const client = yield* BitgetClient;

        // 1. Guard normalizes the scalpel-sized order: a market sell of 0.00125
        //    BTC rounds down to 0.0012 at quantityPrecision 4, and its notional
        //    (0.0012 * 60000 = 72 USDT) clears the 5 USDT minimum while the BTC
        //    balance (0.5) covers the base size.
        const normalized = yield* validateOrder(
          {
            order: {
              symbol: "BTC/USDT",
              side: "sell",
              orderType: "market",
              size: "0.00125",
              clientOid: "scalpel-001",
            },
            instrument: btcInstrument,
            balances,
          },
          "60000",
        );
        expect(normalized.size).toBe("0.0012");

        // 2. Place it through the mocked REST surface.
        const placedOrder = yield* client.placeOrder(normalized);

        // 3. Query its status through the mocked REST surface.
        const status = yield* client.getOrder({
          symbol: normalized.symbol,
          clientOid: placedOrder.clientOid,
        });

        return { normalized, placedOrder, status };
      });

      const result = await Effect.runPromise(
        Effect.provide(program, provideClient(mock.url)),
      );
      // place-order is an acknowledgement: it carries the identifiers, while
      // the authoritative status comes from getOrder.
      expect(result.placedOrder.orderId).toBe("accept-order-1");
      expect(result.placedOrder.clientOid).toBe("scalpel-001");
      expect(result.status.status).toBe("filled");
      expect(result.status.side).toBe("sell");
      expect(result.status.orderType).toBe("market");
      expect(result.status.filledSize).toBe("0.0012");
    } finally {
      mock.stop();
    }
  });
});
