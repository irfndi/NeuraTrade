/**
 * Paper-trading repository — stores simulated trades in SQLite.
 *
 * Mirrors the backend paper_trades_cli table and supports the minimum lifecycle
 * needed for a deterministic scalping paper-trading loop.
 */
import { Context, Data, Effect, Layer } from "effect";
import { SqliteClient, SqliteError } from "./sqlite.ts";

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

export class PaperRepositoryError extends Data.TaggedError(
  "PaperRepositoryError",
)<{
  readonly message: string;
  readonly cause?: unknown;
}> {}

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface PaperTrade {
  readonly id: number;
  readonly symbol: string;
  readonly exchange: string;
  readonly side: "buy" | "sell";
  readonly size: string;
  readonly notional: string;
  readonly entry_price: string;
  readonly exit_price: string | null;
  readonly entry_at: string;
  readonly exit_at: string | null;
  readonly pnl: string | null;
  readonly pnl_pct: string | null;
  readonly fees: string | null;
  readonly status: "open" | "closed";
  readonly exit_reason: string | null;
  readonly signal_id: string | null;
  readonly mode: string;
}

export interface OpenTradeInput {
  readonly symbol: string;
  readonly exchange: string;
  readonly side: "buy" | "sell";
  readonly size: string;
  readonly notional: string;
  readonly entry_price: string;
  readonly entry_at: string;
  readonly signal_id?: string;
  readonly mode?: string;
}

export interface CloseTradeInput {
  readonly id: number;
  readonly exit_price: string;
  readonly exit_at: string;
  readonly pnl: string;
  readonly pnl_pct: string;
  readonly fees: string;
  readonly exit_reason: string;
}

// ---------------------------------------------------------------------------
// Service interface
// ---------------------------------------------------------------------------

export interface PaperRepositoryImpl {
  readonly openTrade: (
    input: OpenTradeInput,
  ) => Effect.Effect<number, PaperRepositoryError>;
  readonly closeTrade: (
    input: CloseTradeInput,
  ) => Effect.Effect<void, PaperRepositoryError>;
  readonly getOpenTrade: (
    symbol: string,
    exchange: string,
  ) => Effect.Effect<PaperTrade | null, PaperRepositoryError>;
  readonly listOpenTrades: () => Effect.Effect<
    ReadonlyArray<PaperTrade>,
    PaperRepositoryError
  >;
  readonly listClosedTrades: (
    limit?: number,
  ) => Effect.Effect<ReadonlyArray<PaperTrade>, PaperRepositoryError>;
  readonly getStats: () => Effect.Effect<
    {
      readonly open_count: number;
      readonly closed_count: number;
      readonly total_pnl: string;
      readonly win_count: number;
      readonly loss_count: number;
    },
    PaperRepositoryError
  >;
}

export class PaperRepository extends Context.Tag("PaperRepository")<
  PaperRepository,
  PaperRepositoryImpl
>() {}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function mapRow(row: Record<string, unknown>): PaperTrade {
  return {
    id: Number(row.id),
    symbol: String(row.symbol),
    exchange: String(row.exchange),
    side: String(row.side) as "buy" | "sell",
    size: String(row.size),
    notional: String(row.notional),
    entry_price: String(row.entry_price),
    exit_price:
      row.exit_price === null || row.exit_price === undefined
        ? null
        : String(row.exit_price),
    entry_at: String(row.entry_at),
    exit_at:
      row.exit_at === null || row.exit_at === undefined
        ? null
        : String(row.exit_at),
    pnl: row.pnl === null || row.pnl === undefined ? null : String(row.pnl),
    pnl_pct:
      row.pnl_pct === null || row.pnl_pct === undefined
        ? null
        : String(row.pnl_pct),
    fees: row.fees === null || row.fees === undefined ? null : String(row.fees),
    status: String(row.status) as "open" | "closed",
    exit_reason:
      row.exit_reason === null || row.exit_reason === undefined
        ? null
        : String(row.exit_reason),
    signal_id:
      row.signal_id === null || row.signal_id === undefined
        ? null
        : String(row.signal_id),
    mode: String(row.mode),
  };
}

function handleSqliteError(cause: unknown): PaperRepositoryError {
  if (cause instanceof SqliteError) {
    return new PaperRepositoryError({ message: cause.message, cause });
  }
  return new PaperRepositoryError({
    message: cause instanceof Error ? cause.message : String(cause),
    cause,
  });
}

// ---------------------------------------------------------------------------
// Live layer
// ---------------------------------------------------------------------------

export const PaperRepositoryLive: Layer.Layer<
  PaperRepository,
  never,
  SqliteClient
