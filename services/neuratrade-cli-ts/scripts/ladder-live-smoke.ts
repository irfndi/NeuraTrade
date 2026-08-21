import { Effect } from "effect";
import { Database } from "bun:sqlite";
import { MarketDataGateway } from "../src/market-data/gateway.js";
import { MarketDataGatewayLive } from "../src/market-data/gateways/index.js";
import { makeCausalSymbolStats } from "../src/scalping/symbol-stats.js";
import {
  PaperTradingRepository,
  PaperTradingRepositorySQLite,
} from "../src/paper-trading/repository.js";
import { runLadderPaperTradingIteration } from "../src/paper-trading/ladder-engine.js";
import { toNumber } from "../src/utils/money.js";

const DB = process.env.NEURATRADE_HOME
  ? `${process.env.NEURATRADE_HOME}/data/neuratrade.db`
  : `${process.env.HOME}/.neuratrade/data/neuratrade.db`;
const repo = new PaperTradingRepositorySQLite(new Database(DB));

const effect = Effect.gen(function* () {
  const gateway = yield* MarketDataGateway;

  // 1) Prove the chop gate is the only blocker for the single-position grid.
  const candles = yield* gateway.fetchOHLCV(
    "bybit-futures",
    "BTC/USDT:USDT",
    "15m",
    60,
  );
  const stats = makeCausalSymbolStats(candles, "15m");
  const i = candles.length - 1;
  console.log(
    `BTC 15m last bar ADX14 = ${stats(i).adx14.toFixed(1)} (gate threshold 15 -> ${stats(i).adx14 >= 15 ? "BLOCKED" : "allowed"})`,
  );
  for (let k = 0; k < 6; k++) {
    const c = candles[candles.length - 1 - k];
    const step = c.open * 0.005; // 0.5% step
    const buyLevel = c.open - step;
    const wouldEnter = c.low <= buyLevel;
    console.log(
      `  bar-${k} ${c.timestamp.toISOString().slice(11, 16)} open=${c.open.toFixed(2)} low=${c.low.toFixed(2)} buyLevel=${buyLevel.toFixed(2)} -> grid entry ${wouldEnter ? "WOULD trigger" : "no"}`,
    );
  }

  // 2) Run the ladder engine on a real survivor (FARTCOIN, rungs=1, step 0.5, no chop gate).
  const res = yield* runLadderPaperTradingIteration({
    exchange: "bybit-futures",
    symbol: "FARTCOIN/USDT:USDT",
    timeframe: "15m",
    rungs: 1,
    gridStepPct: 0.5,
    gridMaxGrids: 2,
    gridPauseAfterLossBars: 0,
    feePct: 0.02,
    slippageBps: 1,
    initialCapital: 50,
    trendFilterPeriod: 0,
    leverage: 1,
  });
  console.log(
    `LADDER FARTCOIN: action=${res.action} capital=${res.capital.toFixed(4)} openRungs=${res.openRungs} note=${res.note}`,
  );
  const saved = yield* repo.getLadderState(
    "bybit-futures",
    "FARTCOIN/USDT:USDT",
    "15m",
  );
  console.log(
    `  persisted: capital=${saved ? toNumber(saved.capital) : "?"} lastTs=${saved?.lastTimestamp?.toISOString()}`,
  );
});

Effect.runPromise(
  effect.pipe(
    Effect.provide(MarketDataGatewayLive),
    Effect.provideService(PaperTradingRepository, repo),
  ),
).then(
  () => process.exit(0),
  (err) => {
    console.error("FAILED:", err);
    process.exit(1);
  },
);
