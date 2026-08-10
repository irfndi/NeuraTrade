import { describe, expect, it } from "bun:test";
import { Effect, Layer } from "effect";

import {
  MarketDataError,
  MarketDataGateway,
  type MarketDataGatewayService,
} from "../market-data/gateway.js";
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

const baseGateway: MarketDataGatewayService = {
  fetchTick: () => Effect.fail(new MarketDataError("unused")),
  fetchOHLCV: (_exchange, _symbol, _tf, limit) =>
    Effect.succeed(Array.from({ length: limit }, (_, i) => candle(i))),
  fetchOrderBook: () => Effect.fail(new MarketDataError("unused")),
  fetchSymbols: () => Effect.succeed(["BTC/USDT", "ETH/USDT"]),
  fetchDemoSymbols: () => Effect.fail({ reason: "not used" } as never),
  fetch24hrVolumes: () => Effect.succeed({}),
  fetchFundingRates: () => Effect.succeed([]),
};

const fakeGateway = Layer.succeed(MarketDataGateway, baseGateway);

describe("CloudflareMarketDataRepositoryLive", () => {
  const runWith = <A, E>(
    effect: Effect.Effect<A, E, MarketDataRepositoryService>,
    gateway: Layer.Layer<MarketDataGatewayService> = fakeGateway,
    seeds: readonly string[] = ["BTC/USDT", "ETH/USDT"],
  ) =>
    Effect.runPromise(
      // The layer provides the repository + gateway; the casts are test-only
      // (effect's Context tag typing is stricter than the runtime contract).
      Effect.provide(
        effect as Effect.Effect<A, E, never>,
        Layer.provide(
          CloudflareMarketDataRepositoryLive(seeds),
          gateway,
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

  it("fails loudly on getFundingRates (stub contract)", async () => {
    const error = await runWith(
      Effect.gen(function* () {
        const repo = yield* MarketDataRepository;
        return yield* repo
          .getFundingRates("bitget-futures", "BTC/USDT")
          .pipe(Effect.flip);
      }),
    );
    expect(error._tag).toBe("MarketDataRepositoryError");
    expect(error.reason).toBe(
      "getFundingRates: not implemented on Cloudflare worker",
    );
  });

  it("maps gateway failures to op-prefixed repository errors", async () => {
    const error = await runWith(
      Effect.gen(function* () {
        const repo = yield* MarketDataRepository;
        return yield* repo
          .getCandles({
            exchange: "bitget-futures",
            symbol: "BTC/USDT",
            timeframe: "15m",
            limit: 10,
          })
          .pipe(Effect.flip);
      }),
      Layer.succeed(MarketDataGateway, {
        ...baseGateway,
        fetchOHLCV: () => Effect.fail(new MarketDataError("rate limited")),
      }),
    );
    expect(error._tag).toBe("MarketDataRepositoryError");
    expect(error.reason).toBe("getCandles: rate limited");
  });

  it("defaults the candle limit to 500 when query.limit is undefined", async () => {
    let seenLimit: number | undefined;
    await runWith(
      Effect.gen(function* () {
        const repo = yield* MarketDataRepository;
        yield* repo.getCandles({
          exchange: "bitget-futures",
          symbol: "BTC/USDT",
          timeframe: "15m",
        });
      }),
      Layer.succeed(MarketDataGateway, {
        ...baseGateway,
        fetchOHLCV: (_exchange, _symbol, _tf, limit) => {
          seenLimit = limit;
          return Effect.succeed([candle(0)]);
        },
      }),
    );
    expect(seenLimit).toBe(500);
  });

  it("skips failed symbols without aborting the universe scan", async () => {
    const counts = await runWith(
      Effect.gen(function* () {
        const repo = yield* MarketDataRepository;
        return yield* repo.listSymbolsByCandleCount(
          "bitget-futures",
          "15m",
          500,
        );
      }),
      Layer.succeed(MarketDataGateway, {
        ...baseGateway,
        fetchOHLCV: (_exchange, symbol, _tf, limit) =>
          symbol === "BTC/USDT"
            ? Effect.fail(new MarketDataError("delisted"))
            : Effect.succeed(
                Array.from({ length: limit }, (_, i) => candle(i)),
              ),
      }),
    );
    expect(counts).toEqual([{ symbol: "ETH/USDT", count: 500 }]);
  });

  it("runs the universe scan with bounded concurrency (max 4 in-flight)", async () => {
    const seeds = Array.from({ length: 8 }, (_, i) => `SYM${i}/USDT`);
    let inFlight = 0;
    let maxInFlight = 0;
    const counts = await runWith(
      Effect.gen(function* () {
        const repo = yield* MarketDataRepository;
        return yield* repo.listSymbolsByCandleCount(
          "bitget-futures",
          "15m",
          2,
        );
      }),
      Layer.succeed(MarketDataGateway, {
        ...baseGateway,
        fetchOHLCV: (_exchange, _symbol, _tf, limit) => {
          inFlight++;
          maxInFlight = Math.max(maxInFlight, inFlight);
          return Effect.sync(() => {
            inFlight--;
            return Array.from({ length: limit }, (_, i) => candle(i));
          }).pipe(Effect.delay(1));
        },
      }),
      seeds,
    );
    expect(counts).toHaveLength(8);
    expect(maxInFlight).toBeGreaterThan(1);
    expect(maxInFlight).toBeLessThanOrEqual(4);
  });
});
