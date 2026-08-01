#!/usr/bin/env bun
import { Database } from "bun:sqlite";
import { join } from "node:path";
import { z } from "zod";
import { bootstrapMeanConfidenceInterval } from "../src/paper-trading/expectancy-confidence.js";
import { splitCandlesByOos } from "../src/scalping/backtest.js";
import { runGridBacktest, type GridOptions } from "../src/scalping/grid.js";
import type { Candle } from "../src/market-data/types.js";
import { money } from "../src/utils/money.js";

const rowSchema = z.object({
  open_price: z.number(),
  high_price: z.number(),
  low_price: z.number(),
  close_price: z.number(),
  volume: z.number(),
  timestamp: z.string(),
});

const rowsSchema = z.array(rowSchema);

function argument(name: string, fallback: string): string {
  const index = process.argv.indexOf(name);
  return index >= 0 && index + 1 < process.argv.length
    ? (process.argv[index + 1] ?? fallback)
    : fallback;
}

function parseTimestamp(value: string): Date {
  const normalized = value.endsWith("Z")
    ? value
    : `${value.replace(" ", "T")}Z`;
  const timestamp = new Date(normalized);
  if (!Number.isFinite(timestamp.getTime())) {
    throw new Error(`invalid candle timestamp: ${value}`);
  }
  return timestamp;
}

const exchange = argument("--exchange", "bitget-futures");
const symbol = argument("--symbol", "BTC/USDT:USDT");
const timeframe = argument("--timeframe", "15m");
const home =
  process.env.NEURATRADE_HOME ?? join(process.env.HOME ?? ".", ".neuratrade");
const database = new Database(join(home, "data", "neuratrade.db"), {
  readonly: true,
});

try {
  const rows = rowsSchema.parse(
    database
      .query(
        `SELECT o.open_price, o.high_price, o.low_price, o.close_price, o.volume, o.timestamp
         FROM ohlcv_data o JOIN exchanges e ON e.id=o.exchange_id JOIN trading_pairs tp ON tp.id=o.trading_pair_id
         WHERE e.name = ? AND tp.symbol = ? AND o.timeframe = ? ORDER BY o.timestamp ASC`,
      )
      .all(exchange, symbol, timeframe),
  );
  if (rows.length === 0) {
    throw new Error(`no candles for ${exchange}:${symbol}:${timeframe}`);
  }

  const candles: Candle[] = rows.map((row) => ({
    exchange,
    symbol,
    timeframe,
    open: row.open_price,
    high: row.high_price,
    low: row.low_price,
    close: row.close_price,
    volume: row.volume,
    timestamp: parseTimestamp(row.timestamp),
  }));
  const { oos } = splitCandlesByOos(candles, 20);
  const recentCandles = candles.slice(-2_880);
  const winner: GridOptions = {
    feePct: 0.02,
    slippageBps: 1,
    trendFilterPeriod: 0,
    leverage: 1,
    onlyWithTrend: false,
    targetRatio: 1,
    chopGateAdxThreshold: 30,
    initialCapital: 10_000,
    gridStepPct: 1,
    gridMaxGrids: 1.5,
    gridPauseAfterLossBars: 12,
  };
  const cases: ReadonlyArray<readonly [string, GridOptions]> = [
    ["optimistic", winner],
    ["taker-stops", { ...winner, takerExitFeePct: 0.06 }],
    [
      "adverse-maker",
      {
        ...winner,
        makerFillProb: 0.7,
        adverseSelection: true,
        takerExitFeePct: 0.06,
      },
    ],
  ];

  const report = cases.map(([label, options]) => {
    const result = runGridBacktest(oos, options);
    const recentResult = runGridBacktest(recentCandles, options);
    const interval = bootstrapMeanConfidenceInterval(
      result.trades.map((trade) => money(trade.pnlPct).times(100)),
      { confidenceLevel: 0.95, resamples: 5_000, seed: 20260802 },
    );
    return {
      label,
      oosCandles: oos.length,
      trades: result.totalTrades,
      totalReturnPct: result.totalReturnPct,
      winRatePct: result.winRate,
      profitFactor: result.profitFactor,
      maxDrawdownPct: result.maxDrawdownPct,
      expectancyPct: interval.sampleMean.toString(),
      expectancyLowerBoundPct: interval.lowerBound.toString(),
      expectancyUpperBoundPct: interval.upperBound.toString(),
      confidenceLevel: interval.confidenceLevel,
      lowerBoundPositive: interval.lowerBound.greaterThan(0),
      recent30d: {
        candles: recentCandles.length,
        trades: recentResult.totalTrades,
        totalReturnPct: recentResult.totalReturnPct,
        winRatePct: recentResult.winRate,
        profitFactor: recentResult.profitFactor,
        expectancyPct:
          recentResult.trades.length > 0
            ? money(
                recentResult.trades.reduce(
                  (sum, trade) => sum.plus(money(trade.pnlPct).times(100)),
                  money(0),
                ),
              )
                .div(recentResult.trades.length)
                .toString()
            : "0",
      },
    };
  });

  console.log(
    JSON.stringify(
      {
        exchange,
        symbol,
        timeframe,
        totalCandles: candles.length,
        oosFraction: 0.2,
        method: "fixed-config OOS bootstrap over realized per-trade expectancy",
        cases: report,
      },
      null,
      2,
    ),
  );
} finally {
  database.close();
}
