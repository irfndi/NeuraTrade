/**
 * Bybit configuration loader.
 *
 * Credentials are intentionally loaded only from environment variables so they
 * never live in config.json. Testnet (api-testnet.bybit.com) is the default:
 * the demo and funnel paths trade against the testnet mirror of the live
 * USDT-perp market (200+ contracts vs the Bitget demo's 25).
 */
import { Config, Context, Data, Effect, Layer, Redacted } from "effect";

export interface BybitCredentials {
  readonly apiKey: string;
  readonly apiSecret: string;
}

export class BybitConfigError extends Data.TaggedError("BybitConfigError")<{
  readonly reason: string;
}> {}

export interface BybitConfigData {
  readonly credentials: BybitCredentials;
  readonly useTestnet: boolean;
}

export class BybitConfig extends Context.Service<
  BybitConfig,
  BybitConfigData
>()("BybitConfig") {}

export const BybitConfigLive: Layer.Layer<BybitConfig, Config.ConfigError> =
  Layer.effect(
    BybitConfig,
    Effect.gen(function* () {
      const apiKey = yield* Config.redacted("BYBIT_API_KEY").pipe(
        Config.withDefault(Redacted.make("")),
      );
      const apiSecret = yield* Config.redacted("BYBIT_API_SECRET").pipe(
        Config.withDefault(Redacted.make("")),
      );
      const useTestnet = yield* Config.boolean("BYBIT_USE_TESTNET").pipe(
        Config.withDefault(true),
      );
      return {
        credentials: {
          apiKey: Redacted.value(apiKey),
          apiSecret: Redacted.value(apiSecret),
        },
        useTestnet,
      };
    }),
  );

export function requireBybitCredentials(
  config: BybitConfigData,
): Effect.Effect<BybitCredentials, BybitConfigError> {
  return Effect.gen(function* () {
    const { apiKey, apiSecret } = config.credentials;
    if (!apiKey || !apiSecret) {
      return yield* Effect.fail(
        new BybitConfigError({
          reason:
            "Bybit credentials missing. Set BYBIT_API_KEY and BYBIT_API_SECRET.",
        }),
      );
    }
    return { apiKey, apiSecret };
  });
}
