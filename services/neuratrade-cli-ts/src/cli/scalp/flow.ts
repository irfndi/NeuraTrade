// Flow Ignition (flow-v1) commands: backtest + universe + recorder + live
// trade engine. Extracted from the scalp.ts god-module; behavior unchanged.
import { Command, Options } from "../kit/kit.ts";
import { BunServices } from "@effect/platform-bun";
import { Console, Duration, Effect, Layer, Option } from "effect";
import { PathLive } from "../../services/path.js";
import { SqliteClient } from "../../services/sqlite.js";
import {
  MarketDataRepositoryError,
  MarketDataRepositorySQLiteLive,
} from "../../market-data/repository.js";
import {
  MarketDataGateway,
  type MarketDataGatewayService,
} from "../../market-data/gateway.js";
import { MarketDataGatewayRepositoryLive } from "../../market-data/gateway-repository.js";
import {
  BybitFuturesExchangeAdapterLive,
  BybitClientLiveConfig,
} from "../../exchange/adapters/bybit-futures.js";
import { BybitConfigLive } from "../../services/bybit-config.js";
import { SimulatedFuturesExchangeAdapterLive } from "../../exchange/adapters/simulated-futures.js";
import {
  FuturesExchangeAdapter,
  type FuturesExchangeAdapterService,
} from "../../exchange/futures-adapter.js";
import { RiskGuard, RiskGuardLive } from "../../risk/guards.js";
import { KillSwitch, KillSwitchSQLiteLive } from "../../risk/kill-switch.js";
import {
  CircuitBreaker,
  CircuitBreakerSQLiteLive,
} from "../../risk/circuit-breaker.js";
import {
  PaperTradingRepository,
  PaperTradingRepositorySQLite,
  PaperTradingRepositorySQLiteLive,
} from "../../paper-trading/repository.js";
import {
  freshFlowTradeState,
  iterateFlowTrade,
  type FlowTradeError,
  type FlowTradeIterationResult,
  type FlowTradeOptions,
} from "../../scalping/flow-trade.js";
import {
  runFlowRecorder,
  resolveFlowSymbols,
  type FlowRecorderRepository,
} from "../../scalping/flow-recorder.js";
import {
  runFlowBacktest,
  defaultFlowBacktestOptions,
  type FlowBacktestData,
  type FlowBacktestOptions,
  type FlowBacktestReport,
  type FlowSymbolSeries,
} from "../../scalping/flow-backtest.js";
import {
  selectFlowUniverse,
  type FlowInstrument,
  type FlowUniverseEntry,
} from "../../scalping/flow-universe.js";
import {
  fetchTickers,
  fetchInstruments,
} from "../../market-data/gateways/bybit.js";
import {
  capitalOption,
  maxPositionSizeOption,
  leverageOption,
  intervalOption,
  iterationsOption,
  minCapitalOption,
  maxDrawdownOption,
  maxDailyLossOption,
  marginModeOption,
  productTypeOption,
  liveOption,
  killSwitchOption,
  disengageOption,
  flowSymbolsOption,
  flowStartOption,
  flowEndOption,
  flowTimeframeOption,
  flowThresholdOption,
  flowHoldTimesOption,
  flowFeeOption,
  flowSpreadBpsOption,
  flowConservativeFillRateOption,
  flowMaxBreakevenWinRateOption,
  flowLimitOption,
  flowMinTurnoverOption,
  flowUniverseDataSourceOption,
  flowTradeExchangeOption,
  flowTradeSymbolOption,
  flowHoldMinutesOption,
} from "../scalp-options/index.js";
import {
  type MutablePartialRiskLimits,
  makeDbLayer,
  parseMarginMode,
  parseProductType,
} from "./shared.js";

/** Wire symbol → canonical candle form: "BTCUSDT" → "BTC/USDT". */
function wireToCanonicalSymbol(symbol: string): string {
  return symbol.endsWith("USDT") && !symbol.includes("/")
    ? `${symbol.slice(0, -4)}/${symbol.slice(-4)}`
    : symbol;
}

function signedPct(value: number): string {
  return `${value >= 0 ? "+" : ""}${value.toFixed(3)}%`;
}

