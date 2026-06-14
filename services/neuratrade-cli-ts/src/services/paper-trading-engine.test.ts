import { describe, expect, it } from "bun:test";
import { Effect, Layer } from "effect";
import type { RawCandle } from "./binance-client.ts";
import type { PaperTrade } from "./paper-repository.ts";
import {
  calculateClosePnl,
  calculatePositionSize,
  checkExit,
  PaperTradingEngine,
  PaperTradingEngineLiveImpl,
  stopLossPrice,
  takeProfitPrice,
} from "./paper-trading-engine.ts";
import type { PaperTradingConfig } from "./paper-trading-engine.ts";
import {
  ApiClient,
  NetworkError,
  type BacktestResponse,
  type BacktestSignal,
} from "./api-client.ts";
import { BinanceClient } from "./binance-client.ts";
import { PaperRepository } from "./paper-repository.ts";

const baseConfig: PaperTradingConfig = {
  symbol: "BTC/USDT",
  exchange: "binance",
  capital: "10000",
  windowHours: 48,
  timeframe: "1h",
  feeRate: "0.001",
  mode: "deterministic",
  leverage: 10,
  riskPct: "0.01",
  stopLossPct: "0.005",
  takeProfitPct: "0.015",
  trailingStopPct: "0",
  maxHoldHours: 24,
};

function makeTrade(
  overrides: Partial<PaperTrade> & { readonly side: "buy" | "sell" },
): PaperTrade {
  return {
    id: 1,
    symbol: "BTC/USDT",
    exchange: "binance",
    size: "1",
    notional: "100",
    entry_price: "100",
    entry_at: new Date("2026-01-01T00:00:00Z").toISOString(),
    exit_price: null,
    exit_at: null,
    pnl: null,
    pnl_pct: null,
    fees: null,
    status: "open",
    exit_reason: null,
    signal_id: null,
    mode: "deterministic",
    ...overrides,
  };
}

function candle(
  timestamp: Date,
  open: string,
  high: string,
  low: string,
  close: string,
  volume: string = "1",
): RawCandle {
  return { timestamp, open, high, low, close, volume };
}

describe("PaperTradingEngine helpers", () => {
  it("calculates risk-based position size", () => {
    const result = calculatePositionSize("10000", "0.01", "100", "0.005", 10);
    expect(result.size).toBe("200");
    expect(result.notional).toBe("20000");
    expect(result.margin).toBe("2000");
  });

  it("calculates long close PnL with leverage and two-sided fees", () => {
    const trade = makeTrade({
      side: "buy",
      entry_price: "100",
      size: "0.1",
      notional: "10",
    });
    const result = calculateClosePnl(trade, "102", 10, "0.001");
    expect(result.pnl).toBe("1.9798");
    expect(result.fees).toBe("0.0202");
  });

  it("calculates short close PnL with leverage and two-sided fees", () => {
    const trade = makeTrade({
      side: "sell",
      entry_price: "100",
      size: "0.1",
      notional: "10",
    });
    const result = calculateClosePnl(trade, "98", 10, "0.001");
    expect(result.pnl).toBe("1.9802");
    expect(result.fees).toBe("0.0198");
  });

  it("computes long SL/TP prices", () => {
    expect(stopLossPrice("100", "long", "0.005")).toBe("99.5");
    expect(takeProfitPrice("100", "long", "0.015")).toBe("101.5");
  });

  it("computes short SL/TP prices", () => {
    expect(stopLossPrice("100", "short", "0.005")).toBe("100.5");
    expect(takeProfitPrice("100", "short", "0.015")).toBe("98.5");
  });

  it("exits long on take-profit", () => {
    const trade = makeTrade({ side: "buy", entry_price: "100" });
    const candles = [
      candle(new Date("2026-01-01T01:00:00Z"), "100", "104", "100", "103"),
    ];
    const exit = checkExit(trade, candles, baseConfig);
    expect(exit).not.toBeNull();
    expect(exit?.exitReason).toBe("take_profit");
    expect(exit?.exitPrice).toBe("101.5");
  });

  it("exits short on take-profit", () => {
    const trade = makeTrade({ side: "sell", entry_price: "100" });
    const candles = [
      candle(new Date("2026-01-01T01:00:00Z"), "100", "100", "96", "97"),
    ];
    const exit = checkExit(trade, candles, baseConfig);
    expect(exit).not.toBeNull();
    expect(exit?.exitReason).toBe("take_profit");
    expect(exit?.exitPrice).toBe("98.5");
  });

  it("exits long on stop-loss", () => {
    const trade = makeTrade({ side: "buy", entry_price: "100" });
    const candles = [
      candle(new Date("2026-01-01T01:00:00Z"), "100", "100", "98", "98.5"),
    ];
    const exit = checkExit(trade, candles, baseConfig);
    expect(exit).not.toBeNull();
    expect(exit?.exitReason).toBe("stop_loss");
    expect(exit?.exitPrice).toBe("99.5");
  });

  it("trails a long position and exits on retracement", () => {
    const trade = makeTrade({ side: "buy", entry_price: "100" });
    const config = { ...baseConfig, trailingStopPct: "0.004" };
    const candles = [
      candle(new Date("2026-01-01T01:00:00Z"), "100", "103", "102", "102.5"),
      candle(
        new Date("2026-01-01T02:00:00Z"),
        "102.5",
        "102.5",
        "101.8",
        "101.9",
      ),
    ];
    const exit = checkExit(trade, candles, config);
    expect(exit).not.toBeNull();
    expect(exit?.exitReason).toBe("stop_loss");
    // trailing stop = 103 * (1 - 0.004) = 102.588
    expect(exit?.exitPrice).toBe("102.588");
  });

  it("exits on time-stop", () => {
    const trade = makeTrade({ side: "buy", entry_price: "100" });
    const config = { ...baseConfig, maxHoldHours: 2 };
    const candles = [
      candle(new Date("2026-01-01T03:00:00Z"), "100", "100.5", "99.6", "100.2"),
    ];
    const exit = checkExit(trade, candles, config);
    expect(exit).not.toBeNull();
    expect(exit?.exitReason).toBe("time_stop");
    expect(exit?.exitPrice).toBe("100.2");
  });
});

