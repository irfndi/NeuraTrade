import { describe, expect, it } from "bun:test";
import { Effect, Option } from "effect";
import { Database } from "bun:sqlite";
import {
  MarketDataRepository,
  MarketDataRepositorySQLiteLive,
} from "../market-data/repository.js";
import type { Candle } from "../market-data/types.js";
import { scanProgram } from "./scalp.js";

function makeCandles(
  symbol: string,
  count: number,
  exchange = "binance",
): Candle[] {
  const candles: Candle[] = [];
  let close = 100;
  for (let i = 0; i < count; i++) {
    const open = close;
    close *= i % 7 === 0 ? 1.02 : 0.998;
    const high = Math.max(open, close) * 1.002;
    const low = Math.min(open, close) * 0.998;
    candles.push({
      exchange,
      symbol,
      timeframe: "1h",
      open,
      high,
      low,
      close,
      volume: 10,
      timestamp: new Date(Date.now() - (count - i) * 3_600_000),
    });
  }
  return candles;
}

describe("scanProgram", () => {
  it("runs a per-symbol optimized scan across stored symbols", async () => {
    const db = new Database(":memory:");
    const repoLayer = MarketDataRepositorySQLiteLive(db);

    await Effect.runPromise(
      Effect.gen(function* () {
        const repo = yield* MarketDataRepository;
        yield* repo.ensureTables();
        yield* repo.saveCandles(makeCandles("BTC/USDT", 120));
        yield* repo.saveCandles(makeCandles("ETH/USDT", 120));
      }).pipe(Effect.provide(repoLayer)),
    );

    const results = await Effect.runPromise(
      scanProgram({
        exchange: "binance",
        timeframe: "1h",
        capital: 10_000,
        positionSize: 100,
        riskPerTrade: 0,
        maxPositionSize: 100,
        fee: 0.1,
        minConfidence: 0.5,
        useAtrStops: true,
        atrStopMultiplier: 2.0,
        atrTakeProfitMultiplier: 2.5,
        atrRiskReward: 0,
        scaleOutAtR: 0,
        scaleOutPct: 50,
        volatilityLookback: 0,
        volatilityLowPct: 20,
        volatilityHighPct: 80,
        volatilityLowFactor: 0.8,
        volatilityHighFactor: 1.2,
        volatilityTargetAnnualPct: 0,
        stopLoss: 1.5,
        takeProfit: 3.0,
        priceOnly: true,
        noRsi: false,
        noTrend: true,
        holdUntilStop: false,
        regimeMode: "reversion",
        breakoutLookback: 20,
        breakoutVolumeMinRatio: 1.2,
        breakoutAdxMin: 20,
        useFunding: false,
        fundingBiasThreshold: 0.0001,
        minAtrPct: 0,
        volumeMinRatio: 0,
        volumeLookback: 20,
        minConfluence: 0,
        entryCandleConfirm: false,
        momentumConfirmBars: 0,
        minCandles: 50,
        top: 0,
        optimize: true,
        oosPct: 0,
        selectBy: "return",
        minTrades: 0,
        minOosTrades: 0,
        minReturnPct: Option.none(),
        minSharpe: Option.none(),
        maxDrawdownPct: Option.none(),
        saveWatchlist: Option.none(),
        makerFeePct: 0,
        entryOrderType: "market",
        entryLimitOffsetBps: 0,
        futures: false,
        fundingRatePct: 0.01,
        slippageBps: 0,
        observedPrice: false,
        strictRealism: false,
        realistic: false,
        realisticSlippageBps: 5,
        rsiPeriod: 14,
        rsiOversoldStrong: 30,
        rsiOverboughtStrong: 70,
        mcIterations: 0,
        adxMin: 0,
        trailingStopPct: 0,
        trailingStopAtrMultiplier: 0,
        signalPersistence: 0,
        lossConfidencePenalty: 0,
        lossConfidenceDecay: 0,
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
        strategyType: "signal",
        gridStepPct: 0,
        gridMaxGrids: 0,
        gridPauseAfterLossBars: 0,
      }).pipe(Effect.provide(repoLayer)),
    );

    expect(results.length).toBe(2);
    for (const r of results) {
      expect(r.bestParams).toBeDefined();
      expect(r.totalTrades).toBeGreaterThanOrEqual(0);
    }

    db.close();
  });

  it("scans multiple comma-separated exchanges", async () => {
    const db = new Database(":memory:");
    const repoLayer = MarketDataRepositorySQLiteLive(db);

    await Effect.runPromise(
      Effect.gen(function* () {
        const repo = yield* MarketDataRepository;
        yield* repo.ensureTables();
        yield* repo.saveCandles(makeCandles("BTC/USDT", 120, "binance"));
        yield* repo.saveCandles(makeCandles("BTC/USDT", 120, "bitget"));
        yield* repo.saveCandles(makeCandles("ETH/USDT", 120, "bitget"));
      }).pipe(Effect.provide(repoLayer)),
    );

    const results = await Effect.runPromise(
      scanProgram({
        exchange: "binance,bitget",
        timeframe: "1h",
        capital: 10_000,
        positionSize: 100,
        riskPerTrade: 0,
        maxPositionSize: 100,
        fee: 0.1,
        minConfidence: 0.5,
        useAtrStops: true,
        atrStopMultiplier: 2.0,
        atrTakeProfitMultiplier: 2.5,
        atrRiskReward: 0,
        scaleOutAtR: 0,
        scaleOutPct: 50,
        volatilityLookback: 0,
        volatilityLowPct: 20,
        volatilityHighPct: 80,
        volatilityLowFactor: 0.8,
        volatilityHighFactor: 1.2,
        volatilityTargetAnnualPct: 0,
        stopLoss: 1.5,
        takeProfit: 3.0,
        priceOnly: true,
        noRsi: false,
        noTrend: true,
        holdUntilStop: false,
        regimeMode: "reversion",
        breakoutLookback: 20,
        breakoutVolumeMinRatio: 1.2,
        breakoutAdxMin: 20,
        useFunding: false,
        fundingBiasThreshold: 0.0001,
        minAtrPct: 0,
        volumeMinRatio: 0,
        volumeLookback: 20,
        minConfluence: 0,
        entryCandleConfirm: false,
        momentumConfirmBars: 0,
        minCandles: 50,
        top: 0,
        optimize: false,
        oosPct: 0,
        selectBy: "return",
        minTrades: 0,
        minOosTrades: 0,
        minReturnPct: Option.none(),
        minSharpe: Option.none(),
        maxDrawdownPct: Option.none(),
        saveWatchlist: Option.none(),
        futures: false,
        fundingRatePct: 0.01,
        slippageBps: 0,
        makerFeePct: 0,
        entryOrderType: "market",
        entryLimitOffsetBps: 0,
        observedPrice: false,
        strictRealism: false,
        realistic: false,
        realisticSlippageBps: 5,
        rsiPeriod: 14,
        rsiOversoldStrong: 30,
        rsiOverboughtStrong: 70,
        mcIterations: 0,
        adxMin: 0,
        trailingStopPct: 0,
        trailingStopAtrMultiplier: 0,
        signalPersistence: 0,
        lossConfidencePenalty: 0,
        lossConfidenceDecay: 0,
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
        strategyType: "signal",
        gridStepPct: 0,
        gridMaxGrids: 0,
        gridPauseAfterLossBars: 0,
      }).pipe(Effect.provide(repoLayer)),
    );

    const exchanges = new Set(results.map((r) => r.exchange));
    expect(exchanges.has("binance")).toBe(true);
    expect(exchanges.has("bitget")).toBe(true);
    expect(
      results.some((r) => r.exchange === "binance" && r.symbol === "BTC/USDT"),
    ).toBe(true);
    expect(
      results.some((r) => r.exchange === "bitget" && r.symbol === "BTC/USDT"),
    ).toBe(true);
    expect(
      results.some((r) => r.exchange === "bitget" && r.symbol === "ETH/USDT"),
    ).toBe(true);

    db.close();
  });
});
