// ponytail: grid-parity fixture — GRID half of Gate 4, both sides EXECUTED.
// Drives the REAL TS paper engine (`runGridPaperTradingIteration`, the same
// path the box's grid soaks use) over the 6-candle fixture from
// `crates/nt-grid/examples/paper_engine_check.rs` with in-memory services
// (same stub pattern as `src/cli/parity-replay.ts`), then writes the TS fill
// prices as CSV for the Rust example to replay and assert against.
//
// NOT the ladder soak: that engine is multi-rung and is P5 Step 3 scope.
// Deliberately does NOT use `runGridBacktest` — that research path divides
// target exits by slippage, while the paper engine (and Rust) do not.
//
// Run: cd services/neuratrade-cli-ts && bun run grid_parity_fixture.ts
import { Effect, Layer } from "effect";
import { writeFileSync } from "node:fs";
import { makeSimulatedFuturesExchangeAdapterService } from "./src/exchange/adapters/simulated-futures.ts";
import { FuturesExchangeAdapter } from "./src/exchange/futures-adapter.ts";
import {
  MarketDataError,
  MarketDataGateway,
  type MarketDataGatewayService,
} from "./src/market-data/gateway.ts";
import {
  PaperTradingRepository,
  PaperTradingRepositoryError,
  type PaperTradingRepositoryService,
} from "./src/paper-trading/repository.ts";
import { runGridPaperTradingIteration } from "./src/paper-trading/grid-engine.ts";
import {
  CircuitBreaker,
  type CircuitBreakerService,
} from "./src/risk/circuit-breaker.ts";
import { KillSwitch, type KillSwitchService } from "./src/risk/kill-switch.ts";
import { RiskGuard, type RiskGuardService } from "./src/risk/guards.ts";
import type { Candle } from "./src/market-data/types.ts";
import type { GridPaperTrade } from "./src/paper-trading/types.ts";
import { money } from "./src/utils/money.ts";
import type { GridPaperState } from "./src/paper-trading/types.ts";

// Same fixture as paper_engine_check.rs: bar 0/5 flat, bar 1 long entry,
// bar 2 long target exit, bar 3 short entry, bar 4 short stop exit.
const h = 3_600_000;
const mk = (
  i: number,
  open: number,
  high: number,
  low: number,
  close: number,
): Candle => ({
  exchange: "bybit-futures",
  symbol: "BTC/USDT:USDT",
  timeframe: "15m",
  open,
  high,
  low,
  close,
  volume: 1,
  timestamp: new Date(i * 2 * h),
});
const candles: Candle[] = [
  mk(0, 100, 100.3, 99.8, 100.1),
  mk(1, 100, 100.5, 98.5, 99.5),
  mk(2, 100.5, 100.6, 99.9, 100.4),
  mk(3, 100, 101.6, 99.3, 101.2),
  mk(4, 100.8, 102.6, 100.2, 102.3),
  mk(5, 102.3, 102.5, 102.1, 102.2),
];

const exchange = "bybit-futures";
const symbol = "BTC/USDT:USDT";
const timeframe = "15m";

const trades: GridPaperTrade[] = [];
let state: GridPaperState | null = null;

const repo: PaperTradingRepositoryService = {
  ensureTables: () => Effect.void,
  getOpenPosition: () =>
    Effect.fail(
      new PaperTradingRepositoryError("replay: no open-position path"),
    ),
  saveOpenPosition: () => Effect.void,
  closePosition: () =>
    Effect.fail(new PaperTradingRepositoryError("replay: no close path")),
  scaleOutPosition: () =>
    Effect.fail(new PaperTradingRepositoryError("replay: no scale-out path")),
  getPortfolio: () => Effect.succeed({ capital: money(1000), peakCapital: money(1000) }),
  setPortfolio: () => Effect.void,
  listRecentTrades: () => Effect.succeed(trades),
  countTradesForDate: () => Effect.succeed(trades.length),
  getTodayRealizedPnl: () => Effect.succeed(money(0)),
  getStartOfDayCapital: (_d: Date, cap) => Effect.succeed(cap),
  getGridState: (ex, sym, tf) =>
    Effect.succeed(
      state && state.exchange === ex && state.symbol === sym && state.timeframe === tf
        ? state
        : null,
    ),
  saveGridState: (s) =>
    Effect.sync(() => {
      state = s;
    }),
  getLadderState: () => Effect.succeed(null),
  saveLadderState: () => Effect.succeed(undefined),
  resetGridState: () =>
    Effect.sync(() => {
      state = null;
    }),
  recordGridTrade: (t) =>
    Effect.sync(() => {
      trades.push(t);
    }),
  listRecentGridTrades: (ex, sym, tf) =>
    Effect.succeed(
      trades.filter(
        (t) => t.exchange === ex && t.symbol === sym && t.timeframe === tf,
      ),
    ),
} as unknown as PaperTradingRepositoryService;

