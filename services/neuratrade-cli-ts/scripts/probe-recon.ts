import { runBacktest } from "../src/scalping/backtest.js";
import { defaultComposerConfig } from "../src/scalping/composer.js";
import type { CandleLike } from "../src/scalping/types.js";

function candle(close: number, index: number, spread = 0.01, volume = 100): CandleLike {
  const open = close * (1 - spread / 2);
  const high = close * (1 + spread / 2);
  const low = close * (1 - spread / 1.5);
  return { open, high, low, close, volume, timestamp: new Date(1_700_000_000_000 + index * 300_000) };
}
function makeSeries(length: number, shiftAt: number, seed = 42): CandleLike[] {
  let state = seed;
  const rand = () => { state = (state * 1103515245 + 12345) % 2147483648; return state / 2147483648 - 0.5; };
  const candles: CandleLike[] = [];
  let price = 100;
  for (let i = 0; i < length; i++) {
    const vol = i >= shiftAt ? 0.035 : 0.008;
    price = Math.max(1, price * (1 + rand() * vol));
    candles.push(candle(price, i, vol / 2, 100 + Math.abs(rand()) * 50));
  }
  return candles;
}
const series = makeSeries(2000, 1200);
const options = {
  symbol: "TEST/USDT", exchange: "test", timeframe: "5m", candles: series,
  composerConfig: defaultComposerConfig, initialCapital: 10_000, positionSizePct: 100,
  stopLossPct: 0, takeProfitPct: 0, feePct: 0.06, minConfidence: 0.35, useAtrStops: true,
  atrStopMultiplier: 1, atrTakeProfitMultiplier: 2, isFutures: true, slippageBps: 2, leverage: 1,
  maxBarsInTrade: 12, htfCandles: [], recordEquityCurve: false,
} as const;
const full = runBacktest({ ...options });
const split = runBacktest({ ...options, oosPct: 20 });
const isRes = split, oosRes = split.oosResult!;
console.log(`FULL ${full.totalTrades} ret ${full.totalReturnPct.toFixed(2)} | IS ${isRes.totalTrades} ret ${isRes.totalReturnPct.toFixed(2)} | OOS ${oosRes.totalTrades} ret ${oosRes.totalReturnPct.toFixed(2)}`);
const cutIdx = Math.floor(series.length * 0.8);
const cutTime = series[cutIdx].timestamp.getTime();
const key = (t: any) => `${t.side}@${new Date(t.entryTime).getTime()}`;
const fullIs = full.trades.filter(t => new Date(t.entryTime).getTime() < cutTime);
const fullOos = full.trades.filter(t => new Date(t.entryTime).getTime() >= cutTime);
const isSet = new Set(isRes.trades.map(key)), fullIsSet = new Set(fullIs.map(key));
console.log(`IS region: full=${fullIs.length} is=${isRes.totalTrades} onlyFull=${fullIs.filter(t=>!isSet.has(key(t))).length} onlyIS=${isRes.trades.filter(t=>!fullIsSet.has(key(t))).length}`);
const oosSet = new Set(oosRes.trades.map(key)), fullOosSet = new Set(fullOos.map(key));
const onlyFullOos = fullOos.filter(t=>!oosSet.has(key(t))), onlyOos = oosRes.trades.filter(t=>!fullOosSet.has(key(t)));
console.log(`OOS region: full=${fullOos.length} oos=${oosRes.totalTrades} onlyFull=${onlyFullOos.length} onlyOOS=${onlyOos.length}`);
for (const t of onlyFullOos.slice(0,8)) console.log(`  onlyFULL ${t.side} ${new Date(t.entryTime).toISOString()} pnl ${t.pnlPct.toFixed(2)} exit ${t.exitReason}`);
for (const t of onlyOos.slice(0,8)) console.log(`  onlyOOS  ${t.side} ${new Date(t.entryTime).toISOString()} pnl ${t.pnlPct.toFixed(2)} exit ${t.exitReason}`);
// boundary trades in FULL around the cut
for (const t of full.trades) {
  const et = new Date(t.entryTime).getTime(), xt = new Date(t.exitTime).getTime();
  if (et < cutTime && xt >= cutTime) console.log(`  BOUNDARY-CROSS ${t.side} entry ${new Date(et).toISOString()} exit ${new Date(xt).toISOString()} pnl ${t.pnlPct.toFixed(2)}`);
}
