import { describe, expect, it } from "bun:test";
import { defaultComposerConfig } from "./composer.js";
import {
  runMultiSymbolPortfolioBacktest,
  type MultiSymbolPortfolioInput,
} from "./portfolio-backtest.js";
import type { CandleLike } from "./types.js";

function makeCandles(
  count: number,
  baseClose = 100,
  trend: "up" | "down" | "flat" = "flat",
): CandleLike[] {
  const candles: CandleLike[] = [];
  let close = baseClose;
  for (let i = 0; i < count; i++) {
    const open = close;
    if (trend === "up") close *= 1.005;
    else if (trend === "down") close *= 0.995;
    const high = Math.max(open, close) * 1.002;
    const low = Math.min(open, close) * 0.998;
    candles.push({
      open,
      high,
      low,
      close,
      volume: 10,
      timestamp: new Date(Date.UTC(2024, 0, 1, 0, 0, i)),
    });
  }
  return candles;
}

function makePortfolioInputs(
  pairs: { symbol: string; candles: CandleLike[] }[],
): MultiSymbolPortfolioInput[] {
  return pairs.map((p) => ({ symbol: p.symbol, candles: p.candles }));
}

const baseOptions = {
  exchange: "binance",
  timeframe: "1h",
  composerConfig: defaultComposerConfig,
  initialCapital: 10_000,
  positionSizePct: 100,
  stopLossPct: 1,
  takeProfitPct: 1.2,
  feePct: 0.1,
  minConfidence: 0.5,
};

describe("runMultiSymbolPortfolioBacktest", () => {
  it("enforces a global max-open-positions limit across symbols", () => {
    const a = makeCandles(120, 100, "up");
    const b = a.map((c, i) => ({
      ...c,
      timestamp: new Date(c.timestamp.getTime() + i * 1000),
    }));

    const unrestricted = runMultiSymbolPortfolioBacktest({
      ...baseOptions,
      maxOpenPositions: 10,
      symbols: makePortfolioInputs([
        { symbol: "A/USDT", candles: a },
        { symbol: "B/USDT", candles: b },
      ]),
    });

    const restricted = runMultiSymbolPortfolioBacktest({
      ...baseOptions,
      maxOpenPositions: 1,
      symbols: makePortfolioInputs([
        { symbol: "A/USDT", candles: a },
        { symbol: "B/USDT", candles: b },
      ]),
    });

    expect(unrestricted.totalTrades).toBeGreaterThan(0);
    expect(restricted.totalTrades).toBeGreaterThan(0);
    expect(restricted.totalTrades).toBeLessThan(unrestricted.totalTrades);
  });

  it("blocks new entries that are highly correlated with open positions", () => {
    const a = makeCandles(120, 100, "up");
    // Symbol B is perfectly correlated with A and shares the same timestamps.
    const b = a.map((c) => ({ ...c }));

    const withoutFilter = runMultiSymbolPortfolioBacktest({
      ...baseOptions,
      maxOpenPositions: 10,
      stopLossPct: 10,
      takeProfitPct: 10,
      correlationFilter: false,
      symbols: makePortfolioInputs([
        { symbol: "A/USDT", candles: a },
        { symbol: "B/USDT", candles: b },
      ]),
    });

    const withFilter = runMultiSymbolPortfolioBacktest({
      ...baseOptions,
      maxOpenPositions: 10,
      stopLossPct: 10,
      takeProfitPct: 10,
      correlationFilter: true,
      correlationThreshold: 0.8,
      correlationLookback: 20,
      symbols: makePortfolioInputs([
        { symbol: "A/USDT", candles: a },
        { symbol: "B/USDT", candles: b },
      ]),
    });

    expect(withoutFilter.totalTrades).toBeGreaterThan(0);
    expect(withFilter.totalTrades).toBeGreaterThan(0);
    expect(withFilter.totalTrades).toBeLessThan(withoutFilter.totalTrades);
  });

  it("reports mark-to-market aggregate drawdown across concurrent positions", () => {
    const a = [
      ...makeCandles(30, 100, "up"),
      ...makeCandles(90, 100 * 1.005 ** 30, "down"),
    ];
    const b = a.map((c, i) => ({
      ...c,
      timestamp: new Date(c.timestamp.getTime() + i * 1000),
    }));

    const result = runMultiSymbolPortfolioBacktest({
      ...baseOptions,
      maxOpenPositions: 10,
      symbols: makePortfolioInputs([
        { symbol: "A/USDT", candles: a },
        { symbol: "B/USDT", candles: b },
      ]),
    });

    expect(result.maxDrawdownPct).toBeGreaterThan(0);
  });

  it("returns an empty result when no symbols are provided", () => {
    const result = runMultiSymbolPortfolioBacktest({
      ...baseOptions,
      maxOpenPositions: 1,
      symbols: [],
    });

    expect(result.symbolCount).toBe(0);
    expect(result.totalTrades).toBe(0);
  });
});
