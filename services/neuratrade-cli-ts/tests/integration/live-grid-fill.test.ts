import { describe, expect, it } from "bun:test";
import { Database } from "bun:sqlite";
import { Effect, Layer } from "effect";
import {
  MarketDataGateway,
  type MarketDataGatewayService,
} from "../../src/market-data/gateway.js";
import type { Candle } from "../../src/market-data/types.js";
import {
  FuturesExchangeAdapter,
  type FuturesExchangeAdapterService,
  type FuturesPosition,
} from "../../src/exchange/futures-adapter.js";
import { money } from "../../src/utils/money.js";
import { PaperTradingRepositorySQLite } from "../../src/paper-trading/repository.js";
import {
  runGridPaperTradingIteration,
  type GridPaperTradingOptions,
} from "../../src/paper-trading/grid-engine.js";
import { PaperTradingRepository } from "../../src/paper-trading/repository.js";
import { evaluateDemoSoak } from "../../src/paper-trading/demo-readiness.js";
import { RiskGuard, type RiskGuardService } from "../../src/risk/guards.js";
import {
  KillSwitch,
  type KillSwitchService,
} from "../../src/risk/kill-switch.js";
import {
  CircuitBreaker,
  type CircuitBreakerService,
} from "../../src/risk/circuit-breaker.js";
import { KillSwitchSQLiteLive } from "../../src/risk/kill-switch.js";

function makeCandles(count: number): Candle[] {
  const candles: Candle[] = [];
  let close = 70_000;
  for (let index = 0; index < count; index++) {
    const open = close;
    close *= index % 2 === 0 ? 1.012 : 0.988;
    candles.push({
      exchange: "bitget-futures",
      symbol: "BTC/USDT:USDT",
      timeframe: "15m",
      open,
      high: Math.max(open, close) * 1.005,
      low: Math.min(open, close) * 0.995,
      close,
      volume: 100,
      timestamp: new Date(Date.UTC(2026, 7, 1, 0, index * 15)),
    });
  }
  return candles;
}

function makeGateway(candles: readonly Candle[]): MarketDataGatewayService {
  return {
    fetchTick: () => Effect.fail({ reason: "not used" } as never),
    fetchOHLCV: () => Effect.succeed([...candles]),
    fetchOrderBook: () =>
      Effect.succeed({
        exchange: "bitget-futures",
        symbol: "BTC/USDT:USDT",
        bids: [{ price: 70_000, volume: 1 }],
        asks: [{ price: 70_001, volume: 1 }],
        timestamp: new Date(),
      }),
    fetchSymbols: () => Effect.fail({ reason: "not used" } as never),
    fetchDemoSymbols: () => Effect.fail({ reason: "not used" } as never),
    fetch24hrVolumes: () => Effect.succeed({}),
    fetchFundingRates: () => Effect.succeed([]),
  };
}

function makeFill(
  request: Parameters<FuturesExchangeAdapterService["placeOrder"]>[0],
  orderId: string,
) {
  return {
    orderId,
    symbol: request.symbol,
    side: request.side,
    productType: request.productType,
    marginMode: request.marginMode,
    filledQty: request.size,
    filledPrice: request.price ?? money(70_000),
    fee: money("0.01"),
    timestamp: new Date(),
  };
}

