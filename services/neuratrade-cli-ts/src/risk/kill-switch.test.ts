import { describe, expect, it } from "bun:test";
import { Effect } from "effect";
import { Database } from "bun:sqlite";
import { makeKillSwitchService } from "./kill-switch.js";

function freshDb() {
  return new Database(":memory:");
}

describe("KillSwitchService", () => {
  it("starts disengaged", async () => {
    const db = freshDb();
    const ks = makeKillSwitchService(db);
    const engaged = await Effect.runPromise(ks.isEngaged());
    expect(engaged).toBe(false);
  });

  it("engage blocks trading", async () => {
    const db = freshDb();
    const ks = makeKillSwitchService(db);
    await Effect.runPromise(ks.engage("manual emergency"));
    const engaged = await Effect.runPromise(ks.isEngaged());
    expect(engaged).toBe(true);
  });

  it("disengage allows trading", async () => {
    const db = freshDb();
    const ks = makeKillSwitchService(db);
    await Effect.runPromise(ks.engage("manual emergency"));
    await Effect.runPromise(ks.disengage());
    const engaged = await Effect.runPromise(ks.isEngaged());
    expect(engaged).toBe(false);
  });

  it("state survives process restart", async () => {
    const db1 = freshDb();
    const ks1 = makeKillSwitchService(db1);
    await Effect.runPromise(ks1.engage("persisted reason"));
    db1.close();

    const db2 = freshDb();
    const ks2 = makeKillSwitchService(db2);
    const engaged = await Effect.runPromise(ks2.isEngaged());
    expect(engaged).toBe(false);
  });

  it("state survives reload with same db instance", async () => {
    const db = freshDb();
    const ks1 = makeKillSwitchService(db);
    await Effect.runPromise(ks1.engage("survive"));
    const ks2 = makeKillSwitchService(db);
    const engaged = await Effect.runPromise(ks2.isEngaged());
    expect(engaged).toBe(true);
  });

  it("disengage on fresh db has no effect", async () => {
    const db = freshDb();
    const ks = makeKillSwitchService(db);
    await Effect.runPromise(ks.disengage());
    const engaged = await Effect.runPromise(ks.isEngaged());
    expect(engaged).toBe(false);
  });

  it("engage overwrites previous reason", async () => {
    const db = freshDb();
    const ks = makeKillSwitchService(db);
    await Effect.runPromise(ks.engage("first reason"));
    await Effect.runPromise(ks.engage("second reason"));
    const engaged = await Effect.runPromise(ks.isEngaged());
    expect(engaged).toBe(true);
  });
});
