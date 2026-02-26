import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import { BackendApiClient } from "./client";

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
    globalThis.fetch = (async (input: RequestInfo | URL) => {
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
    }) as typeof fetch;

    const client = new BackendApiClient({
      baseUrl: "http://127.0.0.1:58080",
      adminKey: "",
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
