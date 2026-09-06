import { Database } from "bun:sqlite";
import type { CandleLike } from "../src/scalping/types.js";

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

function sma(c: CandleLike[], i: number, p: number): number | null {
  if (i < p) return null;
  let s = 0;
  for (let j = i - p + 1; j <= i; j++) s += c[j].close;
  return s / p;
}

interface Opts {
  gridStepPct: number;
  gridMaxGrids: number;
  feePct: number;
  slippageBps: number;
  initialCapital: number;
  trendFilterPeriod: number;
  leverage: number;
  onlyWithTrend: boolean;
  targetRatio: number; // target = step * targetRatio
}

function closeOpenGridPosition(
  positionSize: number,
  entryPrice: number,
  step: number,
  candle: CandleLike,
  slippage: number,
  leverage: number,
  targetRatio: number,
  maxGrids: number,
  closeTrade: (
    exitPrice: number,
    side: "long" | "short",
    liquidation: boolean,
  ) => void,
): void {
  if (positionSize > 0) {
    const target = entryPrice + step * targetRatio;
    const stop = entryPrice - step * maxGrids;
    const liq = entryPrice * (1 - 1 / leverage);
    if (leverage > 1 && candle.low <= liq) {
      closeTrade(liq * slippage, "long", true);
    } else if (candle.high >= target) {
      closeTrade(target / slippage, "long", false);
    } else if (candle.low <= stop) {
      closeTrade(stop * slippage, "long", false);
    }
    return;
  }
  const target = entryPrice - step * targetRatio;
  const stop = entryPrice + step * maxGrids;
  const liq = entryPrice * (1 + 1 / leverage);
  if (leverage > 1 && candle.high >= liq) {
    closeTrade(liq / slippage, "short", true);
  } else if (candle.low <= target) {
    closeTrade(target * slippage, "short", false);
  } else if (candle.high >= stop) {
    closeTrade(stop / slippage, "short", false);
  }
}
function run(candles: CandleLike[], options: Opts) {
  let capital = options.initialCapital;
  let peak = capital;
  let maxDrawdown = 0;
  let positionSize = 0;
  let entryPrice = 0;
  let totalWins = 0;
  let totalLosses = 0;
  let grossProfit = 0;
  let grossLoss = 0;
  const leverage = Math.max(1, options.leverage);
  const startIndex = Math.max(options.trendFilterPeriod, 1);

  for (let i = startIndex; i < candles.length; i++) {
    const c = candles[i];
    const trend = sma(candles, i, options.trendFilterPeriod);
    if (trend === null) continue;

    capital = Math.max(0, capital);
    peak = Math.max(peak, capital);
    const dd = peak > 0 ? (peak - capital) / peak : 0;
    if (dd > maxDrawdown) maxDrawdown = dd;

    const mid = c.open;
    const step = mid * (options.gridStepPct / 100);
    const slippage = 1 + options.slippageBps / 10000;

    if (positionSize === 0) {
      const allowLong = !options.onlyWithTrend || c.close > trend;
      const allowShort = !options.onlyWithTrend || c.close < trend;
      const buyLevel = mid - step;
      const sellLevel = mid + step;
      if (allowLong && c.low <= buyLevel) {
        entryPrice = buyLevel * slippage;
        positionSize = 1;
      } else if (allowShort && c.high >= sellLevel) {
        entryPrice = sellLevel / slippage;
        positionSize = -1;
      }
      continue;
    }

    const fee = (options.feePct / 100) * 2;
    const closeTrade = (
      exitPrice: number,
      exitSide: "long" | "short",
      isLiquidation: boolean,
    ) => {
      const pricePnl =
        exitSide === "long"
          ? (exitPrice - entryPrice) / entryPrice
          : (entryPrice - exitPrice) / entryPrice;
      const net = pricePnl - fee;
      const leveragedReturn = isLiquidation ? -1 : net * leverage;
      const rawCapitalAfter = capital * (1 + leveragedReturn);
      capital = isLiquidation ? 0 : Math.max(0, rawCapitalAfter);
      if (isLiquidation || net < 0) {
        totalLosses++;
        grossLoss += Math.abs(leveragedReturn);
      } else {
        totalWins++;
        grossProfit += leveragedReturn;
      }
      positionSize = 0;
    };

    closeOpenGridPosition(
      positionSize,
      entryPrice,
      step,
      c,
      slippage,
      leverage,
      options.targetRatio,
      options.gridMaxGrids,
      closeTrade,
    );
  }

  const totalTrades = totalWins + totalLosses;
  return {
    totalReturnPct:
      ((capital - options.initialCapital) / options.initialCapital) * 100,
    maxDrawdownPct: maxDrawdown * 100,
    winRate: totalTrades > 0 ? (totalWins / totalTrades) * 100 : 0,
    totalTrades,
    profitFactor:
      grossLoss > 0 ? grossProfit / grossLoss : grossProfit > 0 ? Infinity : 0,
  };
}

console.log(`Loaded ${candles.length} candles`);

for (const onlyWithTrend of [false, true]) {
  for (const targetRatio of [1, 1.5, 2]) {
    for (const step of [0.7, 1.0, 1.5, 2.0]) {
      for (const maxGrids of [1, 1.5]) {
        const opts: Opts = {
          gridStepPct: step,
          gridMaxGrids: maxGrids,
          feePct: 0.2,
          slippageBps: 5,
          initialCapital: 20,
          trendFilterPeriod: 50,
          leverage: 1,
          onlyWithTrend,
          targetRatio,
        };
        const result = run(candles, opts);
        if (result.totalTrades > 0) {
          console.log(
            `trend=${onlyWithTrend} targetRatio=${targetRatio} step=${step}% maxGrids=${maxGrids} -> return=${result.totalReturnPct.toFixed(2)}% trades=${result.totalTrades} win=${result.winRate.toFixed(1)}% dd=${result.maxDrawdownPct.toFixed(2)}% pf=${result.profitFactor.toFixed(3)}`,
          );
        }
      }
    }
  }
}
