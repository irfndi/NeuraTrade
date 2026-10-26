import { Command, Options } from "./kit/kit.ts";
import { Console, Effect, FileSystem, Option, Schedule } from "effect";
import { dirname, resolve } from "node:path";
import { Path } from "../services/path.js";
import { SqliteClient } from "../services/sqlite.js";
import {
  MarketDataRepository,
  MarketDataRepositoryError,
  MarketDataRepositorySQLite,
  MarketDataRepositorySQLiteLive,
} from "../market-data/repository.js";
import { StrategyLibrary } from "../scalping/services.js";
import { MarketDataGatewayLive } from "../market-data/gateways/index.js";
import { MarketDataGateway } from "../market-data/gateway.js";
import {
  BitgetClient,
  BitgetApiError,
  isBitgetUnsupportedInstrumentError,
  toBitgetFuturesSymbol,
  type BitgetClientError,
} from "../services/bitget-client.js";
import { bybitSnapshotCommand } from "./bybit-snapshot.js";
import type { BitgetProductType } from "../services/bitget-client.js";
import {
  PaperTradingRepository,
  PaperTradingRepositorySQLiteLive,
  type WatchlistEntry as DbWatchlistEntry,
} from "../paper-trading/repository.js";
import {
  buildStrategyProfileFromArgs,
  resolveBacktestArgs,
  saveStrategyProfile,
  type ResolvedBacktestArgs,
} from "../scalping/strategy-profile.js";
import {
  evaluateReadiness,
  formatReadinessReport,
} from "../scalping/readiness.js";
import { READINESS_COHORT_CANDIDATES } from "../scalping/grid-candidate.js";
import {
  runGridUniverseScan,
  runMarketUniverseScan,
  DEFAULT_GRID_UNIVERSE_SEARCH_SPACE,
  LADDER_GATE_TAIL,
  DEFAULT_PER_SYMBOL_FILL_CAP,
  accountScaledTargetFillsPerDay,
  accountSymbolCap,
  selectUniversePortfolio,
  type GridUniverseEntry,
  type GridUniverseOptions,
} from "../scalping/grid-universe.js";
import {
  buildBacktestArgsFromTemplate,
  buildComposerConfigFromTemplate,
  type StrategyTemplateName,
} from "../scalping/strategy-library.js";
import { runWalkForward } from "../scalping/walk-forward.js";
import { makeDemoReadinessCommand } from "./demo-readiness.js";
import { makeParityReplayCommand } from "./parity-replay.js";
import { makeTimesFmForecastCommand } from "./timesfm.js";
import { tradeCommand } from "./trade.js";
import {
  maxHoldBarsOption,
  stopRatioOption,
  strategyOption,
  profileNameOption,
  trainWindowOption,
  testWindowOption,
  wfMinTradesOption,
  minTradesPerMonthOption,
  gridUniverseExchangeOption,
  gridUniverseTimeframeOption,
  gridUniverseMinCandlesOption,
  gridUniverseTrainWindowOption,
  gridUniverseTestWindowOption,
  gridUniverseMinProfitableWindowsOption,
  gridUniverseMinAggregateReturnOption,
  gridUniverseFeeOption,
  gridUniverseSlippageOption,
  gridUniverseTrendFilterOption,
  gridUniverseMarketOption,
  gridUniverseOutputOption,
  gridUniverseWatchOption,
  gridUniverseIntervalOption,
  gridUniverseMinFillFrequencyOption,
  gridUniverseTargetFillsPerDayOption,
  gridUniverseAccountCapitalOption,
  gridUniverseTierOption,
  gridUniverseDataSourceOption,
  gridUniverseEngineOption,
  gridUniverseRungsOption,
  watchlistListExchangeOption,
  watchlistListTimeframeOption,
} from "./scalp-options/index.js";
// Layer assembly + enum parsers live in ./scalp/shared.ts now; the flow
// module shares them too.
import { makeDbLayer, makeLayer } from "./scalp/shared.js";

// Backtest / optimize / scan / paper-soak clusters now live in ./scalp/*.ts;
// see the module-cycle note in each. Re-exports keep the command wiring +
// test imports unchanged.
export * from "./scalp/backtest.js";
export * from "./scalp/optimize.js";
export * from "./scalp/scan.js";
import {
  backtestCommand,
  backtestOptions,
  backtestProgram,
  loadProfileIfNeeded,
} from "./scalp/backtest.js";
import { optimizeCommand } from "./scalp/optimize.js";
import { scanCommand } from "./scalp/scan.js";
// Paper-trade + soak cluster lives in ./scalp/paper.ts now; see the module
// cycle note there. Re-exports keep the command wiring + test imports
// unchanged.
import {
  paperTradeCommand,
  resolveFuturesMarketExchange,
  soakCommand,
} from "./scalp/paper.js";
export * from "./scalp/paper.js";

