import { describe, expect, it } from "bun:test";
import type { Candle } from "../src/market-data/types.ts";
import { knobs } from "./knobs.ts";
import { evaluateKnobsOnPanel, type AlignedPanel } from "./prepare.ts";
import {
  classifyDeflatedSharpe,
  deflatedSharpe,
  type SharpVerdict,
} from "../src/scalping/deflated-sharpe.ts";

/**
 * End-to-end contract for the selection-bias pipeline:
 *
 *   candle panel -> evaluateKnobsOnPanel -> { sharpe, skewness, kurtosis }
 *        -> deflatedSharpe(trial population) -> verdict
 *
 * These assert the *plumbing* — that the evaluator emits statistics DSR can
 * consume and that a zero-edge candidate does not come back "robust". They do
 * not assert profitability; the synthetic panel has no edge by construction.
 */

/** Deterministic pseudo-random walk (reproducible, no Math.random). */
function walk(seed: number, count: number, start: number): Candle[] {
  let price = start;
  let state = seed;
  const next = () => {
    state = (state * 1103515245 + 12345) % 2147483648;
    return state / 2147483648;
  };
  return Array.from({ length: count }, (_, i) => {
    const drift = (next() - 0.5) * price * 0.01;
    const open = price;
    const close = Math.max(0.01, price + drift);
    const high = Math.max(open, close) * (1 + next() * 0.002);
    const low = Math.min(open, close) * (1 - next() * 0.002);
    price = close;
    return {
      exchange: "synthetic",
      symbol: "BTC/USDT:USDT",
      timeframe: "5m",
      timestamp: new Date(Date.UTC(2026, 0, 1) + i * 300_000),
      open,
      high,
      low,
      close,
      volume: 100 + next() * 500,
    };
  });
}

const SYMBOLS = ["BTC/USDT:USDT", "ETH/USDT:USDT", "SOL/USDT:USDT"] as const;

function syntheticPanel(seed = 7): AlignedPanel {
  // confirm geometry needs >= 6000 5m candles per symbol (resampled to 15m) and
  // >= 3 symbols; shorter/shallower panels return "insufficient_symbols".
  const aligned = new Map<string, Candle[]>();
  let refLen = 0;
  for (const [i, symbol] of SYMBOLS.entries()) {
    const candles = walk(seed + i * 13, 7000, 100 + i * 25).map((c) => ({
      ...c,
      symbol,
    }));
    aligned.set(symbol, candles);
    refLen = Math.max(refLen, candles.length);
  }
  return {
    symbols: [...SYMBOLS],
    aligned,
    refLen,
    loadedMs: 0,
    exchange: "synthetic",
    timeframe: "5m",
    panelTimeframe: "15m",
    panelHash: `synthetic-${seed}`,
    holdoutBars: 2880,
  };
}

describe("DSR pipeline: evaluator -> statistics -> verdict", () => {
  it("emits Sharpe-family statistics the DSR can consume", () => {
    const result = evaluateKnobsOnPanel(knobs, syntheticPanel(), {
      phase: "confirm",
      maxSteps: 3,
    });

    // The statistic `score` is a location measure and must stay separate.
    expect(Number.isFinite(result.score)).toBe(true);

    // When the panel yields enough windows, the Sharpe inputs must be present
    // and be real numbers (undefined is only allowed for a degenerate sample).
    if (result.windows >= 2) {
      expect(Number.isFinite(result.sharpe as number)).toBe(true);
      expect(Number.isFinite(result.skewness as number)).toBe(true);
      // RAW kurtosis convention: exactly 3 for a normal sample, never negative.
      expect(result.kurtosis as number).toBeGreaterThan(0);
    }
  }, 120_000);

  it("scores a zero-edge synthetic candidate as not robust", () => {
    const result = evaluateKnobsOnPanel(knobs, syntheticPanel(), {
      phase: "confirm",
      maxSteps: 3,
    });
    if (result.sharpe === undefined || result.windows < 2) return; // degenerate

    // A trial population centred on zero with realistic spread: the candidate
    // must not survive deflation on a no-edge panel.
    const population = Array.from(
      { length: 2000 },
      (_, i) => (((i * 37) % 2000) - 1000) / 20000,
    );
    const dsr = deflatedSharpe({
      observedSharpe: result.sharpe,
      trialSharpes: population,
      nObs: result.windows,
      skewness: result.skewness ?? 0,
      kurtosis: result.kurtosis ?? 3,
    });
    const verdict: SharpVerdict = classifyDeflatedSharpe(dsr);
    expect(dsr).toBeGreaterThanOrEqual(0);
    expect(dsr).toBeLessThanOrEqual(1);
    expect(verdict).not.toBe("robust");
  }, 120_000);

  it("produces a finite, comparable Sharpe across different panels", () => {
    const a = evaluateKnobsOnPanel(knobs, syntheticPanel(7), {
      phase: "confirm",
      maxSteps: 2,
    });
    const b = evaluateKnobsOnPanel(knobs, syntheticPanel(99), {
      phase: "confirm",
      maxSteps: 2,
    });
    for (const r of [a, b]) {
      if (r.sharpe === undefined) continue;
      expect(Number.isFinite(r.sharpe)).toBe(true);
      // Per-period units: a 15m-window Sharpe on percentage returns at these
      // scales cannot plausibly exceed single digits.
      expect(Math.abs(r.sharpe)).toBeLessThan(10);
    }
  }, 180_000);
});
