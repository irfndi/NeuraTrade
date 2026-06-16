import { describe, expect, it, beforeEach, afterEach } from "bun:test";
import * as fs from "fs";
import * as os from "os";
import * as nodePath from "path";
import { Effect, Layer } from "effect";
import { BunFileSystem } from "@effect/platform-bun";
import { Path, PathLive } from "./path.ts";
import { PidFile, PidFileLive } from "./pid.ts";
import { Logger, LoggerLive } from "./logger.ts";
import { ProcessManager, ProcessError } from "./process-manager.ts";
import { HealthCheck } from "./health-check.ts";
import { GatewayState, GatewayStateLive } from "./gateway-state.ts";
import {
  GatewayOrchestrator,
  GatewayOrchestratorLive,
} from "./gateway-orchestrator.ts";
import type { ResolvedConfig } from "./config.ts";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function tmpDir(): string {
  return fs.mkdtempSync(nodePath.join(os.tmpdir(), "gw-orch-test-"));
}

function rmDir(dir: string): void {
  try {
    fs.rmSync(dir, { recursive: true, force: true });
  } catch {
    // ignore
  }
}

function readState(home: string): Record<string, unknown> {
  const p = nodePath.join(home, "pids", "gateway-state.json");
  return JSON.parse(fs.readFileSync(p, "utf-8")) as Record<string, unknown>;
}

function makeConfig(overrides?: Partial<ResolvedConfig>): ResolvedConfig {
  return {
    server: { host: "0.0.0.0", port: 8080 },
    database: { driver: "sqlite", sqlite_path: "/tmp/test.db" },
    redis: { host: "127.0.0.1", port: 6379 },
    ccxt: {
      service_url: "http://localhost:3001",
      grpc_address: "127.0.0.1:50051",
    },
    telegram: {
      service_url: "http://localhost:3002",
      grpc_address: "127.0.0.1:50052",
      use_polling: true,
      api_base_url: "http://localhost:8080",
    },
    ai: {
      provider: "openai",
      model: "gpt-4o-mini",
      base_url: undefined,
      temperature: 0.7,
      max_tokens: 4096,
      min_confidence: 0.7,
      daily_budget: "10",
      routing_mode: "primary",
    },
    features: {
      enable_ai: true,
      enable_ai_scalping: true,
      enable_ai_signals: false,
      enable_ai_arbitrage: false,
      paper_trading: true,
      real_trading: false,
    },
    gateway: {
      bind_host: "127.0.0.1",
      ccxt_port: 3001,
      telegram_port: 3002,
      telegram_grpc_port: 50052,
      supervised: false,
      health_timeout_seconds: 150,
      signal_timeout_seconds: 5,
      graceful_timeout_seconds: 10,
      skip_telegram: false,
    },
    admin_api_key: "test-admin-key-32-chars-long!!!!!",
    jwt_secret: "test-jwt-secret-32-chars-long!!!!!!",
    telegram_bot_token: "",
    ai_api_key: "",
    chat_id: "",
    ...overrides,
  };
}

// ---------------------------------------------------------------------------
// Mock state
// ---------------------------------------------------------------------------

interface MockState {
  startCalls: Array<{
    binary: string;
    name: string;
    logPath: string;
    env: Record<string, string>;
    pidFile: string;
  }>;
  stopCalls: Array<{
    name: string;
    pidFile: string;
    patterns: ReadonlyArray<string>;
  }>;
  cleanupCalls: Array<ReadonlyArray<string>>;
}

function createMockState(): MockState {
  return { startCalls: [], stopCalls: [], cleanupCalls: [] };
}

// ---------------------------------------------------------------------------
// Mock ProcessManager
// ---------------------------------------------------------------------------

