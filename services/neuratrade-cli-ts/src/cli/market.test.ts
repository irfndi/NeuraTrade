import { describe, expect, it } from "bun:test";
import { Effect } from "effect";
import { Database } from "bun:sqlite";
import {
  MarketDataGateway,
  type MarketDataGatewayService,
} from "../market-data/gateway.js";
import { MarketDataRepositorySQLiteLive } from "../market-data/repository.js";
import type { Candle, OrderBook } from "../market-data/types.js";
import { fetchUniverseProgram } from "./market.js";

function makeCandle(symbol: string, closeTime: Date): Candle {
  return {
    exchange: "binance",
    symbol,
    timeframe: "1h",
    open: 100,
    high: 101,
    low: 99,
    close: 100,
    volume: 10,
    timestamp: closeTime,
  };
}

function makeGateway(): MarketDataGatewayService {
  return {
    fetchTick: () => Effect.fail({ reason: "not used" } as never),
    fetchOHLCV: () =>
      Effect.succeed([makeCandle("BTC/USDT", new Date(Date.now() - 60_000))]),
    fetchOrderBook: (): Effect.Effect<OrderBook, never, never> =>
      Effect.succeed({
        exchange: "binance",
        symbol: "BTC/USDT",
        bids: [{ price: 99, volume: 1 }],
        asks: [{ price: 101, volume: 1 }],
        timestamp: new Date(),
      }),
    fetchSymbols: () =>
      Effect.succeed(["BTC/USDT", "ETH/USDT", "SOL/USDT", "DOGE/BTC"]),
    fetch24hrVolumes: () =>
      Effect.succeed({
        BTCUSDT: 1_000_000,
        ETHUSDT: 500_000,
        SOLUSDT: 100_000,
        DOGEBTC: 50_000,
      }),
  };
}

describe("fetchUniverseProgram", () => {
  it("fetches and stores candles for top-volume USDT symbols", async () => {
    const db = new Database(":memory:");
    const repoLayer = MarketDataRepositorySQLiteLive(db);
    const gateway = makeGateway();

    const result = await Effect.runPromise(
      fetchUniverseProgram({
        exchange: "binance",
        timeframe: "1h",
        days: 365,
        batch: 1000,
        top: 2,
        quote: "USDT",
        minVolume: 0,
      }).pipe(
        Effect.provide(repoLayer),
        Effect.provideService(MarketDataGateway, gateway),
      ),
    );

    expect(result.symbols).toEqual(["BTC/USDT", "ETH/USDT"]);
    expect(result.totalCandles).toBe(2);

    db.close();
  });
});
