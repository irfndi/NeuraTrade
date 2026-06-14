/**
 * SQLite client service — wraps `bun:sqlite` in Effect-TS.
 *
 * Opens the database at the resolved NeuraTrade path and exposes typed,
 * effectful query helpers plus a close effect.
 */
import * as nodePath from "path";
import { Database, type Changes, type SQLQueryBindings } from "bun:sqlite";
import { Context, Data, Effect, Layer } from "effect";
import { FileSystem } from "@effect/platform";
import { Path } from "./path.ts";
import { RuntimeConfig } from "./config.ts";

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

export class SqliteError extends Data.TaggedError("SqliteError")<{
  readonly message: string;
  readonly sql?: string;
  readonly cause: unknown;
}> {}

// ---------------------------------------------------------------------------
// Service interface
// ---------------------------------------------------------------------------

export interface SqliteClientImpl {
  readonly queryOne: <T>(
    sql: string,
    params?: ReadonlyArray<SQLQueryBindings>,
  ) => Effect.Effect<T | null, SqliteError>;

  readonly queryAll: <T>(
    sql: string,
    params?: ReadonlyArray<SQLQueryBindings>,
  ) => Effect.Effect<ReadonlyArray<T>, SqliteError>;

  readonly execute: (
    sql: string,
    params?: ReadonlyArray<SQLQueryBindings>,
  ) => Effect.Effect<
    { readonly changes: number; readonly lastInsertRowId: number },
    SqliteError
  >;

  readonly exec: (sql: string) => Effect.Effect<void, SqliteError>;

  readonly close: Effect.Effect<void, never>;
}

export class SqliteClient extends Context.Tag("SqliteClient")<
  SqliteClient,
  SqliteClientImpl
>() {}

// ---------------------------------------------------------------------------
// Schema bootstrap (matches backend migrations)
// ---------------------------------------------------------------------------

const SCHEMA_STATEMENTS: ReadonlyArray<string> = [
  "PRAGMA foreign_keys = ON",
  `CREATE TABLE IF NOT EXISTS exchanges (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,
    ccxt_id TEXT NOT NULL UNIQUE,
    api_url TEXT,
    status TEXT DEFAULT 'active',
    has_spot BOOLEAN DEFAULT 1,
    has_futures BOOLEAN DEFAULT 0,
    is_active BOOLEAN DEFAULT 1,
    priority INTEGER DEFAULT 0,
    last_ping DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
  )`,
  `CREATE TABLE IF NOT EXISTS trading_pairs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    exchange_id INTEGER NOT NULL,
    symbol TEXT NOT NULL,
    base_currency TEXT NOT NULL,
    quote_currency TEXT NOT NULL,
    is_active BOOLEAN DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (exchange_id) REFERENCES exchanges(id) ON DELETE CASCADE,
    UNIQUE(exchange_id, symbol)
  )`,
  `CREATE INDEX IF NOT EXISTS idx_exchanges_name ON exchanges(name)`,
  `CREATE INDEX IF NOT EXISTS idx_exchanges_ccxt_id ON exchanges(ccxt_id)`,
  `CREATE INDEX IF NOT EXISTS idx_trading_pairs_symbol ON trading_pairs(symbol)`,
  `CREATE INDEX IF NOT EXISTS idx_trading_pairs_exchange ON trading_pairs(exchange_id)`,
  `CREATE TABLE IF NOT EXISTS ohlcv_data (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    exchange_id INTEGER NOT NULL,
    trading_pair_id INTEGER NOT NULL,
    timeframe TEXT NOT NULL,
    open_price NUMERIC NOT NULL,
    high_price NUMERIC NOT NULL,
    low_price NUMERIC NOT NULL,
    close_price NUMERIC NOT NULL,
    volume NUMERIC NOT NULL,
    timestamp DATETIME NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (exchange_id) REFERENCES exchanges(id) ON DELETE CASCADE,
    FOREIGN KEY (trading_pair_id) REFERENCES trading_pairs(id) ON DELETE CASCADE,
    UNIQUE(exchange_id, trading_pair_id, timeframe, timestamp)
  )`,
  `CREATE INDEX IF NOT EXISTS idx_ohlcv_data_exchange_pair_timeframe_timestamp
    ON ohlcv_data(exchange_id, trading_pair_id, timeframe, timestamp DESC)`,
  `CREATE TABLE IF NOT EXISTS paper_trades_cli (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    symbol TEXT NOT NULL,
    exchange TEXT NOT NULL,
    side TEXT NOT NULL,
    size TEXT NOT NULL,
    notional TEXT NOT NULL,
    entry_price TEXT NOT NULL,
    exit_price TEXT,
    entry_at DATETIME NOT NULL,
    exit_at DATETIME,
    pnl TEXT,
    pnl_pct TEXT,
    fees TEXT,
    status TEXT NOT NULL DEFAULT 'open',
    exit_reason TEXT,
    signal_id TEXT,
    mode TEXT NOT NULL DEFAULT 'deterministic',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
  )`,
  `CREATE INDEX IF NOT EXISTS idx_paper_trades_cli_symbol_status ON paper_trades_cli(symbol, status)`,
  `CREATE INDEX IF NOT EXISTS idx_paper_trades_cli_status ON paper_trades_cli(status)`,
];

