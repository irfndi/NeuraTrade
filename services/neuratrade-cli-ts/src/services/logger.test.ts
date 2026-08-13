import { describe, expect, test } from "bun:test";
import {
  Effect,
  Logger as EffectLogger,
  LogLevel,
  Record,
  References,
} from "effect";
import { Logger, LoggerLive } from "./logger";

/**
 * Helper: capture log entries produced by an Effect program.
 * Installs a custom Effect Logger that pushes structured entries into an array.
 */
function collectLogs(
  program: Effect.Effect<void, never, Logger>,
  options?: { minimumLogLevel?: LogLevel.LogLevel },
): Array<{
  logLevel: string;
  message: unknown;
  annotations: Record.ReadonlyRecord<string, unknown>;
}> {
  const entries: Array<{
    logLevel: string;
    message: unknown;
    annotations: Record.ReadonlyRecord<string, unknown>;
  }> = [];

  const collector = EffectLogger.make(({ logLevel, message, fiber }) => {
    entries.push({
      logLevel: logLevel.toUpperCase(),
      message,
      annotations: fiber.getRef(References.CurrentLogAnnotations),
    });
  });

  const loggerLayer = EffectLogger.layer([collector]);
  const minLevel = options?.minimumLogLevel ?? "Trace";

  const runnable = program.pipe(
    Effect.provide(LoggerLive),
    Effect.provide(loggerLayer),
    Effect.provideService(References.MinimumLogLevel, minLevel),
  );

  Effect.runSync(runnable);
  return entries;
}

describe("Logger service", () => {
  test("info emits INFO-level log with message", () => {
    const entries = collectLogs(
      Effect.gen(function* () {
        const logger = yield* Logger;
        yield* logger.info("server started");
      }),
    );

    expect(entries).toHaveLength(1);
    expect(entries[0].logLevel).toBe("INFO");
    expect(entries[0].message).toContain("server started");
  });

  test("warn emits WARNING-level log with message", () => {
    const entries = collectLogs(
      Effect.gen(function* () {
        const logger = yield* Logger;
        yield* logger.warn("disk usage high");
      }),
    );

    expect(entries).toHaveLength(1);
    expect(entries[0].logLevel).toBe("WARN");
    expect(entries[0].message).toContain("disk usage high");
  });

  test("error emits ERROR-level log with message", () => {
    const entries = collectLogs(
      Effect.gen(function* () {
        const logger = yield* Logger;
        yield* logger.error("connection refused");
      }),
    );

    expect(entries).toHaveLength(1);
    expect(entries[0].logLevel).toBe("ERROR");
    expect(entries[0].message).toContain("connection refused");
  });

  test("debug emits DEBUG-level log with message (requires Debug minimum level)", () => {
    const entries = collectLogs(
      Effect.gen(function* () {
        const logger = yield* Logger;
        yield* logger.debug("step completed");
      }),
      { minimumLogLevel: "Debug" },
    );

    expect(entries).toHaveLength(1);
    expect(entries[0].logLevel).toBe("DEBUG");
    expect(entries[0].message).toContain("step completed");
  });

  test("debug is filtered when minimum log level is Info", () => {
    const entries = collectLogs(
      Effect.gen(function* () {
        const logger = yield* Logger;
        yield* logger.debug("should not appear");
        yield* logger.info("should appear");
      }),
      { minimumLogLevel: "Info" },
    );

    expect(entries).toHaveLength(1);
    expect(entries[0].logLevel).toBe("INFO");
    expect(entries[0].message).toContain("should appear");
  });

  test("annotations are attached to log entries", () => {
    const entries = collectLogs(
      Effect.gen(function* () {
        const logger = yield* Logger;
        yield* logger.info("order placed", {
          symbol: "BTC/USDT",
          quantity: 0.5,
          dryRun: true,
        });
      }),
    );

    expect(entries).toHaveLength(1);
    expect(entries[0].annotations.symbol).toBe("BTC/USDT");
    expect(entries[0].annotations.quantity).toBe(0.5);
    expect(entries[0].annotations.dryRun).toBe(true);
  });

  test("annotations default to empty record when omitted", () => {
    const entries = collectLogs(
      Effect.gen(function* () {
        const logger = yield* Logger;
        yield* logger.info("bare message");
      }),
    );

    expect(entries).toHaveLength(1);
    expect(Object.keys(entries[0].annotations)).toHaveLength(0);
  });

  test("multiple log calls in sequence produce multiple entries", () => {
    const entries = collectLogs(
      Effect.gen(function* () {
        const logger = yield* Logger;
        yield* logger.info("first");
        yield* logger.warn("second");
        yield* logger.error("third");
      }),
    );

    expect(entries).toHaveLength(3);
    expect(entries[0].logLevel).toBe("INFO");
    expect(entries[1].logLevel).toBe("WARN");
    expect(entries[2].logLevel).toBe("ERROR");
  });

  test("LoggerLive is a valid Layer that satisfies the Logger tag", () => {
    // Verify LoggerLive can be provided and the tag resolves
    const result = Effect.gen(function* () {
      const logger = yield* Logger;
      expect(logger).toBeDefined();
      expect(logger.info).toBeInstanceOf(Function);
      expect(logger.warn).toBeInstanceOf(Function);
      expect(logger.error).toBeInstanceOf(Function);
      expect(logger.debug).toBeInstanceOf(Function);
    }).pipe(Effect.provide(LoggerLive));

    Effect.runSync(result);
  });

  test("annotations with all supported types (string, number, boolean)", () => {
    const entries = collectLogs(
      Effect.gen(function* () {
        const logger = yield* Logger;
        yield* logger.info("typed annotations", {
          str: "hello",
          num: 42,
          flag: false,
        });
      }),
    );

    expect(entries).toHaveLength(1);
    expect(entries[0].annotations.str).toBe("hello");
    expect(entries[0].annotations.num).toBe(42);
    expect(entries[0].annotations.flag).toBe(false);
  });
});
