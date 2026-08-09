import { describe, expect, it } from "bun:test";
import * as fc from "fast-check";
import { Effect, Layer } from "effect";
import { MarketDataGateway, MarketDataError } from "../market-data/gateway.js";
import {
  MarketDataRepository,
  type MarketDataRepositoryService,
} from "../market-data/repository.js";
import type { Candle, Tick } from "../market-data/types.js";
import {
  DEFAULT_GRID_UNIVERSE_SEARCH_SPACE,
  MIN_UNIVERSE_24H_VOLUME_USDT,
  passesTimeSplitGate,
  runMarketUniverseScan,
  type GridUniverseOptions,
} from "./grid-universe.js";
import { runGridBacktest, type GridOptions } from "./grid.js";
import {
  accountScaledTargetFillsPerDay,
  accountSymbolCap,
  barsPerDayForTimeframe,
  computeFillFrequencyPct,
  gateScoredEligibility,
  passesStage2Screen,
  selectUniversePortfolio,
  STAGE2_MAX_ATR_PCT,
  STAGE2_MIN_ADX,
  STAGE2_MIN_ATR_PCT,
  type GridUniverseEntry,
} from "./grid-universe.js";
import { runGridWalkForward } from "./grid.js";

const candle = (open: number, high: number, low: number) => ({
  open,
  high,
  low,
});

const entry = (
  symbol: string,
  edgePerTradePct: number,
  fillsPerDay: number,
): GridUniverseEntry => ({
  symbol,
  candles: 100,
  bestParams: {
    gridStepPct: 0.5,
    gridMaxGrids: 2,
    gridPauseAfterLossBars: 0,
  },
  walkForward: {
    windows: [],
    aggregateReturnPct: edgePerTradePct * 10,
    profitableWindowsPct: 100,
    maxDrawdownPct: 0,
    totalTrades: 10,
  },
  passed: true,
  volatility: 1,
  oosTrades: 10,
  fillsPerDay,
  edgePerTradePct,
});

describe("computeFillFrequencyPct", () => {
  it("reports 100 when the gate is disabled, regardless of candle range", () => {
    const candles = [
      candle(100, 100.5, 99.5), // never touches a 2% step
      candle(100, 101.5, 98.5),
    ];
    expect(computeFillFrequencyPct(candles, 2, 0)).toBe(100);
  });

  it("reports 100 for an empty candle set", () => {
    expect(computeFillFrequencyPct([], 2, 50)).toBe(100);
  });

  it("counts a candle that reaches the grid step downward (buy fill)", () => {
    const candles = [
      candle(100, 100.1, 97.9), // low <= 100 * 0.98 -> touched
      candle(100, 100.1, 99), // never reaches step
    ];
    expect(computeFillFrequencyPct(candles, 2, 50)).toBe(50);
  });

  it("counts a candle that reaches the grid step upward (sell fill)", () => {
    const candles = [
      candle(100, 102.1, 99.9), // high >= 100 * 1.02 -> touched
      candle(100, 101, 99.1), // never reaches step
    ];
    expect(computeFillFrequencyPct(candles, 2, 50)).toBe(50);
  });

  it("a candle reaching the step in either direction counts once", () => {
    const candles = [
      candle(100, 102.1, 97.9), // both directions -> still one touch
      candle(100, 100.5, 99.5), // no touch
      candle(100, 98.5, 97.5), // low <= 98 -> touch
    ];
    expect(computeFillFrequencyPct(candles, 2, 50)).toBeCloseTo(66.67, 2);
  });

  it("compares the percentage against a 0-100 scale, not a 0..1 fraction", () => {
    // All candles touch -> 100, which passes a 80% gate but would fail a 0.8
    // fraction gate (0.8 > 1) if the scale were misread. This guards against
    // regressing the doc'd 0-100 % semantics.
    const candles = [candle(100, 102.1, 97.9)];
    expect(computeFillFrequencyPct(candles, 2, 80)).toBe(100);
  });

  it("stays within 0..100 and never rises as the grid step widens", () => {
    const arbCandle = fc
      .record({
        open: fc.double({ min: 1, max: 1000, noNaN: true }),
        up: fc.double({ min: 0, max: 50, noNaN: true }),
        down: fc.double({ min: 0, max: 50, noNaN: true }),
      })
      .map(({ open, up, down }) => ({
        open,
        high: open + up,
        low: open - down,
      }));
    fc.assert(
      fc.property(
        fc.array(arbCandle, { minLength: 1, maxLength: 50 }),
        fc.double({ min: 0.1, max: 5, noNaN: true }),
        (candles, step) => {
          const narrow = computeFillFrequencyPct(candles, step, 1);
          const wide = computeFillFrequencyPct(candles, step * 2, 1);
          expect(narrow).toBeGreaterThanOrEqual(0);
          expect(narrow).toBeLessThanOrEqual(100);
          expect(wide).toBeLessThanOrEqual(narrow);
        },
      ),
    );
  });
});

