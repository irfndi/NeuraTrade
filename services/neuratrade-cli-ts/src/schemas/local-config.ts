/**
 * Schema definitions for localConfig (config.json).
 *
 * Mirrors the Go struct in cmd/neuratrade-cli/local_config.go.
 * All string fields are optional — the Go loader treats missing/empty as ""
 * and resolves values via fallback chains at runtime.
 */
import * as S from "effect/Schema";

// -- Nested sub-schemas --

const AuthConfig = S.Struct({
  jwt_secret: S.optional(S.String),
});

const ServerConfig = S.Struct({
  host: S.optional(S.String),
  port: S.optional(S.Number),
});

const DatabaseConfig = S.Struct({
  driver: S.optional(S.String),
  sqlite_path: S.optional(S.String),
});

const CCXTConfig = S.Struct({
  admin_api_key: S.optional(S.String),
  service_url: S.optional(S.String),
  grpc_address: S.optional(S.String),
});

const TelegramConfig = S.Struct({
  bot_token: S.optional(S.String),
  api_base_url: S.optional(S.String),
  service_url: S.optional(S.String),
  grpc_address: S.optional(S.String),
  chat_id: S.optional(S.String),
});

const ServicesTelegramConfig = S.Struct({
  chat_id: S.optional(S.String),
});

const ServicesConfig = S.Struct({
  telegram: S.optional(ServicesTelegramConfig),
});

const SecurityConfig = S.Struct({
  admin_api_key: S.optional(S.String),
  jwt_secret: S.optional(S.String),
});

const AIConfig = S.Struct({
  api_key: S.optional(S.String),
  base_url: S.optional(S.String),
  provider: S.optional(S.String),
  model: S.optional(S.String),
});

// -- Main localConfig schema --

export const LocalConfigSchema = S.Struct({
  admin_api_key: S.optional(S.String),
  telegram_test_chat_id: S.optional(S.String),
  auth: S.optional(AuthConfig),
  server: S.optional(ServerConfig),
  database: S.optional(DatabaseConfig),
  ccxt: S.optional(CCXTConfig),
  telegram: S.optional(TelegramConfig),
  services: S.optional(ServicesConfig),
  security: S.optional(SecurityConfig),
  ai: S.optional(AIConfig),
});

export type LocalConfig = typeof LocalConfigSchema.Type;

/** Decode an unknown JSON value into LocalConfig (returns Effect). */
export const decodeLocalConfig = S.decodeUnknown(LocalConfigSchema);

/** Decode an unknown JSON value, returning Either. */
export const decodeLocalConfigEither = S.decodeUnknownEither(LocalConfigSchema);
