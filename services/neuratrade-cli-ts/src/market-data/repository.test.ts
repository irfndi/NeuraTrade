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
import type { Candle, FundingRate, Tick } from "./types.js";

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

  it("works against the shared Go backend schema (display_name/ccxt_id/exchange_id NOT NULL)", async () => {
    // Mirror the Go backend's fat schema: extra NOT NULL columns that the
    // CLI's slim ensureTables does not create.
    db.exec(`
      CREATE TABLE exchanges (
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
      CREATE TABLE trading_pairs (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        exchange_id INTEGER NOT NULL,
        symbol TEXT NOT NULL,
        base_currency TEXT NOT NULL,
        quote_currency TEXT NOT NULL,
        is_active BOOLEAN DEFAULT 1,
        created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
        updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
        UNIQUE(exchange_id, symbol)
      );
    `);
    await Effect.runPromise(repo.ensureTables());

    const candles: Candle[] = [
      {
        exchange: "bitget-futures",
        symbol: "BTC/USDT:USDT",
        timeframe: "5m",
        open: 66_000,
        high: 68_000,
        low: 65_500,
        close: 67_000,
        volume: 100,
        timestamp: new Date("2026-01-01T00:00:00Z"),
      },
    ];

    const saved = await Effect.runPromise(repo.saveCandles(candles));
    expect(saved).toBe(1);

    const exchange = db
      .query("SELECT display_name, ccxt_id FROM exchanges WHERE name = ?")
      .get("bitget-futures") as { display_name: string; ccxt_id: string };
    expect(exchange.display_name).toBe("Bitget Futures");
    expect(exchange.ccxt_id).toBe("bitget-futures");

    const pair = db
      .query(
        "SELECT base_currency, quote_currency FROM trading_pairs WHERE symbol = ?",
      )
      .get("BTC/USDT:USDT") as {
      base_currency: string;
      quote_currency: string;
    };
    expect(pair).toEqual({ base_currency: "BTC", quote_currency: "USDT" });

    const loaded = await Effect.runPromise(
      repo.getCandles({
        exchange: "bitget-futures",
        symbol: "BTC/USDT:USDT",
        timeframe: "5m",
      }),
    );
    expect(loaded).toHaveLength(1);
    expect(loaded[0].close).toBe(67_000);
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

  it("returns candle range for a symbol/timeframe", async () => {
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
        timestamp: new Date("2026-01-01T02:00:00Z"),
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
        timestamp: new Date("2026-01-01T00:00:00Z"),
      },
      {
        exchange: "binance",
        symbol: "BTC/USDT",
        timeframe: "1h",
        open: 3,
        high: 3,
        low: 3,
        close: 3,
        volume: 3,
        timestamp: new Date("2026-01-01T01:00:00Z"),
      },
    ];

    await Effect.runPromise(repo.saveCandles(candles));

    const range = await Effect.runPromise(
      repo.getCandleRange("binance", "BTC/USDT", "1h"),
    );

    expect(range.count).toBe(3);
    expect(range.earliest?.toISOString()).toBe("2026-01-01T00:00:00.000Z");
    expect(range.latest?.toISOString()).toBe("2026-01-01T02:00:00.000Z");
  });

  it("deletes candles for a symbol/timeframe", async () => {
    await Effect.runPromise(repo.ensureTables());

    const btc: Candle = {
      exchange: "binance",
      symbol: "BTC/USDT",
      timeframe: "1h",
      open: 1,
      high: 1,
      low: 1,
      close: 1,
      volume: 1,
      timestamp: new Date("2026-01-01T00:00:00Z"),
    };
    const eth: Candle = {
      exchange: "binance",
      symbol: "ETH/USDT",
      timeframe: "1h",
      open: 2,
      high: 2,
      low: 2,
      close: 2,
      volume: 2,
      timestamp: new Date("2026-01-01T00:00:00Z"),
    };

    await Effect.runPromise(repo.saveCandles([btc]));
    await Effect.runPromise(repo.saveCandles([eth]));
    const deleted = await Effect.runPromise(
      repo.deleteCandles("binance", "BTC/USDT", "1h"),
    );

    expect(deleted).toBe(1);

    const btcCandles = await Effect.runPromise(
      repo.getCandles({
        exchange: "binance",
        symbol: "BTC/USDT",
        timeframe: "1h",
      }),
    );
    const ethCandles = await Effect.runPromise(
      repo.getCandles({
        exchange: "binance",
        symbol: "ETH/USDT",
        timeframe: "1h",
      }),
    );

    expect(btcCandles).toHaveLength(0);
    expect(ethCandles).toHaveLength(1);
  });

  it("lists symbols ordered by candle count", async () => {
    await Effect.runPromise(repo.ensureTables());

    const btc: Candle[] = Array.from({ length: 10 }, (_, i) => ({
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

    const eth: Candle[] = Array.from({ length: 5 }, (_, i) => ({
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

    const ranked = await Effect.runPromise(
      repo.listSymbolsByCandleCount("binance", "1h", 2),
    );

    expect(ranked).toHaveLength(2);
    expect(ranked[0].symbol).toBe("BTC/USDT");
    expect(ranked[0].count).toBe(10);
    expect(ranked[1].symbol).toBe("ETH/USDT");
    expect(ranked[1].count).toBe(5);

    const topOne = await Effect.runPromise(
      repo.listSymbolsByCandleCount("binance", "1h", 1),
    );
    expect(topOne).toHaveLength(1);
    expect(topOne[0].symbol).toBe("BTC/USDT");
  });

  it("returns a coverage report across symbols", async () => {
    await Effect.runPromise(repo.ensureTables());

    const start = new Date("2026-01-01T00:00:00Z");
    const end = new Date("2026-01-01T02:00:00Z");

    const btc: Candle[] = [
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
        timestamp: new Date("2026-01-01T01:00:00Z"),
      },
    ];
    const eth: Candle = {
      exchange: "binance",
      symbol: "ETH/USDT",
      timeframe: "1h",
      open: 1,
      high: 1,
      low: 1,
      close: 1,
      volume: 1,
      timestamp: new Date("2026-01-01T00:00:00Z"),
    };

    await Effect.runPromise(repo.saveCandles(btc));
    await Effect.runPromise(repo.saveCandles([eth]));

    const report = await Effect.runPromise(
      repo.getCoverageReport(
        "binance",
        ["BTC/USDT", "ETH/USDT", "SOL/USDT"],
        "1h",
        start,
        end,
      ),
    );

    expect(report).toHaveLength(3);

    const btcReport = report.find((r) => r.symbol === "BTC/USDT");
    expect(btcReport?.count).toBe(2);
    expect(btcReport?.expected).toBe(3);
    expect(btcReport?.coveragePct).toBeCloseTo(2 / 3);
    expect(btcReport?.status).toBe("partial");

    const ethReport = report.find((r) => r.symbol === "ETH/USDT");
    expect(ethReport?.count).toBe(1);
    expect(ethReport?.status).toBe("partial");

    const solReport = report.find((r) => r.symbol === "SOL/USDT");
    expect(solReport?.count).toBe(0);
    expect(solReport?.status).toBe("missing");
  });

  it("saves and retrieves funding rates", async () => {
    await Effect.runPromise(repo.ensureFundingRatesTable());

    const rates: FundingRate[] = [
      {
        exchange: "binance",
        symbol: "BTC/USDT",
        fundingRate: 0.0001,
        timestamp: new Date("2026-01-01T00:00:00Z"),
      },
      {
        exchange: "binance",
        symbol: "BTC/USDT",
        fundingRate: -0.0002,
        timestamp: new Date("2026-01-01T08:00:00Z"),
      },
    ];

    const saved = await Effect.runPromise(
      repo.saveFundingRates("binance", "BTC/USDT", rates),
    );
    expect(saved).toBe(2);

    const loaded = await Effect.runPromise(
      repo.getFundingRates("binance", "BTC/USDT"),
    );
    expect(loaded).toHaveLength(2);
    expect(loaded[0]?.fundingRate).toBe(0.0001);
    expect(loaded[1]?.fundingRate).toBe(-0.0002);
  });

  it("returns the latest funding rate before a timestamp", async () => {
    await Effect.runPromise(repo.ensureFundingRatesTable());

    const rates: FundingRate[] = [
      {
        exchange: "binance",
        symbol: "BTC/USDT",
        fundingRate: 0.0001,
        timestamp: new Date("2026-01-01T00:00:00Z"),
      },
      {
        exchange: "binance",
        symbol: "BTC/USDT",
        fundingRate: -0.0002,
        timestamp: new Date("2026-01-01T08:00:00Z"),
      },
    ];
    await Effect.runPromise(
      repo.saveFundingRates("binance", "BTC/USDT", rates),
    );

    const latest = await Effect.runPromise(
      repo.getLatestFundingRateBefore(
        "binance",
        "BTC/USDT",
        new Date("2026-01-01T04:00:00Z"),
      ),
    );
    expect(latest).not.toBeNull();
    expect(latest!.fundingRate).toBe(0.0001);
  });

  it("read paths do not create exchange or pair rows for unknown symbols", async () => {
    await Effect.runPromise(repo.ensureTables());

    const candles = await Effect.runPromise(
      repo.getCandles({
        exchange: "never-seen-exchange",
        symbol: "SOL/USDT",
        timeframe: "1h",
      }),
    );
    expect(candles).toHaveLength(0);

    const tick = await Effect.runPromise(
      repo.getLatestTick("never-seen-exchange", "SOL/USDT"),
    );
    expect(tick).toBeNull();

    const symbols = await Effect.runPromise(
      repo.listSymbols("never-seen-exchange", "1h", 1),
    );
    expect(symbols).toHaveLength(0);

    const ranked = await Effect.runPromise(
      repo.listSymbolsByCandleCount("never-seen-exchange", "1h", 5),
    );
    expect(ranked).toHaveLength(0);

    const deleted = await Effect.runPromise(
      repo.deleteCandles("never-seen-exchange", "SOL/USDT", "1h"),
    );
    expect(deleted).toBe(0);

    const range = await Effect.runPromise(
      repo.getCandleRange("never-seen-exchange", "SOL/USDT", "1h"),
    );
    expect(range).toEqual({ earliest: null, latest: null, count: 0 });

    const report = await Effect.runPromise(
      repo.getCoverageReport(
        "never-seen-exchange",
        ["SOL/USDT"],
        "1h",
        new Date("2026-01-01T00:00:00Z"),
        new Date("2026-01-01T02:00:00Z"),
      ),
    );
    expect(report[0]?.status).toBe("missing");

    // Nothing was written: both tables stay empty.
    const exchangeCount = (
      db.query("SELECT COUNT(*) AS n FROM exchanges").get() as { n: number }
    ).n;
    const pairCount = (
      db.query("SELECT COUNT(*) AS n FROM trading_pairs").get() as { n: number }
    ).n;
    expect(exchangeCount).toBe(0);
    expect(pairCount).toBe(0);
  });

  it("saveCandles rejects a batch mixing exchanges or symbols", async () => {
    await Effect.runPromise(repo.ensureTables());

    const mixed: Candle[] = [
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
        symbol: "ETH/USDT",
        timeframe: "1h",
        open: 1,
        high: 1,
        low: 1,
        close: 1,
        volume: 1,
        timestamp: new Date("2026-01-01T01:00:00Z"),
      },
    ];

    const result = await Effect.runPromise(
      repo.saveCandles(mixed).pipe(
        Effect.match({
          onFailure: (err) => ({ ok: false, reason: err.reason }),
          onSuccess: () => ({ ok: true, reason: "" }),
        }),
      ),
    );

    expect(result.ok).toBe(false);
    expect(result.reason).toContain(
      "same exchange, symbol, and timeframe",
    );

    // Nothing persisted for the mixed batch.
    const count = (
      db.query("SELECT COUNT(*) AS n FROM ohlcv_data").get() as { n: number }
    ).n;
    expect(count).toBe(0);
  });
});
