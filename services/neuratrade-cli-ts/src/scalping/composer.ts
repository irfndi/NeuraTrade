import { randomUUID } from "node:crypto";
import type {
  CandleLike,
  ComposerConfig,
  ComposerThresholds,
  ComposerWeights,
  Direction,
  OHLCVInput,
  OrderBookMetricsInput,
  ScalpingSignal,
  SignalComponent,
  SignalStrength,
} from "./types.js";
import {
  calculateADX,
  calculateATR,
  calculateBollingerBands,
  calculateEMA,
  calculateRSI,
  calculateVolatility,
} from "./indicators.js";

const defaultWeights: ComposerWeights = {
  spread: 0.18,
  imbalance: 0.22,
  volatility: 0.13,
  trend: 0.18,
  liquidity: 0.09,
  rsi: 0.09,
  regime: 0.11,
};

const defaultThresholds: ComposerThresholds = {
  spreadTightPct: 0.0005,
  spreadModeratePct: 0.001,
  spreadWidePct: 0.002,
  imbalanceWeak: 0.05,
  imbalanceStrong: 0.2,
  volatilityLowPct: 0.005,
  volatilityModeratePct: 0.02,
  volatilityHighPct: 0.05,
  trendWeakPct: 0.001,
  trendStrongPct: 0.005,
  liquidityMedium: 40,
  liquidityStrong: 70,
  rsiOversoldStrong: 30,
  rsiOversoldMedium: 40,
  rsiOverboughtMedium: 60,
  rsiOverboughtStrong: 70,
  adxStrongTrend: 30,
  adxWeakTrend: 20,
  atrMaxPctOfPrice: 0.025,
  bollingerEntryMaxPct: 0.75,
  bollingerEntryMinPct: 0.25,
  minConfidenceSpread: 0.1,
  regimeMode: "trend",
  trendFilterFastPeriod: 50,
  trendFilterSlowPeriod: 100,
};

export const defaultComposerConfig: ComposerConfig = {
  weights: defaultWeights,
  thresholds: defaultThresholds,
};

export function validateWeights(weights: ComposerWeights): boolean {
  const sum =
    weights.spread +
    weights.imbalance +
    weights.volatility +
    weights.trend +
    weights.liquidity +
    weights.rsi +
    weights.regime;
  return Math.abs(sum - 1.0) <= 0.001;
}

function isSyntheticInput(
  ohlcv: OHLCVInput,
  obMetrics: OrderBookMetricsInput,
): boolean {
  // When backtesting from candles we derive synthetic order-book metrics.
  // Those metrics are noise, so we drop order-book weights automatically.
  return ohlcv.exchange === "synthetic" || obMetrics.exchange === "synthetic";
}

function withoutOrderBookWeights(config: ComposerConfig): ComposerConfig {
  const weights = { ...config.weights, spread: 0, imbalance: 0, liquidity: 0 };
  const activeSum =
    weights.volatility + weights.trend + weights.rsi + weights.regime;
  if (activeSum <= 0) return config;

  return {
    ...config,
    weights: {
      spread: 0,
      imbalance: 0,
      liquidity: 0,
      volatility: weights.volatility / activeSum,
      trend: weights.trend / activeSum,
      rsi: weights.rsi / activeSum,
      regime: weights.regime / activeSum,
    },
  };
}

