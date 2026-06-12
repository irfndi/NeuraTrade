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
import { calculateEMA, calculateRSI, calculateVolatility } from "./indicators.js";

const defaultWeights: ComposerWeights = {
  spread: 0.20,
  imbalance: 0.25,
  volatility: 0.15,
  trend: 0.20,
  liquidity: 0.10,
  rsi: 0.10,
};

const defaultThresholds: ComposerThresholds = {
  spreadTightPct: 0.0005,
  spreadModeratePct: 0.001,
  spreadWidePct: 0.002,
  imbalanceWeak: 0.05,
  imbalanceStrong: 0.20,
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
  minConfidenceSpread: 0.10,
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
    weights.rsi;
  return Math.abs(sum - 1.0) <= 0.001;
}

export function composeSignal(
  ohlcv: OHLCVInput,
  obMetrics: OrderBookMetricsInput,
  config: ComposerConfig = defaultComposerConfig,
): ScalpingSignal | null {
  if (!validateWeights(config.weights)) {
    return null;
  }

  const candles = ohlcv.candles;
  if (candles.length < 2) return null;

  const components: SignalComponent[] = [];

  const spreadComponent = buildSpreadComponent(obMetrics, config.weights.spread, config.thresholds);
  if (spreadComponent) components.push(spreadComponent);

  const imbalanceComponent = buildImbalanceComponent(
    obMetrics,
    config.weights.imbalance,
    config.thresholds,
  );
  if (imbalanceComponent) components.push(imbalanceComponent);

  const volatilityComponent = buildVolatilityComponent(
    candles,
    config.weights.volatility,
    config.thresholds,
  );
  if (volatilityComponent) components.push(volatilityComponent);

  const trendComponent = buildTrendComponent(candles, config.weights.trend, config.thresholds);
  if (trendComponent) components.push(trendComponent);

  const liquidityComponent = buildLiquidityComponent(
    obMetrics,
    config.weights.liquidity,
    config.thresholds,
  );
  if (liquidityComponent) components.push(liquidityComponent);

  const rsiComponent = buildRsiComponent(candles, config.weights.rsi, config.thresholds);
  if (rsiComponent) components.push(rsiComponent);

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
  if (candles.length < 5) return null;

  const closes = candles.map((c) => c.close);
  const ema3 = calculateEMA(closes, 3);
  const ema5 = calculateEMA(closes, 5);

  const lastEma3 = ema3[ema3.length - 1];
  const lastEma5 = ema5[ema5.length - 1];
  if (Number.isNaN(lastEma3) || Number.isNaN(lastEma5) || lastEma5 === 0) return null;

  const diff = (lastEma3 - lastEma5) / lastEma5;
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
    description: "EMA 3 vs EMA 5 slope",
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
  return { direction: "hold", confidence: Math.max(buyConfidence, sellConfidence) };
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
