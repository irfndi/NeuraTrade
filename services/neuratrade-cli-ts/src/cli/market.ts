import { Command, Options } from "./kit/kit.ts";
import { BunServices } from "@effect/platform-bun";
import { Console, Effect, Layer, Option } from "effect";
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
  Options.withDescription(
    "Candles per exchange request (max 1000 for Binance)",
  ),
);

const topOption = Options.integer("top").pipe(
  Options.withDefault(20),
  Options.withDescription("Number of top-volume symbols to fetch"),
);

const quoteOption = Options.text("quote").pipe(
  Options.withDefault("USDT"),
  Options.withDescription("Quote asset to filter (e.g. USDT, BTC)"),
);

const minVolumeOption = Options.float("min-volume").pipe(
  Options.withDefault(0),
  Options.withDescription("Minimum 24h quote-volume threshold (0 = disabled)"),
);

const startOption = Options.text("start").pipe(
  Options.optional,
  Options.withDescription("Start date as an ISO-8601 string (e.g. 2025-06-01)"),
);

const endOption = Options.text("end").pipe(
  Options.optional,
  Options.withDescription(
    "End date as an ISO-8601 string (defaults to now if omitted)",
  ),
);

const noResumeOption = Options.boolean("no-resume").pipe(
  Options.withDefault(false),
  Options.withDescription("Delete existing candles before fetching"),
);

const concurrencyOption = Options.integer("concurrency").pipe(
  Options.withDefault(1),
  Options.withDescription("Maximum symbols to fetch in parallel"),
);

const coverageOption = Options.boolean("coverage").pipe(
  Options.withDefault(false),
  Options.withDescription("Print a coverage report before fetching"),
);

const STABLECOIN_BASES = new Set([
  "USDT",
  "USDC",
  "BUSD",
  "TUSD",
  "FDUSD",
  "DAI",
  "PAX",
  "USDD",
  "RLUSD",
]);

function makeLayer(home?: string) {
  return Layer.mergeAll(
    BunServices.layer,
    PathLive(home),
    MarketDataGatewayLive,
  );
}

export const fetchCandlesCommand = Command.make(
  "fetch-candles",
  {
    exchange: exchangeOption,
    symbol: symbolOption,
    timeframe: timeframeOption,
    days: daysOption,
    batch: batchOption,
    start: startOption,
    end: endOption,
    noResume: noResumeOption,
  },
  ({ exchange, symbol, timeframe, days, batch, start, end, noResume }) =>
    Effect.gen(function* () {
      const path = yield* Path;
      const sqlitePath = resolve(path.homeDir, "data", "neuratrade.db");
      const db = yield* Effect.sync(() => {
        const d = new Database(sqlitePath);
        d.exec("PRAGMA foreign_keys = ON;");
        return d;
      });

      const repoLayer = MarketDataRepositorySQLiteLive(db);

      const startDate = yield* parseDateOption(start);
      const endDate = yield* parseDateOption(end);

      const result = yield* fetchCandlesProgram({
        exchange,
        symbol,
        timeframe,
        days,
        batch,
        start: startDate,
        end: endDate,
        noResume,
      }).pipe(
        Effect.provide(repoLayer),
        Effect.tap((total) =>
          Console.log(`Fetched and stored ${total} candles`),
        ),
        Effect.catch((err) =>
          Effect.gen(function* () {
            yield* Console.error(
              `fetch-candles failed: ${failureMessage(err)}`,
            );
            process.exitCode = 1;
            return 0;
          }),
        ),
        Effect.ensuring(Effect.sync(() => db.close())),
      );

      return result;
    }).pipe(Effect.provide(makeLayer(process.env.NEURATRADE_HOME))),
).pipe(
  Command.withDescription("Fetch historical OHLCV candles from an exchange"),
);

interface FetchCandlesArgs {
  readonly exchange: string;
  readonly symbol: string;
  readonly timeframe: string;
  readonly days: number;
  readonly batch: number;
  readonly start?: Date;
  readonly end?: Date;
  readonly noResume?: boolean;
}

