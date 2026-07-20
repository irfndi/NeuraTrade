import { describe, expect, it } from "bun:test";
import * as fc from "fast-check";
import { Effect, Option } from "effect";
import {
  Command,
  Options,
  evaluate,
  runEffect,
  type AnyCommand,
  type CliConfig,
} from "./kit.ts";

const config: CliConfig = { name: "Test CLI", version: "v0.0.0-test" };

function leafCommand(
  name: string,
  // biome-ignore lint/suspicious/noExplicitAny: test helper
  options: any,
  // biome-ignore lint/suspicious/noExplicitAny: test helper
  handler: (args: any) => any = () => "ok",
): AnyCommand {
  return Command.make(name, options, (args) =>
    Effect.sync(() => handler(args)),
  ) as AnyCommand;
}

function runOutcome(cmd: AnyCommand, argv: ReadonlyArray<string>) {
  const outcome = evaluate(cmd, argv, config);
  if (outcome._tag !== "execute") {
    throw new Error(`expected execute outcome, got ${outcome._tag}`);
  }
  return Effect.runPromise(outcome.run() as Effect.Effect<any>);
}

describe("kit: option parsing", () => {
  it("parses --opt value (space-separated)", async () => {
    const cmd = leafCommand("t", { symbol: Options.text("symbol") });
    expect(await runOutcome(cmd, ["--symbol", "BTC/USDT"])).toBe("ok");
  });

  it("parses --opt=value (inline)", async () => {
    let seen: unknown;
    const cmd = leafCommand(
      "t",
      { symbol: Options.text("symbol") },
      (args) => (seen = args.symbol),
    );
    const outcome = evaluate(cmd, ["--symbol=BTC/USDT"], config);
    expect(outcome._tag).toBe("execute");
    if (outcome._tag === "execute")
      await Effect.runPromise(outcome.run() as Effect.Effect<any>);
    expect(seen).toBe("BTC/USDT");
  });

  it("coerces integers and rejects non-integers", async () => {
    let seen: unknown;
    const cmd = leafCommand("t", { count: Options.integer("count") }, (a) => {
      seen = a.count;
      return a.count;
    });
    expect(await runOutcome(cmd, ["--count", "42"])).toBe(42);
    expect(seen).toBe(42);
    const bad = evaluate(cmd, ["--count", "4.2"], config);
    expect(bad._tag).toBe("error");
    if (bad._tag === "error") {
      expect(bad.message).toContain("--count");
      expect(bad.message).toContain("integer");
    }
  });

  it("coerces negative numbers", async () => {
    const result = await runOutcome(
      leafCommand("t", { fee: Options.float("fee") }, (a) => a.fee),
      ["--fee", "-1.5"],
    );
    expect(result).toBe(-1.5);
  });

  it("coerces floats and rejects non-numbers", () => {
    const cmd = leafCommand("t", { fee: Options.float("fee") });
    const bad = evaluate(cmd, ["--fee", "abc"], config);
    expect(bad._tag).toBe("error");
    if (bad._tag === "error") expect(bad.message).toContain("--fee");
  });

  it("boolean flags default to false and flip to true", async () => {
    expect(
      await runOutcome(
        leafCommand("t", { verbose: Options.boolean("verbose") }, (a) => a),
        [],
      ),
    ).toEqual({ verbose: false });
    expect(
      await runOutcome(
        leafCommand("t", { verbose: Options.boolean("verbose") }, (a) => a),
        ["--verbose"],
      ),
    ).toEqual({ verbose: true });
  });

  it("boolean flags accept inline =true / =false", async () => {
    expect(
      await runOutcome(
        leafCommand("t", { verbose: Options.boolean("verbose") }, (a) => a),
        ["--verbose=false"],
      ),
    ).toEqual({ verbose: false });
  });

  it("resolves aliases", async () => {
    const opt = Options.boolean("supervised").pipe(Options.withAlias("s"));
    expect(
      await runOutcome(
        leafCommand("t", { supervised: opt }, (a) => a),
        ["-s"],
      ),
    ).toEqual({ supervised: true });
  });

  it("applies defaults when flags are absent", async () => {
    const args = await runOutcome(
      leafCommand(
        "t",
        {
          exchange: Options.text("exchange").pipe(
            Options.withDefault("binance"),
          ),
          days: Options.integer("days").pipe(Options.withDefault(365)),
        },
        (a) => a,
      ),
      [],
    );
    expect(args).toEqual({ exchange: "binance", days: 365 });
  });

  it("errors on missing required options", () => {
    const cmd = leafCommand("ticker", { symbol: Options.text("symbol") });
    const outcome = evaluate(cmd, [], config);
    expect(outcome._tag).toBe("error");
    if (outcome._tag === "error") {
      expect(outcome.message).toContain("--symbol");
      expect(outcome.message).toContain("ticker");
    }
  });

  it("optional options yield Option.none / Option.some", async () => {
    const cmd = leafCommand(
      "t",
      { start: Options.text("start").pipe(Options.optional) },
      (a) => a.start,
    );
    const none = await runOutcome(cmd, []);
    expect(Option.isNone(none as Option.Option<string>)).toBe(true);
    const some = await runOutcome(cmd, ["--start", "2025-06-01"]);
    if (Option.isSome(some as Option.Option<string>)) {
      expect((some as Option.Some<string>).value).toBe("2025-06-01");
    } else {
      expect.unreachable("expected Option.some");
    }
  });

  it("validates choices and reports allowed values", async () => {
    const opt = Options.choice("mode", ["a", "b"] as const);
    const ok = await runOutcome(
      leafCommand("t", { mode: opt }, (a) => a.mode),
      ["--mode", "a"],
    );
    expect(ok).toBe("a");
    const bad = evaluate(
      leafCommand("t", { mode: opt }),
      ["--mode", "zzz"],
      config,
    );
    expect(bad._tag).toBe("error");
    if (bad._tag === "error") {
      expect(bad.message).toContain("--mode");
      expect(bad.message).toContain("a");
      expect(bad.message).toContain("b");
    }
  });

  it("errors on unknown options", () => {
    const cmd = leafCommand("t", { symbol: Options.text("symbol") });
    const outcome = evaluate(cmd, ["--bogus", "x", "--symbol", "s"], config);
    expect(outcome._tag).toBe("error");
    if (outcome._tag === "error") expect(outcome.message).toContain("--bogus");
  });

  it("errors when an option value is missing", () => {
    const cmd = leafCommand("t", { symbol: Options.text("symbol") });
    const outcome = evaluate(cmd, ["--symbol"], config);
    expect(outcome._tag).toBe("error");
    if (outcome._tag === "error") {
      expect(outcome.message).toContain("--symbol");
    }
  });

  it("errors on unexpected positional arguments", () => {
    const cmd = leafCommand("t", { symbol: Options.text("symbol") });
    const outcome = evaluate(cmd, ["extra", "--symbol", "s"], config);
    expect(outcome._tag).toBe("error");
    if (outcome._tag === "error") expect(outcome.message).toContain("extra");
  });
});

