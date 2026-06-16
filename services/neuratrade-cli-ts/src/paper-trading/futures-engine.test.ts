import { describe, expect, it } from "bun:test";
import { Effect, Either } from "effect";
import {
  MarketDataGateway,
  type MarketDataGatewayService,
} from "../market-data/gateway.js";
import type { Candle, OrderBook } from "../market-data/types.js";
import { FuturesExchangeAdapter } from "../exchange/futures-adapter.js";
import { makeSimulatedFuturesExchangeAdapterService } from "../exchange/adapters/simulated-futures.js";
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
import type { PaperPosition, PaperTrade } from "./types.js";
import {
  runFuturesPaperTradingIteration,
  type FuturesPaperTradingOptions,
} from "./futures-engine.js";
import { defaultComposerConfig } from "../scalping/composer.js";
import type { ComposerConfig } from "../scalping/types.js";

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
  private capital = 10_000;
  private peakCapital = 10_000;
  private position: PaperPosition | null = null;
  private trades: PaperTrade[] = [];

  ensureTables() {
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
    exitPrice: number,
    exitReason: PaperTrade["exitReason"],
    closedAt: Date,
  ) {
    return Effect.sync(() => {
      const priceDiff =
        position.side === "long"
          ? exitPrice - position.entryPrice
          : position.entryPrice - exitPrice;
      const pnl = priceDiff * position.size;
      const pnlPct = (pnl / (position.entryPrice * position.size)) * 100;
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

  getPortfolio() {
    return Effect.succeed({
      capital: this.capital,
      peakCapital: this.peakCapital,
    });
  }

  setPortfolio(capital: number, peakCapital: number) {
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
    return Effect.succeed(this.trades.reduce((sum, t) => sum + t.pnl, 0));
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
    fetch24hrVolumes: () => Effect.succeed({}),
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
    feePct: 0.1,
    minConfidence: 0.5,
    useAtrStops: false,
    atrStopMultiplier: 1.5,
    atrTakeProfitMultiplier: 2.5,
    holdUntilStop: false,
    initialCapital: 10_000,
    isLive: false,
    leverage: 10,
    marginMode: "crossed",
    productType: "USDT-FUTURES",
  };
}

describe("runFuturesPaperTradingIteration", () => {
  it("opens a leveraged long position", async () => {
    const repo = new InMemoryPaperRepository();
    const gateway = makeGateway(100);
    const adapter = makeFuturesAdapter();
    const riskGuard = makeRiskGuard({
      liveTradingEnabled: false,
      maxPositionSizePct: 100,
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
      maxPositionSizePct: 100,
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
      ) as Effect.Effect<
        import("./futures-engine.js").FuturesPaperTradingIterationResult,
        never
      >,
    );

    expect(result.action).toBe("hold");
    expect(result.note).toContain("KILL SWITCH ENGAGED");
    expect(result.position).toBeNull();
  });

  it("blocks entry when circuit breaker is open", async () => {
    const repo = new InMemoryPaperRepository();
    const gateway = makeGateway(100);
    const adapter = makeFuturesAdapter();
    const riskGuard = makeRiskGuard({
      liveTradingEnabled: false,
      maxPositionSizePct: 100,
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
      ) as Effect.Effect<
        import("./futures-engine.js").FuturesPaperTradingIterationResult,
        never
      >,
    );

    expect(result.action).toBe("hold");
    expect(result.note).toContain("CIRCUIT BREAKER OPEN");
    expect(result.position).toBeNull();
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
      };
      const adapter = Effect.runSync(
        makeSimulatedFuturesExchangeAdapterService(gateway, { USDT: 10_000 }),
      );
      const riskGuard = makeRiskGuard({
        liveTradingEnabled: false,
        maxPositionSizePct: 100,
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
          Effect.either,
        ) as Effect.Effect<
          Either.Either<
            import("./futures-engine.js").FuturesPaperTradingIterationResult,
            unknown
          >,
          never
        >,
      );

      expect(outcome._tag).toBe("Right");
      if (outcome._tag === "Right") {
        expect(outcome.right.capital).toBeGreaterThanOrEqual(0);
      }
    }
  });
});