const profileSaveCommand = Command.make(
  "save",
  { name: profileNameOption, ...backtestOptions },
  (args) =>
    Effect.gen(function* () {
      const path = yield* Path;
      const { name, profile: _profile, ...rest } = args;
      const profile = buildStrategyProfileFromArgs(
        name,
        rest as ResolvedBacktestArgs,
      );
      yield* saveStrategyProfile(path.homeDir, name, profile);
      yield* Console.log(
        `Profile saved to ${resolve(path.homeDir, "profiles", `${name}.json`)}`,
      );
    }).pipe(Effect.provide(makeLayer(process.env.NEURATRADE_HOME))),
).pipe(
  Command.withDescription(
    "Save current backtest options as a strategy profile",
  ),
);

const profileCommand = Command.make("profile", {}, () =>
  Console.log(
    "Profile commands. Use 'profile save --name <name> [backtest options]'.",
  ),
).pipe(
  Command.withDescription("Strategy profile management"),
  Command.withSubcommands([profileSaveCommand]),
);

// Select / validate / library helpers live in ./scalp/select.ts; behavior
// unchanged. Re-exports keep every historical ./scalp.js import working.
// (Types are re-exported verbatim; values re-bound one level deep.)
export {
  type SelectArgs,
  type SelectResult,
  type SelectWatchlistEntry,
  type ValidationRow,
  buildCandidate,
  generateCandidates,
  objectiveValue,
  selectWinner,
  generateWalkForwardWindows,
  combineWalkForwardResults,
  runSelectBacktest,
  selectBestForSymbol,
  extractExplicitOverrides,
} from "./scalp/select.js";
export {
  loadSelectWatchlist,
  buildValidateBacktestArgs,
  isLiveReady,
  validateWatchlist,
  buildPaperTradeComposerConfig,
  buildStrategyProfileFromOptimizeResult,
} from "./scalp/select.js";
import {
  objectiveValue,
  generateCandidates,
  generateWalkForwardWindows,
  isLiveReady,
  type SelectArgs,
  type WalkForwardWindow,
  cliDefaultArgs,
  runSelectBacktest,
  selectBestForSymbol,
} from "./scalp/select.js";

const libraryListCommand = Command.make("list", {}, () =>
  Effect.gen(function* () {
    const library = yield* StrategyLibrary;
    const strategies = yield* library.listStrategies();
    for (const s of strategies) {
      yield* Console.log(`${s.name}: ${s.description}`);
    }
    return strategies;
  }).pipe(Effect.provide(makeLayer(process.env.NEURATRADE_HOME))),
).pipe(Command.withDescription("List available strategy templates"));

const libraryStrategyCommand = Command.make(
  "strategy",
  {
    strategy: strategyOption,
    ...backtestOptions,
  },
  (args) =>
    Effect.gen(function* () {
      const template = args.strategy;
      const baseArgs = args as ResolvedBacktestArgs;
      const library = yield* StrategyLibrary;
      const merged = yield* library.buildBacktestArgsFromTemplate(
        template,
        baseArgs,
      );
      const config = yield* library.buildComposerConfigFromTemplate(template);
      yield* Console.log(`Strategy: ${template}`);
      yield* Console.log(JSON.stringify(merged, null, 2));
      return config;
    }).pipe(Effect.provide(makeLayer(process.env.NEURATRADE_HOME))),
).pipe(Command.withDescription("Show strategy template details"));

export const libraryCommand = Command.make(
  "library",
  {
    list: Options.boolean("list").pipe(Options.withDefault(false)),
    strategy: Options.optional(strategyOption),
  },
  (args) =>
    Effect.gen(function* () {
      const library = yield* StrategyLibrary;
      if (args.list || Option.isNone(args.strategy)) {
        const strategies = yield* library.listStrategies();
        for (const s of strategies) {
          yield* Console.log(`${s.name}: ${s.description}`);
        }
        return strategies;
      }
      const strategy = Option.getOrElse(
        args.strategy,
        () => "meanReversion" as StrategyTemplateName,
      );
      const baseArgs = cliDefaultArgs();
      const merged = yield* library.buildBacktestArgsFromTemplate(
        strategy,
        baseArgs,
      );
      const config = yield* library.buildComposerConfigFromTemplate(strategy);
      yield* Console.log(`Strategy: ${strategy}`);
      yield* Console.log(JSON.stringify(merged, null, 2));
      return config;
    }).pipe(Effect.provide(makeLayer(process.env.NEURATRADE_HOME))),
).pipe(
  Command.withDescription("Strategy template library"),
  Command.withSubcommands([libraryListCommand, libraryStrategyCommand]),
);