const DAY_MS = 24 * 60 * 60 * 1000;

function failureMessage(err: unknown): string {
  if (typeof err === "object" && err !== null) {
    if (
      "reason" in err &&
      typeof err.reason === "string" &&
      err.reason.length > 0
    ) {
      return err.reason;
    }
    if (
      "message" in err &&
      typeof err.message === "string" &&
      err.message.length > 0
    ) {
      return err.message;
    }
  }
  return String(err);
}

function parseDateOption(
  opt: Option.Option<string>,
): Effect.Effect<Date | undefined, MarketDataRepositoryError, never> {
  return Option.match(opt, {
    onNone: () => Effect.succeed(undefined),
    onSome: (value) => {
      const date = new Date(value);
      if (Number.isNaN(date.getTime())) {
        return Effect.fail(
          new MarketDataRepositoryError(`Invalid ISO date: ${value}`),
        );
      }
      return Effect.succeed(date);
    },
  });
}

function fetchCandlesProgram(args: FetchCandlesArgs) {
  return Effect.gen(function* () {
    const repo = yield* MarketDataRepository;
    const gateway = yield* MarketDataGateway;

    yield* repo.ensureTables();

    if (args.noResume) {
      const deleted = yield* repo.deleteCandles(
        args.exchange,
        args.symbol,
        args.timeframe,
      );
      if (deleted > 0) {
        yield* Console.log(
          `Cleared ${deleted} existing candles for ${args.symbol} ${args.timeframe}`,
        );
      }
    }

    const endTime = args.end ? args.end.getTime() : Date.now();
    const startTime = args.start
      ? args.start.getTime()
      : endTime - args.days * DAY_MS;
    const timeframeMs = timeframeToMs(args.timeframe);
    const candlesPerBatch = Math.min(args.batch, 1000);

    let totalSaved = 0;
    let currentStart = startTime;

    while (currentStart < endTime) {
      const candles = yield* gateway.fetchOHLCV(
        args.exchange,
        args.symbol,
        args.timeframe,
        candlesPerBatch,
        new Date(currentStart),
      );

      if (candles.length === 0) break;

      const filtered = candles.filter(
        (c) =>
          c.timestamp.getTime() >= startTime &&
          c.timestamp.getTime() <= endTime,
      );
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

    const stored = yield* repo.getCandles({
      exchange: args.exchange,
      symbol: args.symbol,
      timeframe: args.timeframe,
    });
    if (stored.length > 0) {
      const first = stored[0].timestamp.toISOString();
      const last = stored[stored.length - 1].timestamp.toISOString();
      const expected = Math.floor((endTime - startTime) / timeframeMs) + 1;
      yield* Console.log(
        `Integrity: ${stored.length} candles stored (${first} → ${last}), ~${expected} expected`,
      );
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
  readonly minVolume: number;
  readonly start?: Date;
  readonly end?: Date;
  readonly noResume?: boolean;
  readonly concurrency?: number;
  readonly coverage?: boolean;
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
    minVolume: minVolumeOption,
    start: startOption,
    end: endOption,
    noResume: noResumeOption,
    concurrency: concurrencyOption,
    coverage: coverageOption,
  },
  ({
    exchange,
    timeframe,
    days,
    batch,
    top,
    quote,
    minVolume,
    start,
    end,
    noResume,
    concurrency,
    coverage,
  }) =>
    Effect.gen(function* () {
      const path = yield* Path;
      const sqlitePath = resolve(path.homeDir, "data", "neuratrade.db");
      const db = yield* Effect.sync(() => {
        const d = new Database(sqlitePath);
        d.exec("PRAGMA foreign_keys = ON;");
        return d;
      });

      const repoLayer = MarketDataRepositorySQLiteLive(db);

      const startDate = yield* parseDateOption(start);
      const endDate = yield* parseDateOption(end);

      const result = yield* fetchUniverseProgram({
        exchange,
        timeframe,
        days,
        batch,
        top,
        quote,
        minVolume,
        start: startDate,
        end: endDate,
        noResume,
        concurrency,
        coverage,
      }).pipe(
        Effect.provide(repoLayer),
        Effect.tap((summary) =>
          Console.log(
            `Universe fetch complete: ${summary.symbols.length} symbols, ${summary.totalCandles} candles`,
          ),
        ),
        Effect.catch((err) =>
          Effect.gen(function* () {
            yield* Console.error(
              `fetch-universe failed: ${failureMessage(err)}`,
            );
            process.exitCode = 1;
            return { symbols: [], totalCandles: 0 };
          }),
        ),
        Effect.ensuring(Effect.sync(() => db.close())),
      );

      return result;
    }).pipe(Effect.provide(makeLayer(process.env.NEURATRADE_HOME))),
).pipe(
  Command.withDescription(
    "Fetch historical candles for the top-volume symbol universe",
  ),
);

interface CoverageRow {
  readonly symbol: string;
  readonly count: number;
  readonly expected: number;
  readonly coveragePct: number;
  readonly status: string;
}

function printCoverageTable(rows: readonly CoverageRow[]) {
  const lines = [
    "Symbol".padEnd(12) +
      "Count".padStart(8) +
      "Expected".padStart(12) +
      "Coverage".padStart(12) +
      "Status".padStart(10),
    ...rows.map(
      (r) =>
        r.symbol.padEnd(12) +
        String(r.count).padStart(8) +
        String(r.expected).padStart(12) +
        `${(r.coveragePct * 100).toFixed(1)}%`.padStart(12) +
        r.status.padStart(10),
    ),
  ];
  return Effect.gen(function* () {
    for (const line of lines) {
      yield* Console.log(line);
    }
  });
}

export function fetchUniverseProgram(args: FetchUniverseArgs) {
  return Effect.gen(function* () {
    const repo = yield* MarketDataRepository;
    const gateway = yield* MarketDataGateway;

    yield* repo.ensureTables();

    yield* Console.log(`Loading symbol universe for ${args.quote}...`);
    const allSymbols = yield* gateway.fetchSymbols(args.exchange);
    const quoteSymbols = allSymbols.filter((s) => {
      const [base, quote] = s.split("/");
      return (
        quote === args.quote.toUpperCase() &&
        base !== undefined &&
        !STABLECOIN_BASES.has(base)
      );
    });

    if (quoteSymbols.length === 0) {
      return yield* Effect.fail(
        new MarketDataRepositoryError(
          `No symbols found for quote asset ${args.quote} on ${args.exchange}`,
        ),
      );
    }

    yield* Console.log(
      `Loading 24h volumes to rank ${quoteSymbols.length} ${args.quote} symbols...`,
    );
    const volumes = yield* gateway.fetch24hrVolumes(args.exchange);

    const ranked = quoteSymbols
      .map((symbol) => ({
        symbol,
        volume: volumes[symbol.replace("/", "")] ?? 0,
      }))
      .filter((s) => s.volume >= args.minVolume)
      .sort((a, b) => b.volume - a.volume)
      .slice(0, args.top)
      .map((s) => s.symbol);

    const coverage = args.coverage ?? false;
    const concurrency = args.concurrency ?? 1;

    if (coverage) {
      const reportEnd = args.end ?? new Date();
      const reportStart =
        args.start ?? new Date(reportEnd.getTime() - args.days * DAY_MS);
      yield* Console.log(
        `Coverage report for ${args.exchange} ${args.timeframe} (${reportStart.toISOString()} → ${reportEnd.toISOString()})`,
      );
      const rows = yield* repo.getCoverageReport(
        args.exchange,
        ranked,
        args.timeframe,
        reportStart,
        reportEnd,
      );
      yield* printCoverageTable(rows);
    }

    yield* Console.log(
      `Fetching ${ranked.length} symbols: ${ranked.join(", ")}`,
    );

    const results = yield* Effect.all(
      ranked.map((symbol) =>
        fetchCandlesProgram({
          exchange: args.exchange,
          symbol,
          timeframe: args.timeframe,
          days: args.days,
          batch: args.batch,
          start: args.start,
          end: args.end,
          noResume: args.noResume,
        }),
      ),
      { concurrency },
    );

    let totalCandles = 0;
    for (let i = 0; i < ranked.length; i++) {
      totalCandles += results[i];
      yield* Console.log(`  ${ranked[i]}: ${results[i]} candles`);
    }

    return { symbols: ranked, totalCandles };
  });
}

interface FetchFundingRatesArgs {
  readonly exchange: string;
  readonly symbol: string;
  readonly days: number;
  readonly start?: Date;
  readonly end?: Date;
  readonly noResume?: boolean;
}

function fetchFundingRatesProgram(args: FetchFundingRatesArgs) {
  return Effect.gen(function* () {
    const repo = yield* MarketDataRepository;
    const gateway = yield* MarketDataGateway;

    yield* repo.ensureFundingRatesTable();

    if (args.noResume) {
      const db = (repo as unknown as { db?: Database }).db;
      if (db) {
        db.run("DELETE FROM funding_rates WHERE exchange = ? AND symbol = ?", [
          args.exchange,
          args.symbol,
        ]);
      }
    }

    const endTime = args.end ? args.end.getTime() : Date.now();
    const startTime = args.start
      ? args.start.getTime()
      : endTime - args.days * DAY_MS;

    const rates = yield* gateway.fetchFundingRates(
      args.exchange,
      args.symbol,
      new Date(startTime),
      new Date(endTime),
      1000,
    );

    const saved = yield* repo.saveFundingRates(
      args.exchange,
      args.symbol,
      rates,
    );

    return { fetched: rates.length, saved };
  });
}

export const fetchFundingRatesCommand = Command.make(
  "fetch-funding-rates",
  {
    exchange: exchangeOption,
    symbol: symbolOption,
    days: daysOption,
    start: startOption,
    end: endOption,
    noResume: noResumeOption,
  },
  ({ exchange, symbol, days, start, end, noResume }) =>
    Effect.gen(function* () {
      const path = yield* Path;
      const sqlitePath = resolve(path.homeDir, "data", "neuratrade.db");
      const db = yield* Effect.sync(() => {
        const d = new Database(sqlitePath);
        d.exec("PRAGMA foreign_keys = ON;");
        return d;
      });

      const repoLayer = MarketDataRepositorySQLiteLive(db);

      const startDate = yield* parseDateOption(start);
      const endDate = yield* parseDateOption(end);

      const result = yield* fetchFundingRatesProgram({
        exchange,
        symbol,
        days,
        start: startDate,
        end: endDate,
        noResume,
      }).pipe(
        Effect.provide(repoLayer),
        Effect.tap((r) =>
          Console.log(
            `Fetched ${r.fetched} funding rates, stored ${r.saved} new rows`,
          ),
        ),
        Effect.catch((err) =>
          Effect.gen(function* () {
            yield* Console.error(
              `fetch-funding-rates failed: ${failureMessage(err)}`,
            );
            process.exitCode = 1;
            return { fetched: 0, saved: 0 };
          }),
        ),
        Effect.ensuring(Effect.sync(() => db.close())),
      );

      return result;
    }).pipe(Effect.provide(makeLayer(process.env.NEURATRADE_HOME))),
).pipe(
  Command.withDescription(
    "Fetch perpetual futures funding rates from an exchange",
  ),
);

export const marketCommand = Command.make("market", {}, () =>
  Console.log(
    "Market data commands. Use 'market fetch-candles|fetch-funding-rates|fetch-universe --help' for details.",
  ),
).pipe(
  Command.withDescription("Market data operations"),
  Command.withSubcommands([
    fetchCandlesCommand,
    fetchFundingRatesCommand,
    fetchUniverseCommand,
  ]),
);
