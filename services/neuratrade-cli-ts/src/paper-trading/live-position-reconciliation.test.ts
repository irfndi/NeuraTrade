import { describe, expect, it } from "bun:test";
import * as fc from "fast-check";
import type { FuturesPosition } from "../exchange/futures-adapter.js";
import { money } from "../utils/money.js";
import { reconcileLivePosition } from "./live-position-reconciliation.js";

function makePosition(
  side: "long" | "short",
  quantity: ReturnType<typeof money>,
): FuturesPosition {
  return {
    symbol: "BTC/USDT:USDT",
    side,
    productType: "USDT-FUTURES",
    marginMode: "isolated",
    leverage: 1,
    quantity,
    available: quantity,
    entryPrice: money(70_000),
    marginCoin: "USDT",
  };
}

describe("reconcileLivePosition", () => {
  it("matches flat local and exchange state", () => {
    expect(
      reconcileLivePosition(
        {
          side: null,
          entryFillSource: undefined,
          entryFilledQty: undefined,
          entryOrderId: undefined,
          entryFee: undefined,
        },
        null,
      ),
    ).toEqual({ kind: "matched" });
  });

  it("rejects a local position without complete live evidence", () => {
    const position = makePosition("long", money("0.01"));

    expect(
      reconcileLivePosition(
        {
          side: "long",
          entryFillSource: "simulated",
          entryFilledQty: money("0.01"),
          entryOrderId: "entry-1",
          entryFee: money(0),
        },
        position,
      ),
    ).toEqual({
      kind: "mismatch",
      reason: "local position lacks complete live entry evidence",
    });
  });

  it("accepts a live position only when side and quantity match", () => {
    const quantity = money("0.01");

    expect(
      reconcileLivePosition(
        {
          side: "short",
          entryFillSource: "live",
          entryFilledQty: quantity,
          entryOrderId: "entry-1",
          entryFee: money(0),
        },
        makePosition("short", quantity),
      ),
    ).toEqual({ kind: "matched" });
  });

  it("rejects a same-size position with mismatched account contract state", () => {
    const quantity = money("0.01");
    const result = reconcileLivePosition(
      {
        side: "long",
        entryFillSource: "live",
        entryFilledQty: quantity,
        entryOrderId: "entry-1",
        entryFee: money(0),
      },
      makePosition("long", quantity),
      {
        productType: "COIN-FUTURES",
        marginMode: "isolated",
        leverage: 3,
        entryPrice: money("70000"),
      },
    );

    expect(result).toEqual({
      kind: "mismatch",
      reason:
        "exchange product type USDT-FUTURES differs from expected COIN-FUTURES",
    });
  });

  it("rejects every generated quantity mismatch", () => {
    fc.assert(
      fc.property(fc.integer({ min: 1, max: 1_000_000 }), (rawQuantity) => {
        const localQuantity = money(rawQuantity).div(1000);
        const exchangeQuantity = localQuantity.plus("0.001");
        const result = reconcileLivePosition(
          {
            side: "long",
            entryFillSource: "live",
            entryFilledQty: localQuantity,
            entryOrderId: "entry-1",
            entryFee: money(0),
          },
          makePosition("long", exchangeQuantity),
        );

        expect(result.kind).toBe("mismatch");
      }),
    );
  });

  it("rejects a position whose live entry audit fields are incomplete", () => {
    const quantity = money("0.01");

    expect(
      reconcileLivePosition(
        {
          side: "long",
          entryFillSource: "live",
          entryFilledQty: quantity,
          entryOrderId: undefined,
          entryFee: money(0),
        },
        makePosition("long", quantity),
      ),
    ).toEqual({
      kind: "mismatch",
      reason: "local position lacks complete live entry evidence",
    });
  });
});
