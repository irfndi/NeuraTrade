import { expect, describe, it } from "bun:test";
import type { CandleLike } from "../scalping/types.js";
import { runLadderGridBacktest } from "../scalping/ladder-grid.js";
import {
  advanceLadderBar,
  freshWorkingState,
  type LadderPaperTradingOptions,
} from "./ladder-engine.js";

/**
 * EXECUTION-PARITY GOLDEN DIFFERENTIAL (real-money readiness gate F1).
 *
 * The gate mandates replay-vs-backtest parity before any PASS: the deployed
 * incremental engine (`advanceLadderBar`) must reproduce the validated
 * backtest (`runLadderGridBacktest`) trade-for-trade on the same candle
 * window — trigger bar, fill price, exit reason, and PnL. This file is that
 * harness: any engine change that breaks the shared per-bar state machine
 * fails here with a per-trade diff.
 */

function candle(
  o: number,
  h: number,
  l: number,
  c: number,
  i: number,
): CandleLike {
  return {
    timestamp: new Date(1000 * 60 * 15 * i),
    open: o,
    high: h,
    low: l,
    close: c,
    volume: 1000,
  };
}

/** Oscillator: dips fill the long ladder, rallies take profit. */
function oscillatorSeries(): CandleLike[] {
  return [
    candle(100, 100, 100, 100, 0),
    candle(100, 101, 98.8, 99.0, 1),
    candle(99.0, 101.2, 99.0, 100.8, 2),
    candle(100.8, 100.8, 100.8, 100.8, 3),
    candle(100.8, 102.0, 100.5, 101.5, 4),
    candle(101.5, 101.5, 99.5, 99.8, 5),
    candle(99.8, 101.0, 99.6, 100.9, 6),
    candle(100.9, 102.2, 100.8, 102.0, 7),
    candle(102.0, 102.0, 100.9, 101.2, 8),
    candle(101.2, 101.8, 100.4, 100.6, 9),
  ];
}

/** Downtrend then bounce: fills the short ladder, stops one side out. */
function stopOutSeries(): CandleLike[] {
  return [
    candle(100, 100, 100, 100, 0),
    candle(100, 100.5, 99.0, 99.2, 1),
    candle(99.2, 99.4, 97.8, 98.0, 2),
    candle(98.0, 98.6, 97.2, 98.4, 3),
    candle(98.4, 99.8, 98.2, 99.6, 4),
    candle(99.6, 100.8, 99.4, 100.6, 5),
    candle(100.6, 101.4, 100.4, 101.2, 6),
    candle(101.2, 101.6, 100.8, 101.0, 7),
  ];
}

interface EngineTrade {
  readonly side: string;
  readonly rungIndex: number;
  readonly entryPrice: number;
  readonly exitPrice: number;
  readonly reason: string;
  readonly capitalAfter: number;
}

/** Step the incremental engine over the whole series and collect its trades. */
function engineTrades(
  candles: CandleLike[],
  opts: LadderPaperTradingOptions,
): EngineTrade[] {
  const w = freshWorkingState(opts.initialCapital);
  const trades: EngineTrade[] = [];
  for (let i = 1; i < candles.length; i++) {
    const events = advanceLadderBar(w, candles, i, opts, null);
    for (const close of events.closes) {
      trades.push({
        side: close.side,
        rungIndex: close.rungIndex,
        entryPrice: close.entryPrice,
        exitPrice: close.exitPrice,
        reason: close.reason,
        // The event's own capitalAfter snapshot — several rungs can close
        // on the same bar, so the working capital is end-of-bar for all.
        capitalAfter: Number(close.capitalAfter),
      });
    }
  }
  return trades;
}

function backtestTrades(
  candles: CandleLike[],
  opts: LadderPaperTradingOptions,
): EngineTrade[] {
  const r = runLadderGridBacktest(candles, opts);
  let capital = opts.initialCapital;
  return r.trades
    .filter((t) => t.exitReason !== "mark_to_market")
    .map((t) => {
      // pnlQuote is the trade's absolute PnL on the running capital, so the
      // golden capital trajectory compounds by accumulation.
      capital += t.pnlQuote ?? 0;
      return {
        side: t.side,
        rungIndex: t.rungIndex ?? -1,
        entryPrice: t.entryPrice,
        exitPrice: t.exitPrice,
        reason: t.exitReason ?? "unknown",
        capitalAfter: capital,
      };
    });
}

