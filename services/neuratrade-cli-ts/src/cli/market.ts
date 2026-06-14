/**
 * `neuratrade market` commands — historical market data collection and
 * coverage validation.
 */
import { Command, Options } from "@effect/cli";
import { Console, Effect } from "effect";
import { BinanceClient } from "../services/binance-client.ts";
import {
  MarketRepository,
  type Candle,
  type CoverageGap,
} from "../services/market-repository.ts";
import { errorMessage } from "../utils/error-message.ts";

// ---------------------------------------------------------------------------
// Option definitions
// ---------------------------------------------------------------------------

const topOption = Options.integer("top").pipe(
  Options.withDescription("Limit universe to the top N USDT symbols"),
  Options.withDefault(0),
);

const allOption = Options.boolean("all").pipe(
  Options.withDescription("Include all USDT symbols"),
  Options.withDefault(false),
);

const dryRunOption = Options.boolean("dry-run").pipe(
  Options.withDescription("Print symbols without persisting them"),
  Options.withDefault(false),
);

const symbolsOption = Options.text("symbols").pipe(
  Options.withDescription(
    "Comma-separated symbol list, e.g. BTC/USDT,ETH/USDT",
  ),
  Options.withDefault(""),
);

const startOption = Options.text("start").pipe(
  Options.withDescription("Start date (ISO 8601 or RFC3339)"),
  Options.withDefault(""),
);

const endOption = Options.text("end").pipe(
  Options.withDescription("End date (ISO 8601 or RFC3339)"),
  Options.withDefault(""),
);

const timeframeOption = Options.text("timeframe").pipe(
  Options.withDescription("Candle timeframe, e.g. 1h, 1d"),
  Options.withDefault("1h"),
);

const concurrencyOption = Options.integer("concurrency").pipe(
  Options.withDescription("Number of symbols to fetch in parallel"),
  Options.withDefault(4),
);

const noResumeOption = Options.boolean("no-resume").pipe(
  Options.withDescription(
    "Ignore existing candles and re-fetch the full range",
  ),
  Options.withDefault(false),
);

const exchangeOption = Options.text("exchange").pipe(
  Options.withDescription("Exchange name"),
  Options.withDefault("binance"),
);

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

export function parseDate(input: string): Date {
  const trimmed = input.trim();
  if (trimmed === "") {
    throw new Error(`empty date`);
  }
  const parsed = new Date(trimmed);
  if (Number.isNaN(parsed.getTime())) {
    throw new Error(`invalid date: ${input}`);
  }
  return parsed;
}

export function resolveDateRange(
  startInput: string,
  endInput: string,
): { readonly start: Date; readonly end: Date } {
  const now = new Date();
  const oneYearAgo = new Date(
    Date.UTC(now.getUTCFullYear() - 1, now.getUTCMonth(), now.getUTCDate()),
  );

  const start = startInput.trim() !== "" ? parseDate(startInput) : oneYearAgo;
  const end = endInput.trim() !== "" ? parseDate(endInput) : now;

  if (start.getTime() >= end.getTime()) {
    throw new Error("--start must be before --end");
  }
  return { start, end };
}

export function parseSymbols(input: string): ReadonlyArray<string> {
  return input
    .split(",")
    .map((s) => s.trim().toUpperCase())
    .filter((s) => s.length > 0);
}

export function timeframeToIntervalMs(timeframe: string): number {
  const match = timeframe.trim().match(/^(\d+)([mhdwM])$/);
  if (!match) {
    throw new Error(`Unsupported timeframe: ${timeframe}`);
  }
  const value = parseInt(match[1], 10);
  const unit = match[2];
  const multipliers: Record<string, number> = {
    m: 60_000,
    h: 3_600_000,
    d: 86_400_000,
    w: 604_800_000,
    M: 2_592_000_000,
  };
  return value * multipliers[unit];
}

