/**
 * Bitget futures live-order safety checks.
 *
 * Runs immediately before a real order is sent (and during dry-runs) to catch
 * account-state mismatches that are easy to make with Bitget v2 futures:
 *   - reduce-only orders without an opposing position
 *   - margin-mode mismatch against an existing position
 *   - leverage mismatch against the intended margin mode
 *
 * These checks are intentionally conservative. Use --force to bypass when you
 * are deliberately managing the account state manually.
 */
import { Data, Effect } from "effect";
import type {
  BitgetFuturesOrderRequest,
  BitgetFuturesPosition,
  BitgetMarginMode,
} from "./bitget-client.ts";
import { isDecimalString } from "./decimal-string.ts";

export class BitgetFuturesSafetyError extends Data.TaggedError(
  "BitgetFuturesSafetyError",
)<{
  readonly reason: string;
}> {}

export interface BitgetFuturesSafetyContext {
  readonly order: BitgetFuturesOrderRequest;
  readonly positions: ReadonlyArray<BitgetFuturesPosition>;
  readonly leverageInfo: ReadonlyArray<{
    readonly marginMode: BitgetMarginMode;
    readonly leverage: string;
  }>;
  readonly intendedLeverage?: string;
}

export function validateLiveOrderSafety(
  ctx: BitgetFuturesSafetyContext,
): Effect.Effect<void, BitgetFuturesSafetyError> {
  return Effect.gen(function* () {
    const { order, positions, leverageInfo, intendedLeverage } = ctx;
    const bsymbol = order.symbol.replace("/", "").split(":")[0].toUpperCase();
    const relevant = positions.filter(
      (p) => p.symbol.toUpperCase() === bsymbol,
    );

    yield* requireDecimal(order.size, "order size");
    if (order.price !== undefined) {
      yield* requireDecimal(order.price, "order price");
    }
    if (intendedLeverage !== undefined) {
      yield* requireDecimal(intendedLeverage, "intended leverage");
    }
    for (const position of relevant) {
      yield* requireDecimal(position.available, "position available size");
    }
    for (const leverage of leverageInfo) {
      yield* requireDecimal(leverage.leverage, "account leverage");
    }

    if (compare(order.size, "0") <= 0) {
      return yield* safetyFail("order size must be positive");
    }

    // Reduce-only orders must actually reduce an existing position.
    if (order.reduceOnly) {
      const neededSide = order.side === "buy" ? "short" : "long";
      const reducible = relevant.filter((p) => p.holdSide === neededSide);
      const available = reducible.reduce(
        (sum, p) => addStrings(sum, p.available),
        "0",
      );
      if (reducible.length === 0) {
        return yield* safetyFail(
          `reduce-only ${order.side} order rejected: no ${neededSide} position to reduce`,
        );
      }
      if (compare(order.size, available) > 0) {
        return yield* safetyFail(
          `reduce-only ${order.side} order size ${order.size} exceeds available ${neededSide} position ${available}`,
        );
      }
      return;
    }

    // If we already hold the same side, the margin mode must match or Bitget
    // will reject/behaviour will be surprising.
    if (order.marginMode) {
      const sameSide = relevant.filter(
        (p) =>
          (p.holdSide === "long" && order.side === "buy") ||
          (p.holdSide === "short" && order.side === "sell"),
      );
      for (const pos of sameSide) {
        if (pos.marginMode !== order.marginMode) {
          return yield* safetyFail(
            `order marginMode ${order.marginMode} conflicts with existing ${pos.holdSide} position marginMode ${pos.marginMode}`,
          );
        }
      }
    }

    // Verify the account leverage matches the caller's intent.
    if (intendedLeverage && order.marginMode) {
      const current = leverageInfo.find(
        (l) => l.marginMode === order.marginMode,
      );
      if (current && compare(current.leverage, intendedLeverage) !== 0) {
        return yield* safetyFail(
          `leverage mismatch for ${order.marginMode}: account ${current.leverage}x vs intended ${intendedLeverage}x (set leverage first)`,
        );
      }
    }
  });
}

function safetyFail(reason: string) {
  return Effect.fail(new BitgetFuturesSafetyError({ reason }));
}

function requireDecimal(value: string, field: string) {
  return isDecimalString(value)
    ? Effect.void
    : safetyFail(`${field} must be a decimal string`);
}

// Minimal decimal string helpers (BigInt-based to avoid float money errors).
function countDecimals(value: string): number {
  const trimmed = value.trim();
  const dotIndex = trimmed.indexOf(".");
  if (dotIndex === -1) return 0;
  return trimmed.length - dotIndex - 1;
}

function toScaled(value: string, scale: number): bigint {
  const trimmed = value.trim();
  const [intPart = "0", fracPart = ""] = trimmed.split(".");
  const paddedFrac = fracPart.padEnd(scale, "0").slice(0, scale);
  const sign = intPart.startsWith("-") ? "-" : "";
  const absInt = sign ? intPart.slice(1) : intPart;
  return BigInt(`${sign}${absInt}${paddedFrac}`);
}

function addStrings(a: string, b: string): string {
  const scale = Math.max(countDecimals(a), countDecimals(b));
  const sum = toScaled(a, scale) + toScaled(b, scale);
  return fromScaled(sum, scale);
}

function fromScaled(value: bigint, scale: number): string {
  if (scale === 0) return value.toString();
  const negative = value < 0n;
  const abs = negative ? -value : value;
  const str = abs.toString().padStart(scale + 1, "0");
  const intPart = str.slice(0, -scale) || "0";
  const fracPart = str.slice(-scale).replace(/0+$/, "");
  return `${negative ? "-" : ""}${intPart}${fracPart ? `.${fracPart}` : ""}`;
}

function compare(a: string, b: string): number {
  const scale = Math.max(countDecimals(a), countDecimals(b));
  const sa = toScaled(a, scale);
  const sb = toScaled(b, scale);
  if (sa < sb) return -1;
  if (sa > sb) return 1;
  return 0;
}
