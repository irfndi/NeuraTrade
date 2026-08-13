/**
 * Co-located contract tests for the money-critical pieces of the Bitget
 * client: HMAC signing + demo routing (PAPTRADING), the fail-closed
 * unsupported-instrument classifier, symbol normalization, and the strict
 * response parsers (missing money fields must fail, never default).
 *
 * Kept separate from bitget-client.test.ts on purpose: this file only
 * exercises exported pure functions and the live layer through a mock server,
 * so it never depends on the other file's helpers.
 */
import { describe, expect, it } from "bun:test";
import { Cause, Effect, Layer } from "effect";
import {
  BitgetApiError,
  BitgetClient,
  BitgetClientLive,
  authHeaders,
  fromBitgetSymbol,
  isBitgetUnsupportedInstrumentError,
  toBitgetFuturesSymbol,
  toBitgetSymbol,
} from "./bitget-client.ts";
import { RateLimiterLive } from "./rate-limiter.ts";

// ---------------------------------------------------------------------------
// Helpers
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
  apiKey: "test-key",
  apiSecret: "test-secret",
  passphrase: "test-pass",
};

function provideClient(baseUrl: string) {
  return Layer.provide(
    BitgetClientLive({ credentials: testCreds, baseUrl }),
    RateLimiterLive({ perSecond: 1000, perMinute: 60_000 }),
  );
}

async function runOk<A>(
  program: Effect.Effect<A, unknown, BitgetClient>,
  baseUrl: string,
): Promise<A> {
  return Effect.runPromise(
    program.pipe(Effect.provide(provideClient(baseUrl))) as Effect.Effect<
      A,
      never
    >,
  );
}

async function runFail<A>(
  program: Effect.Effect<A, unknown, BitgetClient>,
  baseUrl: string,
): Promise<unknown> {
  const exit = await Effect.runPromiseExit(
    program.pipe(Effect.provide(provideClient(baseUrl))) as Effect.Effect<
      A,
      unknown
    >,
  );
  if (exit._tag === "Failure") {
    return Cause.squash(exit.cause);
  }
  throw new Error("Expected failure but got success");
}

// ---------------------------------------------------------------------------
// authHeaders: signature vector + demo routing
// ---------------------------------------------------------------------------

describe("authHeaders", () => {
  const FIXED_TIMESTAMP = "1710000000000";
  const endpoint = "/api/v2/spot/account/assets";

  it("produces the documented HMAC-SHA256 base64 signature", () => {
    const headers = authHeaders(
      testCreds,
      "GET",
      endpoint,
      "",
      false,
      FIXED_TIMESTAMP,
    );
    expect(headers["ACCESS-KEY"]).toBe("test-key");
    expect(headers["ACCESS-TIMESTAMP"]).toBe(FIXED_TIMESTAMP);
    // Known vector: HMAC-SHA256("test-secret",
    // "1710000000000GET/api/v2/spot/account/assets") in base64.
    expect(headers["ACCESS-SIGN"]).toBe(
      "7+eqqU/kweP2pBiQsqaRSoteA4zWzUW3FyDckfOEtrQ=",
    );
    expect(headers["ACCESS-PASSPHRASE"]).toBe("test-pass");
  });

  it("sends PAPTRADING exactly when sandbox mode is enabled", () => {
    const demo = authHeaders(testCreds, "GET", endpoint, "", true);
    expect(demo["PAPTRADING"]).toBe("1");

    const live = authHeaders(testCreds, "GET", endpoint, "", false);
    expect(live["PAPTRADING"]).toBeUndefined();
  });
});

// ---------------------------------------------------------------------------
// isBitgetUnsupportedInstrumentError: fail-closed classifier
// ---------------------------------------------------------------------------

