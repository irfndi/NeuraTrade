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
import {
  KillSwitch,
  type KillSwitchService,
} from "../risk/kill-switch.js";
import {
  CircuitBreaker,
  type CircuitBreakerService,
} from "../risk/circuit-breaker.js";

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
    return Effect.succeed({ capital: 20, peakCapital: 20 });
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
    return Effect.succeed(0);
  }

  getStartOfDayCapital(_date: Date, currentCapital: number) {
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
        filledQty: 0,
        filledPrice: 1000,
        fee: 0,
        timestamp: new Date(),
      }),
    closePosition: () => Effect.succeed(null),
    getPosition: () => Effect.succeed(null),
    getBalance: () =>
      Effect.succeed({
        marginCoin: "USDT",
        available: 10_000,
        locked: 0,
        equity: 10_000,
        usdtEquity: 10_000,
      }),
    setLeverage: () => Effect.void,
    setMarginMode: () => Effect.void,
    setPositionMode: () => Effect.void,
  };
}

function makeTrackingFuturesAdapter(): {
  adapter: FuturesExchangeAdapterService;
  orders: { side: string; size: number }[];
  closes: { side: string; size: number }[];
} {
  const orders: { side: string; size: number }[] = [];
  const closes: { side: string; size: number }[] = [];
  const adapter: FuturesExchangeAdapterService = {
    placeOrder: (req) =>
      Effect.sync(() => {
        orders.push({ side: req.side, size: req.size });
        return {
          orderId: "live",
          symbol: req.symbol,
          side: req.side,
          productType: req.productType,
          marginMode: req.marginMode,
          filledQty: req.size,
          filledPrice: 1000,
          fee: 0,
          timestamp: new Date(),
        };
      }),
    closePosition: (req) =>
      Effect.sync(() => {
        closes.push({ side: req.side, size: req.size });
        return {
          orderId: "close",
          symbol: req.symbol,
          side: req.side,
          productType: req.productType,
          marginMode: req.marginMode,
          filledQty: req.size,
          filledPrice: 1000,
          fee: 0,
          timestamp: new Date(),
        };
      }),
    getPosition: () => Effect.succeed(null),
    getBalance: () =>
      Effect.succeed({
        marginCoin: "USDT",
        available: 10_000,
        locked: 0,
        equity: 10_000,
        usdtEquity: 10_000,
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
  });
});
