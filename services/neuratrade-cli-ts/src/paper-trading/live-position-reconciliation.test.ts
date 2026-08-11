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

  it("adopts an exchange position that exists without local state", () => {
    const position = makePosition("long", money("0.01"));
    const result = reconcileLivePosition(
      {
        side: null,
        entryFillSource: undefined,
        entryFilledQty: undefined,
        entryOrderId: undefined,
        entryFee: undefined,
      },
      position,
    );

    expect(result).toEqual({ kind: "adopt", position });
  });

  it("rejects an unadoptable exchange position (invalid quantity)", () => {
    const position = makePosition("long", money("0"));
    const result = reconcileLivePosition(
      {
        side: null,
        entryFillSource: undefined,
        entryFilledQty: undefined,
        entryOrderId: undefined,
        entryFee: undefined,
      },
      position,
    );

    expect(result.kind).toBe("unadoptable");
    expect(result).toMatchObject({
      kind: "unadoptable",
      position,
      reason: expect.stringContaining("not adoptable"),
    });
  });

  it("accepts an adopted local position as live-bound evidence", () => {
    const quantity = money("0.01");

    expect(
      reconcileLivePosition(
        {
          side: "long",
          entryFillSource: "adopted",
          entryFilledQty: quantity,
          entryOrderId: "adopted",
          entryFee: money(0),
        },
        makePosition("long", quantity),
      ),
    ).toEqual({ kind: "matched" });
  });

  it("rejects local state without an exchange position", () => {
    const result = reconcileLivePosition(
      {
        side: "long",
        entryFillSource: "live",
        entryFilledQty: money("0.01"),
        entryOrderId: "entry-1",
        entryFee: money(0),
      },
      null,
    );

    expect(result).toEqual({
      kind: "mismatch",
      reason: "local state exists without exchange position",
    });
  });

  it("rejects a side mismatch", () => {
    const quantity = money("0.01");
    const result = reconcileLivePosition(
      {
        side: "long",
        entryFillSource: "live",
        entryFilledQty: quantity,
        entryOrderId: "entry-1",
        entryFee: money(0),
      },
      makePosition("short", quantity),
    );

    expect(result).toEqual({
      kind: "mismatch",
      reason:
        "local long position differs from exchange short position",
    });
  });

  it("rejects a margin mode mismatch", () => {
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
        productType: "USDT-FUTURES",
        marginMode: "crossed",
        leverage: 1,
        entryPrice: money("70000"),
      },
    );

    expect(result).toEqual({
      kind: "mismatch",
      reason:
        "exchange margin mode isolated differs from expected crossed",
    });
  });

  it("rejects a leverage mismatch", () => {
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
        productType: "USDT-FUTURES",
        marginMode: "isolated",
        leverage: 5,
        entryPrice: money("70000"),
      },
    );

    expect(result).toEqual({
      kind: "mismatch",
      reason: "exchange leverage 1 differs from expected 5",
    });
  });

  it("rejects an entry price mismatch", () => {
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
        productType: "USDT-FUTURES",
        marginMode: "isolated",
        leverage: 1,
        entryPrice: money("71000"),
      },
    );

    expect(result).toEqual({
      kind: "mismatch",
      reason:
        "exchange entry price 70000 differs from expected 71000",
    });
  });

  it("rejects an available != quantity position", () => {
    const quantity = money("0.01");
    const position = makePosition("long", quantity);
    const result = reconcileLivePosition(
      {
        side: "long",
        entryFillSource: "live",
        entryFilledQty: quantity,
        entryOrderId: "entry-1",
        entryFee: money(0),
      },
      { ...position, available: money("0.005") },
    );

    expect(result).toEqual({
      kind: "mismatch",
      reason:
        "exchange available quantity 0.005 differs from total quantity 0.01",
    });
  });
});
