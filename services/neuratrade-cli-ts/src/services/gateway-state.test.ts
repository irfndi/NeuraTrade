import { describe, expect, it } from "bun:test";
import * as fs from "fs";
import * as os from "os";
import * as nodePath from "path";
import { Effect } from "effect";
import { BunFileSystem } from "@effect/platform-bun";
import { PathLive } from "./path.ts";
import { GatewayState, GatewayStateLive } from "./gateway-state.ts";
import type { GatewayState as GatewayStateType } from "../schemas/gateway-state.ts";

function tmpDir(): string {
  return fs.mkdtempSync(nodePath.join(os.tmpdir(), "gateway-state-test-"));
}

function rmDir(dir: string): void {
  try {
    fs.rmSync(dir, { recursive: true, force: true });
  } catch {
    // ignore
  }
}

function writeJson(filePath: string, data: unknown): void {
  fs.mkdirSync(nodePath.dirname(filePath), { recursive: true });
  fs.writeFileSync(filePath, JSON.stringify(data, null, 2));
}

function provideAll<A, E, R>(effect: Effect.Effect<A, E, R>, home: string) {
  return effect.pipe(
    Effect.provide(GatewayStateLive),
    Effect.provide(PathLive(home)),
    Effect.provide(BunFileSystem.layer),
  );
}

