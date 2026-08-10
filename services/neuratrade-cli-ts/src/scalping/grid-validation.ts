import type { CandleLike } from "./types.js";
import { runGridBacktest, type GridOptions, type GridResult } from "./grid.js";

export const READINESS_STRESS_SEEDS = [
  20260802, 20260803, 20260804, 20260805, 20260806,
] as const;

export interface GridValidationOptions {
  readonly now: Date;
  /** Candle interval in minutes; drives the spacing/freshness checks (default 15). */
  readonly timeframeMinutes?: number;
  readonly trainBars?: number;
  readonly testBars?: number;
  readonly minimumWindows?: number;
  readonly minimumFixedOosTrades?: number;
  readonly grid: GridOptions;
  readonly executionParityPassed?: boolean;
}

export interface CandleDataQuality {
  readonly valid: boolean;
  readonly failures: readonly string[];
  readonly candleCount: number;
  readonly latestCandle: Date | null;
  readonly completeWindows: number;
}

export interface BlockConfidence {
  readonly lowerBoundPct: number;
  readonly upperBoundPct: number;
  readonly resamples: number;
  readonly blockLength: number;
  readonly seed: number;
  readonly sampleCount: number;
}

export interface GridValidationWindow {
  readonly trainStartIndex: number;
  readonly trainEndIndex: number;
  readonly testStartIndex: number;
  readonly testEndIndex: number;
  readonly result: GridResult;
}

export interface GridValidationOk {
  readonly kind: "ok";
  readonly dataQuality: CandleDataQuality;
  readonly historical: {
    readonly windows: readonly GridValidationWindow[];
    readonly profitableWindowPct: number;
    readonly compoundedReturnPct: number;
    readonly maximumDrawdownPct: number;
    readonly totalTrades: number;
  };
  readonly fixedOos: GridResult;
  readonly confidence: BlockConfidence;
  readonly stress: {
    readonly seeds: readonly number[];
    readonly runs: readonly {
      readonly seed: number;
      readonly result: GridResult;
      readonly confidence: BlockConfidence;
    }[];
    readonly worstReturnPct: number;
    /**
     * Plan amendment 2026-08-07 (owner decision B): the gate's stress LB is
     * the block bootstrap over the COMBINED 5-seed trade sequence (n≈145),
     * not the worst per-seed LB (n≈29/seed widens the CI past the mean).
     * Per-seed runs stay as evidence; the ≥0 threshold is unchanged.
     */
    readonly pooledLowerBoundPct: number;
  };
  readonly executionParity: {
    readonly passed: boolean;
    readonly protocolVersion: "execution-parity/v1";
  };
}

export interface GridValidationInvalid {
  readonly kind: "invalid";
  readonly dataQuality: CandleDataQuality;
  readonly failures: readonly string[];
}

export type GridValidationResult = GridValidationOk | GridValidationInvalid;

const MAX_FRESHNESS_HOURS = 48;
const CONFIDENCE_RESAMPLES = 5000;
const CONFIDENCE_BLOCK_LENGTH = 5;

function xorshift32(state: number): number {
  let value = state >>> 0;
  value ^= value << 13;
  value ^= value >>> 17;
  value ^= value << 5;
  return value >>> 0;
}

function nextState(state: number): number {
  return xorshift32(state);
}

function mean(values: readonly number[]): number {
  return values.reduce((sum, value) => sum + value, 0) / values.length;
}

function quantile(sorted: readonly number[], probability: number): number {
  const position = probability * (sorted.length - 1);
  const lower = Math.floor(position);
  const upper = Math.ceil(position);
  const lowerValue = sorted[lower] ?? 0;
  const upperValue = sorted[upper] ?? lowerValue;
  return lowerValue + (upperValue - lowerValue) * (position - lower);
}

export function bootstrapBlockConfidence(
  values: readonly string[],
  seed: number,
  blockLength = CONFIDENCE_BLOCK_LENGTH,
  resamples = CONFIDENCE_RESAMPLES,
): BlockConfidence {
  const numeric = values.map(Number);
  if (numeric.length < blockLength) {
    // Degenerate evidence (stress path): fewer trades than one bootstrap
    // block cannot support resampling. Return a zero-width interval around
    // the sample mean instead of throwing — callers treat the returned
    // BlockConfidence as evidence, so a low-trade run must not crash the
    // whole universe scan. Width 0 means the gate compares the raw mean,
    // the best estimate available at this sample size.
    const sampleMean =
      numeric.length > 0 && numeric.every((value) => Number.isFinite(value))
        ? mean(numeric)
        : 0;
    return {
      lowerBoundPct: sampleMean,
      upperBoundPct: sampleMean,
      resamples: 0,
      blockLength,
      seed,
      sampleCount: numeric.length,
    };
  }
  if (seed === 0 || !numeric.every((value) => Number.isFinite(value))) {
    // pnlPct.toString() legitimately serializes tiny values in exponential
    // notation (e.g. "1e-7"); a string-syntax gate would crash the whole
    // universe scan on valid evidence. Only genuinely non-numeric input is
    // a caller bug worth throwing on.
    throw new Error("invalid block-bootstrap input");
  }
  const estimands: number[] = [];
  const maximumBlockStart = numeric.length - blockLength;
  for (let iteration = 0; iteration < resamples; iteration += 1) {
    // Per-run state = seed + run index. Seeding EVERY run with the same
    // value makes all resamples identical (xorshift32 is deterministic),
    // collapsing the interval to width 0: the gate then compares the raw
    // block mean against zero and can pass while the true 95% lower bound
    // is negative (fail-open). Determinism is preserved: same seed + same
    // input always yields the same interval.
    let state = (seed + iteration) >>> 0;
    const sample: number[] = [];
    while (sample.length < numeric.length) {
      state = nextState(state);
      const blockStart = Math.floor(
        (state / 2 ** 32) * (maximumBlockStart + 1),
      );
      for (
        let offset = 0;
        offset < blockLength && sample.length < numeric.length;
        offset += 1
      ) {
        sample.push(numeric[blockStart + offset] ?? 0);
      }
    }
    estimands.push(mean(sample));
  }
  estimands.sort((left, right) => left - right);
  return {
    lowerBoundPct: quantile(estimands, 0.025),
    upperBoundPct: quantile(estimands, 0.975),
    resamples,
    blockLength,
    seed,
    sampleCount: numeric.length,
  };
}

