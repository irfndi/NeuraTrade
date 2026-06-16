import { Context, Effect, Layer } from "effect";
import type { Database } from "bun:sqlite";
import { Decimal } from "../utils/money.js";

export class CircuitBreakerError {
  readonly _tag = "CircuitBreakerError" as const;
  constructor(
    readonly reason: string,
    readonly cause?: unknown,
  ) {}
}

export interface CircuitBreakerService {
  readonly recordTradeResult: (
    realizedPnl: number,
    startOfDayCapital: number,
  ) => Effect.Effect<void, CircuitBreakerError, never>;
  readonly isOpen: () => Effect.Effect<boolean, CircuitBreakerError, never>;
  readonly getReason: () => Effect.Effect<string, CircuitBreakerError, never>;
  readonly currentDailyLossPct: () => Effect.Effect<
    number,
    CircuitBreakerError,
    never
  >;
  readonly reset: () => Effect.Effect<void, CircuitBreakerError, never>;
}

export const CircuitBreaker =
  Context.GenericTag<CircuitBreakerService>("CircuitBreaker");

const ensureTableSQL = `
CREATE TABLE IF NOT EXISTS risk_circuit_breaker (
  date TEXT PRIMARY KEY,
  daily_pnl REAL NOT NULL DEFAULT 0,
  open BOOLEAN NOT NULL DEFAULT 0,
  reason TEXT NOT NULL DEFAULT '',
  updated_at DATETIME NOT NULL
);
`;

function todayKey(): string {
  return new Date().toISOString().slice(0, 10);
}

class CircuitBreakerSQLite implements CircuitBreakerService {
  constructor(
    private readonly db: Database,
    private readonly maxDailyLossPct: number,
  ) {
    this.db.exec(ensureTableSQL);
  }

  private upsertRow(dailyPnl: number, open: boolean, reason: string): void {
    this.db
      .query(
        `INSERT INTO risk_circuit_breaker (date, daily_pnl, open, reason, updated_at)
         VALUES (?, ?, ?, ?, datetime('now'))
         ON CONFLICT(date) DO UPDATE SET
           daily_pnl = excluded.daily_pnl,
           open = excluded.open,
           reason = excluded.reason,
           updated_at = excluded.updated_at`,
      )
      .run(todayKey(), dailyPnl, open ? 1 : 0, reason);
  }

  private readRow(): {
    dailyPnl: number;
    open: boolean;
    reason: string;
  } | null {
    const row = this.db
      .query(
        "SELECT daily_pnl, open, reason FROM risk_circuit_breaker WHERE date = ?",
      )
      .get(todayKey()) as {
      daily_pnl: number;
      open: number;
      reason: string;
    } | null;
    if (!row) return null;
    return {
      dailyPnl: row.daily_pnl,
      open: row.open === 1,
      reason: row.reason,
    };
  }

  recordTradeResult(
    realizedPnl: number,
    startOfDayCapital: number,
  ): Effect.Effect<void, CircuitBreakerError, never> {
    return Effect.try({
      try: () => {
        const existing = this.readRow();
        const prevPnl = existing?.dailyPnl ?? 0;
        const pnlPct =
          startOfDayCapital > 0 ? (realizedPnl / startOfDayCapital) * 100 : 0;
        const newPnl = new Decimal(prevPnl).plus(pnlPct).toNumber();
        const shouldOpen = newPnl <= -this.maxDailyLossPct;
        const wasOpen = existing?.open ?? false;

        if (shouldOpen && !wasOpen) {
          this.upsertRow(
            newPnl,
            true,
            `Daily loss ${Math.abs(newPnl).toFixed(2)}% reached threshold ${this.maxDailyLossPct}%`,
          );
        } else if (wasOpen) {
          this.upsertRow(newPnl, true, existing!.reason);
        } else {
          this.upsertRow(newPnl, false, "");
        }
      },
      catch: (err) =>
        new CircuitBreakerError(
          `Failed to record trade result: ${err instanceof Error ? err.message : String(err)}`,
          err,
        ),
    });
  }

  isOpen(): Effect.Effect<boolean, CircuitBreakerError, never> {
    return Effect.try({
      try: () => {
        const existing = this.readRow();
        return existing?.open ?? false;
      },
      catch: (err) =>
        new CircuitBreakerError(
          `Failed to check circuit breaker: ${err instanceof Error ? err.message : String(err)}`,
          err,
        ),
    });
  }

  getReason(): Effect.Effect<string, CircuitBreakerError, never> {
    return Effect.try({
      try: () => {
        const existing = this.readRow();
        return existing?.reason ?? "";
      },
      catch: (err) =>
        new CircuitBreakerError(
          `Failed to read circuit breaker reason: ${err instanceof Error ? err.message : String(err)}`,
          err,
        ),
    });
  }

  currentDailyLossPct(): Effect.Effect<number, CircuitBreakerError, never> {
    return Effect.try({
      try: () => {
        const existing = this.readRow();
        return Math.abs(Math.min(existing?.dailyPnl ?? 0, 0));
      },
      catch: (err) =>
        new CircuitBreakerError(
          `Failed to read daily loss: ${err instanceof Error ? err.message : String(err)}`,
          err,
        ),
    });
  }

  reset(): Effect.Effect<void, CircuitBreakerError, never> {
    return Effect.try({
      try: () => {
        this.db
          .query("DELETE FROM risk_circuit_breaker WHERE date = ?")
          .run(todayKey());
      },
      catch: (err) =>
        new CircuitBreakerError(
          `Failed to reset circuit breaker: ${err instanceof Error ? err.message : String(err)}`,
          err,
        ),
    });
  }
}

export const CircuitBreakerSQLiteLive = (
  db: Database,
  maxDailyLossPct: number,
): Layer.Layer<CircuitBreakerService> =>
  Layer.succeed(
    CircuitBreaker,
    new CircuitBreakerSQLite(db, maxDailyLossPct) as CircuitBreakerService,
  );

export function makeCircuitBreakerService(
  db: Database,
  maxDailyLossPct: number = 2,
): CircuitBreakerService {
  return new CircuitBreakerSQLite(db, maxDailyLossPct);
}
