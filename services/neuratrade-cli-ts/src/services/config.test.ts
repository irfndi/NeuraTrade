import { describe, expect, it, afterEach } from "bun:test";
import * as fs from "fs";
import * as os from "os";
import * as nodePath from "path";
import { Effect } from "effect";
import { BunFileSystem } from "@effect/platform-bun";
import {
  LocalConfig,
  RuntimeConfig,
  loadLocalConfigEffect,
  loadRuntimeConfigEffect,
  resolvedConfigEffect,
  ConfigLive,
  defaultLocalConfig,
  defaultRuntimeConfig,
} from "./config.ts";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Create a temp dir and return its path. */
function tmpDir(): string {
  return fs.mkdtempSync(nodePath.join(os.tmpdir(), "config-test-"));
}

/** Remove a directory recursively, ignoring errors. */
function rmDir(dir: string): void {
  try {
    fs.rmSync(dir, { recursive: true, force: true });
  } catch {
    // ignore
  }
}

/** Write a JSON file at the given path. */
function writeJson(filePath: string, data: unknown): void {
  fs.mkdirSync(nodePath.dirname(filePath), { recursive: true });
  fs.writeFileSync(filePath, JSON.stringify(data, null, 2));
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("Config service", () => {
  const originalEnv = { ...process.env };

  afterEach(() => {
    process.env = { ...originalEnv };
  });

  // -----------------------------------------------------------------------
  // defaultLocalConfig / defaultRuntimeConfig
  // -----------------------------------------------------------------------

  describe("defaultLocalConfig", () => {
    it("returns an empty local config with all fields undefined", () => {
      const d = defaultLocalConfig();
      expect(d.admin_api_key).toBeUndefined();
      expect(d.server).toBeUndefined();
      expect(d.database).toBeUndefined();
      expect(d.ai).toBeUndefined();
      expect(d.telegram).toBeUndefined();
    });
  });

  describe("defaultRuntimeConfig", () => {
    it("returns runtime config with all Go defaults applied", () => {
      const d = defaultRuntimeConfig("/tmp/home");
      expect(d.server.host).toBe("0.0.0.0");
      expect(d.server.port).toBe(8080);
      expect(d.database.driver).toBe("sqlite");
      expect(d.database.sqlite_path).toBe("/tmp/home/data/neuratrade.db");
      expect(d.redis.host).toBe("127.0.0.1");
      expect(d.redis.port).toBe(6379);
      expect(d.ccxt.service_url).toBe("http://localhost:3001");
      expect(d.ccxt.grpc_address).toBe("127.0.0.1:50051");
      expect(d.telegram.service_url).toBe("http://localhost:3002");
      expect(d.telegram.use_polling).toBe(true);
      expect(d.ai.provider).toBe("openai");
      expect(d.ai.model).toBe("gpt-4o-mini");
      expect(d.ai.temperature).toBe(0.7);
      expect(d.ai.max_tokens).toBe(4096);
      expect(d.features.enable_ai).toBe(true);
      expect(d.features.paper_trading).toBe(true);
      expect(d.features.real_trading).toBe(false);
      expect(d.gateway.bind_host).toBe("127.0.0.1");
      expect(d.gateway.ccxt_port).toBe(3001);
      expect(d.gateway.health_timeout_seconds).toBe(150);
    });
  });

  // -----------------------------------------------------------------------
  // loadLocalConfigEffect
  // -----------------------------------------------------------------------

  describe("loadLocalConfigEffect", () => {
    it("loads a valid config.json from the given home directory", async () => {
      const home = tmpDir();
      try {
        writeJson(nodePath.join(home, "config.json"), {
          admin_api_key: "test-key-123",
          server: { host: "127.0.0.1", port: 9090 },
          ai: { provider: "anthropic", model: "claude-3" },
        });

        const program = loadLocalConfigEffect(home);
        const result = await Effect.runPromise(
          program.pipe(Effect.provide(BunFileSystem.layer)),
        );

        expect(result.admin_api_key).toBe("test-key-123");
        expect(result.server?.host).toBe("127.0.0.1");
        expect(result.server?.port).toBe(9090);
        expect(result.ai?.provider).toBe("anthropic");
        expect(result.ai?.model).toBe("claude-3");
      } finally {
        rmDir(home);
      }
    });

    it("returns defaultLocalConfig when config.json is missing", async () => {
      const home = tmpDir();
      try {
        const program = loadLocalConfigEffect(home);
        const result = await Effect.runPromise(
          program.pipe(Effect.provide(BunFileSystem.layer)),
        );

        expect(result).toEqual(defaultLocalConfig());
      } finally {
        rmDir(home);
      }
    });

    it("returns defaultLocalConfig when home directory does not exist", async () => {
      const home = nodePath.join(
        os.tmpdir(),
        "nonexistent-config-dir-" + Date.now(),
      );
      const program = loadLocalConfigEffect(home);
      const result = await Effect.runPromise(
        program.pipe(Effect.provide(BunFileSystem.layer)),
      );
      expect(result).toEqual(defaultLocalConfig());
    });

    it("decodes partial config.json (only some fields present)", async () => {
      const home = tmpDir();
      try {
        writeJson(nodePath.join(home, "config.json"), {
          telegram: { bot_token: "abc:123" },
        });

        const program = loadLocalConfigEffect(home);
        const result = await Effect.runPromise(
          program.pipe(Effect.provide(BunFileSystem.layer)),
        );

        expect(result.telegram?.bot_token).toBe("abc:123");
        expect(result.admin_api_key).toBeUndefined();
        expect(result.server).toBeUndefined();
      } finally {
        rmDir(home);
      }
    });
  });

  // -----------------------------------------------------------------------
  // loadRuntimeConfigEffect
  // -----------------------------------------------------------------------

  describe("loadRuntimeConfigEffect", () => {
    it("loads a valid runtime.json and applies defaults for missing fields", async () => {
      const home = tmpDir();
      try {
        writeJson(nodePath.join(home, "runtime.json"), {
          server: { port: 4000 },
          features: { paper_trading: false },
        });

        const program = loadRuntimeConfigEffect(home);
        const result = await Effect.runPromise(
          program.pipe(Effect.provide(BunFileSystem.layer)),
        );

        // Overridden values
        expect(result.server.port).toBe(4000);
        expect(result.features.paper_trading).toBe(false);

        // Defaults applied
        expect(result.server.host).toBe("0.0.0.0");
        expect(result.database.driver).toBe("sqlite");
        expect(result.redis.host).toBe("127.0.0.1");
        expect(result.ai.provider).toBe("openai");
        expect(result.features.enable_ai).toBe(true);
      } finally {
        rmDir(home);
      }
    });

    it("returns defaultRuntimeConfig when runtime.json is missing", async () => {
      const home = tmpDir();
      try {
        const program = loadRuntimeConfigEffect(home);
        const result = await Effect.runPromise(
          program.pipe(Effect.provide(BunFileSystem.layer)),
        );

        expect(result).toEqual(defaultRuntimeConfig(home));
      } finally {
        rmDir(home);
      }
    });

    it("returns defaultRuntimeConfig when home directory does not exist", async () => {
      const home = nodePath.join(
        os.tmpdir(),
        "nonexistent-runtime-dir-" + Date.now(),
      );
      const program = loadRuntimeConfigEffect(home);
      const result = await Effect.runPromise(
        program.pipe(Effect.provide(BunFileSystem.layer)),
      );
      expect(result).toEqual(defaultRuntimeConfig(home));
    });
  });

  // -----------------------------------------------------------------------
  // resolvedConfigEffect — merge precedence
  // -----------------------------------------------------------------------

  describe("resolvedConfigEffect", () => {
    it("returns defaults when no config files exist", async () => {
      const home = tmpDir();
      try {
        const program = resolvedConfigEffect(home);
        const result = await Effect.runPromise(
          program.pipe(Effect.provide(BunFileSystem.layer)),
        );

        expect(result.server.host).toBe("0.0.0.0");
        expect(result.server.port).toBe(8080);
        expect(result.database.driver).toBe("sqlite");
      } finally {
        rmDir(home);
      }
    });

    it("local config overrides defaults", async () => {
      const home = tmpDir();
      try {
        writeJson(nodePath.join(home, "config.json"), {
          server: { host: "192.168.1.100", port: 3000 },
        });

        const program = resolvedConfigEffect(home);
        const result = await Effect.runPromise(
          program.pipe(Effect.provide(BunFileSystem.layer)),
        );

        expect(result.server.host).toBe("192.168.1.100");
        expect(result.server.port).toBe(3000);
      } finally {
        rmDir(home);
      }
    });

    it("runtime config overrides local config", async () => {
      const home = tmpDir();
      try {
        writeJson(nodePath.join(home, "config.json"), {
          server: { port: 3000 },
        });
        writeJson(nodePath.join(home, "runtime.json"), {
          server: { port: 5000 },
        });

        const program = resolvedConfigEffect(home);
        const result = await Effect.runPromise(
          program.pipe(Effect.provide(BunFileSystem.layer)),
        );

        // Runtime overrides local
        expect(result.server.port).toBe(5000);
      } finally {
        rmDir(home);
      }
    });

    it("env overrides runtime config", async () => {
      const home = tmpDir();
      try {
        writeJson(nodePath.join(home, "runtime.json"), {
          server: { port: 5000 },
        });

        process.env.SERVER_PORT = "7777";

        const program = resolvedConfigEffect(home);
        const result = await Effect.runPromise(
          program.pipe(Effect.provide(BunFileSystem.layer)),
        );

        expect(result.server.port).toBe(7777);
      } finally {
        delete process.env.SERVER_PORT;
        rmDir(home);
      }
    });

    it("env overrides local config (highest priority)", async () => {
      const home = tmpDir();
      try {
        writeJson(nodePath.join(home, "config.json"), {
          server: { port: 3000 },
          ai: { provider: "local-ai", api_key: "local-key" },
        });

        process.env.SERVER_PORT = "8888";
        process.env.AI_PROVIDER = "env-ai";

        const program = resolvedConfigEffect(home);
        const result = await Effect.runPromise(
          program.pipe(Effect.provide(BunFileSystem.layer)),
        );

        expect(result.server.port).toBe(8888);
        expect(result.ai.provider).toBe("env-ai");
      } finally {
        delete process.env.SERVER_PORT;
        delete process.env.AI_PROVIDER;
        rmDir(home);
      }
    });

    it("full precedence chain: env > runtime > local > defaults", async () => {
      const home = tmpDir();
      try {
        writeJson(nodePath.join(home, "config.json"), {
          server: { host: "10.0.0.1", port: 1111 },
          ai: { provider: "local-provider" },
        });
        writeJson(nodePath.join(home, "runtime.json"), {
          server: { host: "10.0.0.2", port: 2222 },
          ai: { provider: "runtime-provider", temperature: 0.3 },
        });

        process.env.SERVER_PORT = "3333";

        const program = resolvedConfigEffect(home);
        const result = await Effect.runPromise(
          program.pipe(Effect.provide(BunFileSystem.layer)),
        );

        // SERVER_PORT overrides port (env wins)
        expect(result.server.port).toBe(3333);
        // Runtime overrides local for host
        expect(result.server.host).toBe("10.0.0.2");
        // Runtime overrides local for provider
        expect(result.ai.provider).toBe("runtime-provider");
        // Runtime value kept (no env/local override)
        expect(result.ai.temperature).toBe(0.3);
      } finally {
        delete process.env.SERVER_PORT;
        rmDir(home);
      }
    });

    it("local-only fields (secrets) are preserved in resolved config", async () => {
      const home = tmpDir();
      const previousAdminKey = process.env.ADMIN_API_KEY;
      try {
        delete process.env.ADMIN_API_KEY;
        writeJson(nodePath.join(home, "config.json"), {
          admin_api_key: "local-admin-key",
          security: { jwt_secret: "local-jwt-secret" },
          ai: { api_key: "local-ai-key" },
        });

        const program = resolvedConfigEffect(home);
        const result = await Effect.runPromise(
          program.pipe(Effect.provide(BunFileSystem.layer)),
        );

        // These fields exist only in local config, not in runtime config
        // The resolved config should include them
        expect(result).toHaveProperty("admin_api_key", "local-admin-key");
        expect(result).toHaveProperty("jwt_secret", "local-jwt-secret");
        expect(result).toHaveProperty("ai_api_key", "local-ai-key");
      } finally {
        if (previousAdminKey === undefined) {
          delete process.env.ADMIN_API_KEY;
        } else {
          process.env.ADMIN_API_KEY = previousAdminKey;
        }
        rmDir(home);
      }
    });

    it("env ADMIN_API_KEY overrides local admin_api_key", async () => {
      const home = tmpDir();
      try {
        writeJson(nodePath.join(home, "config.json"), {
          admin_api_key: "local-admin-key",
        });

        process.env.ADMIN_API_KEY = "env-admin-key-32chars-long-enough!!";

        const program = resolvedConfigEffect(home);
        const result = await Effect.runPromise(
          program.pipe(Effect.provide(BunFileSystem.layer)),
        );

        expect(result.admin_api_key).toBe(
          "env-admin-key-32chars-long-enough!!",
        );
      } finally {
        delete process.env.ADMIN_API_KEY;
        rmDir(home);
      }
    });

    it("env TELEGRAM_BOT_TOKEN overrides local telegram.bot_token", async () => {
      const home = tmpDir();
      try {
        writeJson(nodePath.join(home, "config.json"), {
          telegram: { bot_token: "local-token" },
        });

        process.env.TELEGRAM_BOT_TOKEN = "env-token-abc";

        const program = resolvedConfigEffect(home);
        const result = await Effect.runPromise(
          program.pipe(Effect.provide(BunFileSystem.layer)),
        );

        expect(result).toHaveProperty("telegram_bot_token", "env-token-abc");
      } finally {
        delete process.env.TELEGRAM_BOT_TOKEN;
        rmDir(home);
      }
    });

    it("resolves sqlite_path with env > runtime > local > default", async () => {
      const home = tmpDir();
      try {
        delete process.env.SQLITE_PATH;

        writeJson(nodePath.join(home, "config.json"), {
          database: { sqlite_path: "/local/path/db.sqlite" },
        });
        writeJson(nodePath.join(home, "runtime.json"), {
          database: { sqlite_path: "/runtime/path/db.sqlite" },
        });

        const program = resolvedConfigEffect(home);
        const result = await Effect.runPromise(
          program.pipe(Effect.provide(BunFileSystem.layer)),
        );

        // Runtime overrides local for sqlite_path
        expect(result.database.sqlite_path).toBe("/runtime/path/db.sqlite");
      } finally {
        rmDir(home);
      }
    });

    it("env SQLITE_PATH overrides everything for sqlite_path", async () => {
      const home = tmpDir();
      try {
        writeJson(nodePath.join(home, "config.json"), {
          database: { sqlite_path: "/local/path/db.sqlite" },
        });
        writeJson(nodePath.join(home, "runtime.json"), {
          database: { sqlite_path: "/runtime/path/db.sqlite" },
        });

        process.env.SQLITE_PATH = "/env/path/db.sqlite";

        const program = resolvedConfigEffect(home);
        const result = await Effect.runPromise(
          program.pipe(Effect.provide(BunFileSystem.layer)),
        );

        expect(result.database.sqlite_path).toBe("/env/path/db.sqlite");
      } finally {
        delete process.env.SQLITE_PATH;
        rmDir(home);
      }
    });

    it("features are resolved from runtime config", async () => {
      const home = tmpDir();
      try {
        writeJson(nodePath.join(home, "runtime.json"), {
          features: {
            enable_ai: false,
            paper_trading: false,
            real_trading: true,
          },
        });

        const program = resolvedConfigEffect(home);
        const result = await Effect.runPromise(
          program.pipe(Effect.provide(BunFileSystem.layer)),
        );

        expect(result.features.enable_ai).toBe(false);
        expect(result.features.paper_trading).toBe(false);
        expect(result.features.real_trading).toBe(true);
      } finally {
        rmDir(home);
      }
    });

    it("empty string env vars are treated as unset (fall through)", async () => {
      const home = tmpDir();
      try {
        writeJson(nodePath.join(home, "config.json"), {
          server: { port: 4444 },
        });

        process.env.SERVER_PORT = "  "; // whitespace only

        const program = resolvedConfigEffect(home);
        const result = await Effect.runPromise(
          program.pipe(Effect.provide(BunFileSystem.layer)),
        );

        // Whitespace-only env treated as unset, local wins
        expect(result.server.port).toBe(4444);
      } finally {
        delete process.env.SERVER_PORT;
        rmDir(home);
      }
    });
  });

  // -----------------------------------------------------------------------
  // ConfigLive Layer
  // -----------------------------------------------------------------------

  describe("ConfigLive", () => {
    it("provides LocalConfig and RuntimeConfig tags", async () => {
      const home = tmpDir();
      try {
        writeJson(nodePath.join(home, "config.json"), {
          admin_api_key: "live-key",
        });
        writeJson(nodePath.join(home, "runtime.json"), {
          server: { port: 1234 },
        });

        const program = Effect.gen(function* () {
          const local = yield* LocalConfig;
          const runtime = yield* RuntimeConfig;
          return { local, runtime };
        });

        const result = await Effect.runPromise(
          program.pipe(
            Effect.provide(ConfigLive(home)),
            Effect.provide(BunFileSystem.layer),
          ),
        );

        expect(result.local.admin_api_key).toBe("live-key");
        expect(result.runtime.server.port).toBe(1234);
      } finally {
        rmDir(home);
      }
    });

    it("provides defaults when no config files exist", async () => {
      const home = tmpDir();
      try {
        const program = Effect.gen(function* () {
          const local = yield* LocalConfig;
          const runtime = yield* RuntimeConfig;
          return { local, runtime };
        });

        const result = await Effect.runPromise(
          program.pipe(
            Effect.provide(ConfigLive(home)),
            Effect.provide(BunFileSystem.layer),
          ),
        );

        expect(result.local).toEqual(defaultLocalConfig());
        expect(result.runtime).toEqual(defaultRuntimeConfig(home));
      } finally {
        rmDir(home);
      }
    });
  });
});
