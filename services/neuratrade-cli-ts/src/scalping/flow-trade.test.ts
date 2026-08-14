/**
 * Flow Ignition — live trade engine tests.
 *
 * Fake adapter/repo/gateway/risk services (no DB, no network). Asserts the
 * engine's observable contracts:
 *  1. entry when the flow signal clears the threshold (LONG and SHORT);
 *  2. no entry below the threshold;
 *  3. ATR stop closes at the stop price;
 *  4. time exit closes at exitAt;
 *  5. OFI-flip emergency closes;
 *  6. kill-switch engaged blocks entry and holds;
 *  7. live-position mismatch engages the kill switch;
 *  8. state persists across iterations (reload resumes the position);
 *  9. size rounds to the exchange step (contract-spec logic).
 */

import { describe, expect, it } from "bun:test";
import { Effect } from "effect";
import { money } from "../utils/money.js";
import type { CandleLike } from "./types.js";
import {
  iterateFlowTrade,
  freshFlowTradeState,
  type FlowTradeOptions,
} from "./flow-trade.js";
import type {
  MarketDataError,
  MarketDataGatewayService,
} from "../market-data/gateway.js";
import type { Candle } from "../market-data/types.js";
import type {
  ClosePositionRequest,
  FuturesExchangeAdapterService,
  FuturesOrderFill,
  FuturesOrderRequest,
  FuturesPosition,
} from "../exchange/futures-adapter.js";
import type { PaperTradingRepositoryService } from "../paper-trading/repository.js";
import type { FlowTradeState } from "./flow-trade.js";
import type { KillSwitchService } from "../risk/kill-switch.js";
import type { CircuitBreakerService } from "../risk/circuit-breaker.js";
import {
  RiskError,
  type RiskContext,
  type RiskGuardService,
} from "../risk/guards.js";
import type { ContractSizeSpec } from "../paper-trading/types.js";

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

class FakeGateway implements MarketDataGatewayService {
  candles: Candle[] = [];
  funding = [] as { ts: number; fundingRate: number }[];

  /** Load CandleLike fixtures as full Candle rows for the fake. */
  load(candles: readonly CandleLike[]): void {
    this.candles = candles.map((c) => ({
      exchange: "bybit-futures",
      symbol: "TESTUSDT",
      timeframe: "5m",
      ...c,
    }));
  }

  fetchTick = () => Effect.die(new Error("unused in flow-trade tests"));
  fetchOHLCV = (
    _exchange: string,
    _symbol: string,
    _timeframe: string,
    limit: number,
  ): Effect.Effect<readonly Candle[], MarketDataError, never> =>
    Effect.succeed(this.candles.slice(-limit));
  fetchOrderBook = () => Effect.die(new Error("unused in flow-trade tests"));
  fetchSymbols = () => Effect.die(new Error("unused in flow-trade tests"));
  fetchDemoSymbols = () => Effect.die(new Error("unused in flow-trade tests"));
  fetch24hrVolumes = () => Effect.die(new Error("unused in flow-trade tests"));
  fetchFundingRates = () =>
    Effect.succeed(
      this.funding.map((r) => ({
        exchange: "bybit-futures",
        symbol: "TESTUSDT",
        fundingRate: r.fundingRate,
        timestamp: new Date(r.ts),
      })),
    );
}

class FakeRepo implements PaperTradingRepositoryService {
  oi = [] as { ts: number; oi: number; oiValue?: number }[];
  state: FlowTradeState | null = null;

  ensureTables = () => Effect.void;
  getOpenPosition = () => Effect.die(new Error("unused"));
  saveOpenPosition = () => Effect.die(new Error("unused"));
  closePosition = () => Effect.die(new Error("unused"));
  scaleOutPosition = () => Effect.die(new Error("unused"));
  getPortfolio = () => Effect.die(new Error("unused"));
  setPortfolio = () => Effect.die(new Error("unused"));
  listRecentTrades = () => Effect.die(new Error("unused"));
  countTradesForDate = () => Effect.die(new Error("unused"));
  getTodayRealizedPnl = () => Effect.die(new Error("unused"));
  getStartOfDayCapital = () => Effect.die(new Error("unused"));
  getGridState = () => Effect.die(new Error("unused"));
  saveGridState = () => Effect.die(new Error("unused"));
  getLadderState = () => Effect.die(new Error("unused"));
  saveLadderState = () => Effect.die(new Error("unused"));
  resetGridState = () => Effect.die(new Error("unused"));
  recordGridTrade = () => Effect.die(new Error("unused"));
  listRecentGridTrades = () => Effect.die(new Error("unused"));
  listAllGridTrades = () => Effect.die(new Error("unused"));
  listWatchlist = () => Effect.die(new Error("unused"));
  upsertWatchlist = () => Effect.die(new Error("unused"));
  clearWatchlist = () => Effect.die(new Error("unused"));
  replaceWatchlist = () => Effect.die(new Error("unused"));

