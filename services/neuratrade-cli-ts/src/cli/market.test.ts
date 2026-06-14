import { describe, expect, it } from "bun:test";
import {
  chunksForRange,
  parseDate,
  parseSymbols,
  resolveDateRange,
  timeframeToIntervalMs,
} from "./market.ts";

describe("market command helpers", () => {
  describe("parseDate", () => {
    it("parses ISO date strings", () => {
      const date = parseDate("2025-01-01");
      expect(date.toISOString()).toBe("2025-01-01T00:00:00.000Z");
    });

    it("parses RFC3339 strings", () => {
      const date = parseDate("2025-06-15T12:30:00Z");
      expect(date.toISOString()).toBe("2025-06-15T12:30:00.000Z");
    });

    it("throws on invalid input", () => {
      expect(() => parseDate("not-a-date")).toThrow();
    });
  });

  describe("resolveDateRange", () => {
    it("uses provided dates", () => {
      const range = resolveDateRange("2025-01-01", "2025-02-01");
      expect(range.start.toISOString()).toBe("2025-01-01T00:00:00.000Z");
      expect(range.end.toISOString()).toBe("2025-02-01T00:00:00.000Z");
    });

    it("rejects start >= end", () => {
      expect(() => resolveDateRange("2025-02-01", "2025-01-01")).toThrow(
        "before",
      );
    });
  });

  describe("parseSymbols", () => {
    it("splits and uppercases comma-separated symbols", () => {
      expect(parseSymbols("btc/usdt,eth/usdt")).toEqual([
        "BTC/USDT",
        "ETH/USDT",
      ]);
    });

    it("ignores empty entries", () => {
      expect(parseSymbols("BTC/USDT,,  ,ETH/USDT")).toEqual([
        "BTC/USDT",
        "ETH/USDT",
      ]);
    });
  });

  describe("timeframeToIntervalMs", () => {
    it("converts known timeframes", () => {
      expect(timeframeToIntervalMs("1m")).toBe(60_000);
      expect(timeframeToIntervalMs("1h")).toBe(3_600_000);
      expect(timeframeToIntervalMs("1d")).toBe(86_400_000);
    });

    it("throws on unsupported timeframes", () => {
      expect(() => timeframeToIntervalMs("xyz")).toThrow();
    });
  });

  describe("chunksForRange", () => {
    it("splits a range into 1000-candle chunks", () => {
      const start = new Date("2025-01-01T00:00:00Z");
      const end = new Date("2025-01-01T05:00:00Z");
      const chunks = chunksForRange(start, end, 3_600_000, 2);
      expect(chunks).toHaveLength(3);
      expect(chunks[0].from.toISOString()).toBe("2025-01-01T00:00:00.000Z");
      expect(chunks[0].to.toISOString()).toBe("2025-01-01T02:00:00.000Z");
      expect(chunks[2].to.toISOString()).toBe("2025-01-01T05:00:00.000Z");
    });
  });
});
