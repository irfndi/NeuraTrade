import { describe, expect, it } from "bun:test";
import { BunFileSystem } from "@effect/platform-bun";
import { Effect, Result } from "effect";
import * as fs from "fs";
import * as os from "os";
import * as path from "path";
import {
  decodeStrategyConfig,
  defaultStrategyConfig,
  loadStrategyConfig,
  saveStrategyConfig,
  strategyConfigToComposerConfig,
} from "./strategy-config.js";

function tmpHome(): string {
  return fs.mkdtempSync(path.join(os.tmpdir(), "strategy-config-test-"));
}

describe("decodeStrategyConfig", () => {
  it("decodes an empty object by applying schema defaults", async () => {
    const config = await Effect.runPromise(decodeStrategyConfig({}));

    expect(config.version).toBe("1");
    expect(config.signalRules.minConfidence).toBe(0.5);
    expect(config.signalRules.regimeMode).toBe("trend");
    expect(config.execution.stopLossPct).toBe(1.0);
    expect(config.execution.takeProfitPct).toBe(1.2);
    expect(config.portfolio.maxOpenPositions).toBe(1);
  });

  it("preserves provided values over defaults", async () => {
    const config = await Effect.runPromise(
      decodeStrategyConfig({
        name: "custom",
        signalRules: { minConfidence: 0.7, regimeMode: "reversion" },
        execution: { stopLossPct: 2.5 },
      }),
    );

    expect(config.name).toBe("custom");
    expect(config.signalRules.minConfidence).toBe(0.7);
    expect(config.signalRules.regimeMode).toBe("reversion");
    expect(config.execution.stopLossPct).toBe(2.5);
    // Untouched fields still get their decoding defaults.
    expect(config.execution.takeProfitPct).toBe(1.2);
  });

  it("fails instead of silently casting invalid input", async () => {
    const wrongVersion = await Effect.runPromise(
      Effect.result(decodeStrategyConfig({ version: "2" })),
    );
    expect(Result.isFailure(wrongVersion)).toBe(true);

    const wrongType = await Effect.runPromise(
      Effect.result(
        decodeStrategyConfig({ execution: { stopLossPct: "wide" } }),
      ),
    );
    expect(Result.isFailure(wrongType)).toBe(true);
  });
});

describe("saveStrategyConfig / loadStrategyConfig", () => {
  it("round-trips a config through the schema decoder", async () => {
    const home = tmpHome();
    const config = defaultStrategyConfig();

    await Effect.runPromise(
      saveStrategyConfig(home, config, "roundtrip").pipe(
        Effect.provide(BunFileSystem.layer),
      ),
    );
    const loaded = await Effect.runPromise(
      loadStrategyConfig(home, "roundtrip"),
    );

    expect(loaded).toEqual(config);
    fs.rmSync(home, { recursive: true, force: true });
  });

  it("fails to load a config that violates the schema", async () => {
    const home = tmpHome();
    const dir = path.join(home, "strategy-configs");
    fs.mkdirSync(dir, { recursive: true });
    fs.writeFileSync(
      path.join(dir, "broken.json"),
      JSON.stringify({ version: "2", execution: { stopLossPct: "wide" } }),
    );

    const result = await Effect.runPromise(
      Effect.result(loadStrategyConfig(home, "broken")),
    );
    expect(Result.isFailure(result)).toBe(true);
    fs.rmSync(home, { recursive: true, force: true });
  });
});

describe("strategyConfigToComposerConfig", () => {
  it("maps the default config to a valid composer config", () => {
    const composerConfig = strategyConfigToComposerConfig(
      defaultStrategyConfig(),
    );

    expect(composerConfig.thresholds.regimeMode).toBe("trend");
    expect(composerConfig.enabled?.trend).toBe(true);
    expect(composerConfig.weights.trend).toBeGreaterThan(0);
  });
});
