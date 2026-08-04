import type { BacktestResult } from "./backtest.js";

/**
 * Futures-scalping readiness gates (design doc §3 G1–G4):
 *  - G1 frequency: enough trades per month in-sample, enough OOS trades.
 *  - G2 economics: positive expectancy, profit factor, win rate.
 *  - G3 robustness: OOS return/drawdown, Monte Carlo p95 DD / ruin.
 *  - G4 hold time: average trade duration stays in the scalping envelope.
 *
 * `evaluateReadiness` is pure: it consumes a BacktestResult produced with
 * oosPct + mcIterations set and renders a per-gate verdict table. The CLI
 * layer owns data loading and printing.
 */
export interface ReadinessThresholds {
  /** G1: minimum in-sample trades per month. */
  readonly minTradesPerMonth: number;
  /** G1: minimum out-of-sample trades (absolute count). */
  readonly minOosTrades: number;
  /** G2: minimum win rate as a fraction (0.5 = 50%). */
  readonly minWinRate: number;
  /** G2: minimum profit factor. */
  readonly minProfitFactor: number;
  /** G2: expectancy must be strictly greater than this (percent per trade). */
  readonly minExpectancyPct: number;
  /** G3: minimum OOS total return percent (>= 0 means "not negative"). */
  readonly minOosReturnPct: number;
  /** G3: maximum OOS max drawdown percent. */
  readonly maxOosDrawdownPct: number;
  /** G3: maximum Monte Carlo p95 max drawdown percent. */
  readonly maxMcP95DrawdownPct: number;
  /** G3: maximum Monte Carlo probability-of-ruin percent. */
  readonly maxMcRuinPct: number;
  /** G4: maximum average trade duration in hours. */
  readonly maxAvgDurationHours: number;
}

/** Defaults from the design doc; timeframe-specific G1 rates are applied by the CLI. */
export const defaultReadinessThresholds: ReadinessThresholds = {
  minTradesPerMonth: 20,
  minOosTrades: 10,
  minWinRate: 0.5,
  minProfitFactor: 1.3,
  minExpectancyPct: 0,
  minOosReturnPct: 0,
  maxOosDrawdownPct: 15,
  maxMcP95DrawdownPct: 20,
  maxMcRuinPct: 5,
  maxAvgDurationHours: 4,
};

export interface ReadinessGateResult {
  readonly gate: string;
  readonly description: string;
  readonly target: string;
  readonly actual: string;
  readonly pass: boolean;
}

export interface ReadinessReport {
  readonly symbol: string;
  readonly timeframe: string;
  readonly gates: readonly ReadinessGateResult[];
  readonly ready: boolean;
}

export interface EvaluateReadinessInput {
  /** Full engine result (IS portion when oosPct was set). */
  readonly result: BacktestResult;
  /** Exchange/timeframe context for the report header. */
  readonly timeframe: string;
  /** Data span covered by the in-sample run, in months. */
  readonly inSampleMonths: number;
  /** Threshold overrides; defaults come from defaultReadinessThresholds. */
  readonly thresholds?: Partial<ReadinessThresholds>;
}

function gate(
  id: string,
  description: string,
  target: string,
  actual: string,
  pass: boolean,
): ReadinessGateResult {
  return { gate: id, description, target, actual, pass };
}

export function evaluateReadiness(
  input: EvaluateReadinessInput,
): ReadinessReport {
  const t: ReadinessThresholds = {
    ...defaultReadinessThresholds,
    ...(input.thresholds ?? {}),
  };
  const r = input.result;
  const oos = r.oosResult;
  const mc = r.monteCarlo;

  const months = Math.max(input.inSampleMonths, 1e-9);
  const tradesPerMonth = r.totalTrades / months;

  const gates: ReadinessGateResult[] = [
    gate(
      "G1a",
      "in-sample trade frequency",
      `>= ${t.minTradesPerMonth} trades/month`,
      `${tradesPerMonth.toFixed(1)} trades/month (${r.totalTrades} over ${months.toFixed(1)} mo)`,
      tradesPerMonth >= t.minTradesPerMonth,
    ),
    gate(
      "G1b",
      "out-of-sample trade count",
      `>= ${t.minOosTrades} trades`,
      oos ? `${oos.totalTrades} trades` : "no OOS run",
      oos !== undefined && oos.totalTrades >= t.minOosTrades,
    ),
    gate(
      "G2a",
      "win rate",
      `>= ${(t.minWinRate * 100).toFixed(0)}%`,
      `${(r.winRate * 100).toFixed(2)}%`,
      r.winRate >= t.minWinRate,
    ),
    gate(
      "G2b",
      "profit factor",
      `>= ${t.minProfitFactor}`,
      r.metrics.profitFactor.toFixed(3),
      r.metrics.profitFactor >= t.minProfitFactor,
    ),
    gate(
      "G2c",
      "expectancy (net of fees)",
      `> ${t.minExpectancyPct}%/trade`,
      `${r.metrics.expectancy.toFixed(3)}%/trade`,
      r.metrics.expectancy > t.minExpectancyPct,
    ),
    gate(
      "G3a",
      "OOS total return",
      `>= ${t.minOosReturnPct}%`,
      oos ? `${oos.totalReturnPct.toFixed(2)}%` : "no OOS run",
      oos !== undefined && oos.totalReturnPct >= t.minOosReturnPct,
    ),
    gate(
      "G3b",
      "OOS max drawdown",
      `<= ${t.maxOosDrawdownPct}%`,
      oos ? `${oos.maxDrawdownPct.toFixed(2)}%` : "no OOS run",
      oos !== undefined && oos.maxDrawdownPct <= t.maxOosDrawdownPct,
    ),
    gate(
      "G3c",
      "Monte Carlo p95 max drawdown",
      `<= ${t.maxMcP95DrawdownPct}%`,
      mc ? `${mc.p95MaxDrawdownPct.toFixed(2)}%` : "no MC run",
      mc !== undefined && mc.p95MaxDrawdownPct <= t.maxMcP95DrawdownPct,
    ),
    gate(
      "G3d",
      "Monte Carlo probability of ruin",
      `<= ${t.maxMcRuinPct}%`,
      mc ? `${mc.probabilityOfRuinPct.toFixed(2)}%` : "no MC run",
      mc !== undefined && mc.probabilityOfRuinPct <= t.maxMcRuinPct,
    ),
    gate(
      "G4",
      "average trade duration",
      `<= ${t.maxAvgDurationHours}h`,
      `${r.metrics.averageTradeDurationHours.toFixed(2)}h`,
      r.metrics.averageTradeDurationHours <= t.maxAvgDurationHours,
    ),
  ];

  return {
    symbol: r.symbol,
    timeframe: input.timeframe,
    gates,
    ready: gates.every((g) => g.pass),
  };
}

export function formatReadinessReport(report: ReadinessReport): string {
  const lines = [
    `Scalping readiness: ${report.symbol} (${report.timeframe})`,
    `${"GATE".padEnd(5)} ${"CHECK".padEnd(34)} ${"TARGET".padEnd(26)} ${"ACTUAL".padEnd(46)} VERDICT`,
    ...report.gates.map(
      (g) =>
        `${g.gate.padEnd(5)} ${g.description.padEnd(34)} ${g.target.padEnd(26)} ${g.actual.padEnd(46)} ${g.pass ? "PASS" : "FAIL"}`,
    ),
    report.ready
      ? "READY: all gates pass"
      : `NOT READY: ${report.gates.filter((g) => !g.pass).length} gate(s) failing`,
  ];
  return lines.join("\n");
}
