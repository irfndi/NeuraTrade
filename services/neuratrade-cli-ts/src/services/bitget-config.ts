/**
 * Bitget configuration loader.
 *
 * Credentials are intentionally loaded only from environment variables so they
 * never live in config.json. The optional sandbox flag lets users point at
 * Bitget's demo trading environment when available.
 */
import { Context, Data, Effect, Layer } from "effect";
import type { BitgetCredentials } from "./bitget-client.ts";

export class BitgetConfigError extends Data.TaggedError("BitgetConfigError")<{
  readonly reason: string;
}> {}

export interface BitgetConfigData {
  readonly credentials: BitgetCredentials;
  readonly useSandbox: boolean;
}

export class BitgetConfig extends Context.Tag("BitgetConfig")<
  BitgetConfig,
  BitgetConfigData
>() {}

function envString(key: string): string | undefined {
  const value = process.env[key];
  if (value === undefined) return undefined;
  const trimmed = value.trim();
  return trimmed !== "" ? trimmed : undefined;
}

function envBool(key: string): boolean {
  const value = envString(key);
  if (value === undefined) return false;
  const lower = value.toLowerCase();
  return lower === "true" || lower === "1" || lower === "yes" || lower === "on";
}

export const BitgetConfigLive: Layer.Layer<BitgetConfig, never> = Layer.effect(
  BitgetConfig,
  Effect.sync(() => {
    const apiKey = envString("BITGET_API_KEY") ?? "";
    const apiSecret = envString("BITGET_API_SECRET") ?? "";
    const passphrase = envString("BITGET_PASSPHRASE") ?? "";
    const useSandbox = envBool("BITGET_USE_SANDBOX");
    return {
      credentials: { apiKey, apiSecret, passphrase },
      useSandbox,
    };
  }),
);

export function requireBitgetCredentials(
  config: BitgetConfigData,
): Effect.Effect<BitgetCredentials, BitgetConfigError> {
  return Effect.gen(function* () {
    const { apiKey, apiSecret, passphrase } = config.credentials;
    if (!apiKey || !apiSecret || !passphrase) {
      return yield* Effect.fail(
        new BitgetConfigError({
          reason:
            "Bitget credentials missing. Set BITGET_API_KEY, BITGET_API_SECRET and BITGET_PASSPHRASE.",
        }),
      );
    }
    return { apiKey, apiSecret, passphrase };
  });
}
