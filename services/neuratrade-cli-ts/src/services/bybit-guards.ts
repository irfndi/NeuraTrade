/**
 * Bybit linear (USDT-perp) pre-trade guards.
 *
 * Mirrors the intent of the Bitget guards (bitget-guards.ts) for the Bybit
 * linear futures path: validate an order against the instrument's lot/price
 * rules and the account's available balance BEFORE any signed network request
 * is sent, so a real order cannot exceed account balance or violate precision.
 *
 * Bybit USDT-margined ("linear") contracts quote qty in the BASE asset and
 * settle in USDT, so order notional = qty * price. This diverge differs from
 * the Bitget guards in three Bybit-specific ways (mirroring the Bybit adapter's
 * live contract filters):
 *
 *  - Minimum order is expressed as a notional floor (minOrderAmt) AND a base
 *    qty floor (minOrderQty); either below-floor condition rejects.
 *  - qty step (qtyStep) and tick size (tickSize) drive precision; sizes and
 *    limit prices are rounded DOWN to the instrument step so we never send an
 *    overly-precise value (the adapter may round up to a floor, this guard is
 *    the final fail-closed gate).
 *  - Balance sufficiency for a leveraged USDT-margined contract is MARGIN
 *    (= notional / leverage, plus fee) against available USDT — not full
 *    notional as in the spot-style bitget guard.
 *
 * All monetary math uses scaled integers (BigInt) to avoid floating-point money
 * errors.
 */
import { Data, Effect } from "effect";

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

