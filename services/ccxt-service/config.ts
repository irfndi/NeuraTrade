import { readFileSync, existsSync } from "fs";
import { join } from "path";
import { homedir } from "os";

export type NeuratradeConfig = {
  admin_api_key?: string;
  ccxt?: {
    service_url?: string;
  };
  telegram?: {
    enabled?: boolean;
    bot_token?: string;
    api_base_url?: string;
    use_polling?: boolean;
    port?: number;
  };
  services?: {
    ccxt?: {
      url?: string;
    };
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

export const loadNeuratradeConfig = (): NeuratradeConfig | null => {
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

  if (keyLower === "admin_api_key") {
    if (neuratradeConfig.security?.admin_api_key) {
      return neuratradeConfig.security.admin_api_key;
    }
    return (neuratradeConfig as any).admin_api_key;
  }
  if (keyLower === "ccxt_service_url" || keyLower === "ccxt_url") {
    if (neuratradeConfig.ccxt?.service_url) {
      return neuratradeConfig.ccxt.service_url;
    }
    return neuratradeConfig.services?.ccxt?.url;
  }
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

  return undefined;
};
