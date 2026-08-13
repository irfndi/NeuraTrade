// Walk-forward robustness check for a candidate grid config.
// Usage: CONFIG='{"gridStepPct":0.5,"gridMaxGrids":2,"gridPauseAfterLossBars":0,"targetRatio":2,"chopGateAdx":15,"leverage":1,"positionFraction":1}' \
//   bun run scripts/validate-grid-config.ts
// Reports full-history metrics + disjoint ~60d window win-rate (how many
// windows are profitable) so an in-sample winner isn't trusted if it doesn't
// generalize across time.
import { Database } from "bun:sqlite";
import { runGridBacktest } from "../src/scalping/grid.js";
import type { GridOptions } from "../src/scalping/grid.js";
import type { CandleLike } from "../src/scalping/types.js";

const HOME = process.env.NEURATRADE_HOME ?? `${process.env.HOME}/.neuratrade`;
const db = new Database(`${HOME}/data/neuratrade.db`, { readonly: true });
const rows = db
  .query(
    `SELECT o.open_price as open, o.high_price as high, o.low_price as low,
            o.close_price as close, o.volume as volume, o.timestamp as ts
     FROM ohlcv_data o JOIN exchanges e ON e.id=o.exchange_id
     JOIN trading_pairs tp ON tp.id=o.trading_pair_id
     WHERE e.name='bitget-futures' AND tp.symbol='BTC/USDT:USDT' AND o.timeframe='15m'
     ORDER BY o.timestamp ASC`,
  )
  .all() as Array<{
  open: number;
  high: number;
  low: number;
  close: number;
  volume: number;
  ts: string;
}>;
db.close();
const candles: CandleLike[] = rows.map((r) => ({
  open: r.open,
  high: r.high,
  low: r.low,
  close: r.close,
  volume: r.volume,
  timestamp: new Date(Date.parse(r.ts)),
}));

const cfg = JSON.parse(process.env.CONFIG ?? "{}");
const opts: GridOptions = {
  gridStepPct: cfg.gridStepPct ?? 0.5,
  gridMaxGrids: cfg.gridMaxGrids ?? 2,
  gridPauseAfterLossBars: cfg.gridPauseAfterLossBars ?? 0,
  feePct: 0.02,
  slippageBps: 1,
  initialCapital: 10_000,
  trendFilterPeriod: 0,
  leverage: cfg.leverage ?? 1,
  onlyWithTrend: false,
  targetRatio: cfg.targetRatio ?? 2,
  chopGateAdxThreshold: cfg.chopGateAdx ?? 15,
  positionFraction: cfg.positionFraction ?? 1,
};

const fmt = (x: number | undefined) => (x === undefined ? "-" : x.toFixed(2));

const full = runGridBacktest(candles, opts);
console.log(
  `FULL ret=${fmt(full.totalReturnPct)}% dd=${fmt(full.maxDrawdownPct)}% n=${full.totalTrades} win=${full.winRate.toFixed(1)}% pf=${full.profitFactor.toFixed(2)}`,
);

// Disjoint ~60-day windows (5760 15m bars).
const WINDOW = 5760;
let pos = 0,
  n = 0,
  sumRet = 0,
  maxWinDD = 0;
for (let start = 0; start + WINDOW <= candles.length; start += WINDOW) {
  const win = candles.slice(start, start + WINDOW);
  if (win.length < WINDOW) break;
  const r = runGridBacktest(win, opts);
  sumRet += r.totalReturnPct;
  maxWinDD = Math.max(maxWinDD, r.maxDrawdownPct);
  if (r.totalReturnPct > 0) pos++;
  const trend = win[win.length - 1].close >= win[0].close ? "UP" : "DOWN";
  console.log(
    `  w${n} (${trend}) ret=${fmt(r.totalReturnPct)}% dd=${fmt(r.maxDrawdownPct)}% n=${r.totalTrades} pf=${r.profitFactor.toFixed(2)}`,
  );
  n++;
}
console.log(
  `WINDOWS profitable=${pos}/${n} avgRet=${(sumRet / n).toFixed(2)}% maxWinDD=${fmt(maxWinDD)}%`,
);
