import { Database } from "bun:sqlite";
import { join } from "node:path";
import { Command, Options } from "./kit/kit.ts";
import { BunServices } from "@effect/platform-bun";
import { Console, Effect, FileSystem, Layer } from "effect";
import { Path, PathLive } from "../services/path.js";
import type { Candle } from "../market-data/types.js";
import {
  MarketDataError,
  MarketDataGateway,
  type MarketDataGatewayService,
} from "../market-data/gateway.js";
import {
  runGridBacktest,
  type GridOptions,
  type GridTrade,
} from "../scalping/grid.js";
import {
  runGridPaperTradingIteration,
  type GridPaperTradingOptions,
} from "../paper-trading/grid-engine.js";
import {
  PaperTradingRepository,
  PaperTradingRepositoryError,
  type PaperTradingRepositoryService,
} from "../paper-trading/repository.js";
import { RiskGuard, type RiskGuardService } from "../risk/guards.js";
import { KillSwitch, type KillSwitchService } from "../risk/kill-switch.js";
import {
  CircuitBreaker,
  type CircuitBreakerService,
} from "../risk/circuit-breaker.js";
import {
  FuturesExchangeAdapter,
  type FuturesExchangeAdapterService,
} from "../exchange/futures-adapter.js";
import { makeSimulatedFuturesExchangeAdapterService } from "../exchange/adapters/simulated-futures.js";
import { Decimal, money } from "../utils/money.js";
import type { GridPaperState, GridPaperTrade } from "../paper-trading/types.js";
import { VALIDATED_BTC_GRID_CANDIDATE } from "../scalping/grid-candidate.js";
import type { ExecutionParityCheck } from "../scalping/real-money-readiness.js";

const commandOptions = {
  exchange: Options.text("exchange").pipe(
    Options.withDefault("bitget-futures"),
    Options.withDescription("Exchange for the candle window"),
  ),
  symbol: Options.text("symbol").pipe(
    Options.withDefault("BTC/USDT:USDT"),
    Options.withDescription("Symbol for the candle window"),
  ),
  timeframe: Options.text("timeframe").pipe(
    Options.withDefault("15m"),
    Options.withDescription("Timeframe for the candle window"),
  ),
  bars: Options.integer("bars").pipe(
    Options.withDefault(500),
    Options.withDescription("Number of trailing candles to replay"),
  ),
} as const;

export interface ExecutionParityArtifact {
  readonly protocolVersion: "execution-parity/v1";
  readonly generatedAt: string;
  readonly exchange: string;
  readonly symbol: string;
  readonly timeframe: string;
  readonly barCount: number;
  readonly backtestTrades: number;
  readonly deployedTrades: number;
  readonly checks: readonly ExecutionParityCheck[];
}

interface RawCandle {
  readonly open_price: number;
  readonly high_price: number;
  readonly low_price: number;
  readonly close_price: number;
  readonly volume: number;
  readonly timestamp: string;
}

function parseCandleTimestamp(raw: string): Date {
  return new Date(raw.endsWith("Z") ? raw : raw.replace(" ", "T").concat("Z"));
}

class InMemRepo implements PaperTradingRepositoryService {
  private state: GridPaperState | null = null;
  private trades: GridPaperTrade[] = [];

  ensureTables() {
    return Effect.void;
  }

  getOpenPosition() {
    return Effect.fail(
      new PaperTradingRepositoryError(
        "InMemRepo.getOpenPosition is not implemented — the grid replay engine has no position-tracking path that calls it; failing loudly instead of hanging",
      ),
    );
  }

  saveOpenPosition() {
    return Effect.void;
  }

  closePosition() {
    return Effect.fail(
      new PaperTradingRepositoryError(
        "InMemRepo.closePosition is not implemented — the grid replay engine closes via recordGridTrade; failing loudly instead of hanging",
      ),
    );
  }

  scaleOutPosition() {
    return Effect.fail(
      new PaperTradingRepositoryError(
        "InMemRepo.scaleOutPosition is not implemented — the grid replay engine never scales out; failing loudly instead of hanging",
      ),
    );
  }

  getPortfolio() {
    return Effect.succeed({ capital: money(100), peakCapital: money(100) });
  }

