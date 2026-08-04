import { describe, expect, it } from "bun:test";
import { BunFileSystem } from "@effect/platform-bun";
import { Effect } from "effect";
import * as fs from "fs";
import * as os from "os";
import * as path from "path";
import {
  buildStrategyProfileFromArgs,
  findSymbolOverride,
  loadStrategyProfile,
  resolveBacktestArgs,
  saveStrategyProfile,
  type StrategyProfile,
  type ResolvedBacktestArgs,
  type StrategyProfileParams,
} from "./strategy-profile.js";

function tmpHome(): string {
  return fs.mkdtempSync(path.join(os.tmpdir(), "strategy-profile-test-"));
}

function defaultResolvedArgs(
  overrides: Partial<ResolvedBacktestArgs> = {},
): ResolvedBacktestArgs {
  return {
    exchange: "binance",
    symbol: "BTC/USDT",
    timeframe: "1h",
    capital: 10_000,
    positionSize: 100,
    riskPerTrade: 0,
    maxPositionSize: 100,
    stopLoss: 1.5,
    takeProfit: 3,
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
    fundingBiasThreshold: 0.0001,
    useFunding: false,
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
    htfTimeframe: undefined,
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
    strategyType: "signal",
    gridStepPct: 0,
    gridMaxGrids: 0,
    gridPauseAfterLossBars: 0,
    ...overrides,
  };
}

describe("strategy-profile load/save", () => {
  it("saves and loads a profile", async () => {
    const home = tmpHome();
    const profile: StrategyProfile = {
      name: "default",
      defaults: {
        minConfidence: 0.6,
        stopLossPct: 2,
        takeProfitPct: 4,
        positionSizePct: 50,
      } as StrategyProfileParams,
      symbols: {
        "ETH/USDT": {
          stopLossPct: 1,
        },
      },
    };

    await Effect.runPromise(
      saveStrategyProfile(home, "default", profile).pipe(
        Effect.provide(BunFileSystem.layer),
      ),
    );
    const loaded = await Effect.runPromise(
      loadStrategyProfile(home, "default"),
    );

    expect(loaded.name).toBe("default");
    expect(loaded.defaults.minConfidence).toBe(0.6);
    expect(loaded.defaults.stopLossPct).toBe(2);
    expect(loaded.symbols["ETH/USDT"].stopLossPct).toBe(1);

    fs.rmSync(home, { recursive: true, force: true });
  });

  it("fails to load a missing profile", async () => {
    const home = tmpHome();
    const result = await Effect.runPromise(
      Effect.result(loadStrategyProfile(home, "missing")),
    );
    expect(result._tag).toBe("Failure");
    fs.rmSync(home, { recursive: true, force: true });
  });
});

