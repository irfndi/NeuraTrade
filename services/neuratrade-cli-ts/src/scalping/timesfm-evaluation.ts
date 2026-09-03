import type { Candle } from "../market-data/types.js";
import type { TimesFmForecastRecord } from "../services/timesfm-client.js";

export interface TimesFmEvaluationOrigin {
  readonly id: string;
  readonly index: number;
  readonly futureIndex: number;
}

export interface TimesFmEvaluationForecast {
  readonly origin: TimesFmEvaluationOrigin;
  readonly record: TimesFmForecastRecord;
}

export type EvaluationDirection = "long" | "short" | "flat";

export interface TimesFmEvaluationObservation {
  readonly originIndex: number;
  readonly originTimestamp: string;
  readonly futureTimestamp: string;
  readonly actualReturnPct: number;
  readonly pointForecastReturnPct: number;
  readonly q10ReturnPct: number | null;
  readonly q90ReturnPct: number | null;
  readonly pointDirection: EvaluationDirection;
  readonly modelDirection: EvaluationDirection;
  readonly baselineDirection: EvaluationDirection;
  readonly modelNetReturnPct: number;
  readonly baselineNetReturnPct: number;
}

export interface TimesFmEvaluationMetrics {
  readonly observations: number;
  readonly trades: number;
  readonly coveragePct: number;
  readonly winRatePct: number;
  readonly profitFactor: number | null;
  readonly netReturnPct: number;
  readonly meanAbsoluteErrorPct: number;
  readonly directionAccuracyPct: number;
}

export interface TimesFmEvaluationReport {
  readonly frictionCostPct: number;
  readonly model: TimesFmEvaluationMetrics;
  readonly baseline: TimesFmEvaluationMetrics;
  readonly observations: readonly TimesFmEvaluationObservation[];
}

function requireFinite(value: number, label: string): number {
  if (!Number.isFinite(value)) throw new Error(`${label} must be finite`);
  return value;
}

function requirePositive(value: number, label: string): number {
  requireFinite(value, label);
  if (value <= 0) throw new Error(`${label} must be positive`);
  return value;
}

function directionForReturn(
  returnPct: number,
  thresholdPct = 0,
): EvaluationDirection {
  if (returnPct > thresholdPct) return "long";
  if (returnPct < -thresholdPct) return "short";
  return "flat";
}

function signedNetReturnPct(
  direction: EvaluationDirection,
  actualReturnPct: number,
  frictionCostPct: number,
): number {
  if (direction === "long") return actualReturnPct - frictionCostPct;
  if (direction === "short") return -actualReturnPct - frictionCostPct;
  return 0;
}

function returnFromLogForecast(
  forecastLogClose: number,
  originClose: number,
): number {
  const result = (Math.exp(forecastLogClose) / originClose - 1) * 100;
  return requireFinite(result, "TimesFM forecast return");
}

function quantileReturn(
  record: TimesFmForecastRecord,
  horizonIndex: number,
  quantileIndex: number,
  originClose: number,
): number | null {
  const logClose = record.quantiles?.[0]?.[horizonIndex]?.[quantileIndex];
  return logClose === undefined
    ? null
    : returnFromLogForecast(logClose, originClose);
}

function metricsForObservations(
  observations: readonly TimesFmEvaluationObservation[],
  directionKey: "modelDirection" | "baselineDirection",
  netReturnKey: "modelNetReturnPct" | "baselineNetReturnPct",
  forecastReturn: (observation: TimesFmEvaluationObservation) => number,
): TimesFmEvaluationMetrics {
  const trades = observations.filter(
    (observation) => observation[directionKey] !== "flat",
  );
  const grossProfit = trades.reduce(
    (sum, observation) => sum + Math.max(0, observation[netReturnKey]),
    0,
  );
  const grossLoss = trades.reduce(
    (sum, observation) => sum + Math.max(0, -observation[netReturnKey]),
    0,
  );
  const winningTrades = trades.filter(
    (observation) => observation[netReturnKey] > 0,
  ).length;
  const directionMatches = observations.filter(
    (observation) =>
      observation[directionKey] !== "flat" &&
      observation[directionKey] ===
        directionForReturn(observation.actualReturnPct),
  ).length;
  const meanAbsoluteErrorPct =
    observations.length === 0
      ? 0
      : observations.reduce(
          (sum, observation) =>
            sum +
            Math.abs(forecastReturn(observation) - observation.actualReturnPct),
          0,
        ) / observations.length;
  return {
    observations: observations.length,
    trades: trades.length,
    coveragePct:
      observations.length === 0
        ? 0
        : (trades.length / observations.length) * 100,
    winRatePct: trades.length === 0 ? 0 : (winningTrades / trades.length) * 100,
    profitFactor:
      grossLoss === 0 ? (grossProfit > 0 ? null : 0) : grossProfit / grossLoss,
    netReturnPct: observations.reduce(
      (sum, observation) => sum + observation[netReturnKey],
      0,
    ),
    meanAbsoluteErrorPct,
    directionAccuracyPct:
      trades.length === 0 ? 0 : (directionMatches / trades.length) * 100,
  };
}

/**
 * Select causal forecast origins. Each origin only uses candles through
 * `index`; `futureIndex` is the first point used for the held-out evaluation.
 * Taking the last `maxOrigins` keeps a long database run bounded while still
 * evaluating the newest history.
 */
