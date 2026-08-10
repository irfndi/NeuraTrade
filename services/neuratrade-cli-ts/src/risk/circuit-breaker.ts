import { Context, Effect, Layer } from "effect";
import type { Database } from "bun:sqlite";

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
  Context.Service<CircuitBreakerService>("CircuitBreaker");

const ensureTableSQL = `
CREATE TABLE IF NOT EXISTS risk_circuit_breaker (
  date TEXT PRIMARY KEY,
  daily_pnl REAL NOT NULL DEFAULT 0,
  open BOOLEAN NOT NULL DEFAULT 0,
  reason TEXT NOT NULL DEFAULT '',
  updated_at DATETIME NOT NULL
);
`;

/** UTC calendar day key. This deliberately matches the trading loop's day
 *  definition: paper-trading/repository.ts keys start-of-day capital by
 *  `date.toISOString().slice(0, 10)` and realized PnL by SQLite
 *  `date('now')` (both UTC), so breaker resets stay aligned with
 *  startOfDayCapital resets. Change together if a local/UTC+8 boundary is
 *  ever adopted. */
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

  /** Single-statement UPSERT: accumulates daily_pnl and re-evaluates the
   *  open latch atomically inside SQLite's ON CONFLICT clause, so
   *  concurrent recordTradeResult calls cannot lose updates (no
   *  read-then-write window). Params: ?1 date key, ?2 per-trade pnl %,
   *  ?3 max daily loss % (reused). */
  private accumulateSQL = `
INSERT INTO risk_circuit_breaker (date, daily_pnl, open, reason, updated_at)
VALUES (
  ?1,
  ?2,
  CASE WHEN ?2 <= -?3 THEN 1 ELSE 0 END,
  CASE WHEN ?2 <= -?3 THEN
    printf('Daily loss %.2f%% reached threshold %.2f%%', ABS(?2), ?3)
    ELSE '' END,
  datetime('now')
)
ON CONFLICT(date) DO UPDATE SET
  daily_pnl = daily_pnl + excluded.daily_pnl,
  open = CASE
    WHEN risk_circuit_breaker.open = 1 THEN 1
    WHEN risk_circuit_breaker.daily_pnl + excluded.daily_pnl <= -?3 THEN 1
    ELSE 0
  END,
  reason = CASE
    WHEN risk_circuit_breaker.open = 1 THEN risk_circuit_breaker.reason
    WHEN risk_circuit_breaker.daily_pnl + excluded.daily_pnl <= -?3 THEN
      printf('Daily loss %.2f%% reached threshold %.2f%%',
             ABS(risk_circuit_breaker.daily_pnl + excluded.daily_pnl), ?3)
    ELSE ''
  END,
  updated_at = excluded.updated_at
`;

  private openBreaker(reason: string): void {
    // Preserves the accumulated daily_pnl while forcing the breaker open.
    this.db
      .query(
        `INSERT INTO risk_circuit_breaker (date, daily_pnl, open, reason, updated_at)
         VALUES (?, 0, 1, ?, datetime('now'))
         ON CONFLICT(date) DO UPDATE SET
           open = 1,
           reason = CASE
             WHEN risk_circuit_breaker.open = 1 THEN risk_circuit_breaker.reason
             ELSE excluded.reason END,
           updated_at = excluded.updated_at`,
      )
      .run(todayKey(), reason);
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
        if (startOfDayCapital <= 0) {
          // Zero/negative start-of-day capital means the daily loss limit
          // cannot be expressed as a percentage — fail closed instead of
          // recording 0% (which would let losses through unmeasured).
          this.openBreaker(
            `Start-of-day capital ${startOfDayCapital} is not positive; trading halted`,
          );
          return;
        }
        const pnlPct = (realizedPnl / startOfDayCapital) * 100;
        this.db.query(this.accumulateSQL).run(todayKey(), pnlPct, this.maxDailyLossPct);
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
