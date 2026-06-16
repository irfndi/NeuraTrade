import { describe, expect, it } from "bun:test";
import { Effect } from "effect";
import { Database } from "bun:sqlite";
import { makeCircuitBreakerService } from "./circuit-breaker.js";

function freshDb() {
  return new Database(":memory:");
}

describe("CircuitBreakerService", () => {
  it("starts closed with zero daily loss", async () => {
    const db = freshDb();
    const cb = makeCircuitBreakerService(db, 2);
    const open = await Effect.runPromise(cb.isOpen());
    const loss = await Effect.runPromise(cb.currentDailyLossPct());
    expect(open).toBe(false);
    expect(loss).toBe(0);
  });

  it("stays closed below threshold", async () => {
    const db = freshDb();
    const cb = makeCircuitBreakerService(db, 2);
    await Effect.runPromise(cb.recordTradeResult(-1.5, 100));
    const open = await Effect.runPromise(cb.isOpen());
    expect(open).toBe(false);
  });

  it("opens at threshold", async () => {
    const db = freshDb();
    const cb = makeCircuitBreakerService(db, 2);
    await Effect.runPromise(cb.recordTradeResult(-2, 100));
    const open = await Effect.runPromise(cb.isOpen());
    expect(open).toBe(true);
  });

  it("opens above threshold", async () => {
    const db = freshDb();
    const cb = makeCircuitBreakerService(db, 2);
    await Effect.runPromise(cb.recordTradeResult(-5, 100));
    const open = await Effect.runPromise(cb.isOpen());
    expect(open).toBe(true);
  });

  it("tracks cumulative daily PnL", async () => {
    const db = freshDb();
    const cb = makeCircuitBreakerService(db, 2);
    await Effect.runPromise(cb.recordTradeResult(-0.5, 100));
    await Effect.runPromise(cb.recordTradeResult(-0.5, 100));
    await Effect.runPromise(cb.recordTradeResult(-0.5, 100));
    const loss = await Effect.runPromise(cb.currentDailyLossPct());
    expect(loss).toBe(1.5);
  });

  it("opens when cumulative losses hit threshold", async () => {
    const db = freshDb();
    const cb = makeCircuitBreakerService(db, 2);
    await Effect.runPromise(cb.recordTradeResult(-1, 100));
    await Effect.runPromise(cb.recordTradeResult(-1, 100));
    const open = await Effect.runPromise(cb.isOpen());
    expect(open).toBe(true);
  });

  it("reset clears today state", async () => {
    const db = freshDb();
    const cb = makeCircuitBreakerService(db, 2);
    await Effect.runPromise(cb.recordTradeResult(-3, 100));
    await Effect.runPromise(cb.reset());
    const open = await Effect.runPromise(cb.isOpen());
    const loss = await Effect.runPromise(cb.currentDailyLossPct());
    expect(open).toBe(false);
    expect(loss).toBe(0);
  });

  it("new day starts fresh (previous day data ignored)", async () => {
    const db = freshDb();
    const yesterday = new Date(Date.now() - 86400000)
      .toISOString()
      .slice(0, 10);
    db.exec(
      `CREATE TABLE IF NOT EXISTS risk_circuit_breaker (
        date TEXT PRIMARY KEY,
        daily_pnl REAL NOT NULL DEFAULT 0,
        open BOOLEAN NOT NULL DEFAULT 0,
        reason TEXT NOT NULL DEFAULT '',
        updated_at DATETIME NOT NULL
      )`,
    );
    db.query(
      `INSERT INTO risk_circuit_breaker (date, daily_pnl, open, reason, updated_at)
       VALUES (?, -5, 1, 'yesterday', datetime('now'))`,
    ).run(yesterday);
    db.close();

    const db2 = new Database(":memory:");
    const cb = makeCircuitBreakerService(db2, 2);
    const open = await Effect.runPromise(cb.isOpen());
    const loss = await Effect.runPromise(cb.currentDailyLossPct());
    expect(open).toBe(false);
    expect(loss).toBe(0);
  });

  it("state survives reload on same db", async () => {
    const db = freshDb();
    const cb1 = makeCircuitBreakerService(db, 2);
    await Effect.runPromise(cb1.recordTradeResult(-3, 100));
    const cb2 = makeCircuitBreakerService(db, 2);
    const open = await Effect.runPromise(cb2.isOpen());
    expect(open).toBe(true);
  });

  it("wins recover daily PnL", async () => {
    const db = freshDb();
    const cb = makeCircuitBreakerService(db, 2);
    await Effect.runPromise(cb.recordTradeResult(-1.5, 100));
    await Effect.runPromise(cb.recordTradeResult(0.5, 100));
    const loss = await Effect.runPromise(cb.currentDailyLossPct());
    expect(loss).toBe(1);
  });
});
