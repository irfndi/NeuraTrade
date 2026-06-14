/**
 * String-based decimal arithmetic helpers.
 *
 * Uses scaled big integers to avoid floating-point money bugs. All inputs and
 * outputs are plain decimal strings.
 */

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

export function multiply(a: string, b: string): string {
  const scaleA = countDecimals(a);
  const scaleB = countDecimals(b);
  return fromScaled(toScaled(a, scaleA) * toScaled(b, scaleB), scaleA + scaleB);
}

export function divide(a: string, b: string): string {
  const scaleA = countDecimals(a);
  const scaleB = countDecimals(b);
  const scale = Math.max(scaleA, scaleB) + 8;
  const scaledA = toScaled(a, scale);
  const scaledB = toScaled(b, scale);
  if (scaledB === 0n) return "0";
  return fromScaled((scaledA * 10n ** 8n) / scaledB, 8);
}

export function add(a: string, b: string): string {
  const scale = Math.max(countDecimals(a), countDecimals(b));
  return fromScaled(toScaled(a, scale) + toScaled(b, scale), scale);
}

export function subtract(a: string, b: string): string {
  const scale = Math.max(countDecimals(a), countDecimals(b));
  return fromScaled(toScaled(a, scale) - toScaled(b, scale), scale);
}

export function compare(a: string, b: string): number {
  const scale = Math.max(countDecimals(a), countDecimals(b));
  const sa = toScaled(a, scale);
  const sb = toScaled(b, scale);
  if (sa < sb) return -1;
  if (sa > sb) return 1;
  return 0;
}

export function min(a: string, b: string): string {
  return compare(a, b) <= 0 ? a : b;
}

export function max(a: string, b: string): string {
  return compare(a, b) >= 0 ? a : b;
}

export function abs(value: string): string {
  return value.startsWith("-") ? value.slice(1) : value;
}
