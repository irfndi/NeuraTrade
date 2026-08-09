import { describe, expect, it } from "bun:test";
import { Effect, Layer } from "effect";
import {
  MarketDataGateway,
  type MarketDataGatewayService,
} from "../market-data/gateway.js";
import type { Candle } from "../market-data/types.js";
import {
  PaperTradingRepository,
  type PaperTradingRepositoryService,
} from "./repository.js";
import type { GridPaperState, GridPaperTrade } from "./types.js";
import {
  runGridPaperTradingIteration,
  type GridPaperTradingOptions,
} from "./grid-engine.js";
import {
  FuturesExchangeAdapter,
  type FuturesExchangeAdapterService,
  type FuturesPosition,
} from "../exchange/futures-adapter.js";
import { RiskError, RiskGuard, type RiskGuardService } from "../risk/guards.js";
import { KillSwitch, type KillSwitchService } from "../risk/kill-switch.js";
import {
  CircuitBreaker,
  type CircuitBreakerService,
} from "../risk/circuit-breaker.js";
import { money } from "../utils/money.js";
import { ExchangeError } from "../exchange/adapter.js";
import {
  DEFAULT_STRATEGY_MANIFEST,
  fingerprintStrategyManifest,
} from "../scalping/real-money-readiness.js";

function makeCandles(
  count: number,
  baseClose = 1000,
  pattern: "oscillate" | "trendUp" = "oscillate",
): Candle[] {
  const candles: Candle[] = [];
  let close = baseClose;
  for (let i = 0; i < count; i++) {
    const open = close;
    if (pattern === "oscillate") {
      close *= i % 2 === 0 ? 1.012 : 0.988;
    } else {
      close *= 1.002;
    }
    const high = Math.max(open, close) * 1.005;
    const low = Math.min(open, close) * 0.995;
    candles.push({
      exchange: "binance",
      symbol: "ETH/USDT",
      timeframe: "15m",
      open,
      high,
      low,
      close,
      volume: 10,
      timestamp: new Date(Date.now() - (count - i) * 900_000),
    });
  }
  return candles;
}

class InMemoryPaperRepository implements PaperTradingRepositoryService {
  private gridState: GridPaperState | null = null;
  private gridTrades: GridPaperTrade[] = [];

  ensureTables() {
    return Effect.void;
  }

  resetGridState() {
    this.gridState = null;
    return Effect.void;
  }

  getOpenPosition() {
    return Effect.succeed(null);
  }

  saveOpenPosition() {
    return Effect.void;
  }

  closePosition() {
    return Effect.succeed({} as never);
  }

  scaleOutPosition() {
    return Effect.succeed({} as never);
  }

  getPortfolio() {
    return Effect.succeed({ capital: money(20), peakCapital: money(20) });
  }

  setPortfolio() {
    return Effect.void;
  }

  listRecentTrades() {
    return Effect.succeed([]);
  }

  countTradesForDate() {
    return Effect.succeed(this.gridTrades.length);
  }

  getTodayRealizedPnl() {
    return Effect.succeed(money(0));
  }

  getStartOfDayCapital(_date: Date, currentCapital: ReturnType<typeof money>) {
    return Effect.succeed(currentCapital);
  }

  getGridState(exchange: string, symbol: string, timeframe: string) {
    return Effect.succeed(
      this.gridState &&
        this.gridState.exchange === exchange &&
        this.gridState.symbol === symbol &&
        this.gridState.timeframe === timeframe
        ? this.gridState
        : null,
    );
  }

  saveGridState(state: GridPaperState) {
    return Effect.sync(() => {
      this.gridState = state;
    });
  }

  recordGridTrade(trade: GridPaperTrade) {
    return Effect.sync(() => {
      this.gridTrades.push(trade);
    });
  }

  listRecentGridTrades(
    _exchange: string,
    _symbol: string,
    _timeframe: string,
    limit: number,
  ) {
    return Effect.succeed(
      this.gridTrades.slice(-limit).reverse() as GridPaperTrade[],
    );
  }

  listWatchlist() {
    return Effect.succeed([]);
  }

  upsertWatchlist() {
    return Effect.void;
  }

  clearWatchlist() {
    return Effect.void;
  }

  replaceWatchlist() {
    return Effect.void;
  }