describe("kit: subcommand dispatch", () => {
  function makeTree() {
    const calls: string[] = [];
    const leafA = Command.make("a", { x: Options.integer("x") }, (args) =>
      Effect.sync(() => {
        calls.push(`a:${args.x}`);
        return "ran-a";
      }),
    );
    const leafB = Command.make("b", {}, () =>
      Effect.sync(() => {
        calls.push("b");
        return "ran-b";
      }),
    );
    const mid = Command.make("mid", {}, () =>
      Effect.sync(() => {
        calls.push("mid");
        return "ran-mid";
      }),
    ).pipe(Command.withSubcommands([leafA, leafB]));
    const root = Command.make("root", {}, () =>
      Effect.sync(() => {
        calls.push("root");
        return "ran-root";
      }),
    ).pipe(Command.withSubcommands([mid]));
    return { calls, root };
  }

  it("dispatches to nested leaf and parses its options", async () => {
    const { calls, root } = makeTree();
    const result = await runOutcome(root as AnyCommand, [
      "mid",
      "a",
      "--x",
      "7",
    ]);
    expect(result).toBe("ran-a");
    expect(calls).toEqual(["a:7"]);
  });

  it("runs the parent handler when no subcommand is given", async () => {
    const { calls, root } = makeTree();
    const result = await runOutcome(root as AnyCommand, ["mid"]);
    expect(result).toBe("ran-mid");
    expect(calls).toEqual(["mid"]);
  });

  it("errors on an unknown subcommand with candidates", () => {
    const { root } = makeTree();
    const outcome = evaluate(root as AnyCommand, ["nonsense"], config);
    expect(outcome._tag).toBe("error");
    if (outcome._tag === "error") {
      expect(outcome.message).toContain("nonsense");
      expect(outcome.message).toContain("mid");
    }
  });

  it("dispatches three levels deep", async () => {
    const place = leafCommand("place", {
      side: Options.text("side"),
    });
    const order = Command.make("order", {}, () => Effect.succeed("order")).pipe(
      Command.withSubcommands([place]),
    );
    const futures = Command.make("futures", {}, () =>
      Effect.succeed("futures"),
    ).pipe(Command.withSubcommands([order]));
    const bitget = Command.make("bitget", {}, () =>
      Effect.succeed("bitget"),
    ).pipe(Command.withSubcommands([futures]));
    const root = Command.make("root", {}, () => Effect.succeed("root")).pipe(
      Command.withSubcommands([bitget]),
    );
    const result = await runOutcome(root as AnyCommand, [
      "bitget",
      "futures",
      "order",
      "place",
      "--side",
      "buy",
    ]);
    expect(result).toBe("ok");
  });
});

