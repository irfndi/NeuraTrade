import { describe, expect, test } from "bun:test";
import { Either, pipe } from "effect";
import {
  LocalConfigSchema,
  decodeLocalConfigEither,
  type LocalConfig,
} from "./local-config";

describe("LocalConfigSchema", () => {
  // --- Valid JSON decoding ---

  test("decodes a complete local config", () => {
    const input = {
      admin_api_key: "ak-test-123",
      telegram_test_chat_id: "-100123",
      auth: { jwt_secret: "s3cret" },
      server: { host: "0.0.0.0", port: 8080 },
      database: { driver: "sqlite", sqlite_path: "/tmp/test.db" },
      ccxt: {
        admin_api_key: "ccxt-ak",
        service_url: "http://localhost:3001",
        grpc_address: "127.0.0.1:50051",
      },
      telegram: {
        bot_token: "123456:ABC",
        api_base_url: "http://localhost:8080",
        service_url: "http://localhost:3002",
        grpc_address: "127.0.0.1:50052",
        chat_id: "12345",
      },
      services: { telegram: { chat_id: "67890" } },
      security: { admin_api_key: "sec-ak", jwt_secret: "sec-jwt" },
      ai: {
        api_key: "sk-test",
        base_url: "https://api.openai.com/v1",
        provider: "openai",
        model: "gpt-4o",
      },
    };

    const result = decodeLocalConfigEither(input);
    expect(Either.isRight(result)).toBe(true);

    if (Either.isRight(result)) {
      const cfg = result.right;
      expect(cfg.admin_api_key).toBe("ak-test-123");
      expect(cfg.telegram_test_chat_id).toBe("-100123");
      expect(cfg.auth?.jwt_secret).toBe("s3cret");
      expect(cfg.server?.host).toBe("0.0.0.0");
      expect(cfg.server?.port).toBe(8080);
      expect(cfg.database?.driver).toBe("sqlite");
      expect(cfg.database?.sqlite_path).toBe("/tmp/test.db");
      expect(cfg.ccxt?.admin_api_key).toBe("ccxt-ak");
      expect(cfg.ccxt?.service_url).toBe("http://localhost:3001");
      expect(cfg.ccxt?.grpc_address).toBe("127.0.0.1:50051");
      expect(cfg.telegram?.bot_token).toBe("123456:ABC");
      expect(cfg.telegram?.chat_id).toBe("12345");
      expect(cfg.services?.telegram?.chat_id).toBe("67890");
      expect(cfg.security?.admin_api_key).toBe("sec-ak");
      expect(cfg.security?.jwt_secret).toBe("sec-jwt");
      expect(cfg.ai?.api_key).toBe("sk-test");
      expect(cfg.ai?.provider).toBe("openai");
      expect(cfg.ai?.model).toBe("gpt-4o");
    }
  });

  test("decodes an empty config object", () => {
    const result = decodeLocalConfigEither({});
    expect(Either.isRight(result)).toBe(true);

    if (Either.isRight(result)) {
      const cfg = result.right;
      expect(cfg.admin_api_key).toBeUndefined();
      expect(cfg.auth).toBeUndefined();
      expect(cfg.server).toBeUndefined();
      expect(cfg.ai).toBeUndefined();
    }
  });

  test("decodes a partial config with only top-level fields", () => {
    const input = { admin_api_key: "key1" };
    const result = decodeLocalConfigEither(input);
    expect(Either.isRight(result)).toBe(true);

    if (Either.isRight(result)) {
      expect(result.right.admin_api_key).toBe("key1");
    }
  });

  test("decodes config with nested partial objects", () => {
    const input = {
      server: { port: 9090 },
      ai: { provider: "anthropic" },
    };
    const result = decodeLocalConfigEither(input);
    expect(Either.isRight(result)).toBe(true);

    if (Either.isRight(result)) {
      expect(result.right.server?.port).toBe(9090);
      expect(result.right.server?.host).toBeUndefined();
      expect(result.right.ai?.provider).toBe("anthropic");
      expect(result.right.ai?.model).toBeUndefined();
    }
  });

  // --- Invalid JSON ---

  test("rejects a string instead of an object", () => {
    const result = decodeLocalConfigEither("not-an-object");
    expect(Either.isLeft(result)).toBe(true);
  });

  test("rejects an array", () => {
    const result = decodeLocalConfigEither([1, 2, 3]);
    expect(Either.isLeft(result)).toBe(true);
  });

  test("rejects null", () => {
    const result = decodeLocalConfigEither(null);
    expect(Either.isLeft(result)).toBe(true);
  });

  test("rejects a number", () => {
    const result = decodeLocalConfigEither(42);
    expect(Either.isLeft(result)).toBe(true);
  });

  // --- Type compatibility ---

  test("LocalConfig type is assignable", () => {
    const cfg: LocalConfig = {};
    expect(cfg).toBeDefined();
  });
});
