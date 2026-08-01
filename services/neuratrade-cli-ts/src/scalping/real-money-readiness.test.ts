import { describe, expect, it } from "bun:test";
import {
  DEFAULT_READINESS_THRESHOLDS,
  READINESS_GATE_IDS,
  READINESS_SCHEMA_VERSION,
  evaluateRealMoneyReadiness,
  fingerprintStrategyManifest,
  type RealMoneyReadinessInput,
  type StrategyManifest,
} from "./real-money-readiness.js";

function passingInput(): RealMoneyReadinessInput {
  return {
    prospectiveEvidence: {
      completeTradeCount: 60,
      durationDays: 10,
      expectancyPct: "0.12",
      confidenceLowerBoundPct: "0.01",
      maximumDrawdownPct: "4.2",
      allTradesHaveLiveFillEvidence: true,
    },
    historicalRobustness: {
      completeWindows: 12,
      profitableWindowPct: 66.6667,
      compoundedReturnPct: "4.5",
      maximumDrawdownPct: "11.2",
      totalTrades: 120,
    },
    confidence: {
      sampleCount: 120,
      lowerBoundPct: "0.02",
      upperBoundPct: "0.19",
      resamples: 5000,
      blockLength: 5,
      seed: 20260802,
    },
    executionParity: {
      passed: true,
      protocolVersion: "execution-parity/v1",
      checks: [
        "trigger-bar",
        "order-type",
        "fill-price",
        "fees",
        "slippage",
        "quantity",
        "exit-reason",
        "pnl",
      ],
    },
    stress: {
      returnPct: "0.5",
      lowerBoundPct: "0.01",
      seeds: [20260802, 20260803, 20260804, 20260805, 20260806],
    },
    provenance: {
      valid: true,
      fingerprint: "a".repeat(64),
      expectedFingerprint: "a".repeat(64),
      cohortId: "cohort-2026-08-02",
      candidateLock: "2026-07-01T00:00:00.000Z",
      datasetCutoff: "2026-07-31T23:45:00.000Z",
      earliestEntry: "2026-08-01T00:00:00.000Z",
      latestClose: "2026-08-02T00:00:00.000Z",
      queriedRows: 60,
      expectedRows: 60,
    },
    dataQuality: {
      valid: true,
      candleCount: 20_000,
      completeWindows: 12,
      latestCandle: "2026-08-01T23:45:00.000Z",
    },
    evaluatedAt: "2026-08-02T00:00:00.000Z",
    manifest: {
      schema: "real-money-readiness/v1",
      exchange: "bitget-demo",
      symbol: "BTC/USDT:USDT",
      timeframe: "15m",
      gridStepPct: "1",
      gridMaxGrids: "1.5",
      gridPauseAfterLossBars: "12",
      positionFraction: "0.5",
      feePct: "0.06",
      slippageBps: "2",
      trendFilterPeriod: "96",
      adxGate: "30",
      orderType: "market-after-trigger",
      triggerTiming: "next-bar",
      engineVersion: "grid-engine/v1",
      protocolVersion: "real-money-readiness/v1",
    },
  };
}

describe("real-money readiness contract", () => {
  it("returns a deterministic PASS report for complete qualifying evidence", () => {
    const first = evaluateRealMoneyReadiness(passingInput());
    const second = evaluateRealMoneyReadiness(passingInput());

    expect(first.status).toBe("PASS");
    expect(first).toEqual(second);
    expect(first.schemaVersion).toBe(READINESS_SCHEMA_VERSION);
    expect(first.gates.map((gate) => gate.id)).toEqual([...READINESS_GATE_IDS]);
    expect(first.thresholds).toEqual(DEFAULT_READINESS_THRESHOLDS);
  });

  it("fails closed on missing, unsafe, and mismatched evidence", () => {
    const input = passingInput();
    const result = evaluateRealMoneyReadiness({
      ...input,
      prospectiveEvidence: {
        ...input.prospectiveEvidence,
        allTradesHaveLiveFillEvidence: false,
      },
      executionParity: {
        ...input.executionParity,
        passed: false,
      },
      provenance: {
        ...input.provenance,
        valid: false,
        expectedFingerprint: "b".repeat(64),
      },
      stress: {
        ...input.stress,
        lowerBoundPct: "-0.1",
      },
    });

    expect(result.status).toBe("FAIL");
    expect(result.failedGateIds).toEqual(
      expect.arrayContaining([
        "prospective-evidence",
        "execution-parity",
        "provenance",
        "stress",
      ]),
    );
  });

  it("rejects a threshold override that weakens a default", () => {
    const result = evaluateRealMoneyReadiness({
      ...passingInput(),
      thresholdOverrides: { minimumDemoTrades: 1 },
    });

    expect(result.status).toBe("ERROR");
    expect(result.exitCode).toBe(2);
    expect(result.errors).toContain(
      "threshold override weakens minimumDemoTrades",
    );
  });

  it("changes the fingerprint when an audited manifest field changes", () => {
    const manifest: StrategyManifest = passingInput().manifest;
    const original = fingerprintStrategyManifest(manifest);
    const changed = fingerprintStrategyManifest({
      ...manifest,
      feePct: "0.07",
    });

    expect(original).toMatch(/^[0-9a-f]{64}$/);
    expect(changed).toMatch(/^[0-9a-f]{64}$/);
    expect(changed).not.toBe(original);
  });

  it("is stable for reordered manifest construction and canonical decimals", () => {
    const manifest: StrategyManifest = passingInput().manifest;
    const reordered: StrategyManifest = {
      protocolVersion: manifest.protocolVersion,
      engineVersion: manifest.engineVersion,
      orderType: manifest.orderType,
      triggerTiming: manifest.triggerTiming,
      adxGate: "30.0",
      trendFilterPeriod: "96.00",
      slippageBps: "+2",
      feePct: "0.0600",
      positionFraction: "0.500",
      gridPauseAfterLossBars: "12.0",
      gridMaxGrids: "1.500",
      gridStepPct: "1.00",
      timeframe: manifest.timeframe,
      symbol: manifest.symbol,
      exchange: manifest.exchange,
      schema: manifest.schema,
    };

    expect(fingerprintStrategyManifest(reordered)).toBe(
      fingerprintStrategyManifest(manifest),
    );
  });
});
