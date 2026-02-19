export type TelegramConfig = {
  botToken: string;
  webhookUrl: string | null;
  webhookPath: string;
  webhookSecret: string | null;
  usePolling: boolean;
  port: number;
  apiBaseUrl: string;
  adminApiKey: string;
};

export type TelegramConfigPartial = TelegramConfig & {
  botTokenMissing: boolean;
  configError: string | null;
  // adminApiKeyMissing is deprecated - internal endpoints no longer require auth
  adminApiKeyMissing: boolean;
};

import { readFileSync, existsSync } from "fs";
import { join } from "path";
import { homedir } from "os";

type NeuratradeConfig = {
  telegram?: {
    enabled?: boolean;
    bot_token?: string;
    api_base_url?: string;
    use_polling?: boolean;
    port?: number;
  };
  services?: {
    telegram?: {
      enabled?: boolean;
      bot_token?: string;
      api_base_url?: string;
      use_polling?: boolean;
      port?: number;
    };
  };
  security?: {
    admin_api_key?: string;
  };
};

let cachedConfig: NeuratradeConfig | null = null;

export const resetNeuratradeConfigCache = () => {
  cachedConfig = null;
};

const loadNeuratradeConfig = (): NeuratradeConfig | null => {
  if (cachedConfig !== null) {
    return cachedConfig;
  }
  const configPath = join(homedir(), ".neuratrade", "config.json");
  if (!existsSync(configPath)) {
    cachedConfig = null;
    return null;
  }
  try {
    const content = readFileSync(configPath, "utf-8");
    cachedConfig = JSON.parse(content);
    return cachedConfig;
  } catch {
    cachedConfig = null;
    return null;
  }
};

export const getEnvWithNeuratradeFallback = (
  key: string,
): string | undefined => {
  if (process.env[key]) {
    return process.env[key];
  }
  const neuratradeConfig = loadNeuratradeConfig();
  if (!neuratradeConfig) {
    return undefined;
  }
  const keyLower = key.toLowerCase();
  if (keyLower === "telegram_bot_token") {
    if (neuratradeConfig.telegram?.bot_token) {
      return neuratradeConfig.telegram.bot_token;
    }
    return neuratradeConfig.services?.telegram?.bot_token;
  }
  if (keyLower === "telegram_api_base_url") {
    if (neuratradeConfig.telegram?.api_base_url) {
      return neuratradeConfig.telegram.api_base_url;
    }
    return neuratradeConfig.services?.telegram?.api_base_url;
  }
  if (keyLower === "telegram_use_polling") {
    if (neuratradeConfig.telegram?.use_polling !== undefined) {
      return neuratradeConfig.telegram.use_polling.toString();
    }
    return neuratradeConfig.services?.telegram?.use_polling?.toString();
  }
  if (keyLower === "telegram_port") {
    if (neuratradeConfig.telegram?.port !== undefined) {
      return neuratradeConfig.telegram.port.toString();
    }
    return neuratradeConfig.services?.telegram?.port?.toString();
  }
  if (keyLower === "admin_api_key") {
    return neuratradeConfig.security?.admin_api_key;
  }
  return undefined;
};

const resolvePort = (raw: string | undefined, fallback: number) => {
  if (!raw) {
    return fallback;
  }
  const numericPort = Number(raw);
  if (!Number.isNaN(numericPort) && numericPort > 0 && numericPort < 65536) {
    return numericPort;
  }
  console.warn(`Invalid port value provided. Falling back to default (${fallback}).`);
  return fallback;
};

export const loadConfig = (): TelegramConfigPartial => {
  // Support both TELEGRAM_BOT_TOKEN and TELEGRAM_TOKEN for compatibility with different platforms
  const botToken =
    getEnvWithNeuratradeFallback("TELEGRAM_BOT_TOKEN") ||
    process.env.TELEGRAM_TOKEN ||
    "";
  const botTokenMissing = !botToken;

  // ADMIN_API_KEY is no longer required - internal endpoints use network isolation
  // Keeping the field for backwards compatibility but it's not validated or used
  const adminApiKey = getEnvWithNeuratradeFallback("ADMIN_API_KEY") || "";
  const adminApiKeyMissing = !adminApiKey; // Deprecated, always false effectively

  let configError: string | null = null;

  // No ADMIN_API_KEY validation needed - internal endpoints are network-isolated

  if (botTokenMissing) {
    console.error(
      "❌ CRITICAL: TELEGRAM_BOT_TOKEN environment variable is not set!",
    );
    console.error(
      "   The service will start in degraded mode (health check only).",
    );
    console.error(
      "   Bot functionality will be disabled until the token is configured.",
    );
  }

  const apiBaseUrl = (
    getEnvWithNeuratradeFallback("TELEGRAM_API_BASE_URL") ||
    process.env.TELEGRAM_API_BASE_URL ||
    "http://localhost:8080"
  ).replace(/\/$/, "");

  const webhookUrlRaw = (process.env.TELEGRAM_WEBHOOK_URL || "").trim();
  const webhookUrl = webhookUrlRaw.length > 0 ? webhookUrlRaw : null;
  const webhookPath = (process.env.TELEGRAM_WEBHOOK_PATH || "").trim();
  const resolvedWebhookPath = webhookPath
    ? webhookPath
    : webhookUrl
      ? new URL(webhookUrl).pathname
      : "/telegram/webhook";

  const usePollingEnv = (
    getEnvWithNeuratradeFallback("TELEGRAM_USE_POLLING") || ""
  ).toLowerCase();
  const usePolling =
    usePollingEnv === "true" || usePollingEnv === "1" || webhookUrl === null;

  return {
    botToken,
    botTokenMissing,
    configError,
    adminApiKeyMissing,
    webhookUrl,
    webhookPath: resolvedWebhookPath.startsWith("/")
      ? resolvedWebhookPath
      : `/${resolvedWebhookPath}`,
    webhookSecret: process.env.TELEGRAM_WEBHOOK_SECRET || null,
    usePolling,
    port: resolvePort(
      getEnvWithNeuratradeFallback("TELEGRAM_PORT") ||
        process.env.TELEGRAM_PORT,
      3002,
    ),
    apiBaseUrl,
    adminApiKey,
  };
};