function formatFlowBacktestReport(report: FlowBacktestReport): string {
  const lines: string[] = [];
  lines.push("Flow-v1 backtest report");
  lines.push(
    `  windows (train ${report.options.trainDays}d / test ${report.options.testDays}d / steps ${report.windows.length}):`,
  );
  for (const w of report.windows) {
    lines.push(
      `    #${w.index} test ${new Date(w.testStart).toISOString().slice(0, 16)}..${new Date(w.testEnd).toISOString().slice(0, 16)}: ${w.signals} signals (${w.purged} purged at boundary)`,
    );
  }
  lines.push(
    "  hold-time | trades | win %  | avg edge/trade | max DD  | expectancy",
  );
  for (const h of report.byHoldTime) {
    lines.push(
      `  ${String(h.holdTimeHours).padStart(4)}h    | ${String(h.totalTrades).padStart(6)} | ${(h.winRate * 100).toFixed(1).padStart(5)}% | ${signedPct(h.avgEdgePerTradePct).padStart(15)} | ${h.maxDrawdownPct.toFixed(2).padStart(6)}% | ${signedPct(h.expectancyPct).padStart(9)} | BE ${(h.breakevenWinRate * 100).toFixed(1)}% ${h.passesHonestyGates ? "PASS" : "REJECT"}`,
    );
  }
  const p = report.portfolio;
  lines.push(
    `  portfolio (hold ${p.holdTimeHours}h): ${p.totalTrades} trades, ${(p.winRate * 100).toFixed(1)}% win, ${signedPct(p.avgEdgePerTradePct)} avg edge/trade, ${p.maxDrawdownPct.toFixed(2)}% max DD, expectancy ${signedPct(p.expectancyPct)}`,
  );
  lines.push("  per-symbol:");
  for (const s of p.bySymbol) {
    lines.push(
      `    ${s.symbol.padEnd(12)} ${String(s.trades).padStart(4)} trades  ${(s.winRate * 100).toFixed(1).padStart(5)}% win  ${signedPct(s.avgEdgePct)} avg edge`,
    );
  }
  return lines.join("\n");
}

function formatFlowUniverse(entries: readonly FlowUniverseEntry[]): string {
  const lines: string[] = [];
  lines.push("rank  symbol       turnover24h(USDT)  spreadBps  ageDays");
  for (const e of entries) {
    lines.push(
      `${String(e.rank).padStart(4)}  ${e.symbol.padEnd(12)} ${Math.round(e.turnover24h).toLocaleString("en-US").padStart(17)} ${String(e.spreadBps).padStart(9)} ${e.ageDays.toFixed(0).padStart(7)}`,
    );
  }
  return lines.join("\n");
}

interface FlowBacktestInputArgs {
  readonly symbols: string;
  readonly holdTimes: string;
  readonly zMode: string;
  readonly start: string;
  readonly end: string;
}

interface FlowBacktestInput {
  readonly symbols: readonly string[];
  readonly holdTimes: readonly number[];
  readonly start: Date;
  readonly end: Date;
  readonly zMode: FlowBacktestOptions["zMode"];
}

function resolveFlowBacktestInput(
  args: FlowBacktestInputArgs,
): Effect.Effect<FlowBacktestInput, MarketDataRepositoryError> {
  const symbols = args.symbols
    .split(",")
    .map((s) => s.trim())
    .filter((s) => s.length > 0);
  const holdTimes = args.holdTimes
    .split(",")
    .map((s) => Number.parseFloat(s.trim()))
    .filter((n) => Number.isFinite(n) && n > 0);
  if (args.zMode !== "per-symbol" && args.zMode !== "cross-sectional") {
    return Effect.fail(
      new MarketDataRepositoryError(
        `Invalid --z-mode '${args.zMode}': expected per-symbol or cross-sectional`,
      ),
    );
  }
  if (holdTimes.length === 0) {
    return Effect.fail(
      new MarketDataRepositoryError(
        `Invalid --hold-times '${args.holdTimes}': expected comma-separated hours > 0`,
      ),
    );
  }
  const end = args.end.length > 0 ? new Date(args.end) : new Date();
  const start =
    args.start.length > 0
      ? new Date(args.start)
      : new Date(end.getTime() - 180 * 86_400_000);
  // Validated above: only the two literals reach here.
  const zMode: FlowBacktestOptions["zMode"] =
    args.zMode === "cross-sectional" ? "cross-sectional" : "per-symbol";
  return Effect.succeed({ symbols, holdTimes, start, end, zMode });
}