export function composeSignal(
  ohlcv: OHLCVInput,
  obMetrics: OrderBookMetricsInput,
  config: ComposerConfig = defaultComposerConfig,
): ScalpingSignal | null {
  const effectiveConfig = isSyntheticInput(ohlcv, obMetrics)
    ? withoutOrderBookWeights(config)
    : config;

  if (!validateWeights(effectiveConfig.weights)) {
    return null;
  }

  const candles = ohlcv.candles;
  if (candles.length < 2) return null;

  const weights = effectiveConfig.weights;
  const thresholds = effectiveConfig.thresholds;
  const components: SignalComponent[] = [];

  const spreadComponent = buildSpreadComponent(
    obMetrics,
    weights.spread,
    thresholds,
  );
  if (spreadComponent) components.push(spreadComponent);

  const imbalanceComponent = buildImbalanceComponent(
    obMetrics,
    weights.imbalance,
    thresholds,
  );
  if (imbalanceComponent) components.push(imbalanceComponent);

  const volatilityComponent = buildVolatilityComponent(
    candles,
    weights.volatility,
    thresholds,
  );
  if (volatilityComponent) components.push(volatilityComponent);

  const trendComponent = buildTrendComponent(
    candles,
    weights.trend,
    thresholds,
  );
  if (trendComponent) components.push(trendComponent);

  const liquidityComponent = buildLiquidityComponent(
    obMetrics,
    weights.liquidity,
    thresholds,
  );
  if (liquidityComponent) components.push(liquidityComponent);

  const rsiComponent = buildRsiComponent(candles, weights.rsi, thresholds);
  if (rsiComponent) components.push(rsiComponent);

  const regimeComponent = buildRegimeComponent(
    candles,
    obMetrics,
    weights.regime,
    thresholds,
  );
  if (regimeComponent) components.push(regimeComponent);

  if (components.length === 0) return null;

  const { direction, confidence } = aggregateDirection(
    components,
    config.thresholds.minConfidenceSpread,
  );

  const attributionWeights: Record<string, number> = {};
  for (const c of components) {
    const sign = c.signal === "buy" ? 1 : c.signal === "sell" ? -1 : 0;
    attributionWeights[c.name] = sign * c.weight * strengthValue(c.strength);
  }

  return {
    id: randomUUID(),
    exchange: ohlcv.exchange,
    symbol: ohlcv.symbol,
    direction,
    confidence,
    components,
    microstructure: {
      spread: obMetrics.spread,
      spreadPercent: obMetrics.spreadPercent,
      bidDepth: obMetrics.bidDepth,
      askDepth: obMetrics.askDepth,
      imbalance: obMetrics.imbalance,
      midPrice: obMetrics.midPrice,
      timestamp: obMetrics.timestamp,
    },
    attributionWeights,
    metadata: {
      timeframe: ohlcv.timeframe,
      candleCount: candles.length,
    },
    generatedAt: new Date(),
  };
}

function buildSpreadComponent(
  ob: OrderBookMetricsInput,
  weight: number,
  thresholds: ComposerThresholds,
): SignalComponent | null {
  const spreadPct = ob.spreadPercent;
  let signal: Direction = "hold";
  let strength: SignalStrength = "weak";

  if (spreadPct < thresholds.spreadTightPct) {
    signal = "buy";
    strength = "medium";
  } else if (spreadPct < thresholds.spreadModeratePct) {
    signal = "hold";
    strength = "weak";
  } else if (spreadPct < thresholds.spreadWidePct) {
    signal = "hold";
    strength = "weak";
  } else {
    signal = "sell";
    strength = "weak";
  }

  return {
    name: "spread",
    description: "Bid-ask spread as a percentage of mid price",
    value: spreadPct,
    signal,
    strength,
    weight,
  };
}

function buildImbalanceComponent(
  ob: OrderBookMetricsInput,
  weight: number,
  thresholds: ComposerThresholds,
): SignalComponent | null {
  const imbalance = ob.imbalance;
  let signal: Direction = "hold";
  let strength: SignalStrength = "weak";

  if (imbalance > thresholds.imbalanceStrong) {
    signal = "buy";
    strength = "strong";
  } else if (imbalance > thresholds.imbalanceWeak) {
    signal = "buy";
    strength = "medium";
  } else if (imbalance < -thresholds.imbalanceStrong) {
    signal = "sell";
    strength = "strong";
  } else if (imbalance < -thresholds.imbalanceWeak) {
    signal = "sell";
    strength = "medium";
  }

  return {
    name: "imbalance",
    description: "Order book bid/ask depth imbalance",
    value: imbalance,
    signal,
    strength,
    weight,
  };
}

function buildVolatilityComponent(
  candles: readonly CandleLike[],
  weight: number,
  thresholds: ComposerThresholds,
): SignalComponent | null {
  const volatility = calculateVolatility(candles);
  if (volatility === null) return null;

  let signal: Direction = "hold";
  let strength: SignalStrength = "weak";

  if (volatility < thresholds.volatilityLowPct) {
    signal = "hold";
    strength = "weak";
  } else if (volatility < thresholds.volatilityModeratePct) {
    signal = "buy";
    strength = "medium";
  } else if (volatility < thresholds.volatilityHighPct) {
    signal = "hold";
    strength = "medium";
  } else {
    signal = "sell";
    strength = "strong";
  }

  return {
    name: "volatility",
    description: "Absolute close-to-close price change",
    value: volatility,
    signal,
    strength,
    weight,
  };
}

