/**
 * Bitget pre-trade guards.
 *
 * Validates orders against exchange instrument rules and account balances
 * before any signed network request is sent. All monetary math uses scaled
 * integers (BigInt) to avoid floating-point money errors.
 */
import { Data, Effect } from "effect";
import type {
  BitgetBalance,
  BitgetInstrument,
  BitgetOrderRequest,
} from "./bitget-client.ts";
import { add } from "./decimal.ts";

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

export class BitgetGuardError extends Data.TaggedError("BitgetGuardError")<{
  readonly reason: string;
}> {}

// ---------------------------------------------------------------------------
// Decimal helpers (scaled integer math)
// ---------------------------------------------------------------------------

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

function fromScaled(value: bigint, scale: number): string {
  const negative = value < 0n;
  const abs = negative ? -value : value;
  if (scale === 0) {
    return `${negative ? "-" : ""}${abs.toString()}`;
  }
  const str = abs.toString().padStart(scale + 1, "0");
  const intPart = str.slice(0, -scale) || "0";
  const fracPart = str.slice(-scale).replace(/0+$/, "");
  return `${negative ? "-" : ""}${intPart}${fracPart ? `.${fracPart}` : ""}`;
}

function roundDown(value: string, precision: number): string {
  const scale = Math.max(countDecimals(value), precision);
  const scaled = toScaled(value, scale);
  const factor = 10n ** BigInt(scale - precision);
  const rounded = (scaled / factor) * factor;
  return fromScaled(rounded, scale);
}

function compare(a: string, b: string): number {
  const scale = Math.max(countDecimals(a), countDecimals(b));
  const scaledA = toScaled(a, scale);
  const scaledB = toScaled(b, scale);
  if (scaledA < scaledB) return -1;
  if (scaledA > scaledB) return 1;
  return 0;
}

function multiply(a: string, b: string): string {
  const scaleA = countDecimals(a);
  const scaleB = countDecimals(b);
  const resultScale = scaleA + scaleB;
  return fromScaled(toScaled(a, scaleA) * toScaled(b, scaleB), resultScale);
}

// ---------------------------------------------------------------------------
// Guard inputs
// ---------------------------------------------------------------------------

export interface BitgetGuardContext {
  readonly order: BitgetOrderRequest;
  readonly instrument: BitgetInstrument;
  readonly balances: ReadonlyArray<BitgetBalance>;
  readonly feeRate?: string;
}

export interface BitgetGuardResult {
  readonly ok: true;
  readonly normalized: BitgetOrderRequest;
}

export type BitgetGuardOutput =
  | BitgetGuardResult
  | { readonly ok: false; readonly reason: string };

// ---------------------------------------------------------------------------
// Guards
// ---------------------------------------------------------------------------

function findBalance(
  balances: ReadonlyArray<BitgetBalance>,
  asset: string,
): string {
  const match = balances.find(
    (b) => b.asset.toUpperCase() === asset.toUpperCase(),
  );
  return match?.available ?? "0";
}

function requireInstrument(
  ctx: BitgetGuardContext,
): Effect.Effect<BitgetInstrument, BitgetGuardError> {
  const orderSymbol = ctx.order.symbol.replace("/", "").toUpperCase();
  const instrumentSymbol = ctx.instrument.symbol.toUpperCase();
  if (orderSymbol !== instrumentSymbol) {
    return Effect.fail(
      new BitgetGuardError({
        reason: `instrument mismatch: order ${ctx.order.symbol} vs instrument ${ctx.instrument.symbol}`,
      }),
    );
  }
  if (ctx.instrument.status !== "online" && ctx.instrument.status !== "") {
    return Effect.fail(
      new BitgetGuardError({
        reason: `symbol ${ctx.instrument.symbol} is not tradable (status: ${ctx.instrument.status})`,
      }),
    );
  }
  return Effect.succeed(ctx.instrument);
}

