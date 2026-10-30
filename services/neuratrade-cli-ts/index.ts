export {};

const argv = process.argv.slice(2);
// The readiness command bypasses the gateway boot path so it can run on a
// worker/box without the full service graph. Match its subcommand wherever it
// sits after `scalp` (e.g. `scalp --json real-money-readiness`) instead of
// only at argv[1]: any argv deviation used to fall through to the full
// gateway boot with no signal.
const READINESS_SUBCOMMAND = "real-money-readiness";
const readinessIndex =
  argv[0] === "scalp" ? argv.indexOf(READINESS_SUBCOMMAND) : -1;
const isReadinessCommand = readinessIndex > 0;

if (isReadinessCommand) {
  const {
    helpText,
    runRealMoneyReadiness,
    serializeReadinessResult,
    versionText,
  } = await import("./src/cli/real-money-readiness.ts");
  // Flags before the subcommand belong to the same command; keep them.
  const commandArgs = [
    ...argv.slice(1, readinessIndex),
    ...argv.slice(readinessIndex + 1),
  ];

  if (commandArgs.includes("--help") || commandArgs.includes("-h")) {
    console.log(helpText());
  } else if (commandArgs.includes("--version")) {
    console.log(versionText());
  } else {
    const result = runRealMoneyReadiness(commandArgs);
    console.log(serializeReadinessResult(result));
    process.exitCode = result.exitCode;
  }
} else {
  const { BunFileSystem, BunRuntime, BunServices } =
    await import("@effect/platform-bun");
  const { Config, Console, Effect, Layer } = await import("effect");
  const { rootCommand } = await import("./src/cli/index.ts");
  const { runEffect } = await import("./src/cli/kit/kit.ts");
  const { PathLive } = await import("./src/services/path.ts");
  const { ConfigLive } = await import("./src/services/config.ts");
  const { LoggerLive } = await import("./src/services/logger.ts");
  const { PidFileLive } = await import("./src/services/pid.ts");
  const { HealthCheckLive } = await import("./src/services/health-check.ts");
  const { GatewayStateLive } = await import("./src/services/gateway-state.ts");
  const { ProcessManagerLive } =
    await import("./src/services/process-manager.ts");
  const { GatewayOrchestratorLive } =
    await import("./src/services/gateway-orchestrator.ts");
  const { ApiClientLive } = await import("./src/services/api-client.ts");
  const { BitgetClientLiveConfig } =
    await import("./src/services/bitget-client.ts");
  const { BitgetConfigLive } = await import("./src/services/bitget-config.ts");
  const { RateLimiterLive } = await import("./src/services/rate-limiter.ts");

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

  const envOrEmpty = (name: string) =>
    Config.String(name).pipe(Config.withDefault(""));

  const program = Effect.gen(function* () {
    const serverPort = yield* envOrEmpty("SERVER_PORT");
    const apiBaseUrlEnv = yield* envOrEmpty("NEURATRADE_API_BASE_URL");
    const apiKeyEnv = yield* envOrEmpty("NEURATRADE_API_KEY");
    const adminApiKeyEnv = yield* envOrEmpty("ADMIN_API_KEY");
    const homeEnv = yield* envOrEmpty("NEURATRADE_HOME");

    // @codebuff fix 175: only SERVER_PORT is read for the backend port.
    // The generic PORT env var is set by dev servers / CI / container
    // runtimes for unrelated reasons; falling back to it silently pointed
    // the ApiClient at an unrelated port while the backend listens on
    // SERVER_PORT's value (default 8080).
    const port = serverPort || "8080";
    const baseUrl = apiBaseUrlEnv || `http://localhost:${port}`;
    const apiKey = apiKeyEnv || adminApiKeyEnv || "";
    const home = homeEnv || undefined;

    // @codebuff fix 165: an empty API key is legitimate (the backend
    // disables guarded endpoints when ADMIN_API_KEY is unset) but it must
    // not surface later as opaque 401/403s on the first guarded call.
    if (apiKey === "") {
      yield* Console.error(
        "warning: no NEURATRADE_API_KEY / ADMIN_API_KEY set — authenticated API calls will fail until the client key is configured",
      );
    }

    const rootLayer = buildRootLayer({
      home,
      apiBaseUrl: baseUrl,
      apiKey,
    });
    return yield* runEffect(rootCommand, process.argv.slice(2), cliConfig).pipe(
      Effect.provide(rootLayer),
    );
  });

  BunRuntime.runMain(program);
}
