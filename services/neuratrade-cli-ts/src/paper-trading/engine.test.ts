import { describe, expect, it } from "bun:test";
import { Effect, Layer } from "effect";
import {
  MarketDataGateway,
  type MarketDataGatewayService,
} from "../market-data/gateway.js";
import type { Candle, OrderBook } from "../market-data/types.js";
import { ExchangeAdapter, ExchangeError } from "../exchange/adapter.js";
import type { ExchangeAdapterService } from "../exchange/adapter.js";
import { makeSimulatedExchangeAdapter } from "../exchange/adapters/simulated.js";
import { RiskGuard, makeRiskGuard } from "../risk/guards.js";
import { KillSwitch, type KillSwitchService } from "../risk/kill-switch.js";
import {
  CircuitBreaker,
  type CircuitBreakerService,
} from "../risk/circuit-breaker.js";
import {
  PaperTradingRepository,
  type PaperTradingRepositoryService,
} from "./repository.js";
import type {
  GridPaperState,
  GridPaperTrade,
  PaperPosition,
  PaperTrade,
} from "./types.js";
import {
  runPaperTradingIteration,
  type PaperTradingOptions,
} from "./engine.js";
import { defaultComposerConfig } from "../scalping/composer.js";
import { ExitEngineLive, SignalComposerLive } from "../scalping/services.js";
import type { ComposerConfig } from "../scalping/types.js";
import { Decimal, money } from "../utils/money.js";

const scalpingServiceLayers = Layer.merge(SignalComposerLive, ExitEngineLive);

function makeCandles(
  count: number,
  baseClose = 100,
  trend: "up" | "down" | "flat" = "flat",
): Candle[] {
  const candles: Candle[] = [];
  let close = baseClose;
  for (let i = 0; i < count; i++) {
    const open = close;
    if (trend === "up") close *= 1.01;
    else if (trend === "down") close *= 0.99;
    const high = Math.max(open, close) * 1.002;
    const low = Math.min(open, close) * 0.998;
    candles.push({
      exchange: "binance",
      symbol: "BTC/USDT",
      timeframe: "1h",
      open,
      high,
      low,
      close,
      volume: 10,
      timestamp: new Date(Date.now() - (count - i) * 3_600_000),
    });
  }
  return candles;
}

function makeOrderBook(price: number): OrderBook {
  return {
    exchange: "binance",
    symbol: "BTC/USDT",
    bids: [{ price: price * 0.999, volume: 80 }],
    asks: [{ price: price * 1.001, volume: 20 }],
    timestamp: new Date(),
  };
}

function makeGatewayWithCandles(
  candles: Candle[],
  orderBook: OrderBook,
): MarketDataGatewayService {
  return {
    fetchTick: () => Effect.fail({ reason: "not used" } as never),
    fetchOHLCV: () => Effect.succeed(candles),
    fetchOrderBook: () => Effect.succeed(orderBook),
    fetchSymbols: () => Effect.fail({ reason: "not used" } as never),
    fetchDemoSymbols: () => Effect.fail({ reason: "not used" } as never),
    fetch24hrVolumes: () => Effect.succeed({}),
    fetchFundingRates: () => Effect.succeed([]),
  };
}

/** Flat candles with the LAST bar replaced so exit conditions are hit. */
function makeCandlesWithLastBar(
  last: Partial<Candle>,
  count = 30,
  baseClose = 100,
): Candle[] {
  const candles = makeCandles(count, baseClose, "flat");
  candles[candles.length - 1] = { ...candles[candles.length - 1], ...last };
  return candles;
}

class InMemoryPaperRepository implements PaperTradingRepositoryService {
  private capital: Decimal;
  private peakCapital: Decimal;
  private position: PaperPosition | null = null;
  private trades: PaperTrade[] = [];

  constructor(capital = 10_000, peakCapital = 10_000) {
    this.capital = money(capital);
    this.peakCapital = money(peakCapital);
  }

  seedPosition(position: PaperPosition | null) {
    this.position = position;
  }

  ensureTables() {
    return Effect.void;
  }

  resetGridState() {
    return Effect.void;
  }

  getOpenPosition(_exchange: string, _symbol: string) {
    return Effect.succeed(this.position);
  }

  saveOpenPosition(position: PaperPosition) {
    return Effect.sync(() => {
      this.position = position;
    });
  }

