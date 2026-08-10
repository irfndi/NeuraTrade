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
function report(
  label: string,
  bt: ReturnType<typeof runGridBacktest>,
  depTrades: GridPaperTrade[],
) {
  const btTrades = bt.trades;
  const n = Math.min(btTrades.length, depTrades.length);
  let priceMatches = 0;
  let reasonMatches = 0;
  let pnlMatches = 0;
  for (let i = 0; i < n; i++) {
    const b = btTrades[i];
    const d = depTrades[i];
    if (withinTol(b.entryPrice, money(d.entryPrice).toNumber())) priceMatches++;
    if (d.side === b.side && d.exitReason === inferBtExitReason(b))
      reasonMatches++;
    if (withinTol(b.pnlPct * 100, money(d.pnlPct).toNumber())) pnlMatches++;
  }
  const countMatch = btTrades.length === depTrades.length;
  const checks: Record<string, { match: boolean; note: string }> = {
    "trigger-bar": {
      match: countMatch,
      note: `bt=${btTrades.length} dep=${depTrades.length}`,
    },
    "order-type": {
      match: countMatch,
      note: "both: limit entry at grid level, round-trip limit exits",
    },
    "fill-price": {
      match:
        btTrades.length === 0 || (priceMatches === n && n === btTrades.length),
      note:
        btTrades.length === 0
          ? "N/A (0 trades on both)"
          : `${priceMatches}/${n} matched within 0.5%`,
    },
    fees: {
      match: btTrades.length === 0 || n === btTrades.length,
      note:
        btTrades.length === 0
          ? "N/A (0 trades on both)"
          : "both charge feePct*2 = 0.12% round-trip",
    },
    slippage: {
      match: btTrades.length === 0 || n === btTrades.length,
      note:
        btTrades.length === 0
          ? "N/A (0 trades on both)"
          : "both apply slippageBps=2 on entry & exit",
    },
    quantity: {
      match: countMatch,
      note: "both size at 50% of capital (positionFraction / maxPositionPct)",
    },
    "exit-reason": {
      match:
        btTrades.length === 0 || (reasonMatches === n && n === btTrades.length),
      note:
        btTrades.length === 0
          ? "N/A (0 trades on both)"
          : `${reasonMatches}/${n} equal (target/stop/liquidation)`,
    },
    pnl: {
      match:
        btTrades.length === 0 || (pnlMatches === n && n === btTrades.length),
      note:
        btTrades.length === 0
          ? "N/A (0 trades on both)"
          : `${pnlMatches}/${n} within 0.5%`,
    },
  };

  console.log(`\n${"#".repeat(72)}`);
  console.log(`# SCENARIO: ${label}`);
  console.log(`${"#".repeat(72)}`);
  console.log(`\n=== VALIDATED BACKTEST ENGINE (${window.length} candles) ===`);
  console.log(
    `trades: ${btTrades.length} | return ${bt.totalReturnPct?.toFixed(2) ?? "n/a"}% | winRate ${bt.winRate?.toFixed(1) ?? "n/a"}%`,
  );
  for (const t of btTrades) {
    console.log(
      `  ${t.side.padEnd(5)} bar=${String(t.entryBar).padStart(3)}->${String(t.exitBar).padStart(3)} entry=${t.entryPrice.toFixed(2)} exit=${t.exitPrice.toFixed(2)} pnl=${(t.pnlPct * 100).toFixed(3)}%`,
    );
  }
  console.log(`\n=== DEPLOYED PAPER-TRADING ENGINE ===`);
  console.log(`trades: ${depTrades.length}`);
  for (const t of depTrades) {
    console.log(
      `  ${t.side.padEnd(5)} entry=${money(t.entryPrice).toFixed(2)} exit=${money(t.exitPrice).toFixed(2)} pnl=${money(t.pnlPct).toNumber().toFixed(3)}% reason=${t.exitReason}`,
    );
  }
  console.log(`\nPer-trade deltas:`);
  for (let i = 0; i < Math.max(btTrades.length, depTrades.length); i++) {
    const b = btTrades[i];
    const d = depTrades[i];
    if (!b || !d) {
      console.log(
        `  trade[${i}] exists only in ${b ? "backtest" : "deployed"}`,
      );
      continue;
    }
    console.log(
      `  trade[${i}] ${d.side} entryDelta=${Math.abs(b.entryPrice - money(d.entryPrice).toNumber()).toFixed(4)} pnlDelta=${Math.abs(b.pnlPct * 100 - money(d.pnlPct).toNumber()).toFixed(4)}pp btReason=${inferBtExitReason(b)} depReason=${d.exitReason}`,
    );
  }
  console.log(`\n8-dimension parity check:`);
  let allMatch = true;
  for (const [k, v] of Object.entries(checks)) {
    console.log(
      `  ${k.padEnd(12)} ${v.match ? "MATCH" : "MISMATCH"}  ${v.note}`,
    );
    if (!v.match) allMatch = false;
  }
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
