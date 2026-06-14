import { Command } from "@effect/cli";
import { BunContext, BunFileSystem } from "@effect/platform-bun";
import { Effect, Layer } from "effect";
import { rootCommand } from "./src/cli/index.ts";
import { PathLive } from "./src/services/path.ts";
import { ConfigLive } from "./src/services/config.ts";
import { LoggerLive } from "./src/services/logger.ts";
import { PidFileLive } from "./src/services/pid.ts";
import { HealthCheckLive } from "./src/services/health-check.ts";
import { GatewayStateLive } from "./src/services/gateway-state.ts";
import { ProcessManagerLive } from "./src/services/process-manager.ts";
import { GatewayOrchestratorLive } from "./src/services/gateway-orchestrator.ts";
import { ApiClientLive } from "./src/services/api-client.ts";
import { SqliteClientLive } from "./src/services/sqlite.ts";
import { MarketRepositoryLive } from "./src/services/market-repository.ts";
import { RateLimiterLive } from "./src/services/rate-limiter.ts";
import { BinanceClientLive } from "./src/services/binance-client.ts";
import { BitgetConfigLive } from "./src/services/bitget-config.ts";
import { BitgetClientLiveConfig } from "./src/services/bitget-client.ts";
import { PaperRepositoryLive } from "./src/services/paper-repository.ts";
import { PaperTradingEngineLive } from "./src/services/paper-trading-engine.ts";

const cli = Command.run(rootCommand, {
  name: "NeuraTrade CLI",
  version: "v1.0.0",
});

function buildApiClientLayer() {
  const port = process.env.SERVER_PORT || process.env.PORT || "8080";
  const baseUrl =
    process.env.NEURATRADE_API_BASE_URL || `http://localhost:${port}`;
  const apiKey =
    process.env.NEURATRADE_API_KEY || process.env.ADMIN_API_KEY || "";
  return ApiClientLive(baseUrl, apiKey);
}

function buildRootLayer() {
  const home = process.env.NEURATRADE_HOME;
  const base = Layer.mergeAll(
    BunContext.layer,
    BunFileSystem.layer,
    PathLive(home),
    LoggerLive,
  );
  const pidFile = Layer.provide(PidFileLive, base);
  const withPidFile = Layer.merge(base, pidFile);
  const pm = Layer.provide(ProcessManagerLive, withPidFile);
  const config = Layer.provide(ConfigLive(home), base);
  const gwState = Layer.provide(GatewayStateLive, base);
  const health = HealthCheckLive;
  const apiClient = buildApiClientLayer();
  const sqlite = Layer.provide(SqliteClientLive, Layer.merge(base, config));
  const marketRepository = Layer.provide(MarketRepositoryLive, sqlite);
  const paperRepository = Layer.provide(PaperRepositoryLive, sqlite);
  const rateLimiter = RateLimiterLive();
  const binanceClient = Layer.provide(BinanceClientLive(), rateLimiter);
  const bitgetConfig = BitgetConfigLive;
  const bitgetClient = Layer.provide(
    BitgetClientLiveConfig,
    Layer.merge(rateLimiter, bitgetConfig),
  );

  const serviceLayers = Layer.mergeAll(
    config,
    pidFile,
    pm,
    gwState,
    health,
    apiClient,
    sqlite,
    marketRepository,
    paperRepository,
    rateLimiter,
    binanceClient,
    bitgetConfig,
    bitgetClient,
    PaperTradingEngineLive,
  );
  const orch = Layer.provide(GatewayOrchestratorLive, serviceLayers);
  const allServices = Layer.mergeAll(serviceLayers, orch);

  return Layer.merge(base, Layer.provide(allServices, base));
}

const program = cli(process.argv).pipe(Effect.provide(buildRootLayer()));

Effect.runPromise(program).then(
  () => {
    process.exit(0);
  },
  (err: unknown) => {
    console.error(err);
    process.exit(1);
  },
);
