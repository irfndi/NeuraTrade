import { Command, Options } from "@effect/cli";
import { BunContext } from "@effect/platform-bun";
import { Console, Effect, Layer } from "effect";
import { Database } from "bun:sqlite";
import { resolve } from "node:path";
import { Path, PathLive } from "../services/path.js";
import { MarketDataGateway } from "../market-data/gateway.js";
import {
  MarketDataRepository,
  MarketDataRepositoryError,
  MarketDataRepositorySQLiteLive,
} from "../market-data/repository.js";
import { MarketDataGatewayLive } from "../market-data/gateways/index.js";

const exchangeOption = Options.text("exchange").pipe(
  Options.withDefault("binance"),
  Options.withDescription("Exchange identifier (e.g. binance)"),
);

const symbolOption = Options.text("symbol").pipe(
  Options.withDefault("BTC/USDT"),
  Options.withDescription("Trading pair symbol"),
);

const timeframeOption = Options.text("timeframe").pipe(
  Options.withDefault("1h"),
  Options.withDescription("Candle timeframe (1m, 5m, 15m, 30m, 1h, 4h, 1d)"),
);

const daysOption = Options.integer("days").pipe(
  Options.withDefault(365),
  Options.withDescription("How many days of history to fetch"),
);

const batchOption = Options.integer("batch").pipe(
  Options.withDefault(1000),
  Options.withDescription("Candles per exchange request (max 1000 for Binance)"),
);

const topOption = Options.integer("top").pipe(
  Options.withDefault(20),
  Options.withDescription("Number of top-volume symbols to fetch"),
);

const quoteOption = Options.text("quote").pipe(
  Options.withDefault("USDT"),
  Options.withDescription("Quote asset to filter (e.g. USDT, BTC)"),
);

function makeLayer(home?: string) {
  return Layer.mergeAll(BunContext.layer, PathLive(home), MarketDataGatewayLive);
}

export const fetchCandlesCommand = Command.make(
  "fetch-candles",
  {
    exchange: exchangeOption,
    symbol: symbolOption,
    timeframe: timeframeOption,
    days: daysOption,
    batch: batchOption,
  },
  ({ exchange, symbol, timeframe, days, batch }) =>
    Effect.gen(function* () {
      const path = yield* Path;
      const sqlitePath = resolve(path.homeDir, "data", "neuratrade.db");
      const db = new Database(sqlitePath);
      db.exec("PRAGMA foreign_keys = ON;");

      const repoLayer = MarketDataRepositorySQLiteLive(db);

      const result = yield* fetchCandlesProgram({ exchange, symbol, timeframe, days, batch }).pipe(
        Effect.provide(repoLayer),
        Effect.tap((total) => Console.log(`Fetched and stored ${total} candles`)),
        Effect.catchAll((err) =>
          Effect.gen(function* () {
            yield* Console.error(`fetch-candles failed: ${err.reason}`);
            return 0;
          }),
        ),
        Effect.ensuring(Effect.sync(() => db.close())),
      );

      return result;
    }).pipe(Effect.provide(makeLayer(process.env.NEURATRADE_HOME))),
).pipe(Command.withDescription("Fetch historical OHLCV candles from an exchange"));

interface FetchCandlesArgs {
  readonly exchange: string;
  readonly symbol: string;
  readonly timeframe: string;
  readonly days: number;
  readonly batch: number;
}

function fetchCandlesProgram(args: FetchCandlesArgs) {
  return Effect.gen(function* () {
    const repo = yield* MarketDataRepository;
    const gateway = yield* MarketDataGateway;

    yield* repo.ensureTables();

    const now = Date.now();
    const startTime = now - args.days * 24 * 60 * 60 * 1000;
    const timeframeMs = timeframeToMs(args.timeframe);
    const candlesPerBatch = Math.min(args.batch, 1000);

    let totalSaved = 0;
    let currentStart = startTime;

    while (currentStart < now) {
      const candles = yield* gateway.fetchOHLCV(
        args.exchange,
        args.symbol,
        args.timeframe,
        candlesPerBatch,
        new Date(currentStart),
      );

      if (candles.length === 0) break;

      const filtered = candles.filter((c) => c.timestamp.getTime() >= startTime);
      if (filtered.length === 0) break;

      const saved = yield* repo.saveCandles(filtered);
      totalSaved += saved;

      yield* Console.log(
        `Batch saved ${saved} candles up to ${filtered[filtered.length - 1].timestamp.toISOString()}`,
      );

      const lastTimestamp = filtered[filtered.length - 1].timestamp.getTime();
      if (lastTimestamp <= currentStart) break;
      currentStart = lastTimestamp + timeframeMs;

      // Respectful rate limiting: 100ms between requests (10 req/s).
      yield* Effect.sleep("100 millis");
    }

    return totalSaved;
  });
}

