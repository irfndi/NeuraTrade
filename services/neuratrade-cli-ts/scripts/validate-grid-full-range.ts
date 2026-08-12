// Full-range grid validation for the VALIDATED_BTC_GRID_CANDIDATE over the
// longest stored history (bitget-futures BTC/USDT:USDT 15m, 2024-08..2026-08).
// The CLI `backtest --strategy-type grid` can't reproduce the candidate exactly
// (its --grid-max-grids is integer-only while the candidate uses 1.5), so this
// runs the grid engine directly with the exact locked config.
import { Database } from "bun:sqlite";
import { runGridBacktest } from "../src/scalping/grid.js";
import { VALIDATED_BTC_GRID_CANDIDATE } from "../src/scalping/grid-candidate.js";
import type { CandleLike } from "../src/scalping/types.js";

const HOME = process.env.NEURATRADE_HOME ?? `${process.env.HOME}/.neuratrade`;
const db = new Database(`${HOME}/data/neuratrade.db`, { readonly: true });
const c = VALIDATED_BTC_GRID_CANDIDATE;

const rows = db
  .query(
    `SELECT o.open_price as open, o.high_price as high, o.low_price as low,
            o.close_price as close, o.volume as volume, o.timestamp as ts
     FROM ohlcv_data o
     JOIN exchanges e ON e.id = o.exchange_id
     JOIN trading_pairs tp ON tp.id = o.trading_pair_id
     WHERE e.name = '${c.exchange}' AND tp.symbol = '${c.symbol}' AND o.timeframe = '${c.timeframe}'
     ORDER BY o.timestamp ASC`,
  )
  .all() as Array<{ open: number; high: number; low: number; close: number; volume: number; ts: string }>;
db.close();

const candles: CandleLike[] = rows.map((r) => ({
  open: r.open, high: r.high, low: r.low, close: r.close, volume: r.volume,
  timestamp: new Date(Date.parse(r.ts)),
}));
console.log(`Loaded ${candles.length} ${c.symbol} ${c.timeframe} candles: ${candles[0].timestamp.toISOString()} .. ${candles[candles.length - 1].timestamp.toISOString()}`);

const opts = {
  gridStepPct: c.gridStepPct,
  gridMaxGrids: c.gridMaxGrids,
  gridPauseAfterLossBars: c.gridPauseAfterLossBars,
  feePct: c.feePct,
  slippageBps: c.slippageBps,
  initialCapital: 10_000,
  trendFilterPeriod: c.trendFilterPeriod,
  leverage: c.leverage,
  onlyWithTrend: c.onlyWithTrend,
  targetRatio: c.targetRatio,
  chopGateAdxThreshold: c.chopGateAdx,
};

function run(label: string, src: readonly CandleLike[]) {
  const r = runGridBacktest(src, opts);
  console.log(
`[${label}] ret=${r.totalReturnPct.toFixed(2)}% trades=${r.totalTrades} win=${(r.winRate).toFixed(1)}% pf=${r.profitFactor.toFixed(2)} dd=${(r.maxDrawdownPct ?? 0).toFixed(2)}%`,
  );
}

// Full history.
console.log("\n=== FULL HISTORY ===");
run("FULL", candles);

// Disjoint ~60-day windows (5760 15m bars) across the whole span.
console.log("\n=== DISJOINT 5760-bar (~60d) WINDOWS ===");
const WINDOW = 5760;
let pos = 0, n = 0;
for (let start = 0; start + WINDOW <= candles.length; start += WINDOW) {
  const win = candles.slice(start, start + WINDOW);
  if (win.length < WINDOW) break;
  const first = win[0].close, last = win[win.length - 1].close;
  const trend = last >= first ? "UP" : "DOWN";
  const r = runGridBacktest(win, opts);
  const marker = r.totalReturnPct > 0 ? " win" : "";
  const winLabel = `${(r.winRate).toFixed(1)}`;
  console.log(`[w${n}:${first.toFixed(0)}->${last.toFixed(0)} (${trend})] ret=${r.totalReturnPct.toFixed(2)}% n=${r.totalTrades} win=${winLabel}% pf=${r.profitFactor.toFixed(2)} dd=${(r.maxDrawdownPct ?? 0).toFixed(2)}%${marker}`);
  if (r.totalReturnPct > 0) pos++;
  n++;
}
console.log(`\nProfitable windows: ${pos}/${n}`);
console.log("");