const gateway: MarketDataGatewayService = {
  fetchTick: () => Effect.fail(new MarketDataError("not used in replay")),
  fetchOHLCV: () => Effect.succeed(candles),
  fetchOrderBook: () =>
    Effect.succeed({
      exchange,
      symbol,
      bids: [{ price: candles.at(-1)!.close, volume: 1 }],
      asks: [{ price: candles.at(-1)!.close, volume: 1 }],
      timestamp: new Date(),
    }),
  fetchSymbols: () => Effect.fail(new MarketDataError("not used in replay")),
  fetchDemoSymbols: () => Effect.fail(new MarketDataError("not used in replay")),
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

// Mirror of paper_engine_check.rs PaperEngineConfig: step 100bp, target 1.0x,
// stop 2 grids, slippage 50bp, pos 10%, fee 6bp, capital 1000. replayBars
// walks the whole fixture one bar per iteration (deterministic shadow).
const options = {
  exchange,
  symbol,
  timeframe,
  initialCapital: 1000,
  gridStepPct: 1,
  gridMaxGrids: 2,
  gridPauseAfterLossBars: 0,
  targetRatio: 1,
  trendFilterPeriod: 0,
  chopGateAdxThreshold: 0,
  leverage: 1,
  feePct: 0.06,
  slippageBps: 50,
  maxPositionPct: 10,
  maxDrawdownPct: 100,
  replayBars: candles.length,
};

const program = Effect.gen(function* () {
  const adapter = yield* makeSimulatedFuturesExchangeAdapterService(gateway);
  const layer = Layer.mergeAll(
    Layer.succeed(MarketDataGateway, gateway),
    Layer.succeed(PaperTradingRepository, repo),
    Layer.succeed(FuturesExchangeAdapter, adapter),
    Layer.succeed(RiskGuard, riskGuard),
    Layer.succeed(KillSwitch, killSwitch),
    Layer.succeed(CircuitBreaker, circuitBreaker),
  );
  // One iteration per candle: the engine advances its own cursor, and the
  // replay ends when no new candle remains.
  for (let i = 0; i < candles.length + 3; i++) {
    const result = yield* runGridPaperTradingIteration(options).pipe(
      Effect.provide(layer),
    );
    if (result.note.includes("no new")) break;
  }
});

await Effect.runPromise(program as Effect.Effect<void, never, never>);

console.log(`ts-paper-grid: trades=${trades.length}`);
for (const t of trades) {
  console.log(
    `  ${t.side} entry=${t.entryPrice} exit=${t.exitPrice} reason=${t.exitReason} pnlPct=${t.pnlPct}`,
  );
}
if (trades.length === 0) {
  console.error("fixture produced no trades — Rust side would assert nothing");
  process.exit(1);
}

const candleCsv = candles
  .map((c, i) =>
    [
      i * 2 * h,
      Math.round(c.open * 1_000_000),
      Math.round(c.high * 1_000_000),
      Math.round(c.low * 1_000_000),
      Math.round(c.close * 1_000_000),
    ].join(","),
  )
  .join("\n");
const tradeCsv = trades
  .map((t) =>
    [
      t.side,
      Math.round(Number(t.entryPrice) * 1_000_000),
      Math.round(Number(t.exitPrice) * 1_000_000),
      t.exitReason,
    ].join(","),
  )
  .join("\n");
writeFileSync(new URL("./grid_parity_fixture.candles.csv", import.meta.url), candleCsv + "\n");
writeFileSync(new URL("./grid_parity_fixture.trades.csv", import.meta.url), tradeCsv + "\n");
