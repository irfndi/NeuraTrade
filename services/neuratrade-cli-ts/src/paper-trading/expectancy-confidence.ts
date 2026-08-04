import { money, type Money } from "../utils/money.js";

export interface ExpectancyConfidenceOptions {
  readonly confidenceLevel?: number;
  readonly resamples?: number;
  readonly seed?: number;
}

export interface ExpectancyConfidenceInterval {
  readonly sampleSize: number;
  readonly resamples: number;
  readonly confidenceLevel: number;
  readonly sampleMean: Money;
  readonly lowerBound: Money;
  readonly upperBound: Money;
}

function quantile(sorted: readonly Money[], probability: number): Money {
  const position = probability * (sorted.length - 1);
  const lowerIndex = Math.floor(position);
  const upperIndex = Math.ceil(position);
  const lower = sorted.at(lowerIndex);
  const upper = sorted.at(upperIndex);
  if (lower === undefined || upper === undefined) {
    throw new Error("bootstrap sample is empty");
  }
  if (lowerIndex === upperIndex) return lower;
  return lower.plus(upper.minus(lower).times(position - lowerIndex));
}

export function bootstrapMeanConfidenceInterval(
  values: readonly Money[],
  options: ExpectancyConfidenceOptions = {},
): ExpectancyConfidenceInterval {
  if (values.length === 0) {
    throw new Error("at least one realized expectancy is required");
  }
  if (values.some((value) => !value.isFinite())) {
    throw new Error("realized expectancy values must be finite");
  }

  const confidenceLevel = options.confidenceLevel ?? 0.95;
  if (
    !Number.isFinite(confidenceLevel) ||
    confidenceLevel <= 0 ||
    confidenceLevel >= 1
  ) {
    throw new Error("confidenceLevel must be between 0 and 1");
  }
  const resamples = options.resamples ?? 2_000;
  if (!Number.isInteger(resamples) || resamples <= 0) {
    throw new Error("resamples must be positive");
  }

  let state = Math.trunc(options.seed ?? 0x6d2b79f5) >>> 0;
  const nextIndex = (): number => {
    state = (Math.imul(state, 1_664_525) + 1_013_904_223) >>> 0;
    return Math.floor((state / 4_294_967_296) * values.length);
  };

  const sampleMean = values
    .reduce((sum, value) => sum.plus(value), money(0))
    .div(values.length);
  const bootstrapMeans: Money[] = [];
  for (let sample = 0; sample < resamples; sample++) {
    let total = money(0);
    for (let draw = 0; draw < values.length; draw++) {
      const value = values.at(nextIndex());
      if (value === undefined) {
        throw new Error("bootstrap draw is outside the sample");
      }
      total = total.plus(value);
    }
    bootstrapMeans.push(total.div(values.length));
  }
  bootstrapMeans.sort((left, right) => left.comparedTo(right));

  const tailProbability = (1 - confidenceLevel) / 2;
  return {
    sampleSize: values.length,
    resamples,
    confidenceLevel,
    sampleMean,
    lowerBound: quantile(bootstrapMeans, tailProbability),
    upperBound: quantile(bootstrapMeans, 1 - tailProbability),
  };
}
