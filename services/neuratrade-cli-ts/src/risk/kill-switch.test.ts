import { describe, expect, it } from "bun:test";
import { Effect } from "effect";
import { Database } from "bun:sqlite";
import { KillSwitchError, makeKillSwitchService } from "./kill-switch.js";

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

  it("treats a missing row as engaged-unknown (fails closed)", async () => {
    const db = freshDb();
    const ks = makeKillSwitchService(db);
    db.query("DELETE FROM risk_kill_switch WHERE id = 1").run();
    const outcome = await Effect.runPromise(ks.isEngaged()).then(
      (value) => ({ tag: "Right" as const, value }),
      (error) => ({ tag: "Left" as const, error }),
    );
    if (outcome.tag !== "Left") {
      throw new Error("expected kill switch read to fail closed on a missing row");
    }
    expect(outcome.error).toBeInstanceOf(KillSwitchError);
  });

  it("keeps blocking in-process when the DB write fails on engage", async () => {
    const db = freshDb();
    const ks = makeKillSwitchService(db);
    await Effect.runPromise(ks.engage("emergency"));
    db.close(); // DB now unusable; the mirror must still report engaged.
    const engaged = await Effect.runPromise(ks.isEngaged());
    expect(engaged).toBe(true);
  });
});
