import { describe, expect, it } from "bun:test";
import { money } from "../utils/money.js";
import {
  allocateLadderPortfolioCapital,
  planLadderPortfolioRebalance,
  summarizeLadderPortfolio,
} from "./ladder-portfolio.js";

describe("ladder shared portfolio", () => {
  it("partitions the account instead of multiplying capital per symbol", () => {
    const allocations = allocateLadderPortfolioCapital(
      [
        { key: "ENA", allocatedWeight: 0.5 },
        { key: "GPS", allocatedWeight: 0.5 },
      ],
      "50",
    );

    expect(allocations.get("ENA")?.toString()).toBe("25");
    expect(allocations.get("GPS")?.toString()).toBe("25");
    expect(
      [...allocations.values()]
        .reduce((sum, value) => sum.plus(value), money(0))
        .toString(),
    ).toBe("50");
  });

  it("normalizes legacy missing weights equally", () => {
    const allocations = allocateLadderPortfolioCapital(
      [{ key: "ENA" }, { key: "GPS" }, { key: "SOL" }],
      50,
    );
    expect(allocations.get("ENA")?.toDecimalPlaces(12).toString()).toBe(
      "16.666666666667",
    );
    expect(allocations.get("GPS")?.eq(allocations.get("SOL") ?? money(0))).toBe(
      true,
    );
  });

  it("aggregates realized and mark-to-market member state", () => {
    const summary = summarizeLadderPortfolio(
      "ladder:bybit-futures:15m:50",
      "bybit-futures",
      "15m",
      money(50),
      [
        {
          portfolioId: "ladder:bybit-futures:15m:50",
          exchange: "bybit-futures",
          symbol: "ENA/USDT:USDT",
          timeframe: "15m",
          allocatedCapital: money(25),
          capital: money("24.5"),
          equity: money("24.7"),
          unrealizedPnl: money("0.2"),
          active: true,
          updatedAt: new Date(),
        },
        {
          portfolioId: "ladder:bybit-futures:15m:50",
          exchange: "bybit-futures",
          symbol: "GPS/USDT:USDT",
          timeframe: "15m",
          allocatedCapital: money(25),
          capital: money("25.1"),
          equity: money("25"),
          unrealizedPnl: money("-0.1"),
          active: true,
          updatedAt: new Date(),
        },
      ],
    );

    expect(summary.capital.toString()).toBe("49.6");
    expect(summary.equity.toString()).toBe("49.7");
    expect(summary.unrealizedPnl.toString()).toBe("0.1");
    expect(summary.activeSymbols).toBe(2);
  });

  it("rebalances targets from live equity so winners fund the account", () => {
    const plans = planLadderPortfolioRebalance(
      [{ key: "ENA" }, { key: "GPS" }],
      new Map([
        ["ENA", 30],
        ["GPS", 20],
      ]),
    );
    // Total equity 50, equal weights -> $25 each. ENA is above target (30)
    // so it sizes down to ~83%; GPS is below target (20) and caps at 100%.
    expect(plans.get("ENA")?.targetAllocation.toString()).toBe("25");
    expect(plans.get("GPS")?.targetAllocation.toString()).toBe("25");
    expect(plans.get("ENA")?.positionPct).toBeCloseTo(83.333333, 4);
    expect(plans.get("GPS")?.positionPct).toBe(100);
  });

  it("refuses to rebalance on partial live-capital coverage", () => {
    const plans = planLadderPortfolioRebalance(
      [{ key: "ENA" }, { key: "GPS" }],
      new Map([["ENA", 30]]),
    );
    expect(plans.size).toBe(0);
  });

  it("rebalance respects explicit weights", () => {
    const plans = planLadderPortfolioRebalance(
      [
        { key: "ENA", allocatedWeight: 0.75 },
        { key: "GPS", allocatedWeight: 0.25 },
      ],
      new Map([
        ["ENA", 24],
        ["GPS", 26],
      ]),
    );
    expect(plans.get("ENA")?.targetAllocation.toString()).toBe("37.5");
    expect(plans.get("GPS")?.targetAllocation.toString()).toBe("12.5");
    // Below-target member wants 156% but clamps at 100% of its own capital.
    expect(plans.get("ENA")?.positionPct).toBe(100);
    // Above-target member sizes down to 12.5/26 ≈ 48%.
    expect(plans.get("GPS")?.positionPct).toBeCloseTo(48.0769, 3);
  });
});
