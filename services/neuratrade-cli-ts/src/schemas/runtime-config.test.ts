import { describe, expect, test } from "bun:test";
import { Result } from "effect";
import {
  RuntimeConfigSchema,
  decodeRuntimeConfigEither,
  type RuntimeConfig,
} from "./runtime-config";

describe("RuntimeConfigSchema", () => {
  // --- Valid JSON decoding ---

  test("decodes a complete runtime config", () => {
    const input = {
      server: { host: "0.0.0.0", port: 8080 },
      database: { driver: "sqlite", sqlite_path: "/data/neuratrade.db" },
      redis: { host: "127.0.0.1", port: 6379 },
      ccxt: {
        service_url: "http://localhost:3001",
        grpc_address: "127.0.0.1:50051",
      },
      telegram: {
        service_url: "http://localhost:3002",
        grpc_address: "127.0.0.1:50052",
        use_polling: true,
        api_base_url: "http://localhost:8080",
      },
      ai: {
        provider: "openai",
        model: "gpt-4o-mini",
        base_url: "https://api.openai.com/v1",
        temperature: 0.7,
        max_tokens: 4096,
        min_confidence: 0.7,
        daily_budget: "10",
        routing_mode: "primary",
      },
      features: {
        enable_ai: true,
        enable_ai_scalping: true,
        enable_ai_signals: false,
        enable_ai_arbitrage: false,
        paper_trading: true,
        real_trading: false,
      },
      gateway: {
        bind_host: "127.0.0.1",
        ccxt_port: 3001,
        telegram_port: 3002,
        telegram_grpc_port: 50052,
        supervised: false,
        health_timeout_seconds: 150,
        signal_timeout_seconds: 5,
        graceful_timeout_seconds: 10,
        skip_telegram: false,
      },
    };

    const result = decodeRuntimeConfigEither(input);
    expect(Result.isSuccess(result)).toBe(true);

    if (Result.isSuccess(result)) {
      const cfg = result.success;
      expect(cfg.server.host).toBe("0.0.0.0");
      expect(cfg.server.port).toBe(8080);
      expect(cfg.database.driver).toBe("sqlite");
      expect(cfg.database.sqlite_path).toBe("/data/neuratrade.db");
      expect(cfg.redis.host).toBe("127.0.0.1");
      expect(cfg.redis.port).toBe(6379);
      expect(cfg.ccxt.service_url).toBe("http://localhost:3001");
      expect(cfg.ccxt.grpc_address).toBe("127.0.0.1:50051");
      expect(cfg.telegram.service_url).toBe("http://localhost:3002");
      expect(cfg.telegram.use_polling).toBe(true);
      expect(cfg.ai.provider).toBe("openai");
      expect(cfg.ai.model).toBe("gpt-4o-mini");
      expect(cfg.ai.temperature).toBe(0.7);
      expect(cfg.ai.daily_budget).toBe("10");
      expect(cfg.features.enable_ai).toBe(true);
      expect(cfg.features.paper_trading).toBe(true);
      expect(cfg.gateway.bind_host).toBe("127.0.0.1");
      expect(cfg.gateway.ccxt_port).toBe(3001);
      expect(cfg.gateway.health_timeout_seconds).toBe(150);
    }
  });

  // --- Decoding defaults ---

  test("applies defaults when fields are missing", () => {
    const result = decodeRuntimeConfigEither({});
    expect(Result.isSuccess(result)).toBe(true);

    if (Result.isSuccess(result)) {
      const cfg = result.success;
      expect(cfg.server.host).toBe("0.0.0.0");
      expect(cfg.server.port).toBe(8080);
      expect(cfg.database.driver).toBe("sqlite");
      expect(cfg.redis.host).toBe("127.0.0.1");
      expect(cfg.redis.port).toBe(6379);
      expect(cfg.ccxt.service_url).toBe("http://localhost:3001");
      expect(cfg.ccxt.grpc_address).toBe("127.0.0.1:50051");
      expect(cfg.telegram.service_url).toBe("http://localhost:3002");
      expect(cfg.telegram.grpc_address).toBe("127.0.0.1:50052");
      expect(cfg.telegram.use_polling).toBe(true);
      expect(cfg.telegram.api_base_url).toBe("http://localhost:8080");
      expect(cfg.ai.provider).toBe("openai");
      expect(cfg.ai.model).toBe("gpt-4o-mini");
      expect(cfg.ai.temperature).toBe(0.7);
      expect(cfg.ai.max_tokens).toBe(4096);
      expect(cfg.ai.min_confidence).toBe(0.7);
      expect(cfg.ai.daily_budget).toBe("10");
      expect(cfg.ai.routing_mode).toBe("primary");
      expect(cfg.features.enable_ai).toBe(true);
      expect(cfg.features.enable_ai_scalping).toBe(true);
      expect(cfg.features.enable_ai_signals).toBe(false);
      expect(cfg.features.enable_ai_arbitrage).toBe(false);
      expect(cfg.features.paper_trading).toBe(true);
      expect(cfg.features.real_trading).toBe(false);
      expect(cfg.gateway.bind_host).toBe("127.0.0.1");
      expect(cfg.gateway.ccxt_port).toBe(3001);
      expect(cfg.gateway.telegram_port).toBe(3002);
      expect(cfg.gateway.telegram_grpc_port).toBe(50052);
      expect(cfg.gateway.supervised).toBe(false);
      expect(cfg.gateway.health_timeout_seconds).toBe(150);
      expect(cfg.gateway.signal_timeout_seconds).toBe(5);
      expect(cfg.gateway.graceful_timeout_seconds).toBe(10);
      expect(cfg.gateway.skip_telegram).toBe(false);
    }
  });

  test("overrides defaults with provided values", () => {
    const input = {
      server: { host: "192.168.1.100", port: 9090 },
      ai: { temperature: 0.3, daily_budget: "25" },
      gateway: { health_timeout_seconds: 300, skip_telegram: true },
    };
    const result = decodeRuntimeConfigEither(input);
    expect(Result.isSuccess(result)).toBe(true);

    if (Result.isSuccess(result)) {
      const cfg = result.success;
      expect(cfg.server.host).toBe("192.168.1.100");
      expect(cfg.server.port).toBe(9090);
      expect(cfg.ai.temperature).toBe(0.3);
      expect(cfg.ai.daily_budget).toBe("25");
      expect(cfg.ai.max_tokens).toBe(4096); // default still applies
      expect(cfg.gateway.health_timeout_seconds).toBe(300);
      expect(cfg.gateway.skip_telegram).toBe(true);
    }
  });

  // --- Invalid JSON ---

  test("rejects a string instead of an object", () => {
    const result = decodeRuntimeConfigEither("invalid");
    expect(Result.isFailure(result)).toBe(true);
  });

  test("rejects an array", () => {
    const result = decodeRuntimeConfigEither([]);
    expect(Result.isFailure(result)).toBe(true);
  });

  test("rejects null", () => {
    const result = decodeRuntimeConfigEither(null);
    expect(Result.isFailure(result)).toBe(true);
  });

  test("rejects wrong type for nested field", () => {
    const result = decodeRuntimeConfigEither({
      server: { host: 123 }, // host should be string
    });
    expect(Result.isFailure(result)).toBe(true);
  });

  test("rejects wrong type for boolean field", () => {
    const result = decodeRuntimeConfigEither({
      features: { enable_ai: "yes" }, // should be boolean
    });
    expect(Result.isFailure(result)).toBe(true);
  });

  // --- Type compatibility ---

  test("RuntimeConfig type is assignable", () => {
    const cfg: RuntimeConfig = {
      server: { host: "0.0.0.0", port: 8080 },
      database: { driver: "sqlite", sqlite_path: "/tmp/db" },
      redis: { host: "127.0.0.1", port: 6379 },
      ccxt: {
        service_url: "http://localhost:3001",
        grpc_address: "127.0.0.1:50051",
      },
      telegram: {
        service_url: "http://localhost:3002",
        grpc_address: "127.0.0.1:50052",
        use_polling: true,
        api_base_url: "http://localhost:8080",
      },
      ai: {
        provider: "openai",
        model: "gpt-4o-mini",
        base_url: undefined,
        temperature: 0.7,
        max_tokens: 4096,
        min_confidence: 0.7,
        daily_budget: "10",
        routing_mode: "primary",
      },
      features: {
        enable_ai: true,
        enable_ai_scalping: true,
        enable_ai_signals: false,
        enable_ai_arbitrage: false,
        paper_trading: true,
        real_trading: false,
      },
      gateway: {
        bind_host: "127.0.0.1",
        ccxt_port: 3001,
        telegram_port: 3002,
        telegram_grpc_port: 50052,
        supervised: false,
        health_timeout_seconds: 150,
        signal_timeout_seconds: 5,
        graceful_timeout_seconds: 10,
        skip_telegram: false,
      },
    };
    expect(cfg).toBeDefined();
  });
});
