import { Command, Options } from "@effect/cli";
import { Console, Effect } from "effect";
import { gatewayCommand } from "./gateway.ts";
import { statusCommand } from "./status.ts";
import { healthCommand } from "./health.ts";
import { doctorCommand } from "./doctor.ts";
import { marketCommand } from "./market.ts";
import { scalpCommand } from "./scalp.ts";
import { exchangeCommand } from "./exchange.ts";

export const rootCommand = Command.make(
  "neuratrade",
  {},
  () => Console.log("NeuraTrade CLI (TypeScript/Effect-TS port)"),
).pipe(
  Command.withDescription("NeuraTrade gateway and control CLI"),
  Command.withSubcommands([
    gatewayCommand,
    statusCommand,
    healthCommand,
    doctorCommand,
    marketCommand,
    scalpCommand,
    exchangeCommand,
  ]),
);
