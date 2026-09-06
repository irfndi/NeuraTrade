import { randomUUID } from "node:crypto";
import type {
  CandleLike,
  ComposerConfig,
  ComposerThresholds,
  ComposerWeights,
  Direction,
  FundingRate,
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
  calculateRSISeries,
  calculateSMA,
  calculateVolatility,
  calculateVolumeRatio,
} from "./indicators.js";
import {
  passesDirectionalMarketFilter,
  passesMarketFilters,
} from "./market-filter.js";

const defaultWeights: ComposerWeights = {
  spread: 0.18,
  imbalance: 0.22,
  volatility: 0.13,
  trend: 0.18,
  liquidity: 0.09,
  rsi: 0.09,
  rsiPullback: 0,
  emaPullback: 0,
  regime: 0.11,
  funding: 0,
  connorsRsi2: 0,
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
  rsiPeriod: 14,
  rsiOversoldStrong: 30,
  rsiOversoldMedium: 40,
  rsiOverboughtMedium: 60,
  rsiOverboughtStrong: 70,
  maxBarsInTrade: 0,
  adxStrongTrend: 30,
  adxWeakTrend: 20,
  atrMaxPctOfPrice: 0,
  bollingerEntryMaxPct: 0,
  bollingerEntryMinPct: 0,
  minConfidenceSpread: 0.1,
  regimeMode: "trend",
  breakoutLookback: 20,
  breakoutVolumeMinRatio: 1.2,
  breakoutAdxMin: 20,
  trendFilterFastPeriod: 50,
  trendFilterSlowPeriod: 100,
  volumeMinRatio: 0,
  volumeLookback: 20,
  minConfluence: 0,
  minCategoryConfluence: 0,
  entryCandleConfirm: false,
  momentumConfirmBars: 0,
  directionalOnly: false,
  rsiFollowTrend: false,
  strictAgreement: false,
  maxSpreadPct: 0,
  minLiquidity: 0,
  trendSignalStyle: "slope",
  trendFastPeriod: 9,
  trendSlowPeriod: 21,
  fundingBiasThreshold: 0.0001,
  useFunding: false,
  trendFilterPeriod: 200,
  entryRsiLongThreshold: 10,
  entryRsiShortThreshold: 90,
  exitRsiLongThreshold: 60,
  exitRsiShortThreshold: 40,
};

export const defaultComposerConfig: ComposerConfig = {
  weights: defaultWeights,
  thresholds: defaultThresholds,
};

/** Trend regime classification derived from ADX plus the DI balance. */
export type TrendRegime = "trending" | "ranging";

/** Volatility regime classification derived from ATR relative to price. */
export type VolatilityRegime = "low" | "normal" | "high";

/**
 * A structured digest of the current market regime, computed causally from the
 * candle history. It disambiguates "trending vs ranging" (ADX + DI lines) and
 * "volatility regime" (ATR as a fraction of price), and exposes the Bollinger
 * position used to skip middle-of-band entries.
 */
export interface RegimeAnalysis {
  /** ADX(14) trend strength. Null when there is insufficient data. */
  readonly adx: number | null;
  readonly plusDI: number | null;
  readonly minusDI: number | null;
  /** Wilder ATR(14). Null when there is insufficient data. */
  readonly atr: number | null;
  /** ATR as a fraction of the latest close price. 0 when price is unknown. */
  readonly atrPct: number;
  /** Bollinger(20,2) position. Null when there is insufficient data. */
  readonly bollinger: BollingerPosition | null;
  /** True when ADX indicates a directional trend rather than chop. */
  readonly trending: boolean;
  readonly trendRegime: TrendRegime;
  readonly volatilityRegime: VolatilityRegime;
}

/** Bollinger band snapshot with the normalized percent-B position. */
export interface BollingerPosition {
  readonly upper: number;
  readonly middle: number;
  readonly lower: number;
  readonly percentB: number;
}

/**
 * Compute the current market regime from the trailing candles. Uses the
 * classic ADX/DI/ATR/Bollinger indicators and classifies the trend and
 * volatility regimes. Returns null when the candle history is too short to
 * produce a meaningful reading.
 */
