import { describe, expect, it } from "bun:test";
import {
  buildBacktestArgsFromTemplate,
  buildComposerConfigFromTemplate,
  listStrategies,
  type StrategyTemplateName,
} from "./strategy-library.js";
import type { ResolvedBacktestArgs } from "./strategy-profile.js";

function makeBaseArgs(
  overrides: Partial<ResolvedBacktestArgs> = {},
): ResolvedBacktestArgs {
  return {
    exchange: "binance",
    symbol: "BTC/USDT",
    timeframe: "1h",
    capital: 10000,
    positionSize: 100,
    riskPerTrade: 0,
    maxPositionSize: 100,
    stopLoss: 1.5,
    takeProfit: 3.0,
    fee: 0.1,
    makerFeePct: 0,
    entryOrderType: "market",
    entryLimitOffsetBps: 0,
    minConfidence: 0.5,
    useAtrStops: false,
    atrStopMultiplier: 1.5,
    atrTakeProfitMultiplier: 2.5,
    atrRiskReward: 0,
    rsiPeriod: 14,
    rsiOversoldStrong: 30,
    rsiOverboughtStrong: 70,
    scaleOutAtR: 0,
    scaleOutPct: 50,
    volatilityLookback: 0,
    volatilityLowPct: 20,
    volatilityHighPct: 80,
    volatilityLowFactor: 0.8,
    volatilityHighFactor: 1.2,
    volatilityTargetAnnualPct: 0,
    priceOnly: false,
    noRsi: false,
    holdUntilStop: false,
    noTrend: false,
    regimeMode: "trend",
    breakoutLookback: 20,
    breakoutVolumeMinRatio: 1.2,
    breakoutAdxMin: 20,
    useFunding: false,
    fundingBiasThreshold: 0.0001,
    futures: false,
    fundingRatePct: 0.01,
    slippageBps: 0,
    trailingStopPct: 0,
    trailingStopAtrMultiplier: 0,
    minAtrPct: 0,
    volumeMinRatio: 0,
    volumeLookback: 20,
    minConfluence: 0,
    entryCandleConfirm: false,
    signalPersistence: 0,
    momentumConfirmBars: 0,
    lossConfidencePenalty: 0,
    lossConfidenceDecay: 0,
    adxMin: 0,
    htfTrendFastPeriod: 50,
    htfTrendSlowPeriod: 100,
    htfSignalConfidence: 0,
    entryPullbackEmaPeriod: 0,
    entryPullbackMarginPct: 0.1,
    minEfficiencyRatio: 0,
    efficiencyRatioPeriod: 20,
    rsiLongMax: 0,
    rsiShortMin: 0,
    bollingerLongMaxPctB: -1,
    bollingerShortMinPctB: 2,
    trendFilterPeriod: 200,
    entryRsiLongThreshold: 10,
    entryRsiShortThreshold: 90,
    exitRsiPeriod: 0,
    exitRsiLongLevel: 0,
    exitRsiShortLevel: 0,
    recordEquityCurve: false,
    exportTrades: "",
    oosPct: 0,
    mcIterations: 0,
    leverage: 1,
    breakevenAtR: 0,
    maxBarsInTrade: 0,
    lossCooldownBars: 0,
    sessionStart: "",
    sessionEnd: "",
    autoRegimeFilter: false,
    autoRegimeAdxThreshold: 25,
    trendSignalStyle: "slope",
    trendFastPeriod: 9,
    trendSlowPeriod: 21,
    directionalOnly: false,
    rsiFollowTrend: false,
    strictAgreement: false,
    entryOnClose: false,
    observedPrice: false,
    realistic: false,
    strictRealism: false,
    realisticSlippageBps: 5,
    ...overrides,
  } as ResolvedBacktestArgs;
}

