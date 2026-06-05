import {
  Effect,
  Config,
  ConfigError,
  Schema,
  ParseResult,
  Context,
  Layer,
} from "effect";
import { getEnvWithNeuratradeFallback } from "../config";

export interface TelegramConfig {
  botToken: string;
  botTokenMissing: boolean;
  configError: string | null;
  webhookUrl: string | null;
  webhookPath: string;
  webhookSecret: string | null;
  usePolling: boolean;
  port: number;
  apiBaseUrl: string;
  adminApiKey: string;
  grpcPort: number;
  grpcBindAddr: string;
}

export const ENV_VARS = {
  TELEGRAM_BOT_TOKEN: "TELEGRAM_BOT_TOKEN",
  TELEGRAM_TOKEN: "TELEGRAM_TOKEN",
  ADMIN_API_KEY: "ADMIN_API_KEY",
  TELEGRAM_API_BASE_URL: "TELEGRAM_API_BASE_URL",
  TELEGRAM_WEBHOOK_URL: "TELEGRAM_WEBHOOK_URL",
  TELEGRAM_WEBHOOK_PATH: "TELEGRAM_WEBHOOK_PATH",
  TELEGRAM_WEBHOOK_SECRET: "TELEGRAM_WEBHOOK_SECRET",
  TELEGRAM_USE_POLLING: "TELEGRAM_USE_POLLING",
  TELEGRAM_PORT: "TELEGRAM_PORT",
  TELEGRAM_GRPC_PORT: "TELEGRAM_GRPC_PORT",
  GRPC_BIND_ADDR: "GRPC_BIND_ADDR",
  NODE_ENV: "NODE_ENV",
  SENTRY_ENVIRONMENT: "SENTRY_ENVIRONMENT",
} as const;

export type EnvVarName = (typeof ENV_VARS)[keyof typeof ENV_VARS];

const TelegramConfigSchema = Schema.Struct({
  botToken: Schema.String,
  botTokenMissing: Schema.Boolean,
  configError: Schema.Union(Schema.Null, Schema.String),
  webhookUrl: Schema.Union(Schema.Null, Schema.String),
  webhookPath: Schema.String,
  webhookSecret: Schema.Union(Schema.Null, Schema.String),
  usePolling: Schema.Boolean,
  port: Schema.Number,
  apiBaseUrl: Schema.String,
  adminApiKey: Schema.String,
  grpcPort: Schema.Number,
  grpcBindAddr: Schema.String,
});

const resolvePort = (raw: string | undefined, fallback: number): number => {
  if (!raw) return fallback;
  if (!/^\d+$/.test(raw)) {
    console.warn(
      `Invalid port value provided (${raw}). Falling back to default (${fallback}).`,
    );
    return fallback;
  }
  const numericPort = Number(raw);
  if (Number.isInteger(numericPort) && numericPort > 0 && numericPort < 65536) {
    return numericPort;
  }
  console.warn(
    `Invalid port value provided (${raw}). Falling back to default (${fallback}).`,
  );
  return fallback;
};

const fromConfigWithFallback = (
  key: string,
): Effect.Effect<string, ConfigError.ConfigError> =>
  Config.string(key).pipe(
    Effect.flatMap((val) =>
      val.trim() === ""
        ? Effect.fail(
            ConfigError.MissingData(
              [key],
              `${key} is set to an empty string`,
            ),
          )
        : Effect.succeed(val),
    ),
    Effect.catchAll((err) =>
      Effect.sync(() => getEnvWithNeuratradeFallback(key)).pipe(
        Effect.flatMap(
          (val): Effect.Effect<string, ConfigError.ConfigError> =>
            val && val.trim() !== ""
              ? Effect.succeed(val)
              : Effect.fail(err),
        ),
      ),
    ),
  );

export const loadConfig: Effect.Effect<
  TelegramConfig,
  ConfigError.ConfigError
