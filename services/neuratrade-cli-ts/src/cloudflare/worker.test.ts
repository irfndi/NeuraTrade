import { describe, expect, it } from "bun:test";
import worker from "./worker.ts";

// ---------------------------------------------------------------------------
// Universe-watch fetch handler routing. The /scan path hits live Bitget, so
// it is excluded; the admin-gated + KV-backed routes are covered with a fake
// env.
// ---------------------------------------------------------------------------

function fakeEnv(initial: Record<string, string> = {}) {
  const store = new Map(Object.entries(initial));
  return {
    watchlist: {
      get: async (k: string) => store.get(k) ?? null,
      put: async (k: string, v: string) => {
        store.set(k, v);
      },
    },
    adminKey: "test-admin-key",
  };
}

const req = (url: string, init?: RequestInit) =>
  worker.fetch(
    new Request(`https://universe-watch.example${url}`, init),
    fakeEnv() as never,
  );

describe("universe-watch fetch handler", () => {
  it("serves /health unauthenticated", async () => {
    const res = await req("/health");
    expect(res.status).toBe(200);
    expect(((await res.json()) as { status: string }).status).toBe("healthy");
  });

  it("returns empty watchlist before any scan", async () => {
    const res = await req("/watchlist");
    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({
      survivors: [],
      note: "no scan run yet",
    });
  });

  it("rejects mutating routes without the admin key", async () => {
    const res = await req("/scan", { method: "POST" });
    expect(res.status).toBe(401);
  });

  it("rejects malformed JSON on /seed with a controlled 400", async () => {
    const res = await req("/seed", {
      method: "PUT",
      headers: {
        "x-api-key": "test-admin-key",
        "content-type": "application/json",
      },
      body: "{not json",
    });
    expect(res.status).toBe(400);
    expect(await res.json()).toEqual({ error: "Invalid JSON body" });
  });

  it("rejects empty symbol strings on /seed", async () => {
    const res = await req("/seed", {
      method: "PUT",
      headers: {
        "x-api-key": "test-admin-key",
        "content-type": "application/json",
      },
      body: JSON.stringify(["BTC/USDT", ""]),
    });
    expect(res.status).toBe(400);
  });

  it("stores a valid seed and reads it back", async () => {
    const env = fakeEnv() as never;
    const put = await worker.fetch(
      new Request("https://universe-watch.example/seed", {
        method: "PUT",
        headers: {
          "x-api-key": "test-admin-key",
          "content-type": "application/json",
        },
        body: JSON.stringify(["BTC/USDT", "ETH/USDT"]),
      }),
      env,
    );
    expect(put.status).toBe(200);
    const store = env as unknown as {
      watchlist: { get: (k: string) => Promise<string | null> };
    };
    expect(await store.watchlist.get("seed-symbols")).toBe(
      '["BTC/USDT","ETH/USDT"]',
    );
  });
});
