import { describe, expect, it } from "bun:test";
import {
  DEFAULT_READINESS_THRESHOLDS,
  DEFAULT_STRATEGY_MANIFEST,
  READINESS_GATE_IDS,
  READINESS_SCHEMA_VERSION,
  evaluateRealMoneyReadiness,
  fingerprintStrategyManifest,
  serializeRealMoneyReadiness,
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
        {
          name: "trigger-bar",
          passed: true,
          detail: "backtest=2 deployed=2",
        },
        {
          name: "order-type",
          passed: true,
          detail: "both use limit entry at grid level",
        },
        {
          name: "fill-price",
          passed: true,
          detail: "2/2 entries within 0.5%",
        },
        { name: "fees", passed: true, detail: "both charge 0.12% round-trip" },
        {
          name: "slippage",
          passed: true,
          detail: "both apply slippageBps=2",
        },
        {
          name: "quantity",
          passed: true,
          detail: "both size at 50% of capital",
        },
        {
          name: "exit-reason",
          passed: true,
          detail: "2/2 exit reasons equal",
        },
        { name: "pnl", passed: true, detail: "2/2 within 0.5pp" },
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
      gridMaxGrids: "1",
      gridPauseAfterLossBars: "12",
      positionFraction: "0.5",
      feePct: "0.06",
      slippageBps: "2",
      trendFilterPeriod: "0",
      adxGate: "30",
      targetRatio: "3",
      onlyWithTrend: "false",
      leverage: "1",
      productType: "USDT-FUTURES",
      marginMode: "crossed",
      maxDrawdownPct: "5",
      maxDailyLossPct: "2",
      validationProfile: "gate-scored-grid-search-2026-08-03",
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

  it("uses maker (limit-at-grid-level) execution assumptions for the validated BTC candidate", () => {
    expect(DEFAULT_STRATEGY_MANIFEST).toMatchObject({
      feePct: "0.02",
      slippageBps: "1",
      trendFilterPeriod: "0",
      orderType: "limit-at-grid-level",
      engineVersion: "grid-engine/v2",
    });
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

  it("rejects a non-finite threshold override", () => {
    const result = evaluateRealMoneyReadiness({
      ...passingInput(),
      thresholdOverrides: { maximumFreshnessHours: Number.NaN },
    });

    expect(result.status).toBe("ERROR");
    expect(result.exitCode).toBe(2);
    expect(result.errors).toContain(
      "threshold override is malformed: maximumFreshnessHours",
    );
  });

  it("applies a strengthened profitable-window threshold", () => {
    const result = evaluateRealMoneyReadiness({
      ...passingInput(),
      thresholdOverrides: { minimumProfitableWindowPct: 75 },
    });

    expect(result.status).toBe("FAIL");
    expect(result.failedGateIds).toContain("historical-robustness");
    expect(
      result.gates.find((gate) => gate.id === "historical-robustness")?.reasons,
    ).toContain("profitable historical windows do not exceed the minimum");
  });

  it("fails execution parity when any required check is missing", () => {
    const checks = [
      "trigger-bar",
      "order-type",
      "fill-price",
      "fees",
      "slippage",
      "quantity",
      "exit-reason",
      "pnl",
    ];

    for (const missing of checks) {
      const result = evaluateRealMoneyReadiness({
        ...passingInput(),
        executionParity: {
          ...passingInput().executionParity,
          checks: checks
            .filter((check) => check !== missing)
            .map((name) => ({
              name,
              passed: true,
              detail: `${name}: OK`,
            })),
        },
      });

      expect(result.status).toBe("FAIL");
      expect(result.failedGateIds).toContain("execution-parity");
      expect(
        result.gates.find((gate) => gate.id === "execution-parity")?.reasons,
      ).toContain(`execution parity check is missing: ${missing}`);
    }
  });

  it("fails execution parity when any required check is not passed", () => {
    const result = evaluateRealMoneyReadiness({
      ...passingInput(),
      executionParity: {
        ...passingInput().executionParity,
        checks: passingInput().executionParity.checks.map((check) =>
          check.name === "fill-price"
            ? { ...check, passed: false, detail: "0/2 entries within 0.5%" }
            : check,
        ),
      },
    });

    expect(result.status).toBe("FAIL");
    expect(result.failedGateIds).toContain("execution-parity");
    expect(
      result.gates.find((gate) => gate.id === "execution-parity")?.reasons,
    ).toContain("execution parity check failed: fill-price");
  });

  it("fails an inverted confidence interval", () => {
    const result = evaluateRealMoneyReadiness({
      ...passingInput(),
      confidence: {
        ...passingInput().confidence,
        lowerBoundPct: "0.2",
        upperBoundPct: "0.1",
      },
    });

    expect(result.status).toBe("FAIL");
    expect(result.failedGateIds).toContain("confidence");
    expect(
      result.gates.find((gate) => gate.id === "confidence")?.reasons,
    ).toContain("confidence interval is inverted");
  });

  it("fails non-finite scalar evidence instead of treating it as passing", () => {
    const cases = [
      {
        input: {
          ...passingInput(),
          prospectiveEvidence: {
            ...passingInput().prospectiveEvidence,
            completeTradeCount: Number.NaN,
          },
        },
        gate: "prospective-evidence" as const,
      },
      {
        input: {
          ...passingInput(),
          historicalRobustness: {
            ...passingInput().historicalRobustness,
            completeWindows: Number.POSITIVE_INFINITY,
          },
        },
        gate: "historical-robustness" as const,
      },
      {
        input: {
          ...passingInput(),
          dataQuality: {
            ...passingInput().dataQuality,
            candleCount: Number.NaN,
          },
        },
        gate: "data-quality" as const,
      },
    ];

    for (const testCase of cases) {
      const result = evaluateRealMoneyReadiness(testCase.input);
      expect(result.status).toBe("FAIL");
      expect(result.failedGateIds).toContain(testCase.gate);
    }
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

  it("changes the fingerprint when leverage, margin mode, or risk limits change", () => {
    const manifest: StrategyManifest = passingInput().manifest;
    const original = fingerprintStrategyManifest(manifest);
    for (const variant of [
      { leverage: "2" },
      { marginMode: "isolated" },
      { productType: "COIN-FUTURES" },
      { maxDrawdownPct: "10" },
      { maxDailyLossPct: "5" },
      { validationProfile: "other-profile" },
    ]) {
      expect(fingerprintStrategyManifest({ ...manifest, ...variant })).not.toBe(
        original,
      );
    }
  });

  it("is stable for reordered manifest construction and canonical decimals", () => {
    const manifest: StrategyManifest = passingInput().manifest;
    const reordered: StrategyManifest = {
      protocolVersion: manifest.protocolVersion,
      engineVersion: manifest.engineVersion,
      orderType: manifest.orderType,
      triggerTiming: manifest.triggerTiming,
      validationProfile: manifest.validationProfile,
      maxDailyLossPct: "2.00",
      maxDrawdownPct: "5.0",
      marginMode: manifest.marginMode,
      productType: manifest.productType,
      leverage: "1.00",
      onlyWithTrend: manifest.onlyWithTrend,
      targetRatio: "3.00",
      adxGate: "30.0",
      trendFilterPeriod: "0.00",
      slippageBps: "+2",
      feePct: "0.0600",
      positionFraction: "0.500",
      gridPauseAfterLossBars: "12.0",
      gridMaxGrids: "1.00",
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

  it("rejects a string threshold override that weakens a default", () => {
    const result = evaluateRealMoneyReadiness({
      ...passingInput(),
      thresholdOverrides: { maximumHistoricalDrawdownPct: "20" },
    });

    expect(result.status).toBe("ERROR");
    expect(result.exitCode).toBe(2);
    expect(result.errors).toContain(
      "threshold override weakens maximumHistoricalDrawdownPct",
    );
  });

  it("rejects a malformed string threshold override", () => {
    const result = evaluateRealMoneyReadiness({
      ...passingInput(),
      thresholdOverrides: { minimumDemoExpectancyPct: "not-a-number" },
    });

    expect(result.status).toBe("ERROR");
    expect(result.exitCode).toBe(2);
    expect(result.errors).toContain(
      "threshold override is malformed: minimumDemoExpectancyPct",
    );
  });

  it("accepts a strengthened threshold override", () => {
    const result = evaluateRealMoneyReadiness({
      ...passingInput(),
      thresholdOverrides: { minimumHistoricalWindows: 11 },
    });

    expect(result.status).toBe("PASS");
    expect(result.thresholds.minimumHistoricalWindows).toBe(11);
  });

  it("reports ERROR when the manifest contains a non-finite value", () => {
    const result = evaluateRealMoneyReadiness({
      ...passingInput(),
      manifest: {
        ...passingInput().manifest,
        feePct: "NaN",
      },
    });

    expect(result.status).toBe("ERROR");
    expect(result.exitCode).toBe(2);
    expect(result.errors[0]).toContain("manifest is invalid");
  });

  it("reports ERROR when the manifest contains a non-decimal value", () => {
    const result = evaluateRealMoneyReadiness({
      ...passingInput(),
      manifest: {
        ...passingInput().manifest,
        feePct: "0.06e3",
      },
    });

    expect(result.status).toBe("ERROR");
    expect(result.exitCode).toBe(2);
    expect(result.errors[0]).toContain("manifest is invalid");
  });

  it("serializes a report to JSON and round-trips it", () => {
    const report = evaluateRealMoneyReadiness(passingInput());

    const serialized = serializeRealMoneyReadiness(report);
    const parsed = JSON.parse(serialized) as {
      readonly status: string;
      readonly exitCode: number;
    };

    expect(serialized).toBe(JSON.stringify(report));
    expect(parsed.status).toBe("PASS");
    expect(parsed.exitCode).toBe(0);
  });

  it("fails the freshness gate when the latest candle is stale", () => {
    const result = evaluateRealMoneyReadiness({
      ...passingInput(),
      dataQuality: {
        ...passingInput().dataQuality,
        latestCandle: "2026-07-01T00:00:00.000Z",
      },
    });

    expect(result.status).toBe("FAIL");
    expect(result.failedGateIds).toContain("freshness");
  });

  it("fails the freshness gate when the latest candle is in the future", () => {
    const result = evaluateRealMoneyReadiness({
      ...passingInput(),
      dataQuality: {
        ...passingInput().dataQuality,
        latestCandle: "2026-08-03T00:00:00.000Z",
      },
    });

    expect(result.status).toBe("FAIL");
    expect(result.failedGateIds).toContain("freshness");
  });

  it("fails when the demo trade count exceeds the trades with live fills", () => {
    const result = evaluateRealMoneyReadiness({
      ...passingInput(),
      prospectiveEvidence: {
        ...passingInput().prospectiveEvidence,
        allTradesHaveLiveFillEvidence: false,
      },
    });

    expect(result.status).toBe("FAIL");
    expect(result.failedGateIds).toContain("prospective-evidence");
    expect(
      result.gates.find((gate) => gate.id === "prospective-evidence")?.reasons,
    ).toContain("one or more demo trades lack complete live fill evidence");
  });

  it("fails the provenance gate on a cohort query truncation", () => {
    const result = evaluateRealMoneyReadiness({
      ...passingInput(),
      provenance: {
        ...passingInput().provenance,
        queriedRows: 40,
        expectedRows: 60,
      },
    });

    expect(result.status).toBe("FAIL");
    expect(result.failedGateIds).toContain("provenance");
    expect(
      result.gates.find((gate) => gate.id === "provenance")?.reasons,
    ).toContain("cohort query was truncated");
  });

  it("fails the provenance gate when the candidate lock is after the cutoff", () => {
    const result = evaluateRealMoneyReadiness({
      ...passingInput(),
      provenance: {
        ...passingInput().provenance,
        candidateLock: "2026-08-01T00:00:00.000Z",
        datasetCutoff: "2026-07-31T23:45:00.000Z",
      },
    });

    expect(result.status).toBe("FAIL");
    expect(result.failedGateIds).toContain("provenance");
    expect(
      result.gates.find((gate) => gate.id === "provenance")?.reasons,
    ).toContain("candidate lock is after dataset cutoff");
  });

  it("fails the provenance gate when a close is after the evaluation time", () => {
    const result = evaluateRealMoneyReadiness({
      ...passingInput(),
      provenance: {
        ...passingInput().provenance,
        latestClose: "2026-08-03T00:00:00.000Z",
      },
    });

    expect(result.status).toBe("FAIL");
    expect(result.failedGateIds).toContain("provenance");
    expect(
      result.gates.find((gate) => gate.id === "provenance")?.reasons,
    ).toContain("close is after evaluation time");
  });

  it("fails the stress gate when an adverse seed set is incomplete", () => {
    const result = evaluateRealMoneyReadiness({
      ...passingInput(),
      stress: {
        ...passingInput().stress,
        seeds: [20260802],
      },
    });

    expect(result.status).toBe("FAIL");
    expect(result.failedGateIds).toContain("stress");
    expect(
      result.gates.find((gate) => gate.id === "stress")?.reasons,
    ).toContain("adverse stress seed set is incomplete");
  });
});
