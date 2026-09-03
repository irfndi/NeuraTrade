import { describe, expect, it } from "bun:test";
import type { Candle } from "../market-data/types.js";
import { buildTimesFmRequest } from "./timesfm.js";

function candles(count: number): Candle[] {
  return Array.from({ length: count }, (_, index) => ({
    exchange: "bybit-futures",
    symbol: "BTC/USDT:USDT",
    timeframe: "1m",
    open: 100 + index,
    high: 101 + index,
    low: 99 + index,
    close: 100 + index,
    volume: index,
    timestamp: new Date(1_700_000_000_000 + index * 60_000),
  }));
}

describe("TimesFM forecast request", () => {
  it("builds a regular multivariate log-price request without future bars", () => {
    const request = buildTimesFmRequest({
      exchange: "bybit-futures",
      symbol: "BTC/USDT:USDT",
      timeframe: "1m",
      horizon: 4,
      useZnorm: false,
      symmetricAveraging: true,
      candles: candles(32),
    });

    expect(request.intervalMs).toBe(60_000);
    expect(request.series).toHaveLength(1);
    expect(request.series[0]?.targets).toHaveLength(2);
    expect(request.series[0]?.targets[0]?.[0]).toBeCloseTo(Math.log(100));
    expect(request.series[0]?.targets[1]?.[31]).toBeCloseTo(Math.log1p(31));
    expect(request.series[0]?.timestampsMs.at(-1)).toBe(
      1_700_000_000_000 + 31 * 60_000,
    );
  });

  it("can send volume as a past-only covariate", () => {
    const request = buildTimesFmRequest({
      exchange: "bybit-futures",
      symbol: "BTC/USDT:USDT",
      timeframe: "1m",
      horizon: 4,
      useZnorm: false,
      symmetricAveraging: true,
      volumeAsPastCovariate: true,
      candles: candles(32),
    });

    expect(request.series[0]?.targets).toHaveLength(1);
    expect(request.series[0]?.targetNames).toEqual(["log_close"]);
    expect(request.series[0]?.pastOnlyCovariates).toEqual([
      candles(32).map((candle) => Math.log1p(candle.volume)),
    ]);
    expect(request.series[0]?.pastFutureCovariates).toBeUndefined();
  });

  it("rejects a non-positive close before the log transform", () => {
    const input = candles(32);
    input[3] = { ...input[3]!, close: 0 };

    expect(() =>
      buildTimesFmRequest({
        exchange: "bybit-futures",
        symbol: "BTC/USDT:USDT",
        timeframe: "1m",
        horizon: 4,
        useZnorm: false,
        symmetricAveraging: true,
        candles: input,
      }),
    ).toThrow("candle close must be positive");
  });
});
