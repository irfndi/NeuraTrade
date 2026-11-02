import { describe, expect, it } from "bun:test";
import {
  MAINNET_CONFIRM_ENV,
  MAINNET_CONFIRM_TOKEN,
  validateLiveSandboxMode,
} from "./scalp.js";

describe("mainnet live gate (must never open by accident)", () => {
  it("allows a demo/testnet account without ceremony", () => {
    expect(validateLiveSandboxMode(true, true)).toBeUndefined();
    expect(validateLiveSandboxMode(false, false)).toBeUndefined();
  });

  it("blocks mainnet without --allow-mainnet-live", () => {
    expect(validateLiveSandboxMode(true, false, false)).toBeDefined();
  });

  it("blocks mainnet with --allow-mainnet-live but no confirm token", () => {
    expect(validateLiveSandboxMode(true, false, true)).toBeDefined();
  });

  it("blocks mainnet with a wrong confirm token", () => {
    expect(validateLiveSandboxMode(true, false, true, "yes")).toBeDefined();
    expect(validateLiveSandboxMode(true, false, true, "")).toBeDefined();
  });

  it("opens only with BOTH the flag and the exact env token", () => {
    expect(
      validateLiveSandboxMode(true, false, true, MAINNET_CONFIRM_TOKEN),
    ).toBeUndefined();
    expect(MAINNET_CONFIRM_ENV).toBe("NEURATRADE_MAINNET_CONFIRM");
    expect(MAINNET_CONFIRM_TOKEN).toBe("I-CONFIRM-MAINNET");
  });
});