export function chunksForRange(
  start: Date,
  end: Date,
  intervalMs: number,
  limit: number,
): ReadonlyArray<CoverageGap> {
  const chunks: Array<CoverageGap> = [];
  let cursor = start.getTime();
  const endTime = end.getTime();
  while (cursor < endTime) {
    const chunkEnd = Math.min(cursor + intervalMs * limit, endTime);
    chunks.push({ from: new Date(cursor), to: new Date(chunkEnd) });
    cursor = chunkEnd;
  }
  return chunks;
}

function formatDate(date: Date): string {
  return date.toISOString();
}

// ---------------------------------------------------------------------------
// fetch-universe
// ---------------------------------------------------------------------------

const fetchUniverseCommand = Command.make(
  "fetch-universe",
  {
    top: topOption,
    all: allOption,
    dryRun: dryRunOption,
    exchange: exchangeOption,
  },
  ({ top, all, dryRun, exchange }) =>
    Effect.gen(function* () {
      if (top <= 0 && !all) {
        return yield* Effect.fail(new Error("Specify --top N or --all"));
      }

      const binance = yield* BinanceClient;
      const repo = yield* MarketRepository;

      const info = yield* binance.getExchangeInfo();
      const usdtSymbols = info.symbols
        .filter((s) => s.status === "TRADING" && s.quoteAsset === "USDT")
        .map((s) => `${s.baseAsset}/${s.quoteAsset}`)
        .sort();

      const selected = top > 0 ? usdtSymbols.slice(0, top) : usdtSymbols;

      if (dryRun) {
        yield* Console.log(`Would fetch ${selected.length} symbols`);
        for (const symbol of selected.slice(0, 20)) {
          yield* Console.log(`  ${symbol}`);
        }
        if (selected.length > 20) {
          yield* Console.log(`  ... and ${selected.length - 20} more`);
        }
        return;
      }

      const exchangeId = yield* repo.ensureExchange(exchange);
      yield* Effect.forEach(
        selected,
        (symbol) => repo.ensureTradingPair(symbol, exchangeId),
        { concurrency: 8 },
      );

      yield* Console.log(`✅ Persisted ${selected.length} trading pairs`);
    }).pipe(
      Effect.catchAll((err) =>
        Console.log(`❌ fetch-universe failed: ${errorMessage(err)}`).pipe(
          Effect.flatMap(() => Effect.fail(err)),
        ),
      ),
    ),
).pipe(Command.withDescription("Fetch and persist USDT trading universe"));

// ---------------------------------------------------------------------------
// fetch-candles
// ---------------------------------------------------------------------------

const fetchCandlesCommand = Command.make(
  "fetch-candles",
  {
    symbols: symbolsOption,
    start: startOption,
    end: endOption,
    timeframe: timeframeOption,
    concurrency: concurrencyOption,
    noResume: noResumeOption,
    exchange: exchangeOption,
  },
  ({ symbols, start, end, timeframe, concurrency, noResume, exchange }) =>
    Effect.gen(function* () {
      const symbolList = parseSymbols(symbols);
      if (symbolList.length === 0) {
        return yield* Effect.fail(new Error("--symbols is required"));
      }

      const { start: startDate, end: endDate } = resolveDateRange(start, end);
      const intervalMs = timeframeToIntervalMs(timeframe);

      const binance = yield* BinanceClient;
      const repo = yield* MarketRepository;

      const exchangeId = yield* repo.ensureExchange(exchange);

      yield* Console.log(
        `Fetching ${timeframe} candles from ${formatDate(startDate)} to ${formatDate(endDate)}`,
      );

      const totalInserted = yield* Effect.forEach(
        symbolList,
        (symbol) =>
          Effect.gen(function* () {
            const pairId = yield* repo.ensureTradingPair(symbol, exchangeId);

            const gaps = noResume
              ? [{ from: startDate, to: endDate }]
              : yield* repo.findCoverageGaps({
                  exchangeId,
                  pairId,
                  timeframe,
                  start: startDate,
                  end: endDate,
                  intervalMs,
                });

            if (gaps.length === 0) {
              yield* Console.log(`  ${symbol}: already complete`);
              return 0;
            }

            let insertedForSymbol = 0;
            for (const gap of gaps) {
              const chunks = chunksForRange(gap.from, gap.to, intervalMs, 1000);
              for (const chunk of chunks) {
                const klines = yield* binance.getKlines({
                  symbol,
                  interval: timeframe,
                  startTime: chunk.from.getTime(),
                  endTime: chunk.to.getTime() - 1,
                  limit: 1000,
                });

                if (klines.length === 0) continue;

                const candles: ReadonlyArray<Candle> = klines.map((k) => ({
                  exchangeId,
                  pairId,
                  timeframe,
                  timestamp: k.timestamp,
                  open: k.open,
                  high: k.high,
                  low: k.low,
                  close: k.close,
                  volume: k.volume,
                }));

                yield* repo.insertCandles(candles);
                insertedForSymbol += candles.length;
              }
            }

            yield* Console.log(
              `  ${symbol}: inserted ${insertedForSymbol} candles`,
            );
            return insertedForSymbol;
          }),
        { concurrency },
      );

      yield* Console.log(
        `✅ Inserted ${totalInserted.reduce((a, b) => a + b, 0)} candles total`,
      );
    }).pipe(
      Effect.catchAll((err) =>
        Console.log(`❌ fetch-candles failed: ${errorMessage(err)}`).pipe(
          Effect.flatMap(() => Effect.fail(err)),
        ),
      ),
    ),
).pipe(Command.withDescription("Fetch and store historical OHLCV candles"));

