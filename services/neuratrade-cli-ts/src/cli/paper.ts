/**
 * `neuratrade paper` commands — deterministic scalping paper trading.
 *
 * Uses the backend backtest endpoint to generate signals on recent real
 * candles and simulates order execution in SQLite.
 */
import { Command, Options } from "@effect/cli";
import { Console, Effect } from "effect";
import { PaperRepository } from "../services/paper-repository.ts";
import { PaperTradingEngine } from "../services/paper-trading-engine.ts";

// ---------------------------------------------------------------------------
// Options
// ---------------------------------------------------------------------------

const symbolOption = Options.text("symbol").pipe(
  Options.withDescription("Trading pair, e.g. BTC/USDT"),
  Options.withDefault("BTC/USDT"),
);

const exchangeOption = Options.text("exchange").pipe(
  Options.withDescription("Exchange name"),
  Options.withDefault("binance"),
);

const capitalOption = Options.text("capital").pipe(
  Options.withDescription("Paper capital allocated to this symbol"),
  Options.withDefault("10000"),
);

const windowOption = Options.integer("window-hours").pipe(
  Options.withDescription("Number of hours to look back for signal generation"),
  Options.withDefault(48),
);

const timeframeOption = Options.text("timeframe").pipe(
  Options.withDescription("Candle timeframe"),
  Options.withDefault("1h"),
);

const feeRateOption = Options.text("fee-rate").pipe(
  Options.withDescription("Taker fee rate per side, e.g. 0.001"),
  Options.withDefault("0.001"),
);

const modeOption = Options.text("mode").pipe(
  Options.withDescription("Signal mode: deterministic (default) or ai"),
  Options.withDefault("deterministic"),
);

const leverageOption = Options.integer("leverage").pipe(
  Options.withDescription(
    "Futures leverage multiplier (default: 1 = no leverage)",
  ),
  Options.withDefault(1),
);

const riskPctOption = Options.text("risk-pct").pipe(
  Options.withDescription(
    "Capital risked per trade as a decimal, e.g. 0.01 = 1%",
  ),
  Options.withDefault("0.01"),
);

const stopLossPctOption = Options.text("stop-loss-pct").pipe(
  Options.withDescription("Stop-loss distance as a decimal, e.g. 0.005 = 0.5%"),
  Options.withDefault("0.005"),
);

const takeProfitPctOption = Options.text("take-profit-pct").pipe(
  Options.withDescription(
    "Take-profit distance as a decimal, e.g. 0.015 = 1.5%",
  ),
  Options.withDefault("0.015"),
);

const trailingStopPctOption = Options.text("trailing-stop-pct").pipe(
  Options.withDescription(
    "Trailing-stop distance as a decimal, e.g. 0.004 = 0.4%; 0 to disable",
  ),
  Options.withDefault("0"),
);

const maxHoldHoursOption = Options.integer("max-hold-hours").pipe(
  Options.withDescription("Maximum position hold time in hours"),
  Options.withDefault(24),
);

const dryRunOption = Options.boolean("dry-run").pipe(
  Options.withDescription(
    "Simulate the trade but do not write to the database",
  ),
  Options.withDefault(false),
);

const limitOption = Options.integer("limit").pipe(
  Options.withDescription("Number of closed trades to show"),
  Options.withDefault(10),
);

// ---------------------------------------------------------------------------
// trade command
// ---------------------------------------------------------------------------

