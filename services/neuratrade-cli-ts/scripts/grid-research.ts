import { Database } from "bun:sqlite";
import { resolve } from "path";
import { homedir } from "os";

interface Candle {
  open: number;
  high: number;
  low: number;
  close: number;
  volume: number;
  timestamp: string;
}

interface GridResult {
  totalReturnPct: number;
  maxDrawdownPct: number;
  winRate: number;
  totalTrades: number;
  profitFactor: number;
}

function loadCandles(
  symbol: string,
  timeframe: string,
): Candle[] {
  const dbPath = resolve(homedir(), ".neuratrade", "data", "neuratrade.db");
  const db = new Database(dbPath);
  const [base, quote] = symbol.split("/");
  const rows = db
    .query(
      `SELECT o.open_price as open, o.high_price as high, o.low_price as low,
              o.close_price as close, o.volume, o.timestamp
       FROM ohlcv_data o
       JOIN exchanges e ON o.exchange_id = e.id
       JOIN trading_pairs p ON o.trading_pair_id = p.id
       WHERE e.name = 'binance'
         AND p.base_currency = ? AND p.quote_currency = ?
         AND o.timeframe = ?
       ORDER BY o.timestamp ASC`,
    )
    .all(base, quote, timeframe) as Array<{
      open: number;
      high: number;
      low: number;
      close: number;
      volume: number;
      timestamp: string;
    }>;
  db.close();
  return rows;
}

function runGrid(
  candles: Candle[],
  options: {
    gridStepPct: number;
    maxGrids: number;
    feePct: number;
    slippageBps: number;
    initialCapital: number;
    trendFilterPeriod: number;
    pauseAfterLossBars: number;
  },
): GridResult {
  let capital = options.initialCapital;
  let peak = capital;
  let maxDrawdown = 0;
  let positionSize = 0;
  let entryPrice = 0;
  let totalWins = 0;
  let totalLosses = 0;
  let grossProfit = 0;
  let grossLoss = 0;
  let paused = 0;

  const sma = (i: number, period: number) => {
    if (i < period) return null;
    let sum = 0;
    for (let j = i - period + 1; j <= i; j++) sum += candles[j].close;
    return sum / period;
  };

  for (let i = options.trendFilterPeriod; i < candles.length; i++) {
    const c = candles[i];
    const trend = sma(i, options.trendFilterPeriod);
    if (trend === null) continue;

    capital = Math.max(0, capital);
    peak = Math.max(peak, capital);
    const dd = (peak - capital) / peak;
    if (dd > maxDrawdown) maxDrawdown = dd;

    if (paused > 0) {
      paused--;
      continue;
    }

    const mid = c.open;
    const step = mid * (options.gridStepPct / 100);
    const slippage = 1 + options.slippageBps / 10000;

    // If flat, place first grid order
    if (positionSize === 0) {
      const buyLevel = mid - step;
      const sellLevel = mid + step;
      if (c.low <= buyLevel) {
        entryPrice = buyLevel * slippage;
        positionSize = 1; // one unit
      } else if (c.high >= sellLevel) {
        entryPrice = sellLevel / slippage;
        positionSize = -1;
      }
      continue;
    }

    // Manage open position
    if (positionSize > 0) {
      const target = entryPrice + step;
      const stop = entryPrice - step * options.maxGrids;
      if (c.high >= target) {
        const exitPrice = target / slippage;
        const pnl = (exitPrice - entryPrice) / entryPrice;
        const fee = options.feePct / 100 * 2;
        const net = pnl - fee;
        capital *= 1 + net;
        if (net > 0) {
          totalWins++;
          grossProfit += net;
        } else {
          totalLosses++;
          grossLoss += Math.abs(net);
        }
        positionSize = 0;
      } else if (c.low <= stop) {
        const exitPrice = stop * slippage;
        const pnl = (exitPrice - entryPrice) / entryPrice;
        const fee = options.feePct / 100 * 2;
        const net = pnl - fee;
        capital *= 1 + net;
        if (net > 0) {
          totalWins++;
          grossProfit += net;
        } else {
          totalLosses++;
          grossLoss += Math.abs(net);
        }
        positionSize = 0;
        paused = options.pauseAfterLossBars;
      }
    } else {
      const target = entryPrice - step;
      const stop = entryPrice + step * options.maxGrids;
      if (c.low <= target) {
        const exitPrice = target * slippage;
        const pnl = (entryPrice - exitPrice) / entryPrice;
        const fee = options.feePct / 100 * 2;
        const net = pnl - fee;
        capital *= 1 + net;
        if (net > 0) {
          totalWins++;
          grossProfit += net;
        } else {
          totalLosses++;
          grossLoss += Math.abs(net);
        }
        positionSize = 0;
      } else if (c.high >= stop) {
        const exitPrice = stop / slippage;
        const pnl = (entryPrice - exitPrice) / entryPrice;
        const fee = options.feePct / 100 * 2;
        const net = pnl - fee;
        capital *= 1 + net;
        if (net > 0) {
          totalWins++;
          grossProfit += net;
        } else {
          totalLosses++;
          grossLoss += Math.abs(net);
        }
        positionSize = 0;
        paused = options.pauseAfterLossBars;
      }
    }
  }

  const totalTrades = totalWins + totalLosses;
  return {
    totalReturnPct: ((capital - options.initialCapital) / options.initialCapital) * 100,
    maxDrawdownPct: maxDrawdown * 100,
    winRate: totalTrades > 0 ? (totalWins / totalTrades) * 100 : 0,
    totalTrades,
    profitFactor: grossLoss > 0 ? grossProfit / grossLoss : grossProfit > 0 ? Infinity : 0,
  };
}