describe("isBitgetUnsupportedInstrumentError", () => {
  const apiError = (code: string, body: string) =>
    new BitgetApiError({
      status: 400,
      body,
      endpoint: "/single-position",
      code,
    });

  it.each([
    // bare generic demo-proxy message -> unsupported instrument
    ["Parameter does not exist", true],
    // named symbol/contract/instrument parameters -> unsupported instrument
    ["Parameter symbol does not exist", true],
    ["Parameter symbol not exist", true],
    ["No such parameter instrument", true],
    // named non-symbol parameter -> config defect, NOT unsupported
    ["Parameter marginCoin does not exist", false],
    ["Parameter clientType does not exist", false],
    ["No such parameter leverage", false],
    // wrong business code -> never unsupported
  ])("classifies body %j as unsupported=%s", (body, expected) => {
    expect(isBitgetUnsupportedInstrumentError(apiError("40034", body))).toBe(
      expected,
    );
  });

  it("is never true for a non-40034 code", () => {
    expect(
      isBitgetUnsupportedInstrumentError(
        apiError("40001", "Parameter symbol does not exist"),
      ),
    ).toBe(false);
    expect(
      isBitgetUnsupportedInstrumentError(
        apiError("40034", "Insufficient balance"),
      ),
    ).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// Symbol normalizers
// ---------------------------------------------------------------------------

describe("symbol normalization", () => {
  it("toBitgetSymbol strips the slash and uppercases", () => {
    expect(toBitgetSymbol("BTC/USDT")).toBe("BTCUSDT");
    expect(toBitgetSymbol("eth/usdt")).toBe("ETHUSDT");
  });

  it("toBitgetFuturesSymbol maps CCXT-style symbols and edge inputs", () => {
    expect(toBitgetFuturesSymbol("BTC/USDT:USDT")).toEqual({
      symbol: "BTCUSDT",
      productType: "USDT-FUTURES",
    });
    expect(toBitgetFuturesSymbol("BTC/USDT")).toEqual({
      symbol: "BTCUSDT",
      productType: "USDT-FUTURES",
    });
    expect(toBitgetFuturesSymbol("BTC/USDC:USDC")).toEqual({
      symbol: "BTCUSDC",
      productType: "USDC-FUTURES",
    });
    // Only :USDC / :USD / _DMCBL hints steer the product type; a plain
    // symbol or an unhandled quote hint keeps the caller's default
    // (COIN-FUTURES is always passed explicitly by callers, e.g. marginCoin
    // resolution in the position query).
    expect(toBitgetFuturesSymbol("BTC/USD:BTC")).toEqual({
      symbol: "BTCUSD",
      productType: "USDT-FUTURES",
    });
    // _DMCBL suffix marks a coin-margined delivery contract.
    expect(toBitgetFuturesSymbol("BTCUSD_DMCBL")).toEqual({
      symbol: "BTCUSD_DMCBL",
      productType: "COIN-FUTURES",
    });
    // Ambiguous plain symbol defaults to the requested product type.
    expect(toBitgetFuturesSymbol("BTCUSD", "COIN-FUTURES")).toEqual({
      symbol: "BTCUSD",
      productType: "COIN-FUTURES",
    });
  });

  it("fromBitgetSymbol restores the slash form", () => {
    expect(fromBitgetSymbol("BTCUSDT")).toBe("BTC/USDT");
    expect(fromBitgetSymbol("BTC/USDT")).toBe("BTC/USDT");
    expect(fromBitgetSymbol("BTCUSD")).toBe("BTC/USD");
    expect(fromBitgetSymbol("BTCUSD_DMCBL")).toBe("BTCUSD_DMCBL");
  });
});

// ---------------------------------------------------------------------------
// Strict response parsing: missing money fields fail instead of defaulting
// ---------------------------------------------------------------------------

describe("strict response parsing", () => {
  it("fails getOrder when side/status/size/price are absent", async () => {
    const mock = startMock(() =>
      json({
        code: "00000",
        data: { orderId: "12345", clientOid: "c1", symbol: "BTCUSDT" },
      }),
    );
    try {
      const program = Effect.gen(function* () {
        const client = yield* BitgetClient;
        return yield* client.getOrder({ symbol: "BTC/USDT", orderId: "12345" });
      });
      const err = await runFail(program, mock.url);
      expect(err).toBeInstanceOf(BitgetApiError);
      expect((err as BitgetApiError).code).toBe("PARSE_CONTRACT");
      expect((err as BitgetApiError).body).toContain("side");
    } finally {
      mock.stop();
    }
  });

  it("fails getFuturesPositions when holdSide is missing", async () => {
    const mock = startMock(() =>
      json({
        code: "00000",
        data: [
          {
            positionId: "pos-1",
            symbol: "BTCUSDT",
            productType: "USDT-FUTURES",
            marginMode: "crossed",
            openPrice: "60000",
            total: "0.01",
            available: "0.01",
            leverage: "10",
          },
        ],
      }),
    );
    try {
      const program = Effect.gen(function* () {
        const client = yield* BitgetClient;
        return yield* client.getFuturesPositions(
          "BTC/USDT:USDT",
          "USDT-FUTURES",
        );
      });
      const err = await runFail(program, mock.url);
      expect(err).toBeInstanceOf(BitgetApiError);
      expect((err as BitgetApiError).code).toBe("PARSE_CONTRACT");
      expect((err as BitgetApiError).body).toContain("holdSide");
    } finally {
      mock.stop();
    }
  });

  it("fails getBalances when available is missing on an item", async () => {
    const mock = startMock(() =>
      json({
        code: "00000",
        data: [{ coin: "BTC", frozen: "0.1" }],
      }),
    );
    try {
      const program = Effect.gen(function* () {
        const client = yield* BitgetClient;
        return yield* client.getBalances();
      });
      const err = await runFail(program, mock.url);
      expect(err).toBeInstanceOf(BitgetApiError);
      expect((err as BitgetApiError).code).toBe("PARSE_CONTRACT");
      expect((err as BitgetApiError).body).toContain("available");
    } finally {
      mock.stop();
    }
  });

  it("accepts a sparse place-order acknowledgement with orderId", async () => {
    const mock = startMock(() =>
      json({ code: "00000", data: { orderId: "ack-1", clientOid: "" } }),
    );
    try {
      const program = Effect.gen(function* () {
        const client = yield* BitgetClient;
        return yield* client.placeOrder({
          symbol: "BTC/USDT",
          side: "buy",
          orderType: "market",
          size: "0.001",
        });
      });
      const ack = await runOk(program, mock.url);
      expect(ack.orderId).toBe("ack-1");
      expect(ack.clientOid).toBe("");
    } finally {
      mock.stop();
    }
  });

  it("fails a place-order ack that carries neither orderId nor clientOid", async () => {
    const mock = startMock(() =>
      json({ code: "00000", data: { symbol: "BTCUSDT" } }),
    );
    try {
      const program = Effect.gen(function* () {
        const client = yield* BitgetClient;
        return yield* client.placeOrder({
          symbol: "BTC/USDT",
          side: "buy",
          orderType: "market",
          size: "0.001",
        });
      });
      const err = await runFail(program, mock.url);
      expect(err).toBeInstanceOf(BitgetApiError);
      expect((err as BitgetApiError).code).toBe("PARSE_CONTRACT");
    } finally {
      mock.stop();
    }
  });
});
