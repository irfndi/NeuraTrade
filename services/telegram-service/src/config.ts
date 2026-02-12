import { Effect } from "effect";

/**
 * Resolves a port number from a string environment variable.
 * Falls back to a default value if the provided value is invalid.
 *
 * @param raw - The raw string value from environment variable
 * @param fallback - The default port to use if raw is invalid
 * @returns The resolved port number
 */
export const resolvePort = (raw: string | undefined, fallback: number): number => {
  if (!raw) {
    return fallback;
  }
  const numericPort = Number(raw);
  if (!Number.isNaN(numericPort) && numericPort > 0 && numericPort < 65536) {
    return numericPort;
  }
  console.warn(
    `Invalid port value provided (${raw}). Falling back to default (${fallback}).`,
  );
  return fallback;
};

/**
 * Configuration for the Telegram bot service.
 *
 * @property botToken - Telegram bot authentication token (required)
 * @property webhookUrl - Full URL for Telegram webhook (null for polling mode)
 * @property webhookPath - Path component of webhook URL (e.g., "/telegram/webhook")
 * @property webhookSecret - Secret token for webhook verification (optional)
 * @property usePolling - Whether to use polling mode instead of webhooks
 * @property port - HTTP server port for health/admin endpoints
 * @property apiBaseUrl - Base URL for backend API requests
 * @property adminApiKey - API key for admin-protected endpoints
 * @property grpcPort - Port for gRPC server
 */
export interface TelegramConfig {
  botToken: string;
  webhookUrl: string | null;
  webhookPath: string;
  webhookSecret: string | null;
  usePolling: boolean;
  port: number;
  apiBaseUrl: string;
  adminApiKey: string;
  grpcPort: number;
}

/**
 * Effect-based configuration loader for the Telegram service.
 *
 * Reads configuration from environment variables and validates:
 * - TELEGRAM_BOT_TOKEN or TELEGRAM_TOKEN: Required
 * - ADMIN_API_KEY: Required in production, must be >= 32 chars
 * - TELEGRAM_API_BASE_URL: Backend API URL (default: http://localhost:8080)
 * - TELEGRAM_WEBHOOK_URL: Full webhook URL (enables webhook mode)
 * - TELEGRAM_WEBHOOK_PATH: Webhook path override
 * - TELEGRAM_WEBHOOK_SECRET: Secret for webhook verification
 * - TELEGRAM_USE_POLLING: Force polling mode ("true" or "1")
 * - TELEGRAM_PORT: HTTP server port (default: 3002)
 * - TELEGRAM_GRPC_PORT: gRPC server port (default: 50052)
 * - NODE_ENV / SENTRY_ENVIRONMENT: Environment detection
 *
 * @returns Effect that yields a validated TelegramConfig
 * @throws Error if required configuration is missing or invalid
 *
 * @example
 * ```typescript
 * import { Effect } from "effect";
 * import { loadConfig } from "./config";
 *
 * const config = Effect.runSync(loadConfig);
 * console.log(config.botToken);
 * ```
 */
export const loadConfig = Effect.try((): TelegramConfig => {
  const botToken = process.env.TELEGRAM_BOT_TOKEN || process.env.TELEGRAM_TOKEN;
  if (!botToken) {
    throw new Error(
      "TELEGRAM_BOT_TOKEN or TELEGRAM_TOKEN environment variable must be set",
    );
  }

  const adminApiKey = process.env.ADMIN_API_KEY || "";
  const isProduction =
    process.env.NODE_ENV === "production" ||
    process.env.SENTRY_ENVIRONMENT === "production";

  if (isProduction) {
    if (!adminApiKey) {
      throw new Error(
        "ADMIN_API_KEY environment variable must be set in production",
      );
    }

    if (
      adminApiKey === "admin-secret-key-change-me" ||
      adminApiKey === "admin-dev-key-change-in-production"
    ) {
      throw new Error(
        "ADMIN_API_KEY cannot use default/example values. Please set a secure API key.",
      );
    }

    if (adminApiKey.length < 32) {
      throw new Error(
        "ADMIN_API_KEY must be at least 32 characters long for security",
      );
    }
  } else if (!adminApiKey) {
    console.warn(
      "⚠️ WARNING: ADMIN_API_KEY is not set. Admin endpoints will be disabled.",
    );
  }

  const apiBaseUrl = (
    process.env.TELEGRAM_API_BASE_URL || "http://localhost:8080"
  ).replace(/\/$/, "");

  const webhookUrlRaw = (process.env.TELEGRAM_WEBHOOK_URL || "").trim();
  const webhookUrl = webhookUrlRaw.length > 0 ? webhookUrlRaw : null;
  const webhookPath = (process.env.TELEGRAM_WEBHOOK_PATH || "").trim();
  const resolvedWebhookPath = webhookPath
    ? webhookPath
    : webhookUrl
      ? new URL(webhookUrl).pathname
      : "/telegram/webhook";

  const usePollingEnv = (process.env.TELEGRAM_USE_POLLING || "").toLowerCase();
  const usePolling =
    usePollingEnv === "true" || usePollingEnv === "1" || webhookUrl === null;

  const grpcPort = process.env.TELEGRAM_GRPC_PORT
    ? parseInt(process.env.TELEGRAM_GRPC_PORT, 10)
    : 50052;

  return {
    botToken,
    webhookUrl,
    webhookPath: resolvedWebhookPath.startsWith("/")
      ? resolvedWebhookPath
      : `/${resolvedWebhookPath}`,
    webhookSecret: process.env.TELEGRAM_WEBHOOK_SECRET || null,
    usePolling,
    port: resolvePort(process.env.TELEGRAM_PORT, 3002),
    apiBaseUrl,
    adminApiKey,
    grpcPort,
  };
});

/**
 * Loaded and validated configuration singleton.
 *
 * This is executed at module load time and will throw if configuration is invalid.
 * Use this for synchronous access to configuration throughout the application.
 *
 * @example
 * ```typescript
 * import { config } from "./config";
 *
 * console.log(`Running on port ${config.port}`);
 * ```
 */
export const config = Effect.runSync(loadConfig);

/**
 * Environment variable names used by the Telegram service.
 */
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
  NODE_ENV: "NODE_ENV",
  SENTRY_ENVIRONMENT: "SENTRY_ENVIRONMENT",
} as const;

export type EnvVarName = (typeof ENV_VARS)[keyof typeof ENV_VARS];