describe("passesStage2Screen", () => {
  it("accepts a trending, liquid-volatility candidate", () => {
    expect(passesStage2Screen({ adx14: 25, atr14Pct: 0.01 })).toBe(true);
  });

  it("accepts boundary values exactly at the thresholds", () => {
    expect(
      passesStage2Screen({
        adx14: STAGE2_MIN_ADX,
        atr14Pct: STAGE2_MIN_ATR_PCT,
      }),
    ).toBe(true);
    expect(
      passesStage2Screen({
        adx14: STAGE2_MIN_ADX,
        atr14Pct: STAGE2_MAX_ATR_PCT,
      }),
    ).toBe(true);
  });

  it("rejects chop (ADX below 15)", () => {
    expect(
      passesStage2Screen({ adx14: STAGE2_MIN_ADX - 1, atr14Pct: 0.01 }),
    ).toBe(false);
  });

  it("rejects dead markets (ATR% below the floor)", () => {
    expect(
      passesStage2Screen({ adx14: STAGE2_MIN_ADX, atr14Pct: 0.0004 }),
    ).toBe(false);
  });

  it("rejects moon-shots (ATR% above 10)", () => {
    expect(
      passesStage2Screen({ adx14: STAGE2_MIN_ADX, atr14Pct: 0.2 }),
    ).toBe(false);
  });
});

describe("barsPerDayForTimeframe", () => {
  it("derives bars per day from the timeframe", () => {
    expect(barsPerDayForTimeframe("15m")).toBe(96);
    expect(barsPerDayForTimeframe("5m")).toBe(288);
    expect(barsPerDayForTimeframe("1h")).toBe(24);
    expect(barsPerDayForTimeframe("1d")).toBe(1);
  });

  it("defaults to 15m for unparseable timeframes", () => {
    expect(barsPerDayForTimeframe("fortnight")).toBe(96);
  });
});

describe("accountScaledTargetFillsPerDay", () => {
  it("clamps to the 5/day floor at $100 accounts", () => {
    expect(accountScaledTargetFillsPerDay(100)).toBe(5);
    expect(accountScaledTargetFillsPerDay(10)).toBe(5);
  });

  it("scales linearly in between", () => {
    expect(accountScaledTargetFillsPerDay(200)).toBe(10);
    expect(accountScaledTargetFillsPerDay(500)).toBe(25);
  });

  it("clamps to the 50/day ceiling at $1000+ accounts", () => {
    expect(accountScaledTargetFillsPerDay(1000)).toBe(50);
    expect(accountScaledTargetFillsPerDay(10000)).toBe(50);
  });
});