const tradeCommand = Command.make(
  "trade",
  {
    symbol: symbolOption,
    exchange: exchangeOption,
    capital: capitalOption,
    windowHours: windowOption,
    timeframe: timeframeOption,
    feeRate: feeRateOption,
    mode: modeOption,
    leverage: leverageOption,
    riskPct: riskPctOption,
    stopLossPct: stopLossPctOption,
    takeProfitPct: takeProfitPctOption,
    trailingStopPct: trailingStopPctOption,
    maxHoldHours: maxHoldHoursOption,
    dryRun: dryRunOption,
  },
  (args) =>
    Effect.gen(function* () {
      if (args.mode !== "deterministic") {
        return yield* Effect.fail(
          new Error(`mode ${args.mode} is not supported; use deterministic`),
        );
      }
      const engine = yield* PaperTradingEngine;

      const runCycle = () =>
        Effect.gen(function* () {
          const result = yield* engine
            .evaluateAndTrade({
              symbol: args.symbol,
              exchange: args.exchange,
              capital: args.capital,
              windowHours: args.windowHours,
              timeframe: args.timeframe,
              feeRate: args.feeRate,
              mode: args.mode,
              leverage: args.leverage,
              riskPct: args.riskPct,
              stopLossPct: args.stopLossPct,
              takeProfitPct: args.takeProfitPct,
              trailingStopPct: args.trailingStopPct,
              maxHoldHours: args.maxHoldHours,
              dryRun: args.dryRun,
            })
            .pipe(
              Effect.catchAll((err) =>
                Effect.succeed({
                  action: "no_signal" as const,
                  message: `error: ${err.reason}`,
                }),
              ),
            );
          yield* Console.log(
            `[${new Date().toISOString()}] ${result.action}: ${result.message}`,
          );
        }).pipe(Effect.catchAll(() => Effect.void));

      yield* runCycle();
    }).pipe(
      Effect.catchAll((err: unknown) => {
        const message = err instanceof Error ? err.message : String(err);
        return Console.log(`❌ paper trade failed: ${message}`).pipe(
          Effect.flatMap(() => Effect.fail(new Error(message))),
        );
      }),
    ),
).pipe(Command.withDescription("Run deterministic scalping paper trades"));

// ---------------------------------------------------------------------------
// status command
// ---------------------------------------------------------------------------

const statusCommand = Command.make(
  "status",
  { limit: limitOption },
  ({ limit }) =>
    Effect.gen(function* () {
      const repo = yield* PaperRepository;
      const open = yield* repo.listOpenTrades();
      const closed = yield* repo.listClosedTrades(limit);
      const allClosed = yield* repo.listClosedTrades(10_000);
      const stats = yield* repo.getStats();
      const readiness = computeReadinessMetrics(allClosed);

      yield* Console.log(`Paper trading status`);
      yield* Console.log(`  open trades:   ${stats.open_count}`);
      yield* Console.log(`  closed trades: ${stats.closed_count}`);
      yield* Console.log(`  total pnl:     ${stats.total_pnl}`);
      yield* Console.log(
        `  wins / losses: ${stats.win_count} / ${stats.loss_count}`,
      );
      yield* Console.log("\nReadiness metrics:");
      yield* Console.log(`  win_rate:       ${readiness.winRate}`);
      yield* Console.log(`  profit_factor:  ${readiness.profitFactor}`);
      yield* Console.log(`  expectancy:     ${readiness.expectancy}`);
      yield* Console.log(`  max_drawdown:   ${readiness.maxDrawdown}`);
      yield* Console.log(`  avg_win:        ${readiness.avgWin}`);
      yield* Console.log(`  avg_loss:       ${readiness.avgLoss}`);
      yield* Console.log(`  avg_hold_hours: ${readiness.avgHoldHours}`);
      yield* Console.log(`  ready_guess:    ${readiness.readyGuess}`);

      if (open.length > 0) {
        yield* Console.log("\nOpen trades:");
        for (const t of open) {
          yield* Console.log(
            `  #${t.id} ${t.side} ${t.size} ${t.symbol} @ ${t.entry_price} (${t.entry_at})`,
          );
        }
      }

      if (closed.length > 0) {
        yield* Console.log("\nRecent closed trades:");
        for (const t of closed) {
          yield* Console.log(
            `  #${t.id} ${t.side} ${t.size} ${t.symbol} @ ${t.entry_price} -> ${t.exit_price} pnl=${t.pnl}`,
          );
        }
      }
    }).pipe(
      Effect.catchAll((err) =>
        Console.log(`❌ paper status failed: ${err.message}`).pipe(
          Effect.flatMap(() => Effect.fail(err)),
        ),
      ),
    ),
).pipe(Command.withDescription("Show paper trading positions and PnL"));

