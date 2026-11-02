/**
 * Edge validation for a single-symbol barrier strategy.
 *
 * Every strategy in this repo ultimately rests on one claim: that price reaches
 * the target barrier before the stop barrier more often than costs allow. This
 * module tests that claim directly on real candles, so a strategy can be
 * rejected before any code is written around it.
 *
 * Three things it does that hand-rolled probes kept getting wrong:
 *
 *  1. NON-OVERLAPPING samples by default. Overlapping windows share almost all
 *     their bars, so n is inflated and the error bar collapses. A 4h reversion
 *     rule scored z=6.01 with stride 1 and z=0.50 with stride 16 on the same
 *     data. Reporting the first number would have been a false discovery.
 *  2. COST-ADJUSTED breakeven, not the gross 1/(1+R). The gross figure flatters
 *     a strategy by ~2pp at our fee tier, which is the difference between
 *     "nothing works" and "one symbol works".
 *  3. PER-SYMBOL series. Concatenating trading pairs turns a BTC-to-SOL price
 *     jump into apparent reversion and fabricates a 99.6% hit rate.
 */

export interface BarrierResult {
  /** Samples where the target was touched first. */
  readonly targetFirst: number;
  /** Samples where the stop was touched first. */
  readonly stopFirst: number;
  /** Samples where neither barrier was touched inside the horizon. */
  readonly undecided: number;
  /** targetFirst / (targetFirst + stopFirst). */
  readonly hitRate: number;
  /** Standard error of hitRate. */
  readonly standardError: number;
}

export interface BarrierOptions {
  /** Target distance as a fraction of entry price (0.0195 = 1.95%). */
  readonly targetPct: number;
  /** Stop distance as a fraction of entry price (0.02 = 2%). */
  readonly stopPct: number;
  /** Bars to wait for a barrier before giving up. */
  readonly horizonBars: number;
  /**
   * Sample every Nth bar. Must be >= horizonBars for the samples to be
   * non-overlapping; defaults to horizonBars for exactly that reason.
   */
  readonly stride?: number;
}

/**
 * Which barrier one sample touches first. Split out of `barrierHitRate` so each
 * function has one job: this decides a single sample, the caller counts them.
 */
function firstBarrier(
  closes: readonly number[],
  entryIndex: number,
  up: number,
  down: number,
  horizonBars: number,
): "target" | "stop" | "neither" {
  for (let k = entryIndex + 1; k <= entryIndex + horizonBars; k += 1) {
    const price = closes[k];
    if (!Number.isFinite(price)) continue;
    if (price >= up) return "target";
    if (price <= down) return "stop";
  }
  return "neither";
}

/**
 * First-touch barrier test. No look-ahead: each sample only inspects bars
 * strictly after the observation bar.
 */
export function barrierHitRate(
  closes: readonly number[],
  options: BarrierOptions,
): BarrierResult {
  const { targetPct, stopPct, horizonBars } = options;
  if (!(targetPct > 0) || !(stopPct > 0) || !(horizonBars > 0)) {
    throw new RangeError(
      "barrierHitRate: targetPct, stopPct and horizonBars must all be positive",
    );
  }
  const stride = options.stride ?? horizonBars;
  if (!(stride >= 1)) {
    throw new RangeError(`barrierHitRate: stride must be >= 1, got ${stride}`);
  }

  let targetFirst = 0;
  let stopFirst = 0;
  let undecided = 0;

  for (let i = 0; i + horizonBars < closes.length; i += stride) {
    const entry = closes[i];
    if (!Number.isFinite(entry) || entry <= 0) continue;
    const outcome = firstBarrier(
      closes,
      i,
      entry * (1 + targetPct),
      entry * (1 - stopPct),
      horizonBars,
    );
    if (outcome === "target") targetFirst += 1;
    else if (outcome === "stop") stopFirst += 1;
    else undecided += 1;
  }

  const decidedCount = targetFirst + stopFirst;
  const hitRate = decidedCount > 0 ? targetFirst / decidedCount : 0;
  const standardError =
    decidedCount > 0 ? Math.sqrt((hitRate * (1 - hitRate)) / decidedCount) : 0;
  return { targetFirst, stopFirst, undecided, hitRate, standardError };
}