export function buildTimesFmEvaluationOrigins(
  candles: readonly Candle[],
  contextBars: number,
  horizon: number,
  stepBars: number,
  maxOrigins = 0,
): readonly TimesFmEvaluationOrigin[] {
  if (!Number.isInteger(contextBars) || contextBars < 1) {
    throw new Error("contextBars must be a positive integer");
  }
  if (!Number.isInteger(horizon) || horizon < 1) {
    throw new Error("horizon must be a positive integer");
  }
  if (!Number.isInteger(stepBars) || stepBars < 1) {
    throw new Error("stepBars must be a positive integer");
  }
  if (!Number.isInteger(maxOrigins) || maxOrigins < 0) {
    throw new Error("maxOrigins must be a non-negative integer");
  }

  const lastOrigin = candles.length - horizon - 1;
  const origins: TimesFmEvaluationOrigin[] = [];
  for (let index = contextBars - 1; index <= lastOrigin; index += stepBars) {
    origins.push({
      id: `origin-${index}`,
      index,
      futureIndex: index + horizon,
    });
  }
  return maxOrigins > 0 && origins.length > maxOrigins
    ? origins.slice(-maxOrigins)
    : origins;
}

/** Fail early instead of sending irregular windows to a regular time-series model. */
export function assertRegularCandleSeries(
  candles: readonly Candle[],
  intervalMs: number,
): void {
  if (!Number.isInteger(intervalMs) || intervalMs < 1) {
    throw new Error("intervalMs must be a positive integer");
  }
  for (let index = 1; index < candles.length; index += 1) {
    const previous = candles[index - 1];
    const current = candles[index];
    if (previous === undefined || current === undefined) continue;
    const actualGap =
      current.timestamp.getTime() - previous.timestamp.getTime();
    if (actualGap !== intervalMs) {
      throw new Error(
        `irregular candle timestamps at index ${index}: expected ${intervalMs}ms, got ${actualGap}ms`,
      );
    }
  }
}

/**
 * Score TimesFM point/quantile forecasts against future closes. The model
 * signal requires the entire q10 band above friction for longs or the q90 band
 * below negative friction for shorts; uncertain forecasts remain flat.
 * `baseline` is a one-bar momentum sign with the same friction threshold.
 * This is a diagnostic directional test, not an order-execution backtest.
 */
export function evaluateTimesFmForecasts(
  candles: readonly Candle[],
  forecasts: readonly TimesFmEvaluationForecast[],
  frictionCostPct: number,
): TimesFmEvaluationReport {
  requireFinite(frictionCostPct, "frictionCostPct");
  if (frictionCostPct < 0) {
    throw new Error("frictionCostPct cannot be negative");
  }
  const observations = forecasts.map(({ origin, record }) => {
    const source = candles[origin.index];
    const future = candles[origin.futureIndex];
    const previous = candles[origin.index - 1];
    if (
      source === undefined ||
      future === undefined ||
      previous === undefined
    ) {
      throw new Error(
        `forecast origin ${origin.id} is outside the candle data`,
      );
    }
    const originClose = requirePositive(source.close, "origin close");
    const actualReturnPct =
      (requirePositive(future.close, "future close") / originClose - 1) * 100;
    const pointForecast = record.forecast[0];
    if (pointForecast === undefined || pointForecast.length === 0) {
      throw new Error(`forecast ${origin.id} has no log-close forecast`);
    }
    const horizonIndex = pointForecast.length - 1;
    const pointForecastReturnPct = returnFromLogForecast(
      requireFinite(pointForecast[horizonIndex]!, "point forecast"),
      originClose,
    );
    const quantileRow = record.quantiles?.[0]?.[horizonIndex];
    const q10ReturnPct =
      quantileRow?.[0] === undefined
        ? null
        : quantileReturn(record, horizonIndex, 0, originClose);
    const q90ReturnPct =
      quantileRow === undefined || quantileRow.length === 0
        ? null
        : quantileReturn(
            record,
            horizonIndex,
            quantileRow.length - 1,
            originClose,
          );
    const modelDirection =
      q10ReturnPct !== null && q10ReturnPct > frictionCostPct
        ? "long"
        : q90ReturnPct !== null && q90ReturnPct < -frictionCostPct
          ? "short"
          : "flat";
    const baselineReturnPct =
      (source.close / requirePositive(previous.close, "previous close") - 1) *
      100;
    const baselineDirection = directionForReturn(
      baselineReturnPct,
      frictionCostPct,
    );
    return {
      originIndex: origin.index,
      originTimestamp: source.timestamp.toISOString(),
      futureTimestamp: future.timestamp.toISOString(),
      actualReturnPct,
      pointForecastReturnPct,
      q10ReturnPct,
      q90ReturnPct,
      pointDirection: directionForReturn(pointForecastReturnPct),
      modelDirection,
      baselineDirection,
      modelNetReturnPct: signedNetReturnPct(
        modelDirection,
        actualReturnPct,
        frictionCostPct,
      ),
      baselineNetReturnPct: signedNetReturnPct(
        baselineDirection,
        actualReturnPct,
        frictionCostPct,
      ),
    } satisfies TimesFmEvaluationObservation;
  });

  return {
    frictionCostPct,
    model: metricsForObservations(
      observations,
      "modelDirection",
      "modelNetReturnPct",
      (observation) => observation.pointForecastReturnPct,
    ),
    baseline: metricsForObservations(
      observations,
      "baselineDirection",
      "baselineNetReturnPct",
      () => 0,
    ),
    observations,
  };
}