  getOpenInterest = () =>
    Effect.succeed(
      this.oi.map((r) => ({
        exchange: "bybit-futures",
        symbol: "TESTUSDT",
        timeframe: "5m",
        ts: r.ts,
        oi: r.oi,
        oiValue: r.oiValue ?? 0,
      })),
    );
  getFlowTradeState = () => Effect.succeed(this.state);
  saveFlowTradeState = (state: FlowTradeState) =>
    Effect.sync(() => {
      this.state = { ...state };
    });
  clearFlowTradeState = () =>
    Effect.sync(() => {
      this.state = null;
    });
}

class FakeAdapter implements FuturesExchangeAdapterService {
  positions = new Map<string, { side: "long" | "short"; quantity: number }>();
  orders: Array<{ side: string; size: number; price?: number }> = [];
  fills = [] as FuturesOrderFill[];
  closed: Array<{ side: string; size: number }> = [];

  placeOrder = (request: FuturesOrderRequest) => {
    const self = this;
    return Effect.sync(() => {
      const size = Number(request.size.toFixed(8));
      self.orders.push({
        side: request.side,
        size,
        price: request.price ? Number(request.price) : undefined,
      });
      const fillPrice = request.price ? Number(request.price) : 100;
      self.positions.set(request.symbol, {
        side: request.side === "buy" ? "long" : "short",
        quantity: size,
      });
      const fill: FuturesOrderFill = {
        orderId: `order-${self.orders.length}`,
        symbol: request.symbol,
        side: request.side,
        productType: request.productType,
        marginMode: request.marginMode,
        filledQty: money(size),
        filledPrice: money(fillPrice),
        fee: money(0),
        timestamp: new Date(),
      };
      self.fills.push(fill);
      return fill;
    });
  };
  closePosition = (request: ClosePositionRequest) => {
    const self = this;
    return Effect.sync(() => {
      self.closed.push({ side: request.side, size: Number(request.size) });
      const pos = self.positions.get(request.symbol);
      if (!pos) return null;
      self.positions.delete(request.symbol);
      return {
        orderId: `close-${self.closed.length}`,
        symbol: request.symbol,
        side: request.side,
        productType: request.productType,
        marginMode: request.marginMode,
        filledQty: money(Number(request.size)),
        filledPrice: money(100),
        fee: money(0),
        timestamp: new Date(),
      };
    });
  };
  getPosition = (symbol: string) => {
    const self = this;
    return Effect.succeed(
      (() => {
        const pos = self.positions.get(symbol);
        if (!pos) return null;
        const p: FuturesPosition = {
          symbol,
          side: pos.side,
          productType: "USDT-FUTURES",
          marginMode: "crossed",
          leverage: 1,
          quantity: money(pos.quantity),
          available: money(pos.quantity),
          entryPrice: money(100),
          marginCoin: "USDT",
        };
        return p;
      })(),
    );
  };
  getBalance = () => Effect.die(new Error("unused"));
  setLeverage = () => Effect.void;
  setMarginMode = () => Effect.void;
  setPositionMode = () => Effect.void;
}

class FakeRiskGuard implements RiskGuardService {
  blocked = false;
  check = (ctx: RiskContext) =>
    this.blocked
      ? Effect.fail(
          new RiskError(`risk blocked: capital ${ctx.capital} below minimum`, [
            "capital below minimum",
          ]),
        )
      : Effect.void;
}

class FakeKillSwitch implements KillSwitchService {
  engaged = false;
  reason = "";
  engage = (reason: string) =>
    Effect.sync(() => {
      this.engaged = true;
      this.reason = reason;
    });
  disengage = () =>
    Effect.sync(() => {
      this.engaged = false;
      this.reason = "";
    });
  isEngaged = () => Effect.succeed(this.engaged);
  getReason = () => Effect.succeed(this.reason);
}