export function analyzeRegime(
  candles: readonly CandleLike[],
  thresholds: Pick<
    ComposerThresholds,
    | "adxWeakTrend"
    | "adxStrongTrend"
    | "atrMaxPctOfPrice"
    | "bollingerEntryMinPct"
    | "bollingerEntryMaxPct"
    | "volatilityLowPct"
    | "volatilityModeratePct"
    | "volatilityHighPct"
  >,
): RegimeAnalysis | null {
  if (candles.length < 30) return null;

  const { adx, plusDI, minusDI } = calculateADX(candles, 14);
  const atr = calculateATR(candles, 14);
  const bb = calculateBollingerBands(candles, 20);
  if (adx === null || plusDI === null || minusDI === null || atr === null) {
    return null;
  }

  const lastClose = candles[candles.length - 1].close;
  const atrPct = lastClose > 0 ? atr / lastClose : 0;

  const weakTrend = thresholds.adxWeakTrend ?? 20;
  const strongVm = thresholds.volatilityModeratePct ?? 0.02;
  const highVm = thresholds.volatilityHighPct ?? 0.05;

  let volatilityRegime: VolatilityRegime;
  if (atrPct >= highVm) volatilityRegime = "high";
  else if (atrPct >= strongVm) volatilityRegime = "normal";
  else volatilityRegime = "low";

  const trending = adx >= weakTrend && plusDI !== minusDI;
  const bollinger: BollingerPosition | null =
    bb === null
      ? null
      : {
          upper: bb.upper,
          middle: bb.middle,
          lower: bb.lower,
          percentB: bb.percentB,
        };

  return {
    adx,
    plusDI,
    minusDI,
    atr,
    atrPct,
    bollinger,
    trending,
    trendRegime: trending ? "trending" : "ranging",
    volatilityRegime,
  };
}

export function validateWeights(weights: ComposerWeights): boolean {
  const sum =
    weights.spread +
    weights.imbalance +
    weights.volatility +
    weights.trend +
    weights.liquidity +
    weights.rsi +
    weights.rsiPullback +
    weights.emaPullback +
    weights.regime +
    weights.funding +
    weights.connorsRsi2;
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
  const enabled = config.enabled;
  const weights = { ...config.weights, spread: 0, imbalance: 0, liquidity: 0 };
  const activeSum =
    weights.volatility +
    weights.trend +
    weights.rsi +
    weights.connorsRsi2 +
    weights.rsiPullback +
    weights.emaPullback +
    weights.regime +
    weights.funding;
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
      connorsRsi2: weights.connorsRsi2 / activeSum,
      rsiPullback: weights.rsiPullback / activeSum,
      emaPullback: weights.emaPullback / activeSum,
      regime: weights.regime / activeSum,
      funding: weights.funding / activeSum,
    },
    enabled: enabled
      ? { ...enabled, spread: false, imbalance: false, liquidity: false }
      : undefined,
  };
}

function applyEnabledWeights(config: ComposerConfig): ComposerConfig {
  const enabled = config.enabled;
  if (!enabled) return config;

  const weights = { ...config.weights };
  let activeSum = 0;
  for (const key of Object.keys(weights) as Array<keyof ComposerWeights>) {
    if (enabled[key] === false) weights[key] = 0;
    activeSum += weights[key];
  }
  if (activeSum <= 0) return { ...config, weights };

  for (const key of Object.keys(weights) as Array<keyof ComposerWeights>) {
    weights[key] /= activeSum;
  }
  return { ...config, weights };
}

function isComponentEnabled(
  config: ComposerConfig,
  name: keyof ComposerWeights,
): boolean {
  return config.enabled?.[name] !== false;
}

function addComponent(
  components: SignalComponent[],
  component: SignalComponent | null,
): void {
  if (component) components.push(component);
}

function addDirectionalComponent(
  components: SignalComponent[],
  component: SignalComponent | null,
  directionalOnly: boolean,
  passesFilter: (component: SignalComponent) => boolean,
): boolean {
  if (!component) return true;
  if (directionalOnly && !passesFilter(component)) return false;
  components.push(component);
  return true;
}