interface FlowBacktestOptionsArgs {
  readonly fee: number;
  readonly spreadBps: number;
  readonly threshold: number;
  readonly stopMult: number;
  readonly conservativeFillRate: number;
  readonly maxBreakevenWinRate: number;
}

function buildFlowBacktestOptions(
  args: FlowBacktestOptionsArgs,
  holdTimes: readonly number[],
  zMode: FlowBacktestOptions["zMode"],
): FlowBacktestOptions {
  return {
    fees: {
      taker: args.fee / 100,
      maker: defaultFlowBacktestOptions.fees.maker,
    },
    spreadBps: args.spreadBps,
    thresholds: {
      ...defaultFlowBacktestOptions.thresholds,
      entry: args.threshold,
    },
    holdTimes,
    trainDays: defaultFlowBacktestOptions.trainDays,
    testDays: defaultFlowBacktestOptions.testDays,
    walkForwardSteps: defaultFlowBacktestOptions.walkForwardSteps,
    zMode,
    stopMultiplier: args.stopMult > 0 ? args.stopMult : null,
    conservativeFillRate: Math.max(0, Math.min(1, args.conservativeFillRate)),
    maxBreakevenWinRate: args.maxBreakevenWinRate,
  };
}

export const flowBacktestCommand = Command.make(
  "flow-backtest",
  {
    symbols: flowSymbolsOption,
    start: flowStartOption,
    end: flowEndOption,
    timeframe: flowTimeframeOption,
    threshold: flowThresholdOption,
    holdTimes: flowHoldTimesOption,
    fee: flowFeeOption,
    spreadBps: flowSpreadBpsOption,
    conservativeFillRate: flowConservativeFillRateOption,
    maxBreakevenWinRate: flowMaxBreakevenWinRateOption,
    zMode: Options.text("z-mode").pipe(
      Options.withDefault("per-symbol"),
      Options.withDescription(
        "z-score normalization: per-symbol (rolling) or cross-sectional (across the universe at each boundary)",
      ),
    ),
    stopMult: Options.float("stop-mult").pipe(
      Options.withDefault(defaultFlowBacktestOptions.stopMultiplier ?? 0),
      Options.withDescription(
        "ATR stop multiplier; 0 disables the ATR stop (pure time/OFI exits)",
      ),
    ),
  },
  (args) =>
    Effect.gen(function* () {
      const sqlite = yield* SqliteClient;
      const { symbols, holdTimes, start, end, zMode } =
        yield* resolveFlowBacktestInput(args);

      const options = buildFlowBacktestOptions(args, holdTimes, zMode);

      const series: FlowSymbolSeries[] = [];
      let totalCandles = 0;
      // Flow tables are created by the flow data layer at runtime; guard so
      // a not-yet-fetched universe degrades to empty series, not a crash.
      const hasOiTable =
        (yield* sqlite.queryOne<{ name: string }>(
          "SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'open_interest_history'",
        )) !== null;
      const hasFundingTable =
        (yield* sqlite.queryOne<{ name: string }>(
          "SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'funding_rates'",
        )) !== null;
      for (const symbol of symbols) {
        const canonical = wireToCanonicalSymbol(symbol);
        // OI/funding rows are stored under multiple canonical forms
        // (e.g. "BTC/USDT" and "BTC/USDT:USDT") depending on which recorder
        // wrote them; the raw wire symbol ("BTCUSDT") never matches. Query
        // all variants so the flow honesty gates see real OI/funding data.
        const symbolVariants = [
          symbol,
          canonical,
          canonical.endsWith(":USDT") ? canonical : `${canonical}:USDT`,
        ];
        const variantPlaceholders = symbolVariants.map(() => "?").join(",");
        const candleRows = yield* sqlite.queryAll<{
          open: number;
          high: number;
          low: number;
          close: number;
          volume: number;
          timestamp: string;
        }>(
          `SELECT c.open_price AS open, c.high_price AS high, c.low_price AS low,
                  c.close_price AS close, c.volume, c.timestamp
           FROM ohlcv_data c
           JOIN trading_pairs tp ON tp.id = c.trading_pair_id
           WHERE tp.symbol IN (${variantPlaceholders}) AND c.timeframe = ?
             AND c.timestamp >= ? AND c.timestamp <= ?
           ORDER BY c.timestamp ASC`,
          [
            ...symbolVariants,
            args.timeframe,
            start.toISOString(),
            end.toISOString(),
          ],
        );
        const oiRows = hasOiTable
          ? yield* sqlite.queryAll<{
              ts: number;
              oi: number;
              oiValue: number | null;
            }>(
              `SELECT ts, oi, oi_value AS oiValue FROM open_interest_history
               WHERE exchange IN ('bybit','bybit-futures') AND symbol IN (${variantPlaceholders}) AND ts BETWEEN ? AND ?
               ORDER BY ts ASC`,
              [...symbolVariants, start.getTime(), end.getTime()],
            )
          : [];
        const fundingRows = hasFundingTable
          ? yield* sqlite.queryAll<{
              fundingRate: number;
              timestamp: string;
            }>(
              `SELECT funding_rate AS fundingRate, timestamp FROM funding_rates
               WHERE exchange IN ('bybit','bybit-futures') AND symbol IN (${variantPlaceholders}) AND timestamp >= ? AND timestamp <= ?
               ORDER BY timestamp ASC`,
              [...symbolVariants, start.toISOString(), end.toISOString()],
            )
          : [];
        totalCandles += candleRows.length;
        series.push({
          symbol,
          exchange: "bybit",
          timeframe: args.timeframe,
          candles: candleRows.map((r) => ({
            open: r.open,
            high: r.high,
            low: r.low,
            close: r.close,
            volume: r.volume,
            timestamp: new Date(r.timestamp),
          })),
          oi: oiRows.map((r) => ({
            ts: r.ts,
            oi: r.oi,
            oiValue: r.oiValue ?? undefined,
          })),
          funding: fundingRows.map((r) => ({
            ts: new Date(r.timestamp).getTime(),
            fundingRate: r.fundingRate,
          })),
        });
      }
      if (totalCandles === 0) {
        return yield* Effect.fail(
          new MarketDataRepositoryError(
            `No candles found for ${symbols.join(",")} at ${args.timeframe} between ${start.toISOString()} and ${end.toISOString()} (exchange=bybit). Run the flow data fetch first.`,
          ),
        );
      }
      const data: FlowBacktestData = { series, options };
      return runFlowBacktest(data);
    }).pipe(
      Effect.tap((report) => Console.log(formatFlowBacktestReport(report))),
      Effect.provide(makeDbLayer(process.env.NEURATRADE_HOME)),
    ),
).pipe(
  Command.withDescription(
    "Run the flow-v1 walk-forward backtest on DB candles/OI/funding",
  ),
);