class FakeCircuitBreaker implements CircuitBreakerService {
  open = false;
  recordTradeResult = () => Effect.void;
  isOpen = () => Effect.succeed(this.open);
  getReason = () => Effect.succeed("test breaker");
  currentDailyLossPct = () => Effect.succeed(0);
  reset = () => Effect.void;
}

// ---------------------------------------------------------------------------
// Fixtures (ported from flow-backtest.test.ts — proven to generate signals)
// ---------------------------------------------------------------------------

interface Series {
  readonly candles: CandleLike[];
  readonly oi: { ts: number; oi: number }[];
  readonly funding: { ts: number; fundingRate: number }[];
}

function buildSeries(p: {
  bars: number;
  barMs: number;
  startTs: number;
  direction: 1 | -1;
  trendStrength: number;
  acceleration: number;
  oiChange: number;
  oiAcceleration: number;
  volumeRise: number;
  buyBiasFrom: number;
  buyBiasTo: number;
  fundingRate?: number;
}): Series {
  const candles: CandleLike[] = [];
  let price = 100;
  const n = p.bars;
  for (let i = 0; i < n; i++) {
    const frac = i / Math.max(1, n - 1);
    const ret = p.direction * (p.trendStrength + p.acceleration * frac);
    const bias = p.buyBiasFrom + (p.buyBiasTo - p.buyBiasFrom) * frac;
    const denom = 2 * bias - 1;
    const open = price;
    const rangePct = denom !== 0 ? ret / denom : 0.0005;
    const low = open * (1 - Math.abs(rangePct));
    const high = open * (1 + Math.abs(rangePct));
    const close = low + (high - low) * bias;
    const volume = 1000 * (1 + p.volumeRise * frac);
    candles.push({
      open,
      high,
      low,
      close,
      volume,
      timestamp: new Date(p.startTs + i * p.barMs),
    });
    price = close;
  }

  const oi: { ts: number; oi: number }[] = [];
  const oiStep = (15 * 60_000) / p.barMs;
  let oiValue = 10_000;
  for (let k = 0; k < Math.floor(n / oiStep); k++) {
    const frac = k / Math.max(1, Math.floor(n / oiStep) - 1);
    const dOi = p.direction * (p.oiChange + p.oiAcceleration * frac);
    oi.push({ ts: p.startTs + k * 15 * 60_000, oi: oiValue });
    oiValue = oiValue * (1 + dOi);
  }

  const funding: { ts: number; fundingRate: number }[] = [];
  if (p.fundingRate !== undefined) {
    const spanMs = n * p.barMs;
    for (let ts = p.startTs; ts < p.startTs + spanMs; ts += 8 * 3_600_000) {
      funding.push({ ts, fundingRate: p.fundingRate });
    }
  }

  return { candles, oi, funding };
}

const TRENDING = {
  bars: 700, // ~58h of 5m bars — past the 600-bar history limit is fine
  barMs: 300_000,
  // Anchored inside the engine's 3-day OI/funding window (the engine reads
  // OI/funding since now-3d; the backtest-fixture June dates would be
  // filtered out).
  startTs: Date.now() - 3 * 86_400_000,
  direction: 1 as 1 | -1,
  trendStrength: 0.0001,
  acceleration: 0.0006,
  oiChange: 0.0005,
  oiAcceleration: 0.002,
  volumeRise: 3,
  buyBiasFrom: 0.55,
  buyBiasTo: 0.85,
  fundingRate: 0.0001,
};

const COLLAPSING = {
  ...TRENDING,
  direction: -1 as 1 | -1,
  buyBiasFrom: 0.45,
  buyBiasTo: 0.15,
};

function baseOptions(overrides?: Partial<FlowTradeOptions>): FlowTradeOptions {
  return {
    exchange: "bybit-futures",
    symbol: "TESTUSDT",
    timeframe: "5m",
    capital: 1000,
    maxPositionSizePct: 10,
    leverage: 1,
    productType: "USDT-FUTURES",
    marginMode: "crossed",
    holdMinutes: 60,
    isLive: false,
    ...overrides,
  };
}

