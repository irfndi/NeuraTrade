import { Command, Options } from "@effect/cli";
import { BunContext } from "@effect/platform-bun";
import { Console, Effect, Layer } from "effect";
import { Database } from "bun:sqlite";
import { resolve } from "node:path";
import { Path, PathLive } from "../services/path.js";
import { MarketDataGateway } from "../market-data/gateway.js";
import {
  MarketDataRepository,
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

export const marketCommand = Command.make("market", {}, () =>
  Console.log("Market data commands. Use 'market fetch-candles --help' for details."),
).pipe(
  Command.withDescription("Market data operations"),
  Command.withSubcommands([fetchCandlesCommand]),
);
