import { Command } from "./kit/kit.ts";
import { Console, Effect } from "effect";
import { ApiClient } from "../services/api-client.ts";

export const healthCommand = Command.make("health", {}, () =>
  Effect.gen(function* () {
    const client = yield* ApiClient;
    const result = yield* client
      .health()
      .pipe(
        Effect.catch((err) =>
          Effect.fail(
            new Error(`Could not reach API: ${err._tag} — ${String(err)}`),
          ),
        ),
      );

    yield* Console.log("Health Check Results");
    yield* Console.log("===================");
    const icon =
      result.status === "healthy" || result.status === "ok" ? "✓" : "⚠️";
    yield* Console.log(`${icon} Backend API: ${result.status}`);
    if (result.services) {
      yield* Console.log("Service Health:");
      for (const [name, svc] of Object.entries(result.services)) {
        const svcIcon = svc === "healthy" || svc === "ok" ? "✓" : "⚠️";
        yield* Console.log(`  ${svcIcon} ${name}: ${svc}`);
      }
    }
    if (result.timestamp) {
      yield* Console.log(`Checked at: ${result.timestamp}`);
    }
  }).pipe(
    Effect.catch((err) =>
      Console.log(`❌ ${err.message}`).pipe(
        Effect.flatMap(() => Effect.fail(err)),
      ),
    ),
  ),
).pipe(Command.withDescription("Check system health"));