export const walkForwardCommand = Command.make(
  "walk-forward",
  {
    ...backtestOptions,
    trainWindow: trainWindowOption,
    testWindow: testWindowOption,
    minTrades: wfMinTradesOption,
  },
  (args) =>
    Effect.gen(function* () {
      const sqlite = yield* SqliteClient;
      const repoLayer = MarketDataRepositorySQLiteLive(sqlite.database);

      const resolvedArgs = args as Omit<ResolvedBacktestArgs, "minTrades">;
      const selectArgs: SelectArgs = {
        ...resolvedArgs,
        universe: "",
        top: 0,
        minRobustness: 0,
        minReturnPct: -100,
        maxDrawdownPct: 100,
        minTrades: args.minTrades,
        selectLookbackCandles: 0,
        selectBy: "return",
      };

      const result = yield* Effect.gen(function* () {
        const repo = yield* MarketDataRepository;
        const candles = yield* repo.getCandles({
          exchange: args.exchange,
          symbol: args.symbol,
          timeframe: args.timeframe,
        });
        if (candles.length === 0) {
          return yield* Effect.fail(
            new MarketDataRepositoryError(
              `No candles found for ${args.exchange}:${args.symbol}:${args.timeframe}`,
            ),
          );
        }
        return runWalkForward({
          symbol: args.symbol,
          exchange: args.exchange,
          candles,
          trainWindow: args.trainWindow,
          testWindow: args.testWindow,
          initialCapital: args.capital,
          args: selectArgs,
          selectBestForSymbol,
          runSelectBacktest,
        });
      }).pipe(Effect.provide(repoLayer));

      return result;
    }).pipe(Effect.provide(makeDbLayer(process.env.NEURATRADE_HOME))),
).pipe(Command.withDescription("Run walk-forward optimization"));

export const readinessCommand = Command.make(
  "readiness",
  { ...backtestOptions, minTradesPerMonth: minTradesPerMonthOption },
  (args) =>
    Effect.gen(function* () {
      const path = yield* Path;
      const sqlite = yield* SqliteClient;
      const repoLayer = MarketDataRepositorySQLiteLive(sqlite.database);
      const repo = new MarketDataRepositorySQLite(sqlite.database);

      const profile = yield* loadProfileIfNeeded(path.homeDir, args.profile);
      const programArgs = Option.isSome(profile)
        ? resolveBacktestArgs(
            profile.value,
            args.symbol,
            args.exchange,
            args.timeframe,
            args,
          )
        : args;

      // Readiness is a gate, not a backtest: OOS + Monte Carlo are mandatory.
      const gatedArgs = {
        ...programArgs,
        oosPct: programArgs.oosPct > 0 ? programArgs.oosPct : 20,
        mcIterations:
          programArgs.mcIterations > 0 ? programArgs.mcIterations : 200,
      };

      const result = yield* backtestProgram(gatedArgs).pipe(
        Effect.provide(repoLayer),
      );

      const candles = yield* repo.getCandles({
        exchange: args.exchange,
        symbol: args.symbol,
        timeframe: args.timeframe,
      });
      if (candles.length < 2) {
        return yield* Effect.fail(
          new Error(
            `Not enough candles for ${args.exchange}:${args.symbol}:${args.timeframe} to evaluate readiness.`,
          ),
        );
      }
      const first = candles[0].timestamp.getTime();
      const last = candles[candles.length - 1].timestamp.getTime();
      const fullMonths = Math.max(
        (last - first) / (30.44 * 24 * 60 * 60 * 1000),
        1e-9,
      );
      const inSampleMonths =
        gatedArgs.oosPct > 0
          ? fullMonths * (1 - gatedArgs.oosPct / 100)
          : fullMonths;

      const minTradesPerMonth = Option.isSome(args.minTradesPerMonth)
        ? args.minTradesPerMonth.value
        : args.timeframe === "5m"
          ? 20
          : 10;

      const report = evaluateReadiness({
        result,
        timeframe: args.timeframe,
        inSampleMonths,
        thresholds: { minTradesPerMonth },
      });
      yield* Console.log(formatReadinessReport(report));
      if (!report.ready) {
        return yield* Effect.fail(new Error("readiness gates failed"));
      }
      return report;
    }).pipe(Effect.provide(makeDbLayer(process.env.NEURATRADE_HOME))),
).pipe(
  Command.withDescription(
    "Evaluate scalping readiness gates (G1-G4) for a config; exits non-zero when any gate fails",
  ),
);

/**
 * True when a 40034 body names the probed symbol itself, e.g.
 * `Parameter BLESSUSDT does not exist`. Bitget reports the instrument as
 * the parameter value rather than a parameter *name*, so
 * `isBitgetUnsupportedInstrumentError` (which only accepts literal
 * symbol/contract/instrument parameter names) misses it. Still fails closed:
 * a message naming any other parameter (marginCoin, clientType, ...) or an
 * unrelated token is not treated as an unsupported instrument.
 */
export function probeNamesProbedSymbol(
  error: BitgetApiError,
  probedSymbol: string,
  probedProductType: BitgetProductType = "USDT-FUTURES",
): boolean {
  if (error.code !== "40034") return false;
  // Bitget uses both "Parameter X does not exist" and "Parameter X not exist".
  const token = /\bparameter\s+(\S+)\s+(?:does not|not)\s+exist\b/i.exec(
    error.body,
  )?.[1];
  if (token === undefined) return false;
  const bitgetSymbol = toBitgetFuturesSymbol(
    probedSymbol,
    probedProductType,
  ).symbol;
  return token.toUpperCase() === bitgetSymbol.toUpperCase();
}

/**
 * `scalp grid-universe scan` — per-symbol grid walk-forward universe scanner.
 *
 * Walks every stored symbol with enough candles, finds the best grid
 * parameters in-sample, and reports which symbols survive a profitability and
 * robustness gate. With --output it writes a whitelist JSON consumable by
 * `scalp paper-trade --strategy-type grid --watchlist`.
 */
