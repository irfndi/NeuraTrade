import type {
  FuturesPosition,
  FuturesProductType,
  FuturesMarginMode,
} from "../exchange/futures-adapter.js";
import type { GridPaperState } from "./types.js";

export type LivePositionReconciliation =
  | { readonly kind: "matched" }
  | {
      readonly kind: "adopt";
      /** The exchange position to adopt into local state. */
      readonly position: FuturesPosition;
    }
  | {
      readonly kind: "unadoptable";
      /** The exchange position that could not be adopted. */
      readonly position: FuturesPosition;
      readonly reason: string;
    }
  | { readonly kind: "mismatch"; readonly reason: string };

export interface LivePositionExpectation {
  readonly productType: FuturesProductType;
  readonly marginMode: FuturesMarginMode;
  readonly leverage: number;
  readonly entryPrice: FuturesPosition["entryPrice"];
}

export function reconcileLivePosition(
  state: Pick<
    GridPaperState,
    "side" | "entryFillSource" | "entryFilledQty" | "entryOrderId" | "entryFee"
  >,
  exchangePosition: FuturesPosition | null,
  expected?: LivePositionExpectation,
): LivePositionReconciliation {
  if (state.side === null) {
    if (exchangePosition === null) {
      return { kind: "matched" };
    }
    // The exchange holds a position the local state never recorded (e.g. a
    // fill was lost between placement and persistence). Adopt it so the
    // position is managed instead of tripping the kill switch forever. A
    // position with invalid quantity/price cannot be adopted and must be
    // closed instead (kind "unadoptable").
    const adoptable =
      exchangePosition.quantity.isFinite() &&
      exchangePosition.quantity.greaterThan(0) &&
      exchangePosition.entryPrice.isFinite() &&
      exchangePosition.entryPrice.greaterThan(0) &&
      exchangePosition.available.isFinite() &&
      exchangePosition.available.greaterThan(0);
    return adoptable
      ? { kind: "adopt", position: exchangePosition }
      : {
          kind: "unadoptable",
          position: exchangePosition,
          reason: `exchange position exists without local state and is not adoptable (${exchangePosition.side} ${exchangePosition.quantity.toString()} @ ${exchangePosition.entryPrice.toString()})`,
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
    expected !== undefined &&
    exchangePosition.productType !== expected.productType
  ) {
    return {
      kind: "mismatch",
      reason: `exchange product type ${exchangePosition.productType} differs from expected ${expected.productType}`,
    };
  }

  if (
    expected !== undefined &&
    exchangePosition.marginMode !== expected.marginMode
  ) {
    return {
      kind: "mismatch",
      reason: `exchange margin mode ${exchangePosition.marginMode} differs from expected ${expected.marginMode}`,
    };
  }

  if (
    expected !== undefined &&
    exchangePosition.leverage !== expected.leverage
  ) {
    return {
      kind: "mismatch",
      reason: `exchange leverage ${exchangePosition.leverage} differs from expected ${expected.leverage}`,
    };
  }

  if (
    expected !== undefined &&
    !exchangePosition.entryPrice.equals(expected.entryPrice)
  ) {
    return {
      kind: "mismatch",
      reason: `exchange entry price ${exchangePosition.entryPrice.toString()} differs from expected ${expected.entryPrice.toString()}`,
    };
  }

  if (
    (state.entryFillSource !== "live" && state.entryFillSource !== "adopted") ||
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

  if (
    !exchangePosition.available.isFinite() ||
    !exchangePosition.available.equals(exchangePosition.quantity)
  ) {
    return {
      kind: "mismatch",
      reason: `exchange available quantity ${exchangePosition.available.toString()} differs from total quantity ${exchangePosition.quantity.toString()}`,
    };
  }

  return { kind: "matched" };
}
