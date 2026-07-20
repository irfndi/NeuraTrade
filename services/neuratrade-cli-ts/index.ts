import { BunFileSystem, BunRuntime, BunServices } from "@effect/platform-bun";
import { Config, Effect, Layer } from "effect";
import { rootCommand } from "./src/cli/index.ts";
import { runEffect } from "./src/cli/kit/kit.ts";
import { PathLive } from "./src/services/path.ts";
import { ConfigLive } from "./src/services/config.ts";
import { LoggerLive } from "./src/services/logger.ts";
import { PidFileLive } from "./src/services/pid.ts";
import { HealthCheckLive } from "./src/services/health-check.ts";
import { GatewayStateLive } from "./src/services/gateway-state.ts";
import { ProcessManagerLive } from "./src/services/process-manager.ts";
import { GatewayOrchestratorLive } from "./src/services/gateway-orchestrator.ts";
import { ApiClientLive } from "./src/services/api-client.ts";
import {
  BitgetClient,
  BitgetClientLiveConfig,
} from "./src/services/bitget-client.ts";
import { BitgetConfigLive } from "./src/services/bitget-config.ts";
import { RateLimiterLive } from "./src/services/rate-limiter.ts";

const cliConfig = {
  name: "NeuraTrade CLI",
  version: "v1.0.0",
} as const;

function buildApiClientLayer(baseUrl: string, apiKey: string) {
  return ApiClientLive(baseUrl, apiKey);
}

function buildRootLayer(options: {
  readonly home?: string;
  readonly apiBaseUrl: string;
  readonly apiKey: string;
}) {
  const home = options.home;
  const base = Layer.mergeAll(
    BunServices.layer,
    BunFileSystem.layer,
    PathLive(home),
    LoggerLive,
    BitgetConfigLive,
  );
  const pidFile = Layer.provide(PidFileLive, base);
  const withPidFile = Layer.merge(base, pidFile);
  const pm = Layer.provide(ProcessManagerLive, withPidFile);
  const config = Layer.provide(ConfigLive(home), base);
  const gwState = Layer.provide(GatewayStateLive, base);
  const health = HealthCheckLive;
  const apiClient = buildApiClientLayer(options.apiBaseUrl, options.apiKey);
  const bitgetClient = BitgetClientLiveConfig.pipe(
    Layer.provide(RateLimiterLive()),
  );

  const serviceLayers = Layer.mergeAll(
    config,
    pidFile,
    pm,
    gwState,
    health,
    apiClient,
    bitgetClient,
  );
  const orch = Layer.provide(GatewayOrchestratorLive, serviceLayers);
  const allServices = Layer.mergeAll(serviceLayers, orch);

  return Layer.merge(base, Layer.provide(allServices, base));
}

/** Read a single env var through the Config module, defaulting to "" when unset. */
const envOrEmpty = (name: string) =>
  Config.string(name).pipe(Config.withDefault(""));

const program = Effect.gen(function* () {
  // The default ConfigProvider snapshots process.env once at startup, which
  // matches the previous read-once `process.env` behavior.
  const serverPort = yield* envOrEmpty("SERVER_PORT");
  const portEnv = yield* envOrEmpty("PORT");
  const apiBaseUrlEnv = yield* envOrEmpty("NEURATRADE_API_BASE_URL");
  const apiKeyEnv = yield* envOrEmpty("NEURATRADE_API_KEY");
  const adminApiKeyEnv = yield* envOrEmpty("ADMIN_API_KEY");
  const homeEnv = yield* envOrEmpty("NEURATRADE_HOME");

  const port = serverPort || portEnv || "8080";
  const baseUrl = apiBaseUrlEnv || `http://localhost:${port}`;
  const apiKey = apiKeyEnv || adminApiKeyEnv || "";
  const home = homeEnv || undefined;

  const rootLayer = buildRootLayer({ home, apiBaseUrl: baseUrl, apiKey });
  return yield* runEffect(rootCommand, process.argv.slice(2), cliConfig).pipe(
    Effect.provide(rootLayer),
  );
});

BunRuntime.runMain(program);