type ProbeVerdict =
  | { readonly kind: "untradeable"; readonly evidence: string }
  | { readonly kind: "transient"; readonly reason: string };

/** Classify a failed leverage probe: unsupported instrument vs transient. */
export function classifyProbeOutcome(
  failure: BitgetClientError,
  symbol: string,
): ProbeVerdict {
  if (
    failure instanceof BitgetApiError &&
    (isBitgetUnsupportedInstrumentError(failure) ||
      probeNamesProbedSymbol(failure, symbol, "USDT-FUTURES"))
  ) {
    return {
      kind: "untradeable",
      evidence: `Bitget ${failure.code ?? "?"}: ${failure.body.slice(0, 140)}`,
    };
  }
  const reason =
    failure instanceof BitgetApiError
      ? `BitgetApiError code=${failure.code ?? "-"}: ${failure.body.slice(0, 140)}`
      : failure instanceof Error
        ? failure.message
        : String(failure);
  return { kind: "transient", reason };
}

export function formatUniverseScanEntry(e: GridUniverseEntry): string {
  const mark = e.gatedDropped ? " ✘" : e.passed ? " ✔" : "";
  const reason = e.rejectionReason ? ` [${e.rejectionReason}]` : "";
  const gateReasons = e.gateFailureReasons?.length
    ? ` gate=${e.gateFailureReasons.join("|")}`
    : "";
  return (
    `${e.symbol.padEnd(13)} ${String(e.candles).padStart(7)}  ` +
    `${e.bestParams.gridStepPct.toFixed(2).padStart(5)}  ` +
    `${String(e.bestParams.gridMaxGrids).padStart(5)}  ` +
    `${String(e.bestParams.gridPauseAfterLossBars).padStart(5)}  ` +
    `${e.walkForward.profitableWindowsPct.toFixed(0).padStart(6)}%  ` +
    `${e.walkForward.aggregateReturnPct.toFixed(2).padStart(9)}%${mark}${reason}${gateReasons}`
  );
}

/** Parse --rungs into rung counts; absent means the default 1-3 rung sweep. */
export function parseScanRungs(
  rungs: Option.Option<string>,
): readonly number[] {
  if (Option.isNone(rungs)) return [1, 2, 3];
  return rungs.value
    .split(",")
    .map((token) => Number(token.trim()))
    .filter((n) => Number.isInteger(n) && n >= 1);
}

function logScanNotices(args: {
  readonly engine: string;
  readonly tier: string;
  readonly minFillFrequencyPct: number;
}) {
  return Effect.gen(function* () {
    if (args.engine === "ladder" && args.tier === "readiness") {
      // Ladder readiness is gated by the ladder evidence validator inside
      // ladderGateScoredEligibility (data quality, historical windows,
      // fixed-OOS, block-bootstrap confidence, pooled adverse-stress LB);
      // a survivor that clears it is admissible to the readiness board.
      yield* Console.log(
        `🧪 Ladder readiness tier: gate-scoring through the ladder evidence validator (this is slower than fast tier — every combo runs the 5-seed adverse-stress protocol)`,
      );
    }
    if (args.minFillFrequencyPct <= 0) {
      yield* Console.warn(
        `⚠️ --min-fill-frequency-pct is 0 — the fill gate is DISABLED; survivors whose grid step is too wide to fill live will not be rejected`,
      );
    }
  });
}