export interface CostModel {
  /** Maker fee percent per side (0.02 = 0.02%). */
  readonly makerFeePct: number;
  /** Taker fee percent on stop exits (0.06 = 0.06%). */
  readonly takerExitFeePct: number;
  /** Slippage in basis points per leg (2 = 0.02%). */
  readonly slippageBps: number;
}

/**
 * Breakeven hit rate AFTER costs, as a fraction.
 *
 * The winning leg pays maker on entry and exit; the losing leg pays maker on
 * entry and taker on the stop, plus slippage on both legs each time. Returns
 * the hit rate at which expected value is zero, so a measured hit rate at or
 * below this figure means the strategy loses money regardless of tuning.
 */
export function costAdjustedBreakeven(
  targetPct: number,
  stopPct: number,
  costs: CostModel,
): number {
  const slip = (costs.slippageBps / 10_000) * 100;
  const winLegPct = targetPct * 100 - (costs.makerFeePct * 2 + slip * 2);
  const lossLegPct =
    stopPct * 100 + (costs.makerFeePct + costs.takerExitFeePct + slip * 2);
  const total = winLegPct + lossLegPct;
  if (!(total > 0)) {
    throw new RangeError(
      "costAdjustedBreakeven: target is too small to cover round-trip costs",
    );
  }
  const breakeven = lossLegPct / total;
  // A breakeven above 1 is not a rate, it means the target cannot even pay for
  // its own round trip, so the trade can never win. Returning 1.034 as if it
  // were a hit-rate threshold invites a caller to compare against it and
  // conclude "no edge" instead of "this geometry is impossible".
  if (breakeven > 1) {
    throw new RangeError(
      `costAdjustedBreakeven: breakeven ${(breakeven * 100).toFixed(2)}% exceeds 100% ` +
        "- the target does not cover round-trip costs, so this geometry cannot win at any hit rate",
    );
  }
  return breakeven;
}

export interface EdgeVerdict {
  readonly hitRate: number;
  readonly breakeven: number;
  /** (hitRate - breakeven) in percentage points. */
  readonly marginPp: number;
  /** Hit rate minus breakeven, in standard errors. */
  readonly z: number;
  readonly samples: number;
  readonly verdict: "edge" | "no-edge" | "inconclusive";
}

/**
 * Compare a measured hit rate against the cost-adjusted breakeven.
 *
 * "inconclusive" is a real outcome and is reported rather than coerced: with a
 * small significant-error ratio the honest answer is that more data is needed.
 */
export function assessEdge(
  result: BarrierResult,
  breakeven: number,
  /**
   * z above which a positive margin counts as an edge. 1.96 is the usual 95%
   * two-sided threshold.
   */
  zThreshold = 1.96,
): EdgeVerdict {
  const samples = result.targetFirst + result.stopFirst;
  const marginPp = (result.hitRate - breakeven) * 100;
  const z =
    result.standardError > 0
      ? (result.hitRate - breakeven) / result.standardError
      : 0;
  let verdict: EdgeVerdict["verdict"];
  if (samples === 0) verdict = "inconclusive";
  else if (z > zThreshold) verdict = "edge";
  else if (z < -zThreshold) verdict = "no-edge";
  else verdict = "inconclusive";
  return { hitRate: result.hitRate, breakeven, marginPp, z, samples, verdict };
}

/**
 * Variance ratio of q-bar returns to q times the 1-bar variance.
 *
 * Below 1 means mean reverting, above 1 trending, near 1 a random walk. This is
 * the statistic that decided the route for this repo: 15m was a random walk
 * while 4h to 16h reverted, which is why symmetric grids on 15m could not work.
 *
 * Unlike a lag-k autocorrelation over overlapping windows, this does not inflate
 * its own sample count.
 */
export function varianceRatio(closes: readonly number[], q: number): number {
  if (!(q >= 2)) return Number.NaN;
  const oneBar: number[] = [];
  for (let i = 1; i < closes.length; i += 1) {
    const prev = closes[i - 1];
    const cur = closes[i];
    if (prev > 0 && cur > 0) oneBar.push(Math.log(cur / prev));
  }
  if (oneBar.length < q * 2) return Number.NaN;

  const meanOne = oneBar.reduce((a, b) => a + b, 0) / oneBar.length;
  const varOne =
    oneBar.reduce((a, b) => a + (b - meanOne) ** 2, 0) / oneBar.length;
  if (!(varOne > 0)) return Number.NaN;

  const qBar: number[] = [];
  for (let i = q; i < closes.length; i += 1) {
    const prev = closes[i - q];
    const cur = closes[i];
    if (prev > 0 && cur > 0) qBar.push(Math.log(cur / prev));
  }
  if (qBar.length === 0) return Number.NaN;
  const meanQ = qBar.reduce((a, b) => a + b, 0) / qBar.length;
  const varQ = qBar.reduce((a, b) => a + (b - meanQ) ** 2, 0) / qBar.length;

  return varQ / q / varOne;
}

