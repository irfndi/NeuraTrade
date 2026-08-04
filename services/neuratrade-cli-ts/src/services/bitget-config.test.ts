import { describe, expect, it } from "bun:test";
import { ConfigProvider, Effect } from "effect";
import {
  BitgetConfig,
  BitgetConfigLive,
  requireBitgetCredentials,
} from "./bitget-config.ts";

// Effect v4 snapshots process.env in the default ConfigProvider at first use,
// so tests that mutate process.env must supply a fresh provider per run.
const loadConfig = () =>
  Effect.gen(function* () {
    return yield* BitgetConfig;
  }).pipe(
    Effect.provide(BitgetConfigLive),
    Effect.provideService(
      ConfigProvider.ConfigProvider,
      ConfigProvider.fromEnv(),
    ),
  );

describe("BitgetConfig", () => {
  it("loads credentials and sandbox flag from environment", async () => {
    const original = process.env.BITGET_USE_SANDBOX;
    delete process.env.BITGET_USE_SANDBOX;
    try {
      const config = await Effect.runPromise(loadConfig());
      expect(config.useSandbox).toBe(false);
    } finally {
      if (original === undefined) {
        delete process.env.BITGET_USE_SANDBOX;
      } else {
        process.env.BITGET_USE_SANDBOX = original;
      }
    }
  });

  it("parses BITGET_USE_SANDBOX=true", async () => {
    const original = process.env.BITGET_USE_SANDBOX;
    process.env.BITGET_USE_SANDBOX = "true";
    try {
      const config = await Effect.runPromise(loadConfig());
      expect(config.useSandbox).toBe(true);
    } finally {
      if (original === undefined) {
        delete process.env.BITGET_USE_SANDBOX;
      } else {
        process.env.BITGET_USE_SANDBOX = original;
      }
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
      const config = await Effect.runPromise(loadConfig());
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
