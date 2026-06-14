import { describe, expect, it } from "bun:test";
import { Effect } from "effect";
import { BitgetGuardError, validateOrder } from "./bitget-guards.ts";
import type {
  BitgetBalance,
  BitgetInstrument,
  BitgetOrderRequest,
} from "./bitget-client.ts";

const btcInstrument: BitgetInstrument = {
  symbol: "BTCUSDT",
  baseCoin: "BTC",
  quoteCoin: "USDT",
  status: "online",
  minTradeAmount: "5",
  maxTradeAmount: "1000000",
  takerFeeRate: "0.001",
  makerFeeRate: "0.001",
  pricePrecision: "2",
  quantityPrecision: "6",
  quotePrecision: "6",
};

const balances: ReadonlyArray<BitgetBalance> = [
  { asset: "BTC", available: "0.1", frozen: "0" },
  { asset: "USDT", available: "1000", frozen: "0" },
];

type GuardRunResult =
  | { readonly ok: true; readonly order: BitgetOrderRequest }
  | { readonly ok: false; readonly error: BitgetGuardError };

function run(
  order: BitgetOrderRequest,
  price: string,
  instrument: BitgetInstrument = btcInstrument,
): Promise<GuardRunResult> {
  return Effect.runPromise(
    validateOrder(
      { order, instrument, balances, feeRate: "0.001" },
      price,
    ).pipe(
      Effect.map(
        (normalized): GuardRunResult => ({ ok: true, order: normalized }),
      ),
      Effect.catchAll(
        (err): Effect.Effect<GuardRunResult, never> =>
          Effect.succeed({ ok: false, error: err }),
      ),
    ),
  );
}

describe("BitgetGuards", () => {
  it("accepts a valid sell order and normalizes size", async () => {
    const result = await run(
      {
        symbol: "BTC/USDT",
        side: "sell",
        orderType: "market",
        size: "0.00123456",
      },
      "65000",
    );
    if (!result.ok) {
      throw new Error(`unexpected error: ${result.error.reason}`);
    }
    expect(result.order.size).toBe("0.001234");
  });

  it("rejects an instrument mismatch", async () => {
    const result = await run(
      { symbol: "ETH/USDT", side: "sell", orderType: "market", size: "0.001" },
      "65000",
    );
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.error.reason).toContain("mismatch");
  });

  it("rejects size below notional minimum", async () => {
    const result = await run(
      {
        symbol: "BTC/USDT",
        side: "sell",
        orderType: "market",
        size: "0.00001",
      },
      "65000",
    );
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.error.reason).toContain("min trade amount");
  });

  it("rejects insufficient base balance for sell", async () => {
    const result = await run(
      { symbol: "BTC/USDT", side: "sell", orderType: "market", size: "1" },
      "65000",
    );
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.error.reason).toContain("insufficient BTC");
  });

  it("rejects insufficient quote balance for buy", async () => {
    const result = await run(
      { symbol: "BTC/USDT", side: "buy", orderType: "market", size: "2000" },
      "65000",
    );
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.error.reason).toContain("insufficient USDT");
  });

  it("rejects limit order without price", async () => {
    const result = await run(
      { symbol: "BTC/USDT", side: "buy", orderType: "limit", size: "0.001" },
      "65000",
    );
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.error.reason).toContain(
      "limit order requires positive price",
    );
  });

  it("normalizes limit price precision", async () => {
    const result = await run(
      {
        symbol: "BTC/USDT",
        side: "buy",
        orderType: "limit",
        size: "0.001",
        price: "64999.999",
      },
      "65000",
    );
    if (!result.ok) {
      throw new Error(`unexpected error: ${result.error.reason}`);
    }
    expect(result.order.price).toBe("64999.99");
  });

  it("rejects offline instrument", async () => {
    const offline = { ...btcInstrument, status: "offline" };
    const result = await run(
      { symbol: "BTC/USDT", side: "sell", orderType: "market", size: "0.001" },
      "65000",
      offline,
    );
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.error.reason).toContain("not tradable");
  });
});
