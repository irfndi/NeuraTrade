import { expect, describe, it } from "bun:test";
import { Database } from "bun:sqlite";
import { Effect } from "effect";
import { MarketDataGateway } from "../market-data/gateway.js";
import type { Candle } from "../market-data/types.js";
import { RiskGuard, makeRiskGuard } from "../risk/guards.js";
import {
  FuturesExchangeAdapter,
  type FuturesExchangeAdapterService,
} from "../exchange/futures-adapter.js";
import { ExchangeError } from "../exchange/adapter.js";
import { makeSimulatedFuturesExchangeAdapterService } from "../exchange/adapters/simulated-futures.js";
import type { CandleLike } from "../scalping/types.js";
import { runLadderGridBacktest } from "../scalping/ladder-grid.js";
import { money, toNumber } from "../utils/money.js";
import {
  PaperTradingRepository,
  PaperTradingRepositorySQLite,
} from "./repository.js";
import {
  advanceLadderBar,
  freshLadderState,
  freshWorkingState,
  runLadderPaperTradingIteration,
  configMatchesLadderState,
  type LadderPaperTradingOptions,
} from "./ladder-engine.js";
import type { LadderPaperState } from "./types.js";

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

function baseOptions(
  overrides: Partial<LadderPaperTradingOptions> = {},
): LadderPaperTradingOptions {
  return {
    exchange: "bybit",
    symbol: "TEST",
    timeframe: "15m",
    rungs: 2,
    gridStepPct: 1.0,
    gridMaxGrids: 5,
    gridPauseAfterLossBars: 0,
    feePct: 0.05,
    slippageBps: 0,
    initialCapital: 100,
    trendFilterPeriod: 0,
    leverage: 1,
    ...overrides,
  };
}

// Beautifully oscillating series: dips fill the long ladder, rallies take
// profit back to flat. Used for parity with the backtest.
function oscillatorSeries(): CandleLike[] {
  return [
    candle(100, 100, 100, 100, 0),
    candle(100, 101, 98.8, 99.0, 1),
    candle(99.0, 101.2, 99.0, 100.8, 2),
    candle(100.8, 100.8, 100.8, 100.8, 3),
  ];
}

/** Stepping the incremental engine bar-by-bar over the series must reproduce
 *  the validated backtest's final capital exactly (same state machine). */
function incrementalCapital(
  candles: CandleLike[],
  opts: LadderPaperTradingOptions,
): number {
  const w = freshWorkingState(opts.initialCapital);
  for (let i = 1; i < candles.length; i++) {
    advanceLadderBar(w, candles, i, opts, null);
  }
  return toNumber(w.capital);
}

function backtestCapital(
  candles: CandleLike[],
  opts: LadderPaperTradingOptions,
): number {
  const r = runLadderGridBacktest(candles, opts);
  return opts.initialCapital * (1 + r.totalReturnPct / 100);
}

