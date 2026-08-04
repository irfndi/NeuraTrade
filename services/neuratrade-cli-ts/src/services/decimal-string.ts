const decimalPattern = /^[+-]?(?:\d+(?:\.\d*)?|\.\d+)$/;

export function isDecimalString(value: string): boolean {
  return decimalPattern.test(value.trim());
}
