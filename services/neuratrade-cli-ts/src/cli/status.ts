import { Command } from "./kit/kit.ts";
import { Console, Effect } from "effect";
import { ApiClient } from "../services/api-client.ts";

export const statusCommand = Command.make("status", {}, () =>
  Effect.gen(function* () {
    const client = yield* ApiClient;
    const result = yield* client.health().pipe(
      Effect.catch((err) =>
        Effect.succeed({
          status: "unknown",
          error: err._tag,
          services: {},
          timestamp: "",
        }),
      ),
    );

    yield* Console.log("NeuraTrade System Status");
    yield* Console.log("=======================");
    yield* Console.log(`Status: ${result.status}`);
    if ("services" in result && result.services) {
      yield* Console.log("Connected Services:");
      for (const [name, svc] of Object.entries(result.services)) {
        yield* Console.log(`  - ${name}: ${svc}`);
      }
    }
    if ("timestamp" in result && result.timestamp) {
      yield* Console.log(`Checked at: ${result.timestamp}`);
    }
  }),
).pipe(Command.withDescription("Show NeuraTrade system status"));
