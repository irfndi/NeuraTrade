/**
 * Deflated Sharpe Ratio — selection-bias correction for a search that reports
 * its best result.
 *
 * Bailey & López de Prado (2014), "The Deflated Sharpe Ratio: Correcting for
 * Selection Bias, Backtest Overfitting and Non-Normality".
 *
 * The problem this solves: when a search evaluates N configurations and keeps
 * the best one, the expected maximum score under the null hypothesis (zero
 * true edge everywhere) is strictly positive. Reporting that maximum as
 * evidence of edge is therefore biased upward by construction. DSR tests the
 * observed Sharpe against that null expectation instead of against zero.
 *
 *   SR0 = sqrt(V[SR]) * ((1 - gamma) * z(1 - 1/N) + gamma * z(1 - 1/(N*e)))
 *   DSR = Phi[ (SR - SR0) * sqrt(T - 1) / sqrt(1 - skew*SR + ((kurt - 1)/4)*SR^2) ]
 *
 * where gamma is the Euler-Mascheroni constant, V[SR] the variance of Sharpe
 * across trials, T the number of return observations behind SR, skew the
 * candidate's skewness, and kurt its RAW kurtosis (3 = normal).
 *
 * UNITS — the single most important rule here: `observedSharpe` and every
 * entry of `trialSharpes` must be Sharpe ratios in the SAME per-observation
 * period as `nObs`, and `srStd` must be the spread of those same per-period
 * trial Sharpes. Mixing annualized Sharpes with a daily T inflates the z-score
 * by roughly sqrt(252) and makes noise look overwhelming. Callers that only
 * have a location statistic (a mean or median return) must NOT pass it here:
 * use `sharpeFromReturns` on the per-window return series first.
 *
 * @see https://papers.ssrn.com/sol3/papers.cfm?abstract_id=2460551
 */

/** Euler-Mascheroni constant, used by the expected-maximum approximation. */
const EULER_GAMMA = 0.5772156649015329;

/**
 * Inverse standard normal CDF (Acklam's rational approximation).
 *
 * Relative error below 1.15e-9 across the whole domain, which is two orders
 * of magnitude tighter than the 1e-3 resolution the DSR thresholds (0.80 /
 * 0.95) care about. Avoids a dependency on a stats library for one function.
 */
export function inverseNormalCdf(p: number): number {
  if (!Number.isFinite(p)) {
    throw new RangeError(`inverseNormalCdf: p must be finite, got ${p}`);
  }
  if (p <= 0 || p >= 1) {
    throw new RangeError(
      `inverseNormalCdf: p must be in (0, 1), got ${p} — the normal quantile is infinite at the bounds`,
    );
  }
  // Coefficients in Acklam's (2003) algorithm.
  const a = [
    -3.969683028665376e1, 2.209460984245205e2, -2.759285104469687e2,
    1.38357751867269e2, -3.066479806614716e1, 2.506628277459239,
  ];
  const b = [
    -5.447609879822406e1, 1.615858368580409e2, -1.556989798598866e2,
    6.680131188771972e1, -1.328068155288572e1,
  ];
  const c = [
    -7.784894002430293e-3, -3.223964580411365e-1, -2.400758277161838,
    -2.549732539343734, 4.374664141464968, 2.938163982698783,
  ];
  const d = [
    7.784695709041462e-3, 3.224671290700398e-1, 2.445134137142996,
    3.754408661907416,
  ];
  const pLow = 0.02425;

  if (p < pLow) {
    const q = Math.sqrt(-2 * Math.log(p));
    return (
      (((((c[0] * q + c[1]) * q + c[2]) * q + c[3]) * q + c[4]) * q + c[5]) /
      ((((d[0] * q + d[1]) * q + d[2]) * q + d[3]) * q + 1)
    );
  }
  if (p > 1 - pLow) {
    const q = Math.sqrt(-2 * Math.log(1 - p));
    return -(
      (((((c[0] * q + c[1]) * q + c[2]) * q + c[3]) * q + c[4]) * q + c[5]) /
      ((((d[0] * q + d[1]) * q + d[2]) * q + d[3]) * q + 1)
    );
  }
  const q = p - 0.5;
  const r = q * q;
  return (
    ((((((a[0] * r + a[1]) * r + a[2]) * r + a[3]) * r + a[4]) * r + a[5]) *
      q) /
    (((((b[0] * r + b[1]) * r + b[2]) * r + b[3]) * r + b[4]) * r + 1)
  );
}

