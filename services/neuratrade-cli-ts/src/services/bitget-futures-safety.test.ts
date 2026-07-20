import { describe, expect, it } from "bun:test";
import { Effect } from "effect";
import {
  BitgetFuturesSafetyError,
  validateLiveOrderSafety,
} from "./bitget-futures-safety.ts";
import type {
  BitgetFuturesOrderRequest,
  BitgetFuturesPosition,
} from "./bitget-client.ts";

const baseOrder = (
  overrides: Partial<BitgetFuturesOrderRequest> = {},
): BitgetFuturesOrderRequest => ({
  symbol: "BTC/USDT:USDT",
  productType: "USDT-FUTURES",
  side: "buy",
  orderType: "market",
  size: "0.001",
  ...overrides,
});

const basePosition = (
  overrides: Partial<BitgetFuturesPosition> = {},
): BitgetFuturesPosition => ({
  positionId: "1",
  symbol: "BTCUSDT",
  productType: "USDT-FUTURES",
  marginMode: "crossed",
  holdSide: "long",
  openPrice: "65000",
  total: "0.01",
  available: "0.01",
  leverage: "10",
  unrealizedPL: "0",
  liquidatedPrice: "0",
  ...overrides,
});

function run(
  order: BitgetFuturesOrderRequest,
  positions: ReadonlyArray<BitgetFuturesPosition> = [],
  leverageInfo: ReadonlyArray<{
    marginMode: "isolated" | "crossed";
    leverage: string;
  }> = [],
  intendedLeverage?: string,
) {
  return Effect.runPromise(
    validateLiveOrderSafety({
      order,
      positions,
      leverageInfo,
      intendedLeverage,
    }).pipe(
      Effect.map(() => ({ ok: true as const })),
      Effect.catch((err: BitgetFuturesSafetyError) =>
        Effect.succeed({ ok: false as const, error: err }),
      ),
    ),
  );
}

describe("BitgetFuturesSafety", () => {
  it("accepts a plain long order with no open position", async () => {
    const result = await run(baseOrder());
    expect(result.ok).toBe(true);
  });

  it("rejects zero/negative size", async () => {
    const result = await run(baseOrder({ size: "0" }));
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.error.reason).toContain("size must be positive");
  });

  it("rejects reduce-only buy without a short position", async () => {
    const result = await run(baseOrder({ side: "buy", reduceOnly: true }), [
      basePosition({ holdSide: "long" }),
    ]);
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.error.reason).toContain("no short position to reduce");
  });

  it("accepts reduce-only buy that closes an existing short", async () => {
    const result = await run(
      baseOrder({ side: "buy", reduceOnly: true, size: "0.005" }),
      [basePosition({ holdSide: "short", available: "0.01" })],
    );
    expect(result.ok).toBe(true);
  });

  it("rejects reduce-only buy larger than available short position", async () => {
    const result = await run(
      baseOrder({ side: "buy", reduceOnly: true, size: "0.02" }),
      [basePosition({ holdSide: "short", available: "0.01" })],
    );
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.error.reason).toContain("exceeds available");
  });

  it("rejects margin-mode mismatch with existing same-side position", async () => {
    const result = await run(baseOrder({ marginMode: "isolated" }), [
      basePosition({ holdSide: "long", marginMode: "crossed" }),
    ]);
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.error.reason).toContain("marginMode isolated conflicts");
  });

  it("accepts matching margin-mode with existing same-side position", async () => {
    const result = await run(baseOrder({ marginMode: "crossed" }), [
      basePosition({ holdSide: "long", marginMode: "crossed" }),
    ]);
    expect(result.ok).toBe(true);
  });

  it("rejects leverage mismatch", async () => {
    const result = await run(
      baseOrder({ marginMode: "crossed" }),
      [],
      [{ marginMode: "crossed", leverage: "5" }],
      "10",
    );
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.error.reason).toContain("leverage mismatch");
  });

  it("accepts matching leverage", async () => {
    const result = await run(
      baseOrder({ marginMode: "crossed" }),
      [],
      [{ marginMode: "crossed", leverage: "10" }],
      "10",
    );
    expect(result.ok).toBe(true);
  });
});
