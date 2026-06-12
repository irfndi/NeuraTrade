/**
 * GatewayOrchestrator service — orchestrates starting, stopping, and
 * inspecting NeuraTrade gateway services (backend, CCXT, Telegram).
 *
 * Mirrors the Go gateway CLI logic in cmd/neuratrade-cli/gateway.go:
 *   gatewayStart, gatewayStop, gatewayStatus, cleanupStalePIDs.
 *
 * Does NOT handle OS signal blocking, health monitor loops, or
 * background goroutines — those belong in the CLI command layer.
 */
import * as fs from "fs";
import * as nodePath from "path";
import * as crypto from "crypto";
import { Context, Data, Effect, Layer } from "effect";
import { Path } from "./path.ts";
import { ProcessManager } from "./process-manager.ts";
import { HealthCheck } from "./health-check.ts";
import { GatewayState, type GatewayStateService } from "./gateway-state.ts";
import { Logger } from "./logger.ts";
import type { ResolvedConfig } from "./config.ts";

// ---------------------------------------------------------------------------
// Error type
// ---------------------------------------------------------------------------

export class GatewayOrchestratorError extends Data.TaggedError(
  "GatewayOrchestratorError",
)<{
  readonly message: string;
  readonly cause?: unknown;
}> {}

// ---------------------------------------------------------------------------
// Options & result types
// ---------------------------------------------------------------------------

export interface GatewayStartOptions {
  /** Run in supervised mode (tolerate health failures as "warming"). */
  readonly supervised: boolean;
  /** Fully-resolved configuration (env → runtime → local → defaults). */
  readonly config: ResolvedConfig;
}

export interface StartResult {
  readonly mode: string;
  readonly backendPid: number;
  readonly telegramPid?: number;
  readonly ccxtMode: "native" | "external";
  readonly telegramEnabled: boolean;
}

export interface StopResult {
  readonly stoppedCount: number;
  readonly errors: ReadonlyArray<{
    readonly service: string;
    readonly error: string;
  }>;
}

export interface StatusResult {
  readonly mode: string;
  readonly supervised: boolean;
  readonly updatedAt: string;
  readonly services: Record<
    string,
    { readonly status: string; readonly detail?: string; readonly endpoint?: string }
  >;
  readonly backendHealth: { readonly healthy: boolean; readonly detail: string };
  readonly processes: Record<
    string,
    { readonly running: boolean; readonly pid?: number; readonly detail?: string }
  >;
  readonly backendServices?: Record<string, string>;
}

// ---------------------------------------------------------------------------
// Context.Tag
// ---------------------------------------------------------------------------

export class GatewayOrchestrator extends Context.Tag("GatewayOrchestrator")<
  GatewayOrchestrator,
  {
    readonly start: (
      options: GatewayStartOptions,
    ) => Effect.Effect<StartResult, GatewayOrchestratorError, never>;
    readonly stop: () => Effect.Effect<StopResult, never, never>;
    readonly status: () => Effect.Effect<StatusResult, never, never>;
    readonly cleanup: () => Effect.Effect<void, never, never>;
  }
>() {}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Generate a random hex key if the provided key is too short. */
function normalizeKey(key: string, minLength: number): string {
  const trimmed = key.trim();
  if (trimmed.length >= minLength) return trimmed;
  const bytes = new Uint8Array(24);
  crypto.getRandomValues(bytes);
  return Array.from(bytes)
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");
}

/** Detect external CCXT mode from env vars. */
function isExternalCCXTMode(): boolean {
  const url = process.env.CCXT_SERVICE_URL;
  const grpc = process.env.CCXT_GRPC_ADDRESS;
  return (
    (url !== undefined && url.trim() !== "") ||
    (grpc !== undefined && grpc.trim() !== "")
  );
}

// ---------------------------------------------------------------------------
// Service name constants (mirror Go stop loop)
// ---------------------------------------------------------------------------

const STOP_SERVICES: ReadonlyArray<{
  readonly name: string;
  readonly pidFile: string;
  readonly patterns: ReadonlyArray<string>;
}> = [
  { name: "Backend API", pidFile: "backend", patterns: ["neuratrade-server"] },
  { name: "CCXT Service", pidFile: "ccxt", patterns: ["ccxt-service"] },
  {
    name: "Telegram Service",
    pidFile: "telegram",
    patterns: ["telegram-service", "bun run index.ts"],
  },
];

