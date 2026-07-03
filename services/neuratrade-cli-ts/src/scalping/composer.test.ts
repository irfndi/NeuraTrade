import { describe, expect, it } from "bun:test";
import {
  composeSignal,
  defaultComposerConfig,
  validateWeights,
} from "./composer.js";
import type {
  CandleLike,
  ComposerConfig,
  ComposerThresholds,
  OHLCVInput,
  OrderBookMetricsInput,
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

function makeConnorsCandles(closes: number[]): CandleLike[] {
  return closes.map((close, i) => ({
    open: close,
    high: close,
    low: close,
    close,
    volume: 10,
    timestamp: new Date(Date.now() + i * 60000),
  }));
}

function connorsConfig(
  thresholdOverrides: Partial<ComposerThresholds> = {},
): ComposerConfig {
  return {
    weights: {
      ...defaultComposerConfig.weights,
      connorsRsi2: 1,
      spread: 0,
      imbalance: 0,
      volatility: 0,
      trend: 0,
      liquidity: 0,
      rsi: 0,
      rsiPullback: 0,
      emaPullback: 0,
      regime: 0,
      funding: 0,
    },
    thresholds: {
      ...defaultComposerConfig.thresholds,
      trendFilterPeriod: 3,
      ...thresholdOverrides,
    },
    enabled: {
      connorsRsi2: true,
      spread: false,
      imbalance: false,
      volatility: false,
      trend: false,
      liquidity: false,
      rsi: false,
      rsiPullback: false,
      emaPullback: false,
      regime: false,
      funding: false,
    },
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

  it("produces a buy signal in breakout mode on high-volume range break", () => {
    const candles = makeCandles(60, 100, "up");
    const lookback = 20;
    const window = candles.slice(-lookback - 1, -1);
    const highestHigh = Math.max(...window.map((c) => c.high));

    // Make the last candle break above the lookback high with high volume.
    const lastIndex = candles.length - 1;
    candles[lastIndex] = {
      ...candles[lastIndex],
      open: highestHigh * 0.999,
      high: highestHigh * 1.02,
      low: highestHigh * 0.998,
      close: highestHigh * 1.01,
      volume: 100,
    };

    // Ensure the earlier volume average is low enough that 100x is above the
    // minimum ratio threshold.
    for (let i = 0; i < candles.length - 1; i++) {
      candles[i] = { ...candles[i], volume: 1 };
    }

    const config = {
      ...defaultComposerConfig,
      enabled: { ...defaultComposerConfig.enabled, regime: true },
      thresholds: {
        ...defaultComposerConfig.thresholds,
        regimeMode: "breakout" as const,
        breakoutLookback: lookback,
        breakoutVolumeMinRatio: 1.2,
        breakoutAdxMin: 20,
      },
    };

    const signal = composeSignal(makeOHLCV(candles), makeOB(), config);

    expect(signal).not.toBeNull();
    const regimeComponent = signal!.components.find((c) => c.name === "regime");
    expect(regimeComponent).toBeDefined();
    expect(regimeComponent!.signal).toBe("buy");
    expect(signal!.direction).toBe("buy");
  });

  it("requires category confluence when minCategoryConfluence is set", () => {
    // Strong uptrend with synthetic OB yields components only from the
    // current and lagging categories. Demanding all three categories
    // (leading + current + lagging) suppresses the signal.
    const candles = makeCandles(40, 100, "up");
    const ob = makeOB({ exchange: "synthetic" });
    const config = {
      ...defaultComposerConfig,
      thresholds: {
        ...defaultComposerConfig.thresholds,
        minCategoryConfluence: 3,
      },
    };
    const signal = composeSignal(makeOHLCV(candles), ob, config);

    expect(signal).toBeNull();
  });

  it("ignores disabled indicators and re-normalizes active weights", () => {
    const candles = makeCandles(30, 100, "up");
    const ob = makeOB({
      imbalance: 0.3,
      spreadPercent: 0.0003,
      bidDepth: 80,
      askDepth: 80,
    });
    const config = {
      ...defaultComposerConfig,
      enabled: { trend: false },
    };
    const signal = composeSignal(makeOHLCV(candles), ob, config);

    expect(signal).not.toBeNull();
    expect(signal!.components.map((c) => c.name)).not.toContain("trend");
    expect(signal!.direction).toBe("buy");
  });

  it("emits a buy signal when RSI(2) is deeply oversold", () => {
    const candles: CandleLike[] = [];
    let close = 100;
    for (let i = 0; i < 20; i++) {
      const open = close;
      close *= 0.95; // strong consecutive down moves
      candles.push({
        open,
        high: Math.max(open, close) * 1.002,
        low: Math.min(open, close) * 0.998,
        close,
        volume: 100,
        timestamp: new Date(Date.now() + i * 60000),
      });
    }
    const config = {
      ...defaultComposerConfig,
      enabled: {
        spread: false,
        imbalance: false,
        volatility: false,
        trend: false,
        liquidity: false,
        rsi: true,
        rsiPullback: false,
        emaPullback: false,
        regime: false,
        funding: false,
      },
      thresholds: {
        ...defaultComposerConfig.thresholds,
        rsiPeriod: 2,
        rsiOversoldStrong: 5,
        rsiOverboughtStrong: 95,
        minConfidenceSpread: 0.1,
      },
    };
    const signal = composeSignal(
      makeOHLCV(candles),
      makeOB({ exchange: "synthetic" }),
      config,
    );

    expect(signal).not.toBeNull();
    expect(signal!.direction).toBe("buy");
    const rsi = signal!.components.find((c) => c.name === "rsi");
    expect(rsi).toBeDefined();
    expect(rsi!.signal).toBe("buy");
    expect(rsi!.strength).toBe("strong");
  });

  it("emits a sell signal when RSI(2) is deeply overbought", () => {
    const candles: CandleLike[] = [];
    let close = 100;
    for (let i = 0; i < 20; i++) {
      const open = close;
      close *= 1.05; // strong consecutive up moves
      candles.push({
        open,
        high: Math.max(open, close) * 1.002,
        low: Math.min(open, close) * 0.998,
        close,
        volume: 100,
        timestamp: new Date(Date.now() + i * 60000),
      });
    }
    const config = {
      ...defaultComposerConfig,
      enabled: {
        spread: false,
        imbalance: false,
        volatility: false,
        trend: false,
        liquidity: false,
        rsi: true,
        rsiPullback: false,
        emaPullback: false,
        regime: false,
        funding: false,
      },
      thresholds: {
        ...defaultComposerConfig.thresholds,
        rsiPeriod: 2,
        rsiOversoldStrong: 5,
        rsiOverboughtStrong: 95,
        minConfidenceSpread: 0.1,
      },
    };
    const signal = composeSignal(
      makeOHLCV(candles),
      makeOB({ exchange: "synthetic" }),
      config,
    );

    expect(signal).not.toBeNull();
    expect(signal!.direction).toBe("sell");
    const rsi = signal!.components.find((c) => c.name === "rsi");
    expect(rsi).toBeDefined();
    expect(rsi!.signal).toBe("sell");
    expect(rsi!.strength).toBe("strong");
  });
});

describe("funding bias component", () => {
  function fundingConfig(
    overrides: {
      fundingWeight?: number;
      useFunding?: boolean;
      threshold?: number;
    } = {},
  ) {
    return {
      ...defaultComposerConfig,
      weights: {
        ...defaultComposerConfig.weights,
        funding: overrides.fundingWeight ?? 1,
      },
      enabled: {
        spread: false,
        imbalance: false,
        liquidity: false,
        volatility: false,
        trend: false,
        rsi: false,
        rsiPullback: false,
        emaPullback: false,
        regime: false,
        funding: true,
      },
      thresholds: {
        ...defaultComposerConfig.thresholds,
        useFunding: overrides.useFunding ?? true,
        fundingBiasThreshold: overrides.threshold ?? 0.0001,
      },
    };
  }

  it("is ignored when useFunding is false", () => {
    const candles = makeCandles(30, 100, "flat");
    const config = fundingConfig({ useFunding: false });
    const signal = composeSignal(
      {
        ...makeOHLCV(candles),
        fundingRates: [
          {
            exchange: "binance",
            symbol: "BTC/USDT",
            fundingRate: 0.001,
            timestamp: candles[candles.length - 1].timestamp,
          },
        ],
      },
      makeOB(),
      config,
    );
    expect(signal).toBeNull();
  });

  it("emits a sell signal when funding is strongly positive", () => {
    const candles = makeCandles(30, 100, "flat");
    const config = fundingConfig();
    const signal = composeSignal(
      {
        ...makeOHLCV(candles),
        fundingRates: [
          {
            exchange: "binance",
            symbol: "BTC/USDT",
            fundingRate: 0.0005,
            timestamp: candles[candles.length - 1].timestamp,
          },
        ],
      },
      makeOB(),
      config,
    );
    expect(signal).not.toBeNull();
    const funding = signal!.components.find((c) => c.name === "funding");
    expect(funding).toBeDefined();
    expect(funding!.signal).toBe("sell");
  });

  it("emits a buy signal when funding is strongly negative", () => {
    const candles = makeCandles(30, 100, "flat");
    const config = fundingConfig();
    const signal = composeSignal(
      {
        ...makeOHLCV(candles),
        fundingRates: [
          {
            exchange: "binance",
            symbol: "BTC/USDT",
            fundingRate: -0.0005,
            timestamp: candles[candles.length - 1].timestamp,
          },
        ],
      },
      makeOB(),
      config,
    );
    expect(signal).not.toBeNull();
    const funding = signal!.components.find((c) => c.name === "funding");
    expect(funding).toBeDefined();
    expect(funding!.signal).toBe("buy");
  });

  it("is ignored when the rate is within the threshold", () => {
    const candles = makeCandles(30, 100, "flat");
    const config = fundingConfig();
    const signal = composeSignal(
      {
        ...makeOHLCV(candles),
        fundingRates: [
          {
            exchange: "binance",
            symbol: "BTC/USDT",
            fundingRate: 0.00005,
            timestamp: candles[candles.length - 1].timestamp,
          },
        ],
      },
      makeOB(),
      config,
    );
    expect(signal).toBeNull();
  });

  it("returns strong strength for extreme funding rates", () => {
    const candles = makeCandles(30, 100, "flat");
    const config = fundingConfig();
    const signal = composeSignal(
      {
        ...makeOHLCV(candles),
        fundingRates: [
          {
            exchange: "binance",
            symbol: "BTC/USDT",
            fundingRate: 0.0004,
            timestamp: candles[candles.length - 1].timestamp,
          },
        ],
      },
      makeOB(),
      config,
    );
    const funding = signal!.components.find((c) => c.name === "funding");
    expect(funding).toBeDefined();
    expect(funding!.strength).toBe("strong");
  });
});

describe("Connors RSI(2) component", () => {
  it("emits a buy signal when close is above the trend filter and RSI(2) is oversold", () => {
    const candles = makeConnorsCandles([40, 30, 10, 28]);
    const signal = composeSignal(
      { ...makeOHLCV(candles), exchange: "synthetic" },
      { ...makeOB(), exchange: "synthetic" },
      connorsConfig({ entryRsiLongThreshold: 55 }),
    );
    expect(signal).not.toBeNull();
    expect(signal!.direction).toBe("buy");
    const connors = signal!.components.find((c) => c.name === "connorsRsi2");
    expect(connors).toBeDefined();
    expect(connors!.signal).toBe("buy");
  });

  it("emits a sell signal when close is below the trend filter and RSI(2) is overbought", () => {
    const candles = makeConnorsCandles([60, 70, 100, 81]);
    const signal = composeSignal(
      { ...makeOHLCV(candles), exchange: "synthetic" },
      { ...makeOB(), exchange: "synthetic" },
      connorsConfig({ entryRsiShortThreshold: 45 }),
    );
    expect(signal).not.toBeNull();
    expect(signal!.direction).toBe("sell");
    const connors = signal!.components.find((c) => c.name === "connorsRsi2");
    expect(connors).toBeDefined();
    expect(connors!.signal).toBe("sell");
  });

  it("returns null when RSI(2) is not extreme enough", () => {
    const candles = makeConnorsCandles([40, 30, 10, 35]);
    const signal = composeSignal(
      { ...makeOHLCV(candles), exchange: "synthetic" },
      { ...makeOB(), exchange: "synthetic" },
      connorsConfig({ entryRsiLongThreshold: 30 }),
    );
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