/** Standard normal CDF via a high-accuracy erf approximation (Abramowitz-Stegun 7.1.26 form). */
export function normalCdf(z: number): number {
  if (!Number.isFinite(z)) {
    return z > 0 ? 1 : 0;
  }
  // Φ(z) = 0.5 * erfc(-z / sqrt(2)); use the complementary form so the tails
  // stay accurate instead of collapsing to 0 or 1.
  return 0.5 * erfc(-z / Math.SQRT2);
}

/** Complementary error function (Numerical Recipes erfcc, |error| < 1.2e-7). */
function erfc(x: number): number {
  const z = Math.abs(x);
  const t = 2 / (2 + z);
  const ty = 4 * t - 2;
  const coefficients = [
    -1.3026537197817094, 6.419697923564902e-1, 1.9476473204185836e-2,
    -9.561514786808631e-3, -9.46595344482036e-4, 3.66839497852761e-4,
    4.2523324806907e-5, -2.0278578112534e-5, -1.624290004647e-6,
    1.30365583558e-6, 1.5626441722e-8, -8.5238095915e-8, 6.529054439e-9,
    5.059343495e-9, -9.91364156e-10, -2.27365122e-10, 9.6467911e-11,
    2.394038e-12, -6.886027e-12, 8.94487e-13, 3.13092e-13, -1.12708e-13,
    3.81e-16, 7.106e-15,
  ];
  let d = 0;
  let dd = 0;
  for (let j = coefficients.length - 1; j > 0; j--) {
    const tmp = d;
    d = ty * d - dd + coefficients[j];
    dd = tmp;
  }
  const result = t * Math.exp(-z * z + 0.5 * (coefficients[0] + ty * d) - dd);
  return x >= 0 ? result : 2 - result;
}

/**
 * Per-period Sharpe ratio of a return series.
 *
 * Returns `undefined` when fewer than two observations exist or the series has
 * zero dispersion, because a Sharpe ratio is undefined in both cases. Callers
 * must treat `undefined` as "no evidence", never as zero.
 *
 * `sampleStd` uses the n-1 denominator, matching the convention of the trial
 * Sharpes it will be compared against.
 */
export function sharpeFromReturns(
  returns: readonly number[],
): number | undefined {
  const finite = returns.filter((r) => Number.isFinite(r));
  if (finite.length < 2) return undefined;
  const mean = finite.reduce((sum, r) => sum + r, 0) / finite.length;
  const variance =
    finite.reduce((sum, r) => sum + (r - mean) ** 2, 0) / (finite.length - 1);
  if (variance <= 0) return undefined;
  return mean / Math.sqrt(variance);
}

/** Skewness (g1) and RAW kurtosis (g2, 3 = normal) of a return series. */
export interface ReturnMoments {
  readonly skewness: number;
  readonly kurtosis: number;
}

/** Sample skewness (g1) and RAW kurtosis (g2, 3 = normal) of a series. */
export function returnMoments(returns: readonly number[]): ReturnMoments {
  const finite = returns.filter((r) => Number.isFinite(r));
  const n = finite.length;
  if (n < 2) return { skewness: 0, kurtosis: 3 };
  const mean = finite.reduce((sum, r) => sum + r, 0) / n;
  let m2 = 0;
  let m3 = 0;
  let m4 = 0;
  for (const r of finite) {
    const d = r - mean;
    const d2 = d * d;
    m2 += d2;
    m3 += d2 * d;
    m4 += d2 * d2;
  }
  m2 /= n;
  m3 /= n;
  m4 /= n;
  if (m2 <= 0) return { skewness: 0, kurtosis: 3 };
  const sd = Math.sqrt(m2);
  return { skewness: m3 / sd ** 3, kurtosis: m4 / (m2 * m2) };
}

/** E[max Sharpe] under the null across `nTrials` independent trials. */
export function expectedMaxSharpe(srStd: number, nTrials: number): number {
  if (!Number.isFinite(nTrials) || nTrials <= 1 || !(srStd > 0)) return 0;
  if (nTrials <= 2) {
    // 1 - 1/N is 0 or 0.5 at N = 1, 2; the left quantile is unbounded below at
    // N = 1, so the formula only becomes meaningful for N >= 2.
    return srStd * normalQuantileSafe(1 - 1 / nTrials);
  }
  const z1 = normalQuantileSafe(1 - 1 / nTrials);
  const z2 = normalQuantileSafe(1 - 1 / (nTrials * Math.E));
  return srStd * ((1 - EULER_GAMMA) * z1 + EULER_GAMMA * z2);
}

/** Quantile that tolerates the exact 0/1 endpoints produced by 1 - 1/N. */
function normalQuantileSafe(p: number): number {
  const clamped = Math.min(1 - 1e-12, Math.max(1e-12, p));
  return inverseNormalCdf(clamped);
}