> = Layer.effect(
  PaperRepository,
  Effect.gen(function* () {
    const db = yield* SqliteClient;

    const openTrade = (
      input: OpenTradeInput,
    ): Effect.Effect<number, PaperRepositoryError> =>
      Effect.gen(function* () {
        const result = yield* db.execute(
          `INSERT INTO paper_trades_cli
             (symbol, exchange, side, size, notional, entry_price, entry_at, signal_id, mode, status)
             VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'open')`,
          [
            input.symbol,
            input.exchange,
            input.side,
            input.size,
            input.notional,
            input.entry_price,
            input.entry_at,
            input.signal_id ?? null,
            input.mode ?? "deterministic",
          ],
        );
        return Number(result.lastInsertRowId);
      }).pipe(Effect.catchAll((err) => Effect.fail(handleSqliteError(err))));

    const closeTrade = (
      input: CloseTradeInput,
    ): Effect.Effect<void, PaperRepositoryError> =>
      Effect.gen(function* () {
        yield* db.execute(
          `UPDATE paper_trades_cli
             SET exit_price = ?, exit_at = ?, pnl = ?, pnl_pct = ?, fees = ?,
                 status = 'closed', exit_reason = ?, updated_at = CURRENT_TIMESTAMP
             WHERE id = ?`,
          [
            input.exit_price,
            input.exit_at,
            input.pnl,
            input.pnl_pct,
            input.fees,
            input.exit_reason,
            input.id,
          ],
        );
      }).pipe(Effect.catchAll((err) => Effect.fail(handleSqliteError(err))));

    const getOpenTrade = (
      symbol: string,
      exchange: string,
    ): Effect.Effect<PaperTrade | null, PaperRepositoryError> =>
      db
        .queryOne<
          Record<string, unknown>
        >(`SELECT * FROM paper_trades_cli WHERE symbol = ? AND exchange = ? AND status = 'open' ORDER BY id DESC LIMIT 1`, [symbol, exchange])
        .pipe(
          Effect.map((row) => (row === null ? null : mapRow(row))),
          Effect.catchAll((err) => Effect.fail(handleSqliteError(err))),
        );

    const listOpenTrades = (): Effect.Effect<
      ReadonlyArray<PaperTrade>,
      PaperRepositoryError
    > =>
      db
        .queryAll<
          Record<string, unknown>
        >(`SELECT * FROM paper_trades_cli WHERE status = 'open' ORDER BY entry_at DESC`)
        .pipe(
          Effect.map((rows) => rows.map(mapRow)),
          Effect.catchAll((err) => Effect.fail(handleSqliteError(err))),
        );

    const listClosedTrades = (
      limit = 50,
    ): Effect.Effect<ReadonlyArray<PaperTrade>, PaperRepositoryError> =>
      db
        .queryAll<
          Record<string, unknown>
        >(`SELECT * FROM paper_trades_cli WHERE status = 'closed' ORDER BY exit_at DESC LIMIT ?`, [limit])
        .pipe(
          Effect.map((rows) => rows.map(mapRow)),
          Effect.catchAll((err) => Effect.fail(handleSqliteError(err))),
        );

    const getStats = (): Effect.Effect<
      {
        readonly open_count: number;
        readonly closed_count: number;
        readonly total_pnl: string;
        readonly win_count: number;
        readonly loss_count: number;
      },
      PaperRepositoryError
    > =>
      db
        .queryOne<{
          open_count: number;
          closed_count: number;
          total_pnl: string | null;
          win_count: number;
          loss_count: number;
        }>(
          `SELECT
            SUM(CASE WHEN status = 'open' THEN 1 ELSE 0 END) AS open_count,
            SUM(CASE WHEN status = 'closed' THEN 1 ELSE 0 END) AS closed_count,
            SUM(CASE WHEN status = 'closed' THEN CAST(pnl AS REAL) ELSE 0 END) AS total_pnl,
            SUM(CASE WHEN status = 'closed' AND CAST(pnl AS REAL) > 0 THEN 1 ELSE 0 END) AS win_count,
            SUM(CASE WHEN status = 'closed' AND CAST(pnl AS REAL) <= 0 THEN 1 ELSE 0 END) AS loss_count
          FROM paper_trades_cli`,
        )
        .pipe(
          Effect.map((row) => ({
            open_count: Number(row?.open_count ?? 0),
            closed_count: Number(row?.closed_count ?? 0),
            total_pnl: String(row?.total_pnl ?? "0"),
            win_count: Number(row?.win_count ?? 0),
            loss_count: Number(row?.loss_count ?? 0),
          })),
          Effect.catchAll((err) => Effect.fail(handleSqliteError(err))),
        );

    return {
      openTrade,
      closeTrade,
      getOpenTrade,
      listOpenTrades,
      listClosedTrades,
      getStats,
    };
  }),
);