// ---------------------------------------------------------------------------
// coverage
// ---------------------------------------------------------------------------

const coverageCommand = Command.make(
  "coverage",
  {
    symbols: symbolsOption,
    start: startOption,
    end: endOption,
    timeframe: timeframeOption,
    exchange: exchangeOption,
  },
  ({ symbols, start, end, timeframe, exchange }) =>
    Effect.gen(function* () {
      const symbolList = parseSymbols(symbols);
      if (symbolList.length === 0) {
        return yield* Effect.fail(new Error("--symbols is required"));
      }

      const { start: startDate, end: endDate } = resolveDateRange(start, end);
      const intervalMs = timeframeToIntervalMs(timeframe);

      const repo = yield* MarketRepository;
      const exchangeId = yield* repo.ensureExchange(exchange);

      yield* Console.log("Symbol coverage report");
      yield* Console.log("======================");

      for (const symbol of symbolList) {
        const pairId = yield* repo.ensureTradingPair(symbol, exchangeId);
        const range = yield* repo.getCandleRange({
          exchangeId,
          pairId,
          timeframe,
          start: startDate,
          end: endDate,
        });
        const gaps = yield* repo.findCoverageGaps({
          exchangeId,
          pairId,
          timeframe,
          start: startDate,
          end: endDate,
          intervalMs,
        });

        yield* Console.log(`${symbol}:`);
        yield* Console.log(
          `  earliest: ${range.earliest ? formatDate(range.earliest) : "n/a"}`,
        );
        yield* Console.log(
          `  latest:   ${range.latest ? formatDate(range.latest) : "n/a"}`,
        );
        yield* Console.log(`  count:    ${range.count}`);
        yield* Console.log(`  gaps:     ${gaps.length}`);
        for (const gap of gaps) {
          yield* Console.log(
            `    ${formatDate(gap.from)} → ${formatDate(gap.to)}`,
          );
        }
      }
    }).pipe(
      Effect.catchAll((err) =>
        Console.log(`❌ coverage failed: ${errorMessage(err)}`).pipe(
          Effect.flatMap(() => Effect.fail(err)),
        ),
      ),
    ),
).pipe(Command.withDescription("Print candle coverage report"));

// ---------------------------------------------------------------------------
// Root market command
// ---------------------------------------------------------------------------

export const marketCommand = Command.make("market", {}, () =>
  Console.log(
    "Use 'market fetch-universe', 'market fetch-candles', or 'market coverage'.",
  ),
).pipe(
  Command.withDescription("Market data collection and coverage"),
  Command.withSubcommands([
    fetchUniverseCommand,
    fetchCandlesCommand,
    coverageCommand,
  ]),
);
