import { describe, expect, it } from "bun:test";
import { Schema } from "effect";
import { CollectionConfig, Timeframe } from "./types.js";

describe("market-data types", () => {
  describe("Timeframe", () => {
    it("accepts valid timeframes", () => {
      const valid = ["1m", "5m", "15m", "30m", "1h", "4h", "1d"] as const;
      for (const tf of valid) {
        expect(Schema.decodeUnknownSync(Timeframe)(tf)).toBe(tf);
      }
    });

    it("rejects invalid timeframes", () => {
      expect(() => Schema.decodeUnknownSync(Timeframe)("2m")).toThrow();
      expect(() => Schema.decodeUnknownSync(Timeframe)("1w")).toThrow();
    });
  });

  describe("CollectionConfig", () => {
    it("parses a minimal config", () => {
      const cfg = Schema.decodeUnknownSync(CollectionConfig)({
        exchange: "binance",
        symbol: "BTC/USDT",
      });

      expect(cfg.exchange).toBe("binance");
      expect(cfg.symbol).toBe("BTC/USDT");
      expect(cfg.timeframe).toBe("1m");
      expect(cfg.enabled).toBe(true);
    });

    it("preserves explicit values", () => {
      const cfg = Schema.decodeUnknownSync(CollectionConfig)({
        exchange: "coinbase",
        symbol: "ETH/USD",
        timeframe: "5m",
        enabled: false,
      });

      expect(cfg.timeframe).toBe("5m");
      expect(cfg.enabled).toBe(false);
    });
  });
});