> = Effect.gen(function* () {
  const botToken = yield* fromConfigWithFallback("TELEGRAM_BOT_TOKEN").pipe(
    Effect.catchAll(() =>
      Config.string("TELEGRAM_TOKEN").pipe(
        Effect.catchAll(() => Effect.succeed("")),
      ),
    ),
  );
  const botTokenMissing = !botToken;

  const adminApiKey = yield* fromConfigWithFallback("ADMIN_API_KEY").pipe(
    Effect.catchAll(() => Effect.succeed("")),
  );

  const nodeEnv = yield* Config.string("NODE_ENV").pipe(
    Effect.catchAll(() => Effect.succeed("")),
  );
  const sentryEnv = yield* Config.string("SENTRY_ENVIRONMENT").pipe(
    Effect.catchAll(() => Effect.succeed("")),
  );
  const isProduction = nodeEnv === "production" || sentryEnv === "production";

  if (isProduction) {
    if (!adminApiKey) {
      return yield* Effect.fail(
        ConfigError.MissingData(
          ["ADMIN_API_KEY"],
          "ADMIN_API_KEY environment variable must be set in production",
        ),
      );
    }
    if (
      adminApiKey === "admin-secret-key-change-me" ||
      adminApiKey === "admin-dev-key-change-in-production"
    ) {
      return yield* Effect.fail(
        ConfigError.InvalidData(
          ["ADMIN_API_KEY"],
          "ADMIN_API_KEY cannot use default/example values. Please set a secure API key.",
        ),
      );
    }
    if (adminApiKey.length < 32) {
      return yield* Effect.fail(
        ConfigError.InvalidData(
          ["ADMIN_API_KEY"],
          "ADMIN_API_KEY must be at least 32 characters long for security",
        ),
      );
    }
  } else if (!adminApiKey) {
    console.warn(
      "WARNING: ADMIN_API_KEY is not set. Admin endpoints will be disabled.",
    );
  }

  const rawApiBaseUrl = yield* fromConfigWithFallback(
    "TELEGRAM_API_BASE_URL",
  ).pipe(Effect.catchAll(() => Effect.succeed("http://localhost:8080")));
  const apiBaseUrl = rawApiBaseUrl.includes("api.telegram.org")
    ? "http://localhost:8080"
    : rawApiBaseUrl.replace(/\/$/, "");

  const webhookUrlRaw = (yield* Config.string("TELEGRAM_WEBHOOK_URL").pipe(
    Effect.catchAll(() => Effect.succeed("")),
  )).trim();
  const webhookUrl = webhookUrlRaw.length > 0 ? webhookUrlRaw : null;

  const webhookPath = (yield* Config.string("TELEGRAM_WEBHOOK_PATH").pipe(
    Effect.catchAll(() => Effect.succeed("")),
  )).trim();
  const resolvedWebhookPath = webhookPath
    ? webhookPath
    : webhookUrl
      ? yield* Effect.try({
          try: () => new URL(webhookUrl).pathname,
          catch: (err) =>
            ConfigError.InvalidData(
              ["TELEGRAM_WEBHOOK_URL"],
              `Invalid TELEGRAM_WEBHOOK_URL: ${err instanceof Error ? err.message : String(err)}`,
            ),
        })
      : "/telegram/webhook";

  const webhookSecret =
    (yield* Config.string("TELEGRAM_WEBHOOK_SECRET").pipe(
      Effect.catchAll(() => Effect.succeed("")),
    )).trim() || null;

  const usePollingEnv = (yield* fromConfigWithFallback(
    "TELEGRAM_USE_POLLING",
  ).pipe(Effect.catchAll(() => Effect.succeed("")))).toLowerCase();
  const usePolling =
    usePollingEnv === "true" || usePollingEnv === "1" || webhookUrl === null;

  if (!usePolling && !webhookSecret) {
    return yield* Effect.fail(
      ConfigError.InvalidData(
        ["TELEGRAM_WEBHOOK_SECRET"],
        "TELEGRAM_WEBHOOK_SECRET must be set when webhook mode is enabled (TELEGRAM_USE_POLLING is not true)",
      ),
    );
  }

  const port = resolvePort(
    (yield* fromConfigWithFallback("TELEGRAM_PORT").pipe(
      Effect.catchAll(() => Effect.succeed("")),
    )) || undefined,
    3002,
  );

  const grpcPortStr = yield* Config.string("TELEGRAM_GRPC_PORT").pipe(
    Effect.catchAll(() => Effect.succeed("")),
  );
  const grpcPort = resolvePort(grpcPortStr || undefined, 50052);

  const grpcBindAddr =
    (yield* Config.string("GRPC_BIND_ADDR").pipe(
      Effect.catchAll(() => Effect.succeed("127.0.0.1")),
    )).trim() || "127.0.0.1";

  const configData: TelegramConfig = {
    botToken,
    botTokenMissing,
    configError: null,
    webhookUrl,
    webhookPath: resolvedWebhookPath.startsWith("/")
      ? resolvedWebhookPath
      : `/${resolvedWebhookPath}`,
    webhookSecret,
    usePolling,
    port,
    apiBaseUrl,
    adminApiKey,
    grpcPort,
    grpcBindAddr,
  };

  return yield* Schema.decodeUnknown(TelegramConfigSchema)(configData).pipe(
    Effect.mapError((parseErrors) =>
      ConfigError.InvalidData(
        ["TelegramConfig"],
        `Schema validation failed: ${ParseResult.TreeFormatter.formatErrorSync(parseErrors)}`,
      ),
    ),
  );
});

export const TelegramConfigTag =
  Context.GenericTag<TelegramConfig>("TelegramConfigTag");

export const TelegramConfigLive: Layer.Layer<
  TelegramConfig,
  ConfigError.ConfigError
> = Layer.effect(TelegramConfigTag, loadConfig);

export const config: TelegramConfig = Effect.runSync(loadConfig);