function normalizeSizeAndPrice(
  instrument: BitgetInstrument,
  order: BitgetOrderRequest,
): Effect.Effect<BitgetOrderRequest, BitgetGuardError> {
  return Effect.gen(function* () {
    const qtyPrecision = parseInt(instrument.quantityPrecision, 10);
    const pricePrecision = parseInt(instrument.pricePrecision, 10);
    if (Number.isNaN(qtyPrecision) || qtyPrecision < 0) {
      return yield* Effect.fail(
        new BitgetGuardError({
          reason: `invalid quantityPrecision for ${instrument.symbol}`,
        }),
      );
    }

    const normalizedSize = roundDown(order.size, qtyPrecision);
    if (compare(normalizedSize, "0") <= 0) {
      return yield* Effect.fail(
        new BitgetGuardError({
          reason: `order size ${order.size} rounds to zero at precision ${qtyPrecision}`,
        }),
      );
    }

    let normalizedPrice = order.price;
    if (order.orderType === "limit") {
      if (!normalizedPrice || compare(normalizedPrice, "0") <= 0) {
        return yield* Effect.fail(
          new BitgetGuardError({
            reason: `limit order requires positive price for ${instrument.symbol}`,
          }),
        );
      }
      normalizedPrice = roundDown(normalizedPrice, pricePrecision);
      if (compare(normalizedPrice, "0") <= 0) {
        return yield* Effect.fail(
          new BitgetGuardError({
            reason: `limit price ${order.price} rounds to zero at precision ${pricePrecision}`,
          }),
        );
      }
    }

    return {
      ...order,
      size: normalizedSize,
      price: normalizedPrice,
    };
  });
}

function checkNotionalAndLimits(
  instrument: BitgetInstrument,
  order: BitgetOrderRequest,
  referencePrice: string,
): Effect.Effect<void, BitgetGuardError> {
  return Effect.gen(function* () {
    const minTrade = instrument.minTradeAmount;
    const maxTrade = instrument.maxTradeAmount;

    // Notional guard: for market/limit orders we estimate notional using
    // the reference price and reject if below the instrument minimum.
    const notional =
      order.side === "buy" && order.orderType === "market"
        ? order.size
        : multiply(order.size, referencePrice);

    if (compare(notional, minTrade) < 0) {
      return yield* Effect.fail(
        new BitgetGuardError({
          reason: `notional ${notional} below min trade amount ${minTrade} for ${instrument.symbol}`,
        }),
      );
    }

    if (compare(order.size, maxTrade) > 0) {
      return yield* Effect.fail(
        new BitgetGuardError({
          reason: `size ${order.size} above max trade amount ${maxTrade} for ${instrument.symbol}`,
        }),
      );
    }

    return undefined;
  });
}

function checkBalance(
  ctx: BitgetGuardContext,
  order: BitgetOrderRequest,
  _referencePrice: string,
): Effect.Effect<void, BitgetGuardError> {
  return Effect.gen(function* () {
    const feeRate = ctx.feeRate ?? "0.001";
    if (order.side === "buy") {
      // For a market buy, size is interpreted as quote quantity on many
      // exchanges. We treat it conservatively: required quote = size * (1 + fee).
      // For a limit buy, required quote = size * price * (1 + fee).
      const price = order.orderType === "market" ? "1" : (order.price ?? "1");
      const grossQuote = multiply(order.size, price);
      const requiredQuote = multiply(grossQuote, add("1", feeRate));
      const availableQuote = findBalance(
        ctx.balances,
        ctx.instrument.quoteCoin,
      );
      if (compare(availableQuote, requiredQuote) < 0) {
        return yield* Effect.fail(
          new BitgetGuardError({
            reason: `insufficient ${ctx.instrument.quoteCoin} balance: available ${availableQuote}, required ~${requiredQuote}`,
          }),
        );
      }
    } else {
      const availableBase = findBalance(ctx.balances, ctx.instrument.baseCoin);
      if (compare(availableBase, order.size) < 0) {
        return yield* Effect.fail(
          new BitgetGuardError({
            reason: `insufficient ${ctx.instrument.baseCoin} balance: available ${availableBase}, required ${order.size}`,
          }),
        );
      }
    }
    return undefined;
  });
}

// ---------------------------------------------------------------------------
// Public guard runner
// ---------------------------------------------------------------------------

/**
 * Run all pre-trade guards for a Bitget order.
 *
 * @param ctx Guard context (order, instrument, balances, optional fee rate).
 * @param referencePrice Last known mark/last price for notional estimation.
 * @returns Effect that fails with BitgetGuardError or succeeds with the
 *          normalized order ready for the network call.
 */
export function validateOrder(
  ctx: BitgetGuardContext,
  referencePrice: string,
): Effect.Effect<BitgetOrderRequest, BitgetGuardError> {
  return Effect.gen(function* () {
    const instrument = yield* requireInstrument(ctx);
    const normalized = yield* normalizeSizeAndPrice(instrument, ctx.order);
    yield* checkNotionalAndLimits(instrument, normalized, referencePrice);
    yield* checkBalance(ctx, normalized, referencePrice);
    return normalized;
  });
}