function makeAdapter(): FuturesExchangeAdapterService {
  let orderNumber = 0;
  let closeNumber = 0;
  let position: FuturesPosition | null = null;
  return {
    placeOrder: (request) =>
      Effect.sync(() => {
        const fill = makeFill(request, `entry-${++orderNumber}`);
        position = {
          symbol: request.symbol,
          side: request.side === "buy" ? "long" : "short",
          productType: request.productType,
          marginMode: request.marginMode,
          leverage: request.leverage,
          quantity: fill.filledQty,
          available: fill.filledQty,
          entryPrice: fill.filledPrice,
          marginCoin: "USDT",
        };
        return fill;
      }),
    closePosition: (request) =>
      Effect.sync(() => {
        const fill = makeFill(
          {
            ...request,
            type: request.price === undefined ? "market" : "limit",
          },
          `close-${++closeNumber}`,
        );
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
}

const riskGuard: RiskGuardService = {
  check: () => Effect.void,
};

const killSwitch: KillSwitchService = {
  isEngaged: () => Effect.succeed(false),
  getReason: () => Effect.succeed(""),
  engage: () => Effect.void,
  disengage: () => Effect.void,
};

const circuitBreaker: CircuitBreakerService = {
  isOpen: () => Effect.succeed(false),
  getReason: () => Effect.succeed(""),
  currentDailyLossPct: () => Effect.succeed(0),
  recordTradeResult: () => Effect.void,
  reset: () => Effect.void,
};

const options: GridPaperTradingOptions = {
  exchange: "bitget-futures",
  symbol: "BTC/USDT:USDT",
  timeframe: "15m",
  gridStepPct: 1,
  gridMaxGrids: 1.5,
  gridPauseAfterLossBars: 0,
  feePct: 0.02,
  slippageBps: 1,
  trendFilterPeriod: 10,
  initialCapital: 1000,
  maxPositionPct: 50,
  maxDrawdownPct: 15,
  leverage: 1,
  targetRatio: 1,
  replayBars: 25,
  isLive: true,
  productType: "USDT-FUTURES",
  marginMode: "isolated",
};

describe("live grid fill integration", () => {
  it("persists complete live fills and evaluates the resulting trade", async () => {
    const db = new Database(":memory:");
    try {
      const repository = new PaperTradingRepositorySQLite(db);
      const candles = makeCandles(40);
      const layers = Layer.mergeAll(
        Layer.succeed(MarketDataGateway, makeGateway(candles)),
        Layer.succeed(FuturesExchangeAdapter, makeAdapter()),
        Layer.succeed(RiskGuard, riskGuard),
        Layer.succeed(KillSwitch, killSwitch),
        Layer.succeed(CircuitBreaker, circuitBreaker),
      );

      for (let iteration = 0; iteration < 30; iteration++) {
        await Effect.runPromise(
          runGridPaperTradingIteration(options).pipe(
            Effect.provideService(PaperTradingRepository, repository),
            Effect.provide(layers),
          ),
        );
      }

      const trades = await Effect.runPromise(
        repository.listRecentGridTrades(
          options.exchange,
          options.symbol,
          options.timeframe,
          100,
        ),
      );
      expect(trades.length).toBeGreaterThan(0);
      expect(trades.every((trade) => trade.fillSource === "live")).toBe(true);
      expect(
        trades.every(
          (trade) =>
            trade.entryOrderId !== undefined &&
            trade.exitOrderId !== undefined &&
            trade.entryFilledQty?.greaterThan(0) === true &&
            trade.exitFilledQty?.equals(trade.entryFilledQty) === true &&
            trade.realizedPnlPct?.isFinite() === true,
        ),
      ).toBe(true);

      const report = evaluateDemoSoak(trades, {
        minimumTrades: 1,
        minimumDurationDays: 0,
        minimumExpectancyPct: money(-100),
        maximumDrawdownPct: money(100),
      });
      expect(report.passed).toBe(true);
      expect(report.expectancyPct.greaterThan(0)).toBe(true);
    } finally {
      db.close();
    }
  });

  it("persists a kill switch when local and exchange positions diverge", async () => {
    const db = new Database(":memory:");
    try {
      const repository = new PaperTradingRepositorySQLite(db);
      const orphanPosition: FuturesPosition = {
        symbol: "BTC/USDT:USDT",
        side: "long",
        productType: "USDT-FUTURES",
        marginMode: "isolated",
        leverage: 1,
        quantity: money("0.01"),
        available: money("0.01"),
        entryPrice: money(70_000),
        marginCoin: "USDT",
      };
      const adapter: FuturesExchangeAdapterService = {
        ...makeAdapter(),
        getPosition: () => Effect.succeed(orphanPosition),
      };
      const layers = Layer.mergeAll(
        Layer.succeed(MarketDataGateway, makeGateway(makeCandles(40))),
        Layer.succeed(FuturesExchangeAdapter, adapter),
        Layer.succeed(RiskGuard, riskGuard),
        KillSwitchSQLiteLive(db),
        Layer.succeed(CircuitBreaker, circuitBreaker),
      );

      const result = await Effect.runPromise(
        runGridPaperTradingIteration(options).pipe(
          Effect.provideService(PaperTradingRepository, repository),
          Effect.provide(layers),
        ),
      );

      expect(result.note).toContain("LIVE POSITION MISMATCH");
      expect(
        db.query("SELECT engaged FROM risk_kill_switch WHERE id = 1").get(),
      ).toEqual({ engaged: 1 });
    } finally {
      db.close();
    }
  });
});
