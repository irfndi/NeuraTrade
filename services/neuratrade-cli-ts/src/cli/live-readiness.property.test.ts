import { describe, expect, it } from "bun:test";
import * as fc from "fast-check";
import {
  MAINNET_CONFIRM_TOKEN,
  validateLiveGridConfiguration,
  validateLiveGridWatchlist,
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

  it("names the remedy: unvalidated symbol vs a one-line config mismatch", () => {
    // The candidate fixture matches the BTC validated candidate exactly, so it
    // is the reference for a clean config.
    expect(validateLiveGridConfiguration(candidate)).toBeUndefined();

    // A symbol outside the registry is a research task, not a knob fix.
    const unregistered = { ...candidate, symbol: "LINK/USDT:USDT" };
    const missing = validateLiveGridConfiguration(unregistered);
    expect(missing).toContain("no validated readiness candidate for LINK");
    expect(missing).toContain("BTC/USDT:USDT");

    // A registered symbol with a drifted knob names the field and both values.
    const drifted = { ...candidate, targetRatio: 1.95 };
    const mismatch = validateLiveGridConfiguration(drifted);
    expect(mismatch).toContain("validated readiness cohort candidate");
    expect(mismatch).toContain("targetRatio 1.95 != 2");

    // Sandbox still bypasses the cohort gate entirely.
    expect(validateLiveGridConfiguration(unregistered, true)).toBeUndefined();
  });

  it("admits mainnet only with BOTH the flag and the exact token", () => {
    // Two independent acts, fail-closed on either one missing. A typo, an
    // empty string, or the flag alone must never open a real-money path.
    expect(validateLiveSandboxMode(true, false)).toBeDefined();
    expect(validateLiveSandboxMode(true, false, true)).toBeDefined();
    expect(validateLiveSandboxMode(true, false, true, "")).toBeDefined();
    expect(
      validateLiveSandboxMode(true, false, true, "i-confirm-mainnet"),
    ).toBeDefined();
    expect(
      validateLiveSandboxMode(true, false, true, MAINNET_CONFIRM_TOKEN),
    ).toBeUndefined();
    // Sandbox live is governed by the old rule alone, not by this gate.
    expect(
      validateLiveSandboxMode(true, true, false, undefined),
    ).toBeUndefined();
    expect(
      validateLiveSandboxMode(false, false, false, undefined),
    ).toBeUndefined();
  });

  it("applies the same two-key gate to mainnet grid watchlists", () => {
    const entries = [{ symbol: "LINK/USDT:USDT" }];
    expect(
      validateLiveGridWatchlist(true, "grid", entries, false),
    ).toBeDefined();
    expect(
      validateLiveGridWatchlist(true, "grid", entries, false, true),
    ).toBeDefined();
    expect(
      validateLiveGridWatchlist(
        true,
        "grid",
        entries,
        false,
        true,
        MAINNET_CONFIRM_TOKEN,
      ),
    ).toBeUndefined();
    // Testnet keeps the previous behavior: a watchlist is allowed outright.
    expect(
      validateLiveGridWatchlist(true, "grid", entries, true),
    ).toBeUndefined();
    // No watchlist at all is never a watchlist violation.
    expect(validateLiveGridWatchlist(true, "grid", [], false)).toBeUndefined();
  });
});
