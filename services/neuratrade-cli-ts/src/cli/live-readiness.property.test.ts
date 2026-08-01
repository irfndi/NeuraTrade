import { describe, expect, it } from "bun:test";
import * as fc from "fast-check";
import {
  validateLiveGridConfiguration,
  type LiveGridConfiguration,
} from "./scalp.js";

const candidate: LiveGridConfiguration = {
  exchange: "bitget-futures",
  symbol: "BTC/USDT:USDT",
  timeframe: "15m",
  productType: "USDT-FUTURES",
  gridStepPct: 1,
  gridMaxGrids: 1.5,
  gridPauseAfterLossBars: 12,
  feePct: 0.02,
  slippageBps: 1,
  trendFilterPeriod: 0,
  onlyWithTrend: false,
  targetRatio: 1,
  chopGateAdx: 30,
  leverage: 1,
  maxPositionSizePct: 50,
  maxDrawdownPct: 5,
  maxDailyLossPct: 2,
};

type RiskCap = "maxPositionSizePct" | "maxDrawdownPct" | "maxDailyLossPct";

describe("validated live grid profile properties", () => {
  it("rejects every non-finite risk cap", () => {
    fc.assert(
      fc.property(
        fc.constantFrom<RiskCap>(
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
});