function createMockPM(
  proc: Bun.Subprocess,
  state: MockState,
  opts?: { stopFails?: boolean },
) {
  return {
    resolveServiceBinary: (_name: string, _execDir?: string) =>
      Effect.succeed("/usr/bin/sleep") as Effect.Effect<
        string,
        ProcessError,
        never
      >,

    startService: (
      _binary: string,
      name: string,
      logPath: string,
      env: Record<string, string>,
      pidFile: string,
    ) =>
      Effect.sync(() => {
        state.startCalls.push({ binary: _binary, name, logPath, env, pidFile });
        return proc;
      }) as Effect.Effect<Bun.Subprocess, ProcessError, never>,

    stopServiceByPIDFile: (
      name: string,
      pidFile: string,
      patterns: ReadonlyArray<string>,
    ) => {
      state.stopCalls.push({ name, pidFile, patterns });
      if (opts?.stopFails) {
        return Effect.fail(
          new ProcessError(`${name}: not running (PID file not found)`),
        ) as Effect.Effect<void, ProcessError, never>;
      }
      return Effect.void as Effect.Effect<void, ProcessError, never>;
    },

    signalAndWait: (
      _subprocess: Bun.Subprocess,
      _signal: NodeJS.Signals,
      _timeoutMs: number,
    ) => Effect.succeed(true) as Effect.Effect<boolean, never, never>,

    cleanupStalePIDs: (services: ReadonlyArray<string>) =>
      Effect.sync(() => {
        state.cleanupCalls.push([...services]);
      }) as Effect.Effect<void, never, never>,
  };
}

// ---------------------------------------------------------------------------
// Mock HealthCheck
// ---------------------------------------------------------------------------

function createMockHC(healthy: boolean): HealthCheck {
  return {
    probeHTTP: (_url: string, _timeoutMs: number) =>
      Effect.succeed({
        healthy,
        detail: healthy
          ? "HTTP 200 from http://127.0.0.1:8080/health"
          : "health check failed at http://127.0.0.1:8080/health within 5000ms (connection refused)",
      }),

    waitForHealthy: (_url: string, _timeoutMs: number) =>
      Effect.succeed({
        healthy,
        detail: healthy
          ? "Backend API reachable (200)"
          : "Backend API failed health check at http://127.0.0.1:8080/health within 150000ms (connection refused)",
      }),

    probeProcess: (_pattern: string) =>
      Effect.succeed({ running: true, detail: "found 1 process" }),

    probeHealthJSON: (_url: string, _timeoutMs: number) =>
      Effect.succeed({
        ok: healthy,
        status: healthy ? "healthy" : "unhealthy",
        services: healthy
          ? { database: "healthy", redis: "healthy", ccxt: "healthy" }
          : undefined,
      }),
  };
}

// ---------------------------------------------------------------------------
// Layer builder
// ---------------------------------------------------------------------------

function buildOrchLayer(
  home: string,
  pm: ReturnType<typeof createMockPM>,
  hc: ReturnType<typeof createMockHC>,
) {
  const base = Layer.mergeAll(PathLive(home), BunFileSystem.layer, LoggerLive);
  const pidFile = Layer.provide(PidFileLive, base);
  const withPidFile = Layer.merge(base, pidFile);
  const gwState = Layer.provide(GatewayStateLive, withPidFile);
  const pmLayer = Layer.succeed(ProcessManager, pm);
  const hcLayer = Layer.succeed(HealthCheck, hc);
  const all = Layer.mergeAll(withPidFile, gwState, pmLayer, hcLayer);
  return Layer.provide(GatewayOrchestratorLive, all);
}

