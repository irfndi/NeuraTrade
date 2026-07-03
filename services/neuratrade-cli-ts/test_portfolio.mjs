import { Database } from "bun:sqlite";
import { runPortfolioBacktest } from "./src/scalping/portfolio-backtest.ts";
import { defaultComposerConfig } from "./src/scalping/composer.ts";

const home = process.env.NEURATRADE_HOME || `${process.env.HOME}/.neuratrade`;
const db = new Database(`${home}/data/neuratrade.db`);
const rows = db
  .query(
    `SELECT open_price as open, high_price as high, low_price as low, close_price as close, volume, timestamp 
   FROM ohlcv_data 
   WHERE exchange_id = (SELECT id FROM exchanges WHERE name = 'binance') 
     AND trading_pair_id = (SELECT id FROM trading_pairs WHERE symbol = 'BTC/USDT') 
     AND timeframe = '5m' 
   ORDER BY timestamp ASC`,
  )
  .all();
const candles = rows.map((r) => ({ ...r, timestamp: new Date(r.timestamp) }));

const composerConfig = {
  ...defaultComposerConfig,
  thresholds: {
    ...defaultComposerConfig.thresholds,
    trendSignalStyle: "cross",
  },
};

function run(sl, tp, conf, maxPos, label) {
  const result = runPortfolioBacktest({
    symbol: "BTC/USDT",
    exchange: "binance",
    timeframe: "5m",
    candles,
    composerConfig,
    initialCapital: 10000,
    positionSizePct: 100 / maxPos,
    maxOpenPositions: maxPos,
    stopLossPct: sl,
    takeProfitPct: tp,
    feePct: 0.05,
    minConfidence: conf,
    entryOrderType: "limit",
    makerFeePct: 0.02,
  });
  console.log(
    `${label.padEnd(35)} | ret=${result.totalReturnPct.toFixed(2)}% win=${(result.winRate * 100).toFixed(1)}% trd=${result.totalTrades} dd=${result.maxDrawdownPct.toFixed(2)}% pf=${result.metrics.profitFactor.toFixed(2)} avgBars=${result.metrics.averageTradeDurationHours.toFixed(1)}`,
  );
}

run(1.2, 1.8, 0.55, 1, "cross conf 0.55 maxPos 1");
run(1.2, 1.8, 0.55, 3, "cross conf 0.55 maxPos 3");
run(1.2, 1.8, 0.55, 5, "cross conf 0.55 maxPos 5");
run(1.2, 1.8, 0.5, 1, "cross conf 0.5 maxPos 1");
run(1.2, 1.8, 0.5, 3, "cross conf 0.5 maxPos 3");
run(1.2, 1.8, 0.5, 5, "cross conf 0.5 maxPos 5");