function makeDefaultState(
  overrides?: Partial<GatewayStateType>,
): GatewayStateType {
  return {
    mode: "starting",
    supervised: false,
    updated_at: "2025-01-01T00:00:00Z",
    health_timeout_seconds: 150,
    services: {},
    ...overrides,
  };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("GatewayState service", () => {
  // -----------------------------------------------------------------------
  // read — missing file returns default
  // -----------------------------------------------------------------------

  describe("read", () => {
    it("returns a default state when the file does not exist", async () => {
      const home = tmpDir();
      try {
        const program = Effect.gen(function* () {
          const gw = yield* GatewayState;
          return yield* gw.read();
        });

        const result = await Effect.runPromise(provideAll(program, home));

        expect(result.mode).toBe("");
        expect(result.supervised).toBe(false);
        expect(result.services).toEqual({});
      } finally {
        rmDir(home);
      }
    });

    it("returns the persisted state when the file exists", async () => {
      const home = tmpDir();
      try {
        const statePath = nodePath.join(home, "pids", "gateway-state.json");
        writeJson(statePath, {
          mode: "healthy",
          supervised: true,
          updated_at: "2025-06-01T12:00:00Z",
          health_timeout_seconds: 120,
          services: {
            backend: {
              status: "healthy",
              endpoint: "http://127.0.0.1:8080/health",
            },
          },
        });

        const program = Effect.gen(function* () {
          const gw = yield* GatewayState;
          return yield* gw.read();
        });

        const result = await Effect.runPromise(provideAll(program, home));

        expect(result.mode).toBe("healthy");
        expect(result.supervised).toBe(true);
        expect(result.health_timeout_seconds).toBe(120);
        expect(result.services.backend.status).toBe("healthy");
        expect(result.services.backend.endpoint).toBe(
          "http://127.0.0.1:8080/health",
        );
      } finally {
        rmDir(home);
      }
    });

    it("returns default state when file contains invalid JSON", async () => {
      const home = tmpDir();
      try {
        const statePath = nodePath.join(home, "pids", "gateway-state.json");
        fs.mkdirSync(nodePath.dirname(statePath), { recursive: true });
        fs.writeFileSync(statePath, "not-valid-json{{{");

        const program = Effect.gen(function* () {
          const gw = yield* GatewayState;
          return yield* gw.read();
        });

        const result = await Effect.runPromise(provideAll(program, home));

        expect(result.mode).toBe("");
        expect(result.services).toEqual({});
      } finally {
        rmDir(home);
      }
    });
  });

  // -----------------------------------------------------------------------
  // write — round-trip
  // -----------------------------------------------------------------------

  describe("write", () => {
    it("writes state to disk and reads it back (round-trip)", async () => {
      const home = tmpDir();
      try {
        const program = Effect.gen(function* () {
          const gw = yield* GatewayState;
          const state = makeDefaultState({
            mode: "healthy",
            supervised: true,
            health_timeout_seconds: 200,
            services: {
              backend: {
                status: "healthy",
                endpoint: "http://localhost:8080/health",
              },
            },
          });
          yield* gw.write(state);
          return yield* gw.read();
        });

        const result = await Effect.runPromise(provideAll(program, home));

        expect(result.mode).toBe("healthy");
        expect(result.supervised).toBe(true);
        expect(result.health_timeout_seconds).toBe(200);
        expect(result.services.backend.status).toBe("healthy");
      } finally {
        rmDir(home);
      }
    });

    it("sets updated_at to a valid ISO timestamp on write", async () => {
      const home = tmpDir();
      try {
        const program = Effect.gen(function* () {
          const gw = yield* GatewayState;
          const state = makeDefaultState({ mode: "starting" });
          yield* gw.write(state);
          return yield* gw.read();
        });

        const result = await Effect.runPromise(provideAll(program, home));

        // Should be a valid ISO string
        const parsed = new Date(result.updated_at);
        expect(parsed.toISOString()).toBe(result.updated_at);
        // Should be recent (within 5 seconds)
        expect(Date.now() - parsed.getTime()).toBeLessThan(5000);
      } finally {
        rmDir(home);
      }
    });

    it("creates parent directories if they do not exist", async () => {
      const home = tmpDir();
      try {
        const program = Effect.gen(function* () {
          const gw = yield* GatewayState;
          yield* gw.write(makeDefaultState({ mode: "starting" }));
          return yield* gw.read();
        });

        const result = await Effect.runPromise(provideAll(program, home));

        const statePath = nodePath.join(home, "pids", "gateway-state.json");
        expect(fs.existsSync(statePath)).toBe(true);
        expect(result.mode).toBe("starting");
      } finally {
        rmDir(home);
      }
    });

    it("overwrites existing state file", async () => {
      const home = tmpDir();
      try {
        const program = Effect.gen(function* () {
          const gw = yield* GatewayState;
          yield* gw.write(makeDefaultState({ mode: "starting" }));
          yield* gw.write(makeDefaultState({ mode: "healthy" }));
          return yield* gw.read();
        });

        const result = await Effect.runPromise(provideAll(program, home));

        expect(result.mode).toBe("healthy");
      } finally {
        rmDir(home);
      }
    });
  });

  // -----------------------------------------------------------------------
  // writeMode
  // -----------------------------------------------------------------------

  describe("writeMode", () => {
    it("writes mode to a fresh file when no state exists", async () => {
      const home = tmpDir();
      try {
        const program = Effect.gen(function* () {
          const gw = yield* GatewayState;
          yield* gw.writeMode("healthy", "services started");
          return yield* gw.read();
        });

        const result = await Effect.runPromise(provideAll(program, home));

        expect(result.mode).toBe("healthy");
        expect(result.services.gateway).toEqual({
          status: "healthy",
          detail: "services started",
        });
      } finally {
        rmDir(home);
      }
    });

    it("updates mode on existing state without losing services", async () => {
      const home = tmpDir();
      try {
        const program = Effect.gen(function* () {
          const gw = yield* GatewayState;
          yield* gw.write(
            makeDefaultState({
              mode: "starting",
              services: {
                backend: {
                  status: "healthy",
                  endpoint: "http://localhost:8080/health",
                },
              },
            }),
          );
          yield* gw.writeMode("warming", "backend warming up");
          return yield* gw.read();
        });

        const result = await Effect.runPromise(provideAll(program, home));

        expect(result.mode).toBe("warming");
        expect(result.services.backend.status).toBe("healthy");
        expect(result.services.gateway).toEqual({
          status: "warming",
          detail: "backend warming up",
        });
      } finally {
        rmDir(home);
      }
    });

    it("updates mode without gateway service entry when detail is empty", async () => {
      const home = tmpDir();
      try {
        const program = Effect.gen(function* () {
          const gw = yield* GatewayState;
          yield* gw.write(
            makeDefaultState({
              services: {
                backend: { status: "healthy" },
              },
            }),
          );
          yield* gw.writeMode("degraded");
          return yield* gw.read();
        });

        const result = await Effect.runPromise(provideAll(program, home));

        expect(result.mode).toBe("degraded");
        expect(result.services.gateway).toBeUndefined();
        expect(result.services.backend.status).toBe("healthy");
      } finally {
        rmDir(home);
      }
    });
  });

  // -----------------------------------------------------------------------
  // writeServiceState
  // -----------------------------------------------------------------------

  describe("writeServiceState", () => {
    it("creates a new service entry on a fresh file", async () => {
      const home = tmpDir();
      try {
        const program = Effect.gen(function* () {
          const gw = yield* GatewayState;
          yield* gw.writeServiceState(
            "backend",
            "healthy",
            "probe ok",
            "http://127.0.0.1:8080/health",
          );
          return yield* gw.read();
        });

        const result = await Effect.runPromise(provideAll(program, home));

        expect(result.services.backend).toEqual({
          status: "healthy",
          detail: "probe ok",
          endpoint: "http://127.0.0.1:8080/health",
        });
      } finally {
        rmDir(home);
      }
    });

    it("updates a single service without affecting others", async () => {
      const home = tmpDir();
      try {
        const program = Effect.gen(function* () {
          const gw = yield* GatewayState;
          yield* gw.write(
            makeDefaultState({
              services: {
                backend: { status: "starting" },
                telegram: { status: "starting" },
              },
            }),
          );
          yield* gw.writeServiceState(
            "backend",
            "healthy",
            "ok",
            "http://127.0.0.1:8080/health",
          );
          return yield* gw.read();
        });

        const result = await Effect.runPromise(provideAll(program, home));

        expect(result.services.backend.status).toBe("healthy");
        expect(result.services.backend.detail).toBe("ok");
        expect(result.services.backend.endpoint).toBe(
          "http://127.0.0.1:8080/health",
        );
        expect(result.services.telegram.status).toBe("starting");
      } finally {
        rmDir(home);
      }
    });

    it("omits detail and endpoint when not provided", async () => {
      const home = tmpDir();
      try {
        const program = Effect.gen(function* () {
          const gw = yield* GatewayState;
          yield* gw.writeServiceState("ccxt", "embedded", "native mode");
          return yield* gw.read();
        });

        const result = await Effect.runPromise(provideAll(program, home));

        expect(result.services.ccxt).toEqual({
          status: "embedded",
          detail: "native mode",
        });
        expect(result.services.ccxt.endpoint).toBeUndefined();
      } finally {
        rmDir(home);
      }
    });

    it("writes with only status (no detail, no endpoint)", async () => {
      const home = tmpDir();
      try {
        const program = Effect.gen(function* () {
          const gw = yield* GatewayState;
          yield* gw.writeServiceState("telegram", "disabled");
          return yield* gw.read();
        });

        const result = await Effect.runPromise(provideAll(program, home));

        expect(result.services.telegram).toEqual({
          status: "disabled",
        });
      } finally {
        rmDir(home);
      }
    });
  });

  // -----------------------------------------------------------------------
  // markStopped
  // -----------------------------------------------------------------------

  describe("markStopped", () => {
    it("marks gateway and all known services as down on fresh file", async () => {
      const home = tmpDir();
      try {
        const program = Effect.gen(function* () {
          const gw = yield* GatewayState;
          yield* gw.markStopped("gateway stopped");
          return yield* gw.read();
        });

        const result = await Effect.runPromise(provideAll(program, home));

        expect(result.mode).toBe("down");
        expect(result.services.gateway).toEqual({
          status: "down",
          detail: "gateway stopped",
        });
        expect(result.services.backend).toEqual({
          status: "down",
          detail: "gateway stopped",
        });
        expect(result.services.ccxt).toEqual({
          status: "down",
          detail: "gateway stopped",
        });
        expect(result.services.telegram).toEqual({
          status: "down",
          detail: "gateway stopped",
        });
      } finally {
        rmDir(home);
      }
    });

    it("overwrites existing service states with down", async () => {
      const home = tmpDir();
      try {
        const program = Effect.gen(function* () {
          const gw = yield* GatewayState;
          yield* gw.write(
            makeDefaultState({
              mode: "healthy",
              services: {
                backend: {
                  status: "healthy",
                  endpoint: "http://127.0.0.1:8080/health",
                },
                ccxt: { status: "embedded", detail: "native mode" },
                telegram: {
                  status: "healthy",
                  endpoint: "http://127.0.0.1:3002/health",
                },
              },
            }),
          );
          yield* gw.markStopped("backend health check failed");
          return yield* gw.read();
        });

        const result = await Effect.runPromise(provideAll(program, home));

        expect(result.mode).toBe("down");
        expect(result.services.gateway.status).toBe("down");
        expect(result.services.backend.status).toBe("down");
        expect(result.services.backend.detail).toBe(
          "backend health check failed",
        );
        expect(result.services.ccxt.status).toBe("down");
        expect(result.services.telegram.status).toBe("down");
      } finally {
        rmDir(home);
      }
    });

    it("preserves custom services that are not gateway/backend/ccxt/telegram", async () => {
      const home = tmpDir();
      try {
        const program = Effect.gen(function* () {
          const gw = yield* GatewayState;
          yield* gw.write(
            makeDefaultState({
              mode: "healthy",
              services: {
                backend: { status: "healthy" },
                custom_worker: { status: "running", detail: "custom" },
              },
            }),
          );
          yield* gw.markStopped("gateway stopped");
          return yield* gw.read();
        });

        const result = await Effect.runPromise(provideAll(program, home));

        expect(result.services.backend.status).toBe("down");
        // Custom services are untouched (only gateway/backend/ccxt/telegram are reset)
        expect(result.services.custom_worker.status).toBe("running");
        expect(result.services.custom_worker.detail).toBe("custom");
      } finally {
        rmDir(home);
      }
    });

    it("uses a default detail when none is provided", async () => {
      const home = tmpDir();
      try {
        const program = Effect.gen(function* () {
          const gw = yield* GatewayState;
          yield* gw.markStopped();
          return yield* gw.read();
        });

        const result = await Effect.runPromise(provideAll(program, home));

        expect(result.mode).toBe("down");
        expect(result.services.gateway.detail).toBe("gateway stopped");
      } finally {
        rmDir(home);
      }
    });
  });
});
