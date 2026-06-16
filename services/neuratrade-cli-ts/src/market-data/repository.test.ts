import { describe, expect, it, beforeEach, afterEach } from "bun:test";
import { Effect } from "effect";
import { Database } from "bun:sqlite";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  MarketDataRepositoryService,
  MarketDataRepositorySQLite,
} from "./repository.js";
import type { Candle, Tick } from "./types.js";

function makeTestDb(): Database {
  const dir = mkdtempSync(join(tmpdir(), "neuratrade-market-data-"));
  const path = join(dir, "test.db");
  const db = new Database(path);
  db.exec("PRAGMA foreign_keys = ON;");
  return db;
}

function makeRepo(db: Database): MarketDataRepositoryService {
  return new MarketDataRepositorySQLite(db);
}

describe("MarketDataRepositorySQLite", () => {
  let db: Database;
  let repo: MarketDataRepositoryService;

  beforeEach(() => {
    db = makeTestDb();
    repo = makeRepo(db);
  });

  afterEach(() => {
    const path = db.filename;
    db.close();
    if (path) {
      try {
        rmSync(path, { force: true });
      } catch {
        // ignore cleanup errors
      }
    }
  });

  it("creates tables", async () => {
    await Effect.runPromise(repo.ensureTables());

    const tables = db
      .query(
        "SELECT name FROM sqlite_master WHERE type = 'table' AND name IN ('exchanges', 'trading_pairs', 'market_data', 'ohlcv_data')",
      )
      .all() as Array<{ name: string }>;
    const names = tables.map((t) => t.name).sort();
    expect(names).toEqual([
      "exchanges",
      "market_data",
      "ohlcv_data",
      "trading_pairs",
    ]);
  });

  it("saves and retrieves a tick", async () => {
    await Effect.runPromise(repo.ensureTables());

    const tick: Tick = {
      exchange: "binance",
      symbol: "BTC/USDT",
      price: 67_000,
      volume: 1.5,
      bid: 66_990,
      ask: 67_010,
      timestamp: new Date("2026-01-01T00:00:00Z"),
    };

    await Effect.runPromise(repo.saveTick(tick));

    const latest = await Effect.runPromise(
      repo.getLatestTick("binance", "BTC/USDT"),
    );
    expect(latest).not.toBeNull();
    expect(latest?.price).toBe(67_000);
    expect(latest?.bid).toBe(66_990);
    expect(latest?.ask).toBe(67_010);
    expect(latest?.timestamp.toISOString()).toBe("2026-01-01T00:00:00.000Z");
  });

  it("saves and retrieves candles", async () => {
    await Effect.runPromise(repo.ensureTables());

    const candles: Candle[] = [
      {
        exchange: "binance",
        symbol: "BTC/USDT",
        timeframe: "1h",
        open: 66_000,
        high: 68_000,
        low: 65_500,
        close: 67_000,
        volume: 100,
        timestamp: new Date("2026-01-01T00:00:00Z"),
      },
      {
        exchange: "binance",
        symbol: "BTC/USDT",
        timeframe: "1h",
        open: 67_000,
        high: 69_000,
        low: 66_500,
        close: 68_000,
        volume: 150,
        timestamp: new Date("2026-01-01T01:00:00Z"),
      },
    ];

    const saved = await Effect.runPromise(repo.saveCandles(candles));
    expect(saved).toBe(2);

    const loaded = await Effect.runPromise(
      repo.getCandles({
        exchange: "binance",
        symbol: "BTC/USDT",
        timeframe: "1h",
      }),
    );

    expect(loaded).toHaveLength(2);
    expect(loaded[0].close).toBe(67_000);
    expect(loaded[1].close).toBe(68_000);
  });

  it("respects candle query date range", async () => {
    await Effect.runPromise(repo.ensureTables());

    const candles: Candle[] = [
      {
        exchange: "binance",
        symbol: "BTC/USDT",
        timeframe: "1h",
        open: 1,
        high: 1,
        low: 1,
        close: 1,
        volume: 1,
        timestamp: new Date("2026-01-01T00:00:00Z"),
      },
      {
        exchange: "binance",
        symbol: "BTC/USDT",
        timeframe: "1h",
        open: 2,
        high: 2,
        low: 2,
        close: 2,
        volume: 2,
        timestamp: new Date("2026-01-01T02:00:00Z"),
      },
    ];

    await Effect.runPromise(repo.saveCandles(candles));

    const loaded = await Effect.runPromise(
      repo.getCandles({
        exchange: "binance",
        symbol: "BTC/USDT",
        timeframe: "1h",
        from: new Date("2026-01-01T01:00:00Z"),
        to: new Date("2026-01-01T03:00:00Z"),
      }),
    );

    expect(loaded).toHaveLength(1);
    expect(loaded[0].close).toBe(2);
  });

  it("lists symbols with enough candles", async () => {
    await Effect.runPromise(repo.ensureTables());

    const btc: Candle[] = Array.from({ length: 5 }, (_, i) => ({
      exchange: "binance",
      symbol: "BTC/USDT",
      timeframe: "1h",
      open: i,
      high: i,
      low: i,
      close: i,
      volume: i,
      timestamp: new Date(Date.UTC(2026, 0, 1, i)),
    }));

    const eth: Candle[] = Array.from({ length: 2 }, (_, i) => ({
      exchange: "binance",
      symbol: "ETH/USDT",
      timeframe: "1h",
      open: i,
      high: i,
      low: i,
      close: i,
      volume: i,
      timestamp: new Date(Date.UTC(2026, 0, 1, i)),
    }));

    await Effect.runPromise(repo.saveCandles(btc));
    await Effect.runPromise(repo.saveCandles(eth));

    const symbols = await Effect.runPromise(
      repo.listSymbols("binance", "1h", 3),
    );
    expect(symbols).toEqual(["BTC/USDT"]);
  });
});