  listAllGridTrades(_exchange: string, _timeframe: string, limit: number) {
    return Effect.succeed(
      this.gridTrades.slice(-limit).reverse() as GridPaperTrade[],
    );
  }
}

function makeGateway(candles: Candle[]): MarketDataGatewayService {
  return {
    fetchTick: () => Effect.fail({ reason: "not used" } as never),
    fetchOHLCV: () => Effect.succeed(candles),
    fetchOrderBook: () =>
      Effect.succeed({
        exchange: "binance",
        symbol: "ETH/USDT",
        bids: [{ price: 999, volume: 1 }],
        asks: [{ price: 1001, volume: 1 }],
        timestamp: new Date(),
      }),
    fetchSymbols: () => Effect.fail({ reason: "not used" } as never),
    fetch24hrVolumes: () => Effect.succeed({}),
    fetchFundingRates: () => Effect.succeed([]),
  };
}

function makeFuturesAdapter(): FuturesExchangeAdapterService {
  return {
    placeOrder: () =>
      Effect.succeed({
        orderId: "simulated",
        symbol: "ETHUSDT",
        side: "buy",
        productType: "USDT-FUTURES",
        marginMode: "isolated",
        filledQty: money(0),
        filledPrice: money(1000),
        fee: money(0),
        timestamp: new Date(),
      }),
    closePosition: () => Effect.succeed(null),
    getPosition: () => Effect.succeed(null),
    getBalance: () =>
      Effect.succeed({
        marginCoin: "USDT",
        available: money(10_000),
        locked: money(0),
        equity: money(10_000),
        usdtEquity: money(10_000),
      }),
    setLeverage: () => Effect.void,
    setMarginMode: () => Effect.void,
    setPositionMode: () => Effect.void,
  };
}

function makeTrackingFuturesAdapter(
  closeWithFill = true,
  initialPosition: FuturesPosition | null = null,
): {
  adapter: FuturesExchangeAdapterService;
  orders: {
    side: string;
    size: number;
    type: string;
    price: number | null;
  }[];
  closes: { side: string; size: number }[];
} {
  const orders: {
    side: string;
    size: number;
    type: string;
    price: number | null;
  }[] = [];
  const closes: { side: string; size: number }[] = [];
  let position = initialPosition;
  const adapter: FuturesExchangeAdapterService = {
    placeOrder: (req) =>
      Effect.sync(() => {
        orders.push({
          side: req.side,
          size: req.size.toNumber(),
          type: req.type,
          price: req.price?.toNumber() ?? null,
        });
        const fill = {
          orderId: "live",
          symbol: req.symbol,
          side: req.side,
          productType: req.productType,
          marginMode: req.marginMode,
          filledQty: req.size,
          filledPrice: money(1000),
          fee: money(0),
          timestamp: new Date(),
        };
        position = {
          symbol: req.symbol,
          side: req.side === "buy" ? "long" : "short",
          productType: req.productType,
          marginMode: req.marginMode,
          leverage: req.leverage,
          quantity: req.size,
          available: req.size,
          entryPrice: fill.filledPrice,
          marginCoin: "USDT",
        };
        return fill;
      }),
    closePosition: (req) =>
      Effect.sync(() => {
        closes.push({ side: req.side, size: req.size.toNumber() });
        if (!closeWithFill) return null;
        const fill = {
          orderId: "close",
          symbol: req.symbol,
          side: req.side,
          productType: req.productType,
          marginMode: req.marginMode,
          filledQty: req.size,
          filledPrice: money(1000),
          fee: money(0),
          timestamp: new Date(),
        };
        position = null;
        return fill;
      }),
    getPosition: () => Effect.succeed(position),
    getBalance: () =>
      Effect.succeed({
        marginCoin: "USDT",
        available: money(10_000),
        locked: money(0),
        equity: money(10_000),
        usdtEquity: money(10_000),
      }),
    setLeverage: () => Effect.void,
    setMarginMode: () => Effect.void,
    setPositionMode: () => Effect.void,
  };
  return { adapter, orders, closes };
}

function makeRiskGuard(): RiskGuardService {
  return {
    check: () => Effect.void,
  };
}

function makeKillSwitch(): KillSwitchService {
  return {
    isEngaged: () => Effect.succeed(false),
    getReason: () => Effect.succeed(""),
    engage: () => Effect.void,
    disengage: () => Effect.void,
  };
}

