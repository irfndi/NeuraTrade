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

/**
 * A position with finite, positive quantity, entry price, and available
 * amount can be adopted into local state (kind "adopt"); anything else must
 * be closed instead (kind "unadoptable").
 */
function isAdoptableExchangePosition(position: FuturesPosition): boolean {
  return (
    position.quantity.isFinite() &&
    position.quantity.greaterThan(0) &&
    position.entryPrice.isFinite() &&
    position.entryPrice.greaterThan(0) &&
    position.available.isFinite() &&
    position.available.greaterThan(0)
  );
}

/**
 * Mismatch reason for the first exchange field that differs from the
 * expected product type / margin mode / leverage / entry price, or null when
 * all expected fields match.
 */
function findExpectedFieldMismatch(
  exchangePosition: FuturesPosition,
  expected: LivePositionExpectation,
): string | null {
  if (exchangePosition.productType !== expected.productType) {
    return `exchange product type ${exchangePosition.productType} differs from expected ${expected.productType}`;
  }
  if (exchangePosition.marginMode !== expected.marginMode) {
    return `exchange margin mode ${exchangePosition.marginMode} differs from expected ${expected.marginMode}`;
  }
  if (exchangePosition.leverage !== expected.leverage) {
    return `exchange leverage ${exchangePosition.leverage} differs from expected ${expected.leverage}`;
  }
  if (!exchangePosition.entryPrice.equals(expected.entryPrice)) {
    return `exchange entry price ${exchangePosition.entryPrice.toString()} differs from expected ${expected.entryPrice.toString()}`;
  }
  return null;
}

/** True when the local state lacks the evidence required to trust a live entry. */
function lacksCompleteLiveEntryEvidence(
  state: Pick<
    GridPaperState,
    "side" | "entryFillSource" | "entryFilledQty" | "entryOrderId" | "entryFee"
  >,
): boolean {
  // ponytail: 8-way guard extracted to lower reconcileLivePosition from ~34 to ~12 CC
  // without reordering safety checks (order matters: side/state null, side match,
  // expected fields, fill-source proof, then quantity/available).
  if (state.entryFillSource !== "live" && state.entryFillSource !== "adopted")
    return true;
  if (state.entryOrderId === undefined || state.entryOrderId.length === 0)
    return true;
  if (state.entryFilledQty === undefined) return true;
  if (!state.entryFilledQty.isFinite()) return true;
  if (state.entryFilledQty.lessThanOrEqualTo(0)) return true;
  if (state.entryFee === undefined) return true;
  if (!state.entryFee.isFinite()) return true;
  if (state.entryFee.lessThan(0)) return true;
  return false;
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
    if (isAdoptableExchangePosition(exchangePosition)) {
      return { kind: "adopt", position: exchangePosition };
    }
    return {
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

  if (expected !== undefined) {
    const expectedMismatch = findExpectedFieldMismatch(
      exchangePosition,
      expected,
    );
    if (expectedMismatch !== null) {
      return { kind: "mismatch", reason: expectedMismatch };
    }
  }

  if (lacksCompleteLiveEntryEvidence(state)) {
    return {
      kind: "mismatch",
      reason: "local position lacks complete live entry evidence",
    };
  }

  if (
    !exchangePosition.quantity.isFinite() ||
    exchangePosition.quantity.lessThanOrEqualTo(0) ||
    state.entryFilledQty === undefined ||
    !exchangePosition.quantity.equals(state.entryFilledQty)
  ) {
    return {
      kind: "mismatch",
      reason: `local quantity ${state.entryFilledQty?.toString() ?? "undefined"} differs from exchange quantity ${exchangePosition.quantity.toString()}`,
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
