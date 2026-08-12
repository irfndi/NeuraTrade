import { Command, Options } from "./kit/kit.ts";
import { Console, Effect } from "effect";
import { resolvedConfigEffect } from "../services/config.ts";
import { GatewayOrchestrator } from "../services/gateway-orchestrator.ts";

const supervisedFlag = Options.boolean("supervised").pipe(
  Options.withAlias("s"),
  Options.withDescription(
    "Keep gateway running while backend/telegram warm up even if initial health probe fails",
  ),
  Options.withDefault(false),
);

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
      Effect.catch((err) =>
        Console.log(`❌ Gateway start failed: ${err.message}`).pipe(
          Effect.flatMap(() => Effect.fail(err)),
        ),
      ),
    ),
).pipe(Command.withDescription("Start all NeuraTrade services"));

const stopCommand = Command.make("stop", {}, () =>
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

const statusCommand = Command.make("status", {}, () =>
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

    // Process status (mirror Go CLI)
    for (const [name, proc] of Object.entries(result.processes)) {
      if (proc.running) {
        const pidText = proc.pid ? ` (PID: ${proc.pid})` : "";
        yield* Console.log(`✅ ${name}: Running${pidText}`);
      } else {
        yield* Console.log(`❌ ${name}: Not running`);
      }
    }

    const backendEp = result.services["backend"]?.endpoint;
    if (backendEp) {
      yield* Console.log("");
      yield* Console.log(`🚪 Health Check: ${backendEp}`);
    }
    yield* Console.log("");

    if (result.backendHealth.healthy) {
      yield* Console.log(`✅ Backend Status: ${result.mode}`);
      if (
        result.backendServices &&
        Object.keys(result.backendServices).length > 0
      ) {
        yield* Console.log("");
        yield* Console.log("Service Health:");
        for (const [name, svcStatus] of Object.entries(
          result.backendServices,
        )) {
          const icon =
            svcStatus === "healthy" || svcStatus === "ok" ? "✓" : "⚠️";
          yield* Console.log(`  ${icon} ${name}: ${svcStatus}`);
        }
      }
    } else {
      yield* Console.log(`⚠️  Backend Health: ${result.backendHealth.detail}`);
      if (result.mode === "warming" || result.mode === "degraded") {
        yield* Console.log(
          `Gateway runtime mode is ${result.mode.toUpperCase()} (services may still be warming).`,
        );
      }
      yield* Console.log("");
      yield* Console.log("Make sure the backend is running:");
      yield* Console.log("  neuratrade gateway start");
    }
  }),
).pipe(Command.withDescription("Show service status"));

export const gatewayCommand = Command.make("gateway", {}, () =>
  Console.log("Use 'gateway start', 'gateway stop', or 'gateway status'."),
).pipe(
  Command.withDescription("Manage NeuraTrade gateway (start/stop/status)"),
  Command.withSubcommands([startCommand, stopCommand, statusCommand]),
);
