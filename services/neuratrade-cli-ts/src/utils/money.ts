import Decimal from "decimal.js";

/**
 * Decimal.js wrapper for monetary calculations.
 *
 * The trading layer uses Decimal for all capital, position-size, and PnL math.
 * Values are converted to/from number only at persistence and exchange boundaries.
 */
export { Decimal };

export type Money = Decimal;

export function money(value: number | string | Decimal): Money {
  return new Decimal(value);
}

export function moneyOrZero(value: number | string | Decimal | undefined | null): Money {
  if (value === undefined || value === null) return new Decimal(0);
  return new Decimal(value);
}

export function toNumber(value: Money, decimals = 8): number {
  return Number(value.toFixed(decimals));
}