  closePosition(
    position: PaperPosition,
    exitPrice: Decimal,
    exitReason: PaperTrade["exitReason"],
    closedAt: Date,
  ) {
    return Effect.sync(() => {
      const priceDiff =
        position.side === "long"
          ? exitPrice.minus(position.entryPrice)
          : position.entryPrice.minus(exitPrice);
      const pnl = priceDiff.times(position.size);
      const pnlPct = pnl
        .div(position.entryPrice.times(position.size))
        .times(100);
      const trade: PaperTrade = {
        id: `paper-trade-${Date.now()}`,
        exchange: position.exchange,
        symbol: position.symbol,
        timeframe: position.timeframe,
        side: position.side,
        entryPrice: position.entryPrice,
        exitPrice,
        size: position.size,
        pnl,
        pnlPct,
        exitReason,
        openedAt: position.openedAt,
        closedAt,
      };
      this.trades.push(trade);
      this.position = null;
      return trade;
    });
  }

  scaleOutPosition(
    position: PaperPosition,
    exitPrice: Decimal,
    scaleOutPct: number,
    closedAt: Date,
  ) {
    return Effect.sync(() => {
      const pct = Math.max(0, Math.min(100, scaleOutPct));
      const partialSize = position.size.times(pct / 100);
      const remainingSize = position.size.minus(partialSize);
      const priceDiff =
        position.side === "long"
          ? exitPrice.minus(position.entryPrice)
          : position.entryPrice.minus(exitPrice);
      const pnl = priceDiff.times(partialSize);
      const pnlPct = pnl.div(position.entryPrice.times(partialSize)).times(100);
      const trade: PaperTrade = {
        id: `paper-trade-${Date.now()}`,
        exchange: position.exchange,
        symbol: position.symbol,
        timeframe: position.timeframe,
        side: position.side,
        entryPrice: position.entryPrice,
        exitPrice,
        size: partialSize,
        pnl,
        pnlPct,
        exitReason: "scale_out",
        openedAt: position.openedAt,
        closedAt,
      };
      this.trades.push(trade);
      const updatedPosition: PaperPosition = {
        ...position,
        size: remainingSize,
        stopLoss: position.entryPrice,
        scaledOut: true,
      };
      this.position = updatedPosition;
      return { trade, updatedPosition };
    });
  }

  getPortfolio() {
    return Effect.succeed({
      capital: this.capital,
      peakCapital: this.peakCapital,
    });
  }

  setPortfolio(capital: Decimal, peakCapital: Decimal) {
    return Effect.sync(() => {
      this.capital = capital;
      this.peakCapital = peakCapital;
    });
  }

  listRecentTrades(limit: number) {
    return Effect.succeed(this.trades.slice(0, limit));
  }

  countTradesForDate(_date: Date) {
    return Effect.succeed(this.trades.length);
  }

  getTodayRealizedPnl() {
    return Effect.succeed(
      this.trades.reduce((sum, t) => sum.plus(t.pnl), money(0)),
    );
  }

  getStartOfDayCapital(_date: Date, currentCapital: Decimal) {
    return Effect.succeed(currentCapital);
  }

  private gridState: GridPaperState | null = null;
  private gridTrades: GridPaperTrade[] = [];

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

  getLadderState() {
    return Effect.succeed(null);
  }