describe("kit: help and version", () => {
  const root = Command.make("neuratrade", {}, () => Effect.void).pipe(
    Command.withDescription("Root description"),
    Command.withSubcommands([
      Command.make("gateway", {}, () => Effect.void).pipe(
        Command.withDescription("Manage gateway"),
      ),
      Command.make(
        "backtest",
        {
          exchange: Options.text("exchange").pipe(
            Options.withDefault("binance"),
            Options.withDescription("Exchange identifier"),
          ),
          futures: Options.boolean("futures").pipe(
            Options.withDefault(false),
            Options.withDescription("Trade futures"),
          ),
          symbol: Options.text("symbol").pipe(
            Options.withDescription("Trading pair"),
          ),
        },
        () => Effect.void,
      ).pipe(Command.withDescription("Run a backtest")),
    ]),
  );

  it("--help at root lists the CLI name and subcommands", () => {
    const outcome = evaluate(root as AnyCommand, ["--help"], config);
    expect(outcome._tag).toBe("help");
    if (outcome._tag === "help") {
      expect(outcome.text).toContain("Test CLI");
      expect(outcome.text).toContain("gateway");
      expect(outcome.text).toContain("backtest");
      expect(outcome.text).toContain("Root description");
    }
  });

  it("--help at a leaf lists every option flag", () => {
    const outcome = evaluate(
      root as AnyCommand,
      ["backtest", "--help"],
      config,
    );
    expect(outcome._tag).toBe("help");
    if (outcome._tag === "help") {
      expect(outcome.text).toContain("--exchange");
      expect(outcome.text).toContain("--futures");
      expect(outcome.text).toContain("--symbol");
      expect(outcome.text).toContain("Exchange identifier");
      expect(outcome.text).toContain("Run a backtest");
      expect(outcome.text).toContain("neuratrade backtest");
    }
  });

  it("--help short-circuits even with unknown tokens before it", () => {
    // Mirrors `scalp library gridScalp --help`: gridScalp is not a subcommand
    // of library, but --help must still render library's help (including the
    // strategy subcommand's options in the COMMANDS section).
    const strategy = Command.make(
      "strategy",
      {
        gridStepPct: Options.float("grid-step-pct").pipe(
          Options.withDefault(0),
        ),
        realistic: Options.boolean("realistic").pipe(
          Options.withDefault(false),
        ),
      },
      () => Effect.void,
    ).pipe(Command.withDescription("Show strategy template details"));
    const library = Command.make(
      "library",
      {
        list: Options.boolean("list").pipe(Options.withDefault(false)),
        strategy: Options.optional(
          Options.choice("strategy", ["gridScalp"] as const),
        ),
      },
      () => Effect.void,
    ).pipe(Command.withSubcommands([strategy]));
    const root = Command.make("scalp", {}, () => Effect.void).pipe(
      Command.withSubcommands([library]),
    );
    const outcome = evaluate(
      root as AnyCommand,
      ["library", "gridScalp", "--help"],
      config,
    );
    expect(outcome._tag).toBe("help");
    if (outcome._tag === "help") {
      expect(outcome.text).toContain("--grid-step-pct");
      expect(outcome.text).toContain("--realistic");
      expect(outcome.text).toContain("--list");
      expect(outcome.text).toContain("strategy");
    }
  });

  it("--version prints the version", () => {
    const outcome = evaluate(root as AnyCommand, ["--version"], config);
    expect(outcome._tag).toBe("version");
    if (outcome._tag === "version") {
      expect(outcome.text).toContain("v0.0.0-test");
    }
  });

  it("help for a choice option renders the allowed values", () => {
    const cmd = Command.make(
      "t",
      {
        strategy: Options.choice("strategy", [
          "meanReversion",
          "gridScalp",
        ] as const).pipe(Options.withDefault("meanReversion" as const)),
      },
      () => Effect.void,
    );
    const outcome = evaluate(cmd as AnyCommand, ["--help"], config);
    expect(outcome._tag).toBe("help");
    if (outcome._tag === "help") {
      expect(outcome.text).toContain("meanReversion");
      expect(outcome.text).toContain("gridScalp");
    }
  });
});

