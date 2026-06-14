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
      const secondRef = yield* Ref.make<Bucket>({
        tokens: config.perSecond,
        lastUpdated: now,
      });
      const minuteRef = yield* Ref.make<Bucket>({
        tokens: config.perMinute,
        lastUpdated: now,
      });

      const perSecondRate = config.perSecond / 1000;
      const perMinuteRate = config.perMinute / 60000;

      const acquire = (n = 1): Effect.Effect<void, never> =>
        Effect.gen(function* () {
          while (true) {
            const now = yield* Clock.currentTimeMillis;
            const secondBucket = yield* Ref.get(secondRef);
            const minuteBucket = yield* Ref.get(minuteRef);

            const secondRefilled = refill(
              secondBucket,
              now,
              config.perSecond,
              perSecondRate,
            );
            const minuteRefilled = refill(
              minuteBucket,
              now,
              config.perMinute,
              perMinuteRate,
            );

            yield* Ref.set(secondRef, secondRefilled);
            yield* Ref.set(minuteRef, minuteRefilled);

            if (secondRefilled.tokens >= n && minuteRefilled.tokens >= n) {
              yield* Ref.set(secondRef, {
                ...secondRefilled,
                tokens: secondRefilled.tokens - n,
              });
              yield* Ref.set(minuteRef, {
                ...minuteRefilled,
                tokens: minuteRefilled.tokens - n,
              });
              return;
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
            const waitMs = Math.max(waitSecond, waitMinute, 1);
            yield* Effect.sleep(`${waitMs} millis`);
          }
        });

      return { acquire };
    }),
  );
