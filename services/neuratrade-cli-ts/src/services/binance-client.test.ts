import { describe, expect, it } from "bun:test";
import { Cause, Effect, Layer } from "effect";
import {
  BinanceApiError,
  BinanceClient,
  BinanceClientLive,
  BinanceNetworkError,
  BinanceRateLimitError,
  fromBinanceSymbol,
  toBinanceSymbol,
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

describe("symbol normalization", () => {
  describe("toBinanceSymbol", () => {
    it("strips slash and uppercases BTC/USDT", () => {
      expect(toBinanceSymbol("BTC/USDT")).toBe("BTCUSDT");
    });

    it("uppercases already-joined symbols", () => {
      expect(toBinanceSymbol("ethusdt")).toBe("ETHUSDT");
    });

    it("preserves already-uppercase slashed symbols", () => {
      expect(toBinanceSymbol("SOL/USDT")).toBe("SOLUSDT");
    });
  });

  describe("fromBinanceSymbol", () => {
    it("returns already-slashed symbols unchanged (uppercased)", () => {
      expect(fromBinanceSymbol("BTC/USDT")).toBe("BTC/USDT");
      expect(fromBinanceSymbol("eth/usdt")).toBe("ETH/USDT");
    });

    it("splits USDT-quoted symbols", () => {
      expect(fromBinanceSymbol("BTCUSDT")).toBe("BTC/USDT");
      expect(fromBinanceSymbol("ETHUSDT")).toBe("ETH/USDT");
      expect(fromBinanceSymbol("SOLUSDT")).toBe("SOL/USDT");
    });

    it("splits USDC-quoted symbols", () => {
      expect(fromBinanceSymbol("BTCUSDC")).toBe("BTC/USDC");
    });

    it("splits BTC-quoted symbols (e.g., ETHBTC)", () => {
      expect(fromBinanceSymbol("ETHBTC")).toBe("ETH/BTC");
    });

    it("splits ETH-quoted symbols", () => {
      expect(fromBinanceSymbol("FOOETH")).toBe("FOO/ETH");
    });

    it("splits BNB-quoted symbols", () => {
      expect(fromBinanceSymbol("FOOBNB")).toBe("FOO/BNB");
    });

    it("splits FDUSD-quoted symbols", () => {
      expect(fromBinanceSymbol("BTCFDUSD")).toBe("BTC/FDUSD");
    });

    it("splits TUSD-quoted symbols", () => {
      expect(fromBinanceSymbol("BTCTUSD")).toBe("BTC/TUSD");
    });

    it("splits BUSD-quoted symbols", () => {
      expect(fromBinanceSymbol("BTCBUSD")).toBe("BTC/BUSD");
    });

    it("splits TRY-quoted symbols", () => {
      expect(fromBinanceSymbol("BTCTRY")).toBe("BTC/TRY");
    });

    it("splits EUR-quoted symbols", () => {
      expect(fromBinanceSymbol("BTCEUR")).toBe("BTC/EUR");
    });

    it("splits GBP-quoted symbols", () => {
      expect(fromBinanceSymbol("BTCGBP")).toBe("BTC/GBP");
    });

    it("splits JPY-quoted symbols", () => {
      expect(fromBinanceSymbol("BTCJPY")).toBe("BTC/JPY");
    });

    it("splits AUD-quoted symbols", () => {
      expect(fromBinanceSymbol("BTCAUD")).toBe("BTC/AUD");
    });

    it("splits PAX-quoted symbols", () => {
      expect(fromBinanceSymbol("BTCPAX")).toBe("BTC/PAX");
    });

    it("returns the symbol unchanged when no quote asset matches", () => {
      expect(fromBinanceSymbol("UNKNOWN")).toBe("UNKNOWN");
    });

    it("lowercases input then uppercases for matching", () => {
      expect(fromBinanceSymbol("ethusdt")).toBe("ETH/USDT");
      expect(fromBinanceSymbol("btcusdc")).toBe("BTC/USDC");
    });
  });
});

describe("response parsing", () => {
  it("returns BinanceApiError when response body is not valid JSON", async () => {
    const mock = startMock(() => new Response("not json {{{", { status: 200 }));
    try {
      const program = Effect.gen(function* () {
        const client = yield* BinanceClient;
        return yield* client.getExchangeInfo();
      });
      const err = await runFail(program, mock.url);
      expect(err).toBeInstanceOf(BinanceApiError);
      const apiErr = err as InstanceType<typeof BinanceApiError>;
      expect(apiErr.status).toBe(200);
      expect(typeof apiErr.body).toBe("string");
      expect(apiErr.body.length).toBeGreaterThan(0);
    } finally {
      mock.stop();
    }
  });
});