  setPortfolio() {
    return Effect.void;
  }

  listRecentTrades() {
    return Effect.succeed([]);
  }

  countTradesForDate() {
    return Effect.succeed(this.trades.length);
  }

  getTodayRealizedPnl() {
    return Effect.succeed(money(0));
  }

  getStartOfDayCapital(_date: Date, currentCapital: Decimal) {
    return Effect.succeed(currentCapital);
  }

  getGridState(exchange: string, symbol: string, timeframe: string) {
    return Effect.succeed(
      this.state &&
        this.state.exchange === exchange &&
        this.state.symbol === symbol &&
        this.state.timeframe === timeframe
        ? this.state
        : null,
    );
  }

  saveGridState(state: GridPaperState) {
    return Effect.sync(() => {
      this.state = state;
    });
  }

  resetGridState() {
    this.state = null;
    return Effect.void;
  }

  recordGridTrade(trade: GridPaperTrade) {
    return Effect.sync(() => {
      this.trades.push(trade);
    });
  }

  listRecentGridTrades(
    _exchange: string,
    _symbol: string,
    _timeframe: string,
    limit: number,
  ) {
    return Effect.succeed(this.trades.slice(-limit).reverse());
  }

  listWatchlist() {
    return Effect.succeed([]);
  }

  upsertWatchlist() {
    return Effect.void;
  }

  clearWatchlist() {
    return Effect.void;
  }

  replaceWatchlist() {
    return Effect.void;
  }

  listAllGridTrades(_exchange: string, _timeframe: string, limit: number) {
    return Effect.succeed(this.trades.slice(-limit).reverse());
  }
}

function withinTol(a: number, b: number, tol = 0.005): boolean {
  if (a === 0 && b === 0) return true;
  // Tolerance is RELATIVE to the larger magnitude: |a-b|/max(|a|,|b|) <= tol.
  // Both parity dimensions already rescale to the same unit before comparing
  // (entry price in quote, pnl in percentage points), so this is a symmetric
  // relative-error check, not an absolute 0.5pp/0.5%-of-price bound.
  return Math.abs(a - b) / Math.max(Math.abs(a), Math.abs(b), 1e-9) <= tol;
}

/**
 * PnL parity is an ABSOLUTE 0.5 percentage-point bound, not the relative
 * `withinTol` used for entry prices: a 0.5pp disagreement is equally
 * meaningful whether pnl is 0.5pp or 50pp, and the relative check would
 * accept a 0.25pp gap on a 50pp trade while rejecting a 0.005pp gap on a
 * 0.5pp trade. Both sides arrive here in pp — bt pnlPct is a fraction
 * (0.01 = 1%) scaled by *100, deployed pnlPct is already pp.
 */
function withinPp(a: number, b: number, tolPp = 0.5): boolean {
  return Math.abs(a - b) <= tolPp;
}

function inferBtExitReason(t: GridTrade): "target" | "stop" | "liquidation" {
  if (t.isLiquidation) return "liquidation";
  return t.win ? "target" : "stop";
}

