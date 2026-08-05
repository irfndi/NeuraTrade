#!/usr/bin/env bun
/* Debug: reproduce the sweep result vs the CLI-resolved result for the same profile. */
import { Database } from "bun:sqlite";
import { join } from "node:path";
import { runBacktest } from "../src/scalping/backtest.js";
import { defaultComposerConfig } from "../src/scalping/composer.js";
import type { Candle, FundingRate } from "../src/market-data/types.js";

const home =
  process.env.NEURATRADE_HOME ?? join(process.env.HOME!, ".neuratrade");
const db = new Database(join(home, "data", "neuratrade.db"), {
  readonly: true,
});
const rows = db
  .query(
    `SELECT o.open_price, o.high_price, o.low_price, o.close_price, o.volume, o.timestamp
     FROM ohlcv_data o JOIN exchanges e ON e.id=o.exchange_id JOIN trading_pairs tp ON tp.id=o.trading_pair_id
     WHERE e.name='bitget-futures' AND tp.symbol='BTC/USDT:USDT' AND o.timeframe='15m' ORDER BY o.timestamp ASC`,
  )
  .all() as Array<{
  open_price: number;
  high_price: number;
  low_price: number;
  close_price: number;
  volume: number;
  timestamp: string;
}>;
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
const frows = db
  .query(
    `SELECT funding_rate, timestamp FROM funding_rates WHERE exchange='bitget-futures' AND symbol='BTC/USDT:USDT' ORDER BY timestamp ASC`,
  )
  .all() as Array<{ funding_rate: number; timestamp: string }>;
const fundingRates: FundingRate[] = frows.map((r) => ({
  exchange: "bitget-futures",
  symbol: "BTC/USDT:USDT",
  fundingRate: r.funding_rate,
  timestamp: new Date(
    r.timestamp.endsWith("Z")
      ? r.timestamp
      : r.timestamp.replace(" ", "T") + "Z",
  ),
}));
db.close();

const composerConfig = {
  weights: defaultComposerConfig.weights,
  thresholds: {
    ...defaultComposerConfig.thresholds,
    regimeMode: "reversion" as const,
  },
};

const base = {
  symbol: "BTC/USDT:USDT",
  exchange: "bitget-futures",
  timeframe: "15m",
  candles,
  composerConfig,
  initialCapital: 10000,
  positionSizePct: 100,
  stopLossPct: 0,
  takeProfitPct: 0,
  feePct: 0.06,
  minConfidence: 0.45,
  useAtrStops: true,
  atrStopMultiplier: 1,
  atrTakeProfitMultiplier: 2,
  isFutures: true,
  slippageBps: 2,
  leverage: 1,
  maxBarsInTrade: 12,
  recordEquityCurve: false,
  htfCandles: [],
};

console.log("== A: sweep-style (maker limit + real funding series)");
const a = runBacktest({
  ...base,
  makerFeePct: 0.02,
  entryOrderType: "limit",
  entryLimitOffsetBps: 0,
  fundingRates,
});
console.log(
  `FULL: trades ${a.totalTrades} win ${(a.winRate * 100).toFixed(1)}% PF ${a.metrics.profitFactor.toFixed(2)} exp ${a.metrics.expectancy.toFixed(4)} ret ${a.totalReturnPct.toFixed(2)}%`,
);

console.log("== A2: same but oosPct 20 — reconcile IS/OOS vs FULL");
const a2 = runBacktest({
  ...base,
  makerFeePct: 0.02,
  entryOrderType: "limit",
  entryLimitOffsetBps: 0,
  fundingRates,
  oosPct: 20,
});
const o2 = a2.oosResult!;
console.log(
  `IS: trades ${a2.totalTrades} win ${(a2.winRate * 100).toFixed(1)}% PF ${a2.metrics.profitFactor.toFixed(2)} ret ${a2.totalReturnPct.toFixed(2)}%`,
);
console.log(
  `OOS: trades ${o2.totalTrades} win ${(o2.winRate * 100).toFixed(1)}% PF ${o2.metrics.profitFactor.toFixed(2)} ret ${o2.totalReturnPct.toFixed(2)}%`,
);

console.log("== B1: A + fundingRatePct 0.01 (funding array still present)");
const b1 = runBacktest({
  ...base,
  makerFeePct: 0.02,
  entryOrderType: "limit",
  entryLimitOffsetBps: 0,
  fundingRates,
  fundingRatePct: 0.01,
});
console.log(
  `trades ${b1.totalTrades} win ${(b1.winRate * 100).toFixed(1)}% PF ${b1.metrics.profitFactor.toFixed(2)} exp ${b1.metrics.expectancy.toFixed(4)} ret ${b1.totalReturnPct.toFixed(2)}%`,
);

console.log("== B2: A + oosPct 20 (no fundingRatePct)");
const b2 = runBacktest({
  ...base,
  makerFeePct: 0.02,
  entryOrderType: "limit",
  entryLimitOffsetBps: 0,
  fundingRates,
  oosPct: 20,
});
console.log(
  `trades ${b2.totalTrades} win ${(b2.winRate * 100).toFixed(1)}% PF ${b2.metrics.profitFactor.toFixed(2)} exp ${b2.metrics.expectancy.toFixed(4)} ret ${b2.totalReturnPct.toFixed(2)}%`,
);

console.log("== B3: A minus maker options (market entry, keep funding array)");
const b3 = runBacktest({ ...base, fundingRates });
console.log(
  `trades ${b3.totalTrades} win ${(b3.winRate * 100).toFixed(1)}% PF ${b3.metrics.profitFactor.toFixed(2)} exp ${b3.metrics.expectancy.toFixed(4)} ret ${b3.totalReturnPct.toFixed(2)}%`,
);
