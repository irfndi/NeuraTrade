/**
 * Schema definitions for runtimeConfig (runtime.json).
 *
 * Mirrors the Go struct in cmd/neuratrade-cli/runtime_config.go.
 * Fields use `Schema.optional` + `Schema.withDecodingDefault` where the
 * Go code provides explicit defaults in `defaultRuntimeConfig()`.
 */
import { Effect } from "effect";
import * as S from "effect/Schema";

// -- Nested sub-schemas --

const RuntimeServerConfig = S.Struct({
  host: S.String.pipe(S.withDecodingDefault(Effect.succeed("0.0.0.0"))),
  port: S.Number.pipe(S.withDecodingDefault(Effect.succeed(8080))),
});

const RuntimeDatabaseConfig = S.Struct({
  driver: S.String.pipe(S.withDecodingDefault(Effect.succeed("sqlite"))),
  sqlite_path: S.optional(S.String),
});

const RuntimeRedisConfig = S.Struct({
  host: S.String.pipe(S.withDecodingDefault(Effect.succeed("127.0.0.1"))),
  port: S.Number.pipe(S.withDecodingDefault(Effect.succeed(6379))),
});

const RuntimeCCXTConfig = S.Struct({
  service_url: S.String.pipe(
    S.withDecodingDefault(Effect.succeed("http://localhost:3001")),
  ),
  grpc_address: S.String.pipe(
    S.withDecodingDefault(Effect.succeed("127.0.0.1:50051")),
  ),
});

const RuntimeTelegramConfig = S.Struct({
  service_url: S.String.pipe(
    S.withDecodingDefault(Effect.succeed("http://localhost:3002")),
  ),
  grpc_address: S.String.pipe(
    S.withDecodingDefault(Effect.succeed("127.0.0.1:50052")),
  ),
  use_polling: S.Boolean.pipe(S.withDecodingDefault(Effect.succeed(true))),
  api_base_url: S.String.pipe(
    S.withDecodingDefault(Effect.succeed("http://localhost:8080")),
  ),
});

const RuntimeAIConfig = S.Struct({
  provider: S.String.pipe(S.withDecodingDefault(Effect.succeed("openai"))),
  model: S.String.pipe(S.withDecodingDefault(Effect.succeed("gpt-4o-mini"))),
  base_url: S.optional(S.String),
  temperature: S.Number.pipe(S.withDecodingDefault(Effect.succeed(0.7))),
  max_tokens: S.Number.pipe(S.withDecodingDefault(Effect.succeed(4096))),
  min_confidence: S.Number.pipe(S.withDecodingDefault(Effect.succeed(0.7))),
  // Go uses decimal.Decimal which JSON-encodes as a string
  daily_budget: S.String.pipe(S.withDecodingDefault(Effect.succeed("10"))),
  routing_mode: S.String.pipe(S.withDecodingDefault(Effect.succeed("primary"))),
});

const RuntimeFeaturesConfig = S.Struct({
  enable_ai: S.Boolean.pipe(S.withDecodingDefault(Effect.succeed(true))),
  enable_ai_scalping: S.Boolean.pipe(
    S.withDecodingDefault(Effect.succeed(true)),
  ),
  enable_ai_signals: S.Boolean.pipe(
    S.withDecodingDefault(Effect.succeed(false)),
  ),
  enable_ai_arbitrage: S.Boolean.pipe(
    S.withDecodingDefault(Effect.succeed(false)),
  ),
  paper_trading: S.Boolean.pipe(S.withDecodingDefault(Effect.succeed(true))),
  real_trading: S.Boolean.pipe(S.withDecodingDefault(Effect.succeed(false))),
});

const RuntimeGatewayConfig = S.Struct({
  bind_host: S.String.pipe(S.withDecodingDefault(Effect.succeed("127.0.0.1"))),
  ccxt_port: S.Number.pipe(S.withDecodingDefault(Effect.succeed(3001))),
  telegram_port: S.Number.pipe(S.withDecodingDefault(Effect.succeed(3002))),
  telegram_grpc_port: S.Number.pipe(
    S.withDecodingDefault(Effect.succeed(50052)),
  ),
  supervised: S.Boolean.pipe(S.withDecodingDefault(Effect.succeed(false))),
  health_timeout_seconds: S.Number.pipe(
    S.withDecodingDefault(Effect.succeed(150)),
  ),
  signal_timeout_seconds: S.Number.pipe(
    S.withDecodingDefault(Effect.succeed(5)),
  ),
  graceful_timeout_seconds: S.Number.pipe(
    S.withDecodingDefault(Effect.succeed(10)),
  ),
  skip_telegram: S.Boolean.pipe(S.withDecodingDefault(Effect.succeed(false))),
});

// -- Main runtimeConfig schema --
// All nested objects are optional because Go's json.Unmarshal leaves missing
// fields at their zero values. When a nested object is absent, the default
// value is applied and inner fields fill in their own defaults.

export const RuntimeConfigSchema = S.Struct({
  server: RuntimeServerConfig.pipe(
    S.withDecodingDefault(
      Effect.succeed({
        host: "0.0.0.0",
        port: 8080,
      }),
    ),
  ),
  database: RuntimeDatabaseConfig.pipe(
    S.withDecodingDefault(
      Effect.succeed({
        driver: "sqlite",
        sqlite_path: "",
      }),
    ),
  ),
  redis: RuntimeRedisConfig.pipe(
    S.withDecodingDefault(
      Effect.succeed({
        host: "127.0.0.1",
        port: 6379,
      }),
    ),
  ),
  ccxt: RuntimeCCXTConfig.pipe(
    S.withDecodingDefault(
      Effect.succeed({
        service_url: "http://localhost:3001",
        grpc_address: "127.0.0.1:50051",
      }),
    ),
  ),
  telegram: RuntimeTelegramConfig.pipe(
    S.withDecodingDefault(
      Effect.succeed({
        service_url: "http://localhost:3002",
        grpc_address: "127.0.0.1:50052",
        use_polling: true,
        api_base_url: "http://localhost:8080",
      }),
    ),
  ),
  ai: RuntimeAIConfig.pipe(
    S.withDecodingDefault(
      Effect.succeed({
        provider: "openai",
        model: "gpt-4o-mini",
        base_url: undefined,
        temperature: 0.7,
        max_tokens: 4096,
        min_confidence: 0.7,
        daily_budget: "10",
        routing_mode: "primary",
      }),
    ),
  ),
  features: RuntimeFeaturesConfig.pipe(
    S.withDecodingDefault(
      Effect.succeed({
        enable_ai: true,
        enable_ai_scalping: true,
        enable_ai_signals: false,
        enable_ai_arbitrage: false,
        paper_trading: true,
        real_trading: false,
      }),
    ),
  ),
  gateway: RuntimeGatewayConfig.pipe(
    S.withDecodingDefault(
      Effect.succeed({
        bind_host: "127.0.0.1",
        ccxt_port: 3001,
        telegram_port: 3002,
        telegram_grpc_port: 50052,
        supervised: false,
        health_timeout_seconds: 150,
        signal_timeout_seconds: 5,
        graceful_timeout_seconds: 10,
        skip_telegram: false,
      }),
    ),
  ),
});

export type RuntimeConfig = typeof RuntimeConfigSchema.Type;

/** Decode an unknown JSON value into RuntimeConfig (returns Effect). */
export const decodeRuntimeConfig = S.decodeUnknownEffect(RuntimeConfigSchema);

/** Decode an unknown JSON value, returning Either. */
export const decodeRuntimeConfigEither =
  S.decodeUnknownResult(RuntimeConfigSchema);
