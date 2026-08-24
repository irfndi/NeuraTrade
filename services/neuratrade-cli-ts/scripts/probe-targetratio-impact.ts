// Ponytail probe: does targetRatio (R:R) flip ladder configs from bleed to
// profit on mainnet 15m data? Same candles the funnel's db-mainnet path uses.
import { Database } from "bun:sqlite";
import { resampleCandles } from "../src/scalping/grid-universe.js";
import { runLadderGridBacktest } from "../src/scalping/ladder-grid.js";
import type { Candle } from "../src/market-data/types.js";

const HOME = process.env.NEURATRADE_HOME ?? `${process.env.HOME}/.neuratrade`;
const db = new Database(`${HOME}/data/neuratrade.db`, { readonly: true });

const SYMBOLS = [
  "NEAR/USDT:USDT",
  "LINK/USDT:USDT",
  "ADA/USDT:USDT",
  "BTC/USDT:USDT",
  "SOL/USDT:USDT",
];

const candles5m = (symbol: string) =>
  db
    .query(
      `SELECT o.open_price as open, o.high_price as high, o.low_price as low,
              o.close_price as close, o.volume as volume, o.timestamp as ts
       FROM ohlcv_data o
       JOIN exchanges e ON e.id=o.exchange_id
       JOIN trading_pairs tp ON tp.id=o.trading_pair_id
       WHERE e.name='bybit-futures' AND tp.symbol=? AND o.timeframe='5m'
       ORDER BY o.timestamp ASC`,
    )
    .all(symbol) as Array<Record<string, unknown>>;

const toCandle = (symbol: string) => (r: Record<string, unknown>): Candle => ({
  open: Number(r.open),
  high: Number(r.high),
  low: Number(r.low),
  close: Number(r.close),
  volume: Number(r.volume),
  exchange: "bybit-futures",
  symbol,
  timeframe: "5m",
  timestamp: new Date(String(r.ts)),
});

for (const symbol of SYMBOLS) {
  const raw = candles5m(symbol).map(toCandle(symbol));
  const c15 = resampleCandles(raw, 15, "15m");
  const tail = c15.slice(-6000); // ~62 days recent regime
  const parts = [symbol.padEnd(18)];
  for (const targetRatio of [1, 3, 4]) {
    for (const adx of [0, 12]) {
      const r = runLadderGridBacktest(tail, {
        rungs: 2,
        gridStepPct: 0.5,
        gridMaxGrids: 3,
        gridPauseAfterLossBars: 0,
        feePct: 0.06,
        slippageBps: 2,
        initialCapital: 10000,
        leverage: 1,
        trendFilterPeriod: 0,
        chopGateAdxThreshold: adx,
        targetRatio,
        conservativeIntrabar: true,
      });
      parts.push(
        `tr=${targetRatio},adx=${adx}: ${r.totalReturnPct.toFixed(1)}% (${r.totalTrades}t)`,
      );
    }
  }
  console.log(parts.join(" | "));
}
