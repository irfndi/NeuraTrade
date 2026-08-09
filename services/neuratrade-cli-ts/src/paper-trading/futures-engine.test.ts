import { describe, expect, it } from "bun:test";
import { Effect, Layer, Result } from "effect";
import {
  MarketDataGateway,
  type MarketDataGatewayService,
} from "../market-data/gateway.js";
import type { Candle, OrderBook } from "../market-data/types.js";
import { FuturesExchangeAdapter } from "../exchange/futures-adapter.js";
import { makeSimulatedFuturesExchangeAdapterService } from "../exchange/adapters/simulated-futures.js";
import { ExchangeError } from "../exchange/adapter.js";
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
  runFuturesPaperTradingIteration,
  type FuturesPaperTradingOptions,
} from "./futures-engine.js";
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
      exchange: "bitget-futures",
      symbol: "BTC/USDT:USDT",
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
    exchange: "bitget-futures",
    symbol: "BTC/USDT:USDT",
    bids: [{ price: price * 0.999, volume: 80 }],
    asks: [{ price: price * 1.001, volume: 20 }],
    timestamp: new Date(),
  };
}

class InMemoryPaperRepository implements PaperTradingRepositoryService {
  private capital: Decimal;
  private peakCapital: Decimal;
  private position: PaperPosition | null = null;
  private trades: PaperTrade[] = [];

