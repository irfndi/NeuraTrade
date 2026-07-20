import { Context, Effect, Layer } from "effect";
import type { Database } from "bun:sqlite";

export class KillSwitchError {
  readonly _tag = "KillSwitchError" as const;
  constructor(
    readonly reason: string,
    readonly cause?: unknown,
  ) {}
}

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

class KillSwitchSQLite implements KillSwitchService {
  constructor(private readonly db: Database) {
    this.db.exec(ensureTableSQL);
    this.db.exec(ensureRowSQL);
  }

  engage(reason: string): Effect.Effect<void, KillSwitchError, never> {
    return Effect.try({
      try: () => {
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
        const row = this.db
          .query("SELECT engaged FROM risk_kill_switch WHERE id = 1")
          .get() as { engaged: number } | null;
        return row ? row.engaged === 1 : false;
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
