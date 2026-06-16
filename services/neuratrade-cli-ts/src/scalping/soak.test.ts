import { describe, expect, it } from "bun:test";
import { Effect } from "effect";
import { defaultComposerConfig } from "./composer.js";
import {
  runSoak,
  type IterationRunner,
  type SoakOptions,
  type SoakSymbol,
} from "./soak.js";

function makeMockRunner(): IterationRunner {
  let callCount = 0;
  const capitalBySymbol = new Map<string, number>();

  return (symbol, _exchange, _params) => {
    callCount++;
    const isBtc = symbol.includes("BTC");

    if (!capitalBySymbol.has(symbol)) {
      capitalBySymbol.set(symbol, 10_000);
    }
    let capital = capitalBySymbol.get(symbol)!;

    const cycle = callCount % 4;
    const action =
      cycle === 2
        ? ("opened" as const)
        : cycle === 0
          ? ("closed" as const)
          : ("hold" as const);

    if (action === "opened") {
      capital = 9_900;
    } else if (action === "closed") {
      capital = isBtc ? 10_200 : 9_700;
    }

    capitalBySymbol.set(symbol, capital);
    return Effect.succeed({
      action,
      capital,
      note: `${symbol} iteration ${callCount}`,
    });
  };
}

function makeOptions(
  watchlist: readonly SoakSymbol[],
  iterationsPerSymbol = 4,
): SoakOptions {
  return {
    watchlist,
    iterationsPerSymbol,
    intervalSeconds: 0,
    isLive: false,
    initialCapital: 10_000,
    positionSizePct: 10,
    feePct: 0.1,
    minConfidence: 0.5,
    useAtrStops: false,
    atrStopMultiplier: 1.5,
    atrTakeProfitMultiplier: 2.5,
    holdUntilStop: false,
    regimeMode: "trend",
    composerConfig: defaultComposerConfig,
    leverage: 10,
    marginMode: "crossed",
    productType: "USDT-FUTURES",
  };
}

describe("runSoak", () => {
  it("handles empty watchlist", async () => {
    const result = await Effect.runPromise(
      runSoak(makeOptions([]), makeMockRunner()),
    );

    expect(result.perSymbolResults).toHaveLength(0);
    expect(result.aggregate.totalTrades).toBe(0);
    expect(result.aggregate.avgReturnPct).toBe(0);
    expect(result.aggregate.profitableCount).toBe(0);
    expect(result.aggregate.maxDrawdownPct).toBe(0);
    expect(result.aggregate.avgSharpeRatio).toBe(0);
  });

  it("processes 2+ symbols", async () => {
    const watchlist: readonly SoakSymbol[] = [
      { symbol: "BTC/USDT", exchange: "binance" },
      { symbol: "ETH/USDT", exchange: "binance" },
    ];

    const result = await Effect.runPromise(
      runSoak(makeOptions(watchlist), makeMockRunner()),
    );

    expect(result.perSymbolResults).toHaveLength(2);
    expect(result.perSymbolResults[0].symbol).toBe("BTC/USDT");
    expect(result.perSymbolResults[1].symbol).toBe("ETH/USDT");
  });

  it("computes aggregate metrics", async () => {
    const watchlist: readonly SoakSymbol[] = [
      { symbol: "BTC/USDT", exchange: "binance" },
      { symbol: "ETH/USDT", exchange: "binance" },
    ];

    const result = await Effect.runPromise(
      runSoak(makeOptions(watchlist), makeMockRunner()),
    );

    expect(result.aggregate.profitableCount).toBe(1);
    expect(result.aggregate.avgReturnPct).toBeCloseTo(-0.5, 1);
    expect(result.aggregate.maxDrawdownPct).toBeCloseTo(3.0, 1);
  });

  it("computes per-symbol metrics correctly", async () => {
    const watchlist: readonly SoakSymbol[] = [
      { symbol: "BTC/USDT", exchange: "binance" },
      { symbol: "ETH/USDT", exchange: "binance" },
    ];

    const result = await Effect.runPromise(
      runSoak(makeOptions(watchlist), makeMockRunner()),
    );

    const btc = result.perSymbolResults[0];
    expect(btc.symbol).toBe("BTC/USDT");
    expect(btc.exchange).toBe("binance");
    expect(btc.trades).toBe(1);
    expect(btc.finalCapital).toBe(10_200);
    expect(btc.totalReturnPct).toBeCloseTo(2.0, 1);
    expect(btc.maxDrawdownPct).toBeCloseTo(1.0, 1);
    expect(btc.winRate).toBe(1.0);

    const eth = result.perSymbolResults[1];
    expect(eth.symbol).toBe("ETH/USDT");
    expect(eth.trades).toBe(1);
    expect(eth.finalCapital).toBe(9_700);
    expect(eth.totalReturnPct).toBeCloseTo(-3.0, 1);
    expect(eth.maxDrawdownPct).toBeCloseTo(3.0, 1);
    expect(eth.winRate).toBe(0);
  });

  it("propagates runner errors", async () => {
    const failingRunner: IterationRunner = () =>
      Effect.fail(new Error("connection timeout"));

    const watchlist: readonly SoakSymbol[] = [
      { symbol: "BTC/USDT", exchange: "binance" },
    ];
    const result = await Effect.runPromise(
      runSoak(makeOptions(watchlist, 1), failingRunner).pipe(Effect.either),
    );

    expect(result._tag).toBe("Left");
  });
});
