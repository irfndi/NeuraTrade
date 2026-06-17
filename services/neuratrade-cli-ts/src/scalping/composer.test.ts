import { describe, expect, it } from "bun:test";
import {
  composeSignal,
  defaultComposerConfig,
  validateWeights,
} from "./composer.js";
import type { CandleLike, OHLCVInput, OrderBookMetricsInput } from "./types.js";

function makeCandles(
  count: number,
  baseClose = 100,
  trend: "up" | "down" | "flat" = "flat",
): CandleLike[] {
  const candles: CandleLike[] = [];
  let close = baseClose;
  for (let i = 0; i < count; i++) {
    const open = close;
    if (trend === "up") close *= 1.01;
    else if (trend === "down") close *= 0.99;
    const high = Math.max(open, close) * 1.002;
    const low = Math.min(open, close) * 0.998;
    candles.push({
      open,
      high,
      low,
      close,
      volume: 10,
      timestamp: new Date(Date.now() + i * 60000),
    });
  }
  return candles;
}

function makeOB(
  overrides: Partial<OrderBookMetricsInput> = {},
): OrderBookMetricsInput {
  return {
    exchange: "binance",
    symbol: "BTC/USDT",
    spread: 10,
    spreadPercent: 0.0001,
    bidDepth: 100,
    askDepth: 100,
    imbalance: 0,
    midPrice: 67000,
    timestamp: new Date(),
    ...overrides,
  };
}

function makeOHLCV(candles: CandleLike[]): OHLCVInput {
  return {
    exchange: "binance",
    symbol: "BTC/USDT",
    timeframe: "1m",
    candles,
  };
}