function buildTrendComponent(
  candles: readonly CandleLike[],
  weight: number,
  thresholds: ComposerThresholds,
): SignalComponent | null {
  if (candles.length < 30) return null;

  const closes = candles.map((c) => c.close);
  const emaFast = calculateEMA(closes, 9);
  const emaSlow = calculateEMA(closes, 21);

  const lastFast = emaFast[emaFast.length - 1];
  const lastSlow = emaSlow[emaSlow.length - 1];
  if (Number.isNaN(lastFast) || Number.isNaN(lastSlow) || lastSlow === 0)
    return null;

  const diff = (lastFast - lastSlow) / lastSlow;
  let signal: Direction = "hold";
  let strength: SignalStrength = "weak";

  if (diff >= thresholds.trendStrongPct) {
    signal = "buy";
    strength = "strong";
  } else if (diff >= thresholds.trendWeakPct) {
    signal = "buy";
    strength = "medium";
  } else if (diff <= -thresholds.trendStrongPct) {
    signal = "sell";
    strength = "strong";
  } else if (diff <= -thresholds.trendWeakPct) {
    signal = "sell";
    strength = "medium";
  }

  return {
    name: "trend",
    description: "EMA 9 vs EMA 21 slope",
    value: diff,
    signal,
    strength,
    weight,
  };
}

function buildLiquidityComponent(
  ob: OrderBookMetricsInput,
  weight: number,
  thresholds: ComposerThresholds,
): SignalComponent | null {
  const liquidity = ob.bidDepth + ob.askDepth;
  let signal: Direction = "hold";
  let strength: SignalStrength = "weak";

  if (liquidity > thresholds.liquidityStrong) {
    signal = "buy";
    strength = "strong";
  } else if (liquidity > thresholds.liquidityMedium) {
    signal = "buy";
    strength = "medium";
  }

  return {
    name: "liquidity",
    description: "Combined bid + ask depth near mid price",
    value: liquidity,
    signal,
    strength,
    weight,
  };
}

function buildRsiComponent(
  candles: readonly CandleLike[],
  weight: number,
  thresholds: ComposerThresholds,
): SignalComponent | null {
  if (candles.length < 15) return null;

  const rsi = calculateRSI(candles, 14);
  if (rsi === null) return null;

  let signal: Direction = "hold";
  let strength: SignalStrength = "weak";

  if (rsi < thresholds.rsiOversoldStrong) {
    signal = "buy";
    strength = "strong";
  } else if (rsi < thresholds.rsiOversoldMedium) {
    signal = "buy";
    strength = "medium";
  } else if (rsi > thresholds.rsiOverboughtStrong) {
    signal = "sell";
    strength = "strong";
  } else if (rsi > thresholds.rsiOverboughtMedium) {
    signal = "sell";
    strength = "medium";
  }

  return {
    name: "rsi",
    description: "14-period Relative Strength Index",
    value: rsi,
    signal,
    strength,
    weight,
  };
}