/** Fixture wired to a long-trending series (last signal LONG). */
function longFixture() {
  const series = buildSeries(TRENDING);
  const gateway = new FakeGateway();
  gateway.load(series.candles);
  gateway.funding = series.funding;
  const repo = new FakeRepo();
  repo.oi = series.oi;
  return { series, gateway, repo };
}

function run(
  repo: FakeRepo,
  gateway: FakeGateway,
  adapter: FakeAdapter,
  riskGuard: FakeRiskGuard,
  killSwitch: FakeKillSwitch,
  circuitBreaker: FakeCircuitBreaker,
  opts: FlowTradeOptions,
) {
  return Effect.runPromise(
    iterateFlowTrade(
      repo,
      gateway,
      adapter,
      riskGuard,
      killSwitch,
      circuitBreaker,
      opts,
    ),
  );
}

const SPEC: ContractSizeSpec = {
  minQty: 0.001,
  qtyStep: 0.001,
  minTradeUSDT: 5,
};

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("flow-trade engine", () => {
  it("enters LONG when the signal clears the threshold", async () => {
    const { gateway, repo } = longFixture();
    const adapter = new FakeAdapter();
    const result = await run(
      repo,
      gateway,
      adapter,
      new FakeRiskGuard(),
      new FakeKillSwitch(),
      new FakeCircuitBreaker(),
      baseOptions(),
    );

    expect(result.action).toBe("opened");
    expect(result.side).toBe("LONG");
    expect(result.state.entryPrice).toBeGreaterThan(100);
    expect(result.state.stopPrice).toBeLessThan(result.state.entryPrice!);
    expect(result.state.exitAt).toBe(result.state.entryTime! + 60 * 60_000);
    expect(adapter.orders[0]?.side).toBe("buy");
    expect(adapter.positions.get("TESTUSDT")?.side).toBe("long");
    // State persisted after the iteration.
    expect(repo.state?.side).toBe("LONG");
  });

  it("enters SHORT when the mirrored signal clears the threshold", async () => {
    const series = buildSeries(COLLAPSING);
    const gateway = new FakeGateway();
    gateway.load(series.candles);
    gateway.funding = series.funding;
    const repo = new FakeRepo();
    repo.oi = series.oi;
    const adapter = new FakeAdapter();
    const result = await run(
      repo,
      gateway,
      adapter,
      new FakeRiskGuard(),
      new FakeKillSwitch(),
      new FakeCircuitBreaker(),
      baseOptions(),
    );

    expect(result.action).toBe("opened");
    expect(result.side).toBe("SHORT");
    expect(result.state.stopPrice).toBeGreaterThan(result.state.entryPrice!);
    expect(adapter.orders[0]?.side).toBe("sell");
  });

  it("does not enter when the signal is below the threshold", async () => {
    const { gateway, repo } = longFixture();
    const adapter = new FakeAdapter();
    const result = await run(
      repo,
      gateway,
      adapter,
      new FakeRiskGuard(),
      new FakeKillSwitch(),
      new FakeCircuitBreaker(),
      baseOptions({ threshold: 100 }), // impossibly high → NONE
    );

    expect(result.action).toBe("hold");
    expect(adapter.orders).toHaveLength(0);
    expect(repo.state).toBeNull();
  });

  it("rounds the size to the exchange step via contract-spec logic", async () => {
    const { gateway, repo } = longFixture();
    const adapter = new FakeAdapter();
    const result = await run(
      repo,
      gateway,
      adapter,
      new FakeRiskGuard(),
      new FakeKillSwitch(),
      new FakeCircuitBreaker(),
      baseOptions({ contractSpecs: SPEC }),
    );

    expect(result.action).toBe("opened");
    const size = Number(result.state.qty);
    // Rounded to the step (multiple of 0.001) and never below minQty.
    const steps = size / SPEC.qtyStep;
    expect(Math.abs(Math.round(steps) - steps)).toBeLessThan(1e-6);
    expect(size).toBeGreaterThanOrEqual(SPEC.minQty);
    const orderSteps = adapter.orders[0]!.size / SPEC.qtyStep;
    expect(Math.abs(Math.round(orderSteps) - orderSteps)).toBeLessThan(1e-6);
  });

  it("closes at the ATR stop", async () => {
    const { gateway, repo } = longFixture();
    const adapter = new FakeAdapter();
    const killSwitch = new FakeKillSwitch();

    const opened = await run(
      repo,
      gateway,
      adapter,
      new FakeRiskGuard(),
      killSwitch,
      new FakeCircuitBreaker(),
      baseOptions(),
    );
    expect(opened.action).toBe("opened");
    const stop = opened.state.stopPrice!;

    // Next iteration: flat series below the stop → market close.
    const lastTs =
      gateway.candles[gateway.candles.length - 1]!.timestamp.getTime();
    const fall = Array.from({ length: 60 }, (_, i) => ({
      open: stop * 0.995,
      high: stop * 0.995,
      low: stop * 0.995,
      close: stop * 0.995,
      volume: 1000,
      timestamp: new Date(lastTs + (i + 1) * 300_000),
    }));
    gateway.load(fall);
    repo.oi = [];

    const result = await run(
      repo,
      gateway,
      adapter,
      new FakeRiskGuard(),
      killSwitch,
      new FakeCircuitBreaker(),
      baseOptions(),
    );
    expect(result.action).toBe("closed");
    expect(result.note).toContain("stop");
    expect(adapter.closed[0]?.side).toBe("sell");
    expect(adapter.positions.size).toBe(0);
    expect(repo.state).toBeNull(); // cleared after close
  });

  it("closes at the time exit (exitAt)", async () => {
    const { gateway, repo } = longFixture();
    const adapter = new FakeAdapter();
    const killSwitch = new FakeKillSwitch();

    const opened = await run(
      repo,
      gateway,
      adapter,
      new FakeRiskGuard(),
      killSwitch,
      new FakeCircuitBreaker(),
      baseOptions({ holdMinutes: 0 }), // exitAt = entry time → fires next iteration
    );
    expect(opened.action).toBe("opened");

    // Candles far in the future (well past exitAt) trending up from the
    // current price: stop not hit, OFI sign unchanged → the time exit fires.
    const lastTs =
      gateway.candles[gateway.candles.length - 1]!.timestamp.getTime();
    const base = gateway.candles[gateway.candles.length - 1]!.close;
    const later: CandleLike[] = [];
    let price = base;
    for (let i = 0; i < 60; i++) {
      const low = price * 0.999;
      const high = price * 1.001;
      const close = price * 1.0005; // gentle uptrend, stays above the stop
      later.push({
        open: price,
        high,
        low,
        close,
        volume: 1000,
        timestamp: new Date(lastTs + (i + 1) * 300_000),
      });
      price = close;
    }
    gateway.load(later);
    repo.oi = []; // no OI → zOi = 0 → no emergency exit

    const result = await run(
      repo,
      gateway,
      adapter,
      new FakeRiskGuard(),
      killSwitch,
      new FakeCircuitBreaker(),
      baseOptions({ holdMinutes: 0 }),
    );
    expect(result.action).toBe("closed");
    expect(result.note).toContain("time");
    expect(repo.state).toBeNull();
  });

  it("closes on the OFI-flip emergency when |z_dOI| > 1.5", async () => {
    const { series, gateway, repo } = longFixture();
    const adapter = new FakeAdapter();
    const killSwitch = new FakeKillSwitch();

    const opened = await run(
      repo,
      gateway,
      adapter,
      new FakeRiskGuard(),
      killSwitch,
      new FakeCircuitBreaker(),
      baseOptions(),
    );
    expect(opened.action).toBe("opened");

    // Next window: price declines mildly (stop untouched) with strongly
    // bearish candle flow (OFI sign flips vs the entry's positive OFI) and a
    // hard OI collapse in the final 15m window (|z_dOI| ≫ 1.5). The series
    // spans 60 bars so the z-score history has enough prior samples.
    const lastTs =
      gateway.candles[gateway.candles.length - 1]!.timestamp.getTime();
    const crash: CandleLike[] = [];
    let price = gateway.candles[gateway.candles.length - 1]!.close;
    for (let i = 0; i < 60; i++) {
      const ret = -0.00001; // ~0.001% per bar — far above the stop
      const low = price * (1 - Math.abs(ret) * 2);
      const high = price * (1 + Math.abs(ret) * 0.1);
      const close = low + (high - low) * 0.1; // closes near the low → bearish OFI
      crash.push({
        open: price,
        high,
        low,
        close,
        volume: 5000,
        timestamp: new Date(lastTs + (i + 1) * 300_000),
      });
      price = close;
    }
    gateway.load(crash);
    // OI: continue the entry series' history, then a hard collapse inside the
    // final 15m window (dOi ≈ -0.9 vs an accelerating positive history →
    // z_dOI ≪ -1.5).
    const crashOi = [];
    let oiValue = series.oi[series.oi.length - 1]!.oi;
    for (let k = 0; k < 19; k++) {
      crashOi.push({ ts: lastTs + k * 900_000, oi: oiValue });
      oiValue = oiValue * 1.004;
    }
    crashOi.push({ ts: lastTs + 60 * 300_000, oi: 1000 }); // collapse at series end
    repo.oi = [...series.oi, ...crashOi];

    const result = await run(
      repo,
      gateway,
      adapter,
      new FakeRiskGuard(),
      killSwitch,
      new FakeCircuitBreaker(),
      baseOptions(),
    );
    expect(result.action).toBe("closed");
    expect(result.note).toContain("emergency");
    expect(repo.state).toBeNull();
  });

  it("kill-switch engaged blocks entry and holds", async () => {
    const { gateway, repo } = longFixture();
    const adapter = new FakeAdapter();
    const killSwitch = new FakeKillSwitch();
    killSwitch.engaged = true;
    killSwitch.reason = "operator stop";

    const result = await run(
      repo,
      gateway,
      adapter,
      new FakeRiskGuard(),
      killSwitch,
      new FakeCircuitBreaker(),
      baseOptions(),
    );
    expect(result.action).toBe("hold");
    expect(result.note).toContain("kill switch");
    expect(adapter.orders).toHaveLength(0);
    expect(adapter.positions.size).toBe(0);
    expect(repo.state).toBeNull();
  });

  it("a live-position mismatch engages the kill switch and holds", async () => {
    const { gateway, repo } = longFixture();
    const adapter = new FakeAdapter();
    const killSwitch = new FakeKillSwitch();

    const opened = await run(
      repo,
      gateway,
      adapter,
      new FakeRiskGuard(),
      killSwitch,
      new FakeCircuitBreaker(),
      baseOptions(),
    );
    expect(opened.action).toBe("opened");

    // Exchange loses the position (e.g. liquidation) while state says LONG.
    adapter.positions.delete("TESTUSDT");

    const result = await run(
      repo,
      gateway,
      adapter,
      new FakeRiskGuard(),
      killSwitch,
      new FakeCircuitBreaker(),
      baseOptions(),
    );
    expect(result.action).toBe("hold");
    expect(killSwitch.engaged).toBe(true);
    expect(result.note).toContain("POSITION MISMATCH");
    expect(repo.state?.killed).toBe(true); // sticky latch persisted
  });

  it("persists state across iterations and resumes the position", async () => {
    const { gateway, repo } = longFixture();
    const adapter = new FakeAdapter();
    const killSwitch = new FakeKillSwitch();

    const opened = await run(
      repo,
      gateway,
      adapter,
      new FakeRiskGuard(),
      killSwitch,
      new FakeCircuitBreaker(),
      baseOptions(),
    );
    expect(opened.action).toBe("opened");

    // A brand-new engine instance (fresh adapter/risk fakes) reads the
    // persisted state and continues the SAME position instead of re-entering.
    const adapter2 = new FakeAdapter();
    const result2 = await run(
      repo,
      gateway,
      adapter2,
      new FakeRiskGuard(),
      new FakeKillSwitch(),
      new FakeCircuitBreaker(),
      baseOptions(),
    );
    expect(result2.action).toBe("hold");
    expect(result2.side).toBe("LONG");
    expect(result2.state.entryPrice).toBe(opened.state.entryPrice);
    expect(adapter2.orders).toHaveLength(0); // no double entry
    expect(adapter2.positions.size).toBe(0); // fake adapter2 never entered
  });

  it("starts flat with an empty persisted state and holds on no signal", async () => {
    const { gateway, repo } = longFixture();
    const adapter = new FakeAdapter();
    const result = await run(
      repo,
      gateway,
      adapter,
      new FakeRiskGuard(),
      new FakeKillSwitch(),
      new FakeCircuitBreaker(),
      baseOptions({ threshold: 100 }),
    );
    expect(result.state.side).toBeNull();
    expect(freshFlowTradeState(baseOptions(), 0).side).toBeNull();
    expect(result.state.capital).toBe(1000);
  });
});