export const gridUniverseScanCommand = Command.make(
  "grid-universe-scan",
  {
    exchange: gridUniverseExchangeOption,
    timeframe: gridUniverseTimeframeOption,
    minCandles: gridUniverseMinCandlesOption,
    trainWindow: gridUniverseTrainWindowOption,
    testWindow: gridUniverseTestWindowOption,
    minProfitableWindowsPct: gridUniverseMinProfitableWindowsOption,
    minAggregateReturnPct: gridUniverseMinAggregateReturnOption,
    fee: gridUniverseFeeOption,
    slippageBps: gridUniverseSlippageOption,
    trendFilterPeriod: gridUniverseTrendFilterOption,
    output: gridUniverseOutputOption,
    watch: gridUniverseWatchOption,
    interval: gridUniverseIntervalOption,
    minFillFrequencyPct: gridUniverseMinFillFrequencyOption,
    targetFillsPerDay: gridUniverseTargetFillsPerDayOption,
    accountCapital: gridUniverseAccountCapitalOption,
    tier: gridUniverseTierOption,
    market: gridUniverseMarketOption,
    dataSource: gridUniverseDataSourceOption,
    engine: gridUniverseEngineOption,
    rungs: gridUniverseRungsOption,
    ladderStopRatio: stopRatioOption,
    ladderMaxHoldBars: maxHoldBarsOption,
  },
  (args) =>
    Effect.gen(function* () {
      const scanMode = ():
        | {
            readonly ok: true;
            readonly tier: "readiness" | "fast";
            readonly engine: "grid" | "ladder";
            readonly dataSource: "gateway" | "db-mainnet";
          }
        | { readonly ok: false; readonly error: string } => {
        if (args.tier !== "readiness" && args.tier !== "fast") {
          return {
            ok: false,
            error: `invalid --tier '${args.tier}': expected 'readiness' or 'fast'`,
          };
        }
        if (args.dataSource !== "gateway" && args.dataSource !== "db-mainnet") {
          return {
            ok: false,
            error: `invalid --data-source '${args.dataSource}': expected 'gateway' or 'db-mainnet'`,
          };
        }
        if (args.dataSource === "db-mainnet" && !args.market) {
          // The DB-sourced (non-market) scan reads candles at the scan
          // timeframe — for bybit-futures those 15m rows are TESTNET-native.
          // db-mainnet must go through the market scan so candles come from
          // the resampled 5m mainnet cache instead.
          return {
            ok: false,
            error: `--data-source db-mainnet requires --market (db-mainnet candles come from the 5m mainnet DB cache via the market scan)`,
          };
        }
        if (args.engine !== "grid" && args.engine !== "ladder") {
          return {
            ok: false,
            error: `invalid --engine '${args.engine}': expected 'grid' or 'ladder'`,
          };
        }
        return {
          ok: true,
          tier: args.tier,
          engine: args.engine,
          dataSource: args.dataSource,
        };
      };
      const mode = scanMode();
      if (!mode.ok) {
        return yield* Effect.fail(new Error(mode.error));
      }
      if (args.engine === "ladder" && args.minCandles < LADDER_GATE_TAIL) {
        return yield* Effect.fail(
          new Error(
            `ladder scans require at least ${LADDER_GATE_TAIL} candles (~21 days at 15m); received ${args.minCandles}. Increase --min-candles before creating a paper whitelist`,
          ),
        );
      }
      const rungs = parseScanRungs(args.rungs);
      if (rungs.length === 0) {
        return yield* Effect.fail(
          new Error(
            `invalid --rungs '${args.rungs}': expected comma-separated integers >= 1`,
          ),
        );
      }
      yield* logScanNotices(args);
      const path = yield* Path;
      const sqlite = yield* SqliteClient;
      const repoLayer = MarketDataRepositorySQLiteLive(sqlite.database);
      const paperRepoLayer = PaperTradingRepositorySQLiteLive(sqlite.database);

      const outputPath = Option.isSome(args.output)
        ? resolve(path.homeDir, "data", args.output.value)
        : undefined;

      // One resolved futures-market key for delete-write-read consistency:
      // scan with --exchange binance then paper-trade --futures must both
      // resolve to bitget-futures, or the watchlist lookups disagree.
      const marketExchange = resolveFuturesMarketExchange(args.exchange, true);
      const options: GridUniverseOptions = {
        exchange: args.exchange,
        timeframe: args.timeframe,
        initialCapital: 10000,
        minCandles: args.minCandles,
        trainWindow: args.trainWindow,
        testWindow: args.testWindow,
        minProfitableWindowsPct: args.minProfitableWindowsPct,
        minAggregateReturnPct: args.minAggregateReturnPct,
        minFillFrequencyPct: args.minFillFrequencyPct,
        feePct: args.fee,
        slippageBps: args.slippageBps,
        trendFilterPeriod: args.trendFilterPeriod,
        searchSpace: { ...DEFAULT_GRID_UNIVERSE_SEARCH_SPACE, rungs },
        tier: mode.tier,
        engine: mode.engine,
        // db-mainnet evaluates on mainnet-fidelity candles; its fills are
        // modeled conservatively by default (a wick touch is not a fill).
        dataSource: mode.dataSource,
        fillModel: mode.dataSource === "db-mainnet" ? "conservative" : "wick",
        ladderStopRatio: args.ladderStopRatio,
        ladderMaxHoldBars: args.ladderMaxHoldBars,
      };

      const targetFillsPerDay = Option.isSome(args.targetFillsPerDay)
        ? args.targetFillsPerDay.value
        : accountScaledTargetFillsPerDay(args.accountCapital);

      const persistSurvivors = (result: {
        readonly entries: readonly GridUniverseEntry[];
        readonly survivors: readonly GridUniverseEntry[];
        readonly gateDropped?: number;
      }) =>
        Effect.gen(function* () {
          const paperRepo = yield* PaperTradingRepository;
          yield* paperRepo.ensureTables();
          // Readiness cohort symbols are owned by their candidate soaks;
          // the universe soak must never trade them (its fills/positions
          // carry a different manifest and trip the account kill switch).
          const cohortSymbols = new Set<string>(
            READINESS_COHORT_CANDIDATES.map((candidate) => candidate.symbol),
          );
          const survivors = result.survivors.filter(
            (entry) => !cohortSymbols.has(entry.symbol),
          );

          // Frequency-targeted selection (runs AFTER the tradeability probe
          // upstream, so only tradeable symbols are considered): rank by
          // edge/trade and take the top-K whose capped fills/day reach the
          // target, bounded by how many positions the account capital fits
          // (accountSymbolCap: max(1, floor(A × 0.5 / 10)) symbols; tiny
          // accounts get concentrated mode). Only the selected entries
          // reach the watchlist.
          const symbolCap = accountSymbolCap(args.accountCapital);
          if (symbolCap === 1) {
            yield* Console.log(
              `⚠️ Tiny account ($${args.accountCapital}): concentrated mode — portfolio capped at 1 symbol`,
            );
          }
          const selected = selectUniversePortfolio(
            survivors,
            targetFillsPerDay,
            DEFAULT_PER_SYMBOL_FILL_CAP,
            args.accountCapital,
          );
          // Stage-4 funnel summary: walk-forward survivors → gate-eligible →
          // selected. Entries keep walk-forward failures AND gate-dropped
          // survivors (flagged), so eligibility = passed && !gatedDropped.
          const eligibleCount = result.entries.filter(
            (e) => e.passed && !e.gatedDropped,
          ).length;
          const gateDroppedCount = result.gateDropped ?? 0;
          yield* Console.log(
            `🎯 Gate-scored funnel: ${eligibleCount + gateDroppedCount} walk-forward survivors → ${eligibleCount} gate-eligible (${gateDroppedCount} dropped by stage-4 gates) → ${selected.length} selected`,
          );
          const projectedFills = selected.reduce(
            (sum, e) =>
              sum + Math.min(e.fillsPerDay ?? 0, DEFAULT_PER_SYMBOL_FILL_CAP),
            0,
          );
          yield* Console.log(
            `🎯 Portfolio selection: ${selected.length}/${survivors.length} survivors selected, ~${Math.round(projectedFills)} fills/day projected (target ${targetFillsPerDay})`,
          );

          if (args.engine === "ladder") {
            // Ladder survivors are NOT written to the DB watchlist (that feed
            // gates the single-position readiness cohort, and the readiness
            // tier fails closed for ladder). The demo soak trades these rows
            // directly from the ladder whitelist file, which carries the rungs,
            // via the incremental ladder paper engine (--watchlist
            // grid-whitelist-ladder.json).
            yield* Console.log(
              `💾 Ladder scan: ${selected.length} survivors reported to the whitelist file (NOT the DB watchlist — readiness tier fails closed for ladder); demo soak can trade them via --watchlist grid-whitelist-ladder.json`,
            );
          } else {
            const entries: DbWatchlistEntry[] = selected.map((e) => ({
              // Persist under the resolved futures-market key so scan-write
              // and paper-trade read always agree, even when the scan was run
              // with a raw exchange name that resolves differently (e.g.
              // --exchange binance + futures => bitget-futures).
              exchange: marketExchange,
              symbol: e.symbol,
              timeframe: args.timeframe,
              returnPct: e.walkForward.aggregateReturnPct,
              profitableWindowsPct: e.walkForward.profitableWindowsPct,
              aggregateReturnPct: e.walkForward.aggregateReturnPct,
              gridStepPct: e.bestParams.gridStepPct,
              gridMaxGrids: e.bestParams.gridMaxGrids,
              gridPauseAfterLossBars: e.bestParams.gridPauseAfterLossBars,
              // Stage-4 gate-scored validation fills these; walk-forward-only
              // (DB-sourced) scans default to target 1 / no chop gate.
              targetRatio: e.validatedTargetRatio ?? 1,
              chopGateAdx: e.validatedChopGateAdx ?? 0,
              oosTrades: e.oosTrades ?? 0,
              fillsPerDay: e.fillsPerDay ?? 0,
              edgePerTradePct: e.edgePerTradePct ?? 0,
              volatility: e.volatility ?? 0,
              // ponytail: equal weight per selected symbol — simple, spreads
              // risk; upgrade to edge-proportional (edgePerTradePct / sum)
              // once the soak measures per-symbol edge live.
              allocatedWeight: selected.length > 0 ? 1 / selected.length : 0,
              updatedAt: new Date(),
            }));
            if (entries.length === 0) {
              yield* Console.log(
                `💾 No survivors selected this cycle — keeping existing watchlist unchanged`,
              );
            } else {
              yield* paperRepo.replaceWatchlist(
                marketExchange,
                args.timeframe,
                entries,
              );
              yield* Console.log(
                `💾 Watchlist replaced: ${entries.length} survivors now in DB (${marketExchange}:${args.timeframe}); prior symbols in this scope were removed`,
              );
            }
          }

          // Only ever write the whitelist file after a successful scan with
          // selected rows: a failed/empty scan must not truncate the file the
          // demo soak consumes, and unselected eligible rows must not bypass
          // the account-sized portfolio cap.
          if (outputPath && selected.length > 0) {
            const watchlistJson = selected.map((e) => ({
              symbol: e.symbol,
              exchange: args.exchange,
              returnPct: e.walkForward.aggregateReturnPct,
              gridParams: {
                gridStepPct: e.bestParams.gridStepPct,
                gridMaxGrids: e.bestParams.gridMaxGrids,
                gridPauseAfterLossBars: e.bestParams.gridPauseAfterLossBars,
                rungs: e.bestParams.rungs,
                // Carry the stage-4 gate-validated dials so the ladder soak
                // trades the geometry that cleared walk-forward + time-split
                // evidence — NOT the CLI default targetRatio=1 (which caps
                // wins at one grid step and inverts the ladder's R:R into
                // an ~82%-win-rate-to-break-even bleed on tight steps).
                targetRatio: e.validatedTargetRatio ?? 1,
                chopGateAdx: e.validatedChopGateAdx ?? 0,
              },
            }));
            const fsys = yield* FileSystem.FileSystem;
            yield* fsys.makeDirectory(dirname(outputPath), {
              recursive: true,
            });
            yield* Effect.tryPromise({
              try: () =>
                Bun.write(outputPath, JSON.stringify(watchlistJson, null, 2)),
              catch: (err) =>
                new MarketDataRepositoryError(
                  `Failed to write grid whitelist: ${err instanceof Error ? err.message : String(err)}`,
                ),
            });
            yield* Console.log(`Whitelist written to ${outputPath}`);
          }
        });

      const filterTradeableSurvivors = (
        survivors: readonly GridUniverseEntry[],
      ) =>
        Effect.gen(function* () {
          if (survivors.length === 0) return [...survivors];
          if (args.exchange.toLowerCase() !== "bitget-futures") {
            yield* Console.log(
              `🎯 Probe skipped: ${args.exchange} universe is sourced from its demo/tradeable instrument list`,
            );
            return [...survivors];
          }

          const client = yield* BitgetClient;
          const tradeable: GridUniverseEntry[] = [];
          for (const entry of survivors) {
            const probe = yield* client
              .getLeverage({
                symbol: entry.symbol,
                productType: "USDT-FUTURES",
              })
              .pipe(Effect.result);
            if (probe._tag === "Success") {
              tradeable.push(entry);
              continue;
            }
            const verdict = classifyProbeOutcome(probe.failure, entry.symbol);
            if (verdict.kind === "untradeable") {
              yield* Console.log(
                `🎯 Dropped ${entry.symbol}: not tradeable on ${args.exchange} demo (${verdict.evidence})`,
              );
            } else {
              yield* Console.log(
                `⚠️ Keep ${entry.symbol}: probe failed transiently (${verdict.reason})`,
              );
              tradeable.push(entry);
            }
          }
          if (tradeable.length < survivors.length) {
            yield* Console.log(
              `🎯 Filtered ${survivors.length - tradeable.length} survivors not tradeable in demo`,
            );
          }
          return tradeable;
        });

      const runScan = () =>
        Effect.gen(function* () {
          const gateway = yield* MarketDataGateway;
          // db-mainnet: the universe already came from the mainnet 5m cache —
          // fetching the testnet contract list to filter survivors would
          // re-introduce testnet ground truth. Empty set = no filter.
          const futuresSymbols =
            args.dataSource === "db-mainnet"
              ? []
              : yield* gateway.fetchSymbols(args.exchange).pipe(
                  Effect.catch((err) =>
                    Effect.gen(function* () {
                      const reason =
                        err instanceof Error ? err.message : String(err);
                      yield* Console.warn(
                        `⚠️ futures symbol fetch failed (${reason}) — skipping the futures filter this cycle`,
                      );
                      return [] as readonly string[];
                    }),
                  ),
                );
          const futuresSet = new Set(futuresSymbols);
          const canonicalSymbol = (symbol: string) =>
            symbol.includes(":")
              ? symbol.slice(0, symbol.lastIndexOf(":"))
              : symbol;
          const isFuturesSymbol = (symbol: string) =>
            futuresSet.has(symbol) || futuresSet.has(canonicalSymbol(symbol));

          // Scan errors are NOT swallowed here: they propagate so a failed
          // scan never persists (DB watchlist kept, whitelist file untouched)
          // and the one-shot command exits non-zero.
          const rawResult = args.market
            ? yield* runMarketUniverseScan(options).pipe(
                Effect.provide(repoLayer),
              )
            : yield* runGridUniverseScan(options).pipe(
                Effect.provide(repoLayer),
              );

          const symbolFilteredSurvivors =
            rawResult.survivors.length > 0 && futuresSet.size > 0
              ? rawResult.survivors.filter((e) => isFuturesSymbol(e.symbol))
              : rawResult.survivors;
          const survivors = yield* filterTradeableSurvivors(
            symbolFilteredSurvivors,
          );
          const result = {
            entries: rawResult.entries,
            survivors,
            gateDropped: rawResult.gateDropped ?? 0,
          };

          yield* Console.log(
            `\n🎯 Grid universe scan: ${result.entries.length} symbols, ${result.survivors.length} survivors (tier=${args.tier})`,
          );
          yield* Console.log(
            "Symbol        Candles  Step%  Grids  Pause  ProfitWin%  Aggregate%",
          );
          yield* Console.log(
            "------------------------------------------------------------------",
          );
          for (const e of result.entries) {
            yield* Console.log(formatUniverseScanEntry(e));
          }

          yield* persistSurvivors(result).pipe(Effect.provide(paperRepoLayer));
          return result.survivors;
        });

      const runScanWithLayers = () =>
        runScan().pipe(Effect.provide(MarketDataGatewayLive));

      if (args.watch) {
        // The watch loop must survive transient scan failures: log the cycle
        // error and continue; the DB watchlist and whitelist file are left
        // untouched on failure.
        const watchCycle = runScanWithLayers().pipe(
          Effect.catch((err) =>
            Effect.gen(function* () {
              yield* Console.error(
                `grid-universe scan cycle failed: ${
                  err instanceof Error ? err.message : String(err)
                }`,
              );
              return [] as readonly GridUniverseEntry[];
            }),
          ),
        );
        yield* Console.log(
          `👁 Watching universe ${args.exchange}:${args.timeframe} (tier=${args.tier}), re-scan every ${args.interval}s...`,
        );
        // Repeat until interrupted: Effect.repeat + Schedule.spaced runs the
        // scan, spaces iterations by the interval, and cancels cleanly on
        // SIGTERM/SIGINT (BunRuntime.runMain interrupts the fiber at the
        // schedule boundary).
        yield* Effect.repeat(watchCycle, {
          schedule: Schedule.spaced(`${args.interval} seconds`),
        });
      }

      // One-shot mode: failures propagate (non-zero exit, nothing persisted).
      return yield* runScanWithLayers();
    }).pipe(Effect.provide(makeDbLayer(process.env.NEURATRADE_HOME))),
).pipe(
  Command.withDescription(
    "Per-symbol grid walk-forward scan; finds profitable grid candidates across the stored universe",
  ),
);