function buildRegimeComponent(
  candles: readonly CandleLike[],
  ob: OrderBookMetricsInput,
  weight: number,
  thresholds: ComposerThresholds,
): SignalComponent | null {
  if (candles.length < 30) return null;

  const { adx, plusDI, minusDI } = calculateADX(candles, 14);
  const atr = calculateATR(candles, 14);
  const bb = calculateBollingerBands(candles, 20);

  if (
    adx === null ||
    plusDI === null ||
    minusDI === null ||
    atr === null ||
    bb === null
  ) {
    return null;
  }

  const midPrice = ob.midPrice;
  const atrPct = midPrice > 0 ? atr / midPrice : 0;

  // Skip if volatility is excessive (ATR too large relative to price).
  if (atrPct > thresholds.atrMaxPctOfPrice) {
    return {
      name: "regime",
      description: "Volatility regime filter",
      value: atrPct,
      signal: "hold",
      strength: "strong",
      weight,
    };
  }

  // Skip if price is in the middle of the Bollinger band (no directional edge).
  if (
    bb.percentB > thresholds.bollingerEntryMinPct &&
    bb.percentB < thresholds.bollingerEntryMaxPct
  ) {
    return {
      name: "regime",
      description: "Bollinger band position filter",
      value: bb.percentB,
      signal: "hold",
      strength: "medium",
      weight,
    };
  }

  const higherTrend = higherTimeframeTrend(candles, thresholds);
  const allowLong = higherTrend === null || higherTrend === true;
  const allowShort = higherTrend === null || higherTrend === false;

  const mode = thresholds.regimeMode ?? "trend";
  let signal: Direction = "hold";
  let strength: SignalStrength = "weak";

  if (mode === "reversion") {
    // Mean-reversion regime: only fade extremes in the direction of the
    // higher-timeframe trend. This avoids catching falling knives in a
    // sustained downtrend or shorting into a sustained uptrend.
    if (
      bb.percentB <= thresholds.bollingerEntryMinPct &&
      plusDI >= minusDI &&
      allowLong
    ) {
      signal = "buy";
      strength = adx > thresholds.adxStrongTrend ? "strong" : "medium";
    } else if (
      bb.percentB >= thresholds.bollingerEntryMaxPct &&
      minusDI >= plusDI &&
      allowShort
    ) {
      signal = "sell";
      strength = adx > thresholds.adxStrongTrend ? "strong" : "medium";
    }
  } else {
    // Trend-strength regime: require ADX, DI lines, Bollinger position, and
    // higher-timeframe trend alignment to agree before entering.
    if (adx > thresholds.adxWeakTrend) {
      if (
        allowLong &&
        plusDI > minusDI &&
        bb.percentB >= thresholds.bollingerEntryMinPct
      ) {
        signal = "buy";
        strength = adx > thresholds.adxStrongTrend ? "strong" : "medium";
      } else if (
        allowShort &&
        minusDI > plusDI &&
        bb.percentB <= thresholds.bollingerEntryMaxPct
      ) {
        signal = "sell";
        strength = adx > thresholds.adxStrongTrend ? "strong" : "medium";
      }
    }
  }

  return {
    name: "regime",
    description:
      mode === "reversion"
        ? "Bollinger band mean-reversion regime + higher-timeframe trend filter"
        : "ADX trend strength + Bollinger direction + higher-timeframe trend filter",
    value: adx,
    signal,
    strength,
    weight,
  };
}

function higherTimeframeTrend(
  candles: readonly CandleLike[],
  thresholds: ComposerThresholds,
): boolean | null {
  const fastPeriod = thresholds.trendFilterFastPeriod ?? 50;
  const slowPeriod = thresholds.trendFilterSlowPeriod ?? 100;
  if (candles.length < slowPeriod + 1) return null;

  const closes = candles.map((c) => c.close);
  const fastEMA = calculateEMA(closes, fastPeriod);
  const slowEMA = calculateEMA(closes, slowPeriod);
  const lastFast = fastEMA[fastEMA.length - 1];
  const lastSlow = slowEMA[slowEMA.length - 1];
  if (Number.isNaN(lastFast) || Number.isNaN(lastSlow) || lastSlow === 0)
    return null;
  return lastFast > lastSlow;
}

function aggregateDirection(
  components: SignalComponent[],
  minConfidenceSpread: number,
): { direction: Direction; confidence: number } {
  let buyScore = 0;
  let sellScore = 0;
  let totalWeight = 0;

  for (const c of components) {
    totalWeight += c.weight;
    const value = c.weight * strengthValue(c.strength);
    if (c.signal === "buy") buyScore += value;
    else if (c.signal === "sell") sellScore += value;
  }

  const buyConfidence = totalWeight > 0 ? buyScore / totalWeight : 0;
  const sellConfidence = totalWeight > 0 ? sellScore / totalWeight : 0;

  if (buyConfidence - sellConfidence >= minConfidenceSpread) {
    return { direction: "buy", confidence: buyConfidence };
  }
  if (sellConfidence - buyConfidence >= minConfidenceSpread) {
    return { direction: "sell", confidence: sellConfidence };
  }
  return {
    direction: "hold",
    confidence: Math.max(buyConfidence, sellConfidence),
  };
}

function strengthValue(strength: SignalStrength): number {
  switch (strength) {
    case "strong":
      return 0.9;
    case "medium":
      return 0.6;
    case "weak":
      return 0.3;
  }
}
