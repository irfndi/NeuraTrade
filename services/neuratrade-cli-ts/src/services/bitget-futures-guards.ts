/**
 * Bitget futures pre-trade guards.
 *
 * Validates leverage, size and margin sufficiency before a signed futures
 * order request is sent. Uses scaled integer math to avoid float money errors.
 */
import { Data, Effect } from "effect";
import type {
  BitgetContract,
  BitgetFuturesBalance,
  BitgetFuturesOrderRequest,
} from "./bitget-client.ts";

export class BitgetFuturesGuardError extends Data.TaggedError(
  "BitgetFuturesGuardError",
)<{
  readonly reason: string;
}> {}

// ---------------------------------------------------------------------------
// Decimal helpers
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
  if (scale === 0) return value.toString();
  const negative = value < 0n;
  const abs = negative ? -value : value;
  const str = abs.toString().padStart(scale + 1, "0");
  const intPart = str.slice(0, -scale) || "0";
  const fracPart = str.slice(-scale).replace(/0+$/, "");
  return `${negative ? "-" : ""}${intPart}${fracPart ? `.${fracPart}` : ""}`;
}

function multiply(a: string, b: string): string {
  const scaleA = countDecimals(a);
  const scaleB = countDecimals(b);
  return fromScaled(toScaled(a, scaleA) * toScaled(b, scaleB), scaleA + scaleB);
}

function divide(a: string, b: string): string {
  const scale = Math.max(countDecimals(a), countDecimals(b)) + 8;
  const sb = toScaled(b, scale);
  if (sb === 0n) return "0";
  return fromScaled((toScaled(a, scale) * 10n ** 8n) / sb, 8);
}

function compare(a: string, b: string): number {
  const scale = Math.max(countDecimals(a), countDecimals(b));
  const sa = toScaled(a, scale);
  const sb = toScaled(b, scale);
  if (sa < sb) return -1;
  if (sa > sb) return 1;
  return 0;
}

// ---------------------------------------------------------------------------
// Guard context
// ---------------------------------------------------------------------------

export interface BitgetFuturesGuardContext {
  readonly order: BitgetFuturesOrderRequest;
  readonly contract: BitgetContract;
  readonly balances: ReadonlyArray<BitgetFuturesBalance>;
  readonly lastPrice: string;
  readonly leverage: string;
}

export interface BitgetFuturesGuardResult {
  readonly ok: true;
  readonly notional: string;
  readonly marginRequired: string;
}

// ---------------------------------------------------------------------------
// Guards
// ---------------------------------------------------------------------------

export function validateFuturesOrder(
  ctx: BitgetFuturesGuardContext,
): Effect.Effect<BitgetFuturesGuardResult, BitgetFuturesGuardError> {
  return Effect.gen(function* () {
    const orderSymbol = ctx.order.symbol
      .replace("/", "")
      .split(":")[0]
      .toUpperCase();
    const contractSymbol = ctx.contract.symbol.toUpperCase();
    if (orderSymbol !== contractSymbol) {
      return yield* Effect.fail(
        new BitgetFuturesGuardError({
          reason: `contract mismatch: order ${ctx.order.symbol} vs contract ${ctx.contract.symbol}`,
        }),
      );
    }
    const contractStatus = ctx.contract.symbolStatus || ctx.contract.status;
    if (
      contractStatus !== "online" &&
      contractStatus !== "normal" &&
      contractStatus !== ""
    ) {
      return yield* Effect.fail(
        new BitgetFuturesGuardError({
          reason: `contract ${ctx.contract.symbol} is not tradable`,
        }),
      );
    }

    // Leverage range
    const lev = ctx.leverage;
    if (compare(lev, ctx.contract.minLeverage) < 0) {
      return yield* Effect.fail(
        new BitgetFuturesGuardError({
          reason: `leverage ${lev} below minimum ${ctx.contract.minLeverage}`,
        }),
      );
    }
    if (compare(lev, ctx.contract.maxLeverage) > 0) {
      return yield* Effect.fail(
        new BitgetFuturesGuardError({
          reason: `leverage ${lev} above maximum ${ctx.contract.maxLeverage}`,
        }),
      );
    }

    // Notional and size: use the order limit price when available; otherwise
    // fall back to the last/mark price. USDT-margined contracts publish a
    // USDT minimum notional (minTradeUSDT); coin-margined contracts publish a
    // base-quantity minimum (minTradeNum). Keep minTradeAmount as a legacy fallback.
    const price =
      ctx.order.price && ctx.order.price.trim() !== ""
        ? ctx.order.price
        : ctx.lastPrice;
    const notional = multiply(ctx.order.size, price);

    // Reduce-only closes only need the contract/leverage checks above; they do
    // not consume new margin or require a fresh balance allocation.
    if (ctx.order.reduceOnly) {
      return { ok: true as const, notional, marginRequired: "0" };
    }

    const isUsdtMargined =
      ctx.contract.productType === "USDT-FUTURES" ||
      ctx.contract.productType === "USDC-FUTURES";
    const minNotional =
      isUsdtMargined && compare(ctx.contract.minTradeUSDT, "0") > 0
        ? ctx.contract.minTradeUSDT
        : compare(ctx.contract.minTradeAmount, "0") > 0
          ? ctx.contract.minTradeAmount
          : ctx.contract.minTradeNum;
    if (compare(notional, minNotional) < 0) {
      return yield* Effect.fail(
        new BitgetFuturesGuardError({
          reason: `notional ${notional} below min trade amount ${minNotional} for ${ctx.contract.symbol}`,
        }),
      );
    }

    // Margin required (notional / leverage)
    const marginRequired = divide(notional, lev);

    // Balance check
    const marginCoin = ctx.contract.quoteCoin.toUpperCase();
    const balance = ctx.balances.find(
      (b) => b.marginCoin.toUpperCase() === marginCoin,
    );
    if (
      balance === undefined ||
      compare(balance.available, marginRequired) < 0
    ) {
      return yield* Effect.fail(
        new BitgetFuturesGuardError({
          reason: `insufficient ${marginCoin} margin: available ${balance?.available ?? "0"}, required ~${marginRequired}`,
        }),
      );
    }

    return { ok: true as const, notional, marginRequired };
  });
}
