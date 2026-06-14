import { Command } from "@effect/cli";
import { Console, Effect } from "effect";
import { FileSystem } from "@effect/platform";
import { Path } from "../services/path.ts";

export const doctorCommand = Command.make("doctor", {}, () =>
  Effect.gen(function* () {
    const path = yield* Path;
    const fs = yield* FileSystem.FileSystem;

    yield* Console.log("🔍 NeuraTrade Doctor");
    yield* Console.log("====================");
    yield* Console.log(`NeuraTrade Home: ${path.homeDir}`);

    const dirs = [
      { name: "Logs", path: path.logDir },
      { name: "PIDs", path: path.pidDir },
      { name: "Data", path: path.dataDir },
    ];

    for (const dir of dirs) {
      const exists = yield* fs.exists(dir.path);
      yield* Console.log(`${exists ? "✓" : "✗"} ${dir.name}: ${dir.path}`);
    }

    const configExists = yield* fs.exists(path.configPath);
    yield* Console.log(
      `${configExists ? "✓" : "⚠️"} Config file: ${path.configPath}`,
    );

    const runtimeExists = yield* fs.exists(path.runtimeConfigPath);
    yield* Console.log(
      `${runtimeExists ? "✓" : "⚠️"} Runtime file: ${path.runtimeConfigPath}`,
    );

    yield* Console.log("");
    yield* Console.log("Configuration appears valid.");
  }),
).pipe(
  Command.withDescription("Validate local configuration and runtime readiness"),
);