const baseCandles: ReadonlyArray<RawCandle> = [
  {
    timestamp: new Date("2026-01-01T00:00:00Z"),
    open: "100",
    high: "100",
    low: "100",
    close: "100",
    volume: "1",
  },
];

const engineLayer: Layer.Layer<PaperTradingEngine> = Layer.succeed(
  PaperTradingEngine,
  PaperTradingEngineLiveImpl,
);

function makeMockApiClient(backtest: BacktestResponse): Layer.Layer<ApiClient> {
  return Layer.succeed(ApiClient, {
    generateAuthCode: () => Effect.die("not used"),
    getAIProviders: () => Effect.die("not used"),
    getAIModels: () => Effect.die("not used"),
    getPortfolio: () => Effect.die("not used"),
    getBalance: () => Effect.die("not used"),
    runScalpingBacktest: () => Effect.succeed(backtest),
    health: () => Effect.die("not used"),
  });
}

function makeMockBinanceClient(
  candles: ReadonlyArray<RawCandle>,
): Layer.Layer<BinanceClient> {
  return Layer.succeed(BinanceClient, {
    getExchangeInfo: () => Effect.die("not used"),
    getKlines: () => Effect.succeed(candles),
  });
}

function makeMockPaperRepo(overrides: {
  readonly openTrade?: PaperTrade | null;
  readonly onOpen?: () => number;
  readonly onClose?: () => void;
}): Layer.Layer<PaperRepository> {
  return Layer.succeed(PaperRepository, {
    openTrade: () =>
      overrides.onOpen ? Effect.succeed(overrides.onOpen()) : Effect.succeed(1),
    closeTrade: () =>
      overrides.onClose ? Effect.sync(overrides.onClose) : Effect.void,
    getOpenTrade: () => Effect.succeed(overrides.openTrade ?? null),
    listOpenTrades: () => Effect.succeed([]),
    listClosedTrades: () => Effect.succeed([]),
    getStats: () =>
      Effect.succeed({
        open_count: 0,
        closed_count: 0,
        total_pnl: "0",
        win_count: 0,
        loss_count: 0,
      }),
  });
}

function runEngine(
  config: PaperTradingConfig,
  api: Layer.Layer<ApiClient>,
  binance: Layer.Layer<BinanceClient>,
  repo: Layer.Layer<PaperRepository>,
) {
  return Effect.runPromise(
    Effect.gen(function* () {
      const engine = yield* PaperTradingEngine;
      return yield* engine.evaluateAndTrade(config);
    }).pipe(Effect.provide(Layer.mergeAll(api, binance, repo, engineLayer))),
  );
}

function makeSignal(overrides: Partial<BacktestSignal>): BacktestSignal {
  return {
    signal_id: "sig-1",
    timestamp: "2026-01-01T00:00:00Z",
    symbol: "BTC/USDT",
    exchange: "binance",
    funnel_stage: "accepted",
    ...overrides,
  };
}

const baseBacktest: BacktestResponse = {
  run_id: "r1",
  status: "ok",
  mode: "deterministic",
  summary: {},
  gate_summary: [],
  signals: [],
};

