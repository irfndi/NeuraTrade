import { describe, expect, it } from "bun:test";
import { applyPreset, listPresets, type PresetName } from "./presets.js";
import type { ResolvedBacktestArgs } from "./strategy-profile.js";

describe("applyPreset", () => {
  it("returns a full ResolvedBacktestArgs object", () => {
    const args = applyPreset("balanced");
    expect(args.symbol).toBe("BTC/USDT");
    expect(args.timeframe).toBe("1h");
    expect(args.useAtrStops).toBe(true);
    expect(args.minConfidence).toBe(0.55);
    expect(args.realistic).toBe(true);
    expect(args.observedPrice).toBe(false);
  });

  it.each(["conservative", "balanced", "aggressive"] as PresetName[])(
    "%s preset has realistic true and observedPrice false by default",
    (name) => {
      const args = applyPreset(name);
      expect(args.realistic).toBe(true);
      expect(args.observedPrice).toBe(false);
    },
  );

  it("allows CLI overrides to win against preset defaults", () => {
    const overrides: Partial<ResolvedBacktestArgs> = {
      symbol: "ETH/USDT",
      timeframe: "4h",
      capital: 5000,
      observedPrice: false,
      minConfidence: 0.8,
    };
    const args = applyPreset("balanced", overrides);
    expect(args.symbol).toBe("ETH/USDT");
    expect(args.timeframe).toBe("4h");
    expect(args.capital).toBe(5000);
    expect(args.observedPrice).toBe(false);
    expect(args.minConfidence).toBe(0.8);
  });

  it("conservative uses wider ATR stops and higher confidence than aggressive", () => {
    const conservative = applyPreset("conservative");
    const aggressive = applyPreset("aggressive");
    expect(conservative.atrStopMultiplier).toBeGreaterThan(
      aggressive.atrStopMultiplier,
    );
    expect(conservative.minConfidence).toBeGreaterThan(
      aggressive.minConfidence,
    );
    expect(conservative.riskPerTrade).toBeLessThanOrEqual(
      aggressive.riskPerTrade,
    );
  });
});

describe("listPresets", () => {
  it("returns all three presets with descriptions and highlights", () => {
    const presets = listPresets();
    expect(presets.map((p) => p.name)).toEqual([
      "conservative",
      "balanced",
      "aggressive",
    ]);
    for (const preset of presets) {
      expect(preset.description.length).toBeGreaterThan(0);
      expect(preset.highlights.length).toBeGreaterThan(0);
      expect(preset.highlights.some((h) => h.includes("realistic"))).toBe(true);
    }
  });
});