async function runOrch<A>(
  effect: Effect.Effect<A, unknown, GatewayOrchestrator>,
  home: string,
  pm: ReturnType<typeof createMockPM>,
  hc: ReturnType<typeof createMockHC>,
): Promise<A> {
  return Effect.runPromise(
    effect.pipe(Effect.provide(buildOrchLayer(home, pm, hc))),
  );
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("GatewayOrchestrator service", () => {
  let home: string;
  let mockState: MockState;
  let mockProc: Bun.Subprocess;
  let savedCcxtUrl: string | undefined;
  let savedCcxtGrpc: string | undefined;

  beforeEach(() => {
    home = tmpDir();
    mockState = createMockState();
    mockProc = Bun.spawn(["sleep", "60"]);
    savedCcxtUrl = process.env.CCXT_SERVICE_URL;
    savedCcxtGrpc = process.env.CCXT_GRPC_ADDRESS;
    delete process.env.CCXT_SERVICE_URL;
    delete process.env.CCXT_GRPC_ADDRESS;
  });

  afterEach(() => {
    try {
      mockProc.kill("SIGKILL");
    } catch {
      // already dead
    }
    if (savedCcxtUrl !== undefined) process.env.CCXT_SERVICE_URL = savedCcxtUrl;
    else delete process.env.CCXT_SERVICE_URL;
    if (savedCcxtGrpc !== undefined)
      process.env.CCXT_GRPC_ADDRESS = savedCcxtGrpc;
    else delete process.env.CCXT_GRPC_ADDRESS;
    rmDir(home);
  });

  // -----------------------------------------------------------------------
  // start
  // -----------------------------------------------------------------------

  describe("start", () => {
    it("spawns backend and writes gateway state", async () => {
      const pm = createMockPM(mockProc, mockState);
      const hc = createMockHC(true);

      const result = await runOrch(
        Effect.gen(function* () {
          const orch = yield* GatewayOrchestrator;
          return yield* orch.start({
            supervised: false,
            config: makeConfig(),
          });
        }),
        home,
        pm,
        hc,
      );

      expect(result.mode).toBe("healthy");
      expect(result.ccxtMode).toBe("native");
      expect(result.telegramEnabled).toBe(false);
      expect(result.backendPid).toBe(mockProc.pid);

      // Verify state was written
      const state = readState(home);
      expect(state["mode"]).toBe("healthy");
      const services = state["services"] as Record<
        string,
        { status: string; detail?: string; endpoint?: string }
      >;
      expect(services["backend"]["status"]).toBe("healthy");
      expect(services["ccxt"]["status"]).toBe("embedded");
      expect(services["telegram"]["status"]).toBe("disabled");

      // Verify startService was called for backend only
      expect(mockState.startCalls.length).toBe(1);
      expect(mockState.startCalls[0]["name"]).toBe("Backend API");
      expect(mockState.startCalls[0]["env"]["PORT"]).toBe("8080");
      expect(mockState.startCalls[0]["env"]["HOST"]).toBe("0.0.0.0");
    });

    it("writes backend env map matching Go", async () => {
      const pm = createMockPM(mockProc, mockState);
      const hc = createMockHC(true);

      await runOrch(
        Effect.gen(function* () {
          const orch = yield* GatewayOrchestrator;
          return yield* orch.start({
            supervised: false,
            config: makeConfig({
              admin_api_key: "long-admin-key-that-is-definitely-32-chars!!",
              jwt_secret: "long-jwt-secret-that-is-definitely-32-chars!!!",
              database: { driver: "sqlite", sqlite_path: "/data/test.db" },
              redis: { host: "redis.local", port: 6380 },
            }),
          });
        }),
        home,
        pm,
        hc,
      );

      const env = mockState.startCalls[0]["env"];
      expect(env["DATABASE_DRIVER"]).toBe("sqlite");
      expect(env["SQLITE_PATH"]).toBe("/data/test.db");
      expect(env["SQLITE_DB_PATH"]).toBe("/data/test.db");
      expect(env["REDIS_HOST"]).toBe("redis.local");
      expect(env["REDIS_PORT"]).toBe("6380");
      expect(env["TELEGRAM_SERVICE_URL"]).toBe("http://127.0.0.1:3002");
      expect(env["TELEGRAM_GRPC_ADDRESS"]).toBe("127.0.0.1:50052");
      expect(env["FEATURES_PAPER_TRADING"]).toBe("true");
      expect(env["FEATURES_REAL_TRADING"]).toBe("false");
    });

    it("supervised mode sets warming on health failure", async () => {
      const pm = createMockPM(mockProc, mockState);
      const hc = createMockHC(false); // unhealthy

      const result = await runOrch(
        Effect.gen(function* () {
          const orch = yield* GatewayOrchestrator;
          return yield* orch.start({
            supervised: true,
            config: makeConfig(),
          });
        }),
        home,
        pm,
        hc,
      );

      expect(result.mode).toBe("warming");

      const state = readState(home);
      expect(state["mode"]).toBe("warming");
      const services = state["services"] as Record<string, { status: string }>;
      expect(services["backend"]["status"]).toBe("warming");
    });

    it("non-supervised mode fails on health failure", async () => {
      const pm = createMockPM(mockProc, mockState);
      const hc = createMockHC(false);

      try {
        await runOrch(
          Effect.gen(function* () {
            const orch = yield* GatewayOrchestrator;
            return yield* orch.start({
              supervised: false,
              config: makeConfig(),
            });
          }),
          home,
          pm,
          hc,
        );
        throw new Error("Expected failure");
      } catch (err) {
        if (err instanceof Error && err.message === "Expected failure") {
          throw err;
        }
        const msg = err instanceof Error ? err.message : String(err);
        expect(msg).toContain("failed health check");
      }

      // Backend should have been signaled to stop
      const state = readState(home);
      expect(state["mode"]).toBe("down");
    });

    it("sets ccxt to external when CCXT_SERVICE_URL is set", async () => {
      process.env.CCXT_SERVICE_URL = "http://ccxt.example.com:3001";
      const pm = createMockPM(mockProc, mockState);
      const hc = createMockHC(true);

      const result = await runOrch(
        Effect.gen(function* () {
          const orch = yield* GatewayOrchestrator;
          return yield* orch.start({
            supervised: false,
            config: makeConfig(),
          });
        }),
        home,
        pm,
        hc,
      );

      expect(result.ccxtMode).toBe("external");

      const state = readState(home);
      const services = state["services"] as Record<
        string,
        { status: string; detail?: string }
      >;
      expect(services["ccxt"]["status"]).toBe("external");
      expect(services["ccxt"]["detail"]).toBe("external endpoint configured");
    });

    it("sets ccxt to external when CCXT_GRPC_ADDRESS is set", async () => {
      process.env.CCXT_GRPC_ADDRESS = "ccxt-grpc.example.com:50051";
      const pm = createMockPM(mockProc, mockState);
      const hc = createMockHC(true);

      const result = await runOrch(
        Effect.gen(function* () {
          const orch = yield* GatewayOrchestrator;
          return yield* orch.start({
            supervised: false,
            config: makeConfig(),
          });
        }),
        home,
        pm,
        hc,
      );

      expect(result.ccxtMode).toBe("external");
    });

    it("disables telegram when no token", async () => {
      const pm = createMockPM(mockProc, mockState);
      const hc = createMockHC(true);

      const result = await runOrch(
        Effect.gen(function* () {
          const orch = yield* GatewayOrchestrator;
          return yield* orch.start({
            supervised: false,
            config: makeConfig({ telegram_bot_token: "" }),
          });
        }),
        home,
        pm,
        hc,
      );

      expect(result.telegramEnabled).toBe(false);
      expect(mockState.startCalls.length).toBe(1); // only backend

      const state = readState(home);
      const services = state["services"] as Record<
        string,
        { status: string; detail?: string }
      >;
      expect(services["telegram"]["status"]).toBe("disabled");
      expect(services["telegram"]["detail"]).toBe(
        "Telegram disabled for paper-only runtime",
      );
    });

    it("disables telegram when skip_telegram is true", async () => {
      const pm = createMockPM(mockProc, mockState);
      const hc = createMockHC(true);

      const result = await runOrch(
        Effect.gen(function* () {
          const orch = yield* GatewayOrchestrator;
          return yield* orch.start({
            supervised: false,
            config: makeConfig({
              telegram_bot_token: "valid-token-1234567890",
              gateway: {
                bind_host: "127.0.0.1",
                ccxt_port: 3001,
                telegram_port: 3002,
                telegram_grpc_port: 50052,
                supervised: false,
                health_timeout_seconds: 150,
                signal_timeout_seconds: 5,
                graceful_timeout_seconds: 10,
                skip_telegram: true,
              },
            }),
          });
        }),
        home,
        pm,
        hc,
      );

      expect(result.telegramEnabled).toBe(false);
      expect(mockState.startCalls.length).toBe(1); // only backend
    });

    it("spawns telegram when token is provided", async () => {
      const pm = createMockPM(mockProc, mockState);
      const hc = createMockHC(true);

      const result = await runOrch(
        Effect.gen(function* () {
          const orch = yield* GatewayOrchestrator;
          return yield* orch.start({
            supervised: false,
            config: makeConfig({ telegram_bot_token: "test-bot-token-123456" }),
          });
        }),
        home,
        pm,
        hc,
      );

      expect(result.telegramEnabled).toBe(true);
      expect(result.telegramPid).toBe(mockProc.pid);
      expect(mockState.startCalls.length).toBe(2);
      expect(mockState.startCalls[1]["name"]).toBe("Telegram Service");
      expect(mockState.startCalls[1]["env"]["TELEGRAM_BOT_TOKEN"]).toBe(
        "test-bot-token-123456",
      );
      expect(mockState.startCalls[1]["env"]["NODE_ENV"]).toBe("production");

      const state = readState(home);
      const services = state["services"] as Record<string, { status: string }>;
      expect(services["telegram"]["status"]).toBe("healthy");
    });

    it("supervised mode tolerates telegram health failure", async () => {
      let callCount = 0;
      const hc = {
        probeHTTP: (_url: string, _timeoutMs: number) =>
          Effect.succeed({ healthy: false, detail: "not ready" }),
        waitForHealthy: (_url: string, _timeoutMs: number) => {
          callCount++;
          // First call is backend (healthy), second is telegram (unhealthy)
          return Effect.succeed({
            healthy: callCount <= 1,
            detail:
              callCount <= 1
                ? "Backend API reachable (200)"
                : "Telegram failed",
          });
        },
        probeProcess: (_pattern: string) =>
          Effect.succeed({ running: true, detail: "found 1 process" }),
        probeHealthJSON: (_url: string, _timeoutMs: number) =>
          Effect.succeed({ ok: false, status: "unhealthy" }),
      };

      const pm = createMockPM(mockProc, mockState);

      const result = await runOrch(
        Effect.gen(function* () {
          const orch = yield* GatewayOrchestrator;
          return yield* orch.start({
            supervised: true,
            config: makeConfig({ telegram_bot_token: "test-token-123456" }),
          });
        }),
        home,
        pm,
        hc,
      );

      expect(result.mode).toBe("warming");
      expect(result.telegramEnabled).toBe(true);

      const state = readState(home);
      expect(state["mode"]).toBe("warming");
      const services = state["services"] as Record<string, { status: string }>;
      expect(services["backend"]["status"]).toBe("healthy");
      expect(services["telegram"]["status"]).toBe("warming");
    });
  });

  // -----------------------------------------------------------------------
  // stop
  // -----------------------------------------------------------------------

  describe("stop", () => {
    it("stops services via PID files and marks gateway stopped", async () => {
      const pm = createMockPM(mockProc, mockState);
      const hc = createMockHC(true);

      const result = await runOrch(
        Effect.gen(function* () {
          const orch = yield* GatewayOrchestrator;
          return yield* orch.stop();
        }),
        home,
        pm,
        hc,
      );

      expect(result.stoppedCount).toBe(3);
      expect(result.errors.length).toBe(0);

      // Verify stopServiceByPIDFile was called for each service
      expect(mockState.stopCalls.length).toBe(3);
      expect(mockState.stopCalls[0]["name"]).toBe("Backend API");
      expect(mockState.stopCalls[0]["pidFile"]).toBe("backend");
      expect(mockState.stopCalls[1]["name"]).toBe("CCXT Service");
      expect(mockState.stopCalls[1]["pidFile"]).toBe("ccxt");
      expect(mockState.stopCalls[2]["name"]).toBe("Telegram Service");
      expect(mockState.stopCalls[2]["pidFile"]).toBe("telegram");

      // Verify gateway state is marked down
      const state = readState(home);
      expect(state["mode"]).toBe("down");
      const services = state["services"] as Record<
        string,
        { status: string; detail?: string }
      >;
      expect(services["gateway"]["status"]).toBe("down");
      expect(services["gateway"]["detail"]).toBe("gateway stopped");
    });

    it("reports errors when stop fails", async () => {
      const pm = createMockPM(mockProc, mockState, { stopFails: true });
      const hc = createMockHC(true);

      const result = await runOrch(
        Effect.gen(function* () {
          const orch = yield* GatewayOrchestrator;
          return yield* orch.stop();
        }),
        home,
        pm,
        hc,
      );

      expect(result.stoppedCount).toBe(0);
      expect(result.errors.length).toBe(3);
      expect(result.errors[0]["service"]).toBe("Backend API");

      // Gateway should be marked stopped with "no running services"
      const state = readState(home);
      expect(state["mode"]).toBe("down");
      const services = state["services"] as Record<
        string,
        { status: string; detail?: string }
      >;
      expect(services["gateway"]["detail"]).toBe(
        "gateway stop found no running services",
      );
    });
  });

  // -----------------------------------------------------------------------
  // status
  // -----------------------------------------------------------------------

  describe("status", () => {
    it("reads persisted gateway state and probes health", async () => {
      // Pre-populate state
      const statePath = nodePath.join(home, "pids", "gateway-state.json");
      fs.mkdirSync(nodePath.dirname(statePath), { recursive: true });
      fs.writeFileSync(
        statePath,
        JSON.stringify({
          mode: "healthy",
          supervised: true,
          updated_at: "2025-06-01T12:00:00Z",
          health_timeout_seconds: 150,
          services: {
            backend: {
              status: "healthy",
              endpoint: "http://127.0.0.1:8080/health",
            },
            ccxt: { status: "embedded", detail: "native mode" },
          },
        }),
      );

      const pm = createMockPM(mockProc, mockState);
      const hc = createMockHC(true);

      const result = await runOrch(
        Effect.gen(function* () {
          const orch = yield* GatewayOrchestrator;
          return yield* orch.status();
        }),
        home,
        pm,
        hc,
      );

      expect(result.mode).toBe("healthy");
      expect(result.supervised).toBe(true);
      expect(result.services["backend"]["status"]).toBe("healthy");
      expect(result.services["ccxt"]["status"]).toBe("embedded");
      expect(result.backendHealth.healthy).toBe(true);
    });

    it("reports down services when no state file exists", async () => {
      const pm = createMockPM(mockProc, mockState);
      const hc = createMockHC(false);

      const result = await runOrch(
        Effect.gen(function* () {
          const orch = yield* GatewayOrchestrator;
          return yield* orch.status();
        }),
        home,
        pm,
        hc,
      );

      expect(result.mode).toBe("");
      expect(result.backendHealth.healthy).toBe(false);
    });

    it("probes health at 127.0.0.1 when bind_host is 0.0.0.0", async () => {
      const statePath = nodePath.join(home, "pids", "gateway-state.json");
      fs.mkdirSync(nodePath.dirname(statePath), { recursive: true });
      fs.writeFileSync(
        statePath,
        JSON.stringify({
          mode: "healthy",
          supervised: false,
          updated_at: "2025-06-01T12:00:00Z",
          health_timeout_seconds: 150,
          services: {
            backend: {
              status: "healthy",
              endpoint: "http://0.0.0.0:9090/health",
            },
          },
        }),
      );

      let probedUrl = "";
      const hc = {
        probeHTTP: (url: string, _timeoutMs: number) =>
          Effect.sync(() => {
            probedUrl = url;
            return { healthy: true, detail: "ok" };
          }),
        waitForHealthy: () => Effect.succeed({ healthy: true, detail: "ok" }),
        probeProcess: () => Effect.succeed({ running: true, detail: "ok" }),
        probeHealthJSON: (_url: string, _timeoutMs: number) =>
          Effect.succeed({
            ok: true,
            status: "healthy",
            services: { database: "healthy" },
          }),
      };
      const pm = createMockPM(mockProc, mockState);

      await runOrch(
        Effect.gen(function* () {
          const orch = yield* GatewayOrchestrator;
          return yield* orch.status();
        }),
        home,
        pm,
        hc,
      );

      expect(probedUrl).toBe("http://127.0.0.1:9090/health");
    });
  });

  // -----------------------------------------------------------------------
  // cleanup
  // -----------------------------------------------------------------------

  describe("cleanup", () => {
    it("calls cleanupStalePIDs for known services", async () => {
      const pm = createMockPM(mockProc, mockState);
      const hc = createMockHC(true);

      await runOrch(
        Effect.gen(function* () {
          const orch = yield* GatewayOrchestrator;
          return yield* orch.cleanup();
        }),
        home,
        pm,
        hc,
      );

      expect(mockState.cleanupCalls.length).toBe(1);
      expect(mockState.cleanupCalls[0]).toEqual([
        "backend",
        "ccxt",
        "telegram",
      ]);
    });
  });
});