export interface DeflatedSharpeInput {
  /** Per-period Sharpe of the selected candidate. */
  readonly observedSharpe: number;
  /** Variance source: per-period Sharpe of every completed trial. */
  readonly trialSharpes: readonly number[];
  /** T — observations behind `observedSharpe`. */
  readonly nObs: number;
  /** Skewness of the candidate's returns (0 = normal). */
  readonly skewness?: number;
  /** RAW kurtosis of the candidate's returns (3 = normal). */
  readonly kurtosis?: number;
  /** Null benchmark Sharpe, same units (default 0). */
  readonly benchmarkSharpe?: number;
  /**
   * Effective independent trial count. Strongly correlated knob variants are
   * not independent trials; leaving this unset assumes the raw count, which
   * is conservative (larger N => higher SR0 => lower DSR).
   */
  readonly effectiveTrials?: number;
}

/**
 * Deflated Sharpe Ratio: P(true Sharpe > SR0) in [0, 1].
 *
 * Thresholds (Bailey & López de Prado): >= 0.95 robust, 0.80–0.95 marginal,
 * < 0.80 likely false positive.
 *
 * Degenerate inputs are handled honestly rather than optimistically: with
 * N <= 1 or zero dispersion across trials there is no selection bias to
 * correct, so the result reduces to the Probabilistic Sharpe Ratio against
 * `benchmarkSharpe` — never to the raw Sharpe.
 */
export function deflatedSharpe(input: DeflatedSharpeInput): number {
  const {
    observedSharpe,
    trialSharpes,
    nObs,
    skewness = 0,
    kurtosis = 3,
    benchmarkSharpe = 0,
  } = input;
  if (!Number.isFinite(observedSharpe)) return 0;

  const finite = trialSharpes.filter((s) => Number.isFinite(s));
  const rawN = finite.length;
  const effectiveN = input.effectiveTrials ?? rawN;
  if (!Number.isFinite(effectiveN) || effectiveN < 1) {
    throw new RangeError(
      `deflatedSharpe: effectiveTrials must be finite and >= 1, got ${input.effectiveTrials}`,
    );
  }
  if (effectiveN > rawN && rawN >= 1) {
    throw new RangeError(
      `deflatedSharpe: effectiveTrials (${effectiveN}) must be <= trial count (${rawN})`,
    );
  }

  const srStd =
    finite.length > 1
      ? Math.sqrt(
          finite.reduce((sum, s) => sum + (s - mean(finite)) ** 2, 0) /
            finite.length,
        )
      : 0;

  const sr0 =
    effectiveN <= 1 || srStd === 0
      ? benchmarkSharpe
      : benchmarkSharpe + expectedMaxSharpe(srStd, effectiveN);

  return probabilisticSharpe({
    observedSharpe,
    benchmarkSharpe: sr0,
    nObs,
    skewness,
    kurtosis,
  });
}

/** Probabilistic Sharpe Ratio: P(true Sharpe > benchmark). */
export function probabilisticSharpe(input: {
  readonly observedSharpe: number;
  readonly benchmarkSharpe?: number;
  readonly nObs: number;
  readonly skewness?: number;
  readonly kurtosis?: number;
}): number {
  const {
    observedSharpe,
    benchmarkSharpe = 0,
    nObs,
    skewness = 0,
    kurtosis = 3,
  } = input;
  if (!Number.isFinite(observedSharpe)) return 0;
  if (!Number.isFinite(nObs) || nObs < 2) return 0;

  let denomSq =
    1 - skewness * observedSharpe + ((kurtosis - 1) / 4) * observedSharpe ** 2;
  if (!(denomSq > 0)) {
    // Degenerate higher moments: fall back to the normal-returns denominator
    // instead of fabricating certainty.
    denomSq = 1 + 0.5 * observedSharpe ** 2;
  }
  const z =
    ((observedSharpe - benchmarkSharpe) * Math.sqrt(nObs - 1)) /
    Math.sqrt(denomSq);
  return normalCdf(z);
}

export type SharpVerdict = "robust" | "marginal" | "likely-false-positive";

/** Traffic-light classification of a DSR value. */
export function classifyDeflatedSharpe(dsr: number): SharpVerdict {
  if (!Number.isFinite(dsr)) return "likely-false-positive";
  if (dsr >= 0.95) return "robust";
  if (dsr >= 0.8) return "marginal";
  return "likely-false-positive";
}

/** Population mean of a non-empty series. */
function mean(values: readonly number[]): number {
  return values.reduce((sum, v) => sum + v, 0) / values.length;
}
