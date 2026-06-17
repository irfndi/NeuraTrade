import type { BacktestTrade } from "./backtest.js";

/**
 * Extended performance metrics for a backtest result.
 */
export interface BacktestMetrics {
  readonly profitFactor: number;
  readonly expectancy: number;
  readonly averageRMultiple: number;
  readonly sortinoRatio: number;
  readonly calmarRatio: number;
  readonly maxConsecutiveLosses: number;
  readonly averageTradeDurationHours: number;
  readonly timeInMarketPct: number;
}

export interface ComputePerformanceMetricsInput {
  readonly trades: readonly BacktestTrade[];
  readonly initialCapital: number;
  readonly maxDrawdownPct: number;
  readonly totalReturnPct: number;
  readonly candleSpanMs: number;
  readonly riskFreeRate?: number;
}

const zeroMetrics: BacktestMetrics = {
  profitFactor: 0,
  expectancy: 0,
  averageRMultiple: 0,
  sortinoRatio: 0,
  calmarRatio: 0,
  maxConsecutiveLosses: 0,
  averageTradeDurationHours: 0,
  timeInMarketPct: 0,
};

/**
 * Compute advanced performance metrics from a sequence of backtest trades.
 *
 * All calculations are pure and return safe zero values for degenerate inputs
 * (no trades, no losses, zero drawdown, etc.).
 */
export function computePerformanceMetrics(
  input: ComputePerformanceMetricsInput,
): BacktestMetrics {
  const {
    trades,
    maxDrawdownPct,
    totalReturnPct,
    candleSpanMs,
    riskFreeRate = 0,
  } = input;

  if (trades.length === 0 || input.initialCapital <= 0) {
    return zeroMetrics;
  }

  const wins = trades.filter((t) => t.pnl > 0);
  const losses = trades.filter((t) => t.pnl < 0);

  const grossProfit = wins.reduce((sum, t) => sum + t.pnl, 0);
  const grossLoss = Math.abs(losses.reduce((sum, t) => sum + t.pnl, 0));

  const profitFactor =
    grossLoss === 0
      ? grossProfit > 0
        ? Number.POSITIVE_INFINITY
        : 0
      : grossProfit / grossLoss;

  const winRate = wins.length / trades.length;
  const lossRate = losses.length / trades.length;

  const avgWin =
    wins.length > 0
      ? wins.reduce((sum, t) => sum + t.pnlPct, 0) / wins.length
      : 0;
  const avgLoss =
    losses.length > 0
      ? Math.abs(losses.reduce((sum, t) => sum + t.pnlPct, 0)) / losses.length
      : 0;

  const expectancy = winRate * avgWin - lossRate * avgLoss;

  const rMultiples = trades
    .map((t) => {
      if (!t.initialRiskPct || t.initialRiskPct <= 0) return 0;
      return t.pnlPct / 100 / t.initialRiskPct;
    })
    .filter((r) => Number.isFinite(r));
  const averageRMultiple =
    rMultiples.length > 0
      ? rMultiples.reduce((sum, r) => sum + r, 0) / rMultiples.length
      : 0;

  const returns = trades.map((t) => t.pnlPct);
  const meanReturn = returns.reduce((sum, r) => sum + r, 0) / returns.length;
  const excessReturn = meanReturn - riskFreeRate;

  const downsideReturns = returns.filter((r) => r < 0);
  const downsideDeviation =
    downsideReturns.length > 0
      ? Math.sqrt(
          downsideReturns.reduce((sum, r) => sum + r * r, 0) /
            downsideReturns.length,
        )
      : 0;
  const sortinoRatio =
    downsideDeviation === 0 ? 0 : excessReturn / downsideDeviation;

  const calmarRatio =
    maxDrawdownPct > 0
      ? totalReturnPct / maxDrawdownPct
      : totalReturnPct > 0
        ? Number.POSITIVE_INFINITY
        : 0;

  let maxConsecutiveLosses = 0;
  let currentStreak = 0;
  for (const trade of trades) {
    if (trade.pnl < 0) {
      currentStreak += 1;
      if (currentStreak > maxConsecutiveLosses) {
        maxConsecutiveLosses = currentStreak;
      }
    } else {
      currentStreak = 0;
    }
  }

  const totalDurationMs = trades.reduce(
    (sum, t) => sum + (t.exitTime.getTime() - t.entryTime.getTime()),
    0,
  );
  const averageTradeDurationHours =
    trades.length > 0 ? totalDurationMs / trades.length / 3_600_000 : 0;
  const timeInMarketPct =
    candleSpanMs > 0 ? (totalDurationMs / candleSpanMs) * 100 : 0;

  return {
    profitFactor: Number.isNaN(profitFactor) ? 0 : profitFactor,
    expectancy: Number.isNaN(expectancy) ? 0 : expectancy,
    averageRMultiple: Number.isNaN(averageRMultiple) ? 0 : averageRMultiple,
    sortinoRatio: Number.isNaN(sortinoRatio) ? 0 : sortinoRatio,
    calmarRatio: Number.isNaN(calmarRatio) ? 0 : calmarRatio,
    maxConsecutiveLosses,
    averageTradeDurationHours,
    timeInMarketPct,
  };
}
