import { describe, expect, it } from "bun:test";
import * as fc from "fast-check";
import {
  fingerprintStrategyManifest,
  type StrategyManifest,
} from "./real-money-readiness.js";

describe("real-money readiness fingerprint properties", () => {
  it("is invariant to object key order", () => {
    fc.assert(
      fc.property(
        fc.record({
          schema: fc.constant("real-money-readiness/v1"),
          exchange: fc.constant("bitget-demo"),
          symbol: fc.constant("BTC/USDT:USDT"),
          timeframe: fc.constant("15m"),
          gridStepPct: fc.integer({ min: 1, max: 2 }).map(String),
          gridMaxGrids: fc.integer({ min: 1, max: 2 }).map(String),
          gridPauseAfterLossBars: fc.integer({ min: 0, max: 24 }).map(String),
          positionFraction: fc.constant("0.5"),
          feePct: fc.constant("0.06"),
          slippageBps: fc.constant("2"),
          trendFilterPeriod: fc.constant("96"),
          adxGate: fc.constant("30"),
          orderType: fc.constant("market-after-trigger"),
          triggerTiming: fc.constant("next-bar"),
          engineVersion: fc.constant("grid-engine/v1"),
          protocolVersion: fc.constant("real-money-readiness/v1"),
        }),
        (manifest) => {
          const reordered: StrategyManifest = {
            protocolVersion: manifest.protocolVersion,
            engineVersion: manifest.engineVersion,
            triggerTiming: manifest.triggerTiming,
            orderType: manifest.orderType,
            adxGate: manifest.adxGate,
            trendFilterPeriod: manifest.trendFilterPeriod,
            slippageBps: manifest.slippageBps,
            feePct: manifest.feePct,
            positionFraction: manifest.positionFraction,
            gridPauseAfterLossBars: manifest.gridPauseAfterLossBars,
            gridMaxGrids: manifest.gridMaxGrids,
            gridStepPct: manifest.gridStepPct,
            timeframe: manifest.timeframe,
            symbol: manifest.symbol,
            exchange: manifest.exchange,
            schema: manifest.schema,
          };
          expect(fingerprintStrategyManifest(manifest)).toBe(
            fingerprintStrategyManifest(reordered),
          );
        },
      ),
      { numRuns: 50 },
    );
  });
});