function makeCircuitBreaker(records: number[] = []): CircuitBreakerService {
  return {
    isOpen: () => Effect.succeed(false),
    getReason: () => Effect.succeed(""),
    currentDailyLossPct: () => Effect.succeed(0),
    recordTradeResult: (realizedPnl) =>
      Effect.sync(() => {
        records.push(realizedPnl);
      }),
    reset: () => Effect.void,
  };
}

function makeOptions(
  overrides: Partial<GridPaperTradingOptions> = {},
): GridPaperTradingOptions {
  return {
    exchange: "binance",
    symbol: "ETH/USDT",
    timeframe: "15m",
    gridStepPct: 1.0,
    gridMaxGrids: 2,
    gridPauseAfterLossBars: 0,
    feePct: 0.2,
    slippageBps: 5,
    trendFilterPeriod: 10,
    initialCapital: 20,
    maxPositionPct: 100,
    maxDrawdownPct: 100,
    ...overrides,
  } as GridPaperTradingOptions;
}

function liveTestFingerprint(options: GridPaperTradingOptions): string {
  return fingerprintStrategyManifest({
    ...DEFAULT_STRATEGY_MANIFEST,
    exchange: "bitget-live",
    symbol: options.symbol,
    timeframe: options.timeframe,
    gridStepPct: options.gridStepPct.toString(),
    gridMaxGrids: options.gridMaxGrids.toString(),
    gridPauseAfterLossBars: options.gridPauseAfterLossBars.toString(),
    positionFraction: (options.maxPositionPct / 100).toString(),
    feePct: options.feePct.toString(),
    slippageBps: options.slippageBps.toString(),
    trendFilterPeriod: options.trendFilterPeriod.toString(),
    adxGate: (options.chopGateAdxThreshold ?? 0).toString(),
  });
}

function runWithRepo(
  options: GridPaperTradingOptions,
  repo: PaperTradingRepositoryService,
  candles: Candle[],
  adapter?: FuturesExchangeAdapterService,
  riskGuard: RiskGuardService = makeRiskGuard(),
  circuitBreaker: CircuitBreakerService = makeCircuitBreaker(),
  killSwitch: KillSwitchService = makeKillSwitch(),
) {
  return runGridPaperTradingIteration(options).pipe(
    Effect.provide(
      Layer.mergeAll(
        Layer.succeed(MarketDataGateway, makeGateway(candles)),
        Layer.succeed(PaperTradingRepository, repo),
        Layer.succeed(FuturesExchangeAdapter, adapter ?? makeFuturesAdapter()),
        Layer.succeed(RiskGuard, riskGuard),
        Layer.succeed(KillSwitch, killSwitch),
        Layer.succeed(CircuitBreaker, circuitBreaker),
      ),
    ),
    Effect.runPromise,
  );
}

