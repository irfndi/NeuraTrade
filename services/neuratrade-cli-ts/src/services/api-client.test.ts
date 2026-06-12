import { describe, expect, it } from "bun:test";
import { Cause, Effect } from "effect";
import {
  ApiClient,
  ApiClientLive,
  HttpError,
  TimeoutError,
  NetworkError,
} from "./api-client.ts";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function startMock(
  handler: (req: Request) => Response | Promise<Response>,
): { stop: () => void; url: string } {
  const server = Bun.serve({ port: 0, fetch: handler });
  return { stop: () => server.stop(), url: `http://localhost:${server.port}` };
}

function json(body: unknown, status = 200): Response {
  return Response.json(body, { status });
}

async function runOk<A>(
  program: Effect.Effect<A, unknown, ApiClient>,
  url: string,
  apiKey = "test-api-key",
  timeoutMs?: number,
): Promise<A> {
  return Effect.runPromise(
    program.pipe(Effect.provide(ApiClientLive(url, apiKey, timeoutMs))) as Effect.Effect<A, never>,
  );
}

async function runFail<A>(
  program: Effect.Effect<A, unknown, ApiClient>,
  url: string,
  apiKey = "test-api-key",
  timeoutMs?: number,
): Promise<unknown> {
  const exit = await Effect.runPromiseExit(
    program.pipe(Effect.provide(ApiClientLive(url, apiKey, timeoutMs))) as Effect.Effect<A, unknown>,
  );
  if (exit._tag === "Failure") {
    return Cause.squash(exit.cause);
  }
  throw new Error("Expected failure but got success");
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("ApiClient", () => {
  // -----------------------------------------------------------------------
  // health
  // -----------------------------------------------------------------------

  describe("health", () => {
    it("returns health status on 200", async () => {
      const mock = startMock(() =>
        json({ status: "healthy", timestamp: "2025-01-01T00:00:00Z" }),
      );
      try {
        const program = Effect.gen(function* () {
          const api = yield* ApiClient;
          return yield* api.health();
        });
        const result = await runOk(program, mock.url);
        expect(result.status).toBe("healthy");
        expect(result.timestamp).toBe("2025-01-01T00:00:00Z");
      } finally {
        mock.stop();
      }
    });
  });

  // -----------------------------------------------------------------------
  // generateAuthCode
  // -----------------------------------------------------------------------

  describe("generateAuthCode", () => {
    it("posts to generate-binding-code and returns response", async () => {
      let receivedBody: unknown;
      const mock = startMock(async (req) => {
        receivedBody = await req.json();
        return json({
          success: true,
          message: "Code generated",
          user_id: "u123",
          expires_at: "2025-01-02T00:00:00Z",
        });
      });
      try {
        const program = Effect.gen(function* () {
          const api = yield* ApiClient;
          return yield* api.generateAuthCode("u123");
        });
        const result = await runOk(program, mock.url);
        expect(result.success).toBe(true);
        expect(result.user_id).toBe("u123");
        expect(result.message).toBe("Code generated");
        expect(receivedBody).toEqual({ user_id: "u123" });
      } finally {
        mock.stop();
      }
    });
  });

  // -----------------------------------------------------------------------
  // getAIProviders
  // -----------------------------------------------------------------------

  describe("getAIProviders", () => {
    it("fetches AI providers list", async () => {
      const mock = startMock(() =>
        json({
          providers: [
            { id: "deepseek", name: "DeepSeek", is_active: true, model_count: 3 },
            { id: "openai", name: "OpenAI", is_active: false, model_count: 5 },
          ],
        }),
      );
      try {
        const program = Effect.gen(function* () {
          const api = yield* ApiClient;
          return yield* api.getAIProviders();
        });
        const result = await runOk(program, mock.url);
        expect(result.providers).toHaveLength(2);
        expect(result.providers[0].id).toBe("deepseek");
        expect(result.providers[1].is_active).toBe(false);
      } finally {
        mock.stop();
      }
    });
  });

  // -----------------------------------------------------------------------
  // getAIModels
  // -----------------------------------------------------------------------

  describe("getAIModels", () => {
    it("fetches all models when no provider specified", async () => {
      let receivedPath: string | undefined;
      const mock = startMock((req) => {
        receivedPath = new URL(req.url).pathname;
        return json({
          models: [
            { model_id: "gpt-4o", display_name: "GPT-4o", provider: "openai", cost: "0.01", supports_tools: true, supports_vision: false },
          ],
        });
      });
      try {
        const program = Effect.gen(function* () {
          const api = yield* ApiClient;
          return yield* api.getAIModels();
        });
        const result = await runOk(program, mock.url);
        expect(result.models).toHaveLength(1);
        expect(result.models[0].model_id).toBe("gpt-4o");
        expect(receivedPath).toBe("/api/v1/ai/models");
      } finally {
        mock.stop();
      }
    });

    it("fetches provider-specific models", async () => {
      let receivedPath: string | undefined;
      const mock = startMock((req) => {
        receivedPath = new URL(req.url).pathname;
        return json({ models: [] });
      });
      try {
        const program = Effect.gen(function* () {
          const api = yield* ApiClient;
          return yield* api.getAIModels("deepseek");
        });
        await runOk(program, mock.url);
        expect(receivedPath).toBe("/api/v1/ai/providers/deepseek/models");
      } finally {
        mock.stop();
      }
    });
  });

  // -----------------------------------------------------------------------
  // getPortfolio
  // -----------------------------------------------------------------------

  describe("getPortfolio", () => {
    it("fetches portfolio data", async () => {
      const mock = startMock(() =>
        json({
          total_value: "15000.00",
          cash: "5000.00",
          assets: [{ symbol: "BTC", amount: "0.1", value: "10000.00" }],
          pnl_24h: "250.00",
        }),
      );
      try {
        const program = Effect.gen(function* () {
          const api = yield* ApiClient;
          return yield* api.getPortfolio();
        });
        const result = await runOk(program, mock.url);
        expect(result.total_value).toBe("15000.00");
        expect(result.assets).toHaveLength(1);
        expect(result.assets[0].symbol).toBe("BTC");
      } finally {
        mock.stop();
      }
    });
  });

  // -----------------------------------------------------------------------
  // getBalance
  // -----------------------------------------------------------------------

  describe("getBalance", () => {
    it("fetches balance with chat_id query param", async () => {
      let receivedUrl: string | undefined;
      const mock = startMock((req) => {
        receivedUrl = req.url;
        return json({ total_equity: "8000.00", available_balance: "6000.00" });
      });
      try {
        const program = Effect.gen(function* () {
          const api = yield* ApiClient;
          return yield* api.getBalance("chat-42");
        });
        const result = await runOk(program, mock.url);
        expect(result.total_balance).toBe("8000.00");
        expect(result.available).toBe("6000.00");
        expect(result.locked).toBe("0");
        expect(result.currency).toBe("USDT");
        expect(receivedUrl).toContain("chat_id=chat-42");
      } finally {
        mock.stop();
      }
    });
  });

  // -----------------------------------------------------------------------
  // runScalpingBacktest
  // -----------------------------------------------------------------------

  describe("runScalpingBacktest", () => {
    it("posts backtest request and returns result", async () => {
      let receivedBody: unknown;
      const mock = startMock(async (req) => {
        receivedBody = await req.json();
        return json({
          run_id: "run-001",
          status: "completed",
          mode: "deterministic",
          summary: { total_trades: 42, win_rate: 0.65 },
          gate_summary: [],
        });
      });
      try {
        const program = Effect.gen(function* () {
          const api = yield* ApiClient;
          return yield* api.runScalpingBacktest({
            start_time: "2025-01-01T00:00:00Z",
            end_time: "2025-06-01T00:00:00Z",
            symbols: ["BTC/USDT"],
            mode: "deterministic",
          });
        });
        const result = await runOk(program, mock.url);
        expect(result.run_id).toBe("run-001");
        expect(result.status).toBe("completed");
        expect(result.summary.total_trades).toBe(42);
        expect(receivedBody).toEqual({
          start_time: "2025-01-01T00:00:00Z",
          end_time: "2025-06-01T00:00:00Z",
          symbols: ["BTC/USDT"],
          mode: "deterministic",
        });
      } finally {
        mock.stop();
      }
    });
  });

  // -----------------------------------------------------------------------
  // Error handling
  // -----------------------------------------------------------------------

  describe("error handling", () => {
    it("returns HttpError on non-2xx response", async () => {
      const mock = startMock(() => json({ error: "unauthorized" }, 401));
      try {
        const program = Effect.gen(function* () {
          const api = yield* ApiClient;
          return yield* api.health();
        });
        const err = await runFail(program, mock.url);
        expect(err).toBeInstanceOf(HttpError);
        const httpErr = err as InstanceType<typeof HttpError>;
        expect(httpErr.status).toBe(401);
        expect(httpErr.body).toContain("unauthorized");
        expect(httpErr.endpoint).toBe("/health");
      } finally {
        mock.stop();
      }
    });

    it("returns HttpError on 500 with body text", async () => {
      const mock = startMock(() => new Response("internal server error", { status: 500 }));
      try {
        const program = Effect.gen(function* () {
          const api = yield* ApiClient;
          return yield* api.getPortfolio();
        });
        const err = await runFail(program, mock.url);
        expect(err).toBeInstanceOf(HttpError);
        const httpErr = err as InstanceType<typeof HttpError>;
        expect(httpErr.status).toBe(500);
        expect(httpErr.body).toBe("internal server error");
      } finally {
        mock.stop();
      }
    });

    it("returns TimeoutError when request exceeds timeout", async () => {
      const mock = startMock(
        () => new Promise(() => {}), // never resolves → timeout
      );
      try {
        const program = Effect.gen(function* () {
          const api = yield* ApiClient;
          return yield* api.health();
        });
        // Use 50ms timeout for fast test
        const err = await runFail(program, mock.url, "test-key", 50);
        expect(err).toBeInstanceOf(TimeoutError);
        const timeoutErr = err as TimeoutError;
        expect(timeoutErr.endpoint).toBe("/health");
        expect(timeoutErr.timeoutMs).toBe(50);
      } finally {
        mock.stop();
      }
    });

    it("returns NetworkError when connection is refused", async () => {
      const deadUrl = "http://localhost:1";
      const program = Effect.gen(function* () {
        const api = yield* ApiClient;
        return yield* api.health();
      });
      const err = await runFail(program, deadUrl);
      expect(err).toBeInstanceOf(NetworkError);
      const netErr = err as InstanceType<typeof NetworkError>;
      expect(netErr.endpoint).toBe("/health");
      expect(netErr.cause).toBeDefined();
    });
  });

  // -----------------------------------------------------------------------
  // Headers
  // -----------------------------------------------------------------------

  describe("headers", () => {
    it("injects X-API-Key header when apiKey is provided", async () => {
      let receivedHeaders: Headers | undefined;
      const mock = startMock((req) => {
        receivedHeaders = req.headers;
        return json({ status: "ok" });
      });
      try {
        const program = Effect.gen(function* () {
          const api = yield* ApiClient;
          return yield* api.health();
        });
        await runOk(program, mock.url, "my-secret-key");
        expect(receivedHeaders?.get("x-api-key")).toBe("my-secret-key");
        expect(receivedHeaders?.get("content-type")).toBe("application/json");
      } finally {
        mock.stop();
      }
    });

    it("omits X-API-Key when apiKey is empty", async () => {
      let receivedHeaders: Headers | undefined;
      const mock = startMock((req) => {
        receivedHeaders = req.headers;
        return json({ status: "ok" });
      });
      try {
        const program = Effect.gen(function* () {
          const api = yield* ApiClient;
          return yield* api.health();
        });
        await runOk(program, mock.url, "");
        expect(receivedHeaders?.get("x-api-key")).toBeNull();
      } finally {
        mock.stop();
      }
    });
  });
});
