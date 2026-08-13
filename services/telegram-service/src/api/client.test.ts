import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import { Effect, Cause } from "effect";
import {
  BackendApiClient,
  TelegramApi,
  TelegramApiLive,
  ApiClientError,
} from "./client";

const makeTestAdminKey = (): string =>
  `test-admin-key-${Math.random().toString(36).slice(2, 12)}`;

type FetchImplementation = (
  input: RequestInfo | URL,
  init?: RequestInit,
) => Promise<Response>;

/** Replace the global fetch with a stub that returns a Response or throws. */
function stubFetch(impl: FetchImplementation): void {
  globalThis.fetch = impl as typeof fetch;
}

describe("BackendApiClient fallback behavior", () => {
  const originalFetch = globalThis.fetch;

  beforeEach(() => {
    globalThis.fetch = originalFetch;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  test("falls back to local backend URL when primary base URL is unreachable", async () => {
    const urls: string[] = [];
    stubFetch(async (input: RequestInfo | URL) => {
      const url = String(input);
      urls.push(url);
      if (url.startsWith("http://127.0.0.1:58080")) {
        throw new Error("connect refused");
      }
      return new Response(
        JSON.stringify({
          status: "healthy",
          checked_at: "2026-02-26T00:00:00Z",
          summary: "ok",
          checks: [],
        }),
        {
          status: 200,
          headers: { "Content-Type": "application/json" },
        },
      );
    });

    const client = new BackendApiClient({
      baseUrl: "http://127.0.0.1:58080",
      adminKey: makeTestAdminKey(),
      rateLimit: 1000,
    });

    await client.getDoctor("1082762347");
    await client.getDoctor("1082762347");

    expect(urls[0]).toContain("http://127.0.0.1:58080");
    expect(urls[1]).toContain("http://127.0.0.1:8080");
    // After one successful fallback, subsequent requests should stick to fallback URL.
    expect(urls[2]).toContain("http://127.0.0.1:8080");
  });
});

// PR-5: TelegramApi is the Effect Context.Tag wrapper around
// BackendApiClient. Each method returns Effect<A, ApiClientError> so
// handlers in src/commands/* can be rewritten as Effect.gen programs.
// These tests verify the Layer composition + happy/error paths.
describe("TelegramApi Effect service", () => {
  const originalFetch = globalThis.fetch;

  beforeEach(() => {
    globalThis.fetch = originalFetch;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  test("TelegramApiLive composes with the underlying client", async () => {
    let capturedUrl = "";
    stubFetch(async (input: RequestInfo | URL) => {
      capturedUrl = String(input);
      return new Response(
        JSON.stringify({
          user: {
            id: "u-1",
            subscription_tier: "free",
            created_at: "2026-01-01T00:00:00Z",
          },
        }),
        {
          status: 200,
          headers: { "Content-Type": "application/json" },
        },
      );
    });

    const backend = new BackendApiClient({
      baseUrl: "http://example.test",
      adminKey: makeTestAdminKey(),
      rateLimit: 1000,
    });
    const layer = TelegramApiLive(backend);
    const program = Effect.gen(function* () {
      const api = yield* TelegramApi;
      return yield* api.getUserByChatId("chat-1");
    });
    const result = await Effect.runPromise(Effect.provide(program, layer));
    expect(result).toEqual({
      user: {
        id: "u-1",
        subscription_tier: "free",
        created_at: "2026-01-01T00:00:00Z",
      },
    });
    expect(capturedUrl).toContain("/internal/telegram/users/chat-1");
  });

  test("TelegramApi methods surface ApiClientError on HTTP failure", async () => {
    stubFetch(
      async () =>
        new Response(JSON.stringify({ message: "boom" }), {
          status: 500,
          headers: { "Content-Type": "application/json" },
        }),
    );

    const backend = new BackendApiClient({
      baseUrl: "http://example.test",
      adminKey: makeTestAdminKey(),
      rateLimit: 1000,
    });
    const layer = TelegramApiLive(backend);
    const program = Effect.gen(function* () {
      const api = yield* TelegramApi;
      return yield* api.getUserByChatId("chat-1");
    });
    // Effect wraps thrown errors in a FiberFailure at the runPromise
    // boundary, so we use the typed exit channel via
    // Effect.runPromiseExit + Cause.failures to recover the typed
    // error.
    const exit = await Effect.runPromiseExit(Effect.provide(program, layer));
    expect(exit._tag).toBe("Failure");
    if (exit._tag === "Failure") {
      const failureOption = Cause.findErrorOption(exit.cause);
      expect(failureOption._tag).toBe("Some");
      if (failureOption._tag === "Some") {
        const failure = failureOption.value as ApiClientError;
        expect(failure).toBeInstanceOf(ApiClientError);
        expect(failure.status).toBe(500);
        expect(failure.message).toBe("boom");
      }
    }
  });

  test("TelegramApi methods wrap non-ApiClientError throws as ApiClientError(status=0)", async () => {
    // Defensive coverage: the wrapper catches non-ApiClientError throws
    // and re-wraps them so the typed error channel is preserved even
    // when the underlying client raises a generic Error.
    stubFetch(async () => {
      throw new Error("network blew up");
    });

    const backend = new BackendApiClient({
      baseUrl: "http://example.test",
      adminKey: makeTestAdminKey(),
      rateLimit: 1000,
    });
    const layer = TelegramApiLive(backend);
    const program = Effect.gen(function* () {
      const api = yield* TelegramApi;
      return yield* api.getUserByChatId("chat-1");
    });
    const exit = await Effect.runPromiseExit(Effect.provide(program, layer));
    expect(exit._tag).toBe("Failure");
    if (exit._tag === "Failure") {
      const failureOption = Cause.findErrorOption(exit.cause);
      expect(failureOption._tag).toBe("Some");
      if (failureOption._tag === "Some") {
        const failure = failureOption.value as ApiClientError;
        expect(failure).toBeInstanceOf(ApiClientError);
        expect(failure.status).toBe(0);
        expect(failure.message).toBe("network blew up");
      }
    }
  });
});

describe("BackendApiClient admin guard", () => {
  const originalFetch = globalThis.fetch;

  beforeEach(() => {
    globalThis.fetch = originalFetch;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  test("fail-fast when requireAdmin is true but adminKey is empty", async () => {
    let fetchCalled = false;
    stubFetch(async () => {
      fetchCalled = true;
      return new Response(JSON.stringify({}), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    });

    const client = new BackendApiClient({
      baseUrl: "http://example.test",
      adminKey: "",
      rateLimit: 1000,
    });

    await expect(client.getDoctor("chat-1")).rejects.toThrow(
      "ADMIN_API_KEY is not configured",
    );
    expect(fetchCalled).toBe(false);
  });

  test("TelegramApiLive propagates admin guard error as ApiClientError", async () => {
    const backend = new BackendApiClient({
      baseUrl: "http://example.test",
      adminKey: "",
      rateLimit: 1000,
    });
    const layer = TelegramApiLive(backend);
    const program = Effect.gen(function* () {
      const api = yield* TelegramApi;
      return yield* api.getDoctor("chat-1");
    });
    const exit = await Effect.runPromiseExit(Effect.provide(program, layer));
    expect(exit._tag).toBe("Failure");
    if (exit._tag === "Failure") {
      const failureOption = Cause.findErrorOption(exit.cause);
      expect(failureOption._tag).toBe("Some");
      if (failureOption._tag === "Some") {
        const failure = failureOption.value as ApiClientError;
        expect(failure).toBeInstanceOf(ApiClientError);
        expect(failure.status).toBe(0);
        expect(failure.message).toContain("ADMIN_API_KEY is not configured");
      }
    }
  });
});
