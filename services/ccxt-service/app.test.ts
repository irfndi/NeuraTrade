import "./test-setup";
import { test, expect } from "bun:test";

// Set required environment variables for testing
process.env.ADMIN_API_KEY =
  "test-admin-key-that-is-at-least-32-characters-long-for-security";
process.env.PORT = "3003";

// Cache the service instance to avoid re-initialization
let serviceInstance: {
  port: number | string;
  fetch: (req: Request) => Promise<Response>;
} | null = null;
let initializationPromise: Promise<any> | null = null;

async function getService() {
  if (!serviceInstance) {
    if (!initializationPromise) {
      initializationPromise = (async () => {
        // Force fresh import by adding timestamp to avoid module caching issues
        const mod = await import("./index.ts?" + Date.now());
        // Use named export 'app' instead of default to avoid Bun auto-serve
        serviceInstance = mod.app as {
          port: number | string;
          fetch: (req: Request) => Promise<Response>;
        };

        // Wait for service to be ready using readiness probe
        const timeout = 10000; // 10 seconds
        const interval = 200; // 200ms
        const startTime = Date.now();

        while (Date.now() - startTime < timeout) {
          try {
            const healthRes = await serviceInstance.fetch(
              new Request("http://localhost/health"),
            );
            if (healthRes.status === 200) {
              return serviceInstance;
            }
            // eslint-disable-next-line no-unused-vars
          } catch (error) {
            // Service not ready yet, continue polling
          }
          await new Promise((resolve) => setTimeout(resolve, interval));
        }

        throw new Error(`Service failed to become ready within ${timeout}ms`);
      })();
    }
    await initializationPromise;
  }
  return serviceInstance!;
}

test("health endpoint returns healthy", async () => {
  const svc = await getService();
  const res = await svc.fetch(new Request("http://localhost/health"));
  expect(res.status).toBe(200);
  const body = await res.json();
  expect(body.status).toBe("healthy");
  expect(body.service).toBe("ccxt-service");
}, 10000);

test("not found returns 404 with message", async () => {
  const svc = await getService();
  const res = await svc.fetch(new Request("http://localhost/nope"));
  expect(res.status).toBe(404);
  const body = await res.json();
  expect(body.error).toBe("Not Found");
});

test("/api/exchanges returns mocked exchange list", async () => {
  const svc = await getService();
  const res = await svc.fetch(new Request("http://localhost/api/exchanges"));
  expect(res.status).toBe(200);
  const body = await res.json();
  expect(Array.isArray(body.exchanges)).toBe(true);
  const hasBinance = body.exchanges.some(
    (ex: any) => (typeof ex === "string" ? ex : ex.id) === "binance",
  );
  expect(hasBinance).toBe(true);
});

test("/api/markets/:exchange returns symbols", async () => {
  const svc = await getService();
  const res = await svc.fetch(
    new Request("http://localhost/api/markets/binance"),
  );
  expect(res.status).toBe(200);
  const body = await res.json();
  expect(body.exchange).toBe("binance");
  expect(body.count).toBeGreaterThan(0);
  expect(body.symbols).toContain("BTC/USDT");
}, 10000);

test("/api/ticker/:exchange/:symbol returns ticker data", async () => {
  const svc = await getService();
  const res = await svc.fetch(
    new Request("http://localhost/api/ticker/binance/BTC/USDT"),
  );
  expect(res.status).toBe(200);
  const body = await res.json();
  expect(body.exchange).toBe("binance");
  expect(body.symbol).toBe("BTC/USDT");
  expect(body.ticker.last).toBeDefined();
});

test("/api/ticker with unsupported exchange returns 400", async () => {
  const svc = await getService();
  const res = await svc.fetch(
    new Request("http://localhost/api/ticker/unknown/BTC/USDT"),
  );
  expect(res.status).toBe(400);
  const body = await res.json();
  expect(body.error).toBe("Exchange not supported");
});

test("/api/funding-rates returns array", async () => {
  const svc = await getService();
  const res = await svc.fetch(
    new Request("http://localhost/api/funding-rates/binance"),
  );
  expect(res.status).toBe(200);
  const body = await res.json();
  expect(Array.isArray(body.fundingRates)).toBe(true);
  expect(body.count).toBeGreaterThan(0);
});

test("/api/funding-rates with symbols filter", async () => {
  const svc = await getService();
  const res = await svc.fetch(
    new Request("http://localhost/api/funding-rates/binance?symbols=BTC/USDT"),
  );
  expect(res.status).toBe(200);
  const body = await res.json();
  expect(body.count).toBe(1);
  expect(body.fundingRates[0].symbol).toBe("BTC/USDT");
});

test("health endpoint includes exchanges_count", async () => {
  const svc = await getService();
  const res = await svc.fetch(new Request("http://localhost/health"));
  expect(res.status).toBe(200);
  const body = await res.json();
  expect(body.exchanges_count).toBeGreaterThan(0);
  expect(body.exchange_connectivity).toBe("configured");
});

test("/api/ticker with invalid symbol returns 404", async () => {
  const svc = await getService();
  const res = await svc.fetch(
    new Request("http://localhost/api/ticker/binance/INVALID/INVALID"),
  );
  // Should return 404 for symbol not found, not 500
  expect(res.status).toBe(404);
  const body = await res.json();
  expect(body.error).toBeDefined();
});

test("/api/markets with unsupported exchange returns 400", async () => {
  const svc = await getService();
  const res = await svc.fetch(
    new Request("http://localhost/api/markets/unknown_exchange"),
  );
  expect(res.status).toBe(400);
  const body = await res.json();
  expect(body.error).toBe("Exchange not supported");
});