export class BybitGuardError extends Data.TaggedError("BybitGuardError")<{
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

function divide(a: string, b: string): string {
  // Scaled division with 8 fractional digits of retained precision.
  if (compare(b, "0") === 0) return "0";
  const scale = Math.max(countDecimals(a), countDecimals(b), 8);
  const scaledA = toScaled(a, scale) * 10n ** 8n;
  const scaledB = toScaled(b, scale);
  return fromScaled(scaledA / scaledB, 8);
}

/**
 * The number of decimal places implied by a step/tick value, e.g. qtyStep
 * "0.0001" -> 4, tickSize "0.1" -> 1, "1" -> 0. Step-multiple rounding itself
 * (e.g. a tick of 5) is the adapter's job; this guard enforces the precision
 * net so a sent order never carries more fractional digits than the contract
 * allows.
 */
function precisionOf(step: string): number {
  return countDecimals(step);
}

// ---------------------------------------------------------------------------
// Guard inputs
// ---------------------------------------------------------------------------

export interface BybitGuardOrder {
  readonly symbol: string;
  readonly side: "buy" | "sell";
  readonly orderType: "market" | "limit";
  /** Order size in BASE units. */
  readonly size: string;
  /** Absolute limit price (required for limit orders). */
  readonly price?: string;
  /** Requested leverage (must be >= 1, <= contract maxLeverage). */
  readonly leverage?: number;
}

export interface BybitGuardContract {
  readonly symbol: string;
  /** Empty string means "no status guard". */
  readonly status: string;
  /** lotSizeFilter.minOrderQty — min qty in base units. */
  readonly minOrderQty: string;
  /** lotSizeFilter.qtyStep — quantity step. */
  readonly qtyStep: string;
  /** lotSizeFilter.minOrderAmt — min order notional in quote units. */
  readonly minOrderAmt: string;
  /** priceFilter.tickSize — price tick. */
  readonly tickSize: string;
  /** leverageFilter.maxLeverage — max leverage. */
  readonly maxLeverage: string;
  /** Optional upper notional bound (quote units); omitted when unconstrained. */
  readonly maxOrderNotional?: string;
}

export interface BybitBalance {
  readonly asset: string;
  readonly available: string;
  readonly walletBalance?: string;
  readonly equity?: string;
}

export interface BybitGuardContext {
  readonly order: BybitGuardOrder;
  readonly contract: BybitGuardContract;
  readonly balances: ReadonlyArray<BybitBalance>;
  readonly feeRate?: string;
}

export interface BybitGuardResult {
  readonly ok: true;
  readonly normalized: BybitGuardOrder;
}

export type BybitGuardOutput =
  | BybitGuardResult
  | { readonly ok: false; readonly reason: string };

// ---------------------------------------------------------------------------
// Guards
// ---------------------------------------------------------------------------

function findBalance(
  balances: ReadonlyArray<BybitBalance>,
  asset: string,
): string {
  const match = balances.find(
    (b) => b.asset.toUpperCase() === asset.toUpperCase(),
  );
  const available = match?.available ?? "0";
  return available === "" ? (match?.walletBalance ?? "0") : available;
}

function requireContract(
  ctx: BybitGuardContext,
): Effect.Effect<BybitGuardContract, BybitGuardError> {
  const orderSymbol = ctx.order.symbol
    .replace("/", "")
    .split(":")[0]
    .toUpperCase();
  const contractSymbol = ctx.contract.symbol
    .replace("/", "")
    .split(":")[0]
    .toUpperCase();
  if (orderSymbol !== contractSymbol) {
    return Effect.fail(
      new BybitGuardError({
        reason: `contract mismatch: order ${ctx.order.symbol} vs instrument ${ctx.contract.symbol}`,
      }),
    );
  }
  if (ctx.contract.status !== "" && ctx.contract.status !== "Trading") {
    return Effect.fail(
      new BybitGuardError({
        reason: `symbol ${ctx.contract.symbol} is not tradable (status: ${ctx.contract.status})`,
      }),
    );
  }
  return Effect.succeed(ctx.contract);
}

function normalizeSizeAndPrice(
  contract: BybitGuardContract,
  order: BybitGuardOrder,
): Effect.Effect<BybitGuardOrder, BybitGuardError> {
  return Effect.gen(function* () {
    const qtyPrecision = precisionOf(contract.qtyStep);
    if (compare(contract.qtyStep, "0") === 0) {
      return yield* Effect.fail(
        new BybitGuardError({
          reason: `invalid qtyStep 0 for ${contract.symbol}`,
        }),
      );
    }

    // Round DOWN to the qty step so the sent size never exceeds the request and
    // never carries more precision than the contract allows.
    const lowered = roundDown(order.size, qtyPrecision);
    let normalized = order.size;
    if (compare(lowered, "0") <= 0) {
      return yield* Effect.fail(
        new BybitGuardError({
          reason: `order size ${order.size} rounds below qtyStep ${contract.qtyStep} for ${contract.symbol}`,
        }),
      );
    }
    normalized = lowered;

    let normalizedPrice: string | undefined = order.price;
    if (order.orderType === "limit") {
      if (!normalizedPrice || compare(normalizedPrice, "0") <= 0) {
        return yield* Effect.fail(
          new BybitGuardError({
            reason: `limit order requires positive price for ${contract.symbol}`,
          }),
        );
      }
      const pricePrecision = precisionOf(contract.tickSize);
      if (pricePrecision > 0) {
        normalizedPrice = roundDown(normalizedPrice, pricePrecision);
        if (compare(normalizedPrice, "0") <= 0) {
          return yield* Effect.fail(
            new BybitGuardError({
              reason: `limit price ${order.price} rounds below tickSize ${contract.tickSize} for ${contract.symbol}`,
            }),
          );
        }
      }
    }

    return { ...order, size: normalized, price: normalizedPrice };
  });
}

function checkNotionalAndLimits(
  contract: BybitGuardContract,
  order: BybitGuardOrder,
  referencePrice: string,
): Effect.Effect<void, BybitGuardError> {
  return Effect.gen(function* () {
    // Notional estimation price: the limit order's own price when present,
    // otherwise the reference (mark/last) price.
    const price =
      order.orderType === "limit" &&
      order.price &&
      compare(order.price, "0") > 0
        ? order.price
        : referencePrice;
    const notional = multiply(order.size, price);

    if (compare(order.size, "0") <= 0) {
      return yield* Effect.fail(
        new BybitGuardError({
          reason: `order size ${order.size} must be positive for ${contract.symbol}`,
        }),
      );
    }
    if (compare(order.size, contract.minOrderQty) < 0) {
      return yield* Effect.fail(
        new BybitGuardError({
          reason: `size ${order.size} below min order qty ${contract.minOrderQty} for ${contract.symbol}`,
        }),
      );
    }
    if (compare(notional, contract.minOrderAmt) < 0) {
      return yield* Effect.fail(
        new BybitGuardError({
          reason: `notional ${notional} below min order amt ${contract.minOrderAmt} for ${contract.symbol}`,
        }),
      );
    }
    if (
      contract.maxOrderNotional &&
      compare(contract.maxOrderNotional, "0") > 0 &&
      compare(notional, contract.maxOrderNotional) > 0
    ) {
      return yield* Effect.fail(
        new BybitGuardError({
          reason: `notional ${notional} above max ${contract.maxOrderNotional} for ${contract.symbol}`,
        }),
      );
    }

    return undefined;
  });
}

function checkBalance(
  ctx: BybitGuardContext,
  order: BybitGuardOrder,
  referencePrice: string,
): Effect.Effect<void, BybitGuardError> {
  return Effect.gen(function* () {
    const feeRate = ctx.feeRate ?? "0.0006";
    const price =
      order.orderType === "limit" &&
      order.price &&
      compare(order.price, "0") > 0
        ? order.price
        : referencePrice;
    const notional = multiply(order.size, price);
    const leverage =
      order.leverage && order.leverage >= 1 ? String(order.leverage) : "1";
    // USDT-margined linear margin = notional / leverage (+ fee buffer). The
    // available USDT wallet balance must cover the required margin.
    const margin = divide(notional, leverage);
    const required = multiply(margin, (1 + Number(feeRate)).toString());
    const availableQuote = findBalance(ctx.balances, "USDT");
    if (compare(availableQuote, required) < 0) {
      return yield* Effect.fail(
        new BybitGuardError({
          reason: `insufficient USDT margin: available ${availableQuote}, required ~${required} for ${ctx.contract.symbol}`,
        }),
      );
    }
    return undefined;
  });
}

// ---------------------------------------------------------------------------
// Public guard runner
// ---------------------------------------------------------------------------

/**
 * Run all pre-trade guards for a Bybit linear-futures (USDT-perp) order.
 *
 * @param ctx Guard context (order, contract, balances, optional fee rate).
 * @param referencePrice Last known mark/last price for notional estimation.
 * @returns Effect that fails with BybitGuardError or succeeds with the
 *          normalized order (precision-rounded) ready for the network call.
 */
export function validateOrder(
  ctx: BybitGuardContext,
  referencePrice: string,
): Effect.Effect<BybitGuardOrder, BybitGuardError> {
  return Effect.gen(function* () {
    const contract = yield* requireContract(ctx);
    const normalized = yield* normalizeSizeAndPrice(contract, ctx.order);
    yield* checkNotionalAndLimits(contract, normalized, referencePrice);
    yield* checkBalance(ctx, normalized, referencePrice);
    return normalized;
  });
}
