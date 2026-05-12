import {
  test,
  expect,
  beforeAll,
  afterAll,
  afterEach,
  describe,
  beforeEach,
} from "bun:test";
import { writeFileSync, mkdirSync, rmSync } from "fs";
import { join } from "path";
import {
  getEnvWithNeuratradeFallback,
  resetNeuratradeConfigCache,
} from "./config";

describe("Neuratrade config fallback", () => {
  const neuratradeDir = join(
    import.meta.dir,
    ".tmp-test-neuratrade-home-config",
  );
  const realConfigPath = join(neuratradeDir, "config.json");
  const originalNeuratradeHome = process.env.NEURATRADE_HOME;
  let originalEnv: Record<string, string | undefined>;

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
