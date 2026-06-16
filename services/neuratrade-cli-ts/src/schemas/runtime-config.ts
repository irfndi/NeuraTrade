/**
 * Schema definitions for runtimeConfig (runtime.json).
 *
 * Mirrors the Go struct in cmd/neuratrade-cli/runtime_config.go.
 * Fields use `Schema.optional` + `Schema.withDecodingDefault` where the
 * Go code provides explicit defaults in `defaultRuntimeConfig()`.
 */
import * as S from "effect/Schema";

// -- Nested sub-schemas --

const RuntimeServerConfig = S.Struct({
  host: S.optional(S.String).pipe(S.withDecodingDefault(() => "0.0.0.0")),
  port: S.optional(S.Number).pipe(S.withDecodingDefault(() => 8080)),
});

const RuntimeDatabaseConfig = S.Struct({
  driver: S.optional(S.String).pipe(S.withDecodingDefault(() => "sqlite")),
  sqlite_path: S.optional(S.String),
});

const RuntimeRedisConfig = S.Struct({
  host: S.optional(S.String).pipe(S.withDecodingDefault(() => "127.0.0.1")),
  port: S.optional(S.Number).pipe(S.withDecodingDefault(() => 6379)),
});

const RuntimeCCXTConfig = S.Struct({
  service_url: S.optional(S.String).pipe(
    S.withDecodingDefault(() => "http://localhost:3001"),
  ),
  grpc_address: S.optional(S.String).pipe(
    S.withDecodingDefault(() => "127.0.0.1:50051"),
  ),
});

const RuntimeTelegramConfig = S.Struct({
  service_url: S.optional(S.String).pipe(
    S.withDecodingDefault(() => "http://localhost:3002"),
  ),
  grpc_address: S.optional(S.String).pipe(
    S.withDecodingDefault(() => "127.0.0.1:50052"),
  ),
  use_polling: S.optional(S.Boolean).pipe(S.withDecodingDefault(() => true)),
  api_base_url: S.optional(S.String).pipe(
    S.withDecodingDefault(() => "http://localhost:8080"),
  ),
});

const RuntimeAIConfig = S.Struct({
  provider: S.optional(S.String).pipe(S.withDecodingDefault(() => "openai")),
  model: S.optional(S.String).pipe(S.withDecodingDefault(() => "gpt-4o-mini")),
  base_url: S.optional(S.String),
  temperature: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0.7)),
  max_tokens: S.optional(S.Number).pipe(S.withDecodingDefault(() => 4096)),
  min_confidence: S.optional(S.Number).pipe(S.withDecodingDefault(() => 0.7)),
  // Go uses decimal.Decimal which JSON-encodes as a string
  daily_budget: S.optional(S.String).pipe(S.withDecodingDefault(() => "10")),
  routing_mode: S.optional(S.String).pipe(
    S.withDecodingDefault(() => "primary"),
  ),
});

const RuntimeFeaturesConfig = S.Struct({
  enable_ai: S.optional(S.Boolean).pipe(S.withDecodingDefault(() => true)),
  enable_ai_scalping: S.optional(S.Boolean).pipe(
    S.withDecodingDefault(() => true),
  ),
  enable_ai_signals: S.optional(S.Boolean).pipe(
    S.withDecodingDefault(() => false),
  ),
  enable_ai_arbitrage: S.optional(S.Boolean).pipe(
    S.withDecodingDefault(() => false),
  ),
  paper_trading: S.optional(S.Boolean).pipe(S.withDecodingDefault(() => true)),
  real_trading: S.optional(S.Boolean).pipe(S.withDecodingDefault(() => false)),
});

const RuntimeGatewayConfig = S.Struct({
  bind_host: S.optional(S.String).pipe(
    S.withDecodingDefault(() => "127.0.0.1"),
  ),
  ccxt_port: S.optional(S.Number).pipe(S.withDecodingDefault(() => 3001)),
  telegram_port: S.optional(S.Number).pipe(S.withDecodingDefault(() => 3002)),
  telegram_grpc_port: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 50052),
  ),
  supervised: S.optional(S.Boolean).pipe(S.withDecodingDefault(() => false)),
  health_timeout_seconds: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 150),
  ),
  signal_timeout_seconds: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 5),
  ),
  graceful_timeout_seconds: S.optional(S.Number).pipe(
    S.withDecodingDefault(() => 10),
  ),
  skip_telegram: S.optional(S.Boolean).pipe(S.withDecodingDefault(() => false)),
});

// -- Main runtimeConfig schema --
// All nested objects are optional because Go's json.Unmarshal leaves missing
// fields at their zero values. When a nested object is absent, the default
// value is applied and inner fields fill in their own defaults.

export const RuntimeConfigSchema = S.Struct({
  server: S.optional(RuntimeServerConfig).pipe(
    S.withDecodingDefault(() => ({
      host: "0.0.0.0",
      port: 8080,
    })),
  ),
  database: S.optional(RuntimeDatabaseConfig).pipe(
    S.withDecodingDefault(() => ({
      driver: "sqlite",
      sqlite_path: "",
    })),
  ),
  redis: S.optional(RuntimeRedisConfig).pipe(
    S.withDecodingDefault(() => ({
      host: "127.0.0.1",
      port: 6379,
    })),
  ),
  ccxt: S.optional(RuntimeCCXTConfig).pipe(
    S.withDecodingDefault(() => ({
      service_url: "http://localhost:3001",
      grpc_address: "127.0.0.1:50051",
    })),
  ),
  telegram: S.optional(RuntimeTelegramConfig).pipe(
    S.withDecodingDefault(() => ({
      service_url: "http://localhost:3002",
      grpc_address: "127.0.0.1:50052",
      use_polling: true,
      api_base_url: "http://localhost:8080",
    })),
  ),
  ai: S.optional(RuntimeAIConfig).pipe(
    S.withDecodingDefault(() => ({
      provider: "openai",
      model: "gpt-4o-mini",
      base_url: undefined,
      temperature: 0.7,
      max_tokens: 4096,
      min_confidence: 0.7,
      daily_budget: "10",
      routing_mode: "primary",
    })),
  ),
  features: S.optional(RuntimeFeaturesConfig).pipe(
    S.withDecodingDefault(() => ({
      enable_ai: true,
      enable_ai_scalping: true,
      enable_ai_signals: false,
      enable_ai_arbitrage: false,
      paper_trading: true,
      real_trading: false,
    })),
  ),
  gateway: S.optional(RuntimeGatewayConfig).pipe(
    S.withDecodingDefault(() => ({
      bind_host: "127.0.0.1",
      ccxt_port: 3001,
      telegram_port: 3002,
      telegram_grpc_port: 50052,
      supervised: false,
      health_timeout_seconds: 150,
      signal_timeout_seconds: 5,
      graceful_timeout_seconds: 10,
      skip_telegram: false,
    })),
  ),
});

export type RuntimeConfig = typeof RuntimeConfigSchema.Type;

/** Decode an unknown JSON value into RuntimeConfig (returns Effect). */
export const decodeRuntimeConfig = S.decodeUnknown(RuntimeConfigSchema);

/** Decode an unknown JSON value, returning Either. */
export const decodeRuntimeConfigEither =
  S.decodeUnknownEither(RuntimeConfigSchema);