// ---------------------------------------------------------------------------
// close command
// ---------------------------------------------------------------------------

const closeCommand = Command.make(
  "close",
  {
    tradeId: Options.integer("trade-id").pipe(
      Options.withDescription("Paper trade ID to close"),
    ),
    price: Options.text("price").pipe(
      Options.withDescription("Manual exit price"),
    ),
    reason: Options.text("reason").pipe(
      Options.withDescription("Exit reason"),
      Options.withDefault("manual"),
    ),
    feeRate: feeRateOption,
  },
  ({ tradeId, price, reason, feeRate }) =>
    Effect.gen(function* () {
      const repo = yield* PaperRepository;
      const open = yield* repo.listOpenTrades();
      const trade = open.find((t) => t.id === tradeId);
      if (trade === undefined) {
        return yield* Effect.fail(
          new Error(`no open paper trade with id ${tradeId}`),
        );
      }
      const grossPnl = multiply(subtract(price, trade.entry_price), trade.size);
      const fees = multiply(
        add(trade.notional, multiply(trade.size, price)),
        feeRate,
      );
      const pnl = subtract(grossPnl, fees);
      const pnlPct =
        compare(trade.notional, "0") === 0
          ? "0"
          : multiply(divide(pnl, trade.notional), "100");
      yield* repo.closeTrade({
        id: tradeId,
        exit_price: price,
        exit_at: new Date().toISOString(),
        pnl,
        pnl_pct: pnlPct,
        fees,
        exit_reason: reason,
      });
      yield* Console.log(
        `✅ Closed paper trade #${tradeId} @ ${price} (pnl ${pnl})`,
      );
    }).pipe(
      Effect.catchAll((err) =>
        Console.log(`❌ paper close failed: ${err.message}`).pipe(
          Effect.flatMap(() => Effect.fail(err)),
        ),
      ),
    ),
).pipe(Command.withDescription("Manually close an open paper trade"));

// ---------------------------------------------------------------------------
// Decimal helpers duplicated locally to keep the CLI file self-contained
// ---------------------------------------------------------------------------

function countDecimals(value: string): number {
  const dotIndex = value.indexOf(".");
  return dotIndex === -1 ? 0 : value.length - dotIndex - 1;
}

function toScaled(value: string, scale: number): bigint {
  const [intPart = "0", fracPart = ""] = value.split(".");
  const padded = fracPart.padEnd(scale, "0").slice(0, scale);
  const sign = intPart.startsWith("-") ? "-" : "";
  const abs = sign ? intPart.slice(1) : intPart;
  return BigInt(`${sign}${abs}${padded}`);
}

function fromScaled(value: bigint, scale: number): string {
  if (scale === 0) return value.toString();
  const negative = value < 0n;
  const abs = negative ? -value : value;
  const str = abs.toString().padStart(scale + 1, "0");
  const intPart = str.slice(0, -scale) || "0";
  const fracPart = str.slice(-scale).replace(/0+$/, "");
  return `${negative ? "-" : ""}${intPart}${fracPart ? `.${fracPart}` : ""}`;
}

function multiply(a: string, b: string): string {
  const sa = countDecimals(a);
  const sb = countDecimals(b);
  return fromScaled(toScaled(a, sa) * toScaled(b, sb), sa + sb);
}

function divide(a: string, b: string): string {
  const scale = Math.max(countDecimals(a), countDecimals(b)) + 8;
  const sb = toScaled(b, scale);
  if (sb === 0n) return "0";
  return fromScaled((toScaled(a, scale) * 10n ** 8n) / sb, 8);
}

function add(a: string, b: string): string {
  const scale = Math.max(countDecimals(a), countDecimals(b));
  return fromScaled(toScaled(a, scale) + toScaled(b, scale), scale);
}

function subtract(a: string, b: string): string {
  const scale = Math.max(countDecimals(a), countDecimals(b));
  return fromScaled(toScaled(a, scale) - toScaled(b, scale), scale);
}

function compare(a: string, b: string): number {
  const scale = Math.max(countDecimals(a), countDecimals(b));
  const sa = toScaled(a, scale);
  const sb = toScaled(b, scale);
  if (sa < sb) return -1;
  if (sa > sb) return 1;
  return 0;
}

