import { describe, it } from "bun:test";
import * as fc from "fast-check";
import { Effect } from "effect";
import { Database } from "bun:sqlite";
import { makeKillSwitchService } from "./kill-switch.js";

function freshDb(): Database {
  return new Database(":memory:");
}

describe("KillSwitch property invariants", () => {
  it("engage() makes isEngaged() always true and preserves reason", () => {
    fc.assert(
      fc.property(fc.string(), (reason) => {
        const db = freshDb();
        const ks = makeKillSwitchService(db);
        Effect.runSync(ks.engage(reason));
        return (
          Effect.runSync(ks.isEngaged()) === true &&
          Effect.runSync(ks.getReason()) === reason
        );
      }),
      { numRuns: 50 },
    );
  });

  it("disengage() makes isEngaged() always false", () => {
    fc.assert(
      fc.property(fc.boolean(), (startEngaged) => {
        const db = freshDb();
        const ks = makeKillSwitchService(db);
        if (startEngaged) Effect.runSync(ks.engage("setup"));
        Effect.runSync(ks.disengage());
        return Effect.runSync(ks.isEngaged()) === false;
      }),
      { numRuns: 50 },
    );
  });

  it("engage followed by disengage always returns false", () => {
    fc.assert(
      fc.property(fc.string(), (reason) => {
        const db = freshDb();
        const ks = makeKillSwitchService(db);
        Effect.runSync(ks.engage(reason));
        Effect.runSync(ks.disengage());
        return Effect.runSync(ks.isEngaged()) === false;
      }),
      { numRuns: 50 },
    );
  });
});
