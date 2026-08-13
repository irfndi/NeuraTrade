import { Database } from "bun:sqlite";
import { validateGridEvidence } from "../src/scalping/grid-validation.js";

const home = process.env.NEURATRADE_HOME ?? `${process.env.HOME}/.neuratrade`;
const db = new Database(`${home}/data/neuratrade.db`, { readonly: true });
const rows = db
  .query(
    `SELECT o.open_price open, o.high_price high, o.low_price low, o.close_price close, o.volume volume, o.timestamp ts
     FROM ohlcv_data o JOIN exchanges e ON e.id=o.exchange_id JOIN trading_pairs p ON p.id=o.trading_pair_id
     WHERE e.name=? AND p.symbol=? AND o.timeframe=? ORDER BY o.timestamp ASC`,
  )
  .all("bitget-futures", "ETH/USDT:USDT", "15m") as Array<{
  open: number;
  high: number;
  low: number;
  close: number;
  volume: number;
  ts: string;
}>;
db.close();
const candles = rows.map((r) => ({
  open: r.open,
  high: r.high,
  low: r.low,
  close: r.close,
  volume: r.volume,
  timestamp: new Date(r.ts),
}));
const now = new Date(candles.at(-1)!.timestamp.getTime() + 15 * 60 * 1000);
const cands = [
  { label: "cfg1 grids2 pause48 t4 adx28", g: { gridStepPct: 0.75, gridMaxGrids: 2, gridPauseAfterLossBars: 48, targetRatio: 4, chopGateAdxThreshold: 28 } },
  { label: "cfg2 grids3 pause0 t3 adx20", g: { gridStepPct: 0.75, gridMaxGrids: 3, gridPauseAfterLossBars: 0, targetRatio: 3, chopGateAdxThreshold: 20 } },
];
for (const c of cands) {
  const r = validateGridEvidence(candles, {
    now,
    timeframeMinutes: 15,
    grid: {
      ...c.g,
      feePct: 0.02,
      slippageBps: 1,
      initialCapital: 100,
      trendFilterPeriod: 0,
      leverage: 1,
      positionFraction: 0.5,
      onlyWithTrend: false,
    },
    executionParityPassed: true,
  });
  if (r.kind !== "ok") {
    console.log(c.label, "INVALID", r.failures);
    continue;
  }
  const h = r.historical, g = r.confidence, s = r.stress;
  console.log(c.label);
  console.log(
    `  windows=${h.windows.length} win%=${h.profitableWindowPct.toFixed(2)} ret=${h.compoundedReturnPct.toFixed(2)} dd=${h.maximumDrawdownPct.toFixed(2)} totalTrades=${h.totalTrades}`,
  );
  console.log(
    `  oosTrades=${r.fixedOos.totalTrades} confLB=${g.lowerBoundPct.toFixed(6)} stressWorst=${s.worstReturnPct.toFixed(4)} stressLB=${s.pooledLowerBoundPct.toFixed(6)}`,
  );
  const pass =
    h.profitableWindowPct >= 50 &&
    h.compoundedReturnPct > 0 &&
    h.maximumDrawdownPct <= 15 &&
    r.fixedOos.totalTrades >= 30 &&
    g.lowerBoundPct >= 0 &&
    s.worstReturnPct >= 0 &&
    s.pooledLowerBoundPct >= 0 &&
    h.windows.length >= 10;
  console.log(`  HISTORICAL GATE: ${pass ? "PASS" : "FAIL"}`);
}
