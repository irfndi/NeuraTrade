import { describe, expect, it } from "bun:test";
import { Database, type SQLQueryBindings } from "bun:sqlite";
import { Effect, Layer } from "effect";
import {
  MarketRepository,
  MarketRepositoryLive,
  type Candle,
} from "./market-repository.ts";
import { SqliteClient, type SqliteClientImpl } from "./sqlite.ts";

// ---------------------------------------------------------------------------
// Test harness
// ---------------------------------------------------------------------------

function createTestSqliteClient(): SqliteClientImpl {
  const db = new Database(":memory:");

  const schema = `
    PRAGMA foreign_keys = ON;

    CREATE TABLE IF NOT EXISTS exchanges (
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
    );

    CREATE TABLE IF NOT EXISTS trading_pairs (
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
    );

    CREATE TABLE IF NOT EXISTS ohlcv_data (
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
    );
  `;
  db.exec(schema);

  const runDb = db.run.bind(db) as (
    sql: string,
    ...bindings: Array<unknown>
  ) => { changes: number; lastInsertRowid: number | bigint };

  return {
    queryOne: <T>(sql: string, params: ReadonlyArray<SQLQueryBindings> = []) =>
      Effect.sync(
        () =>
          db
            .query<T, Array<SQLQueryBindings>>(sql)
            .get(...(params as Array<SQLQueryBindings>)) as T | null,
      ),
    queryAll: <T>(sql: string, params: ReadonlyArray<SQLQueryBindings> = []) =>
      Effect.sync(
        () =>
          db
            .query<T, Array<SQLQueryBindings>>(sql)
            .all(...(params as Array<SQLQueryBindings>)) as ReadonlyArray<T>,
      ),
    execute: (sql: string, params: ReadonlyArray<SQLQueryBindings> = []) =>
      Effect.sync(() => {
        const result = runDb(sql, ...(params as Array<SQLQueryBindings>));
        return {
          changes: result.changes,
          lastInsertRowId:
            typeof result.lastInsertRowid === "bigint"
              ? Number(result.lastInsertRowid)
              : result.lastInsertRowid,
        };
      }),
    exec: (sql: string) => Effect.sync(() => db.exec(sql)),
    close: Effect.sync(() => db.close()),
  };
}

function runRepo<A>(
  effect: Effect.Effect<A, unknown, MarketRepository>,
): Promise<A> {
  const sqlite = createTestSqliteClient();
  return Effect.runPromise(
    effect.pipe(
      Effect.provide(MarketRepositoryLive),
      Effect.provide(Layer.succeed(SqliteClient, sqlite)),
    ) as Effect.Effect<A, never>,
  );
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("MarketRepository", () => {
  it("ensureExchange inserts and idempotently returns the same id", async () => {
    const program = Effect.gen(function* () {
      const repo = yield* MarketRepository;
      const id1 = yield* repo.ensureExchange("binance");
      const id2 = yield* repo.ensureExchange("Binance");
      const id3 = yield* repo.ensureExchange("coinbase");
      expect(id1).toBeGreaterThan(0);
      expect(id2).toBe(id1);
      expect(id3).not.toBe(id1);
    });
    await runRepo(program);
  });

  it("ensureTradingPair normalizes and idempotently returns the same id", async () => {
    const program = Effect.gen(function* () {
      const repo = yield* MarketRepository;
      const exchangeId = yield* repo.ensureExchange("binance");
      const id1 = yield* repo.ensureTradingPair("BTC/USDT", exchangeId);
      const id2 = yield* repo.ensureTradingPair("BTCUSDT", exchangeId);
      const id3 = yield* repo.ensureTradingPair("ETH/USDT", exchangeId);
      expect(id1).toBeGreaterThan(0);
      expect(id2).toBe(id1);
      expect(id3).not.toBe(id1);
    });
    await runRepo(program);
  });

  it("insertCandles stores candles and getCandleRange reports them", async () => {
    const program = Effect.gen(function* () {
      const repo = yield* MarketRepository;
      const exchangeId = yield* repo.ensureExchange("binance");
      const pairId = yield* repo.ensureTradingPair("BTC/USDT", exchangeId);

      const candles: ReadonlyArray<Candle> = [
        {
          exchangeId,
          pairId,
          timeframe: "1h",
          timestamp: new Date("2025-01-01T00:00:00Z"),
          open: "42000.00",
          high: "42100.00",
          low: "41900.00",
          close: "42050.00",
          volume: "1.5",
        },
        {
          exchangeId,
          pairId,
          timeframe: "1h",
          timestamp: new Date("2025-01-01T01:00:00Z"),
          open: "42050.00",
          high: "42200.00",
          low: "42000.00",
          close: "42150.00",
          volume: "2.0",
        },
      ];

      yield* repo.insertCandles(candles);

      const range = yield* repo.getCandleRange({
        exchangeId,
        pairId,
        timeframe: "1h",
        start: new Date("2025-01-01T00:00:00Z"),
        end: new Date("2025-01-01T02:00:00Z"),
      });

      expect(range.count).toBe(2);
      expect(range.earliest?.toISOString()).toBe("2025-01-01T00:00:00.000Z");
      expect(range.latest?.toISOString()).toBe("2025-01-01T01:00:00.000Z");
    });
    await runRepo(program);
  });

  it("findCoverageGaps detects missing candles", async () => {
    const program = Effect.gen(function* () {
      const repo = yield* MarketRepository;
      const exchangeId = yield* repo.ensureExchange("binance");
      const pairId = yield* repo.ensureTradingPair("BTC/USDT", exchangeId);

      const candles: ReadonlyArray<Candle> = [
        {
          exchangeId,
          pairId,
          timeframe: "1h",
          timestamp: new Date("2025-01-01T00:00:00Z"),
          open: "1",
          high: "2",
          low: "1",
          close: "2",
          volume: "1",
        },
        {
          exchangeId,
          pairId,
          timeframe: "1h",
          timestamp: new Date("2025-01-01T03:00:00Z"),
          open: "1",
          high: "2",
          low: "1",
          close: "2",
          volume: "1",
        },
      ];

      yield* repo.insertCandles(candles);

      const gaps = yield* repo.findCoverageGaps({
        exchangeId,
        pairId,
        timeframe: "1h",
        start: new Date("2025-01-01T00:00:00Z"),
        end: new Date("2025-01-01T04:00:00Z"),
        intervalMs: 3_600_000,
      });

      expect(gaps).toHaveLength(1);
      expect(gaps[0].from.toISOString()).toBe("2025-01-01T01:00:00.000Z");
      expect(gaps[0].to.toISOString()).toBe("2025-01-01T03:00:00.000Z");
    });
    await runRepo(program);
  });

  it("listKnownSymbols returns distinct symbols", async () => {
    const program = Effect.gen(function* () {
      const repo = yield* MarketRepository;
      const exchangeId = yield* repo.ensureExchange("binance");
      yield* repo.ensureTradingPair("BTC/USDT", exchangeId);
      yield* repo.ensureTradingPair("ETH/USDT", exchangeId);
      yield* repo.ensureTradingPair("BTCUSDT", exchangeId); // duplicate normalized

      const symbols = yield* repo.listKnownSymbols();
      expect(symbols).toEqual(["BTC/USDT", "ETH/USDT"]);
    });
    await runRepo(program);
  });
});
