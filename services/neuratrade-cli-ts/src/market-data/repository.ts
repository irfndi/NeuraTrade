import { Context, Effect, Layer } from "effect";
import { Database } from "bun:sqlite";
import type { Candle, Tick } from "./types.js";

/**
 * Error raised when market-data persistence fails.
 */
export class MarketDataRepositoryError {
  readonly _tag = "MarketDataRepositoryError" as const;
  constructor(
    readonly reason: string,
    readonly cause?: unknown,
  ) {}
}

/**
 * Query parameters for historical candle retrieval.
 */
export interface CandleQuery {
  readonly exchange: string;
  readonly symbol: string;
  readonly timeframe: string;
  readonly from?: Date;
  readonly to?: Date;
  readonly limit?: number;
}

/**
 * Port for persisting and retrieving market data.
 */
export interface MarketDataRepositoryService {
  readonly saveTick: (tick: Tick) => Effect.Effect<void, MarketDataRepositoryError, never>;

  readonly saveCandles: (
    candles: readonly Candle[],
  ) => Effect.Effect<number, MarketDataRepositoryError, never>;

  readonly getCandles: (
    query: CandleQuery,
  ) => Effect.Effect<readonly Candle[], MarketDataRepositoryError, never>;

  readonly getLatestTick: (
    exchange: string,
    symbol: string,
  ) => Effect.Effect<Tick | null, MarketDataRepositoryError, never>;

  readonly listSymbols: (
    exchange: string,
    timeframe: string,
    minCandles?: number,
  ) => Effect.Effect<readonly string[], MarketDataRepositoryError, never>;

  readonly ensureTables: () => Effect.Effect<void, MarketDataRepositoryError, never>;
}

export const MarketDataRepository = Context.GenericTag<MarketDataRepositoryService>(
  "MarketDataRepository",
);

// ---------------------------------------------------------------------------
// SQLite implementation
// ---------------------------------------------------------------------------

const ensureTablesSQL = `
CREATE TABLE IF NOT EXISTS exchanges (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT UNIQUE NOT NULL,
  api_url TEXT NOT NULL DEFAULT '',
  is_active INTEGER DEFAULT 1,
  last_ping DATETIME,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS trading_pairs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  symbol TEXT UNIQUE NOT NULL,
  base_currency TEXT NOT NULL DEFAULT '',
  quote_currency TEXT NOT NULL DEFAULT '',
  is_futures INTEGER DEFAULT 0,
  is_active INTEGER DEFAULT 1,
  volume_24h REAL DEFAULT 0,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS market_data (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  exchange_id INTEGER REFERENCES exchanges(id),
  trading_pair_id INTEGER REFERENCES trading_pairs(id),
  price REAL NOT NULL,
  volume REAL NOT NULL DEFAULT 0,
  bid REAL,
  ask REAL,
  high_24h REAL,
  low_24h REAL,
  volume_24h REAL,
  timestamp DATETIME NOT NULL,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_market_data_timestamp ON market_data(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_market_data_exchange_pair ON market_data(exchange_id, trading_pair_id);

CREATE TABLE IF NOT EXISTS ohlcv_data (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  exchange_id INTEGER NOT NULL REFERENCES exchanges(id),
  trading_pair_id INTEGER NOT NULL REFERENCES trading_pairs(id),
  timeframe TEXT NOT NULL,
  open_price REAL NOT NULL,
  high_price REAL NOT NULL,
  low_price REAL NOT NULL,
  close_price REAL NOT NULL,
  volume REAL NOT NULL,
  timestamp DATETIME NOT NULL,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(exchange_id, trading_pair_id, timeframe, timestamp)
);

CREATE INDEX IF NOT EXISTS idx_ohlcv_pair_timeframe ON ohlcv_data(trading_pair_id, timeframe);
CREATE INDEX IF NOT EXISTS idx_ohlcv_timestamp ON ohlcv_data(timestamp DESC);
`;

export class MarketDataRepositorySQLite implements MarketDataRepositoryService {
  constructor(private readonly db: Database) {}

  ensureTables(): Effect.Effect<void, MarketDataRepositoryError, never> {
    return Effect.try({
      try: () => {
        this.db.exec(ensureTablesSQL);
      },
      catch: (err) =>
        new MarketDataRepositoryError(
          `Failed to create market-data tables: ${
            err instanceof Error ? err.message : String(err)
          }`,
          err,
        ),
    });
  }