describe("selectUniversePortfolio", () => {
  it("picks the highest-edge entries first", () => {
    const a = entry("A", 0.2, 10);
    const b = entry("B", 0.9, 10);
    const c = entry("C", 0.5, 10);
    const selected = selectUniversePortfolio([a, b, c], 25);
    expect(selected.map((e) => e.symbol)).toEqual(["B", "C", "A"]);
  });

  it("stops when the cumulative fills/day target is met", () => {
    const a = entry("A", 0.9, 4);
    const b = entry("B", 0.5, 4);
    const c = entry("C", 0.1, 4);
    const selected = selectUniversePortfolio([a, b, c], 5);
    // A + B reach 8 >= 5; C is not needed.
    expect(selected.map((e) => e.symbol)).toEqual(["A", "B"]);
  });

  it("caps each symbol's fills at the per-symbol cap", () => {
    const a = entry("A", 0.9, 100);
    const b = entry("B", 0.5, 100);
    const selected = selectUniversePortfolio([a, b], 15);
    // Each contributes min(100, 10) = 10; two symbols reach 20 >= 15.
    expect(selected.map((e) => e.symbol)).toEqual(["A", "B"]);
  });

  it("never selects entries without a computed edge or fills", () => {
    const a = entry("A", 0.9, 0);
    const b: GridUniverseEntry = { ...entry("B", 0.8, 10), edgePerTradePct: undefined };
    expect(selectUniversePortfolio([a, b], 50)).toEqual([b]);
  });

  it("selects nothing for a zero target", () => {
    expect(selectUniversePortfolio([entry("A", 0.9, 10)], 0)).toEqual([]);
  });

  it("caps the portfolio at accountSymbolCap symbols", () => {
    const many = Array.from({ length: 20 }, (_, i) =>
      entry(`CAP${i}`, 1 - i * 0.01, 10),
    );
    // Default capital $1000 -> cap 50: never binds on 20 candidates.
    expect(selectUniversePortfolio(many, 1000, 10)).toHaveLength(20);
    // $100 -> max(1, floor(100 × 0.5 / 10)) = 5 concurrent positions.
    expect(selectUniversePortfolio(many, 1000, 10, 100)).toHaveLength(
      accountSymbolCap(100),
    );
    expect(selectUniversePortfolio(many, 1000, 10, 150)).toHaveLength(
      accountSymbolCap(150),
    );
  });

  it("keeps at least 1 symbol for tiny accounts (concentrated mode)", () => {
    const many = Array.from({ length: 20 }, (_, i) =>
      entry(`CAP${i}`, 1 - i * 0.01, 10),
    );
    // Raw floor(A × 0.5 / 10) is 0 for A < 20; max(1, …) must kick in.
    expect(accountSymbolCap(1)).toBe(1);
    expect(accountSymbolCap(10)).toBe(1);
    expect(accountSymbolCap(19)).toBe(1);
    expect(accountSymbolCap(20)).toBe(1);
    expect(accountSymbolCap(39)).toBe(1);
    // Crosses to 2 at A = 40: floor(40 × 0.5 / 10) = 2.
    expect(accountSymbolCap(40)).toBe(2);
    // Selection honors the floor: tiny accounts pick exactly 1 symbol.
    expect(selectUniversePortfolio(many, 1000, 10, 10)).toHaveLength(1);
    expect(selectUniversePortfolio(many, 1000, 10, 19)).toHaveLength(1);
    // Non-tiny accounts unchanged.
    expect(selectUniversePortfolio(many, 1000, 10, 40)).toHaveLength(2);
    expect(accountSymbolCap(1000)).toBe(50);
  });

  it("keeps the highest-edge symbols under a binding capital cap", () => {
    const a = entry("A", 0.9, 10);
    const b = entry("B", 0.5, 10);
    const c = entry("C", 0.1, 10);
    // $20 account -> floor(20 × 0.5 / 10) = 1 symbol: only A fits.
    expect(
      selectUniversePortfolio([a, b, c], 1000, 10, 20).map((e) => e.symbol),
    ).toEqual(["A"]);
  });
});