const symbols = ["ETH/USDT", "BTC/USDT"];
const timeframe = "15m";

const FEE_PCT = 0.2; // 0.1% per side = criterion
const SLIPPAGE_BPS = 5;

function gridSearch(candles: Candle[], subset: [number, number]): GridResult & { params: object } {
  let best: (GridResult & { params: object }) | null = null;
  for (const step of [0.5, 0.8, 1.0, 1.5, 2.0]) {
    for (const maxGrids of [2, 3, 5]) {
      for (const pause of [0, 6, 24, 48, 96]) {
        const res = runGrid(candles.slice(...subset), {
          gridStepPct: step,
          maxGrids,
          feePct: FEE_PCT,
          slippageBps: SLIPPAGE_BPS,
          initialCapital: 20,
          trendFilterPeriod: 96,
          pauseAfterLossBars: pause,
        });
        if (!best || res.totalReturnPct > best.totalReturnPct) {
          best = { ...res, params: { step, maxGrids, pause } };
        }
      }
    }
  }
  return best!;
}

for (const symbol of symbols) {
  const candles = loadCandles(symbol, timeframe);
  console.log(`\n=== Walk-forward grid ${symbol} ${timeframe}: ${candles.length} candles ===`);
  const trainSize = 96 * 30; // 30 days
  const testSize = 96 * 10; // 10 days
  let start = 0;
  let windowNum = 0;
  let aggregateReturn = 0;
  let windows = 0;
  let wins = 0;
  let totalTestTrades = 0;
  let maxDd = 0;
  let runningCapital = 20;

  while (start + trainSize + testSize <= candles.length) {
    windowNum++;
    const train = [start, start + trainSize] as [number, number];
    const test = [start + trainSize, start + trainSize + testSize] as [number, number];
    const best = gridSearch(candles, train);
    const testRes = runGrid(candles.slice(...test), {
      gridStepPct: (best.params as { step: number }).step,
      maxGrids: (best.params as { maxGrids: number }).maxGrids,
      feePct: FEE_PCT,
      slippageBps: SLIPPAGE_BPS,
      initialCapital: runningCapital,
      trendFilterPeriod: 96,
      pauseAfterLossBars: (best.params as { pause: number }).pause,
    });
    runningCapital *= 1 + testRes.totalReturnPct / 100;
    if (testRes.totalReturnPct > 0) wins++;
    totalTestTrades += testRes.totalTrades;
    maxDd = Math.max(maxDd, testRes.maxDrawdownPct);
    windows++;
    console.log(
      `Window ${windowNum} train ${best.totalReturnPct.toFixed(1)}% | test ${testRes.totalReturnPct.toFixed(2)}% dd=${testRes.maxDrawdownPct.toFixed(1)}% trades=${testRes.totalTrades} params=${JSON.stringify(best.params)}`,
    );
    start += testSize;
  }

  aggregateReturn = ((runningCapital - 20) / 20) * 100;
  console.log(`\nAggregate return: ${aggregateReturn.toFixed(2)}%`);
  console.log(`Profitable windows: ${wins}/${windows} (${((wins / windows) * 100).toFixed(1)}%)`);
  console.log(`Max single-window DD: ${maxDd.toFixed(2)}%`);
  console.log(`Total test trades: ${totalTestTrades}`);
}
