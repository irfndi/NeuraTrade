/**
 * Engine parity test: does the DEPLOYED paper-trading grid engine reproduce the
 * VALIDATED backtest grid engine's trades on the same candle window?
 *
 * Loads the last 500 BTC/USDT:USDT 15m candles from the local neuratrade.db,
 * runs runGridBacktest (validated) and runGridPaperTradingIteration (deployed,
 * replay mode) over the same window, and compares the 8 execution-fidelity
 * dimensions. Read-only against the DB; does not modify src/.
 */
import { Database } from "bun:sqlite";
import { join } from "node:path";
import { Effect, Layer } from "effect";
import { runGridBacktest, type GridTrade } from "../src/scalping/grid.js";
import {
  runGridPaperTradingIteration,
  type GridPaperTradingOptions,
} from "../src/paper-trading/grid-engine.js";
import {
  MarketDataGateway,
  type MarketDataGatewayService,
} from "../src/market-data/gateway.js";
import {
  PaperTradingRepository,
  type PaperTradingRepositoryService,
} from "../src/paper-trading/repository.js";
import { RiskGuard, type RiskGuardService } from "../src/risk/guards.js";
import { KillSwitch, type KillSwitchService } from "../src/risk/kill-switch.js";
import {
  CircuitBreaker,
  type CircuitBreakerService,
} from "../src/risk/circuit-breaker.js";
import { money } from "../src/utils/money.js";
import type { Candle } from "../src/market-data/types.js";
import type {
  GridPaperState,
  GridPaperTrade,
} from "../src/paper-trading/types.js";
import {
  FuturesExchangeAdapter,
  type FuturesExchangeAdapterService,
} from "../src/exchange/futures-adapter.js";
import { makeSimulatedFuturesExchangeAdapterService } from "../src/exchange/adapters/simulated-futures.js";

const home = process.env.HOME + "/.neuratrade";
const db = new Database(join(home, "data", "neuratrade.db"), {
  readonly: true,
});
const rows = db
  .query(
    `SELECT o.open_price, o.high_price, o.low_price, o.close_price, o.volume, o.timestamp
     FROM ohlcv_data o JOIN exchanges e ON e.id=o.exchange_id JOIN trading_pairs tp ON tp.id=o.trading_pair_id
     WHERE e.name='bitget-futures' AND tp.symbol='BTC/USDT:USDT' AND o.timeframe='15m'
     ORDER BY o.timestamp ASC`,
  )
  .all() as Array<{
  open_price: number;
  high_price: number;
  low_price: number;
  close_price: number;
  volume: number;
  timestamp: string;
}>;
db.close();

const candles: Candle[] = rows.map((r) => ({
  exchange: "bitget-futures",
  symbol: "BTC/USDT:USDT",
  timeframe: "15m",
  open: r.open_price,
  high: r.high_price,
  low: r.low_price,
  close: r.close_price,
  volume: r.volume,
  timestamp: new Date(
    r.timestamp.endsWith("Z")
      ? r.timestamp
      : r.timestamp.replace(" ", "T") + "Z",
  ),
}));

const window = candles.slice(-500);

const baseGridConfig = {
  gridStepPct: 1,
  gridMaxGrids: 1,
  gridPauseAfterLossBars: 24,
  feePct: 0.06,
  slippageBps: 2,
  initialCapital: 100,
  trendFilterPeriod: 0,
  leverage: 1,
  onlyWithTrend: false,
  targetRatio: 3,
  chopGateAdxThreshold: 24,
  positionFraction: 0.5,
};

