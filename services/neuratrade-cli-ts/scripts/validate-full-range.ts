// Full-range validation for bd NeuraTrade-9m8p (regime filters) + the other
// parallel tracks. Reads ALL stored Binance BTC/USDT 1h candles from the DB
// (2021-2026, ~49.5k candles) and compares:
//   baseline (default composer) vs no-regime vs regime sweep candidates
// across the full history AND disjoint windows, so the regime claim is
// validated "as long as we can", not just on 1000 live-fetched candles.
import { Database } from "bun:sqlite";
import {
  composerSweepCandidate,
  runBacktest,
  sweepComposerConfigs,
} from "../src/scalping/backtest.js";
import { defaultComposerConfig } from "../src/scalping/composer.js";
import type { CandleLike } from "../src/scalping/types.js";

const HOME = process.env.NEURATRADE_HOME ?? `${process.env.HOME}/.neuratrade`;
const db = new Database(`${HOME}/data/neuratrade.db`, { readonly: true });

const rows = db
  .query(
    `SELECT o.open_price as open, o.high_price as high, o.low_price as low,
            o.close_price as close, o.volume as volume, o.timestamp as ts
     FROM ohlcv_data o
     JOIN exchanges e ON e.id = o.exchange_id
     JOIN trading_pairs tp ON tp.id = o.trading_pair_id
     WHERE e.name = 'binance' AND tp.symbol = 'BTC/USDT' AND o.timeframe = '1h'
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

const candles: CandleLike[] = rows.map((r) => ({
  open: r.open,
  high: r.high,
  low: r.low,
  close: r.close,
  volume: r.volume,
  timestamp: new Date(Date.parse(r.ts)),
}));
db.close();

console.log(
  `Loaded ${candles.length} BTC/USDT 1h candles: ${candles[0].timestamp.toISOString()} .. ${candles[candles.length - 1].timestamp.toISOString()}`,
);

// Execution params taken from the committed verify-regime-btc.ts.
const base = {
  symbol: "BTC/USDT",
  exchange: "binance",
  timeframe: "1h",
  candles: [] as readonly CandleLike[],
  initialCapital: 10_000,
  positionSizePct: 100,
  stopLossPct: 1,
  takeProfitPct: 2,
  feePct: 0.06,
  minConfidence: 0.35,
  slippageBps: 2,
  leverage: 1,
  isFutures: true,
  maxBarsInTrade: 12,
  htfCandles: [],
  recordEquityCurve: false,
};

const noRegimeConfig = {
  ...defaultComposerConfig,
  weights: {
    ...defaultComposerConfig.weights,
    regime: 0,
    spread: 0.18,
    imbalance: 0.22,
    volatility: 0.13,
    trend: 0.18,
    liquidity: 0.09,
    rsi: 0.09,
    funding: 0,
    rsiPullback: 0,
    emaPullback: 0,
    connorsRsi2: 0,
  },
  enabled: { ...defaultComposerConfig.enabled, regime: false },
};

const candidates = [
  composerSweepCandidate("regime-tight", {
    adxWeakTrend: 25,
    bollingerEntryMinPct: 0.4,
    bollingerEntryMaxPct: 0.6,
    weights: {
      regime: 0.25,
      trend: 0.14,
      volatility: 0.1,
      rsi: 0.07,
      spread: 0.18,
      imbalance: 0.22,
      liquidity: 0.09,
    },
  }),
  composerSweepCandidate("regime-atr-cap", {
    adxWeakTrend: 25,
    atrMaxPctOfPrice: 0.03,
    bollingerEntryMinPct: 0.35,
    bollingerEntryMaxPct: 0.65,
    weights: {
      regime: 0.3,
      trend: 0.14,
      volatility: 0.1,
      rsi: 0.07,
      spread: 0.15,
      imbalance: 0.15,
      liquidity: 0.09,
    },
  }),
];

function evaluate(src: readonly CandleLike[], label: string): void {
  const bl = runBacktest({ ...base, candles: src, composerConfig: defaultComposerConfig });
  const nr = runBacktest({ ...base, candles: src, composerConfig: noRegimeConfig });
  const ranked = sweepComposerConfigs({ ...base, candles: src }, candidates, defaultComposerConfig);
  const best = ranked[0];
  const ret = (x: number) => x.toFixed(2);
  console.log(
    `[${label}] baseline r=${ret(bl.totalReturnPct)}% sh=${bl.sharpeRatio.toFixed(2)} dd=${ret(bl.maxDrawdownPct)}% n=${bl.totalTrades}`,
  );
  console.log(
    `         no-regime r=${ret(nr.totalReturnPct)}% sh=${nr.sharpeRatio.toFixed(2)} dd=${ret(nr.maxDrawdownPct)}% n=${nr.totalTrades}`,
  );
  for (const r of ranked) {
    console.log(
      `         ${r.name} r=${ret(r.totalReturnPct)}% sh=${r.sharpeRatio.toFixed(2)} dd=${ret(r.maxDrawdownPct)}% n=${r.totalTrades}`,
    );
  }
  // net regime-vs-no-regime on the best candidate
  const ddGain = nr.maxDrawdownPct - best.maxDrawdownPct;
  const shGain = best.sharpeRatio - nr.sharpeRatio;
  console.log(`         regime-vs-no-regime: ddGain=${ddGain.toFixed(2)}pp sharpeGain=${shGain.toFixed(2)}`);
}

// 1) Full history.
console.log("\n=== FULL HISTORY (2021-2026) ===");
evaluate(candles, "FULL");

// 2) Disjoint 2000h (~83 day) windows across the whole span, plus a label of
//    the market regime trend (first vs last close).
console.log("\n=== DISJOINT 2000h WINDOWS ===");
const WINDOW = 2000;
let shSum = 0;
let ddSum = 0;
let nWin = 0;
for (let start = 0; start + WINDOW <= candles.length; start += WINDOW) {
  const win = candles.slice(start, start + WINDOW);
  if (win.length < WINDOW) break;
  const first = win[0].close;
  const last = win[win.length - 1].close;
  const trend = last >= first ? "UP" : "DOWN";
  const label = `w${nWin}:${first.toFixed(0)}->${last.toFixed(0)} (${trend})`;
  const r0 = runBacktest({ ...base, candles: win, composerConfig: defaultComposerConfig });
  const nr = runBacktest({ ...base, candles: win, composerConfig: noRegimeConfig });
  const ranked = sweepComposerConfigs({ ...base, candles: win }, candidates, defaultComposerConfig);
  const best = ranked[0];
  const ddGain = nr.maxDrawdownPct - best.maxDrawdownPct;
  const shGain = best.sharpeRatio - nr.sharpeRatio;
  shSum += shGain;
  ddSum += ddGain;
  const marker = ddGain > 0 && shGain > 0 ? "  <-- regime wins" : "";
  console.log(
    `[${label}] baseline sh=${r0.sharpeRatio.toFixed(2)} | noRegime sh=${nr.sharpeRatio.toFixed(2)} dd=${nr.maxDrawdownPct.toFixed(2)} | best(${best.name}) sh=${best.sharpeRatio.toFixed(2)} dd=${best.maxDrawdownPct.toFixed(2)} | ddGain=${ddGain.toFixed(2)}pp${marker}`,
  );
  nWin++;
}
if (nWin > 0) {
  console.log(`\nAvg regime ddGain over ${nWin} windows: ${(ddSum / nWin).toFixed(2)}pp; avg sharpeGain: ${(shSum / nWin).toFixed(3)}`);
}
console.log("");