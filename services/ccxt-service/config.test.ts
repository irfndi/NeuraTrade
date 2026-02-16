import {
  test,
  expect,
  beforeAll,
  afterAll,
  describe,
  beforeEach,
} from "bun:test";
import { writeFileSync, readFileSync, existsSync } from "fs";
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
    delete process.env.ADMIN_API_KEY;
  });

  test("getEnvWithNeuratradeFallback returns env var when set", () => {
    const testValue = "env-admin-key-12345";
    process.env.ADMIN_API_KEY = testValue;

    const result = getEnvWithNeuratradeFallback("ADMIN_API_KEY");
    expect(result).toBe(testValue);

    delete process.env.ADMIN_API_KEY;
  });

  test("getEnvWithNeuratradeFallback falls back to config.json", () => {
    const configContent = JSON.stringify({
      security: {
        admin_api_key: "config-admin-key-67890",
      },
      services: {
        ccxt: {
          url: "http://localhost:3001",
        },
      },
    });
    writeFileSync(realConfigPath, configContent, "utf-8");

    const result = getEnvWithNeuratradeFallback("ADMIN_API_KEY");
    expect(result).toBe("config-admin-key-67890");
  });

  test("getEnvWithNeuratradeFallback returns undefined when neither set", () => {
    writeFileSync(realConfigPath, JSON.stringify({}), "utf-8");

    delete process.env.ADMIN_API_KEY;
    const result = getEnvWithNeuratradeFallback("ADMIN_API_KEY");
    expect(result).toBeUndefined();
  });
});
