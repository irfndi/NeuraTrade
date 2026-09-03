import { describe, expect, it } from "bun:test";
import type { Candle } from "../market-data/types.js";
import {
  assertRegularCandleSeries,
  buildTimesFmEvaluationOrigins,
  evaluateTimesFmForecasts,
} from "./timesfm-evaluation.js";
import type { TimesFmForecastRecord } from "../services/timesfm-client.js";

function makeCandles(count: number): Candle[] {
  return Array.from({ length: count }, (_, index) => ({
    exchange: "bybit-futures",
    symbol: "BTC/USDT:USDT",
    timeframe: "1m",
    open: 100 + index,
    high: 101 + index,
    low: 99 + index,
    close: 100 + index,
    volume: 10,
    timestamp: new Date(index * 60_000),
  }));
}

function forecastRecord(
  id: string,
  terminalClose: number,
): TimesFmForecastRecord {
  return {
    id,
    targetNames: ["log_close", "log_volume"],
    timestampsMs: [0],
    forecast: [[Math.log(terminalClose)], [Math.log(10)]],
    quantiles: [
      [[Math.log(terminalClose * 0.99), Math.log(terminalClose * 1.01)]],
      [[Math.log(10), Math.log(10)]],
    ],
  };
}

describe("TimesFM walk-forward evaluation", () => {
  it("selects causal origins and bounds them to the available future", () => {
    const candles = makeCandles(10);
    const origins = buildTimesFmEvaluationOrigins(candles, 3, 2, 2, 2);

    expect(origins.map((origin) => origin.index)).toEqual([4, 6]);
    expect(origins.map((origin) => origin.futureIndex)).toEqual([6, 8]);
    expect(origins[0]?.index).toBeLessThan(origins[0]?.futureIndex ?? -1);
  });

  it("scores quantile-gated model signals against a friction-aware baseline", () => {
    const candles = makeCandles(40);
    const origins = buildTimesFmEvaluationOrigins(candles, 32, 2, 2, 1);
    const origin = origins[0]!;
    const futureClose = candles[origin.futureIndex]!.close;
    const report = evaluateTimesFmForecasts(
      candles,
      [{ origin, record: forecastRecord(origin.id, futureClose) }],
      0.2,
    );

    expect(report.model.trades).toBe(1);
    expect(report.model.coveragePct).toBe(100);
    expect(report.model.winRatePct).toBe(100);
    expect(report.model.netReturnPct).toBeGreaterThan(0);
    expect(report.model.directionAccuracyPct).toBe(100);
    expect(report.observations[0]?.pointForecastReturnPct).toBeGreaterThan(0);
  });

  it("rejects irregular timestamps before a regular-model request", () => {
    const candles = makeCandles(3);
    candles[2] = { ...candles[2]!, timestamp: new Date(180_000) };

    expect(() => assertRegularCandleSeries(candles, 60_000)).toThrow(
      "irregular candle timestamps",
    );
  });
});
