/**
 * Ladder-engine readiness evidence validator.
 *
 * Mirrors `grid-validation.ts` (data-quality, historical walk-forward
 * windows, fixed-OOS, block-bootstrap confidence, adverse-stress runs) but
 * evaluates every window through the ladder backtest so a ladder survivor can
 * clear the real-money readiness board. Pure API: consumes candle rows and
 * options, returns a serializable result; never fetches data or prints.
 *
 * This is the "ladder evidence validator" the universe scan's readiness tier
 * failed closed without (--engine ladder required --tier fast).
 */

import type { CandleLike } from "./types.js";
import {
  runLadderGridBacktest,
  type LadderOptions,
  type LadderResult,
} from "./ladder-grid.js";
import {
  READINESS_STRESS_SEEDS,
  bootstrapBlockConfidence,
  validateCandleDataQuality,
  type BlockConfidence,
  type CandleDataQuality,
} from "./grid-validation.js";

export interface LadderValidationOptions {
  readonly now: Date;
  /** Candle interval in minutes; drives spacing/freshness checks (default 15). */
  readonly timeframeMinutes?: number;
  readonly trainBars?: number;
  readonly testBars?: number;
  readonly minimumWindows?: number;
  readonly minimumFixedOosTrades?: number;
  readonly ladder: LadderOptions;
  readonly executionParityPassed?: boolean;
}

export interface LadderValidationWindow {
  readonly trainStartIndex: number;
  readonly trainEndIndex: number;
  readonly testStartIndex: number;
  readonly testEndIndex: number;
  readonly result: LadderResult;
}

export interface LadderValidationOk {
  readonly kind: "ok";
  readonly dataQuality: CandleDataQuality;
  readonly historical: {
    readonly windows: readonly LadderValidationWindow[];
    readonly profitableWindowPct: number;
    readonly compoundedReturnPct: number;
    readonly maximumDrawdownPct: number;
    readonly totalTrades: number;
  };
  readonly fixedOos: LadderResult;
  readonly confidence: BlockConfidence;
  readonly stress: {
    readonly seeds: readonly number[];
    readonly runs: readonly {
      readonly seed: number;
      readonly result: LadderResult;
      readonly confidence: BlockConfidence;
    }[];
    readonly worstReturnPct: number;
    readonly pooledLowerBoundPct: number;
  };
  readonly executionParity: {
    readonly passed: boolean;
    readonly protocolVersion: "execution-parity/v1";
  };
}

export interface LadderValidationInvalid {
  readonly kind: "invalid";
  readonly dataQuality: CandleDataQuality;
  readonly failures: readonly string[];
}

export type LadderValidationResult =
  | LadderValidationOk
  | LadderValidationInvalid;

function resultForWindow(
  candles: readonly CandleLike[],
  start: number,
  trainBars: number,
  testBars: number,
  ladder: LadderOptions,
): LadderResult {
  return runLadderGridBacktest(
    candles.slice(start + trainBars, start + trainBars + testBars),
    {
      ...ladder,
      initialCapital: ladder.initialCapital,
    },
  );
}

function confidenceForResult(
  result: LadderResult,
  seed: number,
): BlockConfidence {
  return bootstrapBlockConfidence(
    result.trades.map((trade) => trade.pnlPct.toString()),
    seed,
  );
}

/**
 * Validate ladder evidence for the readiness board. Data-quality failures and
 * a historical return at/below -100% fail closed; fixed-OOS and stress runs
 * follow the same protocol as the grid validator (block-5 bootstrap, 5000
 * resamples, seeded deterministically, pooled 5-seed stress LB).
 */
export function validateLadderEvidence(
  candles: readonly CandleLike[],
  options: LadderValidationOptions,
): LadderValidationResult {
  const trainBars = options.trainBars ?? 11520;
  const testBars = options.testBars ?? 4320;
  const minimumWindows = options.minimumWindows ?? 10;
  const minimumFixedOosTrades = options.minimumFixedOosTrades ?? 30;
  const dataQuality = validateCandleDataQuality(
    candles,
    options.now,
    trainBars,
    testBars,
    minimumWindows,
    options.timeframeMinutes ?? 15,
  );
  if (!dataQuality.valid) {
    return { kind: "invalid", dataQuality, failures: dataQuality.failures };
  }

  const windows: LadderValidationWindow[] = [];
  let capital = options.ladder.initialCapital;
  let peakCapital = capital;
  let maximumDrawdownPct = 0;
  let totalTrades = 0;
  for (
    let start = 0;
    start + trainBars + testBars <= candles.length;
    start += testBars
  ) {
    const result = resultForWindow(
      candles,
      start,
      trainBars,
      testBars,
      options.ladder,
    );
    if (result.totalReturnPct <= -100) {
      return {
        kind: "invalid",
        dataQuality,
        failures: ["historical return is at or below -100%"],
      };
    }
    capital *= 1 + result.totalReturnPct / 100;
    peakCapital = Math.max(peakCapital, capital);
    maximumDrawdownPct = Math.max(
      maximumDrawdownPct,
      peakCapital > 0 ? ((peakCapital - capital) / peakCapital) * 100 : 0,
    );
    totalTrades += result.totalTrades;
    windows.push({
      trainStartIndex: start,
      trainEndIndex: start + trainBars,
      testStartIndex: start + trainBars,
      testEndIndex: start + trainBars + testBars,
      result,
    });
  }
  const profitableWindowCount = windows.filter(
    (window) => window.result.totalReturnPct > 0,
  ).length;
  const profitableWindowPct = (profitableWindowCount / windows.length) * 100;
  const compoundedReturnPct =
    ((capital - options.ladder.initialCapital) /
      options.ladder.initialCapital) *
    100;
  const oosStart = Math.floor(candles.length * 0.8);
  const fixedOos = runLadderGridBacktest(
    candles.slice(oosStart),
    options.ladder,
  );
  if (fixedOos.totalTrades < minimumFixedOosTrades) {
    return {
      kind: "invalid",
      dataQuality,
      failures: [`fixed OOS trade count is below ${minimumFixedOosTrades}`],
    };
  }
  const confidence = confidenceForResult(fixedOos, 20260802);
  const stressRuns = READINESS_STRESS_SEEDS.map((seed) => {
    const result = runLadderGridBacktest(candles.slice(oosStart), {
      ...options.ladder,
      makerFillProb: 0.7,
      adverseSelection: true,
      takerExitFeePct: 0.06,
      fillSeed: seed,
    });
    return {
      seed,
      result,
      confidence: confidenceForResult(result, seed),
    };
  });
  // Same pooled protocol as the grid validator: block-5 bootstrap over the
  // combined 5-seed trade sequence (n≈145), seed 20260802.
  const pooledStressLowerBoundPct = bootstrapBlockConfidence(
    stressRuns.flatMap((run) =>
      run.result.trades.map((trade) => trade.pnlPct.toString()),
    ),
    20260802,
  ).lowerBoundPct;
  return {
    kind: "ok",
    dataQuality,
    historical: {
      windows,
      profitableWindowPct,
      compoundedReturnPct,
      maximumDrawdownPct,
      totalTrades,
    },
    fixedOos,
    confidence,
    stress: {
      seeds: [...READINESS_STRESS_SEEDS],
      runs: stressRuns,
      worstReturnPct: Math.min(
        ...stressRuns.map((run) => run.result.totalReturnPct),
      ),
      pooledLowerBoundPct: pooledStressLowerBoundPct,
    },
    executionParity: {
      passed: options.executionParityPassed ?? false,
      protocolVersion: "execution-parity/v1",
    },
  };
}