describe("kit: runEffect and Command.run", () => {
  it("runEffect executes the matched handler", async () => {
    let ran = false;
    const cmd = Command.make("t", {}, () =>
      Effect.sync(() => {
        ran = true;
      }),
    );
    await Effect.runPromise(runEffect(cmd, [], config));
    expect(ran).toBe(true);
  });

  it("runEffect prints help for --help", async () => {
    const cmd = Command.make("t", {}, () => Effect.void).pipe(
      Command.withDescription("desc"),
    );
    // Help rendering itself is asserted via evaluate above; here we only
    // verify the effect completes without failure.
    await Effect.runPromise(runEffect(cmd, ["--help"], config));
  });

  it("Command.run returns the handler value and drops argv[0..1]", async () => {
    const cmd = Command.make("t", { n: Options.integer("n") }, (args) =>
      Effect.succeed(args.n * 2),
    );
    const run = Command.run(cmd, config);
    const result = await Effect.runPromise(run(["bun", "script", "--n", "21"]));
    expect(result).toBe(42);
  });
});

describe("kit: property tests", () => {
  const nameArb = fc.stringMatching(
    /^[a-z][a-z0-9]{0,7}(-[a-z0-9]{1,6}){0,2}$/,
  );

  interface GeneratedSpec {
    readonly key: string;
    readonly flag: string;
    readonly kind: "text" | "integer" | "float" | "boolean" | "choice";
    readonly required: boolean;
    readonly optional: boolean;
    readonly choices: ReadonlyArray<string>;
    readonly providedValue: unknown;
    readonly defaultValue: unknown;
    readonly hasDefault: boolean;
  }

  const specArb = (key: string, flag: string): fc.Arbitrary<GeneratedSpec> =>
    fc
      .record({
        kind: fc.constantFrom(
          "text" as const,
          "integer" as const,
          "float" as const,
          "boolean" as const,
          "choice" as const,
        ),
        required: fc.boolean(),
        optional: fc.boolean(),
        provided: fc.boolean(),
        textValue: fc.stringMatching(/^[a-zA-Z0-9][a-zA-Z0-9 ./:_-]{0,15}$/),
        intValue: fc.integer({ min: -10_000, max: 10_000 }),
        floatValue: fc
          .float({ noNaN: true })
          // String(-0) === "0", so -0 cannot round-trip a CLI parse; normalize
          // it away so the round-trip property holds for every generated float.
          .map((v) => (Object.is(v, -0) ? 0 : v)),
        choices: fc
          .array(fc.stringMatching(/^[a-z][a-z0-9]{0,5}$/), {
            minLength: 1,
            maxLength: 4,
          })
          .map((cs) => [...new Set(cs)] as ReadonlyArray<string>)
          .filter((cs) => cs.length > 0),
        defaultText: fc.constant("dflt"),
        defaultInt: fc.constant(7),
      })
      .map((r) => {
        const required = r.required && !r.optional;
        const hasDefault = !required && !r.optional;
        let providedValue: unknown;
        let defaultValue: unknown;
        switch (r.kind) {
          case "text":
            providedValue = r.textValue;
            defaultValue = r.defaultText;
            break;
          case "integer":
            providedValue = r.intValue;
            defaultValue = r.defaultInt;
            break;
          case "float":
            providedValue = r.floatValue;
            defaultValue = 1.5;
            break;
          case "boolean":
            providedValue = true;
            defaultValue = false;
            break;
          case "choice":
            providedValue = r.choices[0];
            defaultValue = r.choices[r.choices.length - 1];
            break;
        }
        return {
          key,
          flag,
          kind: r.kind,
          required,
          optional: r.optional,
          choices: r.choices,
          providedValue: r.provided ? providedValue : undefined,
          defaultValue,
          hasDefault,
        };
      });

  function buildCommand(specs: ReadonlyArray<GeneratedSpec>): {
    cmd: AnyCommand;
    argv: string[];
    expected: Record<string, unknown>;
  } {
    const options: Record<string, ReturnType<typeof Options.text>> = {};
    const argv: string[] = [];
    const expected: Record<string, unknown> = {};
    for (const spec of specs) {
      // biome-ignore lint/suspicious/noExplicitAny: dynamic spec construction
      let opt: any;
      switch (spec.kind) {
        case "text":
          opt = Options.text(spec.flag);
          break;
        case "integer":
          opt = Options.integer(spec.flag);
          break;
        case "float":
          opt = Options.float(spec.flag);
          break;
        case "boolean":
          opt = Options.boolean(spec.flag);
          break;
        case "choice":
          opt = Options.choice(spec.flag, spec.choices);
          break;
      }
      if (spec.optional) {
        opt = opt.pipe(Options.optional);
      } else if (spec.hasDefault) {
        opt = opt.pipe(Options.withDefault(spec.defaultValue));
      }
      options[spec.key] = opt;

      if (spec.providedValue !== undefined) {
        argv.push(`--${spec.flag}`);
        if (spec.kind !== "boolean") argv.push(String(spec.providedValue));
        expected[spec.key] = spec.optional
          ? Option.some(spec.providedValue)
          : spec.providedValue;
      } else if (spec.optional) {
        expected[spec.key] = Option.none();
      } else if (spec.hasDefault) {
        expected[spec.key] = spec.defaultValue;
      } else if (spec.kind === "boolean") {
        expected[spec.key] = false;
      }
    }
    const cmd = Command.make("gen", options, (args) =>
      Effect.succeed(args),
    ) as AnyCommand;
    return { cmd, argv, expected };
  }

  it("generated option tables round-trip parse", () => {
    fc.assert(
      fc.property(
        fc
          .array(nameArb, { minLength: 1, maxLength: 6 })
          .map((names) => [...new Set(names)])
          .filter((names) => names.length > 0)
          .chain((names) =>
            fc.tuple(...names.map((flag, i) => specArb(`opt${i}`, flag))),
          ),
        (specs) => {
          // Boolean flags are never "required" — absence means false.
          const requiredMissing = specs.some(
            (s) =>
              s.required &&
              s.kind !== "boolean" &&
              s.providedValue === undefined,
          );
          const { cmd, argv, expected } = buildCommand(specs);
          const outcome = evaluate(cmd, argv, config);
          if (requiredMissing) {
            return outcome._tag === "error";
          }
          if (outcome._tag !== "execute") return false;
          const parsed = Effect.runSync(outcome.run() as Effect.Effect<any>);
          for (const [key, want] of Object.entries(expected)) {
            const got = (parsed as Record<string, unknown>)[key];
            if (Option.isOption(want as Option.Option<unknown>)) {
              const wantOpt = want as Option.Option<unknown>;
              const gotOpt = got as Option.Option<unknown>;
              if (Option.isSome(wantOpt)) {
                if (!Option.isSome(gotOpt) || gotOpt.value !== wantOpt.value)
                  return false;
              } else if (!Option.isNone(gotOpt)) return false;
            } else if (!Object.is(got, want)) {
              return false;
            }
          }
          return true;
        },
      ),
      { numRuns: 200 },
    );
  }, 20000);
});
