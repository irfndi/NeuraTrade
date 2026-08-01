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
} from "../exchange/futures-adapter.js";
import { RiskGuard, type RiskGuardService } from "../risk/guards.js";
import { KillSwitch, type KillSwitchService } from "../risk/kill-switch.js";
import {
  CircuitBreaker,
  type CircuitBreakerService,
} from "../risk/circuit-breaker.js";
import { money } from "../utils/money.js";
import { ExchangeError } from "../exchange/adapter.js";

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
    return Effect.succeed(0);
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

function makeTrackingFuturesAdapter(closeWithFill = true): {
  adapter: FuturesExchangeAdapterService;
  orders: { side: string; size: number }[];
  closes: { side: string; size: number }[];
} {
  const orders: { side: string; size: number }[] = [];
  const closes: { side: string; size: number }[] = [];
  const adapter: FuturesExchangeAdapterService = {
    placeOrder: (req) =>
      Effect.sync(() => {
        orders.push({ side: req.side, size: req.size.toNumber() });
        return {
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
      }),
    closePosition: (req) =>
      Effect.sync(() => {
        closes.push({ side: req.side, size: req.size.toNumber() });
        if (!closeWithFill) return null;
        return {
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
      }),
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

function makeCircuitBreaker(): CircuitBreakerService {
  return {
    isOpen: () => Effect.succeed(false),
    getReason: () => Effect.succeed(""),
    currentDailyLossPct: () => Effect.succeed(0),
    recordTradeResult: () => Effect.void,
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

function runWithRepo(
  options: GridPaperTradingOptions,
  repo: PaperTradingRepositoryService,
  candles: Candle[],
  adapter?: FuturesExchangeAdapterService,
) {
  return runGridPaperTradingIteration(options).pipe(
    Effect.provide(
      Layer.mergeAll(
        Layer.succeed(MarketDataGateway, makeGateway(candles)),
        Layer.succeed(PaperTradingRepository, repo),
        Layer.succeed(FuturesExchangeAdapter, adapter ?? makeFuturesAdapter()),
        Layer.succeed(RiskGuard, makeRiskGuard()),
        Layer.succeed(KillSwitch, makeKillSwitch()),
        Layer.succeed(CircuitBreaker, makeCircuitBreaker()),
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
    const { adapter, closes } = makeTrackingFuturesAdapter();
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
