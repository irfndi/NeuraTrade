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
  /** Clear the open latch for the current UTC day.
   *
   *  Refuses (fails) while the breaker is open unless `authorized` is set.
   *  A reset never deletes today's row: the accumulated daily PnL is kept
   *  (so the same daily limit cannot be spent twice), and the halt (reason,
   *  PnL, time, operator) is archived to `risk_circuit_breaker_resets`. */
  readonly reset: (
    options?: CircuitBreakerResetOptions,
  ) => Effect.Effect<void, CircuitBreakerError, never>;
}

export interface CircuitBreakerResetOptions {
  /** Explicit operator authorization for a mid-day reset. */
  readonly authorized?: boolean;
  /** Free-form operator/process identity recorded in the audit row. */
  readonly by?: string;
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

/**
 * Fail-closed contract (documented on CircuitBreakerService): a read error
 * surfaces as `CircuitBreakerError` and callers must treat any such error as
 * "trading halted" (kill-switch posture). The constructor seeds today's row,
 * so a missing row for the day this instance was created is unknown state
 * (external deletion / tampering) and every read path FAILS CLOSED with an
 * error instead of reporting open=false. A row missing for a *new* day (the
 * UTC day rolled over after construction) is a legitimate fresh day: no
 * trades are recorded yet, so open=false and 0% loss are correct.
 */

const ensureResetTableSQL = `
CREATE TABLE IF NOT EXISTS risk_circuit_breaker_resets (
  date TEXT NOT NULL,
  daily_pnl REAL NOT NULL,
  was_open BOOLEAN NOT NULL,
  reason TEXT NOT NULL DEFAULT '',
  cleared_at DATETIME NOT NULL,
  cleared_by TEXT NOT NULL DEFAULT ''
);
`;

const ensureTodayRowSQL = `
INSERT INTO risk_circuit_breaker (date, daily_pnl, open, reason, updated_at)
VALUES (?, 0, 0, '', datetime('now'))
ON CONFLICT(date) DO NOTHING;
`;
interface DailyBreakerRow {
  dailyPnl: number;
  open: boolean;
  reason: string;
}

class CircuitBreakerSQLite implements CircuitBreakerService {
  constructor(
    private readonly db: Database,
    private readonly maxDailyLossPct: number,
  ) {
    this.db.exec(ensureTableSQL);
    this.db.exec(ensureResetTableSQL);
    // Anchor today's row so a missing row is genuine unknown state (see the
    // fail-closed contract above), not an ambiguity about the day boundary.
    this.db.query(ensureTodayRowSQL).run(todayKey());
    this.observedRowDate = todayKey();
  }

  /** Date key whose row was last present in this process. A missing row on a
   *  different (or never-observed) date is a legitimate fresh day; a missing
   *  row on the observed date is vanished state and reads fail closed. */
  private observedRowDate: string | null = null;

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
    this.observedRowDate = todayKey();
  }

  private readRow(): DailyBreakerRow | null {
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
    this.observedRowDate = todayKey();
    return {
      dailyPnl: row.daily_pnl,
      open: row.open === 1,
      reason: row.reason,
    };
  }

  /** True when today's row existed in this process and has since vanished. */
  private rowVanished(): boolean {
    return this.observedRowDate === todayKey();
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
        this.db
          .query(this.accumulateSQL)
          .run(todayKey(), pnlPct, this.maxDailyLossPct);
        // The UPSERT guarantees today's row exists; remember that so a later
        // disappearance is detected as vanished state by the read paths.
        this.observedRowDate = todayKey();
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
        if (existing === null && this.rowVanished()) {
          throw new Error(
            "today's circuit-breaker row was observed earlier and has vanished; failing closed",
          );
        }
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
        if (existing === null && this.rowVanished()) {
          throw new Error(
            "today's circuit-breaker row was observed earlier and has vanished; failing closed",
          );
        }
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
        if (existing === null && this.rowVanished()) {
          throw new Error(
            "today's circuit-breaker row was observed earlier and has vanished; failing closed",
          );
        }
        return Math.abs(Math.min(existing?.dailyPnl ?? 0, 0));
      },
      catch: (err) =>
        new CircuitBreakerError(
          `Failed to read daily loss: ${err instanceof Error ? err.message : String(err)}`,
          err,
        ),
    });
  }

  reset(
    options?: CircuitBreakerResetOptions,
  ): Effect.Effect<void, CircuitBreakerError, never> {
    return Effect.try({
      try: () => {
        const existing = this.readRow();
        if (existing === null) {
          // Today's row is anchored at construction; a missing row here is
          // unknown state, not a fresh day.
          throw new Error(
            "today's circuit-breaker row is missing; refusing to reset unknown state",
          );
        }
        if (existing.open && options?.authorized !== true) {
          throw new Error(
            "breaker is open; a mid-day reset requires authorized: true so the halt stays auditable",
          );
        }
        if (!existing.open && existing.dailyPnl === 0) {
          return; // Nothing accumulated today; leave no audit noise.
        }
        // Archive the halt before clearing the latch: the daily PnL, reason
        // and operator survive the reset for audit.
        this.db
          .query(
            `INSERT INTO risk_circuit_breaker_resets
               (date, daily_pnl, was_open, reason, cleared_at, cleared_by)
             VALUES (?, ?, ?, ?, datetime('now'), ?)`,
          )
          .run(
            todayKey(),
            existing.dailyPnl,
            existing.open ? 1 : 0,
            existing.reason,
            options?.by ?? "",
          );
        // Clear the latch but KEEP daily_pnl: resetting must not hand back a
        // fresh loss budget inside the same UTC day.
        this.db
          .query(
            `UPDATE risk_circuit_breaker
             SET open = 0, reason = '', updated_at = datetime('now')
             WHERE date = ?`,
          )
          .run(todayKey());
        this.observedRowDate = todayKey();
        // Audit trail: a reset re-enables trading, so it must be visible in
        // logs even though the breaker itself has no logger dependency.
        console.warn(
          `[risk] circuit breaker RESET for ${todayKey()} (daily PnL ${existing.dailyPnl.toFixed(2)}% retained, trading re-enabled)`,
        );
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