function buildMarketComponents(
  candles: readonly CandleLike[],
  obMetrics: OrderBookMetricsInput,
  config: ComposerConfig,
): SignalComponent[] | null {
  const { weights, thresholds } = config;
  const components: SignalComponent[] = [];
  if (isComponentEnabled(config, "spread")) {
    addComponent(
      components,
      buildSpreadComponent(obMetrics, weights.spread, thresholds),
    );
  }
  if (isComponentEnabled(config, "imbalance")) {
    addComponent(
      components,
      buildImbalanceComponent(obMetrics, weights.imbalance, thresholds),
    );
  }
  if (isComponentEnabled(config, "volatility")) {
    const volatility = buildVolatilityComponent(
      candles,
      weights.volatility,
      thresholds,
    );
    if (
      !addDirectionalComponent(
        components,
        volatility,
        thresholds.directionalOnly ?? false,
        (component) => passesVolatilityFilter(component, thresholds),
      )
    )
      return null;
  }
  if (isComponentEnabled(config, "trend")) {
    addComponent(
      components,
      buildTrendComponent(candles, weights.trend, thresholds),
    );
  }
  if (isComponentEnabled(config, "liquidity")) {
    const liquidity = buildLiquidityComponent(
      obMetrics,
      weights.liquidity,
      thresholds,
    );
    if (
      !addDirectionalComponent(
        components,
        liquidity,
        thresholds.directionalOnly ?? false,
        (component) => passesLiquidityFilter(component, thresholds),
      )
    )
      return null;
  }
  return components;
}

function buildOscillatorComponents(
  candles: readonly CandleLike[],
  config: ComposerConfig,
): SignalComponent[] {
  const { weights, thresholds } = config;
  const components: SignalComponent[] = [];
  if (isComponentEnabled(config, "rsi")) {
    addComponent(
      components,
      buildRsiComponent(candles, weights.rsi, thresholds),
    );
  }
  if (isComponentEnabled(config, "connorsRsi2")) {
    addComponent(
      components,
      buildConnorsRsi2Component(candles, weights.connorsRsi2, thresholds),
    );
  }
  if (isComponentEnabled(config, "rsiPullback")) {
    addComponent(
      components,
      buildRsiPullbackComponent(candles, weights.rsiPullback ?? 0, thresholds),
    );
  }
  if (isComponentEnabled(config, "emaPullback")) {
    addComponent(
      components,
      buildEmaPullbackComponent(candles, weights.emaPullback ?? 0, thresholds),
    );
  }
  return components;
}

function buildContextComponents(
  candles: readonly CandleLike[],
  obMetrics: OrderBookMetricsInput,
  ohlcv: OHLCVInput,
  config: ComposerConfig,
): SignalComponent[] {
  const { weights, thresholds } = config;
  const components: SignalComponent[] = [];
  if (isComponentEnabled(config, "regime")) {
    addComponent(
      components,
      buildRegimeComponent(candles, obMetrics, weights.regime, thresholds),
    );
  }
  if (isComponentEnabled(config, "funding") && thresholds.useFunding) {
    addComponent(
      components,
      buildFundingComponent(
        candles,
        obMetrics,
        weights.funding,
        thresholds,
        ohlcv.fundingRates,
      ),
    );
  }
  return components;
}
interface ValidatedSignalComponents {
  readonly directionalComponents: readonly SignalComponent[];
  readonly direction: "buy" | "sell" | "hold";
  readonly confidence: number;
}

