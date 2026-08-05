import { Command, Options } from "./kit/kit.ts";
import { Console, Effect, Layer } from "effect";
import { SqliteClient, SqliteError } from "../services/sqlite.js";
import { money } from "../utils/money.js";
import {
  evaluateDemoSoak,
  serializeDemoSoakReport,
} from "../paper-trading/demo-readiness.js";
import { PaperTradingRepositorySQLite } from "../paper-trading/repository.js";

const commandOptions = {
  exchange: Options.text("exchange").pipe(
    Options.withDefault("bitget-futures"),
    Options.withDescription("Exchange used for the demo soak"),
  ),
  symbol: Options.text("symbol").pipe(
    Options.withDefault("BTC/USDT:USDT"),
    Options.withDescription("Symbol used for the demo soak"),
  ),
  timeframe: Options.text("timeframe").pipe(
    Options.withDefault("15m"),
    Options.withDescription("Timeframe used for the demo soak"),
  ),
  watchlist: Options.boolean("watchlist").pipe(
    Options.withDefault(false),
    Options.withDescription(
      "Aggregate live-fill evidence across ALL symbols for the exchange/timeframe instead of a single symbol",
    ),
  ),
  limit: Options.integer("limit").pipe(
    Options.withDefault(500),
    Options.withDescription("Maximum completed trades to evaluate"),
  ),
  minimumTrades: Options.integer("min-trades").pipe(
    Options.withDefault(10),
    Options.withDescription("Minimum completed live trades"),
  ),
  minimumDurationDays: Options.float("min-duration-days").pipe(
    Options.withDefault(30),
    Options.withDescription("Minimum elapsed demo duration in days"),
  ),
  minimumExpectancyPct: Options.text("min-expectancy-pct").pipe(
    Options.withDefault("0"),
    Options.withDescription("Minimum realized expectancy per trade in percent"),
  ),
  minimumExpectancyLowerBoundPct: Options.text(
    "min-expectancy-lower-bound-pct",
  ).pipe(
    Options.withDefault("0"),
    Options.withDescription(
      "Minimum bootstrap confidence lower bound for expectancy in percent",
    ),
  ),
  maximumDrawdownPct: Options.text("max-drawdown-pct").pipe(
    Options.withDefault("15"),
    Options.withDescription("Maximum realized drawdown in percent"),
  ),
} as const;

function validateArgs(args: {
  readonly limit: number;
  readonly minimumTrades: number;
  readonly minimumDurationDays: number;
  readonly minimumExpectancyPct: string;
  readonly minimumExpectancyLowerBoundPct: string;
  readonly maximumDrawdownPct: string;
}): Effect.Effect<void, Error> {
  return Effect.try({
    try: () => {
      if (args.limit < 1) throw new Error("--limit must be positive");
      if (args.minimumTrades < 1) {
        throw new Error("--min-trades must be positive");
      }
      if (args.minimumDurationDays < 0) {
        throw new Error("--min-duration-days cannot be negative");
      }
      if (money(args.minimumExpectancyPct).isNaN()) {
        throw new Error("--min-expectancy-pct must be a decimal");
      }
      if (money(args.minimumExpectancyLowerBoundPct).isNaN()) {
        throw new Error("--min-expectancy-lower-bound-pct must be a decimal");
      }
      if (money(args.maximumDrawdownPct).isNegative()) {
        throw new Error("--max-drawdown-pct cannot be negative");
      }
    },
    catch: (error) =>
      error instanceof Error ? error : new Error(String(error)),
  });
}

export function makeDemoReadinessCommand(
  dbLayer: Layer.Layer<SqliteClient, SqliteError, never>,
) {
  return Command.make("demo-readiness", commandOptions, (args) =>
    Effect.gen(function* () {
      yield* validateArgs(args);
      const sqlite = yield* SqliteClient;
      const repository = new PaperTradingRepositorySQLite(sqlite.database);
      yield* repository.ensureTables();
      const allTrades = args.watchlist
        ? yield* repository.listAllGridTrades(
            args.exchange,
            args.timeframe,
            args.limit,
            true,
          )
        : yield* repository.listRecentGridTrades(
            args.exchange,
            args.symbol,
            args.timeframe,
            args.limit,
          );
      // Stale simulated trades from earlier runs share the same key and
      // must never count toward the minimums or block the verdict.
      const trades = allTrades.filter((trade) => trade.fillSource === "live");
      const report = evaluateDemoSoak(trades, {
        minimumTrades: args.minimumTrades,
        minimumDurationDays: args.minimumDurationDays,
        minimumExpectancyPct: money(args.minimumExpectancyPct),
        minimumExpectancyLowerBoundPct: money(
          args.minimumExpectancyLowerBoundPct,
        ),
        maximumDrawdownPct: money(args.maximumDrawdownPct),
      });
      const scope = args.watchlist
        ? `all ${args.exchange}:${args.timeframe} symbols`
        : `${args.symbol}`;
      yield* Console.log(
        `[demo-readiness] scope: ${scope}; live trades: ${trades.length}`,
      );
      yield* Console.log(serializeDemoSoakReport(report));
      if (!report.passed) {
        return yield* Effect.fail(new Error("demo-soak gate failed"));
      }
      return report;
    }).pipe(Effect.provide(dbLayer)),
  ).pipe(
    Command.withDescription(
      "Evaluate persisted live-fill evidence; exits non-zero until the demo gate passes",
    ),
  );
}
