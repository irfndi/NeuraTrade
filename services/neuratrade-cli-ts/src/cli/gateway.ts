import { Command, Options } from "@effect/cli";
import { BunContext } from "@effect/platform-bun";
import { Console, Effect, Layer } from "effect";
import { PathLive } from "../services/path.ts";
import { ConfigLive, resolvedConfigEffect } from "../services/config.ts";
import { LoggerLive } from "../services/logger.ts";
import { PidFileLive } from "../services/pid.ts";
import { HealthCheckLive } from "../services/health-check.ts";
import { GatewayStateLive } from "../services/gateway-state.ts";
import { ProcessManagerLive } from "../services/process-manager.ts";
import {
  GatewayOrchestrator,
  GatewayOrchestratorLive,
} from "../services/gateway-orchestrator.ts";

const supervisedFlag = Options.boolean("supervised").pipe(
  Options.withAlias("s"),
  Options.withDescription(
    "Keep gateway running while backend/telegram warm up even if initial health probe fails",
  ),
  Options.withDefault(false),
);

function makeLayer(home?: string) {
  const base = Layer.mergeAll(BunContext.layer, PathLive(home), LoggerLive);
  const config = Layer.provide(ConfigLive(home), base);
  const pidFile = Layer.provide(PidFileLive, base);
  const health = HealthCheckLive;
  const gwState = Layer.provide(GatewayStateLive, base);
  const pm = Layer.provide(ProcessManagerLive, Layer.merge(pidFile, base));
  const deps = Layer.mergeAll(config, pidFile, health, gwState, pm);
  const orch = Layer.provide(GatewayOrchestratorLive, deps);
  return Layer.provide(Layer.merge(orch, deps), base);
}

const startCommand = Command.make(
  "start",
  { supervised: supervisedFlag },
  ({ supervised }) =>
    Effect.gen(function* () {
      const home = process.env.NEURATRADE_HOME;
      const config = yield* resolvedConfigEffect(home);
      const orch = yield* GatewayOrchestrator;
      const result = yield* orch.start({ supervised, config });

      if (result.mode === "healthy") {
        yield* Console.log("🎉 All services started successfully!");
      } else {
        yield* Console.log(
          `⏳ Services started in warmup mode (supervised). Backend PID: ${result.backendPid}`,
        );
      }
      yield* Console.log(
        `📡 Backend API: http://localhost:${config.server.port}`,
      );
      yield* Console.log(
        `🏥 Health Check: http://localhost:${config.server.port}/health`,
      );
    }).pipe(
      Effect.catchAll((err) =>
        Console.log(`❌ Gateway start failed: ${err.message}`).pipe(
          Effect.flatMap(() => Effect.fail(err)),
        ),
      ),
    ),
).pipe(Command.withDescription("Start all NeuraTrade services"));

const stopCommand = Command.make(
  "stop",
  {},
  () =>
    Effect.gen(function* () {
      const orch = yield* GatewayOrchestrator;
      const result = yield* orch.stop();
      if (result.stoppedCount === 0) {
        yield* Console.log("No running services found.");
      } else {
        yield* Console.log(`✅ Stopped ${result.stoppedCount} service(s)`);
      }
    }),
).pipe(Command.withDescription("Stop all NeuraTrade services"));

const statusCommand = Command.make(
  "status",
  {},
  () =>
    Effect.gen(function* () {
      const orch = yield* GatewayOrchestrator;
      const result = yield* orch.status();
      yield* Console.log("📊 NeuraTrade Service Status");
      yield* Console.log("============================");
      yield* Console.log(`Runtime Mode: ${result.mode.toUpperCase()}`);
      yield* Console.log(`Supervised: ${result.supervised}`);
      if (result.updatedAt) {
        yield* Console.log(`Last Update: ${result.updatedAt}`);
      }
      yield* Console.log("");
      for (const [name, svc] of Object.entries(result.services)) {
        yield* Console.log(
          `  ${name}: ${svc.status}${svc.detail ? ` — ${svc.detail}` : ""}`,
        );
      }
      yield* Console.log("");
      yield* Console.log(
        result.backendHealth.healthy
          ? `✅ Backend Health: ${result.backendHealth.detail}`
          : `⚠️  Backend Health: ${result.backendHealth.detail}`,
      );
    }),
).pipe(Command.withDescription("Show service status"));

export const gatewayCommand = Command.make(
  "gateway",
  {},
  () =>
    Console.log(
      "Use 'gateway start', 'gateway stop', or 'gateway status'.",
    ),
).pipe(
  Command.withDescription("Manage NeuraTrade gateway (start/stop/status)"),
  Command.withSubcommands([startCommand, stopCommand, statusCommand]),
);