/**
 * Ordinary least squares of `y` on `x`. Used to estimate the hedge ratio of a
 * spread and to run the ADF regression.
 */
export interface OlsFit {
  readonly intercept: number;
  readonly slope: number;
}

/** Fit `y = intercept + slope * x`. */
export function ols(y: readonly number[], x: readonly number[]): OlsFit {
  if (y.length !== x.length || y.length < 2) {
    throw new RangeError(
      `ols: y and x must be the same length >= 2 (got ${y.length}, ${x.length})`,
    );
  }
  const n = y.length;
  const meanX = x.reduce((a, b) => a + b, 0) / n;
  const meanY = y.reduce((a, b) => a + b, 0) / n;
  let sxy = 0;
  let sxx = 0;
  for (let i = 0; i < n; i += 1) {
    sxy += (x[i] - meanX) * (y[i] - meanY);
    sxx += (x[i] - meanX) ** 2;
  }
  if (!(sxx > 0)) {
    throw new RangeError("ols: x has no variance, slope is undefined");
  }
  const slope = sxy / sxx;
  return { intercept: meanY - slope * meanX, slope };
}

/**
 * Engle-Granger ADF t-statistic for a residual spread fitted by OLS.
 *
 * This is the necessary condition for a relative-value trade. If the spread is
 * not stationary there is nothing to revert, and any "z-score reversion" you
 * measure is noise being fitted.
 *
 * Returns the t-statistic of `delta(resid) ~ resid[-1]`. Compare against the
 * TWO-VARIABLE Engle-Granger critical value, not the single-series ADF one:
 * about -3.34 at 5% for 2 variables with no trend. Using -2.86 here is the
 * classic way this test gets over-read, because the residuals come from an
 * estimated regression and the test is therefore biased toward acceptance.
 *
 * `undefined` when the residual series is too short to regress.
 */
export function engleGrangerAdf(resid: readonly number[]): number | undefined {
  const n = resid.length;
  if (n < 30) return undefined;
  const delta: number[] = [];
  const lag: number[] = [];
  for (let k = 1; k < n; k += 1) {
    delta.push(resid[k] - resid[k - 1]);
    lag.push(resid[k - 1]);
  }
  let beta: number;
  try {
    beta = ols(delta, lag).slope;
  } catch {
    return undefined;
  }
  const meanDelta = delta.reduce((a, b) => a + b, 0) / delta.length;
  const meanLag = lag.reduce((a, b) => a + b, 0) / lag.length;
  let residualSumSq = 0;
  let lagSumSq = 0;
  for (let k = 0; k < delta.length; k += 1) {
    const fitted = meanDelta + beta * (lag[k] - meanLag);
    residualSumSq += (delta[k] - fitted) ** 2;
    lagSumSq += (lag[k] - meanLag) ** 2;
  }
  if (!(lagSumSq > 0) || delta.length <= 2) return undefined;
  const standardError = Math.sqrt(
    residualSumSq / (delta.length - 2) / lagSumSq,
  );
  return standardError > 0 ? beta / standardError : undefined;
}

/** 5% Engle-Granger critical value for 2 variables, no trend (MacKinnon). */
export const EG_CRITICAL_5PCT_2VAR = -3.34;

/**
 * Whether a spread from `ols` is cointegrated at 5%.
 *
 * Only "yes" licenses a relative-value trade. "no" means the spread wanders, so
 * entry and exit thresholds are arbitrary and the reversion you measure is
 * fitted to noise.
 */
export function isCointegrated(
  resid: readonly number[],
  criticalValue = EG_CRITICAL_5PCT_2VAR,
): boolean {
  const t = engleGrangerAdf(resid);
  return t !== undefined && t < criticalValue;
}