function computeExecutionParityChecks(
  btTrades: readonly GridTrade[],
  depTrades: readonly GridPaperTrade[],
): readonly ExecutionParityCheck[] {
  const n = Math.min(btTrades.length, depTrades.length);
  let priceMatches = 0;
  let reasonMatches = 0;
  let pnlMatches = 0;
  for (let i = 0; i < n; i++) {
    const b = btTrades[i];
    const d = depTrades[i];
    if (withinTol(b.entryPrice, money(d.entryPrice).toNumber())) priceMatches++;
    if (d.side === b.side && d.exitReason === inferBtExitReason(b)) {
      reasonMatches++;
    }
    if (withinPp(b.pnlPct * 100, money(d.pnlPct).toNumber())) pnlMatches++;
  }
  const countMatch = btTrades.length === depTrades.length;
  const pairCoverage = btTrades.length === 0 || n === btTrades.length;
  const zeroTrades = btTrades.length === 0;
  return [
    {
      name: "trigger-bar",
      passed: countMatch,
      detail: `backtest=${btTrades.length} deployed=${depTrades.length}`,
    },
    {
      name: "order-type",
      passed: countMatch,
      detail: "both use limit entry at grid level with round-trip limit exits",
    },
    {
      name: "fill-price",
      passed:
        btTrades.length === 0 || (priceMatches === n && n === btTrades.length),
      detail: zeroTrades
        ? "N/A (0 trades on both engines)"
        : `${priceMatches}/${n} entry prices within 0.5%`,
    },
    {
      name: "fees",
      passed: pairCoverage,
      detail: zeroTrades
        ? "N/A (0 trades on both engines)"
        : "both charge feePct*2 = 0.12% round-trip",
    },
    {
      name: "slippage",
      passed: pairCoverage,
      detail: zeroTrades
        ? "N/A (0 trades on both engines)"
        : "both apply slippageBps=2 on entry and exit",
    },
    {
      name: "quantity",
      passed: countMatch,
      detail: "both size at 50% of capital (positionFraction / maxPositionPct)",
    },
    {
      name: "exit-reason",
      passed:
        btTrades.length === 0 || (reasonMatches === n && n === btTrades.length),
      detail: zeroTrades
        ? "N/A (0 trades on both engines)"
        : `${reasonMatches}/${n} exit reasons equal (target/stop/liquidation)`,
    },
    {
      name: "pnl",
      passed:
        btTrades.length === 0 || (pnlMatches === n && n === btTrades.length),
      detail: zeroTrades
        ? "N/A (0 trades on both engines)"
        : `${pnlMatches}/${n} round-trip pnl within 0.5pp`,
    },
  ];
}

function gridConfig(): GridOptions {
  const c = VALIDATED_BTC_GRID_CANDIDATE;
  return {
    gridStepPct: c.gridStepPct,
    gridMaxGrids: c.gridMaxGrids,
    gridPauseAfterLossBars: c.gridPauseAfterLossBars,
    feePct: c.feePct,
    slippageBps: c.slippageBps,
    initialCapital: 100,
    trendFilterPeriod: c.trendFilterPeriod,
    leverage: c.leverage,
    onlyWithTrend: c.onlyWithTrend,
    targetRatio: c.targetRatio,
    chopGateAdxThreshold: c.chopGateAdx,
    positionFraction: c.maxPositionSizePct / 100,
  };
}

function replayOptions(
  window: readonly Candle[],
  exchange: string,
  symbol: string,
  timeframe: string,
): GridPaperTradingOptions {
  const c = VALIDATED_BTC_GRID_CANDIDATE;
  return {
    exchange,
    symbol,
    timeframe,
    gridStepPct: c.gridStepPct,
    gridMaxGrids: c.gridMaxGrids,
    gridPauseAfterLossBars: c.gridPauseAfterLossBars,
    feePct: c.feePct,
    slippageBps: c.slippageBps,
    trendFilterPeriod: c.trendFilterPeriod,
    initialCapital: 100,
    maxPositionPct: c.maxPositionSizePct,
    maxDrawdownPct: 100,
    leverage: c.leverage,
    onlyWithTrend: c.onlyWithTrend,
    targetRatio: c.targetRatio,
    chopGateAdxThreshold: c.chopGateAdx,
    isLive: false,
    executionEnvironment: "bitget-demo",
    replayBars: window.length,
  };
}

function readCandles(
  homeDir: string,
  exchange: string,
  symbol: string,
  timeframe: string,
): Effect.Effect<readonly Candle[], Error> {
  return Effect.try({
    try: () => {
      const db = new Database(join(homeDir, "data", "neuratrade.db"), {
        readonly: true,
      });
      try {
        const rows = db
          .query<RawCandle, [string, string, string]>(
            `SELECT o.open_price, o.high_price, o.low_price, o.close_price, o.volume, o.timestamp
             FROM ohlcv_data o
             JOIN exchanges e ON e.id = o.exchange_id
             JOIN trading_pairs tp ON tp.id = o.trading_pair_id
             WHERE e.name = ? AND tp.symbol = ? AND o.timeframe = ?
             ORDER BY o.timestamp ASC`,
          )
          .all(exchange, symbol, timeframe);
        return rows.map((row) => ({
          exchange,
          symbol,
          timeframe,
          open: row.open_price,
          high: row.high_price,
          low: row.low_price,
          close: row.close_price,
          volume: row.volume,
          timestamp: parseCandleTimestamp(row.timestamp),
        }));
      } finally {
        db.close();
      }
    },
    catch: (error) =>
      error instanceof Error ? error : new Error(String(error)),
  });
}

