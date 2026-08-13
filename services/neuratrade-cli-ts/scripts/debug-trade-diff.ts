#!/usr/bin/env bun
/* Diff FULL-run IS-region trades vs IS-run trades (real BTC 15m data). */
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
  makerFeePct: 0.02,
  entryOrderType: "limit" as const,
  entryLimitOffsetBps: 0,
  fundingRates,
};

const full = runBacktest(base);
const split = runBacktest({ ...base, oosPct: 20 });
const isRun = split;
const oosRun = split.oosResult!;

const cutTime = candles[Math.floor(candles.length * 0.8)].timestamp.getTime();
const fullIs = full.trades.filter((t) => t.entryTime.getTime() < cutTime);
const fullOos = full.trades.filter((t) => t.entryTime.getTime() >= cutTime);
console.log(
  `FULL: ${full.totalTrades} trades ret ${full.totalReturnPct.toFixed(2)}% (IS-region entries ${fullIs.length}, OOS-region entries ${fullOos.length})`,
);
console.log(
  `IS-run: ${isRun.totalTrades} trades ret ${isRun.totalReturnPct.toFixed(2)}%`,
);
console.log(
  `OOS-run: ${oosRun.totalTrades} trades ret ${oosRun.totalReturnPct.toFixed(2)}%`,
);

const sum = (ts: readonly { pnlPct: number }[]) =>
  ts.reduce((a, t) => a + t.pnlPct, 0);
console.log(
  `FULL IS-region sum pnl%: ${sum(fullIs).toFixed(2)} | IS-run sum pnl%: ${sum(isRun.trades).toFixed(2)}`,
);
console.log(
  `FULL OOS-region sum pnl%: ${sum(fullOos).toFixed(2)} | OOS-run sum pnl%: ${sum(oosRun.trades).toFixed(2)}`,
);

// entry-time diff in the IS region
const isTimes = new Set(isRun.trades.map((t) => t.entryTime.getTime()));
const fullTimes = new Set(fullIs.map((t) => t.entryTime.getTime()));
const onlyFull = fullIs.filter((t) => !isTimes.has(t.entryTime.getTime()));
const onlyIs = isRun.trades.filter(
  (t) => !fullTimes.has(t.entryTime.getTime()),
);
console.log(
  `IS-region entries only in FULL: ${onlyFull.length}, only in IS-run: ${onlyIs.length}`,
);
for (const t of onlyFull.slice(0, 6))
  console.log(
    `  onlyFULL ${t.side} ${t.entryTime.toISOString()} pnl ${t.pnlPct.toFixed(2)}`,
  );
for (const t of onlyIs.slice(0, 6))
  console.log(
    `  onlyIS   ${t.side} ${t.entryTime.toISOString()} pnl ${t.pnlPct.toFixed(2)}`,
  );
