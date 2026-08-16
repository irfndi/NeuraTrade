import { Effect } from "effect";
import { Database } from "bun:sqlite";
import { MarketDataRepository, MarketDataRepositorySQLiteLive } from "../src/market-data/repository.js";
import { ladderGateScoredEligibility, type GridUniverseEntry, type GridUniverseOptions } from "../src/scalping/grid-universe.js";

const DB = `${process.env.HOME}/.neuratrade/data/neuratrade.db`;

const program = Effect.gen(function* () {
  const repo = yield* MarketDataRepository;
  const symbols = yield* repo.listSymbols("bybit-futures", "15m", 500);
  console.log(`symbols with >=500 15m candles: ${symbols.length}`);
  if (symbols.length === 0) return;
  const symbol = symbols[0];
  const candles = yield* repo.getCandles({
    exchange: "bybit-futures",
    symbol,
    timeframe: "15m",
    limit: 60000,
  });
  console.log(`testing ${symbol}: ${candles.length} candles`);

  const entry: GridUniverseEntry = {
    symbol,
    candles: candles.length,
    bestParams: { gridStepPct: 1.25, gridMaxGrids: 3, gridPauseAfterLossBars: 6, rungs: 2 },
    walkForward: {
      windows: [],
      aggregateReturnPct: 10,
      profitableWindowsPct: 70,
      maxDrawdownPct: 5,
      totalTrades: 100,
    },
    passed: true,
    volatility: 2,
    oosTrades: 100,
    fillsPerDay: 20,
    edgePerTradePct: 0.1,
  };

  const options: GridUniverseOptions = {
    exchange: "bybit-futures",
    timeframe: "15m",
    initialCapital: 50,
    minCandles: 500,
    trainWindow: 180,
    testWindow: 60,
    minProfitableWindowsPct: 60,
    minAggregateReturnPct: 0,
    feePct: 0.02,
    slippageBps: 1,
    trendFilterPeriod: 0,
    searchSpace: { gridStepPct: [1.25], gridMaxGrids: [3], gridPauseAfterLossBars: [6], rungs: [2] },
    engine: "ladder",
    tier: "readiness",
  };

  const started = Date.now();
  const result = ladderGateScoredEligibility(entry, candles, options);
  const elapsed = ((Date.now() - started) / 1000).toFixed(1);
  console.log(`readiness ladder eligibility (${elapsed}s): ${result ? `PASS validatedTargetRatio=${result.validatedTargetRatio} chopAdx=${result.validatedChopGateAdx}` : "null (failed a gate)"}`);
});

Effect.runPromise(
  program.pipe(
    Effect.provide(MarketDataRepositorySQLiteLive(new Database(DB))),
  ),
).then(
  () => process.exit(0),
  (err) => {
    console.error("FAILED:", err);
    process.exit(1);
  },
);
