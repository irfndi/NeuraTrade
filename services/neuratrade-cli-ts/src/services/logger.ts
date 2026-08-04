import { Context, Effect, Layer } from "effect";

/**
 * Structured log annotations attached to log entries.
 */
export type LogAnnotations = Record<string, string | number | boolean>;

/**
 * Logger service interface for structured logging using Effect-TS.
 *
 * Wraps Effect's built-in log functions (Effect.log, Effect.logWarning, etc.)
 * behind a Context.Tag for clean dependency injection.
 *
 * Annotations are attached to individual log entries via Effect.annotateLogs.
 */
export interface Logger {
  readonly info: (
    message: string,
    annotations?: LogAnnotations,
  ) => Effect.Effect<void>;
  readonly warn: (
    message: string,
    annotations?: LogAnnotations,
  ) => Effect.Effect<void>;
  readonly error: (
    message: string,
    annotations?: LogAnnotations,
  ) => Effect.Effect<void>;
  readonly debug: (
    message: string,
    annotations?: LogAnnotations,
  ) => Effect.Effect<void>;
}

/**
 * Context.Tag for the Logger service.
 */
export const Logger = Context.Service<Logger>("Logger");

/**
 * Attaches annotations to an Effect if provided, otherwise returns the
 * original Effect unchanged.
 */
function withAnnotations<A, E, R>(
  effect: Effect.Effect<A, E, R>,
  annotations?: LogAnnotations,
): Effect.Effect<A, E, R> {
  if (!annotations || Object.keys(annotations).length === 0) {
    return effect;
  }
  return effect.pipe(Effect.annotateLogs(annotations));
}

/**
 * Layer that provides the Logger service.
 *
 * Delegates to Effect's built-in log functions and attaches structured
 * annotations via Effect.annotateLogs.
 *
 * @example
 * ```ts
 * import { Effect } from "effect";
 * import { Logger, LoggerLive } from "./services/logger";
 *
 * const program = Effect.gen(function* () {
 *   const logger = yield* Logger;
 *   yield* logger.info("started", { port: 8080 });
 * });
 *
 * Effect.runFork(program.pipe(Effect.provide(LoggerLive)));
 * ```
 */
export const LoggerLive: Layer.Layer<Logger> = Layer.succeed(Logger, {
  info: (message, annotations) =>
    withAnnotations(Effect.log(message), annotations),
  warn: (message, annotations) =>
    withAnnotations(Effect.logWarning(message), annotations),
  error: (message, annotations) =>
    withAnnotations(Effect.logError(message), annotations),
  debug: (message, annotations) =>
    withAnnotations(Effect.logDebug(message), annotations),
});
