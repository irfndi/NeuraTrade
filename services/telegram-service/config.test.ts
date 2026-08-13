import {
  test,
  expect,
  beforeAll,
  afterAll,
  afterEach,
  describe,
  beforeEach,
} from "bun:test";
import { Effect } from "effect";
import { writeFileSync, mkdirSync, rmSync } from "fs";
import { join } from "path";
import {
  getEnvWithNeuratradeFallback,
  resetNeuratradeConfigCache,
} from "./config";
import { loadConfig } from "./src/config";

interface EnvSnapshot {
  [key: string]: string | undefined;
}

describe("Neuratrade config fallback", () => {
  const neuratradeDir = join(
    import.meta.dir,
    ".tmp-test-neuratrade-home-config",
  );
  const realConfigPath = join(neuratradeDir, "config.json");
  const originalNeuratradeHome = process.env.NEURATRADE_HOME;
  let originalEnv: EnvSnapshot;

  beforeAll(() => {
    rmSync(neuratradeDir, { recursive: true, force: true });
    mkdirSync(neuratradeDir, { recursive: true });
    process.env.NEURATRADE_HOME = neuratradeDir;
  });

  afterAll(() => {
    if (originalNeuratradeHome === undefined) {
      delete process.env.NEURATRADE_HOME;
    } else {
      process.env.NEURATRADE_HOME = originalNeuratradeHome;
    }
    rmSync(neuratradeDir, { recursive: true, force: true });
  });

  beforeEach(() => {
    originalEnv = {
      TELEGRAM_BOT_TOKEN: process.env.TELEGRAM_BOT_TOKEN,
      TELEGRAM_API_BASE_URL: process.env.TELEGRAM_API_BASE_URL,
      ADMIN_API_KEY: process.env.ADMIN_API_KEY,
    };
    resetNeuratradeConfigCache();
    delete process.env.TELEGRAM_BOT_TOKEN;
    delete process.env.TELEGRAM_API_BASE_URL;
    delete process.env.ADMIN_API_KEY;
  });

  afterEach(() => {
    for (const [key, value] of Object.entries(originalEnv)) {
      if (value === undefined) {
        delete process.env[key];
      } else {
        process.env[key] = value;
      }
    }
  });

  test("getEnvWithNeuratradeFallback returns env var when set", () => {
    const testValue = "env-bot-token-12345";
    process.env.TELEGRAM_BOT_TOKEN = testValue;

    const result = getEnvWithNeuratradeFallback("TELEGRAM_BOT_TOKEN");
    expect(result).toBe(testValue);
  });

  test("getEnvWithNeuratradeFallback falls back to config.json for bot_token", () => {
    const configContent = JSON.stringify({
      services: {
        telegram: {
          enabled: true,
          bot_token: "config-bot-token-67890",
          api_base_url: "http://localhost:8080",
        },
      },
      security: {
        admin_api_key: "config-admin-key",
      },
    });
    writeFileSync(realConfigPath, configContent, "utf-8");

    const result = getEnvWithNeuratradeFallback("TELEGRAM_BOT_TOKEN");
    expect(result).toBe("config-bot-token-67890");
  });

  test("getEnvWithNeuratradeFallback returns undefined when neither set", () => {
    writeFileSync(realConfigPath, JSON.stringify({}), "utf-8");

    const result = getEnvWithNeuratradeFallback("TELEGRAM_BOT_TOKEN");
    expect(result).toBeUndefined();
  });
});