describe("advanceLadderBar (incremental ladder engine)", () => {
  it("reproduces the backtest capital on a take-profit oscillator", () => {
    const opts = baseOptions();
    const candles = oscillatorSeries();
    expect(incrementalCapital(candles, opts)).toBeCloseTo(
      backtestCapital(candles, opts),
      6,
    );
    // Both engines must have actually traded (capital moved off 100).
    expect(incrementalCapital(candles, opts)).not.toBeCloseTo(100, 6);
  });

  it("reproduces the backtest capital under leverage", () => {
    const opts = baseOptions({ leverage: 3 });
    const candles = oscillatorSeries();
    expect(incrementalCapital(candles, opts)).toBeCloseTo(
      backtestCapital(candles, opts),
      6,
    );
  });

  it("stops out the whole ladder at the boundary and pauses after a loss", () => {
    const opts = baseOptions({ gridPauseAfterLossBars: 3 });
    // bar1 drops to just below the first step (99) -> rung1 fills.
    const candles = [
      candle(100, 100, 100, 100, 0),
      candle(100, 99.5, 98.9, 99.3, 1), // rung1 (level 99) fills; boundary is 92
      candle(99.3, 99.3, 90, 90, 2), // craters below boundary -> stop-out both
    ];
    const w = freshWorkingState(100);
    expect(advanceLadderBar(w, candles, 1, opts, null).closes.length).toBe(0);
    expect(w.longRungs.filter((r) => r.filled)).toHaveLength(1);
    // The boundary stop takes the WHOLE ladder: rung2 fills on the way down,
    // then both rungs are closed out at the boundary.
    expect(advanceLadderBar(w, candles, 2, opts, null).closes.length).toBe(2);
    expect(w.totalLosses).toBe(2);
    expect(w.longRungs.filter((r) => r.filled)).toHaveLength(0);
    expect(w.paused).toBe(3);
    // While paused, bars advance for free and the counter decrements.
    expect(advanceLadderBar(w, candles, 0, opts, null).closes.length).toBe(0);
    expect(w.paused).toBe(2);
    expect(incrementalCapital(candles, opts)).toBeCloseTo(
      backtestCapital(candles, opts),
      6,
    );
  });

  it("fills only the first rung when price touches the first step", () => {
    const opts = baseOptions({ rungs: 3 });
    // low (98.9) reaches the first step (level 99) but not the second (98),
    // and the high stays under the TP target (100) so the fill holds.
    const candles = [
      candle(100, 100, 100, 100, 0),
      candle(100, 99.5, 98.9, 99.3, 1),
    ];
    const w = freshWorkingState(100);
    advanceLadderBar(w, candles, 1, opts, null);
    const filled = w.longRungs.filter((r) => r.filled);
    expect(filled).toHaveLength(1);
    expect(filled[0].rungIndex).toBe(1);
    expect(w.longRungs.length).toBe(3); // resting rungs remain armed
  });

  it("re-anchors peak and pauses instead of latching dead on flat drawdown breach", () => {
    // 2026-09-03: the ENA shadow (+2.64 book) went permanently silent after
    // an 8% peak-to-capital slide — flat capital can never trade its way back.
    const opts = baseOptions({
      maxDrawdownPct: 8,
      gridPauseAfterLossBars: 3,
      chopGateAdxThreshold: 0,
    });
    const candles = [
      candle(100, 100, 100, 100, 0),
      candle(100, 100.1, 99.9, 100, 1),
      candle(100, 100.1, 99.9, 100, 2),
      candle(100, 100.1, 99.9, 100, 3),
      candle(100, 100.1, 99.9, 100, 4),
      candle(100, 99.0, 98.9, 99.3, 5), // 1% dip fills the reseeded rung
    ];
    const w = freshWorkingState(100);
    w.capital = money(91); // 9% under peak while flat
    advanceLadderBar(w, candles, 1, opts, null);
    expect(toNumber(w.peak)).toBeCloseTo(91, 6);
    expect(w.paused).toBe(3);
    // Pause decrements without reseeding…
    advanceLadderBar(w, candles, 2, opts, null);
    advanceLadderBar(w, candles, 3, opts, null);
    advanceLadderBar(w, candles, 4, opts, null);
    expect(w.paused).toBe(0);
    // …then the engine seeds again instead of staying dead.
    advanceLadderBar(w, candles, 5, opts, null);
    expect(w.longRungs.length).toBeGreaterThan(0);
    expect(w.longRungs.filter((r) => r.filled)).toHaveLength(1);
  });

  it("holds parity with the backtest through a drawdown-kill breach and recovery", () => {
    // Both engines must agree that a flat breach is paused-then-retry, not
    // death: previously the backtest never enforced the kill at all, so
    // sweeps fitted configs on recovery trades live refuses to take.
    const opts = baseOptions({
      rungs: 1,
      gridStepPct: 0.5,
      gridPauseAfterLossBars: 0,
      leverage: 3,
      maxDrawdownPct: 8,
      chopGateAdxThreshold: 0,
    });
    const candles: CandleLike[] = [];
    let price = 100;
    let n = 0;
    const bar = (o: number, h: number, l: number, c: number) => {
      candles.push({
        timestamp: new Date(1000 * 60 * 15 * n++),
        open: o,
        high: h,
        low: l,
        close: c,
        volume: 1000,
      });
      price = c;
    };
    bar(100, 100, 100, 100);
    for (let i = 0; i < 20; i++) {
      const s = i % 2 === 0 ? 1 : -1;
      bar(price, price + 0.6, price - 0.6, price + 0.4 * s);
    }
    for (let k = 0; k < 3; k++) {
      bar(price, price, price * 0.994, price * 0.996);
      bar(price, price, price * 0.7, price * 0.72);
    }
    for (let i = 0; i < 60; i++) {
      const s = i % 2 === 0 ? 1 : -1;
      bar(price, price + 0.6, price - 0.6, price + 0.4 * s);
    }
    expect(incrementalCapital(candles, opts)).toBeCloseTo(
      backtestCapital(candles, opts),
      6,
    );
  });
});