export const watchlistListCommand = Command.make(
  "list",
  {
    exchange: watchlistListExchangeOption,
    timeframe: watchlistListTimeframeOption,
  },
  (args) =>
    Effect.gen(function* () {
      const sqlite = yield* SqliteClient;
      const paperRepoLayer = PaperTradingRepositorySQLiteLive(sqlite.database);
      const entries = yield* PaperTradingRepository.pipe(
        Effect.flatMap((repo) =>
          Effect.gen(function* () {
            yield* repo.ensureTables();
            return yield* repo.listWatchlist(args.exchange, args.timeframe);
          }),
        ),
        Effect.provide(paperRepoLayer),
      );
      yield* Console.log(
        `\n📋 Watchlist ${args.exchange}:${args.timeframe} (${entries.length} symbols)`,
      );
      yield* Console.log(
        "Symbol        Return%   ProfitWin%  Step%  Grids  Pause  Updated",
      );
      yield* Console.log(
        "------------------------------------------------------------------",
      );
      for (const e of entries) {
        yield* Console.log(
          `${e.symbol.padEnd(13)} ${e.aggregateReturnPct.toFixed(2).padStart(8)}%  ` +
            `${e.profitableWindowsPct.toFixed(0).padStart(8)}%  ` +
            `${e.gridStepPct.toFixed(2).padStart(5)}  ` +
            `${String(e.gridMaxGrids).padStart(5)}  ` +
            `${String(e.gridPauseAfterLossBars).padStart(5)}  ` +
            `  ${e.updatedAt.toISOString().slice(0, 16)}`,
        );
      }
      return entries;
    }).pipe(Effect.provide(makeDbLayer(process.env.NEURATRADE_HOME))),
).pipe(
  Command.withDescription(
    "List the DB-backed watchlist for an exchange/timeframe",
  ),
);

