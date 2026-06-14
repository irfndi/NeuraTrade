import { describe, expect, it } from "bun:test";
import { Effect, Fiber, TestClock, TestContext } from "effect";
import { RateLimiter, RateLimiterLive } from "./rate-limiter.ts";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function runWithTestClock<A>(
  program: Effect.Effect<A, unknown, RateLimiter>,
): Promise<A> {
  return Effect.runPromise(
    program.pipe(
      Effect.provide(RateLimiterLive({ perSecond: 10, perMinute: 600 })),
      Effect.provide(TestContext.TestContext),
    ) as Effect.Effect<A, never>,
  );
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("RateLimiter", () => {
  it("allows burst up to capacity", async () => {
    const program = Effect.gen(function* () {
      const rl = yield* RateLimiter;
      yield* rl.acquire(10);
    });
    await runWithTestClock(program);
  });

  it("waits for token refill after capacity is exhausted", async () => {
    const program = Effect.gen(function* () {
      const rl = yield* RateLimiter;
      // Exhaust the per-second bucket (10 tokens).
      yield* rl.acquire(10);

      // The next acquire should sleep until tokens refill.
      const fiber = yield* Effect.fork(rl.acquire(1));
      yield* TestClock.adjust("150 millis");
      yield* Fiber.join(fiber);
    });
    await runWithTestClock(program);
  });

  it("reserves tokens from both per-second and per-minute buckets", async () => {
    const program = Effect.gen(function* () {
      const rl = yield* RateLimiter;
      yield* rl.acquire(5);
      yield* rl.acquire(5);
      const fiber = yield* Effect.fork(rl.acquire(1));
      yield* TestClock.adjust("150 millis");
      yield* Fiber.join(fiber);
    });
    await runWithTestClock(program);
  });
});
