/**
 * Token-bucket rate limiter.
 *
 * Defaults to Binance public REST limits: 600 requests/minute and
 * 10 requests/second. Uses `Effect.sleep` and `Ref`; no raw `setTimeout`.
 */
import { Clock, Context, Effect, Layer, Ref } from "effect";

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

export interface RateLimiterConfig {
  readonly perSecond: number;
  readonly perMinute: number;
}

export const defaultBinanceRateLimiterConfig: RateLimiterConfig = {
  perSecond: 10,
  perMinute: 600,
};

// ---------------------------------------------------------------------------
// Service interface
// ---------------------------------------------------------------------------

export interface RateLimiterImpl {
  readonly acquire: (n?: number) => Effect.Effect<void, never>;
}

export class RateLimiter extends Context.Tag("RateLimiter")<
  RateLimiter,
  RateLimiterImpl
>() {}

// ---------------------------------------------------------------------------
// Internal model
// ---------------------------------------------------------------------------

interface Bucket {
  readonly tokens: number;
  readonly lastUpdated: number;
}

interface State {
  readonly second: Bucket;
  readonly minute: Bucket;
}

function refill(
  bucket: Bucket,
  now: number,
  capacity: number,
  refillRatePerMs: number,
): Bucket {
  const elapsed = now - bucket.lastUpdated;
  const tokens = Math.min(capacity, bucket.tokens + elapsed * refillRatePerMs);
  return { tokens, lastUpdated: now };
}

function waitTimeMs(
  bucket: Bucket,
  n: number,
  capacity: number,
  refillRatePerMs: number,
): number {
  if (bucket.tokens >= n) return 0;
  const needed = n - bucket.tokens;
  return Math.ceil(needed / refillRatePerMs);
}

// ---------------------------------------------------------------------------
// Live layer
// ---------------------------------------------------------------------------

export const RateLimiterLive = (
  config: RateLimiterConfig = defaultBinanceRateLimiterConfig,
): Layer.Layer<RateLimiter> =>
  Layer.effect(
    RateLimiter,
    Effect.gen(function* () {
      const now = yield* Clock.currentTimeMillis;
      const stateRef = yield* Ref.make({
        second: { tokens: config.perSecond, lastUpdated: now },
        minute: { tokens: config.perMinute, lastUpdated: now },
      });

      const perSecondRate = config.perSecond / 1000;
      const perMinuteRate = config.perMinute / 60000;

      const acquire = (n = 1): Effect.Effect<void, never> =>
        Effect.gen(function* () {
          while (true) {
            const now = yield* Clock.currentTimeMillis;
            const [acquired, waitMs] = yield* Ref.modify(
              stateRef,
              (state): readonly [readonly [boolean, number], State] => {
                const secondRefilled = refill(
                  state.second,
                  now,
                  config.perSecond,
                  perSecondRate,
                );
                const minuteRefilled = refill(
                  state.minute,
                  now,
                  config.perMinute,
                  perMinuteRate,
                );

                if (secondRefilled.tokens >= n && minuteRefilled.tokens >= n) {
                  return [
                    [true, 0],
                    {
                      second: {
                        ...secondRefilled,
                        tokens: secondRefilled.tokens - n,
                      },
                      minute: {
                        ...minuteRefilled,
                        tokens: minuteRefilled.tokens - n,
                      },
                    },
                  ];
                }

                const waitSecond = waitTimeMs(
                  secondRefilled,
                  n,
                  config.perSecond,
                  perSecondRate,
                );
                const waitMinute = waitTimeMs(
                  minuteRefilled,
                  n,
                  config.perMinute,
                  perMinuteRate,
                );
                const wait = Math.max(waitSecond, waitMinute, 1);
                return [
                  [false, wait],
                  { second: secondRefilled, minute: minuteRefilled },
                ];
              },
            );

            if (acquired) return;
            yield* Effect.sleep(`${waitMs} millis`);
          }
        });

      return { acquire };
    }),
  );
