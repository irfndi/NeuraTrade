import { Command, Options } from "./kit/kit.ts";
import { BunServices } from "@effect/platform-bun";
import { Console, Effect, Layer } from "effect";
import * as S from "effect/Schema";
import * as fsMod from "node:fs";
import { Path } from "../services/path.ts";
import { MarketDataGatewayLive } from "../market-data/gateways/index.js";
import { defaultRuleRegistry } from "../rules/registry.js";
import {
  SpecError,
  StrategySpecSchema,
  compileStrategySpec,
  type StrategySpec,
} from "../rules/spec.js";
import {
  StrategySupervisor,
  StrategySupervisorLive,
} from "../rules/runtime.js";

const fileOption = Options.text("file").pipe(
  Options.withDescription("Path to a strategy spec JSON file"),
);

const specsOption = Options.text("specs").pipe(
  Options.withDescription("Directory containing strategy spec JSON files"),
);

const exchangeOption = Options.text("exchange").pipe(
  Options.withDefault("bybit-futures"),
  Options.withDescription("Exchange for market data"),
);

const timeframeOption = Options.text("timeframe").pipe(
  Options.withDefault("15m"),
  Options.withDescription("Candle timeframe"),
);

const intervalOption = Options.float("interval-ms").pipe(
  Options.withDefault(120_000),
  Options.withDescription("Evaluation interval in ms"),
);

const iterationsOption = Options.integer("iterations").pipe(
  Options.withDefault(1),
  Options.withDescription("Number of iterations (0 = run until stopped)"),
);

const lookbackOption = Options.integer("lookback").pipe(
  Options.withDefault(210),
  Options.withDescription("Candles fetched per symbol"),
);

const ruleModuleOption = Options.text("rule-module").pipe(
  Options.optional,
  Options.withDescription("Custom rule module path to load (repeatable)"),
);

/** Decode one spec from raw JSON text; SpecError carries the cause. */
function decodeSpec(json: string): StrategySpec {
  try {
    return S.decodeUnknownSync(StrategySpecSchema)(JSON.parse(json));
  } catch (error) {
    throw new SpecError(`invalid spec JSON: ${String(error)}`);
  }
}

/** Read the raw JSON text of every *.json file in a directory. */
function readSpecTexts(dir: string): string[] {
  const texts: string[] = [];
  for (const entry of fsMod.readdirSync(dir)) {
    if (!entry.endsWith(".json")) continue;
    texts.push(fsMod.readFileSync(`${dir}/${entry}`, "utf8"));
  }
  return texts;
}

/** Decode every spec in a directory; each file may hold one spec or an array. */
function decodeSpecs(texts: readonly string[]): StrategySpec[] {
  return texts.flatMap((text) => {
    const parsed = decodeSpec(text);
    return Array.isArray(parsed) ? parsed : [parsed];
  });
}

export const rulesListCommand = Command.make("list", {}, () =>
  Effect.gen(function* () {
    yield* Console.log("Registered signal rules:");
    yield* Console.log("");
    for (const rule of defaultRuleRegistry().list()) {
      yield* Console.log(
        `  ${rule.id.padEnd(14)} ${rule.category.padEnd(11)} ${rule.description ?? ""}`,
      );
    }
    yield* Console.log("");
    yield* Console.log(
      "Custom rules: export RuleDefinition[] from a module and pass --rule-module to `spec run`.",
    );
  }),
).pipe(Command.withDescription("List rules available to strategy specs"));

export const specValidateCommand = Command.make(
  "validate",
  { file: fileOption },
  ({ file }) =>
    Effect.gen(function* () {
      const raw = yield* Effect.try({
        try: () => fsMod.readFileSync(file, "utf8"),
        catch: (error) => new SpecError(`cannot read spec: ${String(error)}`),
      });
      const spec = yield* Effect.try({
        try: () => decodeSpec(raw),
        catch: (error) => error as SpecError,
      });
      const config = compileStrategySpec(spec);
      const enabledCount = spec.rules.length;
      const weights = Object.entries(config.weights)
        .filter(([, weight]) => weight > 0)
        .map(([id, weight]) => `${id}=${weight.toFixed(3)}`)
        .join(" ");
      yield* Console.log(`✓ spec "${spec.id}" decodes and compiles`);
      yield* Console.log(`  rules (${enabledCount}): ${weights || "(none)"}`);
      yield* Console.log(
        `  risk: drawdown<=${spec.risk.maxDrawdownPct}% dailyLoss<=${spec.risk.maxDailyLossPct}% position<=${spec.risk.maxPositionSizePct}% minCapital=${spec.risk.minCapital} maxTrades/day=${spec.risk.maxTradesPerDay}`,
      );
      yield* Console.log(
        `  execution: size=${spec.execution.positionSizePct}% atrStops=${spec.execution.useAtrStops} maxBars=${spec.execution.maxBarsInTrade}`,
      );
      if (spec.symbols.length > 0) {
        yield* Console.log(`  symbols: ${spec.symbols.join(", ")}`);
      }
    }),
).pipe(Command.withDescription("Validate a strategy spec JSON file"));

export const specRunCommand = Command.make(
  "run",
  {
    specs: specsOption,
    exchange: exchangeOption,
    timeframe: timeframeOption,
    intervalMs: intervalOption,
    iterations: iterationsOption,
    lookback: lookbackOption,
    ruleModule: ruleModuleOption,
  },
  ({
    specs,
    exchange,
    timeframe,
    intervalMs,
    iterations,
    lookback,
    ruleModule,
  }) =>
    Effect.gen(function* () {
      const path = yield* Path;
      const texts = yield* Effect.try({
        try: () => readSpecTexts(specs),
        catch: (error) =>
          new SpecError(`cannot read specs dir: ${String(error)}`),
      });
      const decoded = yield* Effect.try({
        try: () => decodeSpecs(texts),
        catch: (error) => error as SpecError,
      });
      if (decoded.length === 0) {
        throw new SpecError(`no spec JSON files found in ${specs}`);
      }
      const ruleModules = ruleModule._tag === "Some" ? [ruleModule.value] : [];
      const stateDir = `${path.homeDir}/spec-runs`;

      const base = Layer.mergeAll(BunServices.layer, MarketDataGatewayLive);
      const supervisor = yield* StrategySupervisor.pipe(
        Effect.provide(StrategySupervisorLive),
      );
      const result = yield* supervisor
        .run({
          specs: decoded,
          exchange,
          timeframe,
          intervalMs,
          iterations,
          lookback,
          stateDir,
          ruleModules,
        })
        .pipe(Effect.provide(base));

      yield* Console.log("");
      yield* Console.log(
        `Ran ${result.iterations} iterations across ${decoded.length} spec(s). Attribution:`,
      );
      for (const rule of result.attribution) {
        yield* Console.log(
          `  ${rule.ruleId.padEnd(14)} trades=${rule.alignedTrades} winRate=${rule.winRatePct.toFixed(1)}% expectancy=${rule.expectancyPct.toFixed(3)}%`,
        );
      }
    }),
).pipe(
  Command.withDescription(
    "Run strategy specs in parallel (paper, ledger-simulated fills)",
  ),
);

export const rulesCommand = Command.make("rules", {}, () =>
  Effect.gen(function* () {
    yield* Console.log("Usage: neuratrade rules list | rules spec ...");
  }),
).pipe(
  Command.withDescription("Rule registry and strategy-spec tooling"),
  Command.withSubcommands([
    rulesListCommand,
    specValidateCommand,
    specRunCommand,
  ]),
);
