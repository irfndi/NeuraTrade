import { Command } from "@effect/cli";
import { BunContext } from "@effect/platform-bun";
import { Effect } from "effect";
import { rootCommand } from "./src/cli/index.ts";

const cli = Command.run(rootCommand, {
  name: "NeuraTrade CLI",
  version: "v1.0.0",
});

const program = cli(process.argv).pipe(Effect.provide(BunContext.layer));

Effect.runPromise(program).then(
  () => {
    process.exit(0);
  },
  (err: unknown) => {
    console.error(err);
    process.exit(1);
  }
);
