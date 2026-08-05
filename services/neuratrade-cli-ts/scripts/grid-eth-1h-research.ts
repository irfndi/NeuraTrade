import { Database } from "bun:sqlite";
import { runGridBacktest, type GridOptions } from "../src/scalping/grid.ts";

const home = process.env.NEURATRADE_HOME || "/tmp/neuratrade-paper-futures-eth";
const db = new Database(`${home}/data/neuratrade.db`);
const rows = db
  .query(
    "SELECT open_price as open, high_price as high, low_price as low, close_price as close, volume, timestamp FROM ohlcv_data WHERE exchange_id = (SELECT id FROM exchanges WHERE name = ?) AND trading_pair_id = (SELECT id FROM trading_pairs WHERE symbol = ?) AND timeframe = ? ORDER BY timestamp ASC",
  )
  .all("bitget-futures", "ETH/USDT:USDT", "1h") as {
  open: number;
  high: number;
  low: number;
  close: number;
  volume: number;
  timestamp: string;
}[];

const candles = rows.map((r) => ({
  open: r.open,
  high: r.high,
  low: r.low,
  close: r.close,
  volume: r.volume,
  timestamp: new Date(r.timestamp),
}));

console.log(`Loaded ${candles.length} candles`);

for (const leverage of [1, 2, 3]) {
  for (const step of [0.5, 0.7, 1.0, 1.5, 2.0]) {
    for (const maxGrids of [1, 1.5, 2]) {
      const opts: GridOptions = {
        gridStepPct: step,
        gridMaxGrids: maxGrids,
        gridPauseAfterLossBars: 0,
        feePct: 0.2,
        slippageBps: 5,
        initialCapital: 20,
        trendFilterPeriod: 50,
        leverage,
      };
      const result = runGridBacktest(candles, opts);
      if (result.totalTrades > 0) {
        console.log(
          `lev=${leverage} step=${step}% maxGrids=${maxGrids} -> return=${result.totalReturnPct.toFixed(2)}% trades=${result.totalTrades} win=${result.winRate.toFixed(1)}% dd=${result.maxDrawdownPct.toFixed(2)}% pf=${result.profitFactor.toFixed(3)}`,
        );
      }
    }
  }
}
