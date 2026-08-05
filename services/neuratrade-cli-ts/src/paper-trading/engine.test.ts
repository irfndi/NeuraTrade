import { describe, expect, it } from "bun:test";
import { Effect, Layer } from "effect";
import {
  MarketDataGateway,
  type MarketDataGatewayService,
} from "../market-data/gateway.js";
import type { Candle, OrderBook } from "../market-data/types.js";
import {
  ExchangeAdapter,
  type ExchangeAdapterService,
} from "../exchange/adapter.js";
import { makeSimulatedExchangeAdapter } from "../exchange/adapters/simulated.js";
import {
  RiskGuard,
  type RiskGuardService,
  makeRiskGuard,
} from "../risk/guards.js";
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

class InMemoryPaperRepository implements PaperTradingRepositoryService {
  private capital = money(10_000);
  private peakCapital = money(10_000);
  private position: PaperPosition | null = null;
  private trades: PaperTrade[] = [];

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

  listAllGridTrades(
    _exchange: string,
    _timeframe: string,
    limit: number,
  ) {
    return Effect.succeed(
      this.gridTrades.slice(-limit).reverse() as GridPaperTrade[],
    );
  }
}

class InMemoryKillSwitch implements KillSwitchService {
  private engaged = false;

  engage(reason: string) {
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
    fetch24hrVolumes: () => Effect.succeed({}),
    fetchFundingRates: () => Effect.succeed([]),
  };
}

function makeOptions(
  composerConfig: ComposerConfig = defaultComposerConfig,
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
  };
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
});
