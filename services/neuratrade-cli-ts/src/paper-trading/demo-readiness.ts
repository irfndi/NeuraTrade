import type { Money } from "../utils/money.js";
import { money } from "../utils/money.js";
import type { GridPaperTrade } from "./types.js";
import { bootstrapMeanConfidenceInterval } from "./expectancy-confidence.js";

export interface DemoSoakThresholds {
  readonly minimumTrades: number;
  readonly minimumDurationDays: number;
  readonly minimumExpectancyPct: Money;
  readonly minimumExpectancyLowerBoundPct?: Money;
  readonly maximumDrawdownPct: Money;
  readonly confidenceResamples?: number;
}

export interface DemoSoakReport {
  readonly passed: boolean;
  readonly tradeCount: number;
  readonly durationDays: number;
  readonly expectancyPct: Money;
  readonly expectancyLowerBoundPct: Money;
  readonly expectancyUpperBoundPct: Money;
  readonly profitFactor: Money | null;
  readonly maximumDrawdownPct: Money;
  readonly failures: readonly string[];
}

export function serializeDemoSoakReport(report: DemoSoakReport): string {
  return JSON.stringify({
    status: report.passed ? "PASS" : "FAIL",
    passed: report.passed,
    tradeCount: report.tradeCount,
    durationDays: report.durationDays,
    expectancyPct: report.expectancyPct.toString(),
    expectancyLowerBoundPct: report.expectancyLowerBoundPct.toString(),
    expectancyUpperBoundPct: report.expectancyUpperBoundPct.toString(),
    profitFactor: report.profitFactor?.toString() ?? null,
    maximumDrawdownPct: report.maximumDrawdownPct.toString(),
    failures: report.failures,
  });
}

function hasCompleteLiveFillEvidence(trade: GridPaperTrade): boolean {
  return (
    trade.fillSource === "live" &&
    Number.isFinite(trade.openedAt.getTime()) &&
    Number.isFinite(trade.closedAt.getTime()) &&
    Boolean(trade.entryOrderId) &&
    Boolean(trade.exitOrderId) &&
    (trade.entryFilledQty?.greaterThan(0) ?? false) &&
    (trade.entryFilledQty?.isFinite() ?? false) &&
    (trade.exitFilledQty?.greaterThan(0) ?? false) &&
    (trade.exitFilledQty?.isFinite() ?? false) &&
    (trade.exitFilledQty?.equals(trade.entryFilledQty ?? money(0)) ?? false) &&
    (trade.entryFee?.greaterThanOrEqualTo(0) ?? false) &&
    (trade.entryFee?.isFinite() ?? false) &&
    (trade.exitFee?.greaterThanOrEqualTo(0) ?? false) &&
    (trade.exitFee?.isFinite() ?? false) &&
    (trade.realizedPnlPct?.isFinite() ?? false)
  );
}

function durationDays(trades: readonly GridPaperTrade[]): number {
  if (trades.length === 0) return 0;
  if (
    trades.some(
      (trade) =>
        !Number.isFinite(trade.openedAt.getTime()) ||
        !Number.isFinite(trade.closedAt.getTime()),
    )
  ) {
    return 0;
  }
  const openedAt = Math.min(...trades.map((trade) => trade.openedAt.getTime()));
  const closedAt = Math.max(...trades.map((trade) => trade.closedAt.getTime()));
  return (closedAt - openedAt) / (24 * 60 * 60 * 1000);
}

export function evaluateDemoSoak(
  trades: readonly GridPaperTrade[],
  thresholds: DemoSoakThresholds,
): DemoSoakReport {
  const completeLiveTrades = trades.filter(hasCompleteLiveFillEvidence);
  const totalPnl = completeLiveTrades.reduce(
    (sum, trade) => sum.plus(trade.realizedPnlPct ?? money(0)),
    money(0),
  );
  const expectancyPct =
    completeLiveTrades.length > 0
      ? totalPnl.div(completeLiveTrades.length)
      : money(0);
  const expectancyConfidence =
    completeLiveTrades.length > 0
      ? bootstrapMeanConfidenceInterval(
          completeLiveTrades.map((trade) => trade.realizedPnlPct ?? money(0)),
          { resamples: thresholds.confidenceResamples },
        )
      : undefined;
  const expectancyLowerBoundPct = expectancyConfidence?.lowerBound ?? money(0);
  const expectancyUpperBoundPct = expectancyConfidence?.upperBound ?? money(0);
  const grossProfit = completeLiveTrades.reduce(
    (sum, trade) =>
      trade.realizedPnlPct?.greaterThan(0)
        ? sum.plus(trade.realizedPnlPct)
        : sum,
    money(0),
  );
  const grossLoss = completeLiveTrades.reduce(
    (sum, trade) =>
      trade.realizedPnlPct?.lessThan(0)
        ? sum.plus(trade.realizedPnlPct.abs())
        : sum,
    money(0),
  );
  const profitFactor = grossLoss.greaterThan(0)
    ? grossProfit.div(grossLoss)
    : grossProfit.greaterThan(0)
      ? null
      : money(0);

  let capital = money(100);
  let peakCapital = capital;
  let maximumDrawdownPct = money(0);
  for (const trade of [...completeLiveTrades].sort(
    (left, right) => left.closedAt.getTime() - right.closedAt.getTime(),
  )) {
    capital = capital.times(
      money(1).plus((trade.realizedPnlPct ?? money(0)).div(100)),
    );
    peakCapital = peakCapital.greaterThan(capital) ? peakCapital : capital;
    const drawdownPct = peakCapital.minus(capital).div(peakCapital).times(100);
    maximumDrawdownPct = maximumDrawdownPct.greaterThan(drawdownPct)
      ? maximumDrawdownPct
      : drawdownPct;
  }

  const failures: string[] = [];
  if (completeLiveTrades.length < thresholds.minimumTrades) {
    failures.push("trade count is below the minimum");
  }
  const soakDurationDays = durationDays(completeLiveTrades);
  if (soakDurationDays < thresholds.minimumDurationDays) {
    failures.push("duration is below the minimum");
  }
  if (completeLiveTrades.length !== trades.length) {
    failures.push("one or more trades lack complete live fill evidence");
  }
  if (expectancyPct.lessThan(thresholds.minimumExpectancyPct)) {
    failures.push("expectancy is below the minimum");
  }
  if (
    thresholds.minimumExpectancyLowerBoundPct !== undefined &&
    expectancyLowerBoundPct.lessThan(thresholds.minimumExpectancyLowerBoundPct)
  ) {
    failures.push("expectancy confidence lower bound is below the minimum");
  }
  if (maximumDrawdownPct.greaterThan(thresholds.maximumDrawdownPct)) {
    failures.push("drawdown is above the maximum");
  }

  return {
    passed: failures.length === 0,
    tradeCount: completeLiveTrades.length,
    durationDays: soakDurationDays,
    expectancyPct,
    expectancyLowerBoundPct,
    expectancyUpperBoundPct,
    profitFactor,
    maximumDrawdownPct,
    failures,
  };
}
