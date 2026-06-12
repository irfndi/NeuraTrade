import { describe, expect, it } from "bun:test";
import { Decimal, money, toNumber } from "./money.js";

describe("money helpers", () => {
  it("creates a Decimal from a number", () => {
    const m = money(100.5);
    expect(m instanceof Decimal).toBe(true);
    expect(m.toNumber()).toBe(100.5);
  });

  it("preserves precision for fractional arithmetic", () => {
    const a = money("0.1");
    const b = money("0.2");
    expect(a.plus(b).toNumber()).toBe(0.3);
  });

  it("rounds toNumber to the requested decimals", () => {
    const m = money(1).div(3);
    expect(toNumber(m, 4)).toBe(0.3333);
  });
});
