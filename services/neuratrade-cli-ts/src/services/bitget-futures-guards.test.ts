import { describe, expect, it } from "bun:test";
import { Effect } from "effect";
import {
  BitgetFuturesGuardError,
  validateFuturesOrder,
} from "./bitget-futures-guards.ts";
import type {
  BitgetContract,
  BitgetFuturesBalance,
  BitgetFuturesOrderRequest,
} from "./bitget-client.ts";

const contract: BitgetContract = {
  symbol: "BTCUSDT",
  baseCoin: "BTC",
  quoteCoin: "USDT",
  productType: "USDT-FUTURES",
  status: "online",
  symbolStatus: "online",
  pricePrecision: "2",
  quantityPrecision: "3",
  minTradeAmount: "5",
  minTradeNum: "0.0001",
  minTradeUSDT: "5",
  maxLeverage: "125",
  minLeverage: "1",
  takerFeeRate: "0.0006",
  makerFeeRate: "0.0002",
};

const balances: ReadonlyArray<BitgetFuturesBalance> = [
  {
    marginCoin: "USDT",
    available: "1000",
    locked: "0",
    equity: "1000",
    usdtEquity: "1000",
  },
];

function run(
  order: BitgetFuturesOrderRequest,
  leverage = "10",
  lastPrice = "65000",
) {
  return Effect.runPromise(
    validateFuturesOrder({
      order,
      contract,
      balances,
      lastPrice,
      leverage,
    }).pipe(
      Effect.map((r) => ({ ok: true as const, result: r })),
      Effect.catchAll((err) =>
        Effect.succeed({ ok: false as const, error: err }),
      ),
    ),
  );
}

describe("BitgetFuturesGuards", () => {
  it("accepts a valid long order", async () => {
    const result = await run({
      symbol: "BTC/USDT:USDT",
      productType: "USDT-FUTURES",
      side: "buy",
      orderType: "market",
      size: "0.001",
    });
    if (!result.ok) throw new Error(result.error.reason);
    expect(result.result.notional).toBe("65");
    expect(result.result.marginRequired).toBe("6.5");
  });

  it("rejects leverage above max", async () => {
    const result = await run(
      {
        symbol: "BTC/USDT:USDT",
        productType: "USDT-FUTURES",
        side: "buy",
        orderType: "market",
        size: "0.001",
      },
      "200",
    );
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.error.reason).toContain("above maximum");
  });

  it("rejects notional below min", async () => {
    const result = await run({
      symbol: "BTC/USDT:USDT",
      productType: "USDT-FUTURES",
      side: "buy",
      orderType: "market",
      size: "0.00001",
    });
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.error.reason).toContain("min trade amount");
  });

  it("rejects insufficient margin", async () => {
    const smallBalances: ReadonlyArray<BitgetFuturesBalance> = [
      {
        marginCoin: "USDT",
        available: "1",
        locked: "0",
        equity: "1",
        usdtEquity: "1",
      },
    ];
    const result = await Effect.runPromise(
      validateFuturesOrder({
        order: {
          symbol: "BTC/USDT:USDT",
          productType: "USDT-FUTURES",
          side: "buy",
          orderType: "market",
          size: "0.01",
        },
        contract,
        balances: smallBalances,
        lastPrice: "65000",
        leverage: "10",
      }).pipe(
        Effect.map(() => ({ ok: true as const })),
        Effect.catchAll((err: BitgetFuturesGuardError) =>
          Effect.succeed({ ok: false as const, error: err }),
        ),
      ),
    );
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.error.reason).toContain("insufficient USDT margin");
  });

  it("rejects contract mismatch", async () => {
    const result = await run({
      symbol: "ETH/USDT:USDT",
      productType: "USDT-FUTURES",
      side: "buy",
      orderType: "market",
      size: "0.001",
    });
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.error.reason).toContain("mismatch");
  });
});
