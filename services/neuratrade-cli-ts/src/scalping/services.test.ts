import { describe, expect, it } from "bun:test";
import { Effect, Result } from "effect";
import { runBacktest, type BacktestOptions } from "./backtest.js";
import { runGridBacktest, type GridOptions } from "./grid.js";
import { composeSignal, defaultComposerConfig } from "./composer.js";
import {
  checkRsiExit,
  computeExitLevels,
  type ExitEngineOptions,
} from "./exit-engine.js";
import {
  buildBacktestArgsFromTemplate,
  buildComposerConfigFromTemplate,
  listStrategies,
  type StrategyTemplateName,
} from "./strategy-library.js";
import type { ResolvedBacktestArgs } from "./strategy-profile.js";
import {
  BacktestEngine,
  BacktestEngineLive,
  ExitEngine,
  ExitEngineLive,
  SignalComposer,
  SignalComposerLive,
  StrategyLibrary,
  StrategyLibraryLive,
} from "./services.js";
import type {
  CandleLike,
  OHLCVInput,
  OrderBookMetricsInput,
  ScalpingSignal,
} from "./types.js";

function makeCandles(
  count: number,
  baseClose = 100,
  trend: "up" | "down" | "flat" = "flat",
): CandleLike[] {
  const candles: CandleLike[] = [];
  let close = baseClose;
  for (let i = 0; i < count; i++) {
    const open = close;
    if (trend === "up") close *= 1.005;
    else if (trend === "down") close *= 0.995;
    const high = Math.max(open, close) * 1.002;
    const low = Math.min(open, close) * 0.998;
    candles.push({
      open,
      high,
      low,
      close,
      volume: 10,
      timestamp: new Date(Date.UTC(2026, 0, 1) + i * 60000),
    });
  }
  return candles;
}

function makeOscillatingCandles(
  count: number,
  mid = 100.5,
  amplitude = 0.5,
): CandleLike[] {
  const candles: CandleLike[] = [];
  for (let i = 0; i < count; i++) {
    const side = i % 2 === 0 ? 1 : -1;
    const high = mid + amplitude * side + 0.05;
    const low = mid - amplitude * side - 0.05;
    const open = mid - (amplitude / 2) * side;
    const close = mid + (amplitude / 2) * side;
    candles.push({
      open,
      high,
      low,
      close,
      volume: 1,
      timestamp: new Date(Date.UTC(2026, 0, 1) + i * 15 * 60 * 1000),
    });
  }
  return candles;
}

function makeBacktestOptions(candles: CandleLike[]): BacktestOptions {
  return {
    symbol: "BTC/USDT",
    exchange: "binance",
    timeframe: "1h",
    candles,
    composerConfig: defaultComposerConfig,
    initialCapital: 10000,
    positionSizePct: 100,
    stopLossPct: 5,
    takeProfitPct: 10,
    feePct: 0.1,
    minConfidence: 0.1,
  };
}

function makeObMetrics(candle: CandleLike): OrderBookMetricsInput {
  return {
    exchange: "binance",
    symbol: "BTC/USDT",
    spread: 0.01,
    spreadPercent: 0.0001,
    bidDepth: 60,
    askDepth: 40,
    imbalance: 0.2,
    midPrice: candle.close,
    timestamp: candle.timestamp,
  };
}

