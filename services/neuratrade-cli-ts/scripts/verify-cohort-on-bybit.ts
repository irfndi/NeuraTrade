// Verify the current readiness cohort candidates (BTC/SOL/ETH, fitted on
// bitget-futures) against CLEAN Bybit mainnet 15m data using the SAME gate
// (validateGridEvidence). Answers the exchange-alignment question: are the
// bitget-fitted candidates also Bybit-valid, or do they need re-fitting for
// the Bybit live engine (clever-cabin-fyv)?
import { Database } from "bun:sqlite";
import { validateGridEvidence } from "../src/scalping/grid-validation.js";

const home = process.env.NEURATRADE_HOME ?? `${process.env.HOME}/.neuratrade`;
const db = new Database(`${home}/data/neuratrade.db`, { readonly: true });

const candidates = [
  {
    symbol: "BTC/USDT:USDT",
    label: "BTC step0.5 g3 p48 t2 adx15",
    g: {
      gridStepPct: 0.5,
      gridMaxGrids: 3,
      gridPauseAfterLossBars: 48,
      targetRatio: 2,
      chopGateAdxThreshold: 15,
    },
  },
  {
    symbol: "SOL/USDT:USDT",
    label: "SOL step1.25 g2 p36 t4 adx26",
    g: {
      gridStepPct: 1.25,
      gridMaxGrids: 2,
      gridPauseAfterLossBars: 36,
      targetRatio: 4,
      chopGateAdxThreshold: 26,
    },
  },
  {
    symbol: "ETH/USDT:USDT",
    label: "ETH step0.75 g2 p48 t4 adx28",
    g: {
      gridStepPct: 0.75,
      gridMaxGrids: 2,
      gridPauseAfterLossBars: 48,
      targetRatio: 4,
      chopGateAdxThreshold: 28,
    },
  },
];

for (const c of candidates) {
  const rows = db
    .query(
      `SELECT o.open_price open, o.high_price high, o.low_price low, o.close_price close, o.volume volume, o.timestamp ts
       FROM ohlcv_data o JOIN exchanges e ON e.id=o.exchange_id JOIN trading_pairs p ON p.id=o.trading_pair_id
       WHERE e.name='bybit-futures' AND p.symbol=? AND o.timeframe='15m' ORDER BY o.timestamp ASC`,
    )
    .all(c.symbol) as Array<{
    open: number;
    high: number;
    low: number;
    close: number;
    volume: number;
    ts: string;
  }>;
  const candles = rows.map((r) => ({
    open: r.open,
    high: r.high,
    low: r.low,
    close: r.close,
    volume: r.volume,
    timestamp: new Date(r.ts),
  }));
  if (candles.length < 55000) {
    console.log(`${c.symbol}: SKIP (only ${candles.length} candles)`);
    continue;
  }
  const now = new Date(candles.at(-1)!.timestamp.getTime() + 15 * 60 * 1000);
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
    console.log(`${c.symbol}: INVALID -> ${r.failures.join("; ")}`);
    continue;
  }
  const h = r.historical,
    g = r.confidence,
    s = r.stress;
  const pass =
    h.profitableWindowPct >= 50 &&
    h.compoundedReturnPct > 0 &&
    h.maximumDrawdownPct <= 15 &&
    r.fixedOos.totalTrades >= 30 &&
    g.lowerBoundPct >= 0 &&
    s.worstReturnPct >= 0 &&
    s.pooledLowerBoundPct >= 0 &&
    h.windows.length >= 10;
  console.log(
    `${c.label}: ${pass ? "PASS" : "FAIL"} | win=${h.profitableWindowPct.toFixed(1)}% ret=${h.compoundedReturnPct.toFixed(2)}% dd=${h.maximumDrawdownPct.toFixed(2)}% oos=${r.fixedOos.totalTrades} confLB=${g.lowerBoundPct.toFixed(5)} stressLB=${s.pooledLowerBoundPct.toFixed(5)} windows=${h.windows.length}`,
  );
}
db.close();
