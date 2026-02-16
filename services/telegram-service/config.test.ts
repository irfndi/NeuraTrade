import {
  test,
  expect,
  beforeAll,
  afterAll,
  describe,
  beforeEach,
} from "bun:test";
import { writeFileSync, mkdirSync, rmSync, existsSync, readFileSync } from "fs";
import { join } from "path";
import { homedir } from "os";
import {
  getEnvWithNeuratradeFallback,
  resetNeuratradeConfigCache,
} from "./config";

describe("Neuratrade config fallback", () => {
  const realConfigPath = join(homedir(), ".neuratrade", "config.json");
  let backupContent: string | null = null;

  beforeAll(() => {
    if (existsSync(realConfigPath)) {
      backupContent = readFileSync(realConfigPath, "utf-8");
    }
  });

  afterAll(() => {
    if (backupContent !== null) {
      writeFileSync(realConfigPath, backupContent, "utf-8");
    }
  });

  beforeEach(() => {
    resetNeuratradeConfigCache();
    delete process.env.TELEGRAM_BOT_TOKEN;
    delete process.env.TELEGRAM_API_BASE_URL;
    delete process.env.ADMIN_API_KEY;
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