export const flowUniverseCommand = Command.make(
  "flow-universe",
  {
    limit: flowLimitOption,
    minTurnover: flowMinTurnoverOption,
    dataSource: flowUniverseDataSourceOption,
  },
  (args) =>
    Effect.gen(function* () {
      const sqlite = yield* SqliteClient;
      let volumes: Readonly<Record<string, number>>;
      let instruments: readonly FlowInstrument[];
      if (args.dataSource === "db-mainnet") {
        const rows = yield* sqlite.queryAll<{
          symbol: string;
          turnover24h: number;
          firstTs: string;
        }>(
          `WITH first_seen AS (
             SELECT trading_pair_id, exchange_id, timeframe, MIN(timestamp) AS firstTs
             FROM ohlcv_data
             GROUP BY trading_pair_id, exchange_id, timeframe
           )
           SELECT REPLACE(REPLACE(tp.symbol, '/USDT:USDT', 'USDT'), '/USDT', 'USDT') AS symbol,
                  SUM(c.close_price * c.volume) AS turnover24h,
                  fs.firstTs AS firstTs
           FROM ohlcv_data c
           JOIN trading_pairs tp ON tp.id = c.trading_pair_id
           JOIN exchanges e ON e.id = c.exchange_id
           JOIN first_seen fs ON fs.trading_pair_id = c.trading_pair_id
             AND fs.exchange_id = c.exchange_id
             AND fs.timeframe = c.timeframe
           WHERE e.name IN ('bybit','bybit-futures')
             AND c.timestamp >= datetime('now', '-1 day')
           GROUP BY tp.symbol, fs.firstTs`,
        );
        volumes = Object.fromEntries(
          rows.map((r) => [r.symbol, r.turnover24h]),
        );
        instruments = rows.map((r) => ({
          symbol: r.symbol,
          status: "Trading",
          listedTime: new Date(r.firstTs).getTime(),
        }));
      } else {
        // Mainnet Bybit public market data (no auth needed).
        const baseUrl = "https://api.bybit.com";
        const [tickers, rawInstruments] = yield* Effect.all([
          fetchTickers(baseUrl),
          fetchInstruments(baseUrl),
        ]);
        const tickerBySymbol = new Map(
          tickers.map((ticker) => [ticker.symbol, ticker]),
        );
        volumes = Object.fromEntries(
          tickers.map((ticker) => [ticker.symbol, ticker.turnover24h]),
        );
        instruments = rawInstruments.map((instrument) => {
          const ticker = tickerBySymbol.get(instrument.symbol);
          return ticker === undefined
            ? instrument
            : {
                ...instrument,
                bid1Price: ticker.bid1Price,
                ask1Price: ticker.ask1Price,
              };
        });
      }
      const ranked = selectFlowUniverse(volumes, instruments, undefined, {
        topN: args.limit,
      });
      return args.minTurnover > 0
        ? ranked.filter((e) => e.turnover24h >= args.minTurnover)
        : ranked;
    }).pipe(
      Effect.tap((entries) => Console.log(formatFlowUniverse(entries))),
      Effect.catch((err) =>
        Effect.gen(function* () {
          const msg =
            err instanceof Error
              ? err.message
              : ((err as { reason?: string }).reason ?? String(err));
          yield* Console.error(`flow-universe failed: ${msg}`);
          return [] as readonly FlowUniverseEntry[];
        }),
      ),
      Effect.provide(makeDbLayer(process.env.NEURATRADE_HOME)),
    ),
).pipe(
  Command.withDescription(
    "Rank the liquid USDT-perp universe by 24h turnover (mainnet Bybit)",
  ),
);