describe("BacktestEngine", () => {
  it("runBacktest delegates to the pure engine with identical results", async () => {
    const candles = makeCandles(120, 100, "up");
    const options = makeBacktestOptions(candles);
    const expected = runBacktest(options);

    const result = await Effect.runPromise(
      Effect.gen(function* () {
        const engine = yield* BacktestEngine;
        return yield* engine.runBacktest(options);
      }).pipe(Effect.provide(BacktestEngineLive)),
    );

    expect(result).toEqual(expected);
  });

  it("runBacktest with a fraction-like fee still normalizes like the pure engine", async () => {
    const candles = makeCandles(120, 100, "up");
    const fractional = makeBacktestOptions(candles);
    const percent = { ...fractional, feePct: 0.1 };
    const withFraction = { ...fractional, feePct: 0.001 };

    const [fractionalResult, percentResult] = await Effect.runPromise(
      Effect.gen(function* () {
        const engine = yield* BacktestEngine;
        const a = yield* engine.runBacktest(withFraction);
        const b = yield* engine.runBacktest(percent);
        return [a, b] as const;
      }).pipe(Effect.provide(BacktestEngineLive)),
    );

    expect(fractionalResult.totalTrades).toBeGreaterThan(0);
    expect(percentResult.totalFeesPaid).toBeCloseTo(
      fractionalResult.totalFeesPaid,
      0,
    );
  });

  it("runGridBacktest delegates to the pure engine with identical results", async () => {
    const candles = makeOscillatingCandles(300);
    const options: GridOptions = {
      gridStepPct: 0.5,
      gridMaxGrids: 2,
      gridPauseAfterLossBars: 0,
      feePct: 0.04,
      slippageBps: 1,
      initialCapital: 20,
      trendFilterPeriod: 96,
      leverage: 1,
    };
    const expected = runGridBacktest(candles, options);

    const result = await Effect.runPromise(
      Effect.gen(function* () {
        const engine = yield* BacktestEngine;
        return yield* engine.runGridBacktest(candles, options);
      }).pipe(Effect.provide(BacktestEngineLive)),
    );

    expect(result).toEqual(expected);
  });
});

describe("SignalComposer", () => {
  // composeSignal stamps each signal with a random UUID and the current time,
  // so per-call outputs differ in `id`/`generatedAt` even for identical inputs.
  const withoutVolatileFields = (signal: ScalpingSignal | null) => {
    if (signal === null) return null;
    const { id: _id, generatedAt: _generatedAt, ...rest } = signal;
    return rest;
  };

  it("composeSignal delegates to the pure composer with identical results", async () => {
    const candles = makeCandles(60, 100, "up");
    const ohlcv: OHLCVInput = {
      exchange: "binance",
      symbol: "BTC/USDT",
      timeframe: "1h",
      candles,
    };
    const obMetrics = makeObMetrics(candles[candles.length - 1]);
    const expected = composeSignal(ohlcv, obMetrics, defaultComposerConfig);

    const result = await Effect.runPromise(
      Effect.gen(function* () {
        const composer = yield* SignalComposer;
        return yield* composer.composeSignal(
          ohlcv,
          obMetrics,
          defaultComposerConfig,
        );
      }).pipe(Effect.provide(SignalComposerLive)),
    );

    expect(withoutVolatileFields(result)).toEqual(
      withoutVolatileFields(expected),
    );
  });

  it("composeSignal uses the default config when none is given", async () => {
    const candles = makeCandles(60, 100, "up");
    const ohlcv: OHLCVInput = {
      exchange: "binance",
      symbol: "BTC/USDT",
      timeframe: "1h",
      candles,
    };
    const obMetrics = makeObMetrics(candles[candles.length - 1]);

    const result = await Effect.runPromise(
      Effect.gen(function* () {
        const composer = yield* SignalComposer;
        return yield* composer.composeSignal(ohlcv, obMetrics);
      }).pipe(Effect.provide(SignalComposerLive)),
    );

    expect(withoutVolatileFields(result)).toEqual(
      withoutVolatileFields(composeSignal(ohlcv, obMetrics)),
    );
  });
});

