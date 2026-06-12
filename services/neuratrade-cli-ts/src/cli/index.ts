import { Command } from "@effect/cli";
import { Console, Effect } from "effect";

const defaultCommand = Command.make(
  "neuratrade",
  {},
  () => Console.log("NeuraTrade CLI (TypeScript/Effect-TS port)")
).pipe(Command.withDescription("NeuraTrade gateway and control CLI"));

export const rootCommand = defaultCommand;