describe("gateScoredEligibility (stage-4)", () => {
  const BAR_MS = 15 * 60 * 1000;
  // Default gate windowing (trainBars 11520 / testBars 4320 / minWindows 10)
  // needs >= 11520 + 10 × 4320 candles; generate exactly that.
  const GATE_CANDLES = 54720;

  const GATE_OPTIONS: GridUniverseOptions = {
    exchange: "bitget-futures",
    timeframe: "15m",
    initialCapital: 10000,
    minCandles: GATE_CANDLES,
    trainWindow: 100,
    testWindow: 200,
    minProfitableWindowsPct: 60,
    minAggregateReturnPct: 0,
    minFillFrequencyPct: 0,
    feePct: 0.06,
    slippageBps: 2,
    trendFilterPeriod: 0,
    searchSpace: DEFAULT_GRID_UNIVERSE_SEARCH_SPACE,
  };

  /**
   * 54720 exact-15m-spaced candles ending ~1 bar before the test run.
   * `wick` = doji flats alternating with balanced both-wick bars at price
   * 100 (upMove == downMove exactly → ADX stays 0, so the chop gate never
   * blocks; every round trip wins) — the gate-clearing fixture. `trend` =
   * steady uptrend with V-dips (walk-forward profits on the dips, but the
   * strong trend keeps ADX ≥ 24 so the chop gate blocks every entry) — the
   * gate-failing fixture. Both pass walk-forward.
   */
  function gateCandles(wick: boolean): Candle[] {
    const end = Date.now() - BAR_MS;
    const rows: Candle[] = [];
    let price = 100;
    for (let index = 0; index < GATE_CANDLES; index += 1) {
      const base = {
        exchange: "bitget-futures",
        symbol: wick ? "WICK/USDT:USDT" : "TREND/USDT:USDT",
        timeframe: "15m",
        volume: 10,
        timestamp: new Date(end - (GATE_CANDLES - 1 - index) * BAR_MS),
      };
      if (wick) {
        const wickBar = index % 60 === 0 && index > 10;
        rows.push(
          wickBar
            ? { ...base, open: 100, high: 100.5, low: 99.5, close: 100 }
            : { ...base, open: 100, high: 100, low: 100, close: 100 },
        );
      } else {
        const open = price;
        const dip = index % 90 === 0 && index > 10;
        const close = dip ? open * (1 - 0.5 / 100) : open * (1 + 0.02 / 100);
        rows.push(
          dip
            ? { ...base, open, high: open * (1 + 0.05 / 100), low: close, close }
            : { ...base, open, high: close, low: open, close },
        );
        price = close;
      }
    }
    return rows;
  }

  /** Mirrors evaluateUniverseSymbol's survivor path: run the walk-forward
   * over the full series and derive bestParams + passed from it. */
  function walkForwardEntry(
    symbol: string,
    candles: Candle[],
  ): GridUniverseEntry {
    const walkForward = runGridWalkForward(candles, {
      trainWindow: GATE_OPTIONS.trainWindow,
      testWindow: GATE_OPTIONS.testWindow,
      initialCapital: GATE_OPTIONS.initialCapital,
      searchSpace: GATE_OPTIONS.searchSpace,
      baseOptions: {
        feePct: GATE_OPTIONS.feePct,
        slippageBps: GATE_OPTIONS.slippageBps,
        trendFilterPeriod: GATE_OPTIONS.trendFilterPeriod,
        leverage: 1,
      },
    });
    const lastWindow = walkForward.windows[walkForward.windows.length - 1];
    const bestParams = lastWindow?.params ?? {
      gridStepPct: GATE_OPTIONS.searchSpace.gridStepPct[0] ?? 1,
      gridMaxGrids: GATE_OPTIONS.searchSpace.gridMaxGrids[0] ?? 2,
      gridPauseAfterLossBars:
        GATE_OPTIONS.searchSpace.gridPauseAfterLossBars[0] ?? 0,
    };
    return {
      symbol,
      candles: candles.length,
      bestParams: {
        gridStepPct: bestParams.gridStepPct,
        gridMaxGrids: bestParams.gridMaxGrids,
        gridPauseAfterLossBars: bestParams.gridPauseAfterLossBars,
      },
      walkForward,
      passed:
        walkForward.profitableWindowsPct >=
          GATE_OPTIONS.minProfitableWindowsPct &&
        walkForward.aggregateReturnPct >= GATE_OPTIONS.minAggregateReturnPct,
    };
  }

  it("keeps a survivor when a target×ADX combo clears every gate, picking the best return", () => {
    const candles = gateCandles(true);
    const entry = walkForwardEntry("WICK/USDT:USDT", candles);
    expect(entry.passed).toBe(true);

    const gated = gateScoredEligibility(entry, candles, GATE_OPTIONS);
    expect(gated).not.toBeNull();
    if (gated === null) return;
    // Both ADX gates pass at target 1 with identical compounded return; the
    // sweep keeps the first best combo → target 1, ADX 24.
    expect(gated.validatedTargetRatio).toBe(1);
    expect(gated.validatedChopGateAdx).toBe(24);
  });

  it("drops a walk-forward survivor when no target×ADX combo clears the gates", () => {
    const candles = gateCandles(false);
    const entry = walkForwardEntry("TREND/USDT:USDT", candles);
    expect(entry.passed).toBe(true);

    expect(gateScoredEligibility(entry, candles, GATE_OPTIONS)).toBeNull();
  });
});