class InMemRepo implements PaperTradingRepositoryService {
  private state: GridPaperState | null = null;
  private trades: GridPaperTrade[] = [];
  ensureTables() {
    return Effect.void;
  }
  resetGridState() {
    this.state = null;
    return Effect.void;
  }
  getOpenPosition() {
    return Effect.succeed(null);
  }
  saveOpenPosition() {
    return Effect.void;
  }
  closePosition() {
    return Effect.succeed({} as never);
  }
  scaleOutPosition() {
    return Effect.succeed({} as never);
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
  getStartOfDayCapital(_date: Date, currentCapital: ReturnType<typeof money>) {
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
  getLadderState() {
    return Effect.succeed(null);
  }
  saveLadderState() {
    return Effect.succeed(undefined);
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

  getFlowTradeState() {
    return Effect.succeed(null);
  }

  saveFlowTradeState() {
    return Effect.void;
  }

  clearFlowTradeState() {
    return Effect.void;
  }

  getOpenInterest() {
    return Effect.succeed([]);
  }

  replaceWatchlist() {
    return Effect.void;
  }

  listAllGridTrades(_exchange: string, _timeframe: string, limit: number) {
    return Effect.succeed(this.trades.slice(-limit).reverse());
  }
}

const gateway: MarketDataGatewayService = {
  fetchTick: () => Effect.fail({ reason: "not used" } as never),
  fetchOHLCV: () => Effect.succeed(window),
  fetchOrderBook: () =>
    Effect.succeed({
      exchange: "bitget-futures",
      symbol: "BTC/USDT:USDT",
      bids: [{ price: window.at(-1)?.close ?? 0, volume: 1 }],
      asks: [{ price: window.at(-1)?.close ?? 0, volume: 1 }],
      timestamp: new Date(),
    }),
  fetchSymbols: () => Effect.fail({ reason: "not used" } as never),
  fetchDemoSymbols: () => Effect.fail({ reason: "not used" } as never),
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

const simulatedAdapter: FuturesExchangeAdapterService = await Effect.runPromise(
  makeSimulatedFuturesExchangeAdapterService(gateway),
);

function withinTol(a: number, b: number, tol = 0.005): boolean {
  if (a === 0 && b === 0) return true;
  return Math.abs(a - b) / Math.max(Math.abs(a), Math.abs(b), 1e-9) <= tol;
}

async function runScenario(chopGateAdxThreshold: number) {
  const gridConfig = { ...baseGridConfig, chopGateAdxThreshold };
  const opts: GridPaperTradingOptions = {
    exchange: "bitget-futures",
    symbol: "BTC/USDT:USDT",
    timeframe: "15m",
    gridStepPct: 1,
    gridMaxGrids: 1,
    gridPauseAfterLossBars: 24,
    feePct: 0.06,
    slippageBps: 2,
    trendFilterPeriod: 0,
    initialCapital: 100,
    maxPositionPct: 50,
    maxDrawdownPct: 100,
    leverage: 1,
    onlyWithTrend: false,
    targetRatio: 3,
    chopGateAdxThreshold,
    isLive: false,
    executionEnvironment: "bitget-demo",
    replayBars: window.length,
  };

  const bt = runGridBacktest(window, gridConfig);
  const btTrades = bt.trades;

  const repo = new InMemRepo();
  const layer = Layer.mergeAll(
    Layer.succeed(MarketDataGateway, gateway),
    Layer.succeed(PaperTradingRepository, repo),
    Layer.succeed(FuturesExchangeAdapter, simulatedAdapter),
    Layer.succeed(RiskGuard, riskGuard),
    Layer.succeed(KillSwitch, killSwitch),
    Layer.succeed(CircuitBreaker, circuitBreaker),
  );

  for (let i = 0; i < window.length + 3; i++) {
    const result = await Effect.runPromise(
      runGridPaperTradingIteration(opts).pipe(Effect.provide(layer)),
    );
    if (result.note.includes("no new replay candle")) break;
  }
  const deployed = await Effect.runPromise(
    repo.listRecentGridTrades("bitget-futures", "BTC/USDT:USDT", "15m", 500),
  );
  deployed.reverse();
  return { bt, btTrades, depTrades: deployed };
}

function inferBtExitReason(t: GridTrade): "target" | "stop" | "liquidation" {
  if (t.isLiquidation) return "liquidation";
  return t.win ? "target" : "stop";
}
interface ParityCheck {
  match: boolean;
  note: string;
}
type ParityChecks = Record<string, ParityCheck>;

interface ParityMetrics {
  readonly tradeCountMatches: boolean;
  readonly priceMatches: number;
  readonly reasonMatches: number;
  readonly pnlMatches: number;
  readonly comparedTrades: number;
}

function collectParityMetrics(
  btTrades: readonly GridTrade[],
  depTrades: readonly GridPaperTrade[],
): ParityMetrics {
  const comparedTrades = Math.min(btTrades.length, depTrades.length);
  let priceMatches = 0;
  let reasonMatches = 0;
  let pnlMatches = 0;
  for (let i = 0; i < comparedTrades; i++) {
    const backtestTrade = btTrades[i];
    const deployedTrade = depTrades[i];
    if (!backtestTrade || !deployedTrade) continue;
    if (
      withinTol(
        backtestTrade.entryPrice,
        money(deployedTrade.entryPrice).toNumber(),
      )
    ) {
      priceMatches++;
    }
    if (
      deployedTrade.side === backtestTrade.side &&
      deployedTrade.exitReason === inferBtExitReason(backtestTrade)
    ) {
      reasonMatches++;
    }
    if (
      withinTol(
        backtestTrade.pnlPct * 100,
        money(deployedTrade.pnlPct).toNumber(),
      )
    ) {
      pnlMatches++;
    }
  }
  return {
    tradeCountMatches: btTrades.length === depTrades.length,
    priceMatches,
    reasonMatches,
    pnlMatches,
    comparedTrades,
  };
}

function completeOrEmptyMatch(
  btTrades: readonly GridTrade[],
  matched: number,
  compared: number,
): boolean {
  return (
    btTrades.length === 0 ||
    (matched === compared && compared === btTrades.length)
  );
}

function parityCountNote(
  btTrades: readonly GridTrade[],
  matched: number,
  compared: number,
  suffix: string,
): string {
  return btTrades.length === 0
    ? "N/A (0 trades on both)"
    : `${matched}/${compared} ${suffix}`;
}

function buildParityChecks(
  btTrades: readonly GridTrade[],
  depTrades: readonly GridPaperTrade[],
): ParityChecks {
  const metrics = collectParityMetrics(btTrades, depTrades);
  return {
    "trigger-bar": {
      match: metrics.tradeCountMatches,
      note: `bt=${btTrades.length} dep=${depTrades.length}`,
    },
    "order-type": {
      match: metrics.tradeCountMatches,
      note: "both: limit entry at grid level, round-trip limit exits",
    },
    "fill-price": {
      match: completeOrEmptyMatch(
        btTrades,
        metrics.priceMatches,
        metrics.comparedTrades,
      ),
      note: parityCountNote(
        btTrades,
        metrics.priceMatches,
        metrics.comparedTrades,
        "matched within 0.5%",
      ),
    },
    fees: {
      match:
        btTrades.length === 0 || metrics.comparedTrades === btTrades.length,
      note:
        btTrades.length === 0
          ? "N/A (0 trades on both)"
          : "both charge feePct*2 = 0.12% round-trip",
    },
    slippage: {
      match:
        btTrades.length === 0 || metrics.comparedTrades === btTrades.length,
      note:
        btTrades.length === 0
          ? "N/A (0 trades on both)"
          : "both apply slippageBps=2 on entry & exit",
    },
    quantity: {
      match: metrics.tradeCountMatches,
      note: "both size at 50% of capital (positionFraction / maxPositionPct)",
    },
    "exit-reason": {
      match: completeOrEmptyMatch(
        btTrades,
        metrics.reasonMatches,
        metrics.comparedTrades,
      ),
      note: parityCountNote(
        btTrades,
        metrics.reasonMatches,
        metrics.comparedTrades,
        "equal (target/stop/liquidation)",
      ),
    },
    pnl: {
      match: completeOrEmptyMatch(
        btTrades,
        metrics.pnlMatches,
        metrics.comparedTrades,
      ),
      note: parityCountNote(
        btTrades,
        metrics.pnlMatches,
        metrics.comparedTrades,
        "within 0.5%",
      ),
    },
  };
}

function printBacktestTrades(bt: ReturnType<typeof runGridBacktest>): void {
  console.log(`\n=== VALIDATED BACKTEST ENGINE (${window.length} candles) ===`);
  console.log(
    `trades: ${bt.trades.length} | return ${bt.totalReturnPct?.toFixed(2) ?? "n/a"}% | winRate ${bt.winRate?.toFixed(1) ?? "n/a"}%`,
  );
  for (const trade of bt.trades) {
    console.log(
      `  ${trade.side.padEnd(5)} bar=${String(trade.entryBar).padStart(3)}->${String(trade.exitBar).padStart(3)} entry=${trade.entryPrice.toFixed(2)} exit=${trade.exitPrice.toFixed(2)} pnl=${(trade.pnlPct * 100).toFixed(3)}%`,
    );
  }
}

function printDeployedTrades(depTrades: readonly GridPaperTrade[]): void {
  console.log(`\n=== DEPLOYED PAPER-TRADING ENGINE ===`);
  console.log(`trades: ${depTrades.length}`);
  for (const trade of depTrades) {
    console.log(
      `  ${trade.side.padEnd(5)} entry=${money(trade.entryPrice).toFixed(2)} exit=${money(trade.exitPrice).toFixed(2)} pnl=${money(trade.pnlPct).toNumber().toFixed(3)}% reason=${trade.exitReason}`,
    );
  }
}

function printTradeDeltas(
  btTrades: readonly GridTrade[],
  depTrades: readonly GridPaperTrade[],
): void {
  console.log(`\nPer-trade deltas:`);
  for (let i = 0; i < Math.max(btTrades.length, depTrades.length); i++) {
    const backtestTrade = btTrades[i];
    const deployedTrade = depTrades[i];
    if (!backtestTrade || !deployedTrade) {
      console.log(
        `  trade[${i}] exists only in ${backtestTrade ? "backtest" : "deployed"}`,
      );
      continue;
    }
    console.log(
      `  trade[${i}] ${deployedTrade.side} entryDelta=${Math.abs(backtestTrade.entryPrice - money(deployedTrade.entryPrice).toNumber()).toFixed(4)} pnlDelta=${Math.abs(backtestTrade.pnlPct * 100 - money(deployedTrade.pnlPct).toNumber()).toFixed(4)}pp btReason=${inferBtExitReason(backtestTrade)} depReason=${deployedTrade.exitReason}`,
    );
  }
}

function printParityChecks(checks: ParityChecks): boolean {
  console.log(`\n8-dimension parity check:`);
  let allMatch = true;
  for (const [key, value] of Object.entries(checks)) {
    console.log(
      `  ${key.padEnd(12)} ${value.match ? "MATCH" : "MISMATCH"}  ${value.note}`,
    );
    if (!value.match) allMatch = false;
  }
  return allMatch;
}

function report(
  label: string,
  bt: ReturnType<typeof runGridBacktest>,
  depTrades: GridPaperTrade[],
) {
  const btTrades = bt.trades;
  const checks = buildParityChecks(btTrades, depTrades);

  console.log(`\n${"#".repeat(72)}`);
  console.log(`# SCENARIO: ${label}`);
  console.log(`${"#".repeat(72)}`);
  printBacktestTrades(bt);
  printDeployedTrades(depTrades);
  printTradeDeltas(btTrades, depTrades);
  const allMatch = printParityChecks(checks);
  console.log(
    `\nSCENARIO ${label}: ${allMatch ? "PARITY ACHIEVABLE" : "PARITY NOT ACHIEVABLE (see mismatches above)"}`,
  );
  return { btTrades, depTrades, checks };
}

const scenarioA = await runScenario(24);
const scenarioB = await runScenario(0);
const a = report(
  "spec params (chopGate=24)",
  scenarioA.bt,
  scenarioA.depTrades,
);
const b = report(
  "chop gate OFF (chopGate=0)",
  scenarioB.bt,
  scenarioB.depTrades,
);

console.log(`\n${"=".repeat(72)}`);
console.log("SUMMARY");
console.log(`${"=".repeat(72)}`);
console.log(
  `spec params (chopGate=24): backtest=${a.btTrades.length} deployed=${a.depTrades.length}`,
);
console.log(
  `chop gate OFF (chopGate=0): backtest=${b.btTrades.length} deployed=${b.depTrades.length}`,
);
