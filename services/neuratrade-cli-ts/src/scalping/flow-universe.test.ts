import { describe, expect, it } from "bun:test";
import { selectFlowUniverse, type FlowInstrument } from "./flow-universe.js";

const now = Date.now();
const MS_PER_DAY = 86_400_000;

function instrument(
  symbol: string,
  ageDays: number,
  bid?: number,
  ask?: number,
): FlowInstrument {
  return {
    symbol,
    listedTime: now - ageDays * MS_PER_DAY,
    bid1Price: bid,
    ask1Price: ask,
  };
}

describe("selectFlowUniverse", () => {
  it("ranks by turnover24h desc and assigns ranks", () => {
    const volumes = { AUSDT: 10, BUSDT: 100, CUSDT: 50 };
    const symbols = [
      instrument("AUSDT", 200),
      instrument("BUSDT", 200),
      instrument("CUSDT", 200),
    ];

    const universe = selectFlowUniverse(volumes, symbols);

    expect(universe.map((e) => e.symbol)).toEqual(["BUSDT", "CUSDT", "AUSDT"]);
    expect(universe.map((e) => e.rank)).toEqual([1, 2, 3]);
  });

  it("drops symbols whose spread exceeds maxSpreadBps", () => {
    const volumes = { TIGHTUSDT: 100, WIDEUSDT: 90 };
    const symbols = [
      instrument("TIGHTUSDT", 200, 100, 100.04), // 4 bps
      instrument("WIDEUSDT", 200, 100, 100.2), // 20 bps
    ];

    const universe = selectFlowUniverse(volumes, symbols);

    expect(universe.map((e) => e.symbol)).toEqual(["TIGHTUSDT"]);
    expect(universe[0].spreadBps).toBeCloseTo(4, 5);
  });

  it("drops symbols younger than minAgeDays", () => {
    const volumes = { OLDUSDT: 100, NEWUSDT: 90 };
    const symbols = [instrument("OLDUSDT", 200), instrument("NEWUSDT", 5)];

    const universe = selectFlowUniverse(volumes, symbols);

    expect(universe.map((e) => e.symbol)).toEqual(["OLDUSDT"]);
  });

  it("uses defaultSpreadBps when no bid/ask quote exists", () => {
    const volumes = { NOQUOTEUSDT: 100, QUOTEDUSDT: 90 };
    const symbols = [
      instrument("NOQUOTEUSDT", 200), // no bid/ask
      instrument("QUOTEDUSDT", 200, 100, 100.2), // 20 bps > default 6
    ];

    const universe = selectFlowUniverse(volumes, symbols, undefined, {
      defaultSpreadBps: 6,
    });

    // Unquoted symbol passes (spread assumed 6 bps), quoted one is rejected.
    expect(universe.map((e) => e.symbol)).toEqual(["NOQUOTEUSDT"]);
    expect(universe[0].spreadBps).toBe(6);
  });

  it("carries always-include majors even below the cutoff", () => {
    const volumes = { BTCUSDT: 1, DOGEUSDT: 1 };
    const symbols = [instrument("BTCUSDT", 2000), instrument("DOGEUSDT", 2000)];

    const universe = selectFlowUniverse(volumes, symbols, undefined, {
      topN: 1,
      alwaysInclude: ["BTC", "DOGE"],
    });

    expect(universe).toHaveLength(2);
    expect(universe.map((e) => e.symbol)).toContain("BTCUSDT");
    expect(universe.map((e) => e.rank)).toEqual([1, 2]);
  });

  it("attaches funding rate when provided", () => {
    const volumes = { AUSDT: 10 };
    const symbols = [instrument("AUSDT", 200)];

    const universe = selectFlowUniverse(volumes, symbols, { AUSDT: 0.0001 });

    expect(universe[0].fundingRate).toBe(0.0001);
  });

  it("keeps entries sorted by turnover after always-include merge", () => {
    const volumes = { BTCUSDT: 1, DOGEUSDT: 500, ETHUSDT: 300 };
    const symbols = [
      instrument("BTCUSDT", 2000),
      instrument("DOGEUSDT", 2000),
      instrument("ETHUSDT", 2000),
    ];

    const universe = selectFlowUniverse(volumes, symbols, undefined, {
      topN: 1,
    });

    expect(universe.map((e) => e.symbol)).toEqual([
      "DOGEUSDT",
      "ETHUSDT",
      "BTCUSDT",
    ]);
  });

  it("keeps only live USDT-perpetual instruments", () => {
    const volumes = {
      BTCUSDT: 100,
      BTCUSDC: 90,
      HALTEDUSDT: 80,
    };
    const symbols: FlowInstrument[] = [
      { ...instrument("BTCUSDT", 2000), status: "Trading" },
      { ...instrument("BTCUSDC", 2000), status: "Trading" },
      { ...instrument("HALTEDUSDT", 2000), status: "Settling" },
    ];

    expect(selectFlowUniverse(volumes, symbols).map((e) => e.symbol)).toEqual([
      "BTCUSDT",
    ]);
  });
});
