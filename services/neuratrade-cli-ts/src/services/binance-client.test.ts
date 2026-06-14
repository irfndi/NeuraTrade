import { describe, expect, it } from "bun:test";
import { Cause, Effect, Layer } from "effect";
import {
  BinanceApiError,
  BinanceClient,
  BinanceClientLive,
  BinanceNetworkError,
  BinanceRateLimitError,
} from "./binance-client.ts";
import { RateLimiterLive } from "./rate-limiter.ts";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function startMock(handler: (req: Request) => Response | Promise<Response>): {
  stop: () => void;
  url: string;
} {
  const server = Bun.serve({ port: 0, fetch: handler });
  return { stop: () => server.stop(), url: `http://localhost:${server.port}` };
}

function json(body: unknown, status = 200): Response {
  return Response.json(body, { status });
}

function provideClient(baseUrl: string) {
  return Layer.provide(
    BinanceClientLive(baseUrl),
    RateLimiterLive({ perSecond: 1000, perMinute: 60_000 }),
  );
}

async function runOk<A>(
  program: Effect.Effect<A, unknown, BinanceClient>,
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
  program: Effect.Effect<A, unknown, BinanceClient>,
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
// Tests
// ---------------------------------------------------------------------------

describe("BinanceClient", () => {
  describe("getExchangeInfo", () => {
    it("returns exchange info symbols", async () => {
      const mock = startMock(() =>
        json({
          symbols: [
            {
              symbol: "BTCUSDT",
              baseAsset: "BTC",
              quoteAsset: "USDT",
              status: "TRADING",
            },
            {
              symbol: "ETHUSDT",
              baseAsset: "ETH",
              quoteAsset: "USDT",
              status: "TRADING",
            },
          ],
        }),
      );
      try {
        const program = Effect.gen(function* () {
          const client = yield* BinanceClient;
          return yield* client.getExchangeInfo();
        });
        const result = await runOk(program, mock.url);
        expect(result.symbols).toHaveLength(2);
        expect(result.symbols[0].symbol).toBe("BTCUSDT");
      } finally {
        mock.stop();
      }
    });
  });

  describe("getKlines", () => {
    it("returns normalized candles", async () => {
      const mock = startMock((req) => {
        const url = new URL(req.url);
        expect(url.pathname).toBe("/api/v3/klines");
        expect(url.searchParams.get("symbol")).toBe("BTCUSDT");
        expect(url.searchParams.get("interval")).toBe("1h");
        return json([
          [
            1704067200000,
            "42000.00",
            "42100.00",
            "41900.00",
            "42050.00",
            "1.5",
            1704070799999,
            "63000.00",
            100,
            "0.75",
            "31500.00",
          ],
        ]);
      });
      try {
        const program = Effect.gen(function* () {
          const client = yield* BinanceClient;
          return yield* client.getKlines({
            symbol: "BTC/USDT",
            interval: "1h",
            startTime: 1704067200000,
            endTime: 1704070799999,
            limit: 1000,
          });
        });
        const result = await runOk(program, mock.url);
        expect(result).toHaveLength(1);
        expect(result[0].timestamp.toISOString()).toBe(
          "2024-01-01T00:00:00.000Z",
        );
        expect(result[0].open).toBe("42000.00");
        expect(result[0].close).toBe("42050.00");
        expect(result[0].volume).toBe("1.5");
      } finally {
        mock.stop();
      }
    });
  });

  describe("error handling", () => {
    it("returns BinanceRateLimitError on 429", async () => {
      const mock = startMock(
        () =>
          new Response("rate limited", {
            status: 429,
            headers: { "Retry-After": "5" },
          }),
      );
      try {
        const program = Effect.gen(function* () {
          const client = yield* BinanceClient;
          return yield* client.getExchangeInfo();
        });
        const err = await runFail(program, mock.url);
        expect(err).toBeInstanceOf(BinanceRateLimitError);
        const rateErr = err as InstanceType<typeof BinanceRateLimitError>;
        expect(rateErr.retryAfterMs).toBe(5000);
      } finally {
        mock.stop();
      }
    });

    it("returns BinanceApiError on non-2xx response", async () => {
      const mock = startMock(() => json({ msg: "Invalid symbol" }, 400));
      try {
        const program = Effect.gen(function* () {
          const client = yield* BinanceClient;
          return yield* client.getExchangeInfo();
        });
        const err = await runFail(program, mock.url);
        expect(err).toBeInstanceOf(BinanceApiError);
        const apiErr = err as InstanceType<typeof BinanceApiError>;
        expect(apiErr.status).toBe(400);
      } finally {
        mock.stop();
      }
    });

    it("returns BinanceNetworkError when connection is refused", async () => {
      const program = Effect.gen(function* () {
        const client = yield* BinanceClient;
        return yield* client.getExchangeInfo();
      });
      const err = await runFail(program, "http://localhost:1");
      expect(err).toBeInstanceOf(BinanceNetworkError);
    });
  });
});
