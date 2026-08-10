import { describe, expect, it } from "bun:test";
import { Effect, Layer } from "effect";

import { MarketDataError, MarketDataGateway } from "../market-data/gateway.js";
import {
  MarketDataRepository,
  type MarketDataRepositoryService,
} from "../market-data/repository.js";
import type { Candle } from "../market-data/types.js";
import { CloudflareMarketDataRepositoryLive } from "./market-data-repository.ts";

// ---------------------------------------------------------------------------
// CloudflareMarketDataRepositoryLive — the porting seam. The two methods the
// universe scanner uses must work against a live-fetch gateway; everything
// else must fail loudly rather than pretend to persist on the edge.
// ---------------------------------------------------------------------------

const candle = (i: number): Candle => ({
  exchange: "bitget-futures",
  symbol: "BTC/USDT",
  timeframe: "15m",
  open: 1,
  high: 2,
  low: 0.5,
  close: 1.5,
  volume: 100,
  timestamp: new Date(1700000000000 + i * 60_000),
});

const fakeGateway = Layer.succeed(MarketDataGateway, {
  fetchTick: () => Effect.fail(new MarketDataError("unused")),
  fetchOHLCV: (_exchange, _symbol, _tf, limit) =>
    Effect.succeed(Array.from({ length: limit }, (_, i) => candle(i))),
  fetchOrderBook: () => Effect.fail(new MarketDataError("unused")),
  fetchSymbols: () => Effect.succeed(["BTC/USDT", "ETH/USDT"]),
  fetchDemoSymbols: () => Effect.fail({ reason: "not used" } as never),
  fetch24hrVolumes: () => Effect.succeed({}),
  fetchFundingRates: () => Effect.succeed([]),
});

describe("CloudflareMarketDataRepositoryLive", () => {
  const runWith = <A, E>(
    effect: Effect.Effect<A, E, MarketDataRepositoryService>,
  ) =>
    Effect.runPromise(
      // The layer provides the repository + gateway; the casts are test-only
      // (effect's Context tag typing is stricter than the runtime contract).
      Effect.provide(
        effect as Effect.Effect<A, E, never>,
        Layer.provide(
          CloudflareMarketDataRepositoryLive(["BTC/USDT", "ETH/USDT"]),
          fakeGateway,
        ),
      ) as Effect.Effect<A, E, never>,
    );

  it("counts candles per seed symbol via the gateway", async () => {
    const counts = await runWith(
      Effect.gen(function* () {
        const repo = yield* MarketDataRepository;
        return yield* repo.listSymbolsByCandleCount(
          "bitget-futures",
          "15m",
          500,
        );
      }),
    );
    expect(counts).toEqual([
      { symbol: "BTC/USDT", count: 500 },
      { symbol: "ETH/USDT", count: 500 },
    ]);
  });

  it("returns candles for a symbol via the gateway", async () => {
    const candles = await runWith(
      Effect.gen(function* () {
        const repo = yield* MarketDataRepository;
        return yield* repo.getCandles({
          exchange: "bitget-futures",
          symbol: "BTC/USDT",
          timeframe: "15m",
          limit: 10,
        });
      }),
    );
    expect(candles).toHaveLength(10);
    expect(candles[0]?.close).toBe(1.5);
  });

  it("fails loudly on persistence methods (nothing persists on the edge)", async () => {
    const error = await runWith(
      Effect.gen(function* () {
        const repo = yield* MarketDataRepository;
        return yield* repo.saveCandles([]).pipe(Effect.flip);
      }),
    );
    expect(error._tag).toBe("MarketDataRepositoryError");
    expect(error.reason).toContain("not implemented on Cloudflare worker");
  });
});
