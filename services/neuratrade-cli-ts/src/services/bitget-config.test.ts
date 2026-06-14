import { describe, expect, it } from "bun:test";
import { Effect } from "effect";
import {
  BitgetConfig,
  BitgetConfigLive,
  requireBitgetCredentials,
} from "./bitget-config.ts";

describe("BitgetConfig", () => {
  it("loads credentials and sandbox flag from environment", async () => {
    const config = await Effect.runPromise(
      Effect.gen(function* () {
        return yield* BitgetConfig;
      }).pipe(Effect.provide(BitgetConfigLive)),
    );
    expect(config.useSandbox).toBe(false);
  });

  it("parses BITGET_USE_SANDBOX=true", async () => {
    const original = process.env.BITGET_USE_SANDBOX;
    process.env.BITGET_USE_SANDBOX = "true";
    try {
      const config = await Effect.runPromise(
        Effect.gen(function* () {
          return yield* BitgetConfig;
        }).pipe(Effect.provide(BitgetConfigLive)),
      );
      expect(config.useSandbox).toBe(true);
    } finally {
      process.env.BITGET_USE_SANDBOX = original;
    }
  });

  it("requireBitgetCredentials fails when credentials are missing", async () => {
    const originalKey = process.env.BITGET_API_KEY;
    const originalSecret = process.env.BITGET_API_SECRET;
    const originalPass = process.env.BITGET_PASSPHRASE;
    delete process.env.BITGET_API_KEY;
    delete process.env.BITGET_API_SECRET;
    delete process.env.BITGET_PASSPHRASE;
    try {
      const config = await Effect.runPromise(
        Effect.gen(function* () {
          return yield* BitgetConfig;
        }).pipe(Effect.provide(BitgetConfigLive)),
      );
      const exit = await Effect.runPromiseExit(
        requireBitgetCredentials(config),
      );
      expect(exit._tag).toBe("Failure");
    } finally {
      if (originalKey !== undefined) process.env.BITGET_API_KEY = originalKey;
      if (originalSecret !== undefined) {
        process.env.BITGET_API_SECRET = originalSecret;
      }
      if (originalPass !== undefined) {
        process.env.BITGET_PASSPHRASE = originalPass;
      }
    }
  });
});