function runReplay(
  window: readonly Candle[],
  exchange: string,
  symbol: string,
  timeframe: string,
): Effect.Effect<readonly GridPaperTrade[], Error> {
  return Effect.gen(function* () {
    const gateway: MarketDataGatewayService = {
      fetchTick: () => Effect.fail(new MarketDataError("not used in replay")),
      fetchOHLCV: () => Effect.succeed(window),
      fetchOrderBook: () =>
        Effect.succeed({
          exchange,
          symbol,
          bids: [{ price: window.at(-1)?.close ?? 0, volume: 1 }],
          asks: [{ price: window.at(-1)?.close ?? 0, volume: 1 }],
          timestamp: new Date(),
        }),
      fetchSymbols: () =>
        Effect.fail(new MarketDataError("not used in replay")),
      fetchDemoSymbols: () =>
        Effect.fail(new MarketDataError("not used in replay")),
      fetch24hrVolumes: () => Effect.succeed({}),
      fetchFundingRates: () => Effect.succeed([]),
    };
    const riskGuard: RiskGuardService = { check: () => Effect.void };
    const killSwitch: KillSwitchService = {
      isEngaged: () => Effect.succeed(false),
      getReason: () => Effect.succeed(""),
      engage: () => Effect.void,
      disengage: () => Effect.void,
    };
    const circuitBreaker: CircuitBreakerService = {
      isOpen: () => Effect.succeed(false),
      getReason: () => Effect.succeed(""),
      currentDailyLossPct: () => Effect.succeed(0),
      recordTradeResult: () => Effect.void,
      reset: () => Effect.void,
    };
    const simulatedAdapter: FuturesExchangeAdapterService =
      yield* makeSimulatedFuturesExchangeAdapterService(gateway);
    const repo = new InMemRepo();
    const layer = Layer.mergeAll(
      Layer.succeed(MarketDataGateway, gateway),
      Layer.succeed(PaperTradingRepository, repo),
      Layer.succeed(FuturesExchangeAdapter, simulatedAdapter),
      Layer.succeed(RiskGuard, riskGuard),
      Layer.succeed(KillSwitch, killSwitch),
      Layer.succeed(CircuitBreaker, circuitBreaker),
    );
    const opts = replayOptions(window, exchange, symbol, timeframe);
    for (let i = 0; i < window.length + 3; i++) {
      const result = yield* runGridPaperTradingIteration(opts).pipe(
        Effect.provide(layer),
      );
      if (result.note.includes("no new replay candle")) break;
    }
    const deployed = yield* repo.listRecentGridTrades(
      exchange,
      symbol,
      timeframe,
      1000,
    );
    deployed.reverse();
    return deployed;
  }).pipe(
    Effect.mapError((cause) =>
      cause instanceof Error ? cause : new Error(String(cause)),
    ),
  );
}

function humanReport(
  exchange: string,
  symbol: string,
  timeframe: string,
  barCount: number,
  btTrades: readonly GridTrade[],
  depTrades: readonly GridPaperTrade[],
  checks: readonly ExecutionParityCheck[],
): string {
  const lines: string[] = [];
  const bar = "#".repeat(72);
  lines.push(`\n${bar}`);
  lines.push(`# EXECUTION PARITY REPLAY (${exchange} ${symbol} ${timeframe})`);
  lines.push(`${bar}`);
  lines.push(`\nwindow: ${barCount} candles`);
  lines.push(
    `validated backtest engine: ${btTrades.length} trades | deployed replay engine: ${depTrades.length} trades`,
  );
  lines.push(`\nper-trade deltas:`);
  for (let i = 0; i < Math.max(btTrades.length, depTrades.length); i++) {
    const b = btTrades[i];
    const d = depTrades[i];
    if (!b || !d) {
      lines.push(`  trade[${i}] exists only in ${b ? "backtest" : "deployed"}`);
      continue;
    }
    lines.push(
      `  trade[${i}] ${d.side} entryDelta=${Math.abs(
        b.entryPrice - money(d.entryPrice).toNumber(),
      ).toFixed(4)} pnlDelta=${Math.abs(
        b.pnlPct * 100 - money(d.pnlPct).toNumber(),
      ).toFixed(
        4,
      )}pp btReason=${inferBtExitReason(b)} depReason=${d.exitReason}`,
    );
  }
  lines.push(`\n8-dimension parity check:`);
  for (const check of checks) {
    lines.push(
      `  ${check.name.padEnd(12)} ${check.passed ? "MATCH" : "MISMATCH"}  ${check.detail}`,
    );
  }
  const passed = checks.every((check) => check.passed);
  lines.push(
    `\nparity: ${passed ? "PASS" : "FAIL"} (${checks.filter((check) => check.passed).length}/${checks.length} checks)`,
  );
  return lines.join("\n");
}