function abs(value: string): string {
  return value.startsWith("-") ? value.slice(1) : value;
}

// ---------------------------------------------------------------------------
// Readiness metrics
// ---------------------------------------------------------------------------

interface ReadinessMetrics {
  readonly winRate: string;
  readonly profitFactor: string;
  readonly expectancy: string;
  readonly maxDrawdown: string;
  readonly avgWin: string;
  readonly avgLoss: string;
  readonly avgHoldHours: string;
  readonly readyGuess: string;
}

function computeReadinessMetrics(
  closed: ReadonlyArray<{
    readonly pnl: string | null;
    readonly entry_at: string;
    readonly exit_at: string | null;
  }>,
): ReadinessMetrics {
  const trades = closed
    .filter((t) => t.pnl !== null && t.exit_at !== null)
    .map((t) => ({
      pnl: t.pnl as string,
      holdHours:
        (new Date(t.exit_at as string).getTime() -
          new Date(t.entry_at).getTime()) /
        3_600_000,
    }));

  if (trades.length === 0) {
    return {
      winRate: "0",
      profitFactor: "0",
      expectancy: "0",
      maxDrawdown: "0",
      avgWin: "0",
      avgLoss: "0",
      avgHoldHours: "0",
      readyGuess: "no data",
    };
  }

  const wins = trades.filter((t) => compare(t.pnl, "0") > 0);
  const losses = trades.filter((t) => compare(t.pnl, "0") <= 0);
  const winRate = multiply(
    divide(String(wins.length), String(trades.length)),
    "100",
  );

  const grossProfit = wins.reduce((sum, t) => add(sum, t.pnl), "0");
  const grossLoss = losses.reduce((sum, t) => add(sum, abs(t.pnl)), "0");
  const profitFactor =
    compare(grossLoss, "0") === 0
      ? compare(grossProfit, "0") === 0
        ? "0"
        : "999"
      : divide(grossProfit, grossLoss);

  const avgWin =
    wins.length === 0
      ? "0"
      : divide(
          wins.reduce((sum, t) => add(sum, t.pnl), "0"),
          String(wins.length),
        );
  const avgLoss =
    losses.length === 0
      ? "0"
      : divide(
          losses.reduce((sum, t) => add(sum, abs(t.pnl)), "0"),
          String(losses.length),
        );

  const winRateDecimal = divide(String(wins.length), String(trades.length));
  const lossRateDecimal = subtract("1", winRateDecimal);
  const expectancy = subtract(
    multiply(avgWin, winRateDecimal),
    multiply(avgLoss, lossRateDecimal),
  );

  // Max drawdown from equity curve.
  let peak = "0";
  let maxDrawdown = "0";
  let equity = "0";
  for (const t of trades) {
    equity = add(equity, t.pnl);
    if (compare(equity, peak) > 0) peak = equity;
    const drawdown = subtract(peak, equity);
    if (compare(drawdown, maxDrawdown) > 0) maxDrawdown = drawdown;
  }

  const avgHoldHours =
    trades.reduce((sum, t) => sum + t.holdHours, 0) / trades.length;

  const closedCount = trades.length;
  const ready =
    closedCount >= 20 &&
    compare(winRate, "50") > 0 &&
    compare(expectancy, "0") > 0 &&
    compare(maxDrawdown, multiply("0.05", "100")) < 0;

  return {
    winRate,
    profitFactor,
    expectancy,
    maxDrawdown,
    avgWin,
    avgLoss,
    avgHoldHours: avgHoldHours.toFixed(2),
    readyGuess: ready ? "yes" : "no",
  };
}

// ---------------------------------------------------------------------------
// Public export
// ---------------------------------------------------------------------------

export const paperCommand = Command.make("paper", {}, () =>
  Console.log("Usage: neuratrade paper <trade|status|close>"),
).pipe(
  Command.withDescription("Deterministic scalping paper trading"),
  Command.withSubcommands([tradeCommand, statusCommand, closeCommand]),
);
