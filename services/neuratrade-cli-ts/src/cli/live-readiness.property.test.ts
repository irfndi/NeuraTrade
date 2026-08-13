import { describe, expect, it } from "bun:test";
import * as fc from "fast-check";
import {
  validateLiveGridConfiguration,
  validateLiveSandboxMode,
  type LiveGridConfiguration,
} from "./scalp.js";

const candidate: LiveGridConfiguration = {
  exchange: "bybit-futures",
  symbol: "BTC/USDT:USDT",
  timeframe: "15m",
  productType: "USDT-FUTURES",
  gridStepPct: 0.5,
  gridMaxGrids: 3,
  gridPauseAfterLossBars: 48,
  feePct: 0.02,
  slippageBps: 1,
  trendFilterPeriod: 0,
  onlyWithTrend: false,
  targetRatio: 2,
  chopGateAdx: 15,
  leverage: 1,
  maxPositionSizePct: 50,
  maxDrawdownPct: 5,
  maxDailyLossPct: 2,
};

describe("validated live grid profile properties", () => {
  it("rejects every non-finite risk cap", () => {
    fc.assert(
      fc.property(
        fc.constantFrom(
          "maxPositionSizePct",
          "maxDrawdownPct",
          "maxDailyLossPct",
        ),
        fc.constantFrom(Number.NaN, Number.POSITIVE_INFINITY),
        (field, value) => {
          const config = { ...candidate, [field]: value };
          expect(validateLiveGridConfiguration(config)).toBeDefined();
        },
      ),
      { numRuns: 6 },
    );
  });

  it("requires the exchange sandbox for every live execution", () => {
    fc.assert(
      fc.property(fc.boolean(), (sandbox) => {
        const result = validateLiveSandboxMode(true, sandbox);
        if (sandbox) {
          expect(result).toBeUndefined();
        } else {
          expect(result).toBeDefined();
        }
        expect(validateLiveSandboxMode(false, sandbox)).toBeUndefined();
      }),
      { numRuns: 100 },
    );
  });
});
