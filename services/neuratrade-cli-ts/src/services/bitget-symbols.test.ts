import { describe, expect, it } from "bun:test";
import { toBitgetFuturesSymbol, toBitgetSymbol } from "./bitget-client.ts";

describe("toBitgetSymbol", () => {
  const cases: Array<[string, string]> = [
    ["BTC/USDT", "BTCUSDT"],
    ["btc/usdt", "BTCUSDT"],
    ["ETH/BTC", "ETHBTC"],
    ["SOL/USDC", "SOLUSDC"],
  ];

  for (const [input, expected] of cases) {
    it(`normalizes ${input} to ${expected}`, () => {
      expect(toBitgetSymbol(input)).toBe(expected);
    });
  }
});

describe("toBitgetFuturesSymbol", () => {
  const cases: Array<
    [
      string,
      {
        symbol: string;
        productType: import("./bitget-client.ts").BitgetProductType;
      },
    ]
  > = [
    ["BTC/USDT:USDT", { symbol: "BTCUSDT", productType: "USDT-FUTURES" }],
    ["BTC/USDT", { symbol: "BTCUSDT", productType: "USDT-FUTURES" }],
    ["ETH/USD:USD", { symbol: "ETHUSD", productType: "COIN-FUTURES" }],
    ["ETH/USDC:USDC", { symbol: "ETHUSDC", productType: "USDC-FUTURES" }],
  ];

  for (const [input, expected] of cases) {
    it(`normalizes ${input}`, () => {
      expect(toBitgetFuturesSymbol(input)).toEqual(expected);
    });
  }

  it("respects explicit default product type", () => {
    expect(toBitgetFuturesSymbol("BTC/USDT", "COIN-FUTURES")).toEqual({
      symbol: "BTCUSDT",
      productType: "COIN-FUTURES",
    });
  });
});