describe("grid paper engine", () => {
  it("opens a grid position on an oscillating bar", async () => {
    const repo = new InMemoryPaperRepository();
    const candles = makeCandles(20, 1000, "oscillate");
    const result = await runWithRepo(makeOptions(), repo, candles);
    expect(result.action).toBeOneOf(["opened", "closed"]);
  });

  it("records a profitable target exit on an oscillating series", async () => {
    const repo = new InMemoryPaperRepository();
    const candles = makeCandles(20, 1000, "oscillate");
    const opts = makeOptions({ gridPauseAfterLossBars: 0 });
    let closed = 0;
    for (let i = 0; i < 10; i++) {
      const result = await runWithRepo(opts, repo, candles);
      if (result.action === "closed") closed++;
    }
    expect(closed).toBeGreaterThan(0);
    const trades = await Effect.runPromise(
      repo.listRecentGridTrades("binance", "ETH/USDT", "15m", 100),
    );
    expect(trades.length).toBeGreaterThan(0);
  });

  it("replays successive candles when replayBars is set", async () => {
    const repo = new InMemoryPaperRepository();
    const candles = makeCandles(50, 1000, "oscillate");
    const opts = makeOptions({ replayBars: 10, gridPauseAfterLossBars: 0 });

    const results: string[] = [];
    for (let i = 0; i < 12; i++) {
      const result = await runWithRepo(opts, repo, candles);
      results.push(result.note);
    }
    // After replayBars bars are exhausted, the last iterations should report
    // no new replay candle.
    expect(results.at(-1)).toContain("no new replay candle");
  });

  it("places a live futures order when isLive is true and entry triggers", async () => {
    const repo = new InMemoryPaperRepository();
    const { adapter, orders } = makeTrackingFuturesAdapter();
    const candles = makeCandles(20, 1000, "oscillate");
    const opts = makeOptions({
      isLive: true,
      productType: "USDT-FUTURES",
      marginMode: "isolated",
      leverage: 1,
    });
    const result = await runWithRepo(opts, repo, candles, adapter);
    expect(result.note).toContain("[LIVE]");
    expect(orders.length).toBeGreaterThan(0);
    expect(orders[0].size).toBeGreaterThan(0);
    const state = await Effect.runPromise(
      repo.getGridState("binance", "ETH/USDT", "15m"),
    );
    expect(state?.entryFillSource).toBe("live");
    expect(state?.entryOrderId).toBe("live");
    expect(state?.entryFilledQty?.toNumber()).toBeCloseTo(orders[0].size, 12);
  });

  it("places live entries as LIMIT orders at the grid level (maker parity)", async () => {
    const repo = new InMemoryPaperRepository();
    const { adapter, orders } = makeTrackingFuturesAdapter();
    const candles = makeCandles(20, 1000, "oscillate");
    const opts = makeOptions({
      isLive: true,
      productType: "USDT-FUTURES",
      marginMode: "isolated",
      leverage: 1,
    });
    const result = await runWithRepo(opts, repo, candles, adapter);
    expect(result.note).toContain("[LIVE]");
    expect(orders.length).toBeGreaterThan(0);
    // Entries must rest as limit orders (maker), not market orders, so the
    // deployed path matches the validated backtest's fill-at-grid-level model.
    expect(orders[0].type).toBe("limit");
    expect(orders[0].price).not.toBeNull();
    // The limit price must be the raw grid level (mid - gridStepPct%). In
    // live shadow mode the engine evaluates only the latest candle, so the
    // expected level is the last bar's open minus the step (the oscillating
    // series crosses it), not a market/slippage-adjusted price.
    const lastBar = candles.at(-1)!;
    const expectedLevel = lastBar.open * (1 - opts.gridStepPct / 100);
    expect(lastBar.low).toBeLessThanOrEqual(expectedLevel);
    expect(orders[0].price).toBeCloseTo(expectedLevel, 6);
  });

  it("refuses to resume a legacy open state without provenance", async () => {
    const repo = new InMemoryPaperRepository();
    await Effect.runPromise(
      repo.saveGridState({
        exchange: "binance",
        symbol: "ETH/USDT",
        timeframe: "15m",
        capital: money(20),
        peakCapital: money(20),
        paused: 0,
        side: "long",
        entryPrice: money(1000),
        gridStepPct: 1,
        gridMaxGrids: 2,
        gridPauseAfterLossBars: 0,
        feePct: 0.2,
        slippageBps: 5,
        trendFilterPeriod: 10,
        maxPositionPct: 100,
        maxDrawdownPct: 100,
        leverage: 1,
        killed: false,
        lastTimestamp: null,
        updatedAt: new Date(),
      } satisfies GridPaperState),
    );
    const result = await runWithRepo(
      makeOptions({ isLive: true }),
      repo,
      makeCandles(20, 1000, "oscillate"),
    );

    expect(result.action).toBe("hold");
    expect(result.note).toContain("READINESS PROVENANCE MISMATCH");
  });

  it("re-seeds a flat state whose persisted config differs from the options", async () => {
    const repo = new InMemoryPaperRepository();
    await Effect.runPromise(
      repo.saveGridState({
        exchange: "binance",
        symbol: "ETH/USDT",
        timeframe: "15m",
        capital: money(10_000),
        peakCapital: money(10_000),
        paused: 0,
        side: null,
        entryPrice: money(0),
        gridStepPct: 1,
        gridMaxGrids: 1.5,
        gridPauseAfterLossBars: 12,
        feePct: 0.02,
        slippageBps: 1,
        trendFilterPeriod: 200,
        maxPositionPct: 100,
        maxDrawdownPct: 100,
        leverage: 1,
        killed: false,
        lastTimestamp: null,
        updatedAt: new Date(),
      } satisfies GridPaperState),
    );
    const opts = makeOptions({
      initialCapital: 20,
      maxPositionPct: 50,
      maxDrawdownPct: 5,
      gridMaxGrids: 1,
      gridPauseAfterLossBars: 12,
      trendFilterPeriod: 0,
      feePct: 0.06,
      slippageBps: 2,
    });
    await runWithRepo(opts, repo, makeCandles(20, 1000, "oscillate"));

    const state = await Effect.runPromise(
      repo.getGridState("binance", "ETH/USDT", "15m"),
    );
    expect(state).not.toBeNull();
    expect(state?.capital.toNumber()).toBe(20);
    expect(state?.peakCapital.toNumber()).toBe(20);
    expect(state?.maxPositionPct).toBe(50);
    expect(state?.maxDrawdownPct).toBe(5);
    expect(state?.gridMaxGrids).toBe(1);
    expect(state?.trendFilterPeriod).toBe(0);
  });

  it("keeps a flat state whose persisted config matches the options", async () => {
    const repo = new InMemoryPaperRepository();
    await Effect.runPromise(
      repo.saveGridState({
        exchange: "binance",
        symbol: "ETH/USDT",
        timeframe: "15m",
        capital: money(333),
        peakCapital: money(400),
        paused: 0,
        side: null,
        entryPrice: money(0),
        gridStepPct: 1,
        gridMaxGrids: 2,
        gridPauseAfterLossBars: 0,
        feePct: 0.2,
        slippageBps: 5,
        trendFilterPeriod: 10,
        maxPositionPct: 100,
        maxDrawdownPct: 100,
        leverage: 1,
        killed: false,
        lastTimestamp: null,
        updatedAt: new Date(),
      } satisfies GridPaperState),
    );
    await runWithRepo(makeOptions(), repo, makeCandles(20, 1000, "oscillate"));

    const state = await Effect.runPromise(
      repo.getGridState("binance", "ETH/USDT", "15m"),
    );
    expect(state?.capital.toNumber()).toBe(333);
    expect(state?.maxPositionPct).toBe(100);
  });

  it("records both live order fills when a live grid trade closes", async () => {
    const repo = new InMemoryPaperRepository();
    const { adapter, closes } = makeTrackingFuturesAdapter();
    const candles = makeCandles(20, 1000, "oscillate");
    const opts = makeOptions({ isLive: true, gridPauseAfterLossBars: 0 });

    for (let i = 0; i < 10; i++) {
      await runWithRepo(opts, repo, candles, adapter);
    }

    const trades = await Effect.runPromise(
      repo.listRecentGridTrades("binance", "ETH/USDT", "15m", 100),
    );
    expect(closes.length).toBeGreaterThan(0);
    expect(trades.length).toBeGreaterThan(0);
    expect(trades[0]?.fillSource).toBe("live");
    expect(trades[0]?.entryOrderId).toBe("live");
    expect(trades[0]?.exitOrderId).toBe("close");
    expect(trades[0]?.realizedPnlPct).toBeDefined();
  });

  it("applies the daily trade-count risk gate to live grid entries", async () => {
    const repo = new InMemoryPaperRepository();
    await Effect.runPromise(
      repo.recordGridTrade({
        id: "risk-limit-trade",
        exchange: "binance",
        symbol: "ETH/USDT",
        timeframe: "15m",
        side: "long",
        entryPrice: money(1000),
        exitPrice: money(1010),
        capitalBefore: money(20),
        capitalAfter: money(20.1),
        pnlPct: money(0.5),
        exitReason: "target",
        openedAt: new Date(Date.now() - 3_600_000),
        closedAt: new Date(),
      }),
    );
    const { adapter, orders } = makeTrackingFuturesAdapter();
    const tradeLimitGuard: RiskGuardService = {
      check: ({ tradesTodayCount }) =>
        tradesTodayCount > 0
          ? Effect.fail(
              new RiskError("trade limit reached", ["daily trade limit"]),
            )
          : Effect.void,
    };

    const result = await runWithRepo(
      makeOptions({ isLive: true }),
      repo,
      makeCandles(20, 1000, "oscillate"),
      adapter,
      tradeLimitGuard,
    );

    expect(result.note).toContain("RISK BLOCKED");
    expect(orders).toHaveLength(0);
  });

  it("records realized grid closes in the circuit breaker", async () => {
    const repo = new InMemoryPaperRepository();
    const { adapter } = makeTrackingFuturesAdapter();
    const records: number[] = [];
    const options = makeOptions({ isLive: true, gridPauseAfterLossBars: 0 });

    for (let iteration = 0; iteration < 10; iteration++) {
      await runWithRepo(
        options,
        repo,
        makeCandles(20, 1000, "oscillate"),
        adapter,
        makeRiskGuard(),
        makeCircuitBreaker(records),
      );
    }

    expect(records.length).toBeGreaterThan(0);
  });

  it("fails closed when the exchange has an untracked live position", async () => {
    const repo = new InMemoryPaperRepository();
    const { adapter, orders } = makeTrackingFuturesAdapter();
    const exchangePosition: FuturesPosition = {
      symbol: "ETH/USDT",
      side: "long",
      productType: "USDT-FUTURES",
      marginMode: "isolated",
      leverage: 1,
      quantity: money("0.01"),
      available: money("0.01"),
      entryPrice: money(1000),
      marginCoin: "USDT",
    };
    const mismatchAdapter: FuturesExchangeAdapterService = {
      ...adapter,
      getPosition: () => Effect.succeed(exchangePosition),
    };
    let killReason = "";
    const killSwitch: KillSwitchService = {
      isEngaged: () => Effect.succeed(false),
      getReason: () => Effect.succeed(killReason),
      engage: (reason) =>
        Effect.sync(() => {
          killReason = reason;
        }),
      disengage: () => Effect.void,
    };

    const result = await runWithRepo(
      makeOptions({ isLive: true }),
      repo,
      makeCandles(20, 1000, "oscillate"),
      mismatchAdapter,
      makeRiskGuard(),
      makeCircuitBreaker(),
      killSwitch,
    );

    expect(result.note).toContain("LIVE POSITION MISMATCH");
    expect(orders).toHaveLength(0);
    expect(killReason).toContain(
      "exchange position exists without local state",
    );
  });

  it("fails closed when persisted live state has no exchange position", async () => {
    const repo = new InMemoryPaperRepository();
    await Effect.runPromise(
      repo.saveGridState({
        exchange: "binance",
        symbol: "ETH/USDT",
        timeframe: "15m",
        capital: money(20),
        peakCapital: money(20),
        paused: 0,
        side: "long",
        entryPrice: money(1000),
        entryOrderId: "entry-live",
        entryFilledQty: money("0.02"),
        entryFee: money(0),
        entryFillSource: "live",
        strategyConfigFingerprint: liveTestFingerprint(
          makeOptions({ isLive: true }),
        ),
        cohortId: "test-live-cohort",
        candidateLockAt: new Date("2026-01-01T00:00:00.000Z"),
        datasetCutoffAt: new Date("2026-01-01T00:00:00.000Z"),
        entryOpenedAt: new Date("2026-01-01T00:15:00.000Z"),
        executionEnvironment: "bitget-live",
        gridStepPct: 1,
        gridMaxGrids: 2,
        gridPauseAfterLossBars: 0,
        feePct: 0.2,
        slippageBps: 5,
        trendFilterPeriod: 10,
        maxPositionPct: 100,
        maxDrawdownPct: 100,
        leverage: 1,
        killed: false,
        lastTimestamp: null,
        updatedAt: new Date(),
      } satisfies GridPaperState),
    );
    const { adapter, orders } = makeTrackingFuturesAdapter();
    let killReason = "";
    const killSwitch: KillSwitchService = {
      isEngaged: () => Effect.succeed(false),
      getReason: () => Effect.succeed(killReason),
      engage: (reason) =>
        Effect.sync(() => {
          killReason = reason;
        }),
      disengage: () => Effect.void,
    };

    const result = await runWithRepo(
      makeOptions({ isLive: true }),
      repo,
      makeCandles(20, 1000, "oscillate"),
      adapter,
      makeRiskGuard(),
      makeCircuitBreaker(),
      killSwitch,
    );

    expect(result.note).toContain("LIVE POSITION MISMATCH");
    expect(orders).toHaveLength(0);
    expect(killReason).toContain(
      "local state exists without exchange position",
    );
  });

  it("clears a stale killed flag once the reconciliation matches (transient mismatch)", async () => {
    // Regression 2026-08-09: a phantom exchange position set killed=true;
    // after the exchange position vanished, the sticky flag held the soak
    // forever ("kill switch active") with a clean state.
    const repo = new InMemoryPaperRepository();
    await Effect.runPromise(
      repo.saveGridState({
        exchange: "binance",
        symbol: "ETH/USDT",
        timeframe: "15m",
        capital: money(20),
        peakCapital: money(20),
        paused: 0,
        side: null,
        entryPrice: money(0),
        entryOrderId: undefined,
        entryFilledQty: undefined,
        entryFee: undefined,
        gridStepPct: 1,
        gridMaxGrids: 2,
        gridPauseAfterLossBars: 0,
        feePct: 0.2,
        slippageBps: 5,
        trendFilterPeriod: 10,
        maxPositionPct: 100,
        maxDrawdownPct: 100,
        leverage: 1,
        killed: true,
        lastTimestamp: null,
        updatedAt: new Date(),
      } satisfies GridPaperState),
    );
    const { adapter } = makeTrackingFuturesAdapter();
    const killSwitch: KillSwitchService = {
      isEngaged: () => Effect.succeed(false),
      getReason: () => Effect.succeed(""),
      engage: () => Effect.void,
      disengage: () => Effect.void,
    };

    const result = await runWithRepo(
      makeOptions({ isLive: true, gridPauseAfterLossBars: 0 }),
      repo,
      makeCandles(20, 1000, "oscillate"),
      adapter,
      makeRiskGuard(),
      makeCircuitBreaker(),
      killSwitch,
    );

    // A clean reconciliation (no local side, no exchange position) clears
    // the stale kill flag instead of holding forever.
    expect(result.note).not.toContain("kill switch active");
    expect(result.note).not.toContain("MISMATCH");
    const state = await Effect.runPromise(
      repo.getGridState("binance", "ETH/USDT", "15m"),
    );
    expect(state?.killed).toBe(false);
  });

  it("does not clear a live position when the close has no exchange fill", async () => {
    const repo = new InMemoryPaperRepository();
    const { adapter, closes } = makeTrackingFuturesAdapter(false);
    const candles = makeCandles(20, 1000, "oscillate");
    const opts = makeOptions({ isLive: true, gridPauseAfterLossBars: 0 });

    await runWithRepo(opts, repo, candles, adapter);
    let caught: unknown = null;
    for (let attempt = 0; attempt < 10 && caught === null; attempt++) {
      try {
        await runWithRepo(opts, repo, candles, adapter);
      } catch (error) {
        caught = error;
      }
    }

    expect(caught instanceof ExchangeError).toBe(true);
    expect(closes.length).toBeGreaterThan(0);
    const state = await Effect.runPromise(
      repo.getGridState("binance", "ETH/USDT", "15m"),
    );
    expect(state?.side).toBe("long");
    expect(
      await Effect.runPromise(
        repo.listRecentGridTrades("binance", "ETH/USDT", "15m", 100),
      ),
    ).toHaveLength(0);
  });

  it("closes a live position at liquidation before killing local state", async () => {
    const repo = new InMemoryPaperRepository();
    const { adapter, closes } = makeTrackingFuturesAdapter(true, {
      symbol: "ETH/USDT",
      side: "long",
      productType: "USDT-FUTURES",
      marginMode: "isolated",
      leverage: 10,
      quantity: money("0.01"),
      available: money("0.01"),
      entryPrice: money(1000),
      marginCoin: "USDT",
    });
    const baseCandles = makeCandles(20, 1000, "oscillate");
    const lastCandle = baseCandles.at(-1);
    if (lastCandle === undefined) throw new Error("fixture is empty");
    const candles = [
      ...baseCandles.slice(0, -1),
      { ...lastCandle, high: 1001, low: 800, close: 1000 },
    ];
    const openedAt = new Date("2026-08-01T00:00:00.000Z");
    await Effect.runPromise(
      repo.saveGridState({
        exchange: "binance",
        symbol: "ETH/USDT",
        timeframe: "15m",
        capital: money(1000),
        peakCapital: money(1000),
        paused: 0,
        side: "long",
        entryPrice: money(1000),
        entryOrderId: "entry-live",
        entryFilledQty: money("0.01"),
        entryFee: money("0.01"),
        entryFillSource: "live",
        strategyConfigFingerprint: liveTestFingerprint(
          makeOptions({ isLive: true, leverage: 10 }),
        ),
        cohortId: "test-liquidation-cohort",
        candidateLockAt: new Date("2026-08-01T00:00:00.000Z"),
        datasetCutoffAt: new Date("2026-08-01T00:00:00.000Z"),
        entryOpenedAt: openedAt,
        executionEnvironment: "bitget-live",
        gridStepPct: 1,
        gridMaxGrids: 2,
        gridPauseAfterLossBars: 0,
        feePct: 0.2,
        slippageBps: 5,
        trendFilterPeriod: 10,
        maxPositionPct: 100,
        maxDrawdownPct: 100,
        leverage: 10,
        killed: false,
        lastTimestamp: null,
        updatedAt: openedAt,
      }),
    );

    const result = await runWithRepo(
      makeOptions({ isLive: true, leverage: 10 }),
      repo,
      candles,
      adapter,
    );

    expect(result.action).toBe("closed");
    expect(closes).toHaveLength(1);
    const state = await Effect.runPromise(
      repo.getGridState("binance", "ETH/USDT", "15m"),
    );
    expect(state?.side).toBeNull();
    expect(state?.killed).toBe(true);
    const trades = await Effect.runPromise(
      repo.listRecentGridTrades("binance", "ETH/USDT", "15m", 10),
    );
    expect(trades[0]?.exitReason).toBe("liquidation");
    expect(trades[0]?.fillSource).toBe("live");
  });

  it("chop gate blocks entries in a trending market and allows them in chop", async () => {
    const trendRepo = new InMemoryPaperRepository();
    const trendCandles = makeCandles(80, 1000, "trendUp");
    const gatedTrend = await runWithRepo(
      makeOptions({ chopGateAdxThreshold: 25 }),
      trendRepo,
      trendCandles,
    );
    expect(gatedTrend.action).toBe("hold");
    expect(gatedTrend.note).toContain("chop gate active");

    // Low-ADX chop: gentle sine oscillation (ADX ≈ 10, verified vs indicators).
    const chopCandles: Candle[] = [];
    for (let i = 0; i < 80; i++) {
      const close = 1000 * (1 + 0.008 * Math.sin((2 * Math.PI * i) / 8));
      const open = i === 0 ? close : chopCandles[i - 1].close;
      chopCandles.push({
        exchange: "binance",
        symbol: "ETH/USDT",
        timeframe: "15m",
        open,
        high: Math.max(open, close) * 1.0005,
        low: Math.min(open, close) * 0.9995,
        close,
        volume: 10,
        timestamp: new Date(Date.now() - (80 - i) * 900_000),
      });
    }
    const chopRepo = new InMemoryPaperRepository();
    let opens = 0;
    for (let k = 0; k < 10; k++) {
      const r = await runWithRepo(
        makeOptions({
          chopGateAdxThreshold: 25,
          gridStepPct: 0.5,
          replayBars: 8,
        }),
        chopRepo,
        chopCandles,
      );
      if (r.action === "opened") opens++;
    }
    expect(opens).toBeGreaterThan(0);
  });

  it("chop-gated iterations advance the replay pointer (no stall)", async () => {
    const repo = new InMemoryPaperRepository();
    const trendCandles = makeCandles(80, 1000, "trendUp");
    const opts = makeOptions({ chopGateAdxThreshold: 25, replayBars: 40 });

    await runWithRepo(opts, repo, trendCandles);
    const first = await Effect.runPromise(
      repo.getGridState("binance", "ETH/USDT", "15m"),
    );
    expect(first).not.toBeNull();
    const firstTs = first!.lastTimestamp?.getTime() ?? 0;
    expect(firstTs).toBeGreaterThan(0);

    await runWithRepo(opts, repo, trendCandles);
    const second = await Effect.runPromise(
      repo.getGridState("binance", "ETH/USDT", "15m"),
    );
    const secondTs = second!.lastTimestamp?.getTime() ?? 0;
    expect(secondTs).toBeGreaterThan(firstTs);
  });
});