const CLEANUP_SERVICES = ["backend", "ccxt", "telegram"];

// ---------------------------------------------------------------------------
// Layer
// ---------------------------------------------------------------------------

export const GatewayOrchestratorLive: Layer.Layer<
  GatewayOrchestrator,
  never,
  Path | ProcessManager | HealthCheck | GatewayStateService | Logger
> = Layer.effect(
  GatewayOrchestrator,
  Effect.gen(function* () {
    const path = yield* Path;
    const pm = yield* ProcessManager;
    const hc = yield* HealthCheck;
    const gwState = yield* GatewayState;
    const logger = yield* Logger;

    // ---- start ----
    const start = (
      options: GatewayStartOptions,
    ): Effect.Effect<StartResult, GatewayOrchestratorError, never> =>
      Effect.gen(function* () {
        const { supervised, config } = options;

        // Ensure runtime directories
        yield* Effect.sync(() => {
          fs.mkdirSync(path.logDir, { recursive: true });
          fs.mkdirSync(path.pidDir, { recursive: true });
          fs.mkdirSync(path.dataDir, { recursive: true });
        });

        // Resolve ports and host from config
        const backendPort = String(config.server.port);
        const bindHost = config.gateway.bind_host;
        const ccxtPort = String(config.gateway.ccxt_port);
        const telegramPort = String(config.gateway.telegram_port);
        const telegramGRPCPort = String(config.gateway.telegram_grpc_port);
        const healthTimeoutMs = config.gateway.health_timeout_seconds * 1000;
        const signalTimeoutMs = config.gateway.signal_timeout_seconds * 1000;

        // CCXT mode
        const ccxtMode: "native" | "external" = isExternalCCXTMode()
          ? "external"
          : "native";

        // Telegram enabled?
        const telegramToken = config.telegram_bot_token;
        const telegramEnabled =
          telegramToken.trim() !== "" && !config.gateway.skip_telegram;

        // Normalize secrets
        const adminAPIKey = normalizeKey(config.admin_api_key, 32);
        const jwtSecret = normalizeKey(config.jwt_secret, 32);

        // SQLite path
        const sqlitePath =
          config.database.sqlite_path ||
          nodePath.join(path.dataDir, "neuratrade.db");

        // Build backend env map (mirrors Go backendEnv)
        const backendEnv: Record<string, string> = {
          PORT: backendPort,
          SERVER_PORT: backendPort,
          BACKEND_HOST_PORT: backendPort,
          HOST: "0.0.0.0",
          DATABASE_DRIVER: config.database.driver,
          SQLITE_PATH: sqlitePath,
          SQLITE_DB_PATH: sqlitePath,
          REDIS_HOST: config.redis.host,
          REDIS_PORT: String(config.redis.port),
          TELEGRAM_SERVICE_URL: `http://${bindHost}:${telegramPort}`,
          TELEGRAM_GRPC_ADDRESS: `${bindHost}:${telegramGRPCPort}`,
          JWT_SECRET: jwtSecret,
          ADMIN_API_KEY: adminAPIKey,
          SENTRY_ENVIRONMENT: process.env.SENTRY_ENVIRONMENT ?? "production",
          SENTRY_DSN: process.env.SENTRY_DSN ?? "",
          AI_API_KEY: config.ai_api_key,
          AI_BASE_URL: config.ai.base_url ?? "",
          AI_PROVIDER: config.ai.provider,
          AI_MODEL: config.ai.model,
          FEATURES_ENABLE_AI: String(config.features.enable_ai),
          ENABLE_AI_SIGNALS: String(config.features.enable_ai_signals),
          ENABLE_AI_ARBITRAGE: String(config.features.enable_ai_arbitrage),
          FEATURES_PAPER_TRADING: String(config.features.paper_trading),
          FEATURES_REAL_TRADING: String(config.features.real_trading),
        };

        // Endpoints
        const backendEndpoint = `http://${bindHost}:${backendPort}/health`;
        const ccxtEndpoint = `http://${bindHost}:${ccxtPort}/health`;
        const telegramEndpoint = `http://${bindHost}:${telegramPort}/health`;

        // Write initial gateway state
        yield* gwState.write({
          mode: "starting",
          supervised,
          updated_at: new Date().toISOString(),
          health_timeout_seconds: config.gateway.health_timeout_seconds,
          services: {
            backend: { status: "starting", endpoint: backendEndpoint },
            ccxt: { status: "starting", endpoint: ccxtEndpoint },
            ...(telegramEnabled
              ? {
                  telegram: {
                    status: "starting",
                    endpoint: telegramEndpoint,
                  },
                }
              : {
                  telegram: {
                    status: "disabled",
                    detail: "Telegram disabled for paper-only runtime",
                  },
                }),
          },
        });

        // Start Backend API
        yield* logger.info("Starting Backend API");
        const backendBinary = yield* pm
          .resolveServiceBinary("neuratrade-server")
          .pipe(
            Effect.mapError(
              (err) =>
                new GatewayOrchestratorError({
                  message: `Failed to resolve backend binary: ${err.message}`,
                  cause: err,
                }),
            ),
          );

        const backendProc = yield* pm
          .startService(
            backendBinary,
            "Backend API",
            nodePath.join(path.logDir, "backend.log"),
            backendEnv,
            "backend",
          )
          .pipe(
            Effect.mapError(
              (err) =>
                new GatewayOrchestratorError({
                  message: `Failed to start backend: ${err.message}`,
                  cause: err,
                }),
            ),
          );

        // Probe backend health
        const backendProbe = yield* hc.waitForHealthy(
          backendEndpoint,
          healthTimeoutMs,
        );

        if (backendProbe.healthy) {
          yield* gwState.writeServiceState(
            "backend",
            "healthy",
            backendProbe.detail,
            backendEndpoint,
          );
        } else if (supervised) {
          yield* gwState.writeServiceState(
            "backend",
            "warming",
            backendProbe.detail,
            backendEndpoint,
          );
          yield* gwState.writeMode("warming", "backend warming up");
        } else {
          yield* pm.signalAndWait(
            backendProc,
            "SIGTERM",
            signalTimeoutMs,
          );
          yield* gwState.writeServiceState(
            "backend",
            "down",
            backendProbe.detail,
            backendEndpoint,
          );
          yield* gwState.markStopped("backend health check failed");
          return yield* Effect.fail(
            new GatewayOrchestratorError({
              message: backendProbe.detail,
            }),
          );
        }

        // Telegram
        let telegramPid: number | undefined;
        let telegramProbe: { readonly healthy: boolean; readonly detail: string } | undefined;
        if (telegramEnabled) {
          yield* logger.info("Starting Telegram Service");
          const telegramBinary = yield* pm
            .resolveServiceBinary("telegram-service")
            .pipe(
              Effect.mapError(
                (err) =>
                  new GatewayOrchestratorError({
                    message: `Failed to resolve telegram binary: ${err.message}`,
                    cause: err,
                  }),
              ),
            );

          const telegramProc = yield* pm
            .startService(
              telegramBinary,
              "Telegram Service",
              nodePath.join(path.logDir, "telegram.log"),
              {
                PORT: telegramPort,
                BIND_HOST: bindHost,
                TELEGRAM_BOT_TOKEN: telegramToken,
                TELEGRAM_USE_POLLING: String(config.telegram.use_polling),
                TELEGRAM_API_BASE_URL: `http://${bindHost}:${backendPort}`,
                BACKEND_HOST_PORT: backendPort,
                NODE_ENV: "production",
                ADMIN_API_KEY: adminAPIKey,
              },
              "telegram",
            )
            .pipe(
              Effect.mapError(
                (err) =>
                  new GatewayOrchestratorError({
                    message: `Failed to start telegram: ${err.message}`,
                    cause: err,
                  }),
              ),
            );

          telegramPid = telegramProc.pid;

          telegramProbe = yield* hc.waitForHealthy(
            telegramEndpoint,
            healthTimeoutMs,
          );

          if (telegramProbe.healthy) {
            yield* gwState.writeServiceState(
              "telegram",
              "healthy",
              telegramProbe.detail,
              telegramEndpoint,
            );
          } else if (supervised) {
            yield* gwState.writeServiceState(
              "telegram",
              "warming",
              telegramProbe.detail,
              telegramEndpoint,
            );
            yield* gwState.writeMode("warming", "telegram warming up");
          } else {
            yield* pm.signalAndWait(
              telegramProc,
              "SIGTERM",
              signalTimeoutMs,
            );
            yield* pm.signalAndWait(
              backendProc,
              "SIGTERM",
              signalTimeoutMs,
            );
            yield* gwState.writeServiceState(
              "telegram",
              "down",
              telegramProbe.detail,
              telegramEndpoint,
            );
            yield* gwState.markStopped("telegram health check failed");
            return yield* Effect.fail(
              new GatewayOrchestratorError({
                message: telegramProbe.detail,
              }),
            );
          }
        } else {
          yield* gwState.writeServiceState(
            "telegram",
            "disabled",
            "Telegram disabled for paper-only runtime",
          );
        }

        // CCXT state
        if (ccxtMode === "native") {
          yield* gwState.writeServiceState(
            "ccxt",
            "embedded",
            "native mode (embedded in backend)",
          );
        } else {
          yield* gwState.writeServiceState(
            "ccxt",
            "external",
            "external endpoint configured",
          );
        }

        // Final mode
        const anyWarming =
          (!backendProbe.healthy && (supervised || telegramEnabled)) ||
          (telegramEnabled && telegramProbe && !telegramProbe.healthy && supervised);
        const initialMode = anyWarming ? "warming" : "healthy";
        yield* gwState.writeMode(initialMode, "services started");

        return {
          mode: initialMode,
          backendPid: backendProc.pid,
          telegramPid,
          ccxtMode,
          telegramEnabled,
        };
      });

    // ---- stop ----
    const stop = (): Effect.Effect<StopResult, never, never> =>
      Effect.gen(function* () {
        let stoppedCount = 0;
        const errors: Array<{ service: string; error: string }> = [];

        for (const svc of STOP_SERVICES) {
          const result = yield* pm
            .stopServiceByPIDFile(svc.name, svc.pidFile, svc.patterns)
            .pipe(
              Effect.map(() => ({ success: true as const })),
              Effect.catchAll((err) =>
                Effect.succeed({
                  success: false as const,
                  error: err.message,
                }),
              ),
            );
          if (result.success) {
            stoppedCount++;
          } else {
            errors.push({ service: svc.name, error: result.error });
          }
        }

        if (stoppedCount === 0) {
          yield* gwState.markStopped("gateway stop found no running services");
        } else {
          yield* gwState.markStopped("gateway stopped");
        }

        return { stoppedCount, errors };
      });

    // ---- status ----
    const status = (): Effect.Effect<StatusResult, never, never> =>
      Effect.gen(function* () {
        const state = yield* gwState.read();

        // Determine probe host (mirror Go: if 0.0.0.0 or ::, probe 127.0.0.1)
        let probeHost = "127.0.0.1";
        let backendPort = "8080";
        const backendEp = state.services["backend"]?.endpoint;
        if (backendEp) {
          try {
            const parsed = new URL(backendEp);
            probeHost = parsed.hostname;
            backendPort = parsed.port || "8080";
          } catch {
            // ignore — use defaults
          }
        }
        if (probeHost === "0.0.0.0" || probeHost === "::") {
          probeHost = "127.0.0.1";
        }

        const healthUrl = `http://${probeHost}:${backendPort}/health`;
        const [healthResult, healthJSON] = yield* Effect.all(
          [hc.probeHTTP(healthUrl, 5000), hc.probeHealthJSON(healthUrl, 5000)],
          { concurrency: 2 },
        );

        // Process checks for each managed service (mirror Go checkProcess)
        const processes: StatusResult["processes"] = {};
        for (const svc of STOP_SERVICES) {
          const pidFilePath = nodePath.join(path.pidDir, `${svc.pidFile}.pid`);
          let pid: number | undefined;
          try {
            const content = fs.readFileSync(pidFilePath, "utf8").trim();
            pid = Number(content);
            if (Number.isNaN(pid)) pid = undefined;
          } catch {
            pid = undefined;
          }

          let running = false;
          let detail = "no processes found";
          for (const pattern of svc.patterns) {
            const probe = yield* hc.probeProcess(pattern);
            if (probe.running) {
              running = true;
              detail = probe.detail;
              break;
            }
          }

          processes[svc.name] = {
            running,
            pid,
            detail,
          };
        }

        return {
          mode: state.mode,
          supervised: state.supervised,
          updatedAt: state.updated_at,
          services: state.services,
          backendHealth: {
            healthy: healthResult.healthy,
            detail: healthResult.detail,
          },
          processes,
          backendServices: healthJSON.ok ? healthJSON.services : undefined,
        };
      });

    // ---- cleanup ----
    const cleanup = (): Effect.Effect<void, never, never> =>
      pm.cleanupStalePIDs(CLEANUP_SERVICES);

    return { start, stop, status, cleanup };
  }),
);
