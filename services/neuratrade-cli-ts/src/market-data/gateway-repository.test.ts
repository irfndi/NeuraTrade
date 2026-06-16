import { describe, expect, it, beforeEach, afterEach } from "bun:test";
import { Effect, Layer } from "effect";
import { Database } from "bun:sqlite";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { MarketDataGatewayRepositoryLive } from "./gateway-repository.js";
import {
  MarketDataRepository,
  MarketDataRepositorySQLiteLive,
} from "./repository.js";
import { MarketDataGateway } from "./gateway.js";
import type { Candle } from "./types.js";

function makeTestDb(): Database {
  const dir = mkdtempSync(join(tmpdir(), "neuratrade-gateway-repo-"));
  const path = join(dir, "test.db");
  const db = new Database(path);
  db.exec("PRAGMA foreign_keys = ON;");
  return db;
}

function makeGatewayLayer(db: Database) {
  const repoLayer = MarketDataRepositorySQLiteLive(db);
  return Layer.provide(MarketDataGatewayRepositoryLive, repoLayer).pipe(
    Layer.merge(repoLayer),
  );
}

async function seedCandles(
  db: Database,
  candles: readonly Candle[],
): Promise<void> {
  await Effect.runPromise(
    Effect.gen(function* () {
      const repo = yield* MarketDataRepository;
      yield* repo.ensureTables();
      yield* repo.saveCandles(candles);
    }).pipe(Effect.provide(MarketDataRepositorySQLiteLive(db))),
  );
}

describe("MarketDataGatewayRepositoryLive", () => {
  let db: Database;

  beforeEach(() => {
    db = makeTestDb();
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

  it("fetchOHLCV returns stored candles", async () => {
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
    await seedCandles(db, candles);

    const loaded = await Effect.runPromise(
      Effect.gen(function* () {
        const gateway = yield* MarketDataGateway;
        return yield* gateway.fetchOHLCV("binance", "BTC/USDT", "1h", 100);
      }).pipe(Effect.provide(makeGatewayLayer(db))),
    );

    expect(loaded).toHaveLength(2);
    expect(loaded[1].close).toBe(68_000);
  });

  it("fetchOrderBook synthesizes book from latest candle", async () => {
    const candle: Candle = {
      exchange: "binance",
      symbol: "BTC/USDT",
      timeframe: "1h",
      open: 67_000,
      high: 69_000,
      low: 66_000,
      close: 68_000,
      volume: 200,
      timestamp: new Date("2026-01-01T00:00:00Z"),
    };
    await seedCandles(db, [candle]);

    const orderBook = await Effect.runPromise(
      Effect.gen(function* () {
        const gateway = yield* MarketDataGateway;
        return yield* gateway.fetchOrderBook("binance", "BTC/USDT", 5);
      }).pipe(Effect.provide(makeGatewayLayer(db))),
    );

    expect(orderBook.bids).toHaveLength(5);
    expect(orderBook.asks).toHaveLength(5);
    expect(orderBook.bids[0].price).toBeLessThan(orderBook.asks[0].price);
    expect(orderBook.bids[0].price).toBeGreaterThan(0);
  });

  it("fetchTick returns price from latest candle", async () => {
    const candle: Candle = {
      exchange: "binance",
      symbol: "BTC/USDT",
      timeframe: "1h",
      open: 67_000,
      high: 69_000,
      low: 66_000,
      close: 68_123,
      volume: 200,
      timestamp: new Date("2026-01-01T00:00:00Z"),
    };
    await seedCandles(db, [candle]);

    const tick = await Effect.runPromise(
      Effect.gen(function* () {
        const gateway = yield* MarketDataGateway;
        return yield* gateway.fetchTick("binance", "BTC/USDT");
      }).pipe(Effect.provide(makeGatewayLayer(db))),
    );

    expect(tick.price).toBe(68_123);
    expect(tick.symbol).toBe("BTC/USDT");
  });

  it("fetchSymbols lists symbols stored at 1h", async () => {
    const candle: Candle = {
      exchange: "binance",
      symbol: "ETH/USDT",
      timeframe: "1h",
      open: 3_000,
      high: 3_100,
      low: 2_900,
      close: 3_050,
      volume: 50,
      timestamp: new Date("2026-01-01T00:00:00Z"),
    };
    await seedCandles(db, [candle]);

    const symbols = await Effect.runPromise(
      Effect.gen(function* () {
        const gateway = yield* MarketDataGateway;
        return yield* gateway.fetchSymbols("binance");
      }).pipe(Effect.provide(makeGatewayLayer(db))),
    );

    expect(symbols).toContain("ETH/USDT");
  });

  it("fetchOHLCV fails when no candles are stored", async () => {
    await Effect.runPromise(
      Effect.gen(function* () {
        const repo = yield* MarketDataRepository;
        yield* repo.ensureTables();
      }).pipe(Effect.provide(MarketDataRepositorySQLiteLive(db))),
    );

    const result = await Effect.runPromise(
      Effect.gen(function* () {
        const gateway = yield* MarketDataGateway;
        return yield* gateway.fetchOHLCV("binance", "BTC/USDT", "1h", 100).pipe(
          Effect.match({
            onFailure: (err) => ({ ok: false, reason: err.reason }),
            onSuccess: () => ({ ok: true, reason: "" }),
          }),
        );
      }).pipe(Effect.provide(makeGatewayLayer(db))),
    );

    expect(result.ok).toBe(false);
    expect(result.reason).toContain("No stored candles");
  });
});
