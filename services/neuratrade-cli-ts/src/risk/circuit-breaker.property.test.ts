import { describe, it } from "bun:test";
import * as fc from "fast-check";
import { Effect } from "effect";
import { Database } from "bun:sqlite";
import { makeCircuitBreakerService } from "./circuit-breaker.js";

function freshDb(): Database {
  return new Database(":memory:");
}

describe("CircuitBreaker property invariants", () => {
  it("cumulative losses >= threshold opens the breaker", () => {
    fc.assert(
      fc.property(
        fc.array(
          fc.float({ min: -500, max: Math.fround(-0.01), noNaN: true }),
          { minLength: 1, maxLength: 20 },
        ),
        (losses) => {
          const threshold = 2;
          const db = freshDb();
          const cb = makeCircuitBreakerService(db, threshold);
          for (const loss of losses) {
            Effect.runSync(cb.recordTradeResult(loss));
          }
          const sum = losses.reduce((a, b) => a + b, 0);
          if (sum <= -threshold) {
            return Effect.runSync(cb.isOpen()) === true;
          }
          return true;
        },
      ),
      { numRuns: 80 },
    );
  });

  it("only wins never opens the breaker", () => {
    fc.assert(
      fc.property(
        fc.array(fc.float({ min: Math.fround(0.01), max: 1000, noNaN: true }), {
          minLength: 1,
          maxLength: 30,
        }),
        (wins) => {
          const db = freshDb();
          const cb = makeCircuitBreakerService(db, 2);
          for (const win of wins) {
            Effect.runSync(cb.recordTradeResult(win));
          }
          return Effect.runSync(cb.isOpen()) === false;
        },
      ),
      { numRuns: 80 },
    );
  });

  it("reset() always results in isOpen false and currentDailyLossPct 0", () => {
    fc.assert(
      fc.property(
        fc.array(fc.float({ min: -1000, max: 1000, noNaN: true }), {
          minLength: 0,
          maxLength: 20,
        }),
        (pnls) => {
          const db = freshDb();
          const cb = makeCircuitBreakerService(db, 2);
          for (const pnl of pnls) {
            Effect.runSync(cb.recordTradeResult(pnl));
          }
          Effect.runSync(cb.reset());
          const open = Effect.runSync(cb.isOpen());
          const loss = Effect.runSync(cb.currentDailyLossPct());
          return open === false && loss === 0;
        },
      ),
      { numRuns: 80 },
    );
  });
});
