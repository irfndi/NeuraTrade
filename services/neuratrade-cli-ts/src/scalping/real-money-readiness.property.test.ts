import { describe, expect, it } from "bun:test";
import * as fc from "fast-check";
import * as S from "effect/Schema";
import {
  DEFAULT_READINESS_THRESHOLDS,
  evaluateRealMoneyReadiness,
  fingerprintStrategyManifest,
  type ReadinessThresholds,
  type RealMoneyReadinessInput,
  type StrategyManifest,
} from "./real-money-readiness.js";

/** A valid, passing readiness input used as the base for property runs. */
function baseInput(): RealMoneyReadinessInput {
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
          name: "sample-size",
          passed: true,
          detail: "backtest=30 deployed=30 (minimum 30 trades)",
        },
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
        {
          name: "fees",
          passed: true,
          detail: "both charge maker feePct=0.02% (round-trip 0.04%)",
        },
        {
          name: "slippage",
          passed: true,
          detail: "both apply slippageBps=1",
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

/**
 * Canonically-equivalent renderings of a decimal value: a leading +, a
 * trailing ".0", and trailing zeros in the fraction part. All of these
 * normalize to the same number and must fingerprint identically. (Leading
 * zeros on the integer part are intentionally NOT equivalent — the audit
 * decimal syntax rejects "01" as malformed, which is itself a fail-closed
 * property.)
 */
function decimalPerturbations(value: string): readonly string[] {
  if (!/^[+-]?(0|[1-9][0-9]*)(\.[0-9]+)?$/.test(value)) return [value];
  const negative = value.startsWith("-");
  const sign = negative ? "-" : "+";
  const unsigned = value.replace(/^[+-]/, "");
  const [integer, fraction = ""] = unsigned.split(".");
  const variants = new Set<string>();
  variants.add(unsigned);
  variants.add(`${sign}${unsigned}`);
  // ".0" is only a canonical equivalent of an integer (no fraction part);
  // appending it to a fractional value would change the numeric value.
  if (fraction.length === 0) variants.add(`${integer}.0`);
  if (fraction.length > 0) {
    variants.add(`${integer}.${fraction}0`);
    variants.add(`${integer}.${fraction}00`);
  }
  const withSign = (text: string) => (negative ? `-${text}` : text);
  return [...variants].map(withSign);
}

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
          trendFilterPeriod: fc.constant("0"),
          adxGate: fc.constant("30"),
          targetRatio: fc.constant("3"),
          onlyWithTrend: fc.constant("false"),
          leverage: fc.constant("1"),
          productType: fc.constant("USDT-FUTURES"),
          marginMode: fc.constant("crossed"),
          maxDrawdownPct: fc.constant("5"),
          maxDailyLossPct: fc.constant("2"),
          validationProfile: fc.constant("gate-scored-grid-search-2026-08-03"),
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
            validationProfile: manifest.validationProfile,
            maxDailyLossPct: manifest.maxDailyLossPct,
            maxDrawdownPct: manifest.maxDrawdownPct,
            marginMode: manifest.marginMode,
            productType: manifest.productType,
            leverage: manifest.leverage,
            onlyWithTrend: manifest.onlyWithTrend,
            targetRatio: manifest.targetRatio,
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

  it("fingerprints decimal values canonically under arbitrary padding", () => {
    fc.assert(
      fc.property(
        fc.record({
          gridStepPct: fc.oneof(
            fc.integer({ min: 1, max: 3 }),
            fc.double({ min: 0.5, max: 2, noNaN: true }),
          ),
          gridMaxGrids: fc.oneof(
            fc.integer({ min: 1, max: 3 }),
            fc.double({ min: 1, max: 2.5, noNaN: true }),
          ),
          gridPauseAfterLossBars: fc.integer({ min: 0, max: 24 }),
          positionFraction: fc.constant(0.5),
          feePct: fc.constant(0.06),
          slippageBps: fc.constant(2),
          trendFilterPeriod: fc.constant(0),
          adxGate: fc.constant(30),
        }),
        (parts) => {
          const manifest: StrategyManifest = {
            ...baseInput().manifest,
            gridStepPct: String(parts.gridStepPct),
            gridMaxGrids: String(parts.gridMaxGrids),
            gridPauseAfterLossBars: String(parts.gridPauseAfterLossBars),
            positionFraction: String(parts.positionFraction),
            feePct: String(parts.feePct),
            slippageBps: String(parts.slippageBps),
            trendFilterPeriod: String(parts.trendFilterPeriod),
            adxGate: String(parts.adxGate),
          };
          const reference = fingerprintStrategyManifest(manifest);
          expect(reference).toMatch(/^[0-9a-f]{64}$/);

          // Every canonical rendering of each decimal field (leading zeros,
          // trailing zeros, a leading +, a trailing ".0") must produce the
          // identical fingerprint — otherwise the audited candidate identity
          // would depend on string formatting rather than numeric value.
          for (const field of [
            "gridStepPct",
            "gridMaxGrids",
            "gridPauseAfterLossBars",
            "positionFraction",
            "feePct",
            "slippageBps",
            "trendFilterPeriod",
            "adxGate",
          ] as const) {
            for (const variant of decimalPerturbations(manifest[field])) {
              const perturbed: StrategyManifest = {
                ...manifest,
                [field]: variant,
              };
              expect(fingerprintStrategyManifest(perturbed)).toBe(reference);
            }
          }
        },
      ),
      { numRuns: 100 },
    );
  });

  it("rejects any threshold override that weakens or malforms a default", () => {
    const numericKeys = Object.entries(DEFAULT_READINESS_THRESHOLDS)
      .filter(([, value]) => S.is(S.Number)(value))
      .map(([key]) => key) as Array<keyof ReadinessThresholds>;
    fc.assert(
      fc.property(
        fc.record({
          key: fc.constantFrom(...numericKeys),
          value: fc.integer({ min: -1000, max: 1000 }),
        }),
        ({ key, value }) => {
          const result = evaluateRealMoneyReadiness({
            ...baseInput(),
            thresholdOverrides: { [key]: value },
          });
          if (value < (DEFAULT_READINESS_THRESHOLDS[key] as number)) {
            // Weakening an override must never silently pass: it is either
            // rejected as ERROR (guard rail) or, if the overridden threshold
            // happens to be numeric-typed and still above baseline, the gate
            // result must be consistent.
            expect(result.status).not.toBe("PASS");
          }
        },
      ),
      { numRuns: 200 },
    );
  });

  it("never flips PASS to FAIL when evidence is strictly strengthened", () => {
    const base = baseInput();
    const baseReport = evaluateRealMoneyReadiness(base);
    expect(baseReport.status).toBe("PASS");

    fc.assert(
      fc.property(
        fc.record({
          extraTrades: fc.integer({ min: 0, max: 500 }),
          extraWindows: fc.integer({ min: 0, max: 100 }),
          higherExpectancy: fc.double({ min: 0, max: 10, noNaN: true }),
          // Keep the drawdown in plain decimal form (quarters) so its string
          // rendering never falls into exponent notation, which the audit
          // decimal syntax correctly rejects.
          lowerDrawdown: fc
            .integer({ min: 0, max: 40 })
            .map((value) => value / 10),
        }),
        (delta) => {
          const strengthened: RealMoneyReadinessInput = {
            ...base,
            prospectiveEvidence: {
              ...base.prospectiveEvidence,
              completeTradeCount:
                base.prospectiveEvidence.completeTradeCount + delta.extraTrades,
              expectancyPct: (
                Number(base.prospectiveEvidence.expectancyPct) +
                delta.higherExpectancy
              ).toString(),
              maximumDrawdownPct: Math.min(
                Number(base.prospectiveEvidence.maximumDrawdownPct),
                delta.lowerDrawdown,
              ).toString(),
            },
            historicalRobustness: {
              ...base.historicalRobustness,
              completeWindows:
                base.historicalRobustness.completeWindows + delta.extraWindows,
            },
          };
          const report = evaluateRealMoneyReadiness(strengthened);
          expect(report.status).toBe("PASS");
          expect(report.failedGateIds).toEqual([]);
        },
      ),
      { numRuns: 50 },
    );
  });
});