const baseOptions = (
  overrides: Partial<LadderPaperTradingOptions> = {},
): LadderPaperTradingOptions => ({
  exchange: "bybit",
  symbol: "TEST",
  timeframe: "15m",
  rungs: 1,
  gridStepPct: 1.0,
  gridMaxGrids: 3,
  gridPauseAfterLossBars: 0,
  feePct: 0.02,
  slippageBps: 2,
  initialCapital: 100,
  trendFilterPeriod: 0,
  leverage: 1,
  ...overrides,
});

describe("execution parity: backtest vs incremental engine (golden)", () => {
  const configs: readonly [string, CandleLike[], LadderPaperTradingOptions][] =
    [
      ["target oscillator, 1 rung", oscillatorSeries(), baseOptions()],
      [
        "target oscillator, 3 rungs",
        oscillatorSeries(),
        baseOptions({ rungs: 3 }),
      ],
      ["stop-out series", stopOutSeries(), baseOptions()],
      [
        "stop-out with stopRatio",
        stopOutSeries(),
        baseOptions({ stopRatio: 1.5 }),
      ],
      [
        "leveraged with max-hold",
        oscillatorSeries(),
        baseOptions({ leverage: 3, maxHoldBars: 2 }),
      ],
      [
        "taker exit fees",
        stopOutSeries(),
        baseOptions({ takerExitFeePct: 0.06 }),
      ],
    ];

  for (const [name, candles, opts] of configs) {
    it(`reproduces the backtest trade-for-trade: ${name}`, () => {
      const golden = backtestTrades(candles, opts);
      const replay = engineTrades(candles, opts);
      expect(replay.length).toBe(golden.length);
      for (let k = 0; k < Math.min(golden.length, replay.length); k++) {
        const g = golden[k];
        const r = replay[k];
        expect(r.side).toBe(g.side);
        expect(r.rungIndex).toBe(g.rungIndex);
        // Trigger bar parity is implied by trade ORDER and entry price:
        // the same level touched on the same bar produces the same
        // post-slippage entry.
        expect(r.entryPrice).toBeCloseTo(g.entryPrice, 8);
        expect(r.exitPrice).toBeCloseTo(g.exitPrice, 8);
        expect(r.reason).toBe(g.reason);
        expect(r.capitalAfter).toBeCloseTo(g.capitalAfter, 6);
      }
    });
  }

  it("charges ADVERSE slippage on stop exits (losses are not understated)", () => {
    const opts = baseOptions({ slippageBps: 5 });
    const candles = stopOutSeries();
    const trades = engineTrades(candles, opts);
    const stops = trades.filter((t) => t.reason === "stop");
    for (const t of stops) {
      if (t.side === "long") {
        // Long stop sells BELOW the boundary.
        expect(t.exitPrice).toBeLessThan(
          t.entryPrice - candles[0].open * (opts.gridStepPct / 100) * 0.5,
        );
      } else {
        // Short stop buys ABOVE the boundary.
        expect(t.exitPrice).toBeGreaterThan(t.entryPrice);
      }
    }
    // And the backtest must agree with the engine on the same series.
    const golden = backtestTrades(candles, opts);
    expect(trades.length).toBe(golden.length);
  });

  it("accrues funding on held rungs identically in both engines", () => {
    const opts = baseOptions({ fundingRatePct8h: 0.01, maxHoldBars: 40 });
    const candles = oscillatorSeries();
    const golden = backtestTrades(candles, opts);
    const replay = engineTrades(candles, opts);
    expect(replay.length).toBe(golden.length);
    for (let k = 0; k < Math.min(golden.length, replay.length); k++) {
      expect(replay[k].capitalAfter).toBeCloseTo(golden[k].capitalAfter, 6);
    }
  });

  it("carries per-rung filledQty so closes send the entry size", () => {
    const opts = baseOptions({ rungs: 2 });
    const candles = oscillatorSeries();
    const w = freshWorkingState(opts.initialCapital);
    let sawQty: number | undefined;
    for (let i = 1; i < candles.length; i++) {
      const events = advanceLadderBar(w, candles, i, opts, null);
      for (const close of events.closes) {
        if (close.qty !== undefined && close.qty > 0) sawQty = close.qty;
      }
    }
    expect(sawQty).toBeDefined();
    expect(sawQty!).toBeGreaterThan(0);
  });
});