function validateSignalComponents(
  candles: readonly CandleLike[],
  components: SignalComponent[],
  thresholds: ComposerThresholds,
): ValidatedSignalComponents | null {
  if (components.length === 0) return null;
  const directionalComponents = thresholds.directionalOnly
    ? components.filter(
        (component) =>
          component.name === "trend" ||
          component.name === "rsi" ||
          component.name === "connorsRsi2" ||
          component.name === "rsiPullback" ||
          component.name === "emaPullback" ||
          component.name === "regime",
      )
    : components;
  if (directionalComponents.length === 0) return null;
  const { direction, confidence } = aggregateDirection(
    directionalComponents,
    thresholds.minConfidenceSpread,
  );
  if (
    thresholds.strictAgreement &&
    !directionalComponents.every(
      (component) =>
        component.signal === direction || component.signal === "hold",
    )
  )
    return null;
  if (
    !passesConfluenceFilter(
      directionalComponents,
      direction,
      thresholds.minConfluence,
    )
  )
    return null;
  if (
    !passesCategoryConfluenceFilter(
      directionalComponents,
      direction,
      thresholds.minCategoryConfluence,
    )
  )
    return null;
  if (!passesDirectionalMarketFilter(candles, direction, thresholds))
    return null;
  if (
    thresholds.entryCandleConfirm &&
    !passesEntryCandleConfirmation(candles[candles.length - 1], direction)
  )
    return null;
  if (
    thresholds.momentumConfirmBars &&
    thresholds.momentumConfirmBars > 0 &&
    !passesMomentumConfirmation(
      candles,
      direction,
      thresholds.momentumConfirmBars,
    )
  )
    return null;
  return { directionalComponents, direction, confidence };
}
export function composeSignal(
  ohlcv: OHLCVInput,
  obMetrics: OrderBookMetricsInput,
  config: ComposerConfig = defaultComposerConfig,
): ScalpingSignal | null {
  const enabledConfig = applyEnabledWeights(config);
  const effectiveConfig = isSyntheticInput(ohlcv, obMetrics)
    ? withoutOrderBookWeights(enabledConfig)
    : enabledConfig;
  if (!validateWeights(effectiveConfig.weights)) {
    return null;
  }

  const candles = ohlcv.candles;
  if (candles.length < 2) return null;

  const thresholds = effectiveConfig.thresholds;

  if (!passesMarketFilters(candles, obMetrics, thresholds)) {
    return null;
  }

  const marketComponents = buildMarketComponents(
    candles,
    obMetrics,
    effectiveConfig,
  );
  if (marketComponents === null) return null;
  const components = [
    ...marketComponents,
    ...buildOscillatorComponents(candles, effectiveConfig),
    ...buildContextComponents(candles, obMetrics, ohlcv, effectiveConfig),
  ];

  const validated = validateSignalComponents(candles, components, thresholds);
  if (validated === null) return null;
  const { direction, confidence } = validated;
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

function passesVolatilityFilter(
  component: SignalComponent,
  thresholds: ComposerThresholds,
): boolean {
  const volatility = component.value;
  return (
    volatility >= thresholds.volatilityLowPct &&
    volatility <= thresholds.volatilityHighPct
  );
}

function buildTrendComponent(
  candles: readonly CandleLike[],
  weight: number,
  thresholds: ComposerThresholds,
): SignalComponent | null {
  const fastPeriod = thresholds.trendFastPeriod ?? 9;
  const slowPeriod = thresholds.trendSlowPeriod ?? 21;
  const minLength = Math.max(fastPeriod, slowPeriod) + 2;
  if (candles.length < minLength) return null;

  const closes = candles.map((c) => c.close);
  const emaFast = calculateEMA(closes, fastPeriod);
  const emaSlow = calculateEMA(closes, slowPeriod);

  const lastFast = emaFast[emaFast.length - 1];
  const lastSlow = emaSlow[emaSlow.length - 1];
  const prevFast = emaFast[emaFast.length - 2];
  const prevSlow = emaSlow[emaSlow.length - 2];
  if (
    Number.isNaN(lastFast) ||
    Number.isNaN(lastSlow) ||
    Number.isNaN(prevFast) ||
    Number.isNaN(prevSlow) ||
    lastSlow === 0
  )
    return null;

  const style = thresholds.trendSignalStyle ?? "slope";
  const diff = (lastFast - lastSlow) / lastSlow;
  let signal: Direction = "hold";
  let strength: SignalStrength = "weak";

  if (style === "cross") {
    const wasAbove = prevFast > prevSlow;
    const isAbove = lastFast > lastSlow;
    if (!wasAbove && isAbove) {
      signal = "buy";
      strength = "strong";
    } else if (wasAbove && !isAbove) {
      signal = "sell";
      strength = "strong";
    }
  } else {
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
  }

  return {
    name: "trend",
    description:
      style === "cross"
        ? `EMA ${fastPeriod} vs EMA ${slowPeriod} crossover`
        : `EMA ${fastPeriod} vs EMA ${slowPeriod} slope`,
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

function passesLiquidityFilter(
  component: SignalComponent,
  thresholds: ComposerThresholds,
): boolean {
  return component.value >= thresholds.liquidityMedium;
}

function buildRsiComponent(
  candles: readonly CandleLike[],
  weight: number,
  thresholds: ComposerThresholds,
): SignalComponent | null {
  const rsiPeriod = thresholds.rsiPeriod ?? 14;
  if (candles.length < rsiPeriod + 1) return null;

  const rsi = calculateRSI(candles, rsiPeriod);
  if (rsi === null) return null;

  const followTrend =
    thresholds.rsiFollowTrend && thresholds.regimeMode === "trend";
  const oversoldStrong = thresholds.rsiOversoldStrong ?? 30;
  const oversoldMedium = thresholds.rsiOversoldMedium;
  const overboughtMedium = thresholds.rsiOverboughtMedium;
  const overboughtStrong = thresholds.rsiOverboughtStrong ?? 70;

  let signal: Direction = "hold";
  let strength: SignalStrength = "weak";

  if (followTrend) {
    if (rsi > overboughtStrong) {
      signal = "sell";
      strength = "strong";
    } else if (rsi > overboughtMedium) {
      signal = "sell";
      strength = "medium";
    } else if (rsi < oversoldStrong) {
      signal = "buy";
      strength = "strong";
    } else if (rsi < oversoldMedium) {
      signal = "buy";
      strength = "medium";
    } else if (rsi >= 50) {
      signal = "buy";
      strength = "weak";
    } else {
      signal = "sell";
      strength = "weak";
    }
  } else {
    if (rsi < oversoldStrong) {
      signal = "buy";
      strength = "strong";
    } else if (rsi < oversoldMedium) {
      signal = "buy";
      strength = "medium";
    } else if (rsi > overboughtStrong) {
      signal = "sell";
      strength = "strong";
    } else if (rsi > overboughtMedium) {
      signal = "sell";
      strength = "medium";
    }
  }

  return {
    name: "rsi",
    description: followTrend
      ? `${rsiPeriod}-period RSI confirming trend direction`
      : `${rsiPeriod}-period Relative Strength Index`,
    value: rsi,
    signal,
    strength,
    weight,
  };
}

function buildConnorsRsi2Component(
  candles: readonly CandleLike[],
  weight: number,
  thresholds: ComposerThresholds,
): SignalComponent | null {
  if (weight <= 0) return null;

  const rsiPeriod = 2;
  const trendFilterPeriod = thresholds.trendFilterPeriod ?? 200;
  const minLength = Math.max(rsiPeriod + 1, trendFilterPeriod);
  if (candles.length < minLength) return null;

  const rsi = calculateRSI(candles, rsiPeriod);
  if (rsi === null) return null;

  const closes = candles.map((c) => c.close);
  const sma = calculateSMA(closes, trendFilterPeriod);
  const lastClose = closes[closes.length - 1];
  const lastSma = sma[sma.length - 1];
  if (Number.isNaN(lastSma) || lastSma === 0) return null;

  const entryLongThreshold = thresholds.entryRsiLongThreshold ?? 10;
  const entryShortThreshold = thresholds.entryRsiShortThreshold ?? 90;

  let signal: Direction = "hold";
  let strength: SignalStrength = "weak";

  if (lastClose > lastSma && rsi < entryLongThreshold) {
    signal = "buy";
    strength = rsi < 5 ? "strong" : "medium";
  } else if (lastClose < lastSma && rsi > entryShortThreshold) {
    signal = "sell";
    strength = rsi > 95 ? "strong" : "medium";
  }

  if (signal === "hold") return null;

  return {
    name: "connorsRsi2",
    description: `Larry Connors RSI(2) mean reversion with ${trendFilterPeriod}-period SMA trend filter`,
    value: rsi,
    signal,
    strength,
    weight,
  };
}

function buildRsiPullbackComponent(
  candles: readonly CandleLike[],
  weight: number,
  thresholds: ComposerThresholds,
): SignalComponent | null {
  const rsiPeriod = thresholds.rsiPeriod ?? 14;
  if (weight <= 0 || candles.length < Math.max(20, rsiPeriod + 1)) return null;

  const rsiSeries = calculateRSISeries(candles, rsiPeriod);
  if (rsiSeries.length < 6) return null;

  const fastPeriod = thresholds.trendFastPeriod ?? 9;
  const slowPeriod = thresholds.trendSlowPeriod ?? 21;
  if (candles.length < Math.max(fastPeriod, slowPeriod) + 2) return null;

  const closes = candles.map((c) => c.close);
  const emaFast = calculateEMA(closes, fastPeriod);
  const emaSlow = calculateEMA(closes, slowPeriod);
  const lastFast = emaFast[emaFast.length - 1];
  const lastSlow = emaSlow[emaSlow.length - 1];
  const rsi = rsiSeries[rsiSeries.length - 1];
  const prevRsi = rsiSeries.slice(-6, -1);

  const isUptrend = lastFast > lastSlow;
  const isDowntrend = lastFast < lastSlow;

  let signal: Direction = "hold";
  let strength: SignalStrength = "weak";

  if (isUptrend && rsi >= 40 && rsi <= 50 && prevRsi.some((r) => r >= 55)) {
    signal = "buy";
    strength = rsi < 45 ? "strong" : "medium";
  } else if (
    isDowntrend &&
    rsi >= 50 &&
    rsi <= 60 &&
    prevRsi.some((r) => r <= 45)
  ) {
    signal = "sell";
    strength = rsi > 55 ? "strong" : "medium";
  }

  if (signal === "hold") return null;

  return {
    name: "rsiPullback",
    description: "RSI pullback inside EMA trend",
    value: rsi,
    signal,
    strength,
    weight,
  };
}

function buildEmaPullbackComponent(
  candles: readonly CandleLike[],
  weight: number,
  thresholds: ComposerThresholds,
): SignalComponent | null {
  if (weight <= 0 || candles.length < 5) return null;

  const fastPeriod = thresholds.trendFastPeriod ?? 9;
  const slowPeriod = thresholds.trendSlowPeriod ?? 21;
  if (candles.length < Math.max(fastPeriod, slowPeriod) + 2) return null;

  const closes = candles.map((c) => c.close);
  const emaFast = calculateEMA(closes, fastPeriod);
  const emaSlow = calculateEMA(closes, slowPeriod);
  const lastFast = emaFast[emaFast.length - 1];
  const lastSlow = emaSlow[emaSlow.length - 1];
  const prevFast = emaFast[emaFast.length - 2];
  const prevClose = candles[candles.length - 2].close;
  const lastClose = candles[candles.length - 1].close;

  const isUptrend = lastFast > lastSlow;
  const isDowntrend = lastFast < lastSlow;

  let signal: Direction = "hold";
  let strength: SignalStrength = "weak";

  // Pullback to the fast EMA inside a trend.
  if (
    isUptrend &&
    prevClose > prevFast &&
    lastClose <= lastFast &&
    lastClose > lastSlow
  ) {
    signal = "buy";
    strength = lastClose > (lastFast + lastSlow) / 2 ? "medium" : "strong";
  } else if (
    isDowntrend &&
    prevClose < prevFast &&
    lastClose >= lastFast &&
    lastClose < lastSlow
  ) {
    signal = "sell";
    strength = lastClose < (lastFast + lastSlow) / 2 ? "medium" : "strong";
  }

  if (signal === "hold") return null;

  const distance = Math.abs(lastClose - lastFast) / lastFast;
  return {
    name: "emaPullback",
    description: "Price pullback to fast EMA inside trend",
    value: distance,
    signal,
    strength,
    weight,
  };
}

interface RegimeSignal {
  readonly signal: Direction;
  readonly strength: SignalStrength;
  readonly volumeRatio: number | null;
}

function resolveReversionRegime(
  percentB: number,
  plusDI: number,
  minusDI: number,
  adx: number,
  allowLong: boolean,
  allowShort: boolean,
  thresholds: ComposerThresholds,
): RegimeSignal {
  if (
    percentB <= thresholds.bollingerEntryMinPct &&
    plusDI >= minusDI &&
    allowLong
  ) {
    return {
      signal: "buy",
      strength: adx > thresholds.adxStrongTrend ? "strong" : "medium",
      volumeRatio: null,
    };
  }
  if (
    percentB >= thresholds.bollingerEntryMaxPct &&
    minusDI >= plusDI &&
    allowShort
  ) {
    return {
      signal: "sell",
      strength: adx > thresholds.adxStrongTrend ? "strong" : "medium",
      volumeRatio: null,
    };
  }
  return { signal: "hold", strength: "weak", volumeRatio: null };
}

function resolveBreakoutRegime(
  candles: readonly CandleLike[],
  adx: number,
  allowLong: boolean,
  allowShort: boolean,
  thresholds: ComposerThresholds,
): RegimeSignal {
  const breakoutLookback = thresholds.breakoutLookback ?? 20;
  if (candles.length < breakoutLookback + 2) {
    return { signal: "hold", strength: "weak", volumeRatio: null };
  }
  const current = candles[candles.length - 1];
  const window = candles.slice(-breakoutLookback - 1, -1);
  const highestHigh = Math.max(...window.map((candle) => candle.high));
  const lowestLow = Math.min(...window.map((candle) => candle.low));
  const volumeRatio = calculateVolumeRatio(
    candles,
    thresholds.volumeLookback ?? 20,
  );
  const minAdx = thresholds.breakoutAdxMin ?? 20;
  const minVolumeRatio = thresholds.breakoutVolumeMinRatio ?? 1.2;
  const strong =
    adx > thresholds.adxStrongTrend &&
    volumeRatio !== null &&
    volumeRatio > 1.5;
  if (adx >= minAdx && volumeRatio !== null && volumeRatio >= minVolumeRatio) {
    if (current.close > highestHigh && allowLong) {
      return {
        signal: "buy",
        strength: strong ? "strong" : "medium",
        volumeRatio,
      };
    }
    if (current.close < lowestLow && allowShort) {
      return {
        signal: "sell",
        strength: strong ? "strong" : "medium",
        volumeRatio,
      };
    }
  }
  return { signal: "hold", strength: "weak", volumeRatio };
}

function resolveTrendRegime(
  adx: number,
  plusDI: number,
  minusDI: number,
  percentB: number,
  allowLong: boolean,
  allowShort: boolean,
  thresholds: ComposerThresholds,
): RegimeSignal {
  const minAdx = thresholds.adxMin ?? thresholds.adxWeakTrend;
  if (
    adx > minAdx &&
    allowLong &&
    plusDI > minusDI &&
    percentB >= thresholds.bollingerEntryMinPct
  ) {
    return {
      signal: "buy",
      strength: adx > thresholds.adxStrongTrend ? "strong" : "medium",
      volumeRatio: null,
    };
  }
  if (
    adx > minAdx &&
    allowShort &&
    minusDI > plusDI &&
    percentB <= thresholds.bollingerEntryMaxPct
  ) {
    return {
      signal: "sell",
      strength: adx > thresholds.adxStrongTrend ? "strong" : "medium",
      volumeRatio: null,
    };
  }
  return { signal: "hold", strength: "weak", volumeRatio: null };
}
function completeRegimeInputs(regime: RegimeAnalysis): {
  readonly adx: number;
  readonly plusDI: number;
  readonly minusDI: number;
  readonly bb: BollingerPosition;
} | null {
  if (
    regime.adx === null ||
    regime.plusDI === null ||
    regime.minusDI === null ||
    regime.atr === null ||
    regime.bollinger === null
  )
    return null;
  return {
    adx: regime.adx,
    plusDI: regime.plusDI,
    minusDI: regime.minusDI,
    bb: regime.bollinger,
  };
}
function buildRegimeComponent(
  candles: readonly CandleLike[],
  ob: OrderBookMetricsInput,
  weight: number,
  thresholds: ComposerThresholds,
): SignalComponent | null {
  if (candles.length < 30) return null;

  const regime = analyzeRegime(candles, thresholds);
  if (regime === null) return null;
  const inputs = completeRegimeInputs(regime);
  if (inputs === null) return null;
  const { adx, plusDI, minusDI, bb } = inputs;

  const atrPct = regime.atrPct;

  // Skip if volatility is excessive (ATR too large relative to price).
  if (thresholds.atrMaxPctOfPrice > 0 && atrPct > thresholds.atrMaxPctOfPrice) {
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
  const resolved =
    mode === "reversion"
      ? resolveReversionRegime(
          bb.percentB,
          plusDI,
          minusDI,
          adx,
          allowLong,
          allowShort,
          thresholds,
        )
      : mode === "breakout"
        ? resolveBreakoutRegime(candles, adx, allowLong, allowShort, thresholds)
        : resolveTrendRegime(
            adx,
            plusDI,
            minusDI,
            bb.percentB,
            allowLong,
            allowShort,
            thresholds,
          );
  return {
    name: "regime",
    description:
      mode === "reversion"
        ? "Bollinger band mean-reversion regime + higher-timeframe trend filter"
        : mode === "breakout"
          ? "Volume-confirmed breakout regime + higher-timeframe trend filter"
          : "ADX trend strength + Bollinger direction + higher-timeframe trend filter",
    value: mode === "breakout" ? (resolved.volumeRatio ?? adx) : adx,
    signal: resolved.signal,
    strength: resolved.strength,
    weight,
  };
}

function buildFundingComponent(
  candles: readonly CandleLike[],
  _ob: OrderBookMetricsInput,
  weight: number,
  thresholds: ComposerThresholds,
  fundingRates?: readonly FundingRate[],
): SignalComponent | null {
  if (weight <= 0 || !thresholds.useFunding) return null;
  if (!fundingRates || fundingRates.length === 0) return null;
  if (candles.length === 0) return null;

  const threshold = thresholds.fundingBiasThreshold ?? 0.0001;
  if (threshold <= 0) return null;

  const lastCandle = candles[candles.length - 1];
  const lastTime = lastCandle.timestamp.getTime();

  let latest: FundingRate | null = null;
  for (const rate of fundingRates) {
    const rateTime = rate.timestamp.getTime();
    if (
      rateTime <= lastTime &&
      (!latest || rateTime > latest.timestamp.getTime())
    ) {
      latest = rate;
    }
  }
  if (!latest) return null;

  const rate = latest.fundingRate;
  if (Math.abs(rate) <= threshold) return null;

  const signal: Direction = rate > threshold ? "sell" : "buy";
  const strength: SignalStrength =
    Math.abs(rate) > 3 * threshold ? "strong" : "medium";

  return {
    name: "funding",
    description: "Perpetual futures funding-rate contrarian bias",
    value: rate,
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

/** Aggregated direction vote with the confidence that drove it. */
interface DirectionVote {
  readonly direction: Direction;
  readonly confidence: number;
}

function aggregateDirection(
  components: SignalComponent[],
  minConfidenceSpread: number,
): DirectionVote {
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

function passesConfluenceFilter(
  components: readonly SignalComponent[],
  direction: Direction,
  minConfluence = 0,
): boolean {
  if (minConfluence <= 0 || direction === "hold") return true;
  const agreeing = components.filter((c) => c.signal === direction).length;
  return agreeing >= minConfluence;
}

function componentCategory(
  name: SignalComponent["name"],
): "leading" | "current" | "lagging" | null {
  switch (name) {
    case "rsiPullback":
    case "emaPullback":
      return "leading";
    case "trend":
    case "imbalance":
    case "spread":
    case "liquidity":
      return "current";
    case "rsi":
    case "connorsRsi2":
    case "volatility":
    case "regime":
    case "funding":
      return "lagging";
    default:
      return null;
  }
}

function passesCategoryConfluenceFilter(
  components: readonly SignalComponent[],
  direction: Direction,
  minCategoryConfluence = 0,
): boolean {
  if (minCategoryConfluence <= 0 || direction === "hold") return true;
  const categories = new Set<string>();
  for (const c of components) {
    if (c.signal === direction) {
      const cat = componentCategory(c.name);
      if (cat) categories.add(cat);
    }
  }
  return categories.size >= minCategoryConfluence;
}

function passesEntryCandleConfirmation(
  candle: CandleLike,
  direction: Direction,
): boolean {
  if (direction === "buy") {
    return candle.close > candle.open;
  }
  if (direction === "sell") {
    return candle.close < candle.open;
  }
  return true;
}

function passesMomentumConfirmation(
  candles: readonly CandleLike[],
  direction: Direction,
  bars: number,
): boolean {
  if (direction === "hold" || candles.length < bars + 1) return true;
  const start = candles[candles.length - bars - 1].close;
  const end = candles[candles.length - 1].close;
  if (start === 0) return true;
  const change = (end - start) / start;
  return direction === "buy" ? change > 0 : change < 0;
}