describe("runLadderPaperTradingIteration (persistence + resume)", () => {
  it("round-trips the confirmed rung quantity across a restart", async () => {
    const db = new Database(":memory:");
    const repo = new PaperTradingRepositorySQLite(db);
    const opts = baseOptions();
    const state = {
      ...freshLadderState(opts),
      longRungs: [
        {
          rungIndex: 1,
          side: "long" as const,
          level: 99,
          step: 1,
          filled: true,
          entryPrice: 99,
          entryBar: 1,
          entryTimestamp: 1000 * 60 * 15,
          filledQty: 0.123,
        },
      ],
    };

    await Effect.runPromise(repo.ensureTables());
    await Effect.runPromise(repo.saveLadderState(state));
    const loaded = await Effect.runPromise(
      repo.getLadderState(opts.exchange, opts.symbol, opts.timeframe),
    );

    expect(loaded?.longRungs[0]?.filledQty).toBe(0.123);
  });

  it("persists state and resumes from the last processed candle", async () => {
    const db = new Database(":memory:");
    const repo = new PaperTradingRepositorySQLite(db);
    const opts = baseOptions();

    // Gateway returns a growing window: one extra bar on each call.
    let calls = 0;
    const series = oscillatorSeries();
    const gateway = {
      fetchTick: () => Effect.fail({ reason: "not used" } as never),
      fetchOHLCV: () =>
        Effect.succeed(
          (calls++ === 0
            ? series.slice(0, 4)
            : [...series, candle(101, 101, 100, 100.5, 4)]) as Candle[],
        ),
      fetchOrderBook: () => Effect.fail({ reason: "not used" } as never),
      fetchSymbols: () => Effect.fail({ reason: "not used" } as never),
      fetchDemoSymbols: () => Effect.fail({ reason: "not used" } as never),
      fetch24hrVolumes: () => Effect.succeed({}),
      fetchFundingRates: () => Effect.succeed([]),
    };

    const run = () =>
      Effect.runPromise(
        runLadderPaperTradingIteration(opts).pipe(
          Effect.provideService(PaperTradingRepository, repo),
          Effect.provideService(MarketDataGateway, gateway),
        ),
      );

    const first = await run();
    expect(first.closedThisIteration).toBeGreaterThan(0);
    expect(first.capital).not.toBe(100);

    const saved = await Effect.runPromise(
      repo.getLadderState(opts.exchange, opts.symbol, opts.timeframe),
    );
    expect(saved).not.toBeNull();
    expect(saved!.lastTimestamp).not.toBeNull();
    expect(saved!.initialCapital).toBe(100);
    expect(toNumber(saved!.capital)).toBeCloseTo(first.capital, 9);
    expect(configMatchesLadderState(saved!, opts)).toBe(true);

    // Second iteration resumes; the older bars are not reprocessed (a
    // reprocess would not change already-settled capital anyway), and it
    // advances over the single new bar.
    const second = await run();
    expect(second.closedThisIteration).toBeGreaterThanOrEqual(0);
    expect(second.capital).toBeCloseTo(first.capital, 9);
  });

  it("does not replay indicator warmup bars in forward-only mode", async () => {
    const db = new Database(":memory:");
    const repo = new PaperTradingRepositorySQLite(db);
    const candles = oscillatorSeries();
    const result = await Effect.runPromise(
      runLadderPaperTradingIteration(baseOptions({ forwardOnly: true })).pipe(
        Effect.provideService(PaperTradingRepository, repo),
        Effect.provideService(MarketDataGateway, {
          fetchTick: () => Effect.fail({ reason: "n" } as never),
          fetchOHLCV: () => Effect.succeed(candles as Candle[]),
          fetchOrderBook: () => Effect.fail({ reason: "n" } as never),
          fetchSymbols: () => Effect.fail({ reason: "n" } as never),
          fetchDemoSymbols: () => Effect.fail({ reason: "n" } as never),
          fetch24hrVolumes: () => Effect.succeed({}),
          fetchFundingRates: () => Effect.succeed([]),
        }),
      ),
    );

    expect(result.action).toBe("hold");
    expect(result.closedThisIteration).toBe(0);
    expect(result.capital).toBe(100);
    expect(result.note).toContain("ladder iter over 1 bars");
    expect(result.unrealizedPnl).toBeDefined();
    expect(result.equity).toBeDefined();
  });

  it("reports action=opened when an iteration fills a rung without closing it", async () => {
    const db = new Database(":memory:");
    const repo = new PaperTradingRepositorySQLite(db);
    const opts = baseOptions();
    // bar1 dips to fill rung1 (level 99); high stays under the TP target so
    // the rung remains open at the end of the iteration.
    const candles = [
      candle(100, 100, 100, 100, 0),
      candle(100, 99.5, 98.9, 99.3, 1),
    ];
    const result = await Effect.runPromise(
      runLadderPaperTradingIteration(opts).pipe(
        Effect.provideService(PaperTradingRepository, repo),
        Effect.provideService(MarketDataGateway, {
          fetchTick: () => Effect.fail({ reason: "n" } as never),
          fetchOHLCV: () => Effect.succeed(candles as Candle[]),
          fetchOrderBook: () => Effect.fail({ reason: "n" } as never),
          fetchSymbols: () => Effect.fail({ reason: "n" } as never),
          fetchDemoSymbols: () => Effect.fail({ reason: "n" } as never),
          fetch24hrVolumes: () => Effect.succeed({}),
          fetchFundingRates: () => Effect.succeed([]),
        }),
      ),
    );
    expect(result.action).toBe("opened");
    expect(result.openRungs).toBe(1);
    expect(result.closedThisIteration).toBe(0);
    expect(result.unrealizedPnl).toBeDefined();
    expect(result.equity).toBeDefined();
    expect(result.equity!).toBeGreaterThan(result.capital);
  });

  it("executes live fills and closes through the exchange adapter", async () => {
    const db = new Database(":memory:");
    const repo = new PaperTradingRepositorySQLite(db);
    const opts: LadderPaperTradingOptions = {
      ...baseOptions(),
      isLive: true,
      maxPositionPct: 50,
      productType: "USDT-FUTURES",
      marginMode: "isolated",
    };
    const gateway = {
      fetchTick: () => Effect.fail({ reason: "n" } as never),
      fetchOHLCV: () => Effect.succeed(oscillatorSeries() as Candle[]),
      fetchOrderBook: () => Effect.fail({ reason: "n" } as never),
      fetchSymbols: () => Effect.fail({ reason: "n" } as never),
      fetchDemoSymbols: () => Effect.fail({ reason: "n" } as never),
      fetch24hrVolumes: () => Effect.succeed({}),
      fetchFundingRates: () => Effect.succeed([]),
    };
    const riskGuard = makeRiskGuard({
      liveTradingEnabled: true,
      maxPositionSizePct: 100,
      maxDailyLossPct: 100,
      maxDrawdownPct: 100,
      minCapital: 0,
      maxTradesPerDay: Number.MAX_SAFE_INTEGER,
      maxLeverage: 10,
      allowedProductTypes: ["USDT-FUTURES"],
    });

    const adapter = await Effect.runPromise(
      makeSimulatedFuturesExchangeAdapterService(
        gateway,
        { USDT: 1000 },
        "bybit-futures",
      ),
    );

    const result = await Effect.runPromise(
      runLadderPaperTradingIteration(opts).pipe(
        Effect.provideService(PaperTradingRepository, repo),
        Effect.provideService(MarketDataGateway, gateway),
        Effect.provideService(RiskGuard, riskGuard),
        Effect.provideService(FuturesExchangeAdapter, adapter),
      ),
    );
    // The oscillator fills the ladder and takes profit in the same window;
    // the LIVE path must place limit entries and market closes without error.
    expect(result.action).toBe("closed");
    expect(result.closedThisIteration).toBeGreaterThan(0);
    expect(result.note).toContain("[LIVE]");
    expect(result.capital).not.toBe(100);
  });

  it("sizes each live rung from the position cap (regression: 50% cap -> 0.01 fraction)", async () => {
    const db = new Database(":memory:");
    const repo = new PaperTradingRepositorySQLite(db);
    const opts: LadderPaperTradingOptions = {
      ...baseOptions(), // capital 100, rungs 2
      isLive: true,
      maxPositionPct: 50,
      productType: "USDT-FUTURES",
      marginMode: "isolated",
    };
    const gateway = {
      fetchTick: () => Effect.fail({ reason: "n" } as never),
      fetchOHLCV: () => Effect.succeed(oscillatorSeries() as Candle[]),
      fetchOrderBook: () => Effect.fail({ reason: "n" } as never),
      fetchSymbols: () => Effect.fail({ reason: "n" } as never),
      fetchDemoSymbols: () => Effect.fail({ reason: "n" } as never),
      fetch24hrVolumes: () => Effect.succeed({}),
      fetchFundingRates: () => Effect.succeed([]),
    };
    const riskGuard = makeRiskGuard({
      liveTradingEnabled: true,
      maxPositionSizePct: 100,
      maxDailyLossPct: 100,
      maxDrawdownPct: 100,
      minCapital: 0,
      maxTradesPerDay: Number.MAX_SAFE_INTEGER,
      maxLeverage: 10,
      allowedProductTypes: ["USDT-FUTURES"],
    });
    let lastOrderNotional = 0;
    const adapter = await Effect.runPromise(
      makeSimulatedFuturesExchangeAdapterService(
        gateway,
        { USDT: 1000 },
        "bybit-futures",
      ),
    );
    const recordingAdapter: FuturesExchangeAdapterService = {
      ...adapter,
      placeOrder: (req) => {
        lastOrderNotional = toNumber(req.size.times(req.price ?? money(0)));
        return adapter.placeOrder(req);
      },
    };

    await Effect.runPromise(
      runLadderPaperTradingIteration(opts).pipe(
        Effect.provideService(PaperTradingRepository, repo),
        Effect.provideService(MarketDataGateway, gateway),
        Effect.provideService(RiskGuard, riskGuard),
        Effect.provideService(FuturesExchangeAdapter, recordingAdapter),
      ),
    );
    // capital 100, 50% position cap, 2 rungs => 25 USDT per rung. The
    // precedence bug clamped 50 to 1 before /100, sizing 0.25 USDT instead.
    expect(lastOrderNotional).toBeGreaterThan(20);
    expect(lastOrderNotional).toBeLessThan(30);
  });

  it("uses dynamic leverage that scales with account size (small acct -> low cap)", async () => {
    const db = new Database(":memory:");
    const repo = new PaperTradingRepositorySQLite(db);
    // $1000 account, 50% position budget -> the account-scaled cap is
    // 25x (size tier for $500-$5000) discounted x0.5 (budget >25%) = 12x.
    // maxLeverage=150 configured; the effective cap must be ~12x, NOT 150x
    // and not the old static 1x.
    const opts2: LadderPaperTradingOptions = {
      ...baseOptions(),
      initialCapital: 1000,
      isLive: true,
      maxPositionPct: 50,
      maxLeverage: 150,
      productType: "USDT-FUTURES",
      marginMode: "isolated",
    };
    const gateway = {
      fetchTick: () => Effect.fail({ reason: "n" } as never),
      fetchOHLCV: () => Effect.succeed(oscillatorSeries() as Candle[]),
      fetchOrderBook: () => Effect.fail({ reason: "n" } as never),
      fetchSymbols: () => Effect.fail({ reason: "n" } as never),
      fetchDemoSymbols: () => Effect.fail({ reason: "n" } as never),
      fetch24hrVolumes: () => Effect.succeed({}),
      fetchFundingRates: () => Effect.succeed([]),
    };
    const riskGuard = makeRiskGuard({
      liveTradingEnabled: true,
      maxPositionSizePct: 100,
      maxDailyLossPct: 100,
      maxDrawdownPct: 100,
      minCapital: 0,
      maxTradesPerDay: Number.MAX_SAFE_INTEGER,
      maxLeverage: 150,
      allowedProductTypes: ["USDT-FUTURES"],
    });
    const adapter = await Effect.runPromise(
      makeSimulatedFuturesExchangeAdapterService(
        gateway,
        { USDT: 10000 },
        "bybit-futures",
      ),
    );
    let sentLeverage = 1;
    const recordingAdapter: FuturesExchangeAdapterService = {
      ...adapter,
      setLeverage: (_s, _p, _m, lev) => {
        sentLeverage = lev;
        return Effect.void;
      },
    };
    const result = await Effect.runPromise(
      runLadderPaperTradingIteration(opts2).pipe(
        Effect.provideService(PaperTradingRepository, repo),
        Effect.provideService(MarketDataGateway, gateway),
        Effect.provideService(RiskGuard, riskGuard),
        Effect.provideService(FuturesExchangeAdapter, recordingAdapter),
      ),
    );
    expect(result.action).toBe("closed");
    // Account-scaled cap for $1000@50% budget = floor(25*0.5)=12x; the fill
    // uses at most that. It must be >= 1 and <= 12 — never the raw 150x.
    expect(sentLeverage).toBeGreaterThanOrEqual(1);
    expect(sentLeverage).toBeLessThanOrEqual(12);
    expect(sentLeverage).toBeLessThan(150);
  });

  it("advances lastTimestamp on a live rollback so the failing bar is not reprocessed", async () => {
    const db = new Database(":memory:");
    const repo = new PaperTradingRepositorySQLite(db);
    const opts: LadderPaperTradingOptions = {
      ...baseOptions(),
      isLive: true,
      maxPositionPct: 50,
      productType: "USDT-FUTURES",
      marginMode: "isolated",
    };
    const gateway = {
      fetchTick: () => Effect.fail({ reason: "n" } as never),
      fetchOHLCV: () => Effect.succeed(oscillatorSeries() as Candle[]),
      fetchOrderBook: () => Effect.fail({ reason: "n" } as never),
      fetchSymbols: () => Effect.fail({ reason: "n" } as never),
      fetchDemoSymbols: () => Effect.fail({ reason: "n" } as never),
      fetch24hrVolumes: () => Effect.succeed({}),
      fetchFundingRates: () => Effect.succeed([]),
    };
    const riskGuard = makeRiskGuard({
      liveTradingEnabled: true,
      maxPositionSizePct: 100,
      maxDailyLossPct: 100,
      maxDrawdownPct: 100,
      minCapital: 0,
      maxTradesPerDay: Number.MAX_SAFE_INTEGER,
      maxLeverage: 10,
      allowedProductTypes: ["USDT-FUTURES"],
    });
    const base = await Effect.runPromise(
      makeSimulatedFuturesExchangeAdapterService(
        gateway,
        { USDT: 1000 },
        "bybit-futures",
      ),
    );
    // Force the live executor to fail: every order is rejected.
    const failingAdapter: FuturesExchangeAdapterService = {
      ...base,
      placeOrder: () => Effect.fail(new ExchangeError("ab not enough (test)")),
    };
    const run = () =>
      Effect.runPromise(
        runLadderPaperTradingIteration(opts).pipe(
          Effect.provideService(PaperTradingRepository, repo),
          Effect.provideService(MarketDataGateway, gateway),
          Effect.provideService(RiskGuard, riskGuard),
          Effect.provideService(FuturesExchangeAdapter, failingAdapter),
        ),
      );

    const first = await run();
    expect(first.action).toBe("hold");
    expect(first.note).toContain("rolled back");
    // The ledger rolled back: capital unchanged.
    expect(first.capital).toBe(100);
    // Iteration 2 sees the SAME window: the rollback must have advanced
    // lastTimestamp, so the failing bar is NOT reprocessed (churn fix).
    const second = await run();
    expect(second.action).toBe("hold");
    expect(second.note).toBe("no new candle");
    expect(second.capital).toBe(100);
  });

  it("reconciles an orphan real position when the paper ledger is flat", async () => {
    const db = new Database(":memory:");
    const repo = new PaperTradingRepositorySQLite(db);
    const opts: LadderPaperTradingOptions = {
      ...baseOptions(),
      isLive: true,
      maxPositionPct: 50,
      productType: "USDT-FUTURES",
      marginMode: "isolated",
    };
    const gateway = {
      fetchTick: () => Effect.fail({ reason: "n" } as never),
      fetchOHLCV: () =>
        Effect.succeed([
          candle(100, 100, 100, 100, 0),
          candle(100, 100, 100, 100, 1),
        ] as Candle[]),
      fetchOrderBook: () => Effect.fail({ reason: "n" } as never),
      fetchSymbols: () => Effect.fail({ reason: "n" } as never),
      fetchDemoSymbols: () => Effect.fail({ reason: "n" } as never),
      fetch24hrVolumes: () => Effect.succeed({}),
      fetchFundingRates: () => Effect.succeed([]),
    };
    const riskGuard = makeRiskGuard({
      liveTradingEnabled: true,
      maxPositionSizePct: 100,
      maxDailyLossPct: 100,
      maxDrawdownPct: 100,
      minCapital: 0,
      maxTradesPerDay: Number.MAX_SAFE_INTEGER,
      maxLeverage: 10,
      allowedProductTypes: ["USDT-FUTURES"],
    });
    const base = await Effect.runPromise(
      makeSimulatedFuturesExchangeAdapterService(
        gateway,
        { USDT: 1000 },
        "bybit-futures",
      ),
    );
    let closedSide = "";
    const orphanAdapter: FuturesExchangeAdapterService = {
      ...base,
      getPosition: () =>
        Effect.succeed({
          symbol: opts.symbol,
          side: "long",
          productType: "USDT-FUTURES",
          marginMode: "isolated",
          leverage: 1,
          quantity: money(5),
          available: money(5),
          entryPrice: money(100),
          marginCoin: "USDT",
        }),
      closePosition: (req) => {
        closedSide = req.side;
        return Effect.succeed(null);
      },
    };

    const result = await Effect.runPromise(
      runLadderPaperTradingIteration(opts).pipe(
        Effect.provideService(PaperTradingRepository, repo),
        Effect.provideService(MarketDataGateway, gateway),
        Effect.provideService(RiskGuard, riskGuard),
        Effect.provideService(FuturesExchangeAdapter, orphanAdapter),
      ),
    );
    // The orphan long must be closed (sell side) even though the bar holds.
    expect(closedSide).toBe("sell");
    expect(result.action).toBe("hold");
  });

  it("blocks a symbol after a trading-terms (110126) failure instead of re-attempting", async () => {
    const db = new Database(":memory:");
    const repo = new PaperTradingRepositorySQLite(db);
    const opts: LadderPaperTradingOptions = {
      ...baseOptions({ symbol: "BLOCKED/USDT:USDT" }),
      isLive: true,
      maxPositionPct: 50,
      productType: "USDT-FUTURES",
      marginMode: "isolated",
    };
    let gatewayCalls = 0;
    const gateway = {
      fetchTick: () => Effect.fail({ reason: "n" } as never),
      fetchOHLCV: () =>
        Effect.succeed(
          (gatewayCalls++ === 0
            ? oscillatorSeries()
            : [
                ...oscillatorSeries(),
                candle(100, 100, 100, 100, 4),
              ]) as Candle[],
        ),
      fetchOrderBook: () => Effect.fail({ reason: "n" } as never),
      fetchSymbols: () => Effect.fail({ reason: "n" } as never),
      fetchDemoSymbols: () => Effect.fail({ reason: "n" } as never),
      fetch24hrVolumes: () => Effect.succeed({}),
      fetchFundingRates: () => Effect.succeed([]),
    };
    const riskGuard = makeRiskGuard({
      liveTradingEnabled: true,
      maxPositionSizePct: 100,
      maxDailyLossPct: 100,
      maxDrawdownPct: 100,
      minCapital: 0,
      maxTradesPerDay: Number.MAX_SAFE_INTEGER,
      maxLeverage: 10,
      allowedProductTypes: ["USDT-FUTURES"],
    });
    const base = await Effect.runPromise(
      makeSimulatedFuturesExchangeAdapterService(
        gateway,
        { USDT: 1000 },
        "bybit-futures",
      ),
    );
    let placeCalls = 0;
    const termsAdapter: FuturesExchangeAdapterService = {
      ...base,
      placeOrder: (_req) => {
        placeCalls += 1;
        return Effect.fail(
          new ExchangeError(
            "Bybit API 200 on /v5/order/create: 110126: You must sign the required agreement before trading this contract",
          ),
        );
      },
    };
    const run = () =>
      Effect.runPromise(
        runLadderPaperTradingIteration(opts).pipe(
          Effect.provideService(PaperTradingRepository, repo),
          Effect.provideService(MarketDataGateway, gateway),
          Effect.provideService(RiskGuard, riskGuard),
          Effect.provideService(FuturesExchangeAdapter, termsAdapter),
        ),
      );

    const first = await run();
    expect(first.note).toContain("rolled back");
    expect(placeCalls).toBe(1);
    // Iteration 2: the symbol is blocked for the cooldown — no order attempt.
    const second = await run();
    expect(second.note).toContain("blocked");
    expect(placeCalls).toBe(1);
  });

  it("holds (never reprocesses) when no candle is newer than the last processed", async () => {
    const db = new Database(":memory:");
    const repo = new PaperTradingRepositorySQLite(db);
    const opts = baseOptions();
    // Iteration 1 processes the oscillator to flat (capital moves off 100).
    const series = oscillatorSeries();
    const gateway = {
      fetchTick: () => Effect.fail({ reason: "n" } as never),
      fetchOHLCV: () => Effect.succeed(series as Candle[]),
      fetchOrderBook: () => Effect.fail({ reason: "n" } as never),
      fetchSymbols: () => Effect.fail({ reason: "n" } as never),
      fetchDemoSymbols: () => Effect.fail({ reason: "n" } as never),
      fetch24hrVolumes: () => Effect.succeed({}),
      fetchFundingRates: () => Effect.succeed([]),
    };
    const run = () =>
      Effect.runPromise(
        runLadderPaperTradingIteration(opts).pipe(
          Effect.provideService(PaperTradingRepository, repo),
          Effect.provideService(MarketDataGateway, gateway),
        ),
      );
    const first = await run();
    expect(first.closedThisIteration).toBeGreaterThan(0);
    const capitalAfterFirst = first.capital;

    // Iteration 2 sees the IDENTICAL window (no new candle): it must hold
    // without reprocessing — reprocessing would re-fill and re-close rungs,
    // double-counting capital.
    const second = await run();
    expect(second.action).toBe("hold");
    expect(second.note).toBe("no new candle");
    expect(second.capital).toBeCloseTo(capitalAfterFirst, 9);
    expect(second.closedThisIteration).toBe(0);
  });

  it("force-closes a rung held past maxHoldBars at the current close", () => {
    const opts = baseOptions({ maxHoldBars: 2 });
    // bar1 fills rung1; bars 2-4 keep price flat so neither TP nor boundary
    // hits. On bar 3 (entryBar 1, i=3 => held 2 bars) the rung must close.
    const candles = [
      candle(100, 100, 100, 100, 0),
      candle(100, 99.5, 98.9, 99.4, 1), // fill rung1 @ 99
      candle(99.4, 99.4, 99.0, 99.2, 2), // hold (target 100, boundary 92)
      candle(99.2, 99.4, 99.0, 99.3, 3), // i-entryBar = 2 >= maxHoldBars -> close
    ];
    const w = freshWorkingState(100);
    expect(advanceLadderBar(w, candles, 1, opts, null).closes.length).toBe(0);
    expect(advanceLadderBar(w, candles, 2, opts, null).closes.length).toBe(0);
    expect(advanceLadderBar(w, candles, 3, opts, null).closes.length).toBe(1);
    expect(w.longRungs.filter((r) => r.filled)).toHaveLength(0);
    // Closed at the close price (99.3, above the 99 entry) => a small win.
    expect(w.totalWins).toBe(1);
    expect(w.totalLosses).toBe(0);
  });

  it("config match detects a persisted-vs-requested config drift", () => {
    const state = freshLadderState(baseOptions({ rungs: 1 }));
    expect(state.rungs).toBe(1);
    // A config mismatch must be reported (drives the flat re-seed / open-hold
    // paths in runLadderPaperTradingIteration).
    expect(configMatchesLadderState(state, baseOptions({ rungs: 1 }))).toBe(
      true,
    );
    expect(configMatchesLadderState(state, baseOptions({ rungs: 2 }))).toBe(
      false,
    );
    expect(
      configMatchesLadderState(
        state,
        baseOptions({ rungs: 1, gridStepPct: 0.5 }),
      ),
    ).toBe(false);
  });
});

