import { Database } from "bun:sqlite";
import { runBacktest } from "./src/scalping/backtest.ts";
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

function baseConfig() {
  return {
    ...defaultComposerConfig,
    thresholds: { ...defaultComposerConfig.thresholds },
  };
}

function run(cfg, sl, tp, conf, label) {
  const result = runBacktest({
    symbol: "BTC/USDT",
    exchange: "binance",
    timeframe: "5m",
    candles,
    composerConfig: cfg,
    initialCapital: 10000,
    positionSizePct: 100,
    stopLossPct: sl,
    takeProfitPct: tp,
    feePct: 0.05,
    minConfidence: conf,
    entryOrderType: "limit",
    entryLimitOffsetBps: 0,
    makerFeePct: 0.02,
  });
  if (result.totalTrades < 10) return;
  console.log(
    `${label.padEnd(45)} | ret=${result.totalReturnPct.toFixed(2)}% win=${(result.winRate * 100).toFixed(1)}% trd=${result.totalTrades} dd=${result.maxDrawdownPct.toFixed(2)}% pf=${result.metrics.profitFactor.toFixed(2)}`,
  );
}

// 1. EMA cross only
let cfg = baseConfig();
cfg.weights = {
  spread: 0,
  imbalance: 0,
  liquidity: 0,
  volatility: 0,
  trend: 1,
  rsi: 0,
  regime: 0,
};
cfg.thresholds.trendSignalStyle = "cross";
run(cfg, 1.2, 1.8, 0.3, "EMA cross only");

// 2. RSI mean-reversion only (buy oversold, sell overbought)
cfg = baseConfig();
cfg.weights = {
  spread: 0,
  imbalance: 0,
  liquidity: 0,
  volatility: 0,
  trend: 0,
  rsi: 1,
  regime: 0,
};
run(cfg, 1.2, 1.8, 0.3, "RSI mean-revert only");

// 3. RSI follow-trend only (buy when RSI > 50 in uptrend? no, need trend filter)
cfg = baseConfig();
cfg.weights = {
  spread: 0,
  imbalance: 0,
  liquidity: 0,
  volatility: 0,
  trend: 0,
  rsi: 1,
  regime: 0,
};
cfg.thresholds.rsiFollowTrend = true;
run(cfg, 1.2, 1.8, 0.3, "RSI follow-trend only");

// 4. Volatility only
cfg = baseConfig();
cfg.weights = {
  spread: 0,
  imbalance: 0,
  liquidity: 0,
  volatility: 1,
  trend: 0,
  rsi: 0,
  regime: 0,
};
run(cfg, 1.2, 1.8, 0.3, "Volatility only");

// 5. Regime only
cfg = baseConfig();
cfg.weights = {
  spread: 0,
  imbalance: 0,
  liquidity: 0,
  volatility: 0,
  trend: 0,
  rsi: 0,
  regime: 1,
};
run(cfg, 1.2, 1.8, 0.3, "Regime only");

// 6. EMA slope only
cfg = baseConfig();
cfg.weights = {
  spread: 0,
  imbalance: 0,
  liquidity: 0,
  volatility: 0,
  trend: 1,
  rsi: 0,
  regime: 0,
};
cfg.thresholds.trendSignalStyle = "slope";
run(cfg, 1.2, 1.8, 0.3, "EMA slope only");

// 7. EMA cross + RSI follow trend
cfg = baseConfig();
cfg.weights = {
  spread: 0,
  imbalance: 0,
  liquidity: 0,
  volatility: 0,
  trend: 0.6,
  rsi: 0.4,
  regime: 0,
};
cfg.thresholds.trendSignalStyle = "cross";
cfg.thresholds.rsiFollowTrend = true;
run(cfg, 1.2, 1.8, 0.5, "EMA cross + RSI follow-trend");

// 8. EMA slope + RSI follow trend
cfg = baseConfig();
cfg.weights = {
  spread: 0,
  imbalance: 0,
  liquidity: 0,
  volatility: 0,
  trend: 0.6,
  rsi: 0.4,
  regime: 0,
};
cfg.thresholds.trendSignalStyle = "slope";
cfg.thresholds.rsiFollowTrend = true;
run(cfg, 1.2, 1.8, 0.5, "EMA slope + RSI follow-trend");