describe("passesTimeSplitGate (regime-concentration guard)", () => {
  const BAR_MS = 15 * 60 * 1000;
  const grid: GridOptions = {
    gridStepPct: 1,
    gridMaxGrids: 1.5,
    gridPauseAfterLossBars: 0,
    feePct: 0.02,
    slippageBps: 1,
    initialCapital: 100,
    trendFilterPeriod: 0,
    leverage: 1,
    positionFraction: 0.5,
    chopGateAdxThreshold: 0,
    targetRatio: 1,
    onlyWithTrend: false,
  };

  // Wick bars at price 100 with ±1.1% wicks: every bar touches the 1% grid
  // levels (99 buy / 101 sell) -> each cycle wins ~2% minus fees.
  function wickCandle(symbol: string, ts: number): Candle {
    return {
      exchange: "bitget-futures",
      symbol,
      timeframe: "15m",
      open: 100,
      high: 101.1,
      low: 98.9,
      close: 100,
      volume: 10,
      timestamp: new Date(ts),
    };
  }

  // Steady decline: the grid buys the dips but the market never recovers to
  // target -> open positions bleed to the end.
  function declineCandle(symbol: string, ts: number, i: number): Candle {
    const open = 100 - i * 0.05;
    return {
      exchange: "bitget-futures",
      symbol,
      timeframe: "15m",
      open,
      high: open + 0.1,
      low: open - 0.1,
      close: open - 0.05,
      volume: 10,
      timestamp: new Date(ts),
    };
  }

  it("rejects a series whose edge lives only in one half (BTC-15m case)", () => {
    const candles: Candle[] = [];
    const end = Date.now() - BAR_MS;
    for (let i = 0; i < 400; i += 1) {
      candles.push(wickCandle("T/USDT:USDT", end - (499 - i) * BAR_MS));
    }
    // Last 20%: steady decline (grid long positions bleed).
    for (let i = 400; i < 500; i += 1) {
      candles.push(declineCandle("T/USDT:USDT", end - (499 - i) * BAR_MS, i - 400));
    }
    // First half (wicks) is clearly positive; the split must FAIL.
    expect(passesTimeSplitGate(candles, grid)).toBe(false);
  });

  it("accepts a series profitable across both halves", () => {
    const candles: Candle[] = [];
    const end = Date.now() - BAR_MS;
    for (let i = 0; i < 500; i += 1) {
      candles.push(wickCandle("T/USDT:USDT", end - (499 - i) * BAR_MS));
    }
    expect(passesTimeSplitGate(candles, grid)).toBe(true);
  });
});

