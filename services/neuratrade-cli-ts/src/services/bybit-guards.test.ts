import { describe, expect, it } from "bun:test";
import { Effect } from "effect";
import { BybitGuardError, validateOrder } from "./bybit-guards.ts";
import type {
  BybitBalance,
  BybitGuardContract,
  BybitGuardOrder,
} from "./bybit-guards.ts";

const btcContract: BybitGuardContract = {
  symbol: "BTCUSDT",
  status: "Trading",
  minOrderQty: "0.0001",
  qtyStep: "0.0001",
  minOrderAmt: "5",
  tickSize: "0.1",
  maxLeverage: "100",
};

const balances: ReadonlyArray<BybitBalance> = [
  { asset: "USDT", available: "1000", walletBalance: "1000", equity: "1100" },
];

type GuardRunResult =
  | { readonly ok: true; readonly order: BybitGuardOrder }
  | { readonly ok: false; readonly error: BybitGuardError };

function run(
  order: BybitGuardOrder,
  price: string,
  contract: BybitGuardContract = btcContract,
  bal: ReadonlyArray<BybitBalance> = balances,
): Promise<GuardRunResult> {
  return Effect.runPromise(
    validateOrder(
      { order, contract, balances: bal, feeRate: "0.0006" },
      price,
    ).pipe(
      Effect.map((normalized): GuardRunResult => ({
        ok: true,
        order: normalized,
      })),
      Effect.catch((err): Effect.Effect<GuardRunResult, never> =>
        Effect.succeed({ ok: false, error: err }),
      ),
    ),
  );
}

describe("BybitGuards", () => {
  it("accepts a valid sell order and normalizes size precision", async () => {
    const result = await run(
      {
        symbol: "BTCUSDT",
        side: "sell",
        orderType: "market",
        size: "0.00123456",
      },
      "65000",
    );
    if (!result.ok) throw new Error(`unexpected error: ${result.error.reason}`);
    expect(result.order.size).toBe("0.0012");
  });

  it("rejects a contract mismatch", async () => {
    const result = await run(
      { symbol: "ETHUSDT", side: "sell", orderType: "market", size: "0.001" },
      "65000",
    );
    expect(result.ok).toBe(false);
    if (!result.ok) expect(result.error.reason).toContain("mismatch");
  });

  it("rejects a non-tradable contract", async () => {
    const offline = { ...btcContract, status: "PreLaunch" };
    const result = await run(
      { symbol: "BTCUSDT", side: "sell", orderType: "market", size: "0.001" },
      "65000",
      offline,
    );
    expect(result.ok).toBe(false);
    if (!result.ok) expect(result.error.reason).toContain("not tradable");
  });

  it("rejects a size below the base qty minimum", async () => {
    // qtyStep 0.000001 keeps 0.00005 expressible, but minOrderQty 0.0001 still
    // rejects it (base-qty floor, distinct from the step-rounding rejection).
    const finer = { ...btcContract, qtyStep: "0.000001" };
    const result = await run(
      { symbol: "BTCUSDT", side: "sell", orderType: "market", size: "0.00005" },
      "65000",
      finer,
    );
    expect(result.ok).toBe(false);
    if (!result.ok) expect(result.error.reason).toContain("min order qty");
  });

  it("rejects a notional below minOrderAmt", async () => {
    // 0.0001 BTC * 40000 = 4 USDT < floor 5.
    const result = await run(
      { symbol: "BTCUSDT", side: "sell", orderType: "market", size: "0.0001" },
      "40000",
    );
    expect(result.ok).toBe(false);
    if (!result.ok)
      expect(result.error.reason).toContain("below min order amt");
  });

  it("rejects a notional above the max order bound", async () => {
    const capped = { ...btcContract, maxOrderNotional: "100000" };
    const result = await run(
      { symbol: "BTCUSDT", side: "sell", orderType: "market", size: "2" },
      "65000",
      capped,
    );
    expect(result.ok).toBe(false);
    if (!result.ok) expect(result.error.reason).toContain("above max");
  });

  it("rejects insufficient USDT margin for a leveraged buy", async () => {
    // size 1 BTC * 65000 / leverage 1 = 65000 margin > 1000 available.
    const result = await run(
      {
        symbol: "BTCUSDT",
        side: "buy",
        orderType: "market",
        size: "1",
        leverage: 1,
      },
      "65000",
    );
    expect(result.ok).toBe(false);
    if (!result.ok)
      expect(result.error.reason).toContain("insufficient USDT margin");
  });

  it("accepts a leveraged order whose margin fits the balance", async () => {
    // size 0.01 BTC * 65000 / leverage 10 = 65 margin < 1000 available.
    const result = await run(
      {
        symbol: "BTCUSDT",
        side: "buy",
        orderType: "market",
        size: "0.01",
        leverage: 10,
      },
      "65000",
    );
    if (!result.ok) throw new Error(`unexpected error: ${result.error.reason}`);
    expect(result.order.size).toBe("0.01");
  });

  it("rejects a zero/negative size after rounding", async () => {
    const result = await run(
      {
        symbol: "BTCUSDT",
        side: "sell",
        orderType: "market",
        size: "0.00000001",
      },
      "65000",
    );
    expect(result.ok).toBe(false);
    if (!result.ok)
      expect(result.error.reason).toContain("rounds below qtyStep");
  });

  it("rejects a limit order without a positive price", async () => {
    const result = await run(
      { symbol: "BTCUSDT", side: "buy", orderType: "limit", size: "0.001" },
      "65000",
    );
    expect(result.ok).toBe(false);
    if (!result.ok)
      expect(result.error.reason).toContain("requires positive price");
  });

  it("normalizes limit price to the tick precision", async () => {
    const result = await run(
      {
        symbol: "BTCUSDT",
        side: "buy",
        orderType: "limit",
        size: "0.001",
        price: "64999.999",
      },
      "65000",
    );
    if (!result.ok) throw new Error(`unexpected error: ${result.error.reason}`);
    expect(result.order.price).toBe("64999.9");
  });

  it("uses the wallet balance when available is blank", async () => {
    const bal: ReadonlyArray<BybitBalance> = [
      { asset: "USDT", available: "", walletBalance: "5000" },
    ];
    const result = await run(
      {
        symbol: "BTCUSDT",
        side: "buy",
        orderType: "market",
        size: "0.01",
        leverage: 10,
      },
      "65000",
      btcContract,
      bal,
    );
    if (!result.ok) throw new Error(`unexpected error: ${result.error.reason}`);
    expect(result.order.size).toBe("0.01");
  });
});