describe("strategy library", () => {
  it("listStrategies() returns at least 5 templates", () => {
    const strategies = listStrategies();
    expect(strategies.length).toBeGreaterThanOrEqual(5);
    const names = strategies.map((s) => s.name);
    expect(names).toContain("meanReversion");
    expect(names).toContain("trendFollowing");
    expect(names).toContain("breakout");
    expect(names).toContain("emaPullback");
    expect(names).toContain("momentum");
    expect(names).toContain("rangeExpansion");
    expect(names).toContain("fundingCarry");
    expect(names).toContain("microScalp");
    expect(names).toContain("connorsRsi2");
  });

  it("buildBacktestArgsFromTemplate('breakout') sets regimeMode: 'breakout'", () => {
    const base = makeBaseArgs({ regimeMode: "trend" });
    const args = buildBacktestArgsFromTemplate("breakout", base);
    expect(args.regimeMode).toBe("breakout");
    expect(args.useAtrStops).toBe(true);
    expect(args.atrStopMultiplier).toBe(1.5);
    expect(args.atrTakeProfitMultiplier).toBe(2.0);
  });

  it("buildBacktestArgsFromTemplate leaves zero overrides alone", () => {
    const base = makeBaseArgs({ stopLoss: 2.0 });
    const args = buildBacktestArgsFromTemplate("emaPullback", base);
    // emaPullback does not override stopLoss, so the base value is preserved.
    expect(args.stopLoss).toBe(2.0);
  });

  it("buildComposerConfigFromTemplate('emaPullback') enables emaPullback", () => {
    const config = buildComposerConfigFromTemplate("emaPullback");
    expect(config.weights.emaPullback).toBeGreaterThan(0);
    expect(config.enabled?.emaPullback).not.toBe(false);
  });

  it("buildComposerConfigFromTemplate('fundingCarry') enables funding", () => {
    const config = buildComposerConfigFromTemplate("fundingCarry");
    expect(config.weights.funding).toBeGreaterThan(0);
    expect(config.thresholds.useFunding).toBe(true);
    expect(config.thresholds.fundingBiasThreshold).toBe(0.0001);
  });

  it("buildBacktestArgsFromTemplate('fundingCarry') sets useFunding", () => {
    const base = makeBaseArgs();
    const args = buildBacktestArgsFromTemplate("fundingCarry", base);
    expect(args.useFunding).toBe(true);
    expect(args.fundingBiasThreshold).toBe(0.0001);
    expect(args.regimeMode).toBe("reversion");
  });

  it("buildComposerConfigFromTemplate normalizes weights", () => {
    const config = buildComposerConfigFromTemplate("trendFollowing");
    const sum = Object.values(config.weights).reduce((a, b) => a + b, 0);
    expect(sum).toBeCloseTo(1.0, 5);
  });

  it("buildBacktestArgsFromTemplate('microScalp') uses fixed TP/SL like manual scalping", () => {
    const base = makeBaseArgs();
    const args = buildBacktestArgsFromTemplate("microScalp", base);
    expect(args.regimeMode).toBe("reversion");
    expect(args.useAtrStops).toBe(false);
    expect(args.stopLoss).toBe(0.3);
    expect(args.takeProfit).toBe(0.8);
    expect(args.maxBarsInTrade).toBe(0);
  });

  it("buildComposerConfigFromTemplate('microScalp') uses RSI(2) extremes", () => {
    const config = buildComposerConfigFromTemplate("microScalp");
    expect(config.thresholds.rsiPeriod).toBe(2);
    expect(config.thresholds.rsiOversoldStrong).toBe(5);
    expect(config.thresholds.rsiOverboughtStrong).toBe(95);
    expect(config.thresholds.regimeMode).toBe("reversion");
    expect(config.weights.rsi).toBeGreaterThan(0);
  });

  it("buildComposerConfigFromTemplate('connorsRsi2') enables the Connors component", () => {
    const config = buildComposerConfigFromTemplate("connorsRsi2");
    expect(config.weights.connorsRsi2).toBeGreaterThan(0);
    expect(config.thresholds.trendFilterPeriod).toBe(200);
    expect(config.thresholds.entryRsiLongThreshold).toBe(10);
    expect(config.thresholds.entryRsiShortThreshold).toBe(90);
  });

  it("buildBacktestArgsFromTemplate('connorsRsi2') disables ATR stops and uses RSI exits", () => {
    const base = makeBaseArgs();
    const args = buildBacktestArgsFromTemplate("connorsRsi2", base);
    expect(args.useAtrStops).toBe(false);
    expect(args.exitRsiPeriod).toBe(2);
    expect(args.exitRsiLongLevel).toBe(60);
    expect(args.exitRsiShortLevel).toBe(40);
    expect(args.regimeMode).toBe("reversion");
  });

  it.each([
    "meanReversion",
    "trendFollowing",
    "breakout",
    "emaPullback",
    "momentum",
    "rangeExpansion",
    "fundingCarry",
    "microScalp",
    "connorsRsi2",
  ] as StrategyTemplateName[])(
    "buildComposerConfigFromTemplate(%s) is valid",
    (name) => {
      const config = buildComposerConfigFromTemplate(name);
      const sum = Object.values(config.weights).reduce((a, b) => a + b, 0);
      expect(sum).toBeCloseTo(1.0, 5);
    },
  );
});