export const flowRecordCommand = Command.make(
  "flow-record",
  {
    symbols: Options.text("symbols").pipe(
      Options.withDefault(""),
      Options.withDescription(
        "Comma-separated Bybit USDT-perp symbols (default: flow universe top-100 or fallback set)",
      ),
    ),
    duration: Options.integer("duration").pipe(
      Options.withDefault(0),
      Options.withDescription(
        "Minutes to record before exiting (0 = until Ctrl-C)",
      ),
    ),
  },
  (args) =>
    Effect.gen(function* () {
      const sqlite = yield* SqliteClient;
      const paperRepo = new PaperTradingRepositorySQLite(sqlite.database);
      const flowRepo: FlowRecorderRepository = paperRepo;

      const symbols = yield* Effect.promise(() =>
        resolveFlowSymbols(
          args.symbols.length > 0
            ? args.symbols
                .split(",")
                .map((s) => s.trim())
                .filter((s) => s.length > 0)
            : undefined,
        ),
      );

      yield* Console.log(
        `Recording live flow (trades/liquidations) for ${symbols.length} symbols ` +
          "from Bybit mainnet public WS; Ctrl-C to stop.",
      );

      const record = runFlowRecorder(flowRepo, {
        symbols,
        onFlush: (rows) => {
          for (const row of rows) {
            console.log(
              `[flow] ${new Date(row.ts).toISOString()} ${row.symbol} ` +
                `buy=${row.buyVol} sell=${row.sellVol} trades=${row.trades}`,
            );
          }
        },
        onAggregate: (rows, prices) => {
          for (const row of rows) {
            const last = prices.get(row.symbol);
            console.log(
              `[flow] ${new Date(row.ts).toISOString()} ${row.symbol} ` +
                `buy=${row.buyVol} sell=${row.sellVol} trades=${row.trades}` +
                (last !== undefined ? ` last=${last}` : ""),
            );
          }
        },
        onWarn: (message) => console.warn(`[flow] ${message}`),
      });

      if (args.duration > 0) {
        // Whichever finishes first wins; the loser is interrupted, which runs
        // the recorder's close finalizer (flush + clean WS shutdown).
        yield* Effect.race(
          record,
          Effect.sleep(Duration.minutes(args.duration)),
        );
      } else {
        yield* record;
      }
    }).pipe(Effect.provide(makeDbLayer(process.env.NEURATRADE_HOME))),
).pipe(
  Command.withDescription(
    "Record live Bybit order-flow (trades -> 1m OFI, liquidations) into the DB",
  ),
);