describe("runMarketUniverseScan (market-sourced batch)", () => {
  const SCAN_OPTIONS: GridUniverseOptions = {
    exchange: "bitget-futures",
    // Tiny deep-fetch budget so the pacing (250ms/request) keeps tests fast;
    // production defaults to 300 requests/cycle.
    deepFetchBudgetPerCycle: 5,
    timeframe: "15m",
    initialCapital: 10000,
    minCandles: 500,
    trainWindow: 180,
    testWindow: 60,
    minProfitableWindowsPct: 60,
    minAggregateReturnPct: 0,
    minFillFrequencyPct: 10,
    feePct: 0.06,
    slippageBps: 2,
    trendFilterPeriod: 0,
    searchSpace: DEFAULT_GRID_UNIVERSE_SEARCH_SPACE,
  };

  const BAR_MS = 15 * 60 * 1000;

  /** Trending candle series: steady rise + small noise -> ADX above the
   * stage-2 floor, so the cheap screen does not skip them. */
  function trendingCandle(symbol: string, ts: number, i: number): Candle {
    const base = 100 + i * 0.05 + Math.sin(i / 11) * 0.4;
    return {
      exchange: "bitget-futures",
      symbol,
      timeframe: "15m",
      open: base,
      high: base + 0.6,
      low: base - 0.3,
      close: base + 0.2,
      volume: 100,
      timestamp: new Date(ts),
    };
  }

  function flatCandle(symbol: string, ts: number): Candle {
    return {
      exchange: "bitget-futures",
      symbol,
      timeframe: "15m",
      open: 100,
      high: 100,
      low: 100,
      close: 100,
      volume: 100,
      timestamp: new Date(ts),
    };
  }

  function makeGateway(
    behavior: {
      symbols: string[];
      volumes: Record<string, number>;
      fetchCalls: Map<string, number>;
      fail?: { symbol: string; reason: string };
      flatSymbols?: Set<string>;
    },
  ) {
    return Layer.succeed(MarketDataGateway, {
      fetchTick: () => Effect.die("unused"),
      fetchOHLCV: (_ex, symbol, _tf, limit, startTime) => {
        behavior.fetchCalls.set(
          symbol,
          (behavior.fetchCalls.get(symbol) ?? 0) + 1,
        );
        if (
          behavior.fail &&
          behavior.fail.symbol === symbol &&
          behavior.fetchCalls.get(symbol) === 1
        ) {
          return Effect.fail(new MarketDataError(behavior.fail.reason));
        }
        const effective = Math.min(limit, 200);
        const end = startTime === undefined ? Date.now() : startTime.getTime();
        return Effect.succeed(
          Array.from({ length: effective }, (_, i) => {
            const ts = end - (effective - 1 - i) * BAR_MS;
            return behavior.flatSymbols?.has(symbol)
              ? flatCandle(symbol, ts)
              : trendingCandle(symbol, ts, i);
          }),
        );
      },
      fetchOrderBook: () => Effect.die("unused"),
      fetchSymbols: () => Effect.succeed(behavior.symbols),
      fetch24hrVolumes: () => Effect.succeed(behavior.volumes),
      fetchFundingRates: () => Effect.die("unused"),
    });
  }

  function makeRepo() {
    const candlesByKey = new Map<string, Candle[]>();
    const repo: MarketDataRepositoryService = {
      saveTick: () => Effect.die("unused"),
      saveCandles: (candles) =>
        Effect.sync(() => {
          const first = candles[0];
          if (first === undefined) return 0;
          const key = `${first.symbol}:${first.timeframe}`;
          const merged = new Map<number, Candle>();
          for (const c of candlesByKey.get(key) ?? []) {
            merged.set(c.timestamp.getTime(), c);
          }
          let added = 0;
          for (const c of candles) {
            if (!merged.has(c.timestamp.getTime())) {
              merged.set(c.timestamp.getTime(), c);
              added += 1;
            }
          }
          candlesByKey.set(
            key,
            [...merged.values()].sort(
              (a, b) => a.timestamp.getTime() - b.timestamp.getTime(),
            ),
          );
          return added;
        }),
      getCandles: (query) =>
        Effect.sync(() => {
          const all = candlesByKey.get(`${query.symbol}:${query.timeframe}`) ?? [];
          return query.limit === undefined
            ? all
            : all.slice(-query.limit);
        }),
      getLatestTick: () => Effect.succeed(null),
      listSymbols: () => Effect.die("unused"),
      listSymbolsByCandleCount: () => Effect.die("unused"),
      deleteCandles: () => Effect.die("unused"),
      getCandleRange: (_ex, symbol, timeframe) =>
        Effect.sync(() => {
          const all = candlesByKey.get(`${symbol}:${timeframe}`) ?? [];
          return {
            earliest: all[0]?.timestamp ?? null,
            latest: all.at(-1)?.timestamp ?? null,
            count: all.length,
          };
        }),
      getCoverageReport: () => Effect.die("unused"),
      ensureTables: () => Effect.die("unused"),
      ensureFundingRatesTable: () => Effect.die("unused"),
      saveFundingRates: () => Effect.die("unused"),
      getFundingRates: () => Effect.die("unused"),
      getLatestFundingRateBefore: () => Effect.die("unused"),
    };
    return { repo, candlesByKey };
  }

  function runScan(
    gateway: ReturnType<typeof makeGateway>,
    repo: ReturnType<typeof makeRepo>,
    options: GridUniverseOptions = SCAN_OPTIONS,
  ) {
    return Effect.runPromise(
      Effect.provide(
        Effect.provide(
          runMarketUniverseScan(options),
          Layer.succeed(MarketDataRepository, repo.repo),
        ),
        gateway,
      ),
    );
  }

  it("digests the volume-filtered market batch, caching every fetched candle", async () => {
    const fetchCalls = new Map<string, number>();
    const gateway = makeGateway({
      symbols: ["ALPHA/USDT", "BETA/USDT", "GAMMA/USDT"],
      volumes: {
        ALPHAUSDT: 5_000_000,
        BETAUSDT: 500_000,
        GAMMAUSDT: 2_000_000,
      },
      fetchCalls,
    });
    const { repo, candlesByKey } = makeRepo();

    const result = await runScan(gateway, { repo, candlesByKey });

    // BETA is below the 1M USDT volume floor: never fetched, never cached.
    expect(fetchCalls.has("BETA/USDT:USDT")).toBe(false);
    expect(fetchCalls.get("ALPHA/USDT:USDT")).toBeGreaterThanOrEqual(1);
    // Both volume-passing symbols were evaluated (entries = evaluated, not
    // only survivors).
    expect(result.entries.map((e) => e.symbol).sort()).toEqual([
      "ALPHA/USDT:USDT",
      "GAMMA/USDT:USDT",
    ]);
    // The candle cache persisted the fetched history (>= minCandles).
    expect(candlesByKey.get("ALPHA/USDT:USDT:15m")?.length ?? 0).toBeGreaterThanOrEqual(500);
    expect(candlesByKey.get("GAMMA/USDT:USDT:15m")?.length ?? 0).toBeGreaterThanOrEqual(500);
    expect(candlesByKey.has("BETA/USDT:USDT")).toBe(false);
    // Entries carry the funnel metrics.
    for (const entry of result.entries) {
      expect(entry.volatility).toBeGreaterThan(0);
      expect(entry.oosTrades ?? 0).toBeGreaterThan(0);
      expect(entry.fillsPerDay ?? 0).toBeGreaterThan(0);
    }
  });

  it("fetches only the tail for symbols already in the cache (1 req/cycle)", async () => {
    const fetchCalls = new Map<string, number>();
    const gateway = makeGateway({
      symbols: ["ALPHA/USDT"],
      volumes: { ALPHAUSDT: 5_000_000 },
      fetchCalls,
    });
    const { repo, candlesByKey } = makeRepo();
    const now = Date.now();
    const seeded = Array.from({ length: 500 }, (_, i) =>
      trendingCandle("ALPHA/USDT:USDT", now - (500 - i) * BAR_MS, i),
    );
    candlesByKey.set("ALPHA/USDT:USDT:15m", seeded);

    await runScan(gateway, { repo, candlesByKey });

    // Warm cache: 1 tail request + the deep-fetch budget (5) for the
    // walk-forward passer = 6 — no full-history pagination (~275 requests).
    expect(fetchCalls.get("ALPHA/USDT:USDT")).toBe(6);
    expect(candlesByKey.get("ALPHA/USDT:USDT:15m")?.length ?? 0).toBeGreaterThanOrEqual(500);
  });

  it("caches before screening and skips chop symbols (stage-2)", async () => {
    const fetchCalls = new Map<string, number>();
    const gateway = makeGateway({
      symbols: ["TREND/USDT", "FLAT/USDT"],
      volumes: { TRENDUSDT: 5_000_000, FLATUSDT: 5_000_000 },
      fetchCalls,
      flatSymbols: new Set(["FLAT/USDT:USDT"]),
    });
    const { repo, candlesByKey } = makeRepo();

    const result = await runScan(gateway, { repo, candlesByKey });

    const evaluated = result.entries.map((e) => e.symbol);
    expect(evaluated).toContain("TREND/USDT:USDT");
    expect(evaluated).not.toContain("FLAT/USDT:USDT");
    // The flat symbol was still cached (stage 2 runs after the cache fill).
    expect((candlesByKey.get("FLAT/USDT:USDT:15m")?.length ?? 0)).toBeGreaterThanOrEqual(500);
  });

  it("retries transient failures (429) and completes the batch", async () => {
    const fetchCalls = new Map<string, number>();
    const gateway = makeGateway({
      symbols: ["ALPHA/USDT"],
      volumes: { ALPHAUSDT: 5_000_000 },
      fetchCalls,
      fail: { symbol: "ALPHA/USDT:USDT", reason: "Bitget HTTP 429 for /api/v2" },
    });
    const { repo, candlesByKey } = makeRepo();

    const result = await runScan(gateway, { repo, candlesByKey });

    expect(fetchCalls.get("ALPHA/USDT:USDT") ?? 0).toBeGreaterThanOrEqual(2);
    expect(result.entries.map((e) => e.symbol)).toContain("ALPHA/USDT:USDT");
  }, 30_000);

  it("propagates non-transient failures (a broken scan never persists)", async () => {
    const fetchCalls = new Map<string, number>();
    const gateway = makeGateway({
      symbols: ["ALPHA/USDT"],
      volumes: { ALPHAUSDT: 5_000_000 },
      fetchCalls,
      fail: { symbol: "ALPHA/USDT:USDT", reason: "Bitget HTTP 40053 invalid" },
    });
    const { repo, candlesByKey } = makeRepo();

    await expect(runScan(gateway, { repo, candlesByKey })).rejects.toThrow();
  });
});