describe("PaperTradingEngineLiveImpl", () => {
  it("returns no_signal when Binance returns no candles", async () => {
    const result = await runEngine(
      baseConfig,
      makeMockApiClient(baseBacktest),
      makeMockBinanceClient([]),
      makeMockPaperRepo({}),
    );
    expect(result.action).toBe("no_signal");
  });

  it("returns no_signal when no accepted signal in window", async () => {
    const result = await runEngine(
      baseConfig,
      makeMockApiClient({
        ...baseBacktest,
        signals: [
          makeSignal({ funnel_stage: "rejected", rejection_reason: "no edge" }),
        ],
      }),
      makeMockBinanceClient(baseCandles),
      makeMockPaperRepo({}),
    );
    expect(result.action).toBe("no_signal");
  });

  it("returns no_signal when accepted signal has no actionable side", async () => {
    const result = await runEngine(
      baseConfig,
      makeMockApiClient({
        ...baseBacktest,
        signals: [makeSignal({ hints: { suggested_action: "hold" } })],
      }),
      makeMockBinanceClient(baseCandles),
      makeMockPaperRepo({}),
    );
    expect(result.action).toBe("no_signal");
  });

  it("opens long on buy signal with no open position", async () => {
    let openCalled = false;
    const result = await runEngine(
      baseConfig,
      makeMockApiClient({
        ...baseBacktest,
        signals: [makeSignal({ hints: { suggested_action: "buy" } })],
      }),
      makeMockBinanceClient(baseCandles),
      makeMockPaperRepo({
        onOpen: () => {
          openCalled = true;
          return 42;
        },
      }),
    );
    expect(openCalled).toBe(true);
    expect(result.action).toBe("open_long");
    expect(result.tradeId).toBe(42);
  });

  it("opens short on sell signal with no open position", async () => {
    let openCalled = false;
    const result = await runEngine(
      baseConfig,
      makeMockApiClient({
        ...baseBacktest,
        signals: [makeSignal({ hints: { suggested_action: "sell" } })],
      }),
      makeMockBinanceClient(baseCandles),
      makeMockPaperRepo({
        onOpen: () => {
          openCalled = true;
          return 7;
        },
      }),
    );
    expect(openCalled).toBe(true);
    expect(result.action).toBe("open_short");
    expect(result.tradeId).toBe(7);
  });

  it("closes an open long when take-profit is hit", async () => {
    const openLong = makeTrade({
      side: "buy",
      entry_price: "100",
      entry_at: new Date("2026-01-01T00:00:00Z").toISOString(),
    });
    const tpCandles = [
      candle(new Date("2026-01-01T01:00:00Z"), "100", "104", "100", "103"),
    ];
    let closeCalled = false;
    const result = await runEngine(
      baseConfig,
      makeMockApiClient(baseBacktest),
      makeMockBinanceClient(tpCandles),
      makeMockPaperRepo({
        openTrade: openLong,
        onClose: () => {
          closeCalled = true;
        },
      }),
    );
    expect(closeCalled).toBe(true);
    expect(result.action).toBe("close_long");
    expect(result.pnl).toBeDefined();
  });

  it("closes an open short when take-profit is hit", async () => {
    const openShort = makeTrade({
      side: "sell",
      entry_price: "100",
      entry_at: new Date("2026-01-01T00:00:00Z").toISOString(),
    });
    const tpCandles = [
      candle(new Date("2026-01-01T01:00:00Z"), "100", "100", "96", "97"),
    ];
    let closeCalled = false;
    const result = await runEngine(
      baseConfig,
      makeMockApiClient(baseBacktest),
      makeMockBinanceClient(tpCandles),
      makeMockPaperRepo({
        openTrade: openShort,
        onClose: () => {
          closeCalled = true;
        },
      }),
    );
    expect(closeCalled).toBe(true);
    expect(result.action).toBe("close_short");
  });

  it("reverses an open long to flat on sell signal", async () => {
    const openLong = makeTrade({
      side: "buy",
      entry_price: "100",
      entry_at: new Date("2026-01-01T00:00:00Z").toISOString(),
    });
    let closeCalled = false;
    const result = await runEngine(
      baseConfig,
      makeMockApiClient({
        ...baseBacktest,
        signals: [makeSignal({ hints: { suggested_action: "sell" } })],
      }),
      makeMockBinanceClient(baseCandles),
      makeMockPaperRepo({
        openTrade: openLong,
        onClose: () => {
          closeCalled = true;
        },
      }),
    );
    expect(closeCalled).toBe(true);
    expect(result.action).toBe("close_long");
    expect(result.message).toContain("reversed long");
  });

  it("reverses an open short to flat on buy signal", async () => {
    const openShort = makeTrade({
      side: "sell",
      entry_price: "100",
      entry_at: new Date("2026-01-01T00:00:00Z").toISOString(),
    });
    let closeCalled = false;
    const result = await runEngine(
      baseConfig,
      makeMockApiClient({
        ...baseBacktest,
        signals: [makeSignal({ hints: { suggested_action: "buy" } })],
      }),
      makeMockBinanceClient(baseCandles),
      makeMockPaperRepo({
        openTrade: openShort,
        onClose: () => {
          closeCalled = true;
        },
      }),
    );
    expect(closeCalled).toBe(true);
    expect(result.action).toBe("close_short");
    expect(result.message).toContain("reversed short");
  });

  it("returns hold when buy signal already long", async () => {
    const openLong = makeTrade({
      side: "buy",
      entry_price: "100",
      entry_at: new Date("2026-01-01T00:00:00Z").toISOString(),
    });
    let openCalled = false;
    let closeCalled = false;
    const result = await runEngine(
      baseConfig,
      makeMockApiClient({
        ...baseBacktest,
        signals: [makeSignal({ hints: { suggested_action: "buy" } })],
      }),
      makeMockBinanceClient(baseCandles),
      makeMockPaperRepo({
        openTrade: openLong,
        onOpen: () => {
          openCalled = true;
          return 99;
        },
        onClose: () => {
          closeCalled = true;
        },
      }),
    );
    expect(openCalled).toBe(false);
    expect(closeCalled).toBe(false);
    expect(result.action).toBe("hold");
  });

  it("does not persist trades in dryRun mode", async () => {
    let openCalled = false;
    let closeCalled = false;
    const result = await runEngine(
      { ...baseConfig, dryRun: true },
      makeMockApiClient({
        ...baseBacktest,
        signals: [makeSignal({ hints: { suggested_action: "buy" } })],
      }),
      makeMockBinanceClient(baseCandles),
      makeMockPaperRepo({
        onOpen: () => {
          openCalled = true;
          return 1;
        },
        onClose: () => {
          closeCalled = true;
        },
      }),
    );
    expect(openCalled).toBe(false);
    expect(closeCalled).toBe(false);
    expect(result.action).toBe("open_long");
  });

  it("wraps underlying errors in PaperTradingError", async () => {
    const errorApiClient = Layer.succeed(ApiClient, {
      generateAuthCode: () => Effect.die("not used"),
      getAIProviders: () => Effect.die("not used"),
      getAIModels: () => Effect.die("not used"),
      getPortfolio: () => Effect.die("not used"),
      getBalance: () => Effect.die("not used"),
      runScalpingBacktest: () =>
        Effect.fail(
          new NetworkError({ cause: "backend down", endpoint: "/backtest" }),
        ),
      health: () => Effect.die("not used"),
    });

    const exit = await Effect.runPromiseExit(
      Effect.gen(function* () {
        const engine = yield* PaperTradingEngine;
        return yield* engine.evaluateAndTrade(baseConfig);
      }).pipe(
        Effect.provide(
          Layer.mergeAll(
            errorApiClient,
            makeMockBinanceClient(baseCandles),
            makeMockPaperRepo({}),
            engineLayer,
          ),
        ),
      ),
    );
    if (exit._tag !== "Failure") {
      throw new Error(`expected Failure exit, got ${exit._tag}`);
    }
    const serialized = JSON.parse(JSON.stringify(exit.cause)) as {
      failure?: { _tag?: string; reason?: unknown };
    };
    expect(serialized.failure?._tag).toBe("PaperTradingError");
    const reason = String(serialized.failure?.reason ?? "");
    expect(reason).not.toBe("");
    expect(reason).toContain("backend down");
    expect(reason).toContain("/backtest");
  });

  it("uses the latest eligible signal, skipping a trailing rejected one", async () => {
    const result = await runEngine(
      baseConfig,
      makeMockApiClient({
        ...baseBacktest,
        signals: [
          makeSignal({
            timestamp: "2026-01-01T00:00:00Z",
            funnel_stage: "accepted",
            hints: { suggested_action: "buy" },
          }),
          makeSignal({
            signal_id: "sig-2",
            timestamp: "2026-01-01T01:00:00Z",
            funnel_stage: "rejected",
            rejection_reason: "no edge",
          }),
        ],
      }),
      makeMockBinanceClient(baseCandles),
      makeMockPaperRepo({}),
    );
    expect(result.action).toBe("open_long");
  });

  it("returns no_signal when every signal is non-eligible", async () => {
    const result = await runEngine(
      baseConfig,
      makeMockApiClient({
        ...baseBacktest,
        signals: [
          makeSignal({ funnel_stage: "rejected", rejection_reason: "no edge" }),
          makeSignal({
            signal_id: "sig-2",
            funnel_stage: "rejected",
            rejection_reason: "wide spread",
          }),
        ],
      }),
      makeMockBinanceClient(baseCandles),
      makeMockPaperRepo({}),
    );
    expect(result.action).toBe("no_signal");
  });
});