describe("ScalpingSignalComposer", () => {
  it("returns null with insufficient candles", () => {
    const signal = composeSignal(makeOHLCV(makeCandles(1)), makeOB());
    expect(signal).toBeNull();
  });

  it("returns null with invalid weights", () => {
    const config = {
      ...defaultComposerConfig,
      weights: { ...defaultComposerConfig.weights, spread: 0.5 },
    };
    const signal = composeSignal(makeOHLCV(makeCandles(20)), makeOB(), config);
    expect(signal).toBeNull();
  });

  it("produces a buy signal on strong uptrend + positive imbalance", () => {
    const candles = makeCandles(30, 100, "up");
    const ob = makeOB({
      imbalance: 0.3,
      spreadPercent: 0.0003,
      bidDepth: 80,
      askDepth: 80,
    });
    const signal = composeSignal(makeOHLCV(candles), ob);

    expect(signal).not.toBeNull();
    expect(signal!.direction).toBe("buy");
    expect(signal!.confidence).toBeGreaterThan(0.5);
    expect(signal!.components.length).toBeGreaterThanOrEqual(4);
  });

  it("produces sell trend and imbalance components on downtrend + negative imbalance", () => {
    const candles = makeCandles(30, 100, "down");
    const ob = makeOB({
      imbalance: -0.25,
      spreadPercent: 0.0003,
      bidDepth: 80,
      askDepth: 80,
    });
    const signal = composeSignal(makeOHLCV(candles), ob);

    expect(signal).not.toBeNull();
    const componentNames = signal!.components.map((c) => c.name);
    expect(componentNames).toContain("trend");
    expect(componentNames).toContain("imbalance");

    const trendComponent = signal!.components.find((c) => c.name === "trend");
    expect(trendComponent?.signal).toBe("sell");

    const imbalanceComponent = signal!.components.find(
      (c) => c.name === "imbalance",
    );
    expect(imbalanceComponent?.signal).toBe("sell");
  });

  it("holds when signals are mixed", () => {
    const candles = makeCandles(30, 100, "flat");
    const ob = makeOB({ imbalance: 0.02, spreadPercent: 0.0015 });
    const signal = composeSignal(makeOHLCV(candles), ob);

    expect(signal).not.toBeNull();
    expect(signal!.direction).toBe("hold");
  });

  it("includes microstructure context", () => {
    const ob = makeOB({ spread: 5, imbalance: 0.1 });
    const signal = composeSignal(makeOHLCV(makeCandles(30)), ob);

    expect(signal).not.toBeNull();
    expect(signal!.microstructure.spread).toBe(5);
    expect(signal!.microstructure.imbalance).toBe(0.1);
  });

  it("drops synthetic order-book weights to avoid candle-derived noise", () => {
    const candles = makeCandles(60, 100, "up");
    const ob = makeOB({
      exchange: "synthetic",
      imbalance: -0.3,
      spreadPercent: 0.002,
      bidDepth: 10,
      askDepth: 10,
    });
    const signal = composeSignal(makeOHLCV(candles), ob);

    expect(signal).not.toBeNull();
    expect(signal!.direction).toBe("buy");
    expect(Math.abs(signal!.attributionWeights.spread)).toBeCloseTo(0, 10);
    expect(Math.abs(signal!.attributionWeights.imbalance)).toBeCloseTo(0, 10);
    expect(Math.abs(signal!.attributionWeights.liquidity)).toBeCloseTo(0, 10);
  });

  it("filters low-volume candles with volumeMinRatio", () => {
    const candles = makeCandles(30, 100, "up").map((c, i) => ({
      ...c,
      volume: i === 29 ? 1 : 100,
    }));
    const config = {
      ...defaultComposerConfig,
      thresholds: {
        ...defaultComposerConfig.thresholds,
        volumeMinRatio: 1.0,
        volumeLookback: 20,
      },
    };
    const signal = composeSignal(makeOHLCV(candles), makeOB(), config);

    expect(signal).toBeNull();
  });

  it("requires entry candle confirmation when enabled", () => {
    const candles = makeCandles(30, 100, "up");
    // Force the last candle to be a red candle in an uptrend.
    candles[candles.length - 1] = {
      ...candles[candles.length - 1],
      open: candles[candles.length - 1].close * 1.01,
      close: candles[candles.length - 1].close * 0.99,
      high: candles[candles.length - 1].close * 1.02,
      low: candles[candles.length - 1].close * 0.98,
    };
    const config = {
      ...defaultComposerConfig,
      thresholds: {
        ...defaultComposerConfig.thresholds,
        entryCandleConfirm: true,
      },
    };
    const signal = composeSignal(makeOHLCV(candles), makeOB(), config);

    expect(signal).toBeNull();
  });

  it("requires momentum confirmation when enabled", () => {
    const candles = makeCandles(50, 100, "up");
    // Make the net return over the last 3 candles negative while keeping the
    // longer-term trend positive.
    const anchor = candles[candles.length - 4].close;
    const c = candles[candles.length - 1];
    candles[candles.length - 1] = {
      ...c,
      open: anchor * 1.002,
      close: anchor * 0.995,
      high: anchor * 1.003,
      low: anchor * 0.994,
    };
    const config = {
      ...defaultComposerConfig,
      thresholds: {
        ...defaultComposerConfig.thresholds,
        momentumConfirmBars: 3,
      },
    };
    const ob = makeOB({ exchange: "synthetic" });
    const signal = composeSignal(makeOHLCV(candles), ob, config);

    expect(signal).toBeNull();
  });

  it("requires confluence when minConfluence is set", () => {
    // Strong uptrend with synthetic OB gives a buy signal from a handful of
    // components; demanding 10 agreeing components suppresses it.
    const candles = makeCandles(40, 100, "up");
    const ob = makeOB({ exchange: "synthetic" });
    const config = {
      ...defaultComposerConfig,
      thresholds: {
        ...defaultComposerConfig.thresholds,
        minConfluence: 10,
      },
    };
    const signal = composeSignal(makeOHLCV(candles), ob, config);

    expect(signal).toBeNull();
  });
});

describe("validateWeights", () => {
  it("accepts default weights", () => {
    expect(validateWeights(defaultComposerConfig.weights)).toBe(true);
  });

  it("rejects weights that do not sum to 1", () => {
    expect(
      validateWeights({ ...defaultComposerConfig.weights, spread: 0.5 }),
    ).toBe(false);
  });
});