function countWindows(
  candleCount: number,
  trainBars: number,
  testBars: number,
): number {
  let count = 0;
  for (
    let start = 0;
    start + trainBars + testBars <= candleCount;
    start += testBars
  ) {
    count += 1;
  }
  return count;
}

export function validateCandleDataQuality(
  candles: readonly CandleLike[],
  now: Date,
  trainBars = 11520,
  testBars = 4320,
  minimumWindows = 10,
  timeframeMinutes = 15,
): CandleDataQuality {
  const failures: string[] = [];
  const barMillis = timeframeMinutes * 60 * 1000;
  let previousTimestamp: number | null = null;
  for (const [index, candle] of candles.entries()) {
    const values = [
      candle.open,
      candle.high,
      candle.low,
      candle.close,
      candle.volume,
    ];
    if (!values.every((value) => Number.isFinite(value))) {
      failures.push(`candle ${index} contains a non-finite OHLCV value`);
    }
    if (
      candle.open <= 0 ||
      candle.high <= 0 ||
      candle.low <= 0 ||
      candle.close <= 0
    ) {
      failures.push(`candle ${index} contains a non-positive price`);
    }
    if (candle.volume < 0)
      failures.push(`candle ${index} contains negative volume`);
    if (candle.high < Math.max(candle.open, candle.close)) {
      failures.push(`candle ${index} high is below open or close`);
    }
    if (candle.low > Math.min(candle.open, candle.close)) {
      failures.push(`candle ${index} low is above open or close`);
    }
    const timestamp = candle.timestamp.getTime();
    if (!Number.isFinite(timestamp)) {
      failures.push(`candle ${index} timestamp is invalid`);
    } else if (previousTimestamp !== null) {
      const delta = timestamp - previousTimestamp;
      if (delta !== barMillis) {
        failures.push(
          `candle ${index} is not exactly ${timeframeMinutes}m after the previous candle`,
        );
      }
    }
    previousTimestamp = timestamp;
  }
  const latestCandle = candles.at(-1)?.timestamp ?? null;
  const latestMillis = latestCandle?.getTime() ?? Number.NaN;
  const nowMillis = now.getTime();
  if (candles.length === 0) failures.push("candle evidence is empty");
  if (Number.isFinite(latestMillis) && latestMillis > nowMillis) {
    failures.push("latest candle is in the future");
  }
  if (
    Number.isFinite(latestMillis) &&
    nowMillis - latestMillis > MAX_FRESHNESS_HOURS * 60 * 60 * 1000
  ) {
    failures.push("latest candle is stale");
  }
  const completeWindows = countWindows(candles.length, trainBars, testBars);
  if (completeWindows < minimumWindows) {
    failures.push(`complete window count is below ${minimumWindows}`);
  }
  return {
    valid: failures.length === 0,
    failures,
    candleCount: candles.length,
    latestCandle,
    completeWindows,
  };
}

function resultForWindow(
  candles: readonly CandleLike[],
  start: number,
  trainBars: number,
  testBars: number,
  grid: GridOptions,
): GridResult {
  return runGridBacktest(
    candles.slice(start + trainBars, start + trainBars + testBars),
    {
      ...grid,
      initialCapital: grid.initialCapital,
    },
  );
}

function confidenceForResult(
  result: GridResult,
  seed: number,
): BlockConfidence {
  return bootstrapBlockConfidence(
    result.trades.map((trade) => trade.pnlPct.toString()),
    seed,
  );
}

export function validateGridEvidence(
  candles: readonly CandleLike[],
  options: GridValidationOptions,
): GridValidationResult {
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

  const windows: GridValidationWindow[] = [];
  let capital = options.grid.initialCapital;
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
      options.grid,
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
    ((capital - options.grid.initialCapital) / options.grid.initialCapital) *
    100;
  const oosStart = Math.floor(candles.length * 0.8);
  const fixedOos = runGridBacktest(candles.slice(oosStart), options.grid);
  if (fixedOos.totalTrades < minimumFixedOosTrades) {
    return {
      kind: "invalid",
      dataQuality,
      failures: [`fixed OOS trade count is below ${minimumFixedOosTrades}`],
    };
  }
  const confidence = confidenceForResult(fixedOos, 20260802);
  const stressRuns = READINESS_STRESS_SEEDS.map((seed) => {
    const result = runGridBacktest(candles.slice(oosStart), {
      ...options.grid,
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
  // Amendment 2026-08-07: gate LB = bootstrap over the combined 5-seed
  // sequence (same protocol: block 5, 5000 resamples, seed 20260802).
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