export const watchlistCommand = Command.make("watchlist", {}, () =>
  Console.log(
    "Watchlist commands. Use 'watchlist list --exchange <ex> --timeframe <tf>'.",
  ),
).pipe(
  Command.withDescription("DB-backed watchlist management"),
  Command.withSubcommands([watchlistListCommand]),
);
export const demoReadinessCommand = makeDemoReadinessCommand(
  makeDbLayer(process.env.NEURATRADE_HOME),
);

export const parityReplayCommand = makeParityReplayCommand(
  process.env.NEURATRADE_HOME,
);

// Flow Ignition (flow-v1) lives in ./scalp/flow.ts; behavior unchanged.
import {
  flowBacktestCommand,
  flowUniverseCommand,
  flowRecordCommand,
  flowTradeCommand,
  type FlowTradeArgs,
} from "./scalp/flow.js";
export {
  flowBacktestCommand,
  flowUniverseCommand,
  flowRecordCommand,
  flowTradeCommand,
  type FlowTradeArgs,
} from "./scalp/flow.js";

const timesFmForecastCommand = makeTimesFmForecastCommand(
  makeDbLayer(process.env.NEURATRADE_HOME),
);

export const scalpCommand = Command.make("scalp", {}, () =>
  Console.log(
    "Scalping commands. Use 'scalp backtest|optimize|scan|paper-trade|soak|profile|readiness|demo-readiness|bybit-snapshot|timesfm-forecast|parity-replay|flow-backtest|flow-universe|flow-record|flow-trade --help' for details.",
  ),
).pipe(
  Command.withDescription("Deterministic scalping operations"),
  Command.withSubcommands([
    backtestCommand,
    optimizeCommand,
    scanCommand,
    paperTradeCommand,
    soakCommand,
    profileCommand,
    libraryCommand,
    walkForwardCommand,
    readinessCommand,
    demoReadinessCommand,
    bybitSnapshotCommand,
    timesFmForecastCommand,
    parityReplayCommand,
    gridUniverseScanCommand,
    watchlistCommand,
    flowBacktestCommand,
    flowUniverseCommand,
    flowRecordCommand,
    flowTradeCommand,
    tradeCommand,
  ]),
);