describe("resolveBacktestArgs", () => {
  it("uses profile defaults when CLI args are present (CLI wins)", () => {
    const profile: StrategyProfile = {
      defaults: {
        minConfidence: 0.6,
        stopLossPct: 2,
        takeProfitPct: 4,
      } as StrategyProfileParams,
      symbols: {},
    };
    const cli = defaultResolvedArgs({
      minConfidence: 0.7,
      stopLoss: 1,
      takeProfit: 2,
    });
    const resolved = resolveBacktestArgs(
      profile,
      cli.symbol,
      cli.exchange,
      cli.timeframe,
      cli,
    );
    expect(resolved.minConfidence).toBe(0.7);
    expect(resolved.stopLoss).toBe(1);
    expect(resolved.takeProfit).toBe(2);
  });

  it("applies per-symbol overrides", () => {
    const profile: StrategyProfile = {
      defaults: {
        minConfidence: 0.6,
        stopLossPct: 2,
      } as StrategyProfileParams,
      symbols: {
        "ETH/USDT": {
          stopLossPct: 1,
        },
      },
    };
    const cli = defaultResolvedArgs({ symbol: "ETH/USDT", stopLoss: 3 });
    const resolved = resolveBacktestArgs(
      profile,
      cli.symbol,
      cli.exchange,
      cli.timeframe,
      cli,
    );
    expect(resolved.stopLoss).toBe(3);
  });

  it("maps regimeMode from the profile when CLI is at the default", () => {
    const profile: StrategyProfile = {
      defaults: { regimeMode: "reversion" } as StrategyProfileParams,
      symbols: {},
    };
    const cli = defaultResolvedArgs({ regimeMode: "trend" });
    const resolved = resolveBacktestArgs(
      profile,
      cli.symbol,
      cli.exchange,
      cli.timeframe,
      cli,
    );
    expect(resolved.regimeMode).toBe("reversion");
  });

  it("lets an explicit CLI regimeMode win over the profile", () => {
    const profile: StrategyProfile = {
      defaults: { regimeMode: "reversion" } as StrategyProfileParams,
      symbols: {},
    };
    const cli = defaultResolvedArgs({ regimeMode: "breakout" });
    const resolved = resolveBacktestArgs(
      profile,
      cli.symbol,
      cli.exchange,
      cli.timeframe,
      cli,
    );
    expect(resolved.regimeMode).toBe("breakout");
  });

  it("matches spot-keyed overrides for futures symbols (settle suffix stripped)", () => {
    const profile: StrategyProfile = {
      defaults: {} as StrategyProfileParams,
      symbols: {
        "BTC/USDT": { atrStopMultiplier: 1 },
      },
    };
    const cli = defaultResolvedArgs({ symbol: "BTC/USDT:USDT" });
    const resolved = resolveBacktestArgs(
      profile,
      cli.symbol,
      cli.exchange,
      cli.timeframe,
      cli,
    );
    expect(resolved.atrStopMultiplier).toBe(1);
  });

  it("matches futures-keyed overrides for spot symbols", () => {
    const profile: StrategyProfile = {
      defaults: {} as StrategyProfileParams,
      symbols: {
        "BTC/USDT:USDT": { atrStopMultiplier: 0.8 },
      },
    };
    const cli = defaultResolvedArgs({ symbol: "BTC/USDT" });
    const resolved = resolveBacktestArgs(
      profile,
      cli.symbol,
      cli.exchange,
      cli.timeframe,
      cli,
    );
    expect(resolved.atrStopMultiplier).toBe(0.8);
  });

  it("prefers exact symbol key over stripped match", () => {
    const profile: StrategyProfile = {
      defaults: {} as StrategyProfileParams,
      symbols: {
        "BTC/USDT": { atrStopMultiplier: 1 },
        "BTC/USDT:USDT": { atrStopMultiplier: 0.5 },
      },
    };
    const cli = defaultResolvedArgs({ symbol: "BTC/USDT:USDT" });
    const resolved = resolveBacktestArgs(
      profile,
      cli.symbol,
      cli.exchange,
      cli.timeframe,
      cli,
    );
    expect(resolved.atrStopMultiplier).toBe(0.5);
  });

  it("exposes whether any symbol override matched (for loud warnings)", () => {
    const profile: StrategyProfile = {
      defaults: {} as StrategyProfileParams,
      symbols: { "ETH/USDT": { atrStopMultiplier: 1 } },
    };
    expect(findSymbolOverride(profile, "BTC/USDT:USDT")).toBeUndefined();
    expect(findSymbolOverride(profile, "ETH/USDT:USDT")).toEqual({
      atrStopMultiplier: 1,
    });
  });

  it("fills defaults from profile for mapped fields", () => {
    const profile: StrategyProfile = {
      defaults: {
        minConfidence: 0.6,
        stopLossPct: 2,
        takeProfitPct: 4,
        positionSizePct: 50,
        feePct: 0.05,
      } as StrategyProfileParams,
      symbols: {},
    };
    const cli = defaultResolvedArgs({
      minConfidence: 0,
      stopLoss: 0,
      takeProfit: 0,
      positionSize: 0,
      fee: 0,
    });
    const resolved = resolveBacktestArgs(
      profile,
      cli.symbol,
      cli.exchange,
      cli.timeframe,
      cli,
    );
    expect(resolved.minConfidence).toBe(0);
    expect(resolved.stopLoss).toBe(0);
  });

  it("does not let CLI defaults overwrite profile values", () => {
    const profile: StrategyProfile = {
      defaults: {
        minConfidence: 0.8,
        stopLossPct: 1,
        takeProfitPct: 2,
        useAtrStops: true,
        atrStopMultiplier: 3,
        atrRiskReward: 4,
        riskPerTradePct: 0.5,
      } as StrategyProfileParams,
      symbols: {},
    };
    // All CLI values below are the built-in command defaults, so they should
    // NOT clobber the profile values.
    const cli = defaultResolvedArgs();
    const resolved = resolveBacktestArgs(
      profile,
      cli.symbol,
      cli.exchange,
      cli.timeframe,
      cli,
    );
    expect(resolved.minConfidence).toBe(0.8);
    expect(resolved.stopLoss).toBe(1);
    expect(resolved.takeProfit).toBe(2);
    expect(resolved.useAtrStops).toBe(true);
    expect(resolved.atrStopMultiplier).toBe(3);
    expect(resolved.atrRiskReward).toBe(4);
    expect(resolved.riskPerTrade).toBe(0.5);
  });

  it("fills values not provided by the profile from CLI defaults", () => {
    const profile: StrategyProfile = {
      defaults: {
        minConfidence: 0.6,
      } as StrategyProfileParams,
      symbols: {},
    };
    const cli = defaultResolvedArgs();
    const resolved = resolveBacktestArgs(
      profile,
      cli.symbol,
      cli.exchange,
      cli.timeframe,
      cli,
    );
    expect(resolved.capital).toBe(10_000);
    expect(resolved.regimeMode).toBe("trend");
    expect(resolved.futures).toBe(false);
  });

  it("applies per-symbol timeframe overrides", () => {
    const profile: StrategyProfile = {
      defaults: {
        timeframe: "5m",
      } as StrategyProfileParams,
      symbols: {
        "BNB/USDT": {
          timeframe: "1h",
        },
      },
    };
    const cli = defaultResolvedArgs({ symbol: "BNB/USDT" });
    const resolved = resolveBacktestArgs(
      profile,
      cli.symbol,
      cli.exchange,
      cli.timeframe,
      cli,
    );
    expect(resolved.timeframe).toBe("1h");
  });
});

describe("buildStrategyProfileFromArgs", () => {
  it("maps CLI arg names to profile parameter names", () => {
    const args = defaultResolvedArgs({
      stopLoss: 2.5,
      takeProfit: 5,
      positionSize: 75,
      fee: 0.2,
    });
    const profile = buildStrategyProfileFromArgs("aggressive", args);
    expect(profile.name).toBe("aggressive");
    expect(profile.defaults.stopLossPct).toBe(2.5);
    expect(profile.defaults.takeProfitPct).toBe(5);
    expect(profile.defaults.positionSizePct).toBe(75);
    expect(profile.defaults.feePct).toBe(0.2);
    expect(profile.symbols).toEqual({});
  });
});