function runSql(
  db: Database,
  sql: string,
  params: ReadonlyArray<SQLQueryBindings>,
): { readonly changes: number; readonly lastInsertRowId: number } {
  const result = (
    db.run as (sql: string, ...bindings: Array<SQLQueryBindings>) => Changes
  )(sql, ...(params as Array<SQLQueryBindings>));
  return {
    changes: result.changes,
    lastInsertRowId:
      typeof result.lastInsertRowid === "bigint"
        ? Number(result.lastInsertRowid)
        : result.lastInsertRowid,
  };
}

function initSchema(db: Database): Effect.Effect<void, SqliteError> {
  return Effect.gen(function* () {
    for (const sql of SCHEMA_STATEMENTS) {
      yield* Effect.try({
        try: () => db.run(sql),
        catch: (cause) =>
          new SqliteError({
            message: `Failed to initialize schema: ${String(cause)}`,
            sql,
            cause,
          }),
      });
    }
  });
}

// ---------------------------------------------------------------------------
// Live layer (scoped)
// ---------------------------------------------------------------------------

export const SqliteClientLive: Layer.Layer<
  SqliteClient,
  SqliteError,
  Path | RuntimeConfig | FileSystem.FileSystem
> = Layer.scoped(
  SqliteClient,
  Effect.gen(function* () {
    const path = yield* Path;
    const runtimeConfig = yield* RuntimeConfig;
    const fs = yield* FileSystem.FileSystem;

    const dbPath =
      runtimeConfig.database.sqlite_path &&
      runtimeConfig.database.sqlite_path.length > 0
        ? runtimeConfig.database.sqlite_path
        : nodePath.join(path.homeDir, "data", "neuratrade.db");

    yield* fs.makeDirectory(nodePath.dirname(dbPath), { recursive: true }).pipe(
      Effect.catchAll((cause) =>
        Effect.fail(
          new SqliteError({
            message: `Failed to create data directory: ${String(cause)}`,
            cause,
          }),
        ),
      ),
    );

    const db = yield* Effect.acquireRelease(
      Effect.try({
        try: () => new Database(dbPath, { create: true }),
        catch: (cause) =>
          new SqliteError({
            message: `Failed to open database at ${dbPath}: ${String(cause)}`,
            cause,
          }),
      }),
      (database) => Effect.sync(() => database.close()),
    );

    yield* initSchema(db);

    const queryOne = <T>(
      sql: string,
      params: ReadonlyArray<SQLQueryBindings> = [],
    ): Effect.Effect<T | null, SqliteError> =>
      Effect.try({
        try: () =>
          db
            .query<T, Array<SQLQueryBindings>>(sql)
            .get(...(params as Array<SQLQueryBindings>)) as T | null,
        catch: (cause) =>
          new SqliteError({ message: String(cause), sql, cause }),
      });

    const queryAll = <T>(
      sql: string,
      params: ReadonlyArray<SQLQueryBindings> = [],
    ): Effect.Effect<ReadonlyArray<T>, SqliteError> =>
      Effect.try({
        try: () =>
          db
            .query<T, Array<SQLQueryBindings>>(sql)
            .all(...(params as Array<SQLQueryBindings>)) as ReadonlyArray<T>,
        catch: (cause) =>
          new SqliteError({ message: String(cause), sql, cause }),
      });

    const execute = (
      sql: string,
      params: ReadonlyArray<SQLQueryBindings> = [],
    ): Effect.Effect<
      { readonly changes: number; readonly lastInsertRowId: number },
      SqliteError
    > =>
      Effect.try({
        try: () => runSql(db, sql, params),
        catch: (cause) =>
          new SqliteError({ message: String(cause), sql, cause }),
      });

    const exec = (sql: string): Effect.Effect<void, SqliteError> =>
      Effect.try({
        try: () => {
          db.run(sql);
        },
        catch: (cause) =>
          new SqliteError({ message: String(cause), sql, cause }),
      });

    const close = Effect.sync(() => db.close());

    return {
      queryOne,
      queryAll,
      execute,
      exec,
      close,
    } satisfies SqliteClientImpl;
  }),
);