  saveLadderState() {
    return Effect.succeed(undefined);
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

  getFlowTradeState() {
    return Effect.succeed(null);
  }

  saveFlowTradeState() {
    return Effect.void;
  }

  clearFlowTradeState() {
    return Effect.void;
  }

  getOpenInterest() {
    return Effect.succeed([]);
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

class InMemoryKillSwitch implements KillSwitchService {
  private engaged = false;

  engage(_reason: string) {
    return Effect.sync(() => {
      this.engaged = true;
    });
  }

  disengage() {
    return Effect.sync(() => {
      this.engaged = false;
    });
  }

  isEngaged() {
    return Effect.succeed(this.engaged);
  }

  getReason() {
    return Effect.succeed("");
  }
}

class InMemoryCircuitBreaker implements CircuitBreakerService {
  constructor(private openState = false) {}

  recordTradeResult(_realizedPnl: number) {
    return Effect.void;
  }

  isOpen() {
    return Effect.succeed(this.openState);
  }

  currentDailyLossPct() {
    return Effect.succeed(0);
  }

  reset() {
    return Effect.void;
  }

  getReason() {
    return Effect.succeed("");
  }
}

function makeGateway(price: number): MarketDataGatewayService {
  const candles = makeCandles(100, price, "up");
  const orderBook = makeOrderBook(price);
  return {
    fetchTick: () => Effect.fail({ reason: "not used" } as never),
    fetchOHLCV: () => Effect.succeed(candles),
    fetchOrderBook: () => Effect.succeed(orderBook),
    fetchSymbols: () => Effect.fail({ reason: "not used" } as never),
    fetchDemoSymbols: () => Effect.fail({ reason: "not used" } as never),
    fetch24hrVolumes: () => Effect.succeed({}),
    fetchFundingRates: () => Effect.succeed([]),
  };
}

function makeOptions(
  composerConfig: ComposerConfig = defaultComposerConfig,
  overrides: Partial<PaperTradingOptions> = {},
): PaperTradingOptions {
  return {
    exchange: "binance",
    symbol: "BTC/USDT",
    timeframe: "1h",
    composerConfig,
    positionSizePct: 100,
    riskPerTradePct: 0,
    maxPositionSizePct: 100,
    feePct: 0.1,
    minConfidence: 0.5,
    useAtrStops: false,
    atrStopMultiplier: 1.5,
    atrTakeProfitMultiplier: 2.5,
    atrRiskReward: 0,
    scaleOutAtR: 0,
    scaleOutPct: 50,
    volatilityLookback: 0,
    volatilityLowPct: 20,
    volatilityHighPct: 80,
    volatilityLowFactor: 0.8,
    volatilityHighFactor: 1.2,
    volatilityTargetAnnualPct: 0,
    stopLossPct: 1.5,
    takeProfitPct: 3.0,
    holdUntilStop: false,
    minAtrPct: 0,
    initialCapital: 10_000,
    isLive: false,
    ...overrides,
  };
}

function defaultRiskGuard() {
  return makeRiskGuard({
    liveTradingEnabled: false,
    maxPositionSizePct: 100,
    maxDailyLossPct: 100,
    maxDrawdownPct: 100,
    minCapital: 0,
    maxTradesPerDay: Number.MAX_SAFE_INTEGER,
  });
}

function runIteration(
  options: PaperTradingOptions,
  deps: {
    repo?: InMemoryPaperRepository;
    gateway?: MarketDataGatewayService;
    adapter?: ExchangeAdapterService;
    riskGuard?: ReturnType<typeof makeRiskGuard>;
    killSwitch?: KillSwitchService;
    circuitBreaker?: CircuitBreakerService;
  } = {},
) {
  return Effect.runPromise(
    runPaperTradingIteration(options).pipe(
      Effect.provideService(
        PaperTradingRepository,
        deps.repo ?? new InMemoryPaperRepository(),
      ),
      Effect.provideService(
        MarketDataGateway,
        deps.gateway ?? makeGateway(100),
      ),
      Effect.provideService(
        ExchangeAdapter,
        deps.adapter ?? makeSimulatedExchangeAdapter({ USDT: 10_000 }),
      ),
      Effect.provideService(RiskGuard, deps.riskGuard ?? defaultRiskGuard()),
      Effect.provideService(
        KillSwitch,
        deps.killSwitch ?? new InMemoryKillSwitch(),
      ),
      Effect.provideService(
        CircuitBreaker,
        deps.circuitBreaker ?? new InMemoryCircuitBreaker(),
      ),
      Effect.provide(scalpingServiceLayers),
    ),
  );
}

describe("runPaperTradingIteration", () => {
  it("opens a position when signal is strong and risk guard allows", async () => {
    const repo = new InMemoryPaperRepository();
    const gateway = makeGateway(100);
    const adapter = makeSimulatedExchangeAdapter({ USDT: 10_000 });
    const riskGuard = makeRiskGuard({
      liveTradingEnabled: false,
      maxPositionSizePct: 100,
      maxDailyLossPct: 100,
      maxDrawdownPct: 100,
      minCapital: 0,
      maxTradesPerDay: Number.MAX_SAFE_INTEGER,
    });

    const result = await Effect.runPromise(
      runPaperTradingIteration(makeOptions()).pipe(
        Effect.provideService(PaperTradingRepository, repo),
        Effect.provideService(MarketDataGateway, gateway),
        Effect.provideService(ExchangeAdapter, adapter),
        Effect.provideService(RiskGuard, riskGuard),
        Effect.provideService(KillSwitch, new InMemoryKillSwitch()),
        Effect.provideService(CircuitBreaker, new InMemoryCircuitBreaker()),
        Effect.provide(scalpingServiceLayers),
      ),
    );

    expect(result.action).toBe("opened");
    expect(result.position).not.toBeNull();
    expect(result.position?.side).toBe("long");
    expect(result.capital).toBeLessThan(10_000);
  });

  it("blocks entry when the risk guard rejects the trade", async () => {
    const repo = new InMemoryPaperRepository();
    const gateway = makeGateway(100);
    const adapter = makeSimulatedExchangeAdapter({ USDT: 10_000 });
    const riskGuard = makeRiskGuard({
      liveTradingEnabled: false,
      maxPositionSizePct: 0,
      maxDailyLossPct: 100,
      maxDrawdownPct: 100,
      minCapital: 0,
      maxTradesPerDay: Number.MAX_SAFE_INTEGER,
    });

    const result = await Effect.runPromise(
      runPaperTradingIteration(makeOptions()).pipe(
        Effect.provideService(PaperTradingRepository, repo),
        Effect.provideService(MarketDataGateway, gateway),
        Effect.provideService(ExchangeAdapter, adapter),
        Effect.provideService(RiskGuard, riskGuard),
        Effect.provideService(KillSwitch, new InMemoryKillSwitch()),
        Effect.provideService(CircuitBreaker, new InMemoryCircuitBreaker()),
        Effect.provide(scalpingServiceLayers),
      ),
    );

    expect(result.action).toBe("hold");
    expect(result.note).toContain("RISK BLOCKED");
    expect(result.position).toBeNull();
  });

  it("blocks entry when kill switch is engaged", async () => {
    const repo = new InMemoryPaperRepository();
    const gateway = makeGateway(100);
    const adapter = makeSimulatedExchangeAdapter({ USDT: 10_000 });
    const riskGuard = makeRiskGuard({
      liveTradingEnabled: false,
      maxPositionSizePct: 100,
      maxDailyLossPct: 100,
      maxDrawdownPct: 100,
      minCapital: 0,
      maxTradesPerDay: Number.MAX_SAFE_INTEGER,
    });
    const killSwitch = new InMemoryKillSwitch();
    Effect.runSync(killSwitch.engage("test"));

    const result = await Effect.runPromise(
      runPaperTradingIteration(makeOptions()).pipe(
        Effect.provideService(PaperTradingRepository, repo),
        Effect.provideService(MarketDataGateway, gateway),
        Effect.provideService(ExchangeAdapter, adapter),
        Effect.provideService(RiskGuard, riskGuard),
        Effect.provideService(KillSwitch, killSwitch),
        Effect.provideService(CircuitBreaker, new InMemoryCircuitBreaker()),
        Effect.provide(scalpingServiceLayers),
      ),
    );

    expect(result.action).toBe("hold");
    expect(result.note).toContain("KILL SWITCH ENGAGED");
    expect(result.position).toBeNull();
  });

  it("blocks entry when circuit breaker is open", async () => {
    const repo = new InMemoryPaperRepository();
    const gateway = makeGateway(100);
    const adapter = makeSimulatedExchangeAdapter({ USDT: 10_000 });
    const riskGuard = makeRiskGuard({
      liveTradingEnabled: false,
      maxPositionSizePct: 100,
      maxDailyLossPct: 100,
      maxDrawdownPct: 100,
      minCapital: 0,
      maxTradesPerDay: Number.MAX_SAFE_INTEGER,
    });

    const result = await Effect.runPromise(
      runPaperTradingIteration(makeOptions()).pipe(
        Effect.provideService(PaperTradingRepository, repo),
        Effect.provideService(MarketDataGateway, gateway),
        Effect.provideService(ExchangeAdapter, adapter),
        Effect.provideService(RiskGuard, riskGuard),
        Effect.provideService(KillSwitch, new InMemoryKillSwitch()),
        Effect.provideService(CircuitBreaker, new InMemoryCircuitBreaker(true)),
        Effect.provide(scalpingServiceLayers),
      ),
    );

    expect(result.action).toBe("hold");
    expect(result.note).toContain("CIRCUIT BREAKER OPEN");
    expect(result.position).toBeNull();
  });

  it("caps entry size at maxPositionSizePct", async () => {
    // riskPerTradePct 1% of 10_000 = 100 risked over a ~1.5% stop sizes
    // 6666.67 notional; the 20% position cap cuts it to 2000 -> size 20 BTC.
    const result = await runIteration(
      makeOptions(undefined, { riskPerTradePct: 1, maxPositionSizePct: 20 }),
    );

    expect(result.action).toBe("opened");
    expect(result.position?.size.toNumber()).toBeCloseTo(20, 4);
  });

  it("does not place an order below the minAtrPct volatility gate", async () => {
    // minAtrPct 100% can never be met by the up-trend candles' ATR; the gate
    // must hold BEFORE adapter.placeOrder, so no order and no fee.
    const adapter = {
      ...makeSimulatedExchangeAdapter({ USDT: 10_000 }),
      placeOrder: () =>
        Effect.fail(new ExchangeError("unexpected order below minAtrPct gate")),
    };

    const result = await runIteration(
      makeOptions(undefined, { minAtrPct: 100 }),
      { adapter },
    );

    expect(result.action).toBe("hold");
    expect(result.note).toContain("LOW VOLATILITY");
    expect(result.position).toBeNull();
    expect(result.capital).toBe(10_000);
  });

  it("holds without ordering when the order book is empty", async () => {
    const gateway = {
      ...makeGateway(100),
      fetchOrderBook: () =>
        Effect.succeed({ ...makeOrderBook(100), bids: [], asks: [] }),
    };
    const adapter = {
      ...makeSimulatedExchangeAdapter({ USDT: 10_000 }),
      placeOrder: () =>
        Effect.fail(
          new ExchangeError("no order should be placed on empty book"),
        ),
    };

    const result = await runIteration(makeOptions(), { gateway, adapter });

    expect(result.action).toBe("hold");
    expect(result.note).toBe("empty order book");
    expect(result.position).toBeNull();
  });

  it("seeds initialCapital only on a fresh portfolio and stays terminal when blown", async () => {
    const freshRepo = new InMemoryPaperRepository(0, 0);
    const fresh = await runIteration(
      makeOptions(undefined, { initialCapital: 500 }),
      { repo: freshRepo },
    );
    expect(fresh.action).toBe("opened");
    // Sized from the seeded 500, not from any other default.
    expect(fresh.position?.size.toNumber()).toBeCloseTo(5, 4);
    expect(fresh.capital).toBeLessThan(500);

    // A blown account (capital 0, peak retained) must NOT be resurrected.
    const blownRepo = new InMemoryPaperRepository(0, 5_000);
    const adapter = {
      ...makeSimulatedExchangeAdapter({ USDT: 10_000 }),
      placeOrder: () =>
        Effect.fail(new ExchangeError("no entry on a blown account")),
    };
    const blown = await runIteration(
      makeOptions(undefined, { initialCapital: 10_000 }),
      { repo: blownRepo, adapter },
    );

    expect(blown.action).toBe("hold");
    expect(blown.note).toContain("capital exhausted");
    expect(blown.capital).toBe(0);
    expect(blown.position).toBeNull();
  });

  it("closes with the adapter fill price and fee when a stop loss hits", async () => {
    const repo = new InMemoryPaperRepository();
    repo.seedPosition({
      id: "pos-stop",
      exchange: "binance",
      symbol: "BTC/USDT",
      timeframe: "1h",
      side: "long",
      entryPrice: money(100),
      size: money(10),
      stopLoss: money(95),
      takeProfit: money(120),
      openedAt: new Date(),
      signalId: "signal-1",
      scaledOut: false,
      scaleOutPrice: money(0),
    });
    const gateway = makeGatewayWithCandles(
      makeCandlesWithLastBar({ open: 100, high: 101, low: 94, close: 100 }),
      makeOrderBook(100),
    );
    const adapter = {
      ...makeSimulatedExchangeAdapter({ USDT: 10_000 }),
      closePosition: () =>
        Effect.succeed({
          orderId: "close-1",
          symbol: "BTC/USDT",
          side: "sell" as const,
          filledQty: 10,
          filledPrice: 96,
          fee: 0.5,
          timestamp: new Date(),
        }),
    };

    const result = await runIteration(makeOptions(), {
      repo,
      gateway,
      adapter,
    });

    expect(result.action).toBe("closed");
    expect(result.position).toBeNull();
    expect(result.note).toContain("stop_loss");
    // pnl (96-100)*10 = -40, fee 0.5 -> 10000 - 40 - 0.5.
    expect(result.capital).toBeCloseTo(9959.5, 6);
  });

  it("falls back to the take-profit level and charges a fee when no fill", async () => {
    const repo = new InMemoryPaperRepository();
    repo.seedPosition({
      id: "pos-tp",
      exchange: "binance",
      symbol: "BTC/USDT",
      timeframe: "1h",
      side: "long",
      entryPrice: money(100),
      size: money(10),
      stopLoss: money(90),
      takeProfit: money(120),
      openedAt: new Date(),
      signalId: "signal-1",
      scaledOut: false,
      scaleOutPrice: money(0),
    });
    const gateway = makeGatewayWithCandles(
      makeCandlesWithLastBar({ open: 110, high: 121, low: 109, close: 120 }),
      makeOrderBook(100),
    );
    const adapter = {
      ...makeSimulatedExchangeAdapter({ USDT: 10_000 }),
      closePosition: () => Effect.succeed(null),
    };

    const result = await runIteration(makeOptions(), {
      repo,
      gateway,
      adapter,
    });

    expect(result.action).toBe("closed");
    expect(result.note).toContain("take_profit");
    // fallback exit = max(open 110, takeProfit 120) = 120; pnl (120-100)*10
    // = 200, fee 120*10*0.1% = 1.2 -> 10000 + 200 - 1.2.
    expect(result.capital).toBeCloseTo(10198.8, 6);
  });

  it("closes on a signal exit at the candle close without a fill", async () => {
    const repo = new InMemoryPaperRepository();
    repo.seedPosition({
      id: "pos-signal",
      exchange: "binance",
      symbol: "BTC/USDT",
      timeframe: "1h",
      side: "long",
      entryPrice: money(100),
      size: money(10),
      stopLoss: money(10),
      takeProfit: money(150),
      openedAt: new Date(),
      signalId: "signal-1",
      scaledOut: false,
      scaleOutPrice: money(0),
    });
    // Down-trend candles + ask-heavy book compose a sell signal that exits
    // the long (exit does not gate on confidence, only direction).
    const orderBook: OrderBook = {
      exchange: "binance",
      symbol: "BTC/USDT",
      bids: [{ price: 99.0, volume: 10 }],
      asks: [{ price: 100.0, volume: 200 }],
      timestamp: new Date(),
    };
    const gateway = makeGatewayWithCandles(
      makeCandles(100, 100, "down"),
      orderBook,
    );
    const adapter = {
      ...makeSimulatedExchangeAdapter({ USDT: 10_000 }),
      closePosition: () => Effect.succeed(null),
    };

    const result = await runIteration(makeOptions(), {
      repo,
      gateway,
      adapter,
    });

    expect(result.action).toBe("closed");
    expect(result.note).toContain("signal");
    expect(result.position).toBeNull();
    expect(result.capital).toBeLessThan(10_000);
  });

  it("records the scale-out fee against the partial size only", async () => {
    const repo = new InMemoryPaperRepository();
    repo.seedPosition({
      id: "pos-scale",
      exchange: "binance",
      symbol: "BTC/USDT",
      timeframe: "1h",
      side: "long",
      entryPrice: money(100),
      size: money(10),
      stopLoss: money(90),
      takeProfit: money(130),
      openedAt: new Date(),
      signalId: "signal-1",
      scaledOut: false,
      scaleOutPrice: money(115),
    });
    const gateway = makeGatewayWithCandles(
      makeCandlesWithLastBar({ open: 110, high: 116, low: 109, close: 115 }),
      makeOrderBook(100),
    );

    const result = await runIteration(makeOptions(), { repo, gateway });

    expect(result.action).toBe("scaled_out");
    expect(result.note).toContain("SCALE-OUT");
    // Scale 50% of 10 = 5 at 115: pnl (115-100)*5 = 75, fee 115*5*0.1% =
    // 0.575 -> 10000 + 75 - 0.575; remaining size 5.
    expect(result.capital).toBeCloseTo(10074.425, 6);
    expect(result.position?.size.toNumber()).toBeCloseTo(5, 6);
  });
});
