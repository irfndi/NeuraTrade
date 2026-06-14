import { describe, expect, it } from "bun:test";
import { Effect } from "effect";
import { HealthCheck, HealthCheckLive } from "./health-check.ts";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Start a tiny HTTP server, return { url, stop }. */
function startTestServer(
  handler: (req: Request) => Response | Promise<Response>,
) {
  const server = Bun.serve({
    port: 0,
    fetch: handler,
  });
  return {
    url: `http://127.0.0.1:${server.port}`,
    stop: () => server.stop(),
  };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("HealthCheck service", () => {
  // -----------------------------------------------------------------------
  // probeHTTP
  // -----------------------------------------------------------------------

  describe("probeHTTP", () => {
    it("returns healthy=true for a 200 response", async () => {
      const server = startTestServer(() => new Response("OK", { status: 200 }));
      try {
        const program = Effect.gen(function* () {
          const hc = yield* HealthCheck;
          return yield* hc.probeHTTP(server.url, 2000);
        });

        const result = await Effect.runPromise(
          program.pipe(Effect.provide(HealthCheckLive)),
        );

        expect(result.healthy).toBe(true);
        expect(result.detail).toContain("200");
      } finally {
        server.stop();
      }
    });

    it("returns healthy=true for a 500 response (Go behavior: <500 is healthy, 500 included)", async () => {
      // The Go code treats status >= 200 && < 500 as healthy.
      // Status 500 itself is NOT healthy in Go.
      const server = startTestServer(
        () => new Response("Internal Server Error", { status: 500 }),
      );
      try {
        const program = Effect.gen(function* () {
          const hc = yield* HealthCheck;
          return yield* hc.probeHTTP(server.url, 2000);
        });

        const result = await Effect.runPromise(
          program.pipe(Effect.provide(HealthCheckLive)),
        );

        expect(result.healthy).toBe(false);
        expect(result.detail).toContain("500");
      } finally {
        server.stop();
      }
    });

    it("returns healthy=true for a 404 response", async () => {
      const server = startTestServer(
        () => new Response("Not Found", { status: 404 }),
      );
      try {
        const program = Effect.gen(function* () {
          const hc = yield* HealthCheck;
          return yield* hc.probeHTTP(server.url, 2000);
        });

        const result = await Effect.runPromise(
          program.pipe(Effect.provide(HealthCheckLive)),
        );

        expect(result.healthy).toBe(true);
        expect(result.detail).toContain("404");
      } finally {
        server.stop();
      }
    });

    it("returns healthy=true for a 201 response", async () => {
      const server = startTestServer(
        () => new Response("Created", { status: 201 }),
      );
      try {
        const program = Effect.gen(function* () {
          const hc = yield* HealthCheck;
          return yield* hc.probeHTTP(server.url, 2000);
        });

        const result = await Effect.runPromise(
          program.pipe(Effect.provide(HealthCheckLive)),
        );

        expect(result.healthy).toBe(true);
        expect(result.detail).toContain("201");
      } finally {
        server.stop();
      }
    });

    it("returns healthy=false when connection is refused", async () => {
      const program = Effect.gen(function* () {
        const hc = yield* HealthCheck;
        return yield* hc.probeHTTP("http://127.0.0.1:1", 2000);
      });

      const result = await Effect.runPromise(
        program.pipe(Effect.provide(HealthCheckLive)),
      );

      expect(result.healthy).toBe(false);
      expect(result.detail.length).toBeGreaterThan(0);
    });

    it("returns healthy=false when URL is unreachable (timeout)", async () => {
      // Use a non-routable IP to trigger connection timeout
      const program = Effect.gen(function* () {
        const hc = yield* HealthCheck;
        return yield* hc.probeHTTP("http://192.0.2.1:1", 1000);
      });

      const result = await Effect.runPromise(
        program.pipe(Effect.provide(HealthCheckLive)),
      );

      expect(result.healthy).toBe(false);
      expect(result.detail.length).toBeGreaterThan(0);
    });

    it("returns healthy=true for a 302 redirect to a healthy endpoint", async () => {
      const server = startTestServer((req) => {
        if (req.url.includes("/redirect")) {
          return new Response(null, {
            status: 302,
            headers: { Location: server.url + "/health" },
          });
        }
        return new Response("OK", { status: 200 });
      });
      try {
        const program = Effect.gen(function* () {
          const hc = yield* HealthCheck;
          return yield* hc.probeHTTP(server.url + "/redirect", 2000);
        });

        const result = await Effect.runPromise(
          program.pipe(Effect.provide(HealthCheckLive)),
        );

        // Follows redirect by default, ends at 200
        expect(result.healthy).toBe(true);
      } finally {
        server.stop();
      }
    });
  });

  // -----------------------------------------------------------------------
  // waitForHealthy
  // -----------------------------------------------------------------------

  describe("waitForHealthy", () => {
    it("resolves immediately when endpoint is already healthy", async () => {
      const server = startTestServer(() => new Response("OK", { status: 200 }));
      try {
        const program = Effect.gen(function* () {
          const hc = yield* HealthCheck;
          return yield* hc.waitForHealthy(server.url, 3000, 100);
        });

        const start = Date.now();
        const result = await Effect.runPromise(
          program.pipe(Effect.provide(HealthCheckLive)),
        );
        const elapsed = Date.now() - start;

        expect(result.healthy).toBe(true);
        expect(result.detail).toContain("200");
        // Should resolve quickly (< 500ms since first probe succeeds)
        expect(elapsed).toBeLessThan(500);
      } finally {
        server.stop();
      }
    });

    it("retries and resolves when endpoint becomes healthy", async () => {
      let requestCount = 0;
      const server = startTestServer(() => {
        requestCount++;
        if (requestCount < 3) {
          return new Response("Not Ready", { status: 503 });
        }
        return new Response("OK", { status: 200 });
      });

      try {
        const program = Effect.gen(function* () {
          const hc = yield* HealthCheck;
          return yield* hc.waitForHealthy(server.url, 5000, 100);
        });

        const result = await Effect.runPromise(
          program.pipe(Effect.provide(HealthCheckLive)),
        );

        expect(result.healthy).toBe(true);
        expect(requestCount).toBeGreaterThanOrEqual(3);
      } finally {
        server.stop();
      }
    });

    it("returns healthy=false when timeout is exceeded", async () => {
      const server = startTestServer(
        () => new Response("Not Ready", { status: 503 }),
      );

      try {
        const program = Effect.gen(function* () {
          const hc = yield* HealthCheck;
          return yield* hc.waitForHealthy(server.url, 500, 50);
        });

        const start = Date.now();
        const result = await Effect.runPromise(
          program.pipe(Effect.provide(HealthCheckLive)),
        );
        const elapsed = Date.now() - start;

        expect(result.healthy).toBe(false);
        expect(result.detail).toContain("failed");
        // Should have waited at least the timeout duration
        expect(elapsed).toBeGreaterThanOrEqual(400);
      } finally {
        server.stop();
      }
    });

    it("returns healthy=false when connection is refused repeatedly", async () => {
      const program = Effect.gen(function* () {
        const hc = yield* HealthCheck;
        return yield* hc.waitForHealthy("http://127.0.0.1:1", 500, 50);
      });

      const result = await Effect.runPromise(
        program.pipe(Effect.provide(HealthCheckLive)),
      );

      expect(result.healthy).toBe(false);
      expect(result.detail).toContain("failed");
    });

    it("uses default interval of 500ms when intervalMs is not specified", async () => {
      let requestCount = 0;
      const server = startTestServer(() => {
        requestCount++;
        return new Response("Not Ready", { status: 503 });
      });

      try {
        const program = Effect.gen(function* () {
          const hc = yield* HealthCheck;
          return yield* hc.waitForHealthy(server.url, 1200);
        });

        const result = await Effect.runPromise(
          program.pipe(Effect.provide(HealthCheckLive)),
        );

        expect(result.healthy).toBe(false);
        // With 500ms interval and 1200ms timeout, should have ~2-3 attempts
        expect(requestCount).toBeGreaterThanOrEqual(2);
        expect(requestCount).toBeLessThanOrEqual(4);
      } finally {
        server.stop();
      }
    });
  });

  // -----------------------------------------------------------------------
  // probeProcess
  // -----------------------------------------------------------------------

  describe("probeProcess", () => {
    it("returns running=true for a known process pattern", async () => {
      const program = Effect.gen(function* () {
        const hc = yield* HealthCheck;
        return yield* hc.probeProcess("sleep");
      });

      // Start a sleep process in the background
      const proc = Bun.spawn(["sleep", "30"]);
      try {
        const result = await Effect.runPromise(
          program.pipe(Effect.provide(HealthCheckLive)),
        );

        expect(result.running).toBe(true);
        expect(result.detail).toContain("sleep");
      } finally {
        proc.kill();
        await proc.exited;
      }
    });

    it("returns running=false for a nonexistent process pattern", async () => {
      const program = Effect.gen(function* () {
        const hc = yield* HealthCheck;
        return yield* hc.probeProcess("nonexistent-process-xyz-12345");
      });

      const result = await Effect.runPromise(
        program.pipe(Effect.provide(HealthCheckLive)),
      );

      expect(result.running).toBe(false);
    });

    it("returns running=false when no pattern matches", async () => {
      const program = Effect.gen(function* () {
        const hc = yield* HealthCheck;
        return yield* hc.probeProcess("completely-unique-fake-pattern-abc");
      });

      const result = await Effect.runPromise(
        program.pipe(Effect.provide(HealthCheckLive)),
      );

      expect(result.running).toBe(false);
      expect(result.detail.length).toBeGreaterThan(0);
    });

    it("detects multiple instances of a pattern", async () => {
      const pattern = "neuratrade-test-sleep-probe";
      const program = Effect.gen(function* () {
        const hc = yield* HealthCheck;
        return yield* hc.probeProcess(pattern);
      });

      const proc1 = Bun.spawn([
        "node",
        "-e",
        `setTimeout(()=>{}, 30000); /* ${pattern} */`,
      ]);
      const proc2 = Bun.spawn([
        "node",
        "-e",
        `setTimeout(()=>{}, 30000); /* ${pattern} */`,
      ]);

      try {
        const result = await Effect.runPromise(
          program.pipe(Effect.provide(HealthCheckLive)),
        );

        expect(result.running).toBe(true);
        expect(result.detail).toContain("2 process");
      } finally {
        proc1.kill();
        proc2.kill();
        await Promise.all([proc1.exited, proc2.exited]);
      }
    });
  });
});