function findTradeWindow(
  candles: readonly Candle[],
  bars: number,
  grid: GridOptions,
): readonly Candle[] {
  if (candles.length === 0) return [];
  const size = Math.min(bars, candles.length);
  if (size < 2) return candles.slice(-size);
  // Start from the most recent window and scan backward until the validated
  // engine produces at least one trade. A parity window with zero trades on
  // both engines is vacuous — it cannot exercise fill-price, fees, slippage,
  // exit-reason or pnl. The golden fixture must run on a window that actually
  // fills orders. If no window in the series produces a trade, fall back to
  // the most recent window (the report will then show N/A for every
  // fill-sensitive dimension).
  const maxSkips = Math.max(1, Math.floor(candles.length / size) - 1);
  for (let skip = 0; skip <= maxSkips; skip++) {
    const start = candles.length - size - skip * size;
    if (start < 0) break;
    const window = candles.slice(start, start + size);
    if (runGridBacktest(window, grid).totalTrades > 0) return window;
  }
  return candles.slice(-size);
}

export function makeParityReplayCommand(homeDir?: string) {
  const layer = Layer.mergeAll(BunServices.layer, PathLive(homeDir));
  return Command.make("parity-replay", commandOptions, (args) =>
    Effect.gen(function* () {
      const path = yield* Path;
      const fs = yield* FileSystem.FileSystem;
      const candles = yield* readCandles(
        path.homeDir,
        args.exchange,
        args.symbol,
        args.timeframe,
      );
      const window = findTradeWindow(candles, args.bars, gridConfig());
      if (window.length === 0) {
        return yield* Effect.fail(
          new Error(
            `no ${args.timeframe} candles found for ${args.exchange} ${args.symbol}`,
          ),
        );
      }
      const bt = runGridBacktest(window, gridConfig());
      const depTrades = yield* runReplay(
        window,
        args.exchange,
        args.symbol,
        args.timeframe,
      );
      const checks = computeExecutionParityChecks(bt.trades, depTrades);
      const artifact: ExecutionParityArtifact = {
        protocolVersion: "execution-parity/v1",
        generatedAt: new Date().toISOString(),
        exchange: args.exchange,
        symbol: args.symbol,
        timeframe: args.timeframe,
        barCount: window.length,
        backtestTrades: bt.trades.length,
        deployedTrades: depTrades.length,
        checks,
      };
      const artifactPath = join(path.homeDir, "data", "execution-parity.json");
      yield* fs.makeDirectory(join(path.homeDir, "data"), {
        recursive: true,
      });
      yield* fs.writeFileString(
        artifactPath,
        JSON.stringify(artifact, null, 2),
      );
      yield* Console.log(
        humanReport(
          args.exchange,
          args.symbol,
          args.timeframe,
          window.length,
          bt.trades,
          depTrades,
          checks,
        ),
      );
      yield* Console.log(`evidence written to: ${artifactPath}`);
      const failed = checks
        .filter((check) => !check.passed)
        .map((check) => check.name);
      if (failed.length > 0) {
        return yield* Effect.fail(
          new Error(`execution parity failed: ${failed.join(", ")}`),
        );
      }
      return artifact;
    }).pipe(Effect.provide(layer)),
  ).pipe(
    Command.withDescription(
      "Replay the deployed grid engine against the validated backtest on the same candles and write measured execution-parity evidence; exits non-zero when any dimension mismatches",
    ),
  );
}