export interface FlowTradeArgs {
  readonly exchange: string;
  readonly symbol: string;
  readonly timeframe: "5m" | "1m";
  readonly capital: number;
  readonly maxPositionSizePct: Option.Option<number>;
  readonly leverage: number;
  readonly interval: number;
  readonly iterations: number;
  readonly threshold: number;
  readonly holdMinutes: number;
  readonly minCapital: Option.Option<number>;
  readonly maxDrawdownPct: Option.Option<number>;
  readonly maxDailyLossPct: Option.Option<number>;
  readonly marginMode: string;
  readonly productType: string;
  readonly live: boolean;
  readonly killSwitch: boolean;
  readonly disengage: boolean;
}

/**
 * Flow Ignition live trade engine — testnet execution validation of the
 * flow-v1 signal. Signals are computed from MAINNET data in the local DB (the
 * flow recorder / fetch), orders go through the exchange adapter (bybit
 * testnet creds with --live). Mirrors paper-trade's risk wiring.
 */
export const flowTradeCommand = Command.make(
  "flow-trade",
  {
    exchange: flowTradeExchangeOption,
    symbol: flowTradeSymbolOption,
    timeframe: flowTimeframeOption,
    capital: capitalOption,
    maxPositionSizePct: maxPositionSizeOption,
    leverage: leverageOption,
    interval: intervalOption,
    iterations: iterationsOption,
    threshold: flowThresholdOption,
    holdMinutes: flowHoldMinutesOption,
    minCapital: minCapitalOption,
    maxDrawdownPct: maxDrawdownOption,
    maxDailyLossPct: maxDailyLossOption,
    marginMode: marginModeOption,
    productType: productTypeOption,
    live: liveOption,
    killSwitch: killSwitchOption,
    disengage: disengageOption,
  },
  (args) =>
    Effect.gen(function* () {
      const sqlite = yield* SqliteClient;
      const db = sqlite.database;

      const repoLayer = MarketDataRepositorySQLiteLive(db);
      const paperRepoLayer = PaperTradingRepositorySQLiteLive(db);

      const riskOverrides: MutablePartialRiskLimits = {};
      if (Option.isSome(args.maxDrawdownPct))
        riskOverrides.maxDrawdownPct = args.maxDrawdownPct.value;
      if (Option.isSome(args.maxDailyLossPct))
        riskOverrides.maxDailyLossPct = args.maxDailyLossPct.value;
      if (Option.isSome(args.minCapital))
        riskOverrides.minCapital = args.minCapital.value;
      const riskGuardLayer = RiskGuardLive(args.live, riskOverrides);
      const killSwitchLayer = KillSwitchSQLiteLive(db);
      const circuitBreakerMaxLoss = Option.getOrElse(
        args.maxDailyLossPct,
        () => 2,
      );
      const circuitBreakerLayer = CircuitBreakerSQLiteLive(
        db,
        circuitBreakerMaxLoss,
      );
      // Signals ALWAYS come from the mainnet data in the DB (the proposal's
      // split: mainnet research, testnet execution); the live gateway is only
      // used by the bybit adapter for reference ticks, which the engine
      // avoids by passing its own reference price.
      const marketDataLayer = Layer.provide(
        MarketDataGatewayRepositoryLive,
        repoLayer,
      );
      const futuresAdapterLayer = (
        args.live
          ? BybitFuturesExchangeAdapterLive.pipe(
              Layer.provide(BybitClientLiveConfig),
              Layer.provide(BybitConfigLive),
            )
          : SimulatedFuturesExchangeAdapterLive()
      ) as Layer.Layer<
        FuturesExchangeAdapterService,
        never,
        MarketDataGatewayService
      >;
      const layers = Layer.mergeAll(
        BunServices.layer,
        PathLive(process.env.NEURATRADE_HOME),
        marketDataLayer,
        repoLayer,
        paperRepoLayer,
        riskGuardLayer,
        killSwitchLayer,
        circuitBreakerLayer,
      );

      if (args.killSwitch) {
        yield* Effect.provide(
          KillSwitch.pipe(
            Effect.flatMap((ks) => ks.engage("CLI --kill-switch")),
          ),
          killSwitchLayer,
        );
      }
      if (args.disengage) {
        yield* Effect.provide(
          KillSwitch.pipe(Effect.flatMap((ks) => ks.disengage())),
          killSwitchLayer,
        );
      }

      return yield* flowTradeProgram(args).pipe(
        Effect.provide(futuresAdapterLayer),
        Effect.provide(layers),
        Effect.tapError((err) =>
          Console.error(
            `flow-trade failed: ${"reason" in err ? err.reason : String(err)}`,
          ),
        ),
      );
    }).pipe(Effect.provide(makeDbLayer(process.env.NEURATRADE_HOME))),
).pipe(
  Command.withDescription(
    "Run the flow-v1 live trade engine (testnet execution, mainnet DB signals)",
  ),
);

