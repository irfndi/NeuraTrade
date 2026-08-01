import type { FuturesPosition } from "../exchange/futures-adapter.js";
import type { GridPaperState } from "./types.js";

export type LivePositionReconciliation =
  | { readonly kind: "matched" }
  | { readonly kind: "mismatch"; readonly reason: string };

export function reconcileLivePosition(
  state: Pick<
    GridPaperState,
    "side" | "entryFillSource" | "entryFilledQty" | "entryOrderId" | "entryFee"
  >,
  exchangePosition: FuturesPosition | null,
): LivePositionReconciliation {
  if (state.side === null) {
    return exchangePosition === null
      ? { kind: "matched" }
      : {
          kind: "mismatch",
          reason: `exchange position exists without local state (${exchangePosition.side} ${exchangePosition.quantity.toString()})`,
        };
  }

  if (exchangePosition === null) {
    return {
      kind: "mismatch",
      reason: "local state exists without exchange position",
    };
  }

  if (exchangePosition.side !== state.side) {
    return {
      kind: "mismatch",
      reason: `local ${state.side} position differs from exchange ${exchangePosition.side} position`,
    };
  }

  if (
    state.entryFillSource !== "live" ||
    state.entryOrderId === undefined ||
    state.entryOrderId.length === 0 ||
    state.entryFilledQty === undefined ||
    !state.entryFilledQty.isFinite() ||
    state.entryFilledQty.lessThanOrEqualTo(0) ||
    state.entryFee === undefined ||
    !state.entryFee.isFinite() ||
    state.entryFee.lessThan(0)
  ) {
    return {
      kind: "mismatch",
      reason: "local position lacks complete live entry evidence",
    };
  }

  if (
    !exchangePosition.quantity.isFinite() ||
    exchangePosition.quantity.lessThanOrEqualTo(0) ||
    !exchangePosition.quantity.equals(state.entryFilledQty)
  ) {
    return {
      kind: "mismatch",
      reason: `local quantity ${state.entryFilledQty.toString()} differs from exchange quantity ${exchangePosition.quantity.toString()}`,
    };
  }

  return { kind: "matched" };
}