  constructor(initialCapital: number = 10_000) {
    this.capital = money(initialCapital);
    this.peakCapital = money(initialCapital);
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
  readonly recordedPnl: number[] = [];

  recordTradeResult(realizedPnl: number) {
    this.recordedPnl.push(realizedPnl);
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
    fetch24hrVolumes: () => Effect.succeed({}),
    fetchFundingRates: () => Effect.succeed([]),
  };
}

function makeFuturesAdapter() {
  return Effect.runSync(
    makeSimulatedFuturesExchangeAdapterService(makeGateway(100), {
      USDT: 10_000,
    }),
  );
}

function makeOptions(
  composerConfig: ComposerConfig = defaultComposerConfig,
): FuturesPaperTradingOptions {
  return {
    exchange: "bitget-futures",
    symbol: "BTC/USDT:USDT",
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
    leverage: 10,
    marginMode: "crossed",
    productType: "USDT-FUTURES",
  };
}

function makeOpenPosition(
  overrides: Partial<PaperPosition> = {},
): PaperPosition {
  return {
    id: "position-1",
    exchange: "bitget-futures",
    symbol: "BTC/USDT:USDT",
    timeframe: "1h",
    side: "long",
    entryPrice: money(100),
    size: money(1),
    stopLoss: money(90),
    takeProfit: money(110),
    openedAt: new Date(Date.now() - 3_600_000),
    signalId: "signal-1",
    scaledOut: false,
    scaleOutPrice: money(0),
    ...overrides,
  };
}

describe("runFuturesPaperTradingIteration", () => {
  it("opens a leveraged long position", async () => {
    const repo = new InMemoryPaperRepository();
    const gateway = makeGateway(100);
    const adapter = makeFuturesAdapter();
    const riskGuard = makeRiskGuard({
      liveTradingEnabled: false,
      maxPositionSizePct: Number.MAX_SAFE_INTEGER,
      maxDailyLossPct: 100,
      maxDrawdownPct: 100,
      minCapital: 0,
      maxTradesPerDay: Number.MAX_SAFE_INTEGER,
    });

    const result = await Effect.runPromise(
      runFuturesPaperTradingIteration(makeOptions()).pipe(
        Effect.provideService(PaperTradingRepository, repo),
        Effect.provideService(MarketDataGateway, gateway),
        Effect.provideService(FuturesExchangeAdapter, adapter),
        Effect.provideService(RiskGuard, riskGuard),
        Effect.provideService(KillSwitch, new InMemoryKillSwitch()),
        Effect.provideService(CircuitBreaker, new InMemoryCircuitBreaker()),
        Effect.provide(scalpingServiceLayers),
      ) as Effect.Effect<
        import("./futures-engine.js").FuturesPaperTradingIterationResult,
        never
      >,
    );

    expect(result.action).toBe("opened");
    expect(result.position).not.toBeNull();
    expect(result.position?.side).toBe("long");
    expect(result.note).toContain("leverage=10x");
    expect(result.capital).toBeLessThan(10_000);
  });

  it("caps risk per trade by the daily-loss bound", async () => {
    // capital 100, 5% risk/trade at a 1.5% stop would size 333.33 notional;
    // the 2% daily cap / 1 concurrent trade allows at most 2% risk per trade,
    // so the notional is capped at 2 / 0.015 = 133.33.
    const repo = new InMemoryPaperRepository(100);
    const gateway = makeGateway(100);
    const adapter = makeFuturesAdapter();
    const riskGuard = makeRiskGuard({
      liveTradingEnabled: false,
      maxPositionSizePct: Number.MAX_SAFE_INTEGER,
      maxDailyLossPct: 100,
      maxDrawdownPct: 100,
      minCapital: 0,
      maxTradesPerDay: Number.MAX_SAFE_INTEGER,
    });

    const result = await Effect.runPromise(
      runFuturesPaperTradingIteration({
        ...makeOptions(),
        initialCapital: 100,
        riskPerTradePct: 5,
        maxPositionSizePct: 300,
        maxDailyLossPct: 2,
        maxConcurrentTrades: 1,
      }).pipe(
        Effect.provideService(PaperTradingRepository, repo),
        Effect.provideService(MarketDataGateway, gateway),
        Effect.provideService(FuturesExchangeAdapter, adapter),
        Effect.provideService(RiskGuard, riskGuard),
        Effect.provideService(KillSwitch, new InMemoryKillSwitch()),
        Effect.provideService(CircuitBreaker, new InMemoryCircuitBreaker()),
        Effect.provide(scalpingServiceLayers),
      ) as Effect.Effect<
        import("./futures-engine.js").FuturesPaperTradingIterationResult,
        never
      >,
    );

    expect(result.action).toBe("opened");
    // size = 133.33 notional / 100 entry = 1.3333 (uncapped would be 3.0).
    expect(result.position?.size.toNumber()).toBeCloseTo(1.3333, 2);
  });

  it("raises a sub-floor notional to the minimum order size", async () => {
    // capital 10, 0.1% risk at a 1.5% stop sizes 0.67 notional, below the
    // 5 USDT exchange floor; the order is raised to the floor (margin
    // capacity 10 * 1x leverage allows it).
    const repo = new InMemoryPaperRepository(10);
    const gateway = makeGateway(100);
    const adapter = makeFuturesAdapter();
    const riskGuard = makeRiskGuard({
      liveTradingEnabled: false,
      maxPositionSizePct: Number.MAX_SAFE_INTEGER,
      maxDailyLossPct: 100,
      maxDrawdownPct: 100,
      minCapital: 0,
      maxTradesPerDay: Number.MAX_SAFE_INTEGER,
    });

    const result = await Effect.runPromise(
      runFuturesPaperTradingIteration({
        ...makeOptions(),
        initialCapital: 10,
        riskPerTradePct: 0.1,
        maxPositionSizePct: 100,
        leverage: 1,
      }).pipe(
        Effect.provideService(PaperTradingRepository, repo),
        Effect.provideService(MarketDataGateway, gateway),
        Effect.provideService(FuturesExchangeAdapter, adapter),
        Effect.provideService(RiskGuard, riskGuard),
        Effect.provideService(KillSwitch, new InMemoryKillSwitch()),
        Effect.provideService(CircuitBreaker, new InMemoryCircuitBreaker()),
        Effect.provide(scalpingServiceLayers),
      ) as Effect.Effect<
        import("./futures-engine.js").FuturesPaperTradingIterationResult,
        never
      >,
    );

    expect(result.action).toBe("opened");
    // size = 5 floor notional / 100 entry = 0.05.
    expect(result.position?.size.toNumber()).toBeCloseTo(0.05, 4);
    expect(result.note).toContain("leverage=1x");
  });

  it("raises leverage so a tiny account can afford the floor", async () => {
    // capital 3 with 10x max leverage: a floor-sized 5 USDT order needs
    // ceil(5/3) = 2x leverage for its margin (2.5) to fit the account.
    const repo = new InMemoryPaperRepository(3);
    const gateway = makeGateway(100);
    const adapter = makeFuturesAdapter();
    const riskGuard = makeRiskGuard({
      liveTradingEnabled: false,
      maxPositionSizePct: Number.MAX_SAFE_INTEGER,
      maxDailyLossPct: 100,
      maxDrawdownPct: 100,
      minCapital: 0,
      maxTradesPerDay: Number.MAX_SAFE_INTEGER,
    });

    const result = await Effect.runPromise(
      runFuturesPaperTradingIteration({
        ...makeOptions(),
        initialCapital: 3,
        riskPerTradePct: 0.1,
        maxPositionSizePct: 100,
        leverage: 10,
      }).pipe(
        Effect.provideService(PaperTradingRepository, repo),
        Effect.provideService(MarketDataGateway, gateway),
        Effect.provideService(FuturesExchangeAdapter, adapter),
        Effect.provideService(RiskGuard, riskGuard),
        Effect.provideService(KillSwitch, new InMemoryKillSwitch()),
        Effect.provideService(CircuitBreaker, new InMemoryCircuitBreaker()),
        Effect.provide(scalpingServiceLayers),
      ) as Effect.Effect<
        import("./futures-engine.js").FuturesPaperTradingIterationResult,
        never
      >,
    );

    expect(result.action).toBe("opened");
    // ceil(5/3) = 2x leverage, order raised to the 5 USDT floor.
    expect(result.position?.size.toNumber()).toBeCloseTo(0.05, 4);
    expect(result.note).toContain("leverage=2x");
  });

  it("blocks entry when the risk guard rejects the trade", async () => {
    const repo = new InMemoryPaperRepository();
    const gateway = makeGateway(100);
    const adapter = makeFuturesAdapter();
    const riskGuard = makeRiskGuard({
      liveTradingEnabled: false,
      maxPositionSizePct: 0,
      maxDailyLossPct: 100,
      maxDrawdownPct: 100,
      minCapital: 0,
      maxTradesPerDay: Number.MAX_SAFE_INTEGER,
    });

    const result = await Effect.runPromise(
      runFuturesPaperTradingIteration(makeOptions()).pipe(
        Effect.provideService(PaperTradingRepository, repo),
        Effect.provideService(MarketDataGateway, gateway),
        Effect.provideService(FuturesExchangeAdapter, adapter),
        Effect.provideService(RiskGuard, riskGuard),
        Effect.provideService(KillSwitch, new InMemoryKillSwitch()),
        Effect.provideService(CircuitBreaker, new InMemoryCircuitBreaker()),
        Effect.provide(scalpingServiceLayers),
      ) as Effect.Effect<
        import("./futures-engine.js").FuturesPaperTradingIterationResult,
        never
      >,
    );

    expect(result.action).toBe("hold");
    expect(result.note).toContain("RISK BLOCKED");
    expect(result.position).toBeNull();
  });

  it("blocks entry when kill switch is engaged", async () => {
    const repo = new InMemoryPaperRepository();
    const gateway = makeGateway(100);
    const adapter = makeFuturesAdapter();
    const riskGuard = makeRiskGuard({
      liveTradingEnabled: false,
      maxPositionSizePct: Number.MAX_SAFE_INTEGER,
      maxDailyLossPct: 100,
      maxDrawdownPct: 100,
      minCapital: 0,
      maxTradesPerDay: Number.MAX_SAFE_INTEGER,
    });
    const killSwitch = new InMemoryKillSwitch();
    Effect.runSync(killSwitch.engage("test"));

    const result = await Effect.runPromise(
      runFuturesPaperTradingIteration(makeOptions()).pipe(
        Effect.provideService(PaperTradingRepository, repo),
        Effect.provideService(MarketDataGateway, gateway),
        Effect.provideService(FuturesExchangeAdapter, adapter),
        Effect.provideService(RiskGuard, riskGuard),
        Effect.provideService(KillSwitch, killSwitch),
        Effect.provideService(CircuitBreaker, new InMemoryCircuitBreaker()),
        Effect.provide(scalpingServiceLayers),
      ) as Effect.Effect<
        import("./futures-engine.js").FuturesPaperTradingIterationResult,
        never
      >,
    );

    expect(result.action).toBe("hold");
    expect(result.note).toContain("KILL SWITCH ENGAGED");
    expect(result.position).toBeNull();
  });

  it("prioritises kill switch over risk guard rejection", async () => {
    const repo = new InMemoryPaperRepository();
    const gateway = makeGateway(100);
    const adapter = makeFuturesAdapter();
    const riskGuard = makeRiskGuard({
      liveTradingEnabled: false,
      maxPositionSizePct: 0,
      maxDailyLossPct: 100,
      maxDrawdownPct: 100,
      minCapital: 0,
      maxTradesPerDay: Number.MAX_SAFE_INTEGER,
    });
    const killSwitch = new InMemoryKillSwitch();
    Effect.runSync(killSwitch.engage("test"));

    const result = await Effect.runPromise(
      runFuturesPaperTradingIteration(makeOptions()).pipe(
        Effect.provideService(PaperTradingRepository, repo),
        Effect.provideService(MarketDataGateway, gateway),
        Effect.provideService(FuturesExchangeAdapter, adapter),
        Effect.provideService(RiskGuard, riskGuard),
        Effect.provideService(KillSwitch, killSwitch),
        Effect.provideService(CircuitBreaker, new InMemoryCircuitBreaker()),
        Effect.provide(scalpingServiceLayers),
      ) as Effect.Effect<
        import("./futures-engine.js").FuturesPaperTradingIterationResult,
        never
      >,
    );

    expect(result.action).toBe("hold");
    expect(result.note).toContain("KILL SWITCH ENGAGED");
    expect(result.note).not.toContain("RISK BLOCKED");
    expect(result.position).toBeNull();
  });

  it("blocks entry when circuit breaker is open", async () => {
    const repo = new InMemoryPaperRepository();
    const gateway = makeGateway(100);
    const adapter = makeFuturesAdapter();
    const riskGuard = makeRiskGuard({
      liveTradingEnabled: false,
      maxPositionSizePct: Number.MAX_SAFE_INTEGER,
      maxDailyLossPct: 100,
      maxDrawdownPct: 100,
      minCapital: 0,
      maxTradesPerDay: Number.MAX_SAFE_INTEGER,
    });

    const result = await Effect.runPromise(
      runFuturesPaperTradingIteration(makeOptions()).pipe(
        Effect.provideService(PaperTradingRepository, repo),
        Effect.provideService(MarketDataGateway, gateway),
        Effect.provideService(FuturesExchangeAdapter, adapter),
        Effect.provideService(RiskGuard, riskGuard),
        Effect.provideService(KillSwitch, new InMemoryKillSwitch()),
        Effect.provideService(CircuitBreaker, new InMemoryCircuitBreaker(true)),
        Effect.provide(scalpingServiceLayers),
      ) as Effect.Effect<
        import("./futures-engine.js").FuturesPaperTradingIterationResult,
        never
      >,
    );

    expect(result.action).toBe("hold");
    expect(result.note).toContain("CIRCUIT BREAKER OPEN");
    expect(result.position).toBeNull();
  });

  it("fails closed when a live close returns no exchange fill", async () => {
    const repo = new InMemoryPaperRepository();
    const position = makeOpenPosition();
    Effect.runSync(repo.saveOpenPosition(position));
    const simulatedAdapter = makeFuturesAdapter();
    const adapter = {
      ...simulatedAdapter,
      closePosition: () => Effect.succeed(null),
    };
    const riskGuard = makeRiskGuard({
      liveTradingEnabled: true,
      maxPositionSizePct: Number.MAX_SAFE_INTEGER,
      maxDailyLossPct: 100,
      maxDrawdownPct: 100,
      minCapital: 0,
      maxTradesPerDay: Number.MAX_SAFE_INTEGER,
    });

    let failure: unknown;
    try {
      await Effect.runPromise(
        runFuturesPaperTradingIteration({
          ...makeOptions(),
          isLive: true,
        }).pipe(
          Effect.provideService(PaperTradingRepository, repo),
          Effect.provideService(MarketDataGateway, makeGateway(100)),
          Effect.provideService(FuturesExchangeAdapter, adapter),
          Effect.provideService(RiskGuard, riskGuard),
          Effect.provideService(KillSwitch, new InMemoryKillSwitch()),
          Effect.provideService(CircuitBreaker, new InMemoryCircuitBreaker()),
          Effect.provide(scalpingServiceLayers),
        ) as Effect.Effect<
          import("./futures-engine.js").FuturesPaperTradingIterationResult,
          never
        >,
      );
    } catch (error) {
      failure = error;
    }

    expect(failure).toBeInstanceOf(ExchangeError);
    expect(
      Effect.runSync(repo.getOpenPosition(position.exchange, position.symbol)),
    ).toEqual(position);
    expect(Effect.runSync(repo.listRecentTrades(10))).toHaveLength(0);
  });

  it("rejects live scale-out until it has exchange-fill reconciliation", async () => {
    const repo = new InMemoryPaperRepository();
    const position = makeOpenPosition({
      scaleOutPrice: money(100),
    });
    Effect.runSync(repo.saveOpenPosition(position));
    const riskGuard = makeRiskGuard({
      liveTradingEnabled: true,
      maxPositionSizePct: Number.MAX_SAFE_INTEGER,
      maxDailyLossPct: 100,
      maxDrawdownPct: 100,
      minCapital: 0,
      maxTradesPerDay: Number.MAX_SAFE_INTEGER,
    });

    let failure: unknown;
    try {
      await Effect.runPromise(
        runFuturesPaperTradingIteration({
          ...makeOptions(),
          isLive: true,
        }).pipe(
          Effect.provideService(PaperTradingRepository, repo),
          Effect.provideService(MarketDataGateway, makeGateway(100)),
          Effect.provideService(FuturesExchangeAdapter, makeFuturesAdapter()),
          Effect.provideService(RiskGuard, riskGuard),
          Effect.provideService(KillSwitch, new InMemoryKillSwitch()),
          Effect.provideService(CircuitBreaker, new InMemoryCircuitBreaker()),
          Effect.provide(scalpingServiceLayers),
        ) as Effect.Effect<
          import("./futures-engine.js").FuturesPaperTradingIterationResult,
          never
        >,
      );
    } catch (error) {
      failure = error;
    }

    expect(failure).toBeInstanceOf(ExchangeError);
    expect(
      Effect.runSync(repo.getOpenPosition(position.exchange, position.symbol)),
    ).toEqual(position);
    expect(Effect.runSync(repo.listRecentTrades(10))).toHaveLength(0);
  });

  it("records the realized PnL into the circuit breaker on a paper scale-out", async () => {
    const repo = new InMemoryPaperRepository();
    const position = makeOpenPosition({
      entryPrice: money(100),
      stopLoss: money(90),
      takeProfit: money(110),
      scaleOutPrice: money(105),
    });
    Effect.runSync(repo.saveOpenPosition(position));
    const circuitBreaker = new InMemoryCircuitBreaker();
    const riskGuard = makeRiskGuard({
      liveTradingEnabled: false,
      maxPositionSizePct: Number.MAX_SAFE_INTEGER,
      maxDailyLossPct: 100,
      maxDrawdownPct: 100,
      minCapital: 0,
      maxTradesPerDay: Number.MAX_SAFE_INTEGER,
    });

    const result = await Effect.runPromise(
      runFuturesPaperTradingIteration({
        ...makeOptions(),
        scaleOutAtR: 1,
        scaleOutPct: 50,
      }).pipe(
        Effect.provideService(PaperTradingRepository, repo),
        Effect.provideService(MarketDataGateway, makeGateway(105)),
        Effect.provideService(FuturesExchangeAdapter, makeFuturesAdapter()),
        Effect.provideService(RiskGuard, riskGuard),
        Effect.provideService(KillSwitch, new InMemoryKillSwitch()),
        Effect.provideService(CircuitBreaker, circuitBreaker),
        Effect.provide(scalpingServiceLayers),
      ) as Effect.Effect<
        import("./futures-engine.js").FuturesPaperTradingIterationResult,
        never
      >,
    );

    expect(result.action).toBe("scaled_out");
    expect(circuitBreaker.recordedPnl.length).toBeGreaterThan(0);
  });

  it("checks minimum ATR before placing a live entry order", async () => {
    const repo = new InMemoryPaperRepository();
    const simulatedAdapter = makeFuturesAdapter();
    const adapter = {
      ...simulatedAdapter,
      placeOrder: () =>
        Effect.fail(
          new ExchangeError("unexpected entry order in low volatility"),
        ),
    };
    const riskGuard = makeRiskGuard({
      liveTradingEnabled: true,
      maxPositionSizePct: Number.MAX_SAFE_INTEGER,
      maxDailyLossPct: 100,
      maxDrawdownPct: 100,
      minCapital: 0,
      maxTradesPerDay: Number.MAX_SAFE_INTEGER,
    });

    const result = await Effect.runPromise(
      runFuturesPaperTradingIteration({
        ...makeOptions(),
        isLive: true,
        minAtrPct: 100,
      }).pipe(
        Effect.provideService(PaperTradingRepository, repo),
        Effect.provideService(MarketDataGateway, makeGateway(100)),
        Effect.provideService(FuturesExchangeAdapter, adapter),
        Effect.provideService(RiskGuard, riskGuard),
        Effect.provideService(KillSwitch, new InMemoryKillSwitch()),
        Effect.provideService(CircuitBreaker, new InMemoryCircuitBreaker()),
        Effect.provide(scalpingServiceLayers),
      ) as Effect.Effect<
        import("./futures-engine.js").FuturesPaperTradingIterationResult,
        never
      >,
    );

    expect(result.action).toBe("hold");
    expect(result.note).toContain("LOW VOLATILITY");
    expect(result.position).toBeNull();
    expect(result.capital).toBe(10_000);
  });

  it("survives random volatile candle sequences without crashing", async () => {
    for (let i = 0; i < 15; i++) {
      const repo = new InMemoryPaperRepository();
      const basePrice = 50 + Math.random() * 950;
      const candles = makeCandles(
        100,
        basePrice,
        Math.random() > 0.5 ? "up" : "down",
      );
      // Inject random volatility.
      for (let j = 1; j < candles.length; j++) {
        if (Math.random() < 0.3) {
          const factor = 1 + (Math.random() - 0.5) * 0.05;
          candles[j] = {
            ...candles[j],
            close: candles[j].close * factor,
            high: candles[j].high * factor,
            low: candles[j].low * factor,
          };
        }
      }
      const gateway: MarketDataGatewayService = {
        fetchTick: () => Effect.fail({ reason: "not used" } as never),
        fetchOHLCV: () => Effect.succeed(candles),
        fetchOrderBook: () => Effect.succeed(makeOrderBook(basePrice)),
        fetchSymbols: () => Effect.fail({ reason: "not used" } as never),
        fetch24hrVolumes: () => Effect.succeed({}),
        fetchFundingRates: () => Effect.succeed([]),
      };
      const adapter = Effect.runSync(
        makeSimulatedFuturesExchangeAdapterService(gateway, { USDT: 10_000 }),
      );
      const riskGuard = makeRiskGuard({
        liveTradingEnabled: false,
        maxPositionSizePct: Number.MAX_SAFE_INTEGER,
        maxDailyLossPct: 100,
        maxDrawdownPct: 100,
        minCapital: 0,
        maxTradesPerDay: Number.MAX_SAFE_INTEGER,
      });

      const outcome = await Effect.runPromise(
        runFuturesPaperTradingIteration(makeOptions()).pipe(
          Effect.provideService(PaperTradingRepository, repo),
          Effect.provideService(MarketDataGateway, gateway),
          Effect.provideService(FuturesExchangeAdapter, adapter),
          Effect.provideService(RiskGuard, riskGuard),
          Effect.provideService(KillSwitch, new InMemoryKillSwitch()),
          Effect.provideService(CircuitBreaker, new InMemoryCircuitBreaker()),
          Effect.provide(scalpingServiceLayers),
          Effect.result,
        ) as Effect.Effect<
          Result.Result<
            import("./futures-engine.js").FuturesPaperTradingIterationResult,
            unknown
          >,
          never
        >,
      );

      expect(outcome._tag).toBe("Success");
      if (outcome._tag === "Success") {
        expect(outcome.success.capital).toBeGreaterThanOrEqual(0);
      }
    }
  });
});