  saveTick(tick: Tick): Effect.Effect<void, MarketDataRepositoryError, never> {
    const db = this.db;
    return Effect.gen(function* () {
      const exchangeId = yield* getOrCreateExchange(tick.exchange, db);
      const pairId = yield* getOrCreateTradingPair(tick.symbol, db);

      const insert = db.query(
        `INSERT INTO market_data (exchange_id, trading_pair_id, price, volume, bid, ask, high_24h, low_24h, volume_24h, timestamp)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
      );

      return yield* Effect.try({
        try: () => {
          insert.run(
            exchangeId,
            pairId,
            tick.price,
            tick.volume,
            tick.bid ?? null,
            tick.ask ?? null,
            tick.high24h ?? null,
            tick.low24h ?? null,
            tick.volume24h ?? null,
            tick.timestamp.toISOString(),
          );
        },
        catch: (err) =>
          new MarketDataRepositoryError(
            `Failed to save tick: ${err instanceof Error ? err.message : String(err)}`,
            err,
          ),
      });
    });
  }

  saveCandles(
    candles: readonly Candle[],
  ): Effect.Effect<number, MarketDataRepositoryError, never> {
    if (candles.length === 0) {
      return Effect.succeed(0);
    }

    const db = this.db;
    return Effect.gen(function* () {
      const exchangeId = yield* getOrCreateExchange(candles[0].exchange, db);
      const pairId = yield* getOrCreateTradingPair(candles[0].symbol, db);

      const insert = db.query(
        `INSERT OR IGNORE INTO ohlcv_data
         (exchange_id, trading_pair_id, timeframe, open_price, high_price, low_price, close_price, volume, timestamp)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
      );

      return yield* Effect.try({
        try: () => {
          let saved = 0;
          for (const c of candles) {
            const result = insert.run(
              exchangeId,
              pairId,
              c.timeframe,
              c.open,
              c.high,
              c.low,
              c.close,
              c.volume,
              c.timestamp.toISOString(),
            );
            saved += Number(result.changes);
          }
          return saved;
        },
        catch: (err) =>
          new MarketDataRepositoryError(
            `Failed to save candles: ${err instanceof Error ? err.message : String(err)}`,
            err,
          ),
      });
    });
  }

  getCandles(
    query: CandleQuery,
  ): Effect.Effect<readonly Candle[], MarketDataRepositoryError, never> {
    const db = this.db;
    return Effect.gen(function* () {
      const exchangeId = yield* getOrCreateExchange(query.exchange, db);
      const pairId = yield* getOrCreateTradingPair(query.symbol, db);

      const conditions: string[] = [
        "exchange_id = ?",
        "trading_pair_id = ?",
        "timeframe = ?",
      ];
      const params: (string | number)[] = [exchangeId, pairId, query.timeframe];

      if (query.from) {
        conditions.push("timestamp >= ?");
        params.push(query.from.toISOString());
      }
      if (query.to) {
        conditions.push("timestamp <= ?");
        params.push(query.to.toISOString());
      }

      const sql = `SELECT open_price, high_price, low_price, close_price, volume, timestamp
                   FROM ohlcv_data
                   WHERE ${conditions.join(" AND ")}
                   ORDER BY timestamp ASC
                   ${query.limit ? "LIMIT ?" : ""}`;
      if (query.limit) params.push(query.limit);

      return yield* Effect.try({
        try: () => {
          const rows = db.query(sql).all(...params) as Array<{
            open_price: number;
            high_price: number;
            low_price: number;
            close_price: number;
            volume: number;
            timestamp: string;
          }>;

          return rows.map(
            (r): Candle => ({
              exchange: query.exchange,
              symbol: query.symbol,
              timeframe: query.timeframe,
              open: r.open_price,
              high: r.high_price,
              low: r.low_price,
              close: r.close_price,
              volume: r.volume,
              timestamp: new Date(r.timestamp),
            }),
          );
        },
        catch: (err) =>
          new MarketDataRepositoryError(
            `Failed to load candles: ${err instanceof Error ? err.message : String(err)}`,
            err,
          ),
      });
    });
  }