describe("ladder config-mismatch force-reseed", () => {
  it("force-closes open rungs and re-seeds when config changed and force-reseed is set", async () => {
    const db = new Database(":memory:");
    const repo = new PaperTradingRepositorySQLite(db);
    await Effect.runPromise(repo.ensureTables());
    // State persisted under the OLD config (gridMaxGrids 2) with a filled rung.
    const staleOpts = baseOptions({ gridMaxGrids: 2 });
    const staleState: LadderPaperState = {
      ...freshLadderState(staleOpts),
      longRungs: [
        {
          rungIndex: 1,
          side: "long",
          level: 99,
          step: 1,
          filled: true,
          entryPrice: 99,
          entryBar: 1,
          entryTimestamp: 1000 * 60 * 15,
        },
      ],
      longBase: 100,
      capital: money(99),
      peakCapital: money(100),
    };
    await Effect.runPromise(repo.saveLadderState(staleState));

    // New options: gridMaxGrids 3 (the whitelist changed) + force-reseed.
    const opts: LadderPaperTradingOptions = {
      ...baseOptions(),
      gridMaxGrids: 3,
      configMismatchAction: "force-reseed",
    };
    const gateway = {
      fetchTick: () => Effect.fail({ reason: "not used" } as never),
      // Current close 97 — a loss vs the 99 entry.
      fetchOHLCV: () =>
        Effect.succeed([
          candle(100, 100, 100, 100, 0),
          candle(100, 99, 97, 97, 1),
        ] as Candle[]),
      fetchOrderBook: () => Effect.fail({ reason: "not used" } as never),
      fetchSymbols: () => Effect.fail({ reason: "not used" } as never),
      fetchDemoSymbols: () => Effect.fail({ reason: "not used" } as never),
      fetch24hrVolumes: () => Effect.succeed({}),
      fetchFundingRates: () => Effect.succeed([]),
    };

    const result = await Effect.runPromise(
      runLadderPaperTradingIteration(opts).pipe(
        Effect.provideService(PaperTradingRepository, repo),
        Effect.provideService(MarketDataGateway, gateway),
      ),
    );

    expect(result.action).toBe("closed");
    expect(result.closedThisIteration).toBe(1);
    expect(result.openRungs).toBe(0);
    expect(result.note).toContain("force-closed 1 stale rung");

    // The persisted state must now match the NEW config (re-seeded fresh).
    const saved = await Effect.runPromise(
      repo.getLadderState(opts.exchange, opts.symbol, opts.timeframe),
    );
    expect(configMatchesLadderState(saved!, opts)).toBe(true);
  });

  it("still holds (does not force-close) when the default action is used", async () => {
    const db = new Database(":memory:");
    const repo = new PaperTradingRepositorySQLite(db);
    await Effect.runPromise(repo.ensureTables());
    const staleOpts = baseOptions({ gridMaxGrids: 2 });
    const staleState: LadderPaperState = {
      ...freshLadderState(staleOpts),
      longRungs: [
        {
          rungIndex: 1,
          side: "long",
          level: 99,
          step: 1,
          filled: true,
          entryPrice: 99,
          entryBar: 1,
          entryTimestamp: 1000 * 60 * 15,
        },
      ],
      longBase: 100,
      capital: money(99),
      peakCapital: money(100),
    };
    await Effect.runPromise(repo.saveLadderState(staleState));

    const opts: LadderPaperTradingOptions = {
      ...baseOptions(),
      gridMaxGrids: 3, // mismatch, but default action = hold
    };
    const gateway = {
      fetchTick: () => Effect.fail({ reason: "not used" } as never),
      fetchOHLCV: () =>
        Effect.succeed([
          candle(100, 100, 100, 100, 0),
          candle(100, 99, 97, 97, 1),
        ] as Candle[]),
      fetchOrderBook: () => Effect.fail({ reason: "not used" } as never),
      fetchSymbols: () => Effect.fail({ reason: "not used" } as never),
      fetchDemoSymbols: () => Effect.fail({ reason: "not used" } as never),
      fetch24hrVolumes: () => Effect.succeed({}),
      fetchFundingRates: () => Effect.succeed([]),
    };

    const result = await Effect.runPromise(
      runLadderPaperTradingIteration(opts).pipe(
        Effect.provideService(PaperTradingRepository, repo),
        Effect.provideService(MarketDataGateway, gateway),
      ),
    );

    expect(result.action).toBe("hold");
    expect(result.openRungs).toBe(1);
    expect(result.note).toContain("config mismatch on open ladder");
  });
});