describe("Effect-based loadConfig", () => {
  const originalEnv: EnvSnapshot = {};
  const isolatedNeuratradeDir = join(
    import.meta.dir,
    ".tmp-effect-config-test",
  );
  const originalNeuratradeHome = process.env.NEURATRADE_HOME;

  beforeAll(() => {
    // Save env vars we might modify
    for (const key of [
      "TELEGRAM_BOT_TOKEN",
      "TELEGRAM_TOKEN",
      "TELEGRAM_WEBHOOK_URL",
      "TELEGRAM_WEBHOOK_SECRET",
      "TELEGRAM_USE_POLLING",
      "TELEGRAM_PORT",
      "TELEGRAM_GRPC_PORT",
      "GRPC_BIND_ADDR",
      "TELEGRAM_API_BASE_URL",
      "ADMIN_API_KEY",
      "NODE_ENV",
      "SENTRY_ENVIRONMENT",
    ] as const) {
      originalEnv[key] = process.env[key];
    }

    // Isolate neuratrade config to prevent real config.json from leaking in
    rmSync(isolatedNeuratradeDir, { recursive: true, force: true });
    mkdirSync(isolatedNeuratradeDir, { recursive: true });
    process.env.NEURATRADE_HOME = isolatedNeuratradeDir;
    resetNeuratradeConfigCache();

    // Ensure all required env vars are set for base tests
    process.env.TELEGRAM_BOT_TOKEN = "test-bot-token-12345";
    process.env.ADMIN_API_KEY =
      "test-admin-key-that-is-at-least-32-characters-long";
    process.env.TELEGRAM_WEBHOOK_SECRET = "test-webhook-secret-32-chars!";
    delete process.env.NODE_ENV;
    delete process.env.SENTRY_ENVIRONMENT;
    delete process.env.TELEGRAM_WEBHOOK_URL;
    delete process.env.TELEGRAM_USE_POLLING;
  });

  afterAll(() => {
    // Restore NEURATRADE_HOME
    if (originalNeuratradeHome === undefined) {
      delete process.env.NEURATRADE_HOME;
    } else {
      process.env.NEURATRADE_HOME = originalNeuratradeHome;
    }
    resetNeuratradeConfigCache();
    rmSync(isolatedNeuratradeDir, { recursive: true, force: true });

    // Restore all env vars
    for (const [key, value] of Object.entries(originalEnv)) {
      if (value === undefined) {
        delete process.env[key];
      } else {
        process.env[key] = value;
      }
    }
  });

  test("loads config successfully with all env vars set", () => {
    const config = Effect.runSync(loadConfig);
    expect(config.botToken).toBe("test-bot-token-12345");
    expect(config.botTokenMissing).toBe(false);
    expect(config.configError).toBeNull();
    expect(config.port).toBeGreaterThan(0);
    expect(config.port).toBeLessThan(65536);
    expect(config.apiBaseUrl).toBeTruthy();
    expect(config.adminApiKey).toEqual(expect.any(String));
    expect(config.grpcPort).toBeGreaterThan(0);
    expect(config.grpcBindAddr).toBeTruthy();
  });

  test("loads degraded config when bot token is missing", () => {
    const origToken = process.env.TELEGRAM_BOT_TOKEN;
    delete process.env.TELEGRAM_BOT_TOKEN;
    delete process.env.TELEGRAM_TOKEN;
    // Ensure neuratrade config cache doesn't provide a fallback
    resetNeuratradeConfigCache();

    try {
      const config = Effect.runSync(loadConfig);
      expect(config.botToken).toBe("");
      expect(config.botTokenMissing).toBe(true);
    } finally {
      process.env.TELEGRAM_BOT_TOKEN = origToken;
    }
  });

  test("fails when webhook mode requires secret token", () => {
    const origWebhookUrl = process.env.TELEGRAM_WEBHOOK_URL;
    const origUsePolling = process.env.TELEGRAM_USE_POLLING;
    const origWebhookSecret = process.env.TELEGRAM_WEBHOOK_SECRET;

    try {
      // Enable webhook mode without TELEGRAM_WEBHOOK_SECRET
      process.env.TELEGRAM_USE_POLLING = "false";
      process.env.TELEGRAM_WEBHOOK_URL = "https://example.com/webhook";
      delete process.env.TELEGRAM_WEBHOOK_SECRET;

      expect(() => Effect.runSync(loadConfig)).toThrow();
    } finally {
      if (origWebhookUrl === undefined) {
        delete process.env.TELEGRAM_WEBHOOK_URL;
      } else {
        process.env.TELEGRAM_WEBHOOK_URL = origWebhookUrl;
      }
      if (origUsePolling === undefined) {
        delete process.env.TELEGRAM_USE_POLLING;
      } else {
        process.env.TELEGRAM_USE_POLLING = origUsePolling;
      }
      if (origWebhookSecret === undefined) {
        delete process.env.TELEGRAM_WEBHOOK_SECRET;
      } else {
        process.env.TELEGRAM_WEBHOOK_SECRET = origWebhookSecret;
      }
    }
  });

  test("validates port is a positive number", () => {
    const origPort = process.env.TELEGRAM_PORT;

    try {
      process.env.TELEGRAM_PORT = "invalid_port";

      const config = Effect.runSync(loadConfig);
      expect(config.port).toBe(3002);
    } finally {
      if (origPort === undefined) {
        delete process.env.TELEGRAM_PORT;
      } else {
        process.env.TELEGRAM_PORT = origPort;
      }
    }
  });

  test("fails in production when ADMIN_API_KEY is missing", () => {
    const origNodeEnv = process.env.NODE_ENV;
    const origAdminKey = process.env.ADMIN_API_KEY;

    try {
      process.env.NODE_ENV = "production";
      delete process.env.ADMIN_API_KEY;

      expect(() => Effect.runSync(loadConfig)).toThrow();
    } finally {
      if (origNodeEnv === undefined) {
        delete process.env.NODE_ENV;
      } else {
        process.env.NODE_ENV = origNodeEnv;
      }
      if (origAdminKey === undefined) {
        delete process.env.ADMIN_API_KEY;
      } else {
        process.env.ADMIN_API_KEY = origAdminKey;
      }
    }
  });

  test("fails in production when ADMIN_API_KEY is too short", () => {
    const origNodeEnv = process.env.NODE_ENV;
    const origAdminKey = process.env.ADMIN_API_KEY;

    try {
      process.env.NODE_ENV = "production";
      process.env.ADMIN_API_KEY = "short";

      expect(() => Effect.runSync(loadConfig)).toThrow();
    } finally {
      if (origNodeEnv === undefined) {
        delete process.env.NODE_ENV;
      } else {
        process.env.NODE_ENV = origNodeEnv;
      }
      if (origAdminKey === undefined) {
        delete process.env.ADMIN_API_KEY;
      } else {
        process.env.ADMIN_API_KEY = origAdminKey;
      }
    }
  });

  test("fails in production when ADMIN_API_KEY is a default value", () => {
    const origNodeEnv = process.env.NODE_ENV;
    const origAdminKey = process.env.ADMIN_API_KEY;

    try {
      process.env.NODE_ENV = "production";
      process.env.ADMIN_API_KEY = "admin-secret-key-change-me";

      expect(() => Effect.runSync(loadConfig)).toThrow();
    } finally {
      if (origNodeEnv === undefined) {
        delete process.env.NODE_ENV;
      } else {
        process.env.NODE_ENV = origNodeEnv;
      }
      if (origAdminKey === undefined) {
        delete process.env.ADMIN_API_KEY;
      } else {
        process.env.ADMIN_API_KEY = origAdminKey;
      }
    }
  });
});
