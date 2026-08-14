// Ladder-universe validation sweep: run the multi-level ladder grid walk-forward
// over every bybit-futures 15m symbol in the mainnet DB and report OOS pass/fail
// against the SAME gates the universe scan uses (profitableWindowsPct >= 60,
// aggregateReturn > 0). Goal: confirm the ladder engine surfaces survivors where
// the single-position engine finds none.
//
// Usage:
//   bun run scripts/ladder-universe-sweep.ts [--min-candles=1000]
import { Database } from "bun:sqlite";
import { runLadderGridWalkForward } from "../src/scalping/ladder-grid.js";

const HOME = process.env.NEURATRADE_HOME ?? `${process.env.HOME}/.neuratrade`;
const arg = (flag: string, fallback: string) =>
  process.argv.find((a) => a.startsWith(flag))?.slice(flag.length) ?? fallback;
const minCandles = Number(arg("--min-candles=", "0"));
// Bound history to the LAST N candles (default 2000 ≈ 3 weeks). The
// walk-forward compounds window returns, so running over the full 2-year
// history (69k candles ≈ 1148 windows) inflates aggregateReturnPct to
// non-physical values (e+34%). A bounded tail keeps returns interpretable
// while profitableWindowsPct stays comparable. Use --tail=0 for full history.
const tail = Number(arg("--tail=", "2000"));

const db = new Database(`${HOME}/data/neuratrade.db`, { readonly: true });
const symbols = (
  db
    .query(
      `SELECT tp.symbol FROM ohlcv_data o
       JOIN exchanges e ON e.id=o.exchange_id
       JOIN trading_pairs tp ON tp.id=o.trading_pair_id
       WHERE e.name='bybit-futures' AND o.timeframe='15m'
       GROUP BY tp.symbol HAVING COUNT(*) >= ?
       ORDER BY tp.symbol ASC`,
    )
    .all(minCandles) as Array<{ symbol: string }>
).map((r) => r.symbol);

const getCandles = (symbol: string) =>
  db
    .query(
      `SELECT o.open_price as open, o.high_price as high, o.low_price as low,
              o.close_price as close, o.volume as volume, o.timestamp as ts
       FROM ohlcv_data o JOIN exchanges e ON e.id=o.exchange_id
       JOIN trading_pairs tp ON tp.id=o.trading_pair_id
       WHERE e.name='bybit-futures' AND tp.symbol=? AND o.timeframe='15m'
       ORDER BY o.timestamp ASC`,
    )
    .all(symbol) as Array<{
    open: number;
    high: number;
    low: number;
    close: number;
    volume: number;
    ts: string;
  }>;

const searchSpace = {
  rungs: [1, 2, 3],
  gridStepPct: [1.0, 1.25, 1.5, 2.0],
  gridMaxGrids: [2, 3],
  gridPauseAfterLossBars: [0],
};

const baseOptions = {
  feePct: 0.06,
  slippageBps: 2,
  leverage: 1,
  trendFilterPeriod: 0,
  targetRatio: 1,
};

const results: Array<{
  symbol: string;
  candles: number;
  aggregateReturnPct: number;
  profitableWindowsPct: number;
  totalTrades: number;
  maxDrawdownPct: number;
  passed: boolean;
  best: { rungs: number; gridStepPct: number; gridMaxGrids: number };
}> = [];

const started = Date.now();
for (const symbol of symbols) {
  const rows = getCandles(symbol);
  if (rows.length === 0) continue;
  const mapped = rows.map((r) => ({
    open: r.open,
    high: r.high,
    low: r.low,
    close: r.close,
    volume: r.volume,
    timestamp: new Date(r.ts),
  }));
  const candles = tail > 0 ? mapped.slice(-tail) : mapped;
  const wf = runLadderGridWalkForward(candles, {
    trainWindow: 180,
    testWindow: 60,
    initialCapital: 10000,
    searchSpace,
    baseOptions,
  });
  const last = wf.windows[wf.windows.length - 1];
  const passed =
    wf.windows.length >= 1 &&
    wf.profitableWindowsPct >= 60 &&
    wf.aggregateReturnPct > 0;
  results.push({
    symbol,
    candles: candles.length,
    aggregateReturnPct: Number(wf.aggregateReturnPct.toFixed(2)),
    profitableWindowsPct: Number(wf.profitableWindowsPct.toFixed(1)),
    totalTrades: wf.totalTrades,
    maxDrawdownPct: Number(wf.maxDrawdownPct.toFixed(2)),
    passed,
    best: {
      rungs: (last?.params as { rungs?: number })?.rungs ?? 0,
      gridStepPct: (last?.params as { gridStepPct?: number })?.gridStepPct ?? 0,
      gridMaxGrids:
        (last?.params as { gridMaxGrids?: number })?.gridMaxGrids ?? 0,
    },
  });
}
db.close();

const passed = results.filter((r) => r.passed);
const sorted = [...results].sort(
  (a, b) => b.aggregateReturnPct - a.aggregateReturnPct,
);
console.log(
  `\nLadder universe sweep: ${results.length} symbols in ${(
    (Date.now() - started) /
    1000
  ).toFixed(1)}s — ${passed.length} PASS\n`,
);
console.log(
  [
    "symbol",
    "candles",
    "ret%",
    "profWin%",
    "trades",
    "dd%",
    "best(rungs/step/grids)",
    "PASS",
  ].join("\t"),
);
for (const r of sorted) {
  console.log(
    [
      r.symbol,
      r.candles,
      r.aggregateReturnPct,
      r.profitableWindowsPct,
      r.totalTrades,
      r.maxDrawdownPct,
      `${r.best.rungs}/${r.best.gridStepPct}/${r.best.gridMaxGrids}`,
      r.passed ? "PASS" : "",
    ].join("\t"),
  );
}