describe("ExitEngine", () => {
  const baseExitOptions = (): ExitEngineOptions => ({
    side: "long",
    entryPrice: 100,
    atr: null,
    useAtr: false,
    atrStopMultiplier: 1.5,
    atrRiskReward: 2,
    stopLossPct: 1,
    takeProfitPct: 2,
    scaleOutAtR: 0,
    candles: makeCandles(60, 100, "up"),
    volatilityLookback: 0,
    volatilityLowPct: 20,
    volatilityHighPct: 80,
    volatilityLowFactor: 0.8,
    volatilityHighFactor: 1.2,
  });

  it("computeExitLevels delegates to the pure engine with identical results", async () => {
    const options = baseExitOptions();
    const expected = computeExitLevels(options);

    const result = await Effect.runPromise(
      Effect.gen(function* () {
        const engine = yield* ExitEngine;
        return yield* engine.computeExitLevels(options);
      }).pipe(Effect.provide(ExitEngineLive)),
    );

    expect(result).toEqual(expected);
  });

  it("checkRsiExit delegates to the pure engine with identical results", async () => {
    const options = {
      side: "long" as const,
      candles: makeCandles(60, 100, "up"),
      exitRsiPeriod: 2,
      exitRsiLongLevel: 60,
      exitRsiShortLevel: 40,
    };
    const expected = checkRsiExit(options);

    const result = await Effect.runPromise(
      Effect.gen(function* () {
        const engine = yield* ExitEngine;
        return yield* engine.checkRsiExit(options);
      }).pipe(Effect.provide(ExitEngineLive)),
    );

    expect(result).toBe(expected);
  });
});

describe("StrategyLibrary", () => {
  const baseArgs: Partial<ResolvedBacktestArgs> = {
    regimeMode: "trend",
  };

  it("listStrategies delegates to the pure library with identical results", async () => {
    const result = await Effect.runPromise(
      Effect.gen(function* () {
        const library = yield* StrategyLibrary;
        return yield* library.listStrategies();
      }).pipe(Effect.provide(StrategyLibraryLive)),
    );

    expect(result).toEqual(listStrategies());
  });

  it("buildBacktestArgsFromTemplate delegates to the pure library", async () => {
    const result = await Effect.runPromise(
      Effect.gen(function* () {
        const library = yield* StrategyLibrary;
        return yield* library.buildBacktestArgsFromTemplate(
          "breakout",
          baseArgs,
        );
      }).pipe(Effect.provide(StrategyLibraryLive)),
    );

    expect(result).toEqual(buildBacktestArgsFromTemplate("breakout", baseArgs));
  });

  it("buildComposerConfigFromTemplate delegates to the pure library", async () => {
    const result = await Effect.runPromise(
      Effect.gen(function* () {
        const library = yield* StrategyLibrary;
        return yield* library.buildComposerConfigFromTemplate("emaPullback");
      }).pipe(Effect.provide(StrategyLibraryLive)),
    );

    expect(result).toEqual(buildComposerConfigFromTemplate("emaPullback"));
  });

  it("buildBacktestArgsFromTemplate fails with a typed Error for unknown templates", async () => {
    const result = await Effect.runPromise(
      Effect.gen(function* () {
        const library = yield* StrategyLibrary;
        return yield* Effect.result(
          library.buildBacktestArgsFromTemplate(
            "doesNotExist" as StrategyTemplateName,
            baseArgs,
          ),
        );
      }).pipe(Effect.provide(StrategyLibraryLive)),
    );

    expect(Result.isFailure(result)).toBe(true);
    if (Result.isFailure(result)) {
      expect(result.failure).toBeInstanceOf(Error);
      expect(result.failure.message).toContain(
        "Unknown strategy template: doesNotExist",
      );
    }
  });

  it("buildComposerConfigFromTemplate fails with a typed Error for unknown templates", async () => {
    const result = await Effect.runPromise(
      Effect.gen(function* () {
        const library = yield* StrategyLibrary;
        return yield* Effect.result(
          library.buildComposerConfigFromTemplate(
            "doesNotExist" as StrategyTemplateName,
          ),
        );
      }).pipe(Effect.provide(StrategyLibraryLive)),
    );

    expect(Result.isFailure(result)).toBe(true);
    if (Result.isFailure(result)) {
      expect(result.failure).toBeInstanceOf(Error);
      expect(result.failure.message).toContain(
        "Unknown strategy template: doesNotExist",
      );
    }
  });
});
