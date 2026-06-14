import { describe, expect, it } from "bun:test";
import type { RawCandle } from "./binance-client.ts";
import type { PaperTrade } from "./paper-repository.ts";
import {
  calculateClosePnl,
  calculatePositionSize,
  checkExit,
  stopLossPrice,
  takeProfitPrice,
} from "./paper-trading-engine.ts";
import type { PaperTradingConfig } from "./paper-trading-engine.ts";

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