describe("ladder per-position max-drawdown kill", () => {
  it("force-closes a bleeding long rung past the per-position drawdown threshold", () => {
    const opts = baseOptions({
      leverage: 7,
      rungs: 1,
      maxPositionDrawdownPct: 3,
    });
    // rung1 fills @ 99 on bar 1; bar 2's close drops 4% below entry (95.1)
    // => per-position drawdown kill fires (before the ladder boundary at 92).
    const candles = [
      candle(100, 100, 100, 100, 0),
      candle(100, 99.5, 98.9, 99.3, 1),
      candle(99.3, 99.3, 95.0, 95.1, 2),
    ];
    const w = freshWorkingState(100);
    advanceLadderBar(w, candles, 1, opts, null);
    const events = advanceLadderBar(w, candles, 2, opts, null);
    expect(events.closes).toHaveLength(1);
    expect(events.closes[0].reason).toBe("stop");
    expect(w.longRungs.filter((r) => r.filled)).toHaveLength(0);
    // The loss is capped by the close price (~4% on the rung's capital share
    // at 7x leverage), NOT the full liquidation wipe — the guard's whole
    // point is to exit before the ladder stop/liquidation takes more.
    expect(toNumber(w.capital)).toBeGreaterThan(60);
    expect(toNumber(w.capital)).toBeLessThan(100);
  });

  it("does not kill when the drawdown stays under the threshold", () => {
    const opts = baseOptions({
      leverage: 7,
      rungs: 1,
      maxPositionDrawdownPct: 3,
    });
    const candles = [
      candle(100, 100, 100, 100, 0),
      candle(100, 99.5, 98.9, 99.3, 1),
      // close 98 = 1% below entry: under the 3% threshold, rung stays open.
      candle(99.3, 99.3, 97.5, 98.0, 2),
    ];
    const w = freshWorkingState(100);
    advanceLadderBar(w, candles, 1, opts, null);
    const events = advanceLadderBar(w, candles, 2, opts, null);
    expect(events.closes).toHaveLength(0);
    expect(w.longRungs.filter((r) => r.filled)).toHaveLength(1);
  });
});
