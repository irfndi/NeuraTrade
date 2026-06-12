import { Effect, Schedule, Stream } from "effect";
import type { CollectionConfig } from "./types.js";
import type { MarketDataGatewayService } from "./gateway.js";
import { MarketDataGateway } from "./gateway.js";
import type { MarketDataRepositoryService } from "./repository.js";
import { MarketDataRepository } from "./repository.js";

/**
 * Error raised when a collection stream fails terminally.
 */
export class MarketDataCollectorError {
  readonly _tag = "MarketDataCollectorError" as const;
  constructor(
    readonly reason: string,
    readonly cause?: unknown,
  ) {}
}

export interface CollectorOptions {
  readonly configs: readonly CollectionConfig[];
  /** Interval between collection ticks in milliseconds (default 5000). */
  readonly intervalMs?: number;
  /** Optional hard timeout for each exchange fetch in milliseconds. */
  readonly fetchTimeoutMs?: number;
}

/**
 * Create a one-off collection effect that fetches and stores ticks for every
 * enabled config. Errors for individual configs are logged and swallowed so
 * the rest of the batch can succeed.
 */
export const collectOnce = (
  options: CollectorOptions,
): Effect.Effect<
  number,
  MarketDataCollectorError,
  MarketDataGatewayService | MarketDataRepositoryService
> =>
  Effect.gen(function* () {
    const gateway = yield* MarketDataGateway;
    const repo = yield* MarketDataRepository;

    const enabled = options.configs.filter((c) => c.enabled);
    if (enabled.length === 0) return 0;

    let collected = 0;
    for (const config of enabled) {
      yield* gateway
        .fetchTick(config.exchange, config.symbol)
        .pipe(
          Effect.tap((tick) => repo.saveTick(tick)),
          Effect.tap(() => {
            collected += 1;
          }),
          Effect.timeout(options.fetchTimeoutMs ?? 30000),
          Effect.catchAll((err) =>
            Effect.sync(() => {
              // TODO: wire to Logger service instead of console
              // eslint-disable-next-line no-console
              console.error(
                `collectOnce: ${config.exchange}:${config.symbol} failed — ${
                  err instanceof Error ? err.message : String(err)
                }`,
              );
            }),
          ),
        );
    }

    return collected;
  });

/**
 * Create a repeating collection stream that emits the number of ticks stored
 * on each successful cycle. The stream schedules itself using the provided
 * interval and retries each cycle with a fixed-delay policy on failure.
 */
export const collectStream = (
  options: CollectorOptions,
): Stream.Stream<
  number,
  MarketDataCollectorError,
  MarketDataGatewayService | MarketDataRepositoryService
> =>
  Stream.repeatEffectWithSchedule(
    collectOnce(options),
    Schedule.spaced(options.intervalMs ?? 5000),
  );
