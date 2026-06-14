import { describe, expect, it } from "bun:test";
import { Database, type SQLQueryBindings } from "bun:sqlite";
import { Effect, Layer } from "effect";
import {
  runLocalBacktest,
  type LocalBacktestConfig,
} from "./backtest-engine.ts";
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

function runBacktest<A>(
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

function baseConfig(): LocalBacktestConfig {
  return {
    exchange: "binance",
    timeframe: "1h",
    symbols: ["BTC/USDT"],
    start: new Date("2025-01-01T00:00:00Z"),
    end: new Date("2025-01-10T00:00:00Z"),
    initialCapital: "10000",
    feeRate: "0.0005",
    slippagePct: "0",
    leverage: 1,
    riskPct: "0.01",
    stopLossPct: "0.005",
    takeProfitPct: "0.01",
    trailingStopPct: "0",
    maxHoldHours: 24,
    maxOpenPositions: 1,
    fastEmaPeriod: 5,
    slowEmaPeriod: 10,
    trendEmaPeriod: 0,
    rsiPeriod: 7,
    rsiLongMax: 70,
    rsiShortMin: 30,
    rsiExitLevel: 110,
    volumeLookback: 5,
    atrPeriod: 7,
    atrMaxPct: 100,
    minVolumeRatio: 0,
    adxPeriod: 7,
    adxMin: 0,
    cooldownCandles: 0,
    minTrendDistancePct: 0,
    breakoutPeriod: 5,
  };
}

function makeFlatCandle(timestamp: Date, price: number, volume = 1): Candle {
  return {
    exchangeId: 1,
    pairId: 1,
    timeframe: "1h",
    timestamp,
    open: String(price),
    high: String(price),
    low: String(price),
    close: String(price),
    volume: String(volume),
  };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("runLocalBacktest", () => {
  it("exits a long trade on take-profit", async () => {
    const program = Effect.gen(function* () {
      const repo = yield* MarketRepository;
      const exchangeId = yield* repo.ensureExchange("binance");
      const pairId = yield* repo.ensureTradingPair("BTC/USDT", exchangeId);

      const candles: Candle[] = [];
      const start = new Date("2025-01-01T00:00:00Z");
      for (let i = 0; i < 30; i++) {
        candles.push({
          ...makeFlatCandle(new Date(start.getTime() + i * 3_600_000), 100),
          exchangeId,
          pairId,
        });
      }
      // Breakout close above prior 5-candle high.
      candles[25] = { ...candles[25], high: "100", close: "101" };
      // Next candle hits TP at 101.
      candles[26] = {
        ...candles[26],
        high: "102",
        low: "100.5",
        close: "101.5",
      };
      // Fill rest flat.
      for (let i = 27; i < 30; i++) {
        candles[i] = {
          ...candles[i],
          high: "101.5",
          low: "101.5",
          close: "101.5",
        };
      }

      yield* repo.insertCandles(candles);

      const result = yield* runLocalBacktest({
        ...baseConfig(),
        start: new Date("2025-01-01T00:00:00Z"),
        end: new Date("2025-01-02T10:00:00Z"),
      });

      expect(result.totalTrades).toBe(1);
      expect(result.trades[0].exitReason).toBe("take_profit");
      expect(Number(result.trades[0].pnl)).toBeGreaterThan(0);
    });
    await runBacktest(program);
  });

  it("exits a long trade on stop-loss", async () => {
    const program = Effect.gen(function* () {
      const repo = yield* MarketRepository;
      const exchangeId = yield* repo.ensureExchange("binance");
      const pairId = yield* repo.ensureTradingPair("BTC/USDT", exchangeId);

      const candles: Candle[] = [];
      const start = new Date("2025-01-01T00:00:00Z");
      for (let i = 0; i < 30; i++) {
        candles.push({
          ...makeFlatCandle(new Date(start.getTime() + i * 3_600_000), 100),
          exchangeId,
          pairId,
        });
      }
      candles[25] = { ...candles[25], high: "100", close: "101" };
      candles[26] = { ...candles[26], high: "100.5", low: "99", close: "99.5" };

      yield* repo.insertCandles(candles);

      const result = yield* runLocalBacktest({
        ...baseConfig(),
        start: new Date("2025-01-01T00:00:00Z"),
        end: new Date("2025-01-02T10:00:00Z"),
      });

      expect(result.totalTrades).toBe(1);
      expect(result.trades[0].exitReason).toBe("stop_loss");
      expect(Number(result.trades[0].pnl)).toBeLessThan(0);
    });
    await runBacktest(program);
  });

  it("does not rsi-exit short trades when rsiExitLevel is disabled", async () => {
    const program = Effect.gen(function* () {
      const repo = yield* MarketRepository;
      const exchangeId = yield* repo.ensureExchange("binance");
      const pairId = yield* repo.ensureTradingPair("BTC/USDT", exchangeId);

      const candles: Candle[] = [];
      const start = new Date("2025-01-01T00:00:00Z");
      for (let i = 0; i < 30; i++) {
        candles.push({
          ...makeFlatCandle(new Date(start.getTime() + i * 3_600_000), 100),
          exchangeId,
          pairId,
        });
      }
      // Breakout down.
      candles[25] = { ...candles[25], low: "100", close: "99" };
      // TP at 98.01 is reached.
      candles[26] = { ...candles[26], high: "99", low: "98", close: "98.2" };

      yield* repo.insertCandles(candles);

      const result = yield* runLocalBacktest({
        ...baseConfig(),
        start: new Date("2025-01-01T00:00:00Z"),
        end: new Date("2025-01-02T10:00:00Z"),
      });

      expect(result.totalTrades).toBe(1);
      expect(result.trades[0].exitReason).not.toBe("rsi_exit");
    });
    await runBacktest(program);
  });

  it("applies adverse slippage to both entry and exit", async () => {
    const program = Effect.gen(function* () {
      const repo = yield* MarketRepository;
      const exchangeId = yield* repo.ensureExchange("binance");
      const pairId = yield* repo.ensureTradingPair("BTC/USDT", exchangeId);

      const candles: Candle[] = [];
      const start = new Date("2025-01-01T00:00:00Z");
      for (let i = 0; i < 30; i++) {
        candles.push({
          ...makeFlatCandle(new Date(start.getTime() + i * 3_600_000), 100),
          exchangeId,
          pairId,
        });
      }
      candles[25] = { ...candles[25], high: "100", close: "101" };
      candles[26] = {
        ...candles[26],
        high: "102",
        low: "100.5",
        close: "101.5",
      };
      for (let i = 27; i < 30; i++) {
        candles[i] = {
          ...candles[i],
          high: "101.5",
          low: "101.5",
          close: "101.5",
        };
      }

      yield* repo.insertCandles(candles);

      const noSlip = yield* runLocalBacktest({
        ...baseConfig(),
        slippagePct: "0",
      });
      const withSlip = yield* runLocalBacktest({
        ...baseConfig(),
        slippagePct: "0.001",
      });

      expect(withSlip.totalTrades).toBe(noSlip.totalTrades);
      expect(Number(withSlip.finalCapital)).toBeLessThan(
        Number(noSlip.finalCapital),
      );
    });
    await runBacktest(program);
  });

  it("exits a trade via time-stop when neither sl nor tp is hit", async () => {
    const program = Effect.gen(function* () {
      const repo = yield* MarketRepository;
      const exchangeId = yield* repo.ensureExchange("binance");
      const pairId = yield* repo.ensureTradingPair("BTC/USDT", exchangeId);

      const candles: Candle[] = [];
      const start = new Date("2025-01-01T00:00:00Z");
      for (let i = 0; i < 50; i++) {
        candles.push({
          ...makeFlatCandle(new Date(start.getTime() + i * 3_600_000), 100),
          exchangeId,
          pairId,
        });
      }
      candles[25] = { ...candles[25], high: "100", close: "101" };
      // Price stays inside SL/TP corridor for the next 24 candles.
      for (let i = 26; i < 50; i++) {
        candles[i] = {
          ...candles[i],
          high: "100.8",
          low: "100.2",
          close: "100.5",
        };
      }

      yield* repo.insertCandles(candles);

      const result = yield* runLocalBacktest({
        ...baseConfig(),
        maxHoldHours: 2,
        start: new Date("2025-01-01T00:00:00Z"),
        end: new Date("2025-01-03T10:00:00Z"),
      });

      expect(result.totalTrades).toBe(1);
      expect(result.trades[0].exitReason).toBe("time_stop");
    });
    await runBacktest(program);
  });
});
