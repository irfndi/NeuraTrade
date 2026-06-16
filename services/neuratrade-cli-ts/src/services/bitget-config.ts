/**
 * Bitget configuration loader.
 *
 * Credentials are intentionally loaded only from environment variables so they
 * never live in config.json. The optional sandbox flag lets users point at
 * Bitget's demo trading environment when available.
 */
import {
  Config,
  ConfigError,
  Context,
  Data,
  Effect,
  Layer,
  Redacted,
} from "effect";
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

export const BitgetConfigLive: Layer.Layer<
  BitgetConfig,
  ConfigError.ConfigError
> = Layer.effect(
  BitgetConfig,
  Effect.gen(function* () {
    const apiKey = yield* Config.redacted("BITGET_API_KEY").pipe(
      Config.withDefault(Redacted.make("")),
    );
    const apiSecret = yield* Config.redacted("BITGET_API_SECRET").pipe(
      Config.withDefault(Redacted.make("")),
    );
    const passphrase = yield* Config.redacted("BITGET_PASSPHRASE").pipe(
      Config.withDefault(Redacted.make("")),
    );
    const useSandbox = yield* Config.boolean("BITGET_USE_SANDBOX").pipe(
      Config.withDefault(false),
    );
    return {
      credentials: {
        apiKey: Redacted.value(apiKey),
        apiSecret: Redacted.value(apiSecret),
        passphrase: Redacted.value(passphrase),
      },
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