function flowTradeProgram(args: FlowTradeArgs) {
  return Effect.gen(function* () {
    const repo = yield* PaperTradingRepository;
    const gateway = yield* MarketDataGateway;
    const adapter = yield* FuturesExchangeAdapter;
    const riskGuard = yield* RiskGuard;
    const killSwitch = yield* KillSwitch;
    const circuitBreaker = yield* CircuitBreaker;
    yield* repo.ensureTables();

    const productType = parseProductType(args.productType);
    const marginMode = parseMarginMode(args.marginMode);
    const opts: FlowTradeOptions = {
      exchange: args.exchange,
      symbol: args.symbol,
      timeframe: args.timeframe,
      capital: args.capital,
      maxPositionSizePct: Option.getOrElse(args.maxPositionSizePct, () => 10),
      leverage: args.leverage,
      productType,
      marginMode,
      threshold: args.threshold,
      holdMinutes: args.holdMinutes,
      isLive: args.live,
    };

    const runIteration = (): Effect.Effect<
      FlowTradeIterationResult,
      FlowTradeError,
      never
    > =>
      iterateFlowTrade(
        repo,
        gateway,
        adapter,
        riskGuard,
        killSwitch,
        circuitBreaker,
        opts,
      ).pipe(
        Effect.catch((err) =>
          Effect.gen(function* () {
            const tag = err._tag;
            // Safety-critical errors must propagate so the loop stops and the
            // process exits for the operator; only transient network/IO
            // errors are safe to skip and retry on the next cadence.
            if (
              tag === "RiskError" ||
              tag === "KillSwitchError" ||
              tag === "CircuitBreakerError"
            ) {
              return yield* Effect.fail(err);
            }
            const current = yield* repo
              .getFlowTradeState(args.exchange, args.symbol)
              .pipe(Effect.orElseSucceed(() => null));
            const reason = err.reason;
            yield* Console.error(
              `flow-trade iteration skipped (network/IO error): ${reason}`,
            );
            return {
              action: "hold" as const,
              side: current?.side ?? null,
              state: current ?? freshFlowTradeState(opts, Date.now()),
              note: `skip: ${reason}`,
            };
          }),
        ),
      );

    let remaining = args.iterations;
    let last: FlowTradeIterationResult | null = null;
    // iterations=0 means run forever.
    while (args.iterations === 0 || remaining !== 0) {
      const result = yield* runIteration();
      last = result;
      yield* Console.log(`[flow-trade] ${result.note}`);

      if (remaining > 0) {
        remaining -= 1;
      }

      // Sleep between iterations: always in infinite mode (0), otherwise only
      // when more iterations remain.
      if (args.iterations === 0 || remaining !== 0) {
        yield* Effect.sleep(`${args.interval} seconds`);
      }
    }
    return last;
  });
}