  getLatestTick(
    exchange: string,
    symbol: string,
  ): Effect.Effect<Tick | null, MarketDataRepositoryError, never> {
    const db = this.db;
    return Effect.gen(function* () {
      const exchangeId = yield* getOrCreateExchange(exchange, db);
      const pairId = yield* getOrCreateTradingPair(symbol, db);

      return yield* Effect.try({
        try: () => {
          const row = db
            .query(
              `SELECT price, volume, bid, ask, high_24h, low_24h, volume_24h, timestamp
               FROM market_data
               WHERE exchange_id = ? AND trading_pair_id = ?
               ORDER BY timestamp DESC
               LIMIT 1`,
            )
            .get(exchangeId, pairId) as {
            price: number;
            volume: number;
            bid: number | null;
            ask: number | null;
            high_24h: number | null;
            low_24h: number | null;
            volume_24h: number | null;
            timestamp: string;
          } | null;

          if (!row) return null;

          return {
            exchange,
            symbol,
            price: row.price,
            volume: row.volume,
            bid: row.bid ?? undefined,
            ask: row.ask ?? undefined,
            high24h: row.high_24h ?? undefined,
            low24h: row.low_24h ?? undefined,
            volume24h: row.volume_24h ?? undefined,
            timestamp: new Date(row.timestamp),
          };
        },
        catch: (err) =>
          new MarketDataRepositoryError(
            `Failed to load latest tick: ${err instanceof Error ? err.message : String(err)}`,
            err,
          ),
      });
    });
  }

  listSymbols(
    exchange: string,
    timeframe: string,
    minCandles = 100,
  ): Effect.Effect<readonly string[], MarketDataRepositoryError, never> {
    const db = this.db;
    return Effect.gen(function* () {
      const exchangeId = yield* getOrCreateExchange(exchange, db);

      return yield* Effect.try({
        try: () => {
          const rows = db
            .query(
              `SELECT tp.symbol, COUNT(o.id) AS candle_count
               FROM ohlcv_data o
               JOIN trading_pairs tp ON o.trading_pair_id = tp.id
               WHERE o.exchange_id = ? AND o.timeframe = ?
               GROUP BY tp.symbol
               HAVING candle_count >= ?
               ORDER BY candle_count DESC`,
            )
            .all(exchangeId, timeframe, minCandles) as Array<{ symbol: string; candle_count: number }>;

          return rows.map((r) => r.symbol);
        },
        catch: (err) =>
          new MarketDataRepositoryError(
            `Failed to list symbols: ${err instanceof Error ? err.message : String(err)}`,
            err,
          ),
      });
    });
  }
}

function getOrCreateExchange(
  name: string,
  db: Database,
): Effect.Effect<number, MarketDataRepositoryError, never> {
  return Effect.try({
    try: () => {
      const existing = db.query("SELECT id FROM exchanges WHERE name = ?").get(name) as
        | { id: number }
        | undefined;
      if (existing) return existing.id;

      const result = db
        .query("INSERT INTO exchanges (name, api_url) VALUES (?, ?)")
        .run(name, "");
      return Number(result.lastInsertRowid);
    },
    catch: (err) =>
      new MarketDataRepositoryError(
        `Exchange lookup failed for ${name}: ${err instanceof Error ? err.message : String(err)}`,
        err,
      ),
  });
}

function getOrCreateTradingPair(
  symbol: string,
  db: Database,
): Effect.Effect<number, MarketDataRepositoryError, never> {
  return Effect.gen(function* () {
    const existing = yield* Effect.try({
      try: () => db.query("SELECT id FROM trading_pairs WHERE symbol = ?").get(symbol) as
        | { id: number }
        | undefined,
      catch: (err) =>
        new MarketDataRepositoryError(
          `Trading pair lookup failed for ${symbol}: ${
            err instanceof Error ? err.message : String(err)
          }`,
          err,
        ),
    });
    if (existing) return existing.id;

    const parts = symbol.split("/");
    const base = parts[0] ?? symbol;
    const quote = parts[1] ?? "";

    return yield* Effect.try({
      try: () => {
        const result = db
          .query("INSERT INTO trading_pairs (symbol, base_currency, quote_currency) VALUES (?, ?, ?)")
          .run(symbol, base, quote);
        return Number(result.lastInsertRowid);
      },
      catch: (err) =>
        new MarketDataRepositoryError(
          `Trading pair insert failed for ${symbol}: ${
            err instanceof Error ? err.message : String(err)
          }`,
          err,
        ),
    });
  });
}

export const MarketDataRepositorySQLiteLive = (db: Database) =>
  Layer.succeed(
    MarketDataRepository,
    new MarketDataRepositorySQLite(db) as MarketDataRepositoryService,
  );
