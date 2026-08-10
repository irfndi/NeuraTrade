import { Context, Effect, Layer } from "effect";
import type { Database } from "bun:sqlite";

export class KillSwitchError {
  readonly _tag = "KillSwitchError" as const;
  constructor(
    readonly reason: string,
    readonly cause?: unknown,
  ) {}
}

/**
 * Kill switch port. Fail-closed contract: callers MUST abort trading on any
 * KillSwitchError, and unknown/unreadable state must be treated as engaged.
 */
export interface KillSwitchService {
  readonly engage: (
    reason: string,
  ) => Effect.Effect<void, KillSwitchError, never>;
  readonly disengage: () => Effect.Effect<void, KillSwitchError, never>;
  readonly isEngaged: () => Effect.Effect<boolean, KillSwitchError, never>;
  readonly getReason: () => Effect.Effect<string, KillSwitchError, never>;
}

export const KillSwitch = Context.Service<KillSwitchService>("KillSwitch");

const ensureTableSQL = `
CREATE TABLE IF NOT EXISTS risk_kill_switch (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  engaged BOOLEAN NOT NULL DEFAULT 0,
  reason TEXT NOT NULL DEFAULT '',
  updated_at DATETIME NOT NULL
);
`;

const ensureRowSQL = `
INSERT INTO risk_kill_switch (id, engaged, reason, updated_at)
VALUES (1, 0, '', datetime('now'))
ON CONFLICT(id) DO NOTHING;
`;

/**
 * SQLite-backed kill switch with an in-process write-through mirror.
 *
 * Fail-closed contract (live soaks depend on this): callers MUST abort
 * trading on any KillSwitchError, and must treat an unreadable/missing
 * persisted state as engaged (unknown => block). The in-process mirror is
 * set synchronously on engage BEFORE the DB write, so a failed persist still
 * leaves this process blocking; disengage only clears the mirror after a
 * confirmed DB write, so a failed disengage stays engaged.
 */
class KillSwitchSQLite implements KillSwitchService {
  /** Last known engaged state; null until first confirmed read/write. */
  private mirror: boolean | null = null;

  constructor(private readonly db: Database) {
    this.db.exec(ensureTableSQL);
    this.db.exec(ensureRowSQL);
  }

  engage(reason: string): Effect.Effect<void, KillSwitchError, never> {
    return Effect.try({
      try: () => {
        // Fail closed: reflect the intent in-process before touching the DB.
        this.mirror = true;
        this.db
          .query(
            `UPDATE risk_kill_switch
             SET engaged = 1, reason = ?, updated_at = datetime('now')
             WHERE id = 1`,
          )
          .run(reason);
      },
      catch: (err) =>
        new KillSwitchError(
          `Failed to engage kill switch: ${err instanceof Error ? err.message : String(err)}`,
          err,
        ),
    });
  }

  disengage(): Effect.Effect<void, KillSwitchError, never> {
    return Effect.try({
      try: () => {
        this.db
          .query(
            `UPDATE risk_kill_switch
             SET engaged = 0, reason = '', updated_at = datetime('now')
             WHERE id = 1`,
          )
          .run();
        // Only clear the mirror after the write succeeds; a failed disengage
        // must leave the switch engaged (fail closed).
        this.mirror = false;
      },
      catch: (err) =>
        new KillSwitchError(
          `Failed to disengage kill switch: ${err instanceof Error ? err.message : String(err)}`,
          err,
        ),
    });
  }

  isEngaged(): Effect.Effect<boolean, KillSwitchError, never> {
    return Effect.try({
      try: () => {
        if (this.mirror !== null) return this.mirror;
        const row = this.db
          .query("SELECT engaged FROM risk_kill_switch WHERE id = 1")
          .get() as { engaged: number } | null;
        if (!row) {
          // Engaged-unknown: the row should exist (constructor ensures it).
          // Fail closed instead of reading a deleted state as disengaged.
          throw new Error("kill switch row missing; treating as engaged");
        }
        this.mirror = row.engaged === 1;
        return this.mirror;
      },
      catch: (err) =>
        new KillSwitchError(
          `Failed to read kill switch: ${err instanceof Error ? err.message : String(err)}`,
          err,
        ),
    });
  }

  getReason(): Effect.Effect<string, KillSwitchError, never> {
    return Effect.try({
      try: () => {
        const row = this.db
          .query("SELECT reason FROM risk_kill_switch WHERE id = 1")
          .get() as { reason: string } | null;
        return row?.reason ?? "";
      },
      catch: (err) =>
        new KillSwitchError(
          `Failed to read kill switch reason: ${err instanceof Error ? err.message : String(err)}`,
          err,
        ),
    });
  }
}

export const KillSwitchSQLiteLive = (
  db: Database,
): Layer.Layer<KillSwitchService> =>
  Layer.succeed(KillSwitch, new KillSwitchSQLite(db) as KillSwitchService);

export function makeKillSwitchService(db: Database): KillSwitchService {
  return new KillSwitchSQLite(db);
}