function timeframeToMs(timeframe: string): number {
  const value = Number.parseInt(timeframe, 10);
  const unit = timeframe.slice(-1);
  const multiplier: Record<string, number> = {
    m: 60_000,
    h: 3_600_000,
    d: 86_400_000,
    w: 604_800_000,
  };
  return value * (multiplier[unit] ?? 60_000);
}

interface FetchUniverseArgs {
  readonly exchange: string;
  readonly timeframe: string;
  readonly days: number;
  readonly batch: number;
  readonly top: number;
  readonly quote: string;
}

export const fetchUniverseCommand = Command.make(
  "fetch-universe",
  {
    exchange: exchangeOption,
    timeframe: timeframeOption,
    days: daysOption,
    batch: batchOption,
    top: topOption,
    quote: quoteOption,
  },
  ({ exchange, timeframe, days, batch, top, quote }) =>
    Effect.gen(function* () {
      const path = yield* Path;
      const sqlitePath = resolve(path.homeDir, "data", "neuratrade.db");
      const db = new Database(sqlitePath);
      db.exec("PRAGMA foreign_keys = ON;");

      const repoLayer = MarketDataRepositorySQLiteLive(db);

      const result = yield* fetchUniverseProgram({ exchange, timeframe, days, batch, top, quote }).pipe(
        Effect.provide(repoLayer),
        Effect.tap((summary) =>
          Console.log(
            `Universe fetch complete: ${summary.symbols.length} symbols, ${summary.totalCandles} candles`,
          ),
        ),
        Effect.catchAll((err) =>
          Effect.gen(function* () {
            yield* Console.error(`fetch-universe failed: ${err.reason}`);
            return { symbols: [], totalCandles: 0 };
          }),
        ),
        Effect.ensuring(Effect.sync(() => db.close())),
      );

      return result;
    }).pipe(Effect.provide(makeLayer(process.env.NEURATRADE_HOME))),
).pipe(Command.withDescription("Fetch historical candles for the top-volume symbol universe"));

export function fetchUniverseProgram(args: FetchUniverseArgs) {
  return Effect.gen(function* () {
    const repo = yield* MarketDataRepository;
    const gateway = yield* MarketDataGateway;

    yield* repo.ensureTables();

    yield* Console.log(`Loading symbol universe for ${args.quote}...`);
    const allSymbols = yield* gateway.fetchSymbols(args.exchange);
    const quoteSymbols = allSymbols.filter((s) => s.endsWith(`/${args.quote.toUpperCase()}`));

    if (quoteSymbols.length === 0) {
      return yield* Effect.fail(
        new MarketDataRepositoryError(
          `No symbols found for quote asset ${args.quote} on ${args.exchange}`,
        ),
      );
    }

    yield* Console.log(`Loading 24h volumes to rank ${quoteSymbols.length} ${args.quote} symbols...`);
    const volumes = yield* gateway.fetch24hrVolumes(args.exchange);

    const ranked = quoteSymbols
      .map((symbol) => ({
        symbol,
        volume: volumes[symbol.replace("/", "")] ?? 0,
      }))
      .sort((a, b) => b.volume - a.volume)
      .slice(0, args.top)
      .map((s) => s.symbol);

    yield* Console.log(`Fetching ${ranked.length} symbols: ${ranked.join(", ")}`);

    let totalCandles = 0;
    for (const symbol of ranked) {
      const saved = yield* fetchCandlesProgram({
        exchange: args.exchange,
        symbol,
        timeframe: args.timeframe,
        days: args.days,
        batch: args.batch,
      });
      totalCandles += saved;
      yield* Console.log(`  ${symbol}: ${saved} candles`);
    }

    return { symbols: ranked, totalCandles };
  });
}

export const marketCommand = Command.make("market", {}, () =>
  Console.log("Market data commands. Use 'market fetch-candles|fetch-universe --help' for details."),
).pipe(
  Command.withDescription("Market data operations"),
  Command.withSubcommands([fetchCandlesCommand, fetchUniverseCommand]),
);